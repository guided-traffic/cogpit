// Package cluster owns connections to clusters: kubeconfig loading, client-go configuration and
// authentication, discovery, the counting transport, and the few direct API calls (actions,
// streams) that the cache cannot serve.
//
// Layer L1. Only cluster, cache, terminal, helm and metrics talk to the API server.
//
// Errors from this package name the context and the problem, never a credential: kubeconfig
// files are validated through kubeconfig.Parse, whose errors are made safe, before client-go
// loads them, and connection errors are rewritten into a ConnectError.
package cluster

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// ErrNotImplemented marks a Phase 0 stub.
var ErrNotImplemented = errors.New("not implemented")

// Defaults for Options (architecture §5; preferences later).
const (
	// DefaultQPS and DefaultBurst bound lists, gets and writes per cluster.
	DefaultQPS   = 5
	DefaultBurst = 10
	// DefaultConnectTimeout bounds Connect's reachability check. It is generous because an exec
	// credential plugin may need a moment (a cloud CLI refreshing a token) on the first request.
	DefaultConnectTimeout = 15 * time.Second
	// ServedResourcesTTL is how long a discovery result is reused before the cluster's custom
	// resource definitions confirmed it, or when they cannot be listed (discocache.go); a confirmed
	// one is reused for DefaultDiscoveryCacheTTL.
	ServedResourcesTTL = 10 * time.Minute
	// partialServedResourcesTTL is how long a discovery result with failed groups (an aggregated
	// API that is down) is reused before discovery is tried again.
	partialServedResourcesTTL = time.Minute
	// discoveryQPS and discoveryBurst are discovery's own budget (kubectl's values).
	discoveryQPS   = 50
	discoveryBurst = 300
	// listQPS and listBurst are the cache's own budget for its lists (D-135, the owner's choice of
	// 2026-10-26: discovery's values): a read lists its objects once and then watches, and a connect
	// that reads many kinds, several namespaces each, would wait seconds at DefaultQPS.
	listQPS   = 50
	listBurst = 300
)

// Options say which kubeconfig context to connect to.
type Options struct {
	// KubeconfigPaths are the kubeconfig files, merged in order. Empty means client-go's default
	// loading rules: $KUBECONFIG, else ~/.kube/config. Tests always pass explicit paths.
	KubeconfigPaths []string
	// Context is the context name; empty means the files' current-context.
	Context string
	// QPS and Burst bound lists, gets and writes per cluster (not established watches).
	// Zero means the defaults 5 and 10.
	QPS   float32
	Burst int
	// ConnectTimeout bounds Connect's reachability check only; later requests are not bounded
	// by it, because watches stay open. It bounds the request below the credential plugin: a
	// plugin's run (a browser login) is bounded by LoginTimeout instead (loginrun.go, D-73). Zero
	// means DefaultConnectTimeout.
	ConnectTimeout time.Duration
	// LoginTimeout bounds each run of the context's credential plugin, at the connect and at every
	// renewal after it: a run that has not returned after it is ended (SIGTERM, then SIGKILL) and
	// the request that waited for it fails with a *LoginEndedError (loginrun.go, D-73). Zero means
	// DefaultLoginTimeout.
	LoginTimeout time.Duration
	// Login, when set, is told when a run of the context's credential plugin begins and ends, from
	// Connect on (a connect that waits for a browser login says so). Nil: nobody is told.
	Login *LoginWatch
	// RequestTimeout bounds every later request that is not a stream (health.go, D-58): a read
	// without an answer ends with ErrNoAnswer, a write as an OutcomeUnknownError. Watches, logs,
	// exec, attach and port forwards are not bounded. Zero means DefaultRequestTimeout.
	RequestTimeout time.Duration
	// ExecPolicy, when set, is asked before a context whose user runs an exec credential plugin
	// gets a client. It receives the plugin as client-go will run it (taken from the very
	// configuration the client is built from, so a file changed meanwhile cannot slip another
	// command past it); an error refuses the connection before the plugin runs. Nil runs plugins
	// as kubectl does.
	ExecPolicy func(*kubeconfig.ExecPlugin) error
	// Proxy, when set, is the proxy every request of the connection goes through (proxy.go): the
	// one the user named for this context in Cogpit, in place of the kubeconfig's proxy-url. Nil
	// leaves client-go's choice. Its password never appears in an error.
	Proxy *url.URL
	// DiscoveryCacheDir, when set, is the directory where the connection keeps its discovery answer
	// for the next connection to the same server as the same identity (discocache.go, D-65): one
	// owner-only file each, no credential in its name or content. Empty: discovery in memory only.
	DiscoveryCacheDir string
	// DiscoveryCacheTTL is how long an answer the cluster's definitions confirmed is reused; zero
	// means DefaultDiscoveryCacheTTL.
	DiscoveryCacheTTL time.Duration
	// Diag, when set, is the context's diagnostics log (diaglog.go, D-133), kept by the caller across
	// the context's connections; nil gives the connection a log of its own.
	Diag *DiagLog
	// Trace, when set, receives Connect's steps for the connection log (connecttrace.go, D-66):
	// the kubeconfig, the server, the login, the certificate check, the proxy and the first
	// answer, and the error picture of a failed one. It is called on Connect's goroutine and must
	// not block. No step holds a credential.
	Trace func(ConnectStep)

	// dial, in tests, replaces the dialer below the connection's (a resolver of their own, so that
	// a lookup reaches no network; connectstages_test.go).
	dial func(ctx context.Context, network, address string) (net.Conn, error)
}

// loadingRules returns client-go's loading rules for paths: exactly these files in this order,
// or the default rules ($KUBECONFIG, else ~/.kube/config) when paths is empty.
func loadingRules(paths []string) *clientcmd.ClientConfigLoadingRules {
	var rules *clientcmd.ClientConfigLoadingRules
	if len(paths) > 0 {
		rules = &clientcmd.ClientConfigLoadingRules{Precedence: append([]string(nil), paths...)}
	} else {
		rules = clientcmd.NewDefaultClientConfigLoadingRules()
	}
	// The default rules copy a kubeconfig from a legacy location (~/.kube/.kubeconfig) to
	// ~/.kube/config when the latter is missing. That is an implicit kubeconfig write, which
	// Cogpit never does.
	rules.MigrationRules = nil
	// Missing files are reported by checkFiles, not by client-go's warner on stderr.
	rules.WarnIfAllMissing = false
	return rules
}

// NamedKubeconfigFiles returns the kubeconfig files the user names for this run: paths themselves
// (Cogpit's --kubeconfig), else the files $KUBECONFIG lists; absolute, clean, without duplicates.
// Unlike client-go's default loading it does not fall back to ~/.kube/config (that file belongs to
// the synced ~/.kube folder, D-28), it reads no file and does not check that a file exists.
func NamedKubeconfigFiles(paths []string) []string {
	if len(paths) == 0 {
		paths = filepath.SplitList(os.Getenv(clientcmd.RecommendedConfigPathEnvVar))
	}
	var out []string
	seen := map[string]bool{}
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		abs = filepath.Clean(abs)
		if !seen[abs] {
			seen[abs] = true
			out = append(out, abs)
		}
	}
	return out
}

// checkFiles reads and validates the kubeconfig files of a loading precedence. It returns the
// absolute paths of the files that exist and parse, and one safe error per file that does not
// parse. A missing file is an error only when the caller named it (explicit); in the default
// precedence a missing ~/.kube/config is normal.
func checkFiles(precedence []string, explicit bool) (usable []string, problems []error) {
	seen := map[string]bool{}
	for _, p := range precedence {
		if p == "" {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		data, err := os.ReadFile(abs)
		switch {
		case errors.Is(err, os.ErrNotExist):
			if explicit {
				problems = append(problems, fmt.Errorf("kubeconfig file %s does not exist", abs))
			}
			continue
		case err != nil:
			problems = append(problems, fmt.Errorf("kubeconfig file %s cannot be read: %w", abs, err))
			continue
		}
		if _, err := kubeconfig.Parse(data); err != nil {
			problems = append(problems, fmt.Errorf("kubeconfig file %s: %w", abs, err))
			continue
		}
		usable = append(usable, abs)
	}
	return usable, problems
}

// ListContexts returns the contexts of the merged kubeconfig files (default loading rules when
// paths is empty), sorted by name. A file that is missing (when named explicitly) or does not
// parse is skipped: ListContexts then returns the contexts of the other files together with an
// error that names each skipped file, so one broken file does not hide the rest.
func ListContexts(paths []string) ([]kubeconfig.Context, error) {
	rules := loadingRules(paths)
	usable, problems := checkFiles(rules.GetLoadingPrecedence(), len(paths) > 0)
	rules.Precedence = usable
	cfg, err := rules.Load()
	if err != nil {
		problems = append(problems, fmt.Errorf("kubeconfig: %w", kubeconfig.SafeError(err)))
	}
	out := kubeconfig.FromConfig(cfg)
	if out == nil {
		out = []kubeconfig.Context{}
	}
	fillClientCerts(out, describePaths(usable)) // a client-certificate file's dates (clientcert.go, D-75)
	return out, errors.Join(problems...)
}

// ConnectError says why Connect failed. Its text names the context and the problem, never a
// credential; Err is the underlying error, kept for errors.Is and errors.As.
type ConnectError struct {
	Context string
	Problem string
	Err     error
	// Stages are the connect's stages, the failed one with its cause (connectstages.go, D-114); nil
	// for a failure outside Connect. Cause is the failed stage's text: the failure in a sentence,
	// shorter than Problem, which keeps the whole chain.
	Stages []Stage
	Cause  string
}

func (e *ConnectError) Error() string {
	if e.Context == "" {
		return e.Problem
	}
	return fmt.Sprintf("context %q: %s", e.Context, e.Problem)
}

func (e *ConnectError) Unwrap() error { return e.Err }

// Conn is one connection to one kubeconfig context: its client configuration, clients, cached
// discovery and request counter. It is safe for concurrent use.
type Conn struct {
	contextName string
	namespace   string
	paths       []string
	server      string
	version     string                 // the API server's version as Connect read it, the discovery's
	seen        atomic.Pointer[string] // the version the last heartbeat read (SeenVersion)
	config      *rest.Config
	httpClient  *http.Client
	kube        kubernetes.Interface
	dyn         dynamic.Interface
	meta        metadata.Interface
	cacheDyn    dynamic.Interface  // the cache's, with the lists' budget
	cacheMeta   metadata.Interface // the cache's metadata-only reads, with the lists' budget
	disco       discovery.DiscoveryInterface
	counter     *RequestCounter
	warnings    *warningLog

	// The diagnostics log and the rate limits that note in it (diaglog.go, diagtransport.go, D-133);
	// the access reviews' limit is theirs (access.go).
	diag       *DiagLog
	limiter    *measuredLimiter
	listLimit  *measuredLimiter
	discoLimit *measuredLimiter

	// The discovery answer: client-go's memory cache over the disk cache over disco (discocache.go),
	// built on first use; discoCfg says where it is kept.
	discoMu  sync.Mutex
	discoCfg discoveryConfig
	dcache   *discoveryCache
	cached   discovery.CachedDiscoveryInterfaceWithContext

	objects objectMapper // the REST mapper of the generic object writes (objects.go)
	access  reviewClient // the access reviews' client, with their own budget (access.go)
	health  *health      // the gate, the login guard, the tracked dialer and the probe's client (health.go); nil in tests' bare Conns

	// The credential plugin's standard error (pluginstderr.go, D-66): nil without a plugin; secrets
	// are the user entry's values it is scrubbed of.
	stderr  *pluginStderr
	secrets []string
	trace   tracer         // Connect's steps; nil after Connect
	stages  *connectStages // Connect's stages (connectstages.go); nil after Connect

	// hold holds the credential plugin while the engine says so (loginhold.go, D-69 item 4); nil
	// without a plugin.
	hold *loginHold

	closed atomic.Bool

	who whoAmI // who the connection is logged in as (whoami.go, D-68)

	// The login as the kubeconfig held it at the connect (loginfile.go, D-81): its fingerprint, and
	// whether it runs a credential plugin.
	login  string
	plugin bool
}

// Connect builds a connection for opts.Context. It verifies the server is reachable with one
// discovery request (GET /version, bounded by opts.ConnectTimeout) and returns a *ConnectError a
// user can act on otherwise. Credentials never appear in the error.
func Connect(ctx context.Context, opts Options) (_ *Conn, err error) {
	// A program folder that appeared since start, or one the user added, counts from this login on
	// (toolpath.go, D-120): before the plugin's approval resolves its program.
	RefreshToolPath()
	// The connect's stages (connectstages.go, D-114): reported last, on the error the caller sees.
	stages := &connectStages{secrets: proxySecrets(opts.Proxy)}
	defer func() { err = stages.finish(ctx, tracer(opts.Trace), err) }()
	defer func() { err = scrubProxySecret(err, opts.Proxy) }()
	name := opts.Context
	rules := loadingRules(opts.KubeconfigPaths)
	usable, problems := checkFiles(rules.GetLoadingPrecedence(), len(opts.KubeconfigPaths) > 0)
	if len(problems) > 0 {
		return nil, &ConnectError{Context: name, Problem: errors.Join(problems...).Error()}
	}
	rules.Precedence = usable

	cc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{CurrentContext: name})
	raw, err := cc.RawConfig()
	if err != nil {
		return nil, &ConnectError{Context: name, Problem: "cannot load kubeconfig: " + kubeconfig.SafeError(err).Error()}
	}
	// client-go quotes a proxy-url it cannot use whole, its password included (proxyurl.go).
	kubeProxySecrets := kubeconfigProxySecrets(raw)
	defer func() { err = scrubSecrets(err, kubeProxySecrets) }()
	stages.secrets = append(stages.secrets, kubeProxySecrets...)
	if name == "" {
		name = raw.CurrentContext
	}
	if name == "" {
		return nil, &ConnectError{Problem: "no context given and the kubeconfig sets no current-context"}
	}
	kctx, ok := raw.Contexts[name]
	if !ok || kctx == nil {
		return nil, &ConnectError{Context: name, Problem: "no such context in the kubeconfig files " + describePaths(usable)}
	}
	trace := tracer(opts.Trace)
	trace.step(StepKubeconfig, describeKubeconfig(name, kctx, usable))

	// A token file outside the kubeconfig's folder is read by ClientConfig below: ask first (D-17).
	if err := checkTokenFilePolicy(opts.ExecPolicy, raw, name, kctx); err != nil {
		return nil, &ConnectError{Context: name, Problem: err.Error(), Err: err}
	}
	cfg, err := cc.ClientConfig()
	if err != nil {
		stages.failedConfig(configCause(err)) // "the client certificate file … does not exist"
		return nil, &ConnectError{Context: name, Problem: "invalid kubeconfig: " + kubeconfig.SafeError(err).Error()}
	}
	stages.secrets = append(stages.secrets, knownLoginSecrets(raw, kctx)...)
	if err := checkExecPolicy(opts.ExecPolicy, raw, name, kctx, cfg); err != nil {
		return nil, &ConnectError{Context: name, Problem: err.Error(), Err: err}
	}
	// clientcmd's persister writes tokens a legacy auth provider (oidc) refreshes back into the
	// user's kubeconfig file. Cogpit makes no implicit kubeconfig writes: refreshed tokens stay in
	// memory (gap H-4, docs/security/credentials.md).
	cfg.AuthConfigPersister = memoryPersister{}
	applyProxy(cfg, opts.Proxy) // the cluster's proxy in Cogpit's settings, over the kubeconfig's
	trace.traceSetup(cfg, raw, kctx, opts.ExecPolicy != nil, opts.Proxy != nil)
	if cl := raw.Clusters[kctx.Cluster]; cl != nil {
		stages.configuredFrom(cfg, cl.ProxyURL)
	} else {
		stages.configuredFrom(cfg, "")
	}
	namespace, _, err := cc.Namespace()
	if err != nil || namespace == "" {
		namespace = "default"
	}

	cfg.QPS, cfg.Burst = opts.QPS, opts.Burst
	if cfg.QPS <= 0 {
		cfg.QPS = DefaultQPS
	}
	if cfg.Burst <= 0 {
		cfg.Burst = DefaultBurst
	}
	// One token bucket for the cluster. Left to client-go, the typed clientset and the dynamic
	// client would each build their own from QPS and Burst, doubling the budget; copies of
	// RESTConfig() share this one too. It counts what waits at it (diagtransport.go, D-133).
	diag := opts.Diag
	if diag == nil {
		diag = NewDiagLog()
	}
	limiter := newMeasuredLimiter(BudgetRequests, cfg.QPS, cfg.Burst, diag)
	cfg.RateLimiter = limiter
	// The API server's warnings, logged once per connection, not once per request (warnings.go).
	warnings := newWarningLog(name)
	cfg.WarningHandlerWithContext = warnings
	// No cfg.Timeout: it would end every watch after that long. A request that is not a stream is
	// bounded by RequestTimeout instead (health.go).
	server := kubeconfig.StripUserInfo(cfg.Host)
	if opts.dial != nil {
		cfg.Dial = opts.dial
	}
	counter := NewRequestCounter()
	counter.prefix = serverPathPrefix(cfg.Host)
	cfg.Wrap(counter.Wrap)
	// The request timeout, the login guard and the gate, in every transport built from cfg; the
	// probe's anonymous client of its own (D-58).
	health := installHealth(cfg, counter.prefix, opts.RequestTimeout)
	// The diagnostics log's round-tripper, above the timeout and the gates (D-133).
	diag.Connecting(server)
	installDiag(cfg, diag, counter.prefix)
	if err := health.anonymousProbe(cfg, counter); err != nil {
		return nil, &ConnectError{Context: name, Problem: "cannot build the client: " + kubeconfig.SafeError(err).Error(), Err: err}
	}

	// The credential plugin's standard error, kept for a failed login (pluginstderr.go): set on the
	// Authenticator the client built next uses.
	stderr := capturePluginStderr(cfg)
	// The hold of the plugin while the cluster is not shown (loginhold.go), on the same Authenticator,
	// and its runs' bound and watch (loginrun.go).
	health.guard.ends.watch = opts.Login
	hold := registerLoginHold(cfg, health.guard, opts.LoginTimeout)
	defer func() {
		if err != nil {
			hold.drop() // no connection uses the registration
		}
	}()
	httpClient, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return nil, &ConnectError{Context: name, Problem: "cannot build the client: " + kubeconfig.SafeError(err).Error(), Err: err}
	}
	// client-go read the client certificate file just now: its dates for the cluster list, which
	// does not read a file in a place macOS protects (clientcert.go, D-75). The key is not read.
	noteClientCertFile(cfg.CertFile)
	// The gate in front of client-go's credential plugin: a request it holds never runs the plugin.
	httpClient.Transport = health.gate.outer(httpClient.Transport)
	// One HTTP client for both clientsets: one connection pool (one HTTP/2 connection) per
	// cluster, not one per client.
	kube, err := kubernetes.NewForConfigAndClient(cfg, httpClient)
	if err != nil {
		return nil, &ConnectError{Context: name, Problem: "cannot build the client: " + kubeconfig.SafeError(err).Error(), Err: err}
	}
	dyn, err := dynamic.NewForConfigAndClient(cfg, httpClient)
	if err != nil {
		return nil, &ConnectError{Context: name, Problem: "cannot build the client: " + kubeconfig.SafeError(err).Error(), Err: err}
	}
	// The metadata client serves metadata-only informers (Secrets, H-8): it asks the server for
	// PartialObjectMetadata, so object content never crosses the wire.
	meta, err := metadata.NewForConfigAndClient(cfg, httpClient)
	if err != nil {
		return nil, &ConnectError{Context: name, Problem: "cannot build the client: " + kubeconfig.SafeError(err).Error(), Err: err}
	}
	// The cache's lists get their own, larger budget (D-135): served from the API server's watch
	// cache (resourceVersion=0), they are cheap for it; gets and writes stay at the requests' budget.
	// The clients share the HTTP client, and with it the connection, the counters and the gates.
	listCfg := rest.CopyConfig(cfg)
	listCfg.QPS, listCfg.Burst = listQPS, listBurst
	listLimiter := newMeasuredLimiter(BudgetLists, listQPS, listBurst, diag)
	listCfg.RateLimiter = listLimiter
	cacheDyn, err := dynamic.NewForConfigAndClient(listCfg, httpClient)
	if err != nil {
		return nil, &ConnectError{Context: name, Problem: "cannot build the client: " + kubeconfig.SafeError(err).Error(), Err: err}
	}
	cacheMeta, err := metadata.NewForConfigAndClient(listCfg, httpClient)
	if err != nil {
		return nil, &ConnectError{Context: name, Problem: "cannot build the client: " + kubeconfig.SafeError(err).Error(), Err: err}
	}
	// Discovery gets its own, larger budget, as kubectl gives it: a cluster without aggregated
	// discovery answers with one request per group version (and client-go repeats the round once
	// when a group fails), which at 5 per second would take many seconds.
	discoCfg := rest.CopyConfig(cfg)
	discoCfg.QPS, discoCfg.Burst = discoveryQPS, discoveryBurst
	discoLimiter := newMeasuredLimiter(BudgetDiscovery, discoveryQPS, discoveryBurst, diag)
	discoCfg.RateLimiter = discoLimiter
	disco, err := discovery.NewDiscoveryClientForConfigAndClient(discoCfg, httpClient)
	if err != nil {
		return nil, &ConnectError{Context: name, Problem: "cannot build the client: " + kubeconfig.SafeError(err).Error(), Err: err}
	}

	c := &Conn{
		contextName: name,
		namespace:   namespace,
		paths:       usable,
		server:      server,
		config:      cfg,
		httpClient:  httpClient,
		kube:        kube,
		dyn:         dyn,
		meta:        meta,
		cacheDyn:    cacheDyn,
		cacheMeta:   cacheMeta,
		disco:       disco,
		counter:     counter,
		diag:        diag,
		limiter:     limiter,
		listLimit:   listLimiter,
		discoLimit:  discoLimiter,
		warnings:    warnings,
		health:      health,
		stderr:      stderr,
		secrets:     knownLoginSecrets(raw, kctx),
		trace:       trace,
		hold:        hold,
		login:       loginFingerprint(raw, kctx),
		plugin:      cfg.ExecProvider != nil,
		stages:      stages,
	}
	file := kctx.LocationOfOrigin
	if file == "" {
		file = describePaths(usable)
	}
	stages.passedConfig(configText(name, file, cfg.Host, cfg), connectLimit(opts.ConnectTimeout))
	err = c.checkReachable(ctx, opts.ConnectTimeout)
	c.trace, c.stages = nil, nil
	if err != nil {
		c.Close()
		return nil, err
	}
	key := discoveryKey(server, cfg.ServerName, usable, kctx.Cluster, kctx.AuthInfo, cfg.Impersonate)
	c.discoCfg = discoveryConfig{file: discoveryFileName(opts.DiscoveryCacheDir, key), key: key, server: server,
		version: c.version, context: name, ttl: opts.DiscoveryCacheTTL}
	return c, nil
}

// memoryPersister keeps an auth provider's refreshed configuration in memory only: the provider
// holds it itself, so persisting is a no-op that reports success.
type memoryPersister struct{}

func (memoryPersister) Persist(map[string]string) error { return nil }

// checkReachable makes the one cheap request Connect pays for: GET /version. It proves the
// server answers and accepts the credentials (a request with bad credentials is rejected before
// authorization, even for /version). timeout bounds the request itself, below the credential plugin
// (withRequestBound): the plugin's run, which may wait for the user in a browser, has its own bound
// (LoginTimeout), and ctx's cancel ends it (D-73).
func (c *Conn) checkReachable(ctx context.Context, timeout time.Duration) error {
	timeout = connectLimit(timeout)
	start := time.Now()
	mark, ended := c.stderr.mark(), c.loginEndMark()
	rctx := ctx
	if c.stages != nil {
		rctx = c.stages.withTrace(ctx) // its DNS, TCP and TLS, from client-go's own request (D-114)
	}
	info, err := c.firstAnswer(rctx, timeout)
	took := time.Since(start)
	run, ran := c.loginRunSince(ctx, start) // its end noted, or waited for (loginrun.go)
	c.traceLoginRun(run, ran)
	c.stageLoginRun(run, ran)
	if e := c.loginEndedSince(ended); err != nil && e != nil {
		err = endedWithStderr(err, e, c.stderr, mark, c.secrets)
	} else {
		err = withPluginStderr(err, c.stderr, mark, c.secrets)
	}
	if err == nil {
		if info != nil {
			c.version = info.GitVersion
		}
		if c.stages != nil {
			c.stages.answered(c.version)
		}
		c.trace.timed(StepAnswer, "First answer: GET /version: Kubernetes "+c.version, took, false)
		return nil
	}
	c.trace.timed(StepAnswer, fmt.Sprintf("First answer: GET /version failed (the limit is %s, a login not counted)", timeout), took, true)
	var pe *PluginError
	stderr := ""
	if errors.As(err, &pe) {
		stderr = pe.Stderr
	}
	c.trace.failure(err, stderr)
	return &ConnectError{Context: c.contextName, Problem: c.describeConnectFailure(ctx, timeout, err), Err: err}
}

// abandonAfter is how long a cancelled connect waits for its first request once it ended the
// credential plugin's run: past it the request is left to end by itself (a child of the plugin
// may keep its output open, and client-go waits for that).
const abandonAfter = killGrace + 2*time.Second

// cancelSettle is how long a cancelled connect without a plugin's run of its own to end waits for
// its first request to see the cancel.
const cancelSettle = 500 * time.Millisecond

// firstAnswer is Connect's GET /version. A cancel of ctx ends a credential plugin's run the request
// waits for (CancelLogin) instead of waiting for it, as client-go would: its exec authenticator runs
// the plugin without a context.
func (c *Conn) firstAnswer(ctx context.Context, timeout time.Duration) (*version.Info, error) {
	type answer struct {
		info *version.Info
		err  error
	}
	ch := make(chan answer, 1)
	go func() {
		var a answer
		defer func() {
			if r := recover(); r != nil {
				a = answer{err: fmt.Errorf("internal error: %v", r)}
			}
			ch <- a
		}()
		a.info, a.err = discovery.ToDiscoveryInterfaceWithContext(c.disco).ServerVersionWithContext(withRequestBound(ctx, timeout))
	}()
	select {
	case a := <-ch:
		return a.info, a.err
	case <-ctx.Done():
	}
	wait := abandonAfter
	if !c.CancelLogin() { // the run this connect waits for ends; one another connection needs too runs on
		wait = cancelSettle
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case a := <-ch:
		return a.info, a.err
	case <-t.C:
		return nil, ctx.Err()
	}
}

// describeConnectFailure turns a failed request into a sentence a user can act on.
func (c *Conn) describeConnectFailure(ctx context.Context, timeout time.Duration, err error) string {
	var ue *url.Error
	var ended *LoginEndedError
	switch {
	case errors.As(err, &ended) && ended.TimedOut() && !ended.RunsOn:
		return ended.Error() + "; Connect starts it again"
	case errors.As(err, &ended):
		return ended.Error()
	case ctx.Err() != nil:
		return "connecting was cancelled"
	case errors.Is(err, ErrNoAnswer):
		return fmt.Sprintf("no answer from the API server %s within %s", c.server, timeout)
	case apierrors.IsUnauthorized(err):
		return fmt.Sprintf("the API server %s rejected the credentials (401 Unauthorized); log in again or check the user entry", c.server)
	case apierrors.IsForbidden(err):
		return fmt.Sprintf("the API server %s denied access (403 Forbidden)", c.server)
	case errors.As(err, &ue):
		// The URL adds nothing but noise; the inner error says what went wrong (connection
		// refused, TLS, DNS, an exec credential plugin that is not on PATH). A certificate that
		// failed the check gets a hint at the kubeconfig field to look at (tls.go).
		msg := fmt.Sprintf("cannot reach the API server %s: %s", c.server, kubeconfig.SafeError(ue.Err).Error())
		if hint := certificateHint(ue.Err); hint != "" {
			msg += "; " + hint
		}
		return msg
	default:
		return fmt.Sprintf("the API server %s did not answer as expected: %s", c.server, kubeconfig.SafeError(err).Error())
	}
}

// serverPathPrefix returns the path part of a server URL, without a trailing slash: requests to a
// cluster behind a path-routing proxy carry it in front of /api and /apis.
func serverPathPrefix(host string) string {
	u, err := url.Parse(host)
	if err != nil {
		return ""
	}
	return strings.TrimRight(u.Path, "/")
}

func describePaths(paths []string) string {
	if len(paths) == 0 {
		return "(none found)"
	}
	return strings.Join(paths, ", ")
}

// Context returns the kubeconfig context name.
func (c *Conn) Context() string { return c.contextName }

// Namespace returns the context's default namespace ("default" when the context sets none).
func (c *Conn) Namespace() string { return c.namespace }

// KubeconfigPaths returns the kubeconfig files the connection was loaded from, in precedence
// order. A terminal for this context puts its own file in front of these.
func (c *Conn) KubeconfigPaths() []string { return append([]string(nil), c.paths...) }

// Server returns the API server URL, without user information.
func (c *Conn) Server() string { return c.server }

// ServerVersion returns the API server's version as Connect saw it ("v1.36.1").
func (c *Conn) ServerVersion() string { return c.version }

// SeenVersion returns the API server's version as the last heartbeat that answered read it, ""
// before the first: an upgrade of the cluster while Cogpit stays connected shows here, while
// ServerVersion stays the version the connection's discovery was made for (the owner's
// colleague's case of 2026-10-25; the engine takes it, connwatch_version.go).
func (c *Conn) SeenVersion() string {
	if v := c.seen.Load(); v != nil {
		return *v
	}
	return ""
}

// RESTConfig returns a copy of the client configuration, with the counting transport installed.
func (c *Conn) RESTConfig() *rest.Config { return rest.CopyConfig(c.config) }

// Kube returns the typed clientset.
func (c *Conn) Kube() kubernetes.Interface { return c.kube }

// Dynamic returns the dynamic client of gets and writes, with the requests' budget.
func (c *Conn) Dynamic() dynamic.Interface { return c.dyn }

// Metadata returns the metadata client with the requests' budget.
func (c *Conn) Metadata() metadata.Interface { return c.meta }

// CacheDynamic returns the dynamic client the cache builds its informers from: the lists' budget
// (D-135); Dynamic for a connection built around a clientset (tests).
func (c *Conn) CacheDynamic() dynamic.Interface {
	if c.cacheDyn != nil {
		return c.cacheDyn
	}
	return c.dyn
}

// CacheMetadata returns the metadata client the cache builds its metadata-only informers from: the
// lists' budget (D-135); Metadata for a connection built around a clientset (tests).
func (c *Conn) CacheMetadata() metadata.Interface {
	if c.cacheMeta != nil {
		return c.cacheMeta
	}
	return c.meta
}

// Counter returns the connection's API request counter.
func (c *Conn) Counter() *RequestCounter { return c.counter }

// DiagLog returns the context's diagnostics log the connection notes in (D-133); nil for a
// connection built around a clientset (tests).
func (c *Conn) DiagLog() *DiagLog { return c.diag }

// Budgets returns the connection's rate limits and what waited at each since it began: the
// requests', the cache's lists' (D-135), discovery's and, once the first review was asked, the access reviews' (D-133).
func (c *Conn) Budgets() []BudgetStats {
	var out []BudgetStats
	for _, m := range []*measuredLimiter{c.limiter, c.listLimit, c.discoLimit, c.access.limiter.Load()} {
		if m != nil {
			out = append(out, m.Stats())
		}
	}
	return out
}

// discovery returns the connection's cached discovery (client-go's memory cache over the disk
// cache, discocache.go) and its disk layer, built on first use.
func (c *Conn) discovery() (discovery.CachedDiscoveryInterfaceWithContext, *discoveryCache, error) {
	c.discoMu.Lock()
	defer c.discoMu.Unlock()
	if c.cached != nil {
		return c.cached, c.dcache, nil
	}
	if c.disco == nil {
		return nil, nil, c.errNoDiscovery()
	}
	c.dcache = newDiscoveryCache(discovery.ToDiscoveryInterfaceWithContext(c.disco), c.discoCfg)
	c.cached = memory.NewMemCacheClientWithContext(c.dcache)
	return c.cached, c.dcache, nil
}

// ServedResources returns the group/version/resources the cluster serves in their preferred
// version and that support list and watch (discovery verbs: whether the user may list them is
// an access-review question). The answer is cached (discocache.go): in memory and, with a
// DiscoveryCacheDir, on disk for the next connection, for ServedResourcesTTL until the cluster's
// custom resource definitions confirmed it (DefinitionsSeen), then DefaultDiscoveryCacheTTL; groups
// whose discovery fails (an aggregated API that is down) are left out, and such a partial answer is
// kept for a minute only and never written. The map is the caller's to keep.
func (c *Conn) ServedResources(ctx context.Context) (map[schema.GroupVersionResource]bool, error) {
	cached, dc, err := c.discovery()
	if err != nil {
		return nil, err
	}
	if dc.expired() {
		cached.InvalidateWithContext(ctx)
	}
	lists, err := discovery.ServerPreferredResourcesWithContext(ctx, cached)
	if err != nil {
		var failed *discovery.ErrGroupDiscoveryFailed
		if !errors.As(err, &failed) || len(lists) == 0 {
			return nil, fmt.Errorf("context %q: discovery: %w", c.contextName, err)
		}
		if dc.firstPartialNotice() { // once per answer, not at every read of it
			groups := make([]string, 0, len(failed.Groups))
			for gv := range failed.Groups {
				groups = append(groups, gv.String())
			}
			slog.Warn("cluster: discovery left out groups that did not answer", "context", c.contextName, "groups", groups)
		}
	}
	out := map[schema.GroupVersionResource]bool{}
	for _, l := range discovery.FilteredBy(discovery.SupportsAllVerbs{Verbs: []string{"list", "watch"}}, lists) {
		gv, err := schema.ParseGroupVersion(l.GroupVersion)
		if err != nil {
			continue
		}
		for _, r := range l.APIResources {
			if strings.Contains(r.Name, "/") { // a subresource
				continue
			}
			out[gv.WithResource(r.Name)] = true
		}
	}
	return out, nil
}

// InvalidateServedResources drops the cached discovery answer, in memory and on disk, so the next
// ServedResources or kind mapping asks the server again.
func (c *Conn) InvalidateServedResources() {
	cached, _, err := c.discovery()
	if err != nil {
		return
	}
	cached.InvalidateWithContext(context.Background())
	c.resetMapper()
}

// DefinitionsSeen takes a digest of the cluster's custom resource definitions, as the engine's
// watch of them lists them (names, generations, established): a cached discovery answer checked
// against other definitions, or read from a file nobody checked, is dropped from memory and disk,
// so the next read asks the server; an answer asked in this connection takes the digest and is
// kept on disk for DefaultDiscoveryCacheTTL. It reports whether it dropped the answer.
func (c *Conn) DefinitionsSeen(digest string) bool {
	if digest == "" {
		return false
	}
	_, dc, err := c.discovery()
	if err != nil || !dc.definitionsSeen(digest) {
		return false
	}
	c.InvalidateServedResources()
	return true
}

// DiscoveryInfo says where the connection's discovery answer came from (the disk cache or the
// server), when it was fetched and how large it is, for the Diagnostics view.
func (c *Conn) DiscoveryInfo() DiscoveryInfo {
	_, dc, err := c.discovery()
	if err != nil {
		return DiscoveryInfo{}
	}
	return dc.info()
}

// Close releases the connection's idle network connections. Requests in flight (watches of a
// cache that is still running) are not cancelled: stop the cache first.
func (c *Conn) Close() {
	if !c.closed.CompareAndSwap(false, true) {
		return
	}
	c.CancelLogin() // a credential plugin's run it alone waits for ends with it (loginrun.go, D-73)
	c.hold.drop()   // nil-safe: a closed connection neither holds nor frees its login
	if c.httpClient != nil {
		c.httpClient.CloseIdleConnections()
	}
}
