package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/kubeconfig"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// DemoContext is the context name of the synthetic demo cluster.
const DemoContext = "demo"

// errDemoUnavailable is returned when the demo package provides no source, and by a release
// build, which has no demo cluster (connector_nodemo.go).
var errDemoUnavailable = errors.New("the demo cluster is not available in this build")

// IsDemoContext reports whether a context id names the synthetic demo cluster: never in a release
// build (DemoBuilt false, D-67), where a kubeconfig context may be called "demo" like any other.
func IsDemoContext(id string) bool {
	return DemoBuilt && id == DemoContext
}

// ClusterConnector is the Connector of the desktop app and the CLI: the contexts of the user's
// kubeconfig files through package cluster, and optionally the synthetic demo cluster.
type ClusterConnector struct {
	// KubeconfigPaths are the kubeconfig files, merged in order. Empty means client-go's default
	// loading rules ($KUBECONFIG, else ~/.kube/config). Used only without Sources: with them every
	// context comes from a source file read on its own and nothing is merged (D-28).
	KubeconfigPaths []string
	// IncludeDemo offers the demo cluster as the context DemoContext. It shadows a kubeconfig
	// context of the same name. A release build has no demo cluster and ignores it (DemoBuilt, D-67).
	IncludeDemo bool
	// DemoOptions size the demo cluster; a release build's are empty.
	DemoOptions DemoOptions
	// WarmPeriod is how long the cache keeps an unused informer; zero means
	// cache.DefaultWarmPeriod.
	WarmPeriod time.Duration
	// StreamStall bounds a streamed initial list's silence before its end (cache.Options,
	// initiallists.go, D-new); zero means cache.DefaultStreamStall.
	StreamStall time.Duration
	// MetricsOptions tune the polling of every backend's metrics (intervals, TTL, series bounds);
	// the zero value is the defaults of architecture §9. Tests shorten the intervals.
	MetricsOptions metrics.Options
	// MetricsSettings are the Prometheus overrides the user set, per context (metricssettings.go);
	// a connection starts with its context's. Nil: none kept.
	MetricsSettings *MetricsSettings
	// ClusterSettings are the user's settings per context (clustersettings.go), the session's own
	// store: a connection goes through its context's proxy (proxy.go). Nil: no proxy of Cogpit's.
	ClusterSettings *ClusterSettingsStore
	// Logger receives connection warnings; nil means slog.Default().
	Logger *slog.Logger
	// Sources are the kubeconfig files and sync folders (the user's ~/.kube, the files named in
	// $KUBECONFIG, and those the user added in Cogpit; kubeconfigs.go): every context is offered as
	// "<context> [<file>]". With Sources, KubeconfigPaths is not read. Nil: the merged
	// KubeconfigPaths (tests, and a start without a configuration directory).
	Sources *KubeconfigSources
	// DiscoveryCacheDir is where connections keep their discovery answers for the next connection
	// (cluster.Options.DiscoveryCacheDir, D-65): <user cache dir>/Cogpit/discovery in both
	// binaries (app.Dirs.DiscoveryCacheDir). Empty: discovery in memory only (tests).
	DiscoveryCacheDir string
	// LoginTimeout bounds each run of a context's credential plugin (cluster.Options.LoginTimeout,
	// D-73); zero means cluster.DefaultLoginTimeout, 5 minutes. Tests shorten it.
	LoginTimeout time.Duration
}

// metricsOptions are the options of a backend's metrics hub.
func (c *ClusterConnector) metricsOptions() metrics.Options {
	o := c.MetricsOptions
	if o.Logger == nil {
		o.Logger = c.logger()
	}
	return o
}

// demoOffered reports whether the connector offers the demo cluster: asked to, in a build that has
// one.
func (c *ClusterConnector) demoOffered() bool {
	return DemoBuilt && c.IncludeDemo
}

func (c *ClusterConnector) logger() *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return slog.Default()
}

// Contexts returns the kubeconfig contexts, sorted by name, and the demo cluster last when it is
// offered (demoOffered). If the kubeconfig cannot be loaded, the demo entry is still returned,
// together with the error.
func (c *ClusterConnector) Contexts() ([]ContextInfo, error) {
	var ctxs []kubeconfig.Context
	var err error
	if c.Sources == nil {
		ctxs, err = cluster.ListContexts(c.KubeconfigPaths)
	}
	out := make([]ContextInfo, 0, len(ctxs)+1)
	for _, k := range ctxs {
		if c.demoOffered() && k.Name == DemoContext {
			c.logger().Warn("engine: the kubeconfig context is shadowed by the demo cluster", "context", k.Name)
			continue
		}
		out = append(out, ContextInfo{
			Name: k.Name, Cluster: k.Cluster, User: k.User, Namespace: k.Namespace,
			Server: k.Server, Current: k.Current, Insecure: k.Insecure, ClientCert: k.ClientCert,
		})
	}
	out, err = c.withSourceContexts(out, err)
	if c.demoOffered() {
		out = append(out, ContextInfo{Name: DemoContext, Namespace: "default", Demo: true})
	}
	if err != nil {
		return out, fmt.Errorf("kubeconfig: %w", err)
	}
	return out, nil
}

// Connect connects one context: the demo cluster, or a kubeconfig context whose served kinds
// (from discovery) select the built-in profiles. If discovery fails, every built-in profile is
// offered and a kind the cluster lacks shows its error in its view. A real cluster's backend also
// watches its custom resource definitions (Backend.CustomResources) and answers its commands'
// Enabled from the connected user's access reviews (Backend.Access, accessreviews.go).
func (c *ClusterConnector) Connect(ctx context.Context, name string) (*Backend, error) {
	if c.demoOffered() && name == DemoContext {
		return c.connectDemo()
	}
	opts, sourceID, err := c.connectOptions(name)
	if err != nil {
		return nil, withConfigStages(err)
	}
	settingsID := name // the context id the settings are kept under, as the UI state names it
	if sourceID != "" {
		settingsID = sourceID
	}
	rawProxy, _ := c.ClusterSettings.proxyOf(settingsID) // an unreadable file is reported where it loads: no proxy then
	if opts.Proxy, err = parseClusterProxy(rawProxy); err != nil {
		return nil, withConfigStages(&cluster.ConnectError{Context: settingsID, Problem: "the proxy in its Cluster Settings: " + err.Error(), Err: err})
	}
	opts.ExecPolicy = proxyBoundPolicy(opts.ExecPolicy, RedactProxyURL(rawProxy))
	opts.DiscoveryCacheDir = c.DiscoveryCacheDir
	steps := connectStepsOf(ctx) // the connection log's attempt (connectlog_steps.go, D-66)
	opts.Trace = steps.traceFunc()
	opts.Diag = steps.diagLog()                                       // the context's diagnostics log, kept across its connections (D-133)
	opts.Login, opts.LoginTimeout = loginWatchOf(ctx), c.LoginTimeout // its plugin's runs (loginwait.go, D-73)
	conn, err := cluster.Connect(ctx, opts)
	if err != nil {
		return nil, err
	}
	profiles := kinds.Builtin()
	customResources := true
	discoStart := time.Now()
	served, err := conn.ServedResources(ctx)
	steps.logDiscovery(conn, len(served), time.Since(discoStart), err)
	if err != nil {
		c.logger().Warn("engine: discovery failed; offering every built-in kind", "context", name, "err", err)
	} else {
		profiles = servedProfiles(profiles, served)
		customResources = served[crdsGVR]
	}
	profiles = withApplications(withHelmReleases(profiles)) // applications.go
	// The local kinds (port forwards) are Cogpit's own objects: every real cluster offers them.
	profiles = append(slices.Clip(profiles), kinds.Local()...)
	ch := cache.New(cache.DynamicSource(conn.Dynamic(), conn.Metadata()), cache.Options{WarmPeriod: c.WarmPeriod, Logger: c.Logger,
		StreamStall: c.StreamStall, Lists: c.ClusterSettings.streamsOffOf(settingsID)}) // a cluster whose streams are off lists from the start (initiallists.go, D-new)
	wireDiagnostics(conn, ch) // its reads named, their fallbacks and slow loads noted (diaglogs.go, D-133)
	ctxName := conn.Context()
	if ctxName == "" {
		ctxName = name
	}
	sourceFile := ""
	if sourceID != "" { // a source's context is known by its id, not its name in the file
		ctxName = sourceID
		sourceFile = opts.KubeconfigPaths[0]
	}
	access := newAccessReviews(conn, conn.Namespace())
	var once sync.Once
	be := &Backend{
		Context: ctxName, Namespace: conn.Namespace(), Cache: ch, Conn: conn, Profiles: profiles,
		CustomResources: customResources, Access: access, SourceFile: sourceFile, CredentialPlugin: conn.CredentialPlugin(),
		Close: func() {
			once.Do(func() {
				access.stop()
				ch.Close()
				conn.Close()
			})
		},
	}
	// Metrics: nothing is asked of the cluster until a Pods or Nodes view or a drawer is open. Both
	// providers together: the Metrics Server for the current values, Prometheus (found among the
	// cached Services when a drawer opens or the Metrics Server fails) for history (§9).
	if src, prom, err := metrics.SourcesForConn(conn, metrics.ServiceSourceFunc(be.promServices), metrics.PrometheusOptions{Logger: c.logger()}); err != nil {
		c.logger().Warn("engine: no metrics client; CPU and memory stay empty", "context", name, "err", err)
	} else {
		be.Metrics, be.PrometheusDetector, be.MetricsSettings = metrics.NewHubFor(src, c.metricsOptions()), prom, c.MetricsSettings
		prom.SetOverride(c.MetricsSettings.Override(ctxName)) // the user's Prometheus for this context, if set
		prom.SetQuery(c.MetricsSettings.Query(ctxName))       // and how it is asked (format, filter, HTTPS)
	}
	return be, nil
}

// withHelmReleases adds the Releases kind: Helm's releases come from the cluster's Secrets, so it
// is offered wherever Secrets are, each backend with its own memo of decoded releases.
func withHelmReleases(profiles []*model.KindProfile) []*model.KindProfile {
	if slices.ContainsFunc(profiles, isSecrets) {
		return append(slices.Clip(profiles), helm.NewReleaseProfile())
	}
	return profiles
}

// servedProfiles keeps the profiles whose group, version and resource the cluster serves. A
// profile whose preferred version is not served is kept at the first of its FallbackVersions that
// is, as a copy with that version, so that every cache scope and write of the kind uses a version
// the cluster serves.
func servedProfiles(all []*model.KindProfile, served map[schema.GroupVersionResource]bool) []*model.KindProfile {
	out := make([]*model.KindProfile, 0, len(all))
	for _, p := range all {
		if served[p.GVR()] {
			out = append(out, p)
			continue
		}
		for _, v := range p.FallbackVersions {
			if served[schema.GroupVersionResource{Group: p.Group, Version: v, Resource: p.Resource}] {
				at := *p
				at.Version = v
				out = append(out, &at)
				break
			}
		}
	}
	return out
}
