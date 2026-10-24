// Package commands registers Cogpit's engine commands, one file per area (ui.go, resources.go,
// dock.go, ...). Every command becomes a menu item, a palette entry and an MCP tool through the
// registry; the GUI and agents dispatch the same commands.
//
// Layer L3, with package engine.
package commands

import (
	"errors"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Kind keys the commands refer to.
const (
	kindPods        = "pods"
	kindDeployments = "deployments"
)

// RegisterAll registers every command. It fails on the first registration error (a duplicate id
// is a programming error found at start-up).
func RegisterAll(r *engine.Registry) error {
	g := &registrar{r: r}
	registerUI(g)
	registerCluster(g)
	registerResources(g)
	registerDock(g)
	registerDockOutput(g)
	registerActions(g)
	registerSelection(g)
	registerNodes(g)
	registerNodeShell(g)
	registerWorkloads(g)
	registerEdit(g)
	registerConfigMapData(g)
	registerDrafts(g)
	registerRevert(g)
	registerAccess(g)
	registerPortForward(g)
	registerSecrets(g)
	registerSecretData(g) // a Secret's values edited in the drawer, the window's only (D-59)
	registerHelm(g)
	registerHelmUpgrade(g)
	registerHelmRepos(g)
	registerHelmHTTP(g)
	registerHelmInstall(g)
	registerChartDrawer(g)
	registerWorkloadActions(g)
	registerAttach(g)
	registerDebugContainer(g) // a pod's debug (ephemeral) container (D-102)
	registerLogs(g)
	registerCopy(g)
	registerTables(g)
	registerViewRows(g)
	registerStatusFilter(g)
	registerOverview(g)
	registerClusterOverview(g)
	registerAlerts(g) // the Alertmanager integration: the alerts pill and the alert board (D-77)
	registerKubeconfigs(g)
	registerMetrics(g)
	registerMetricsRange(g)
	registerDrawerTables(g)
	registerHotbar(g)
	registerCreateDialogs(g)
	registerSecretCreate(g)
	registerPalette(g)
	registerApplications(g)
	registerArgoCD(g)
	registerPreferences(g)
	registerProgramFolders(g) // Preferences › Kubernetes › Program folders (D-120)
	registerLoginItem(g)
	registerClusterSettings(g)
	registerIntegrations(g) // the question of a new cluster's integrations (D-76)
	registerAllowance(g)
	registerAccentColor(g) // a cluster's accent colour, the user's alone (D-80)
	registerLightStrip(g)  // how the strip under a view's header shows it, the user's alone too (D-new)
	registerAudit(g)
	registerDiagnostics(g)
	registerTabs(g)
	registerTabMoves(g) // tabmove.go: ui.tab.move, ui.tab.moveLeft and Right, dock.move (GW4)
	registerWelcome(g)
	registerClusterMenu(g)
	registerNotifications(g)
	registerNavScope(g)
	registerNavigator(g)
	registerLayout(g)
	registerKubectl(g)
	registerFlux(g)
	registerUpdates(g)
	registerChat(g)
	registerRestartMany(g)
	registerConnection(g)
	registerJobs(g)
	registerKyverno(g) // the Kyverno integration's violations (D-79)
	// Remove finalizers on an object being deleted (WF43, D-98).
	registerFinalizers(g)
	return g.err
}

// registrar registers commands and keeps the first error.
type registrar struct {
	r   *engine.Registry
	err error
}

func add[P any](g *registrar, spec engine.Spec[P]) {
	if g.err == nil {
		g.err = engine.Register(g.r, spec)
	}
}

// labelDelete is the label of every kind's last action, as Lens labels it: object.delete, and the
// commands that take its place for kinds that are not API objects (Helm releases, port forwards).
const labelDelete = "Delete"

// cacheReads are the verbs of commands that read through the cache: the informer lists and
// watches the kind they read. The kind is a parameter, hence the wildcard.
var cacheReads = []engine.Verb{{Verb: "list", Resource: "*"}, {Verb: "watch", Resource: "*"}}

// connected is the Enabled check of commands that need the cluster the window shows.
func connected(s *engine.Session) (*engine.Backend, error) { return connectedOn(s, "") }

// connectedOn is the Enabled check of commands that need a connected cluster: the context's, ""
// the one the window shows (D-69: a command on an object uses its reference's context, one with a
// context parameter that context). A context that is not connected is an error that says so
// (engine.NotConnectedError): no command connects a cluster by itself.
func connectedOn(s *engine.Session, contextID string) (*engine.Backend, error) {
	b, err := s.TargetBackend(contextID)
	if err != nil {
		if contextID == "" {
			return nil, errors.New("no cluster connected")
		}
		return nil, err
	}
	return b, nil
}

// writable is the Enabled check of commands that change the cluster the window shows.
func writable(s *engine.Session) (*engine.Backend, error) { return writableOn(s, "") }

// writableOn is the Enabled check of commands that change a connected context's cluster, "" the
// shown one.
func writableOn(s *engine.Session, contextID string) (*engine.Backend, error) {
	b, err := connectedOn(s, contextID)
	if err != nil {
		return nil, err
	}
	if !b.CanWrite() {
		return nil, errors.New("the demo cluster cannot be changed")
	}
	return b, nil
}

// streamableOn is the Enabled check of commands that open logs, shells or terminals on a connected
// context's cluster, "" the shown one.
func streamableOn(s *engine.Session, contextID, what string) (*engine.Backend, error) {
	b, err := connectedOn(s, contextID)
	if err != nil {
		return nil, err
	}
	if b.Conn == nil {
		return nil, errors.New(what + " are not available on the demo cluster")
	}
	return b, nil
}

// contextOf is the context parameter of a command's parameters (engine.OnCluster), "" without one
// or for the shown cluster.
func contextOf(p any) string {
	if t, ok := p.(engine.Targeted); ok {
		return t.TargetContext()
	}
	return ""
}

// refsContext is the context a selection's references name (the first that names one; Dispatch
// refuses references of several clusters), "" for the shown cluster.
func refsContext(refs []model.ObjectRef) string {
	for _, r := range refs {
		if r.Context != "" {
			return r.Context
		}
	}
	return ""
}

// cachedTarget is the Enabled check of actions on one object: the reference must be of the kind
// and the object must be in the cache.
func cachedTarget(s *engine.Session, ref model.ObjectRef, kind, kindErr string) error {
	if ref.Kind != kind {
		return errors.New(kindErr)
	}
	if _, err := s.Lookup(ref); err != nil {
		return err
	}
	return nil
}

// targetOf is the Targets function of commands on one object: the resolved reference, so that
// the confirmation names its namespace and context.
func targetOf(s *engine.Session, ref model.ObjectRef) ([]model.ObjectRef, error) {
	r, _, err := s.ResolveRef(ref)
	if err != nil {
		return nil, err
	}
	return []model.ObjectRef{r}, nil
}
