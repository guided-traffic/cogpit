package engine

import (
	"context"
	"log/slog"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/kubeconfig"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Backend is one connected cluster as the engine sees it.
type Backend struct {
	// Context is the kubeconfig context name ("demo" for the demo cluster).
	Context string
	// Namespace is the context's default namespace.
	Namespace string
	// Cache serves every read.
	Cache *cache.Cache
	// Streaming says the connection began streaming its initial lists (the cluster's opt-in,
	// initiallists.go, D-135); false: it lists them.
	Streaming bool
	// Conn is the connection for streams and actions; nil for the demo cluster, where logs,
	// shells, terminals and mutating commands are disabled.
	Conn *cluster.Conn
	// Actions performs the cluster writes of the action commands. Nil means Conn does; tests
	// substitute a fake here.
	Actions Actions
	// Profiles are the built-in kinds this cluster serves, in sidebar order. AllProfiles adds the
	// kinds of its custom resource definitions.
	Profiles []*model.KindProfile
	// CustomResources makes the engine watch the cluster's custom resource definitions (one watch
	// through the cache, for as long as the backend is connected) and offer a view for each kind
	// they define. The real cluster connector sets it; the demo cluster and backends that leave it
	// off have the built-in kinds only.
	CustomResources bool
	// Discovery lists the group/version/resources the cluster serves (API discovery, as
	// cluster.Conn's ServedResources). Nil means Conn's; without either nothing is discovered. The
	// custom kinds use it when the definitions cannot be listed (A90, customkinds_discovery.go).
	Discovery func(ctx context.Context) (map[schema.GroupVersionResource]bool, error)
	// Metrics polls the cluster's metrics while a view or the drawer that shows them is open
	// (metrics.go, architecture §9). Nil means the backend has none; it is closed with the backend.
	Metrics *metrics.Hub
	// PrometheusDetector is the Prometheus half of the metrics detection, kept so that the cluster's
	// settings can set the override (SetOverride, then Metrics.Forget); nil without a hub of a real
	// cluster.
	PrometheusDetector *metrics.PrometheusDetector
	// MetricsSettings keeps the cluster's Prometheus override in Cogpit's preferences
	// (metricssettings.go); nil keeps it for the connection only.
	MetricsSettings *MetricsSettings
	// Demo marks the synthetic demo cluster.
	Demo bool
	// CredentialPlugin is set when the context logs in through a credential plugin (the user
	// entry's exec): Log in again runs it once on the connection. Without one, Log in again
	// connects anew from the kubeconfig file, and a login the API server rejects reconnects by
	// itself once the file holds another one (reconnect.go, D-81). The connector sets it from
	// cluster.Conn.CredentialPlugin.
	CredentialPlugin bool
	// DemoStats reports the demo cluster's counters; nil for real clusters.
	DemoStats func() any
	// Version is the API server's version of a backend without a connection: the demo cluster's
	// (ServerVersion); Conn's otherwise.
	Version string
	// Close stops the cache and closes the connection.
	Close func()
	// SourceFile is the kubeconfig file of a kubeconfig source the connection was made from
	// (kubeconfigs.go); empty for the demo cluster and a connector without sources. When the file goes
	// away, or its source is removed, the session closes the connection.
	SourceFile string

	// Access answers the access reviews of the connected user (access.go); nil allows everything,
	// and the API server decides.
	Access Access

	// Health is what the connection watch asks (connwatch.go, D-58). Nil means Conn; without either
	// (the demo cluster) the connection has no watch.
	Health HealthProber

	// watch is the connection's watch; the Session sets it before it publishes the backend.
	watch *connWatch
	// login follows the runs of the connection's credential plugin (loginwait.go, D-73); the
	// Session sets it before it publishes the backend, nil for a backend made otherwise.
	login *loginNotes

	// custom is the custom kinds watch (customkinds.go); the Session sets it before it publishes
	// the backend, nil without CustomResources.
	custom *customKinds
	// nsWatch watches the namespaces while the shown cluster's namespace filter names namespaces
	// (nswatch.go, AW11); the Session sets it before it publishes the backend, nil without a cache.
	nsWatch *nsWatch
	// local holds the objects of the local kinds (kindscope.go), made on first use.
	local localStore
	// apps is the Applications view's cache (applications.go), made on first use.
	apps appsStore
	// forwards are the backend's running port forwards (portforward.go); they end with it.
	forwards forwardSet
	// ns is what decides whether a namespaced kind is read namespace by namespace
	// (accessiblens_plan.go).
	ns nsState
	// scrub replaces the cached Secrets' values in what agents read (secretscrub.go, H-82).
	scrub secretScrub
	// alerts polls the cluster's Alertmanager while the integration is on (alerts.go, D-77).
	alerts alertSource
	// history polls the cluster's Prometheus for the alert board's history while the board is
	// shown (alerts_history.go).
	history historySource
	// owners indexes the cached pods' controllers for the alert board (alerts_owner.go).
	owners podOwnerIndex
	// overlays are the integrations' cells and drawer sections on its objects (overlay.go, D-79).
	overlays overlays
	// renewal is the kubeconfig login it last reconnected by itself for (reconnect.go, D-81).
	renewal fileRenewal
	// live is the API server's version as the heartbeats read it since the connect, once it
	// changed (connwatch_version.go); nil while it is the connect's.
	live atomic.Pointer[string]
}

// Actions are the cluster writes of the MVP's action commands. *cluster.Conn implements them.
type Actions interface {
	// RestartDeployment restarts a Deployment as `kubectl rollout restart` does; a non-empty uid
	// must match the Deployment's (cluster.ErrReplaced otherwise).
	RestartDeployment(ctx context.Context, namespace, name, uid string) error
	// DeletePod deletes one pod with the default grace period; a non-empty uid must match the
	// pod's, so that a replacement with the same name is not deleted.
	DeletePod(ctx context.Context, namespace, name, uid string) error
}

// Writer returns what performs the backend's writes, or nil when it cannot write (the demo
// cluster).
func (b *Backend) Writer() Actions {
	if b.Actions != nil {
		return b.Actions
	}
	if b.Conn != nil {
		return b.Conn
	}
	return nil
}

// CanWrite reports whether the backend can run mutating commands: false for the demo cluster.
func (b *Backend) CanWrite() bool { return b.Writer() != nil }

// ServerVersion returns the API server's version ("v1.36.1"): as the heartbeats read it once it
// changed while connected (connwatch_version.go), else as the connect saw it (connectVersion); ""
// when nothing knows one.
func (b *Backend) ServerVersion() string {
	if b == nil {
		return ""
	}
	if v := b.live.Load(); v != nil {
		return *v
	}
	return b.connectVersion()
}

// connectVersion returns the API server's version as the connect saw it, the one the connection's
// discovery was made for: the connection's, else Version.
func (b *Backend) connectVersion() string {
	if b.Conn != nil {
		return b.Conn.ServerVersion()
	}
	return b.Version
}

// setServerVersion makes v the backend's version (ServerVersion).
func (b *Backend) setServerVersion(v string) { b.live.Store(&v) }

// close stops the backend's cache and connection, recovering a panicking Close.
func (b *Backend) close(log *slog.Logger) {
	if b == nil {
		return
	}
	b.Conn.CancelLogin() // a credential plugin's run it waits for ends first: nothing below waits for it (D-73); nil-safe
	b.watch.stop()       // the connection watch (connwatch.go); nil-safe
	b.ns.stop()          // the session's replan loop (accessiblens_plan.go)
	if b.custom != nil {
		b.custom.stop()
	}
	b.nsWatch.stop() // nil-safe (nswatch.go)
	b.forwards.stopAll()
	// The Alertmanager's polls (alerts.go), before the connection closes.
	b.alerts.stop()
	b.history.stop()
	b.overlays.close() // the integrations' reads (overlay.go)
	b.apps.close()     // before the cluster's cache: it releases its handles on the workloads
	b.local.close()
	if b.Metrics != nil {
		b.Metrics.Close()
	}
	if b.Close == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Error("engine: closing the backend failed", "context", b.Context, "panic", r)
		}
	}()
	b.Close()
}

// Profile returns the backend's profile for a kind key: a built-in kind ("pods"), or a kind of
// the cluster's custom resource definitions ("gateways.gateway.networking.k8s.io"). A custom
// kind's profile changes when its definition does: callers hold on to it for one operation only.
func (b *Backend) Profile(kind string) (*model.KindProfile, bool) {
	if p := b.builtinProfile(kind); p != nil {
		return p, true
	}
	if set := b.customSet(); set != nil {
		if p, ok := set.byKey[kind]; ok {
			return p, true
		}
	}
	return nil, false
}

// AllProfiles returns every kind the cluster serves: the built-in profiles in sidebar order, then
// the kinds of its custom resource definitions sorted by API group and Kind. Callers must not
// modify the result; it is a snapshot that a change of the definitions replaces.
func (b *Backend) AllProfiles() []*model.KindProfile {
	custom := b.CustomProfiles()
	if len(custom) == 0 {
		return b.Profiles
	}
	out := make([]*model.KindProfile, 0, len(b.Profiles)+len(custom))
	out = append(out, b.Profiles...)
	return append(out, custom...)
}

// ContextInfo is one context Cogpit offers: a row of the navigator's section (cluster.contexts).
type ContextInfo struct {
	Name      string `json:"name"`
	Cluster   string `json:"cluster,omitempty"`
	User      string `json:"user,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Server    string `json:"server,omitempty"`
	Current   bool   `json:"current,omitempty"` // the kubeconfig's current-context
	Demo      bool   `json:"demo,omitempty"`    // the synthetic demo cluster
	// Source, KubeContext and File describe a context from a kubeconfig source the user added in
	// Cogpit (kubeconfigs.go): the file's label ("dev.yaml", "kubeconfigs/team.yaml"),
	// the context's name in that file (Name is then "<context> [<label>]") and the file's path.
	// Empty for the demo cluster and a connector without sources.
	Source      string `json:"source,omitempty"`
	KubeContext string `json:"kubeContext,omitempty"`
	File        string `json:"file,omitempty"`
	// ConnectProblem is set when the last attempt to connect to this context failed, or its
	// connection was closed because its kubeconfig file went away; cluster.connectLog has the log.
	ConnectProblem bool `json:"connectProblem,omitempty"`
	// Exec is ExecApproved, ExecRequired or ExecChanged for a source context whose user runs an
	// exec credential plugin or names a token file outside its folder (D-17), which run or are read
	// only once the user allowed it, or whose cluster skips the certificate check (A28), which
	// connects only once the user allowed it; empty otherwise. ExecKind says which: "program",
	// "tokenFile", "both", or "insecure" when the unchecked certificate is all (a plugin's approval
	// covers its context's check).
	Exec     string `json:"exec,omitempty"`
	ExecKind string `json:"execKind,omitempty"`
	// Insecure is set when the context's cluster skips the check of the server's certificate
	// (insecure-skip-tls-verify): the window warns, a source context asks once before its first
	// connect (Exec), and connecting writes a warning into the connect log and marks the
	// connection (Connection.TLSUnchecked; tlswarning.go).
	Insecure bool `json:"insecure,omitempty"`
	// ClientCert is the validity of the client certificate the context's user logs in with
	// (client-certificate-data, or a client-certificate file; D-75): notBefore, notAfter and the
	// state at the time of the listing (valid, expiring within 14 days or a fifth of its lifetime,
	// expired, notYetValid). Only the dates: never the certificate, never the key. Nil for a user
	// without one, one that runs a credential plugin, a certificate that cannot be read, and a
	// file in a place macOS protects until a connect read it. It never blocks a connect.
	ClientCert *kubeconfig.CertValidity `json:"clientCert,omitempty"`
}

// Connector lists the contexts a user can connect to and connects one.
type Connector interface {
	Contexts() ([]ContextInfo, error)
	Connect(ctx context.Context, name string) (*Backend, error)
}
