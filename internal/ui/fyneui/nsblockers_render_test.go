package fyneui

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
	"github.com/guided-traffic/cogpit/internal/testkit/enginekit"
)

// A Terminating namespace's drawer (WF43, D-98) over the real engine: walk-fin as the namespace
// controller left it on kind (a Config Map and a Secret held by finalizers, and a third kind),
// for a user who may not list Secrets there; then Remove Finalizers from the list's row, its
// confirmation as rows. PNGs: namespace_blocking_{dark,light}.png,
// confirm_remove_finalizers_{dark,light}.png.

var renderConfigMapsGVR = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}

// finalizerWriter answers the writes of the namespace render: none is expected but the removal,
// which it records.
type finalizerWriter struct {
	mu      sync.Mutex
	removed []string
}

func (*finalizerWriter) RestartDeployment(context.Context, string, string, string) error { return nil }
func (*finalizerWriter) DeletePod(context.Context, string, string, string) error         { return nil }
func (w *finalizerWriter) RemoveFinalizers(_ context.Context, _ schema.GroupVersionResource, ns, name, _ string, fins []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.removed = append(w.removed, ns+"/"+name+" "+strings.Join(fins, ","))
	return nil
}

// noSecrets is an Access that denies listing Secrets and allows the rest.
type noSecrets struct{}

func (noSecrets) Allowed(_ string, v engine.Verb) (allowed, known bool) {
	return v.Verb != "list" || v.Resource != "secrets", true
}

// limitedConnector is the memory connector for a user who may not list Secrets.
type limitedConnector struct{ enginekit.MemoryConnector }

func (c limitedConnector) Connect(ctx context.Context, name string) (*engine.Backend, error) {
	b, err := c.MemoryConnector.Connect(ctx, name)
	if err == nil {
		b.Access = noSecrets{}
	}
	return b, err
}

func heldObject(kind, name string, fins ...string) *unstructured.Unstructured {
	list := make([]any, len(fins))
	for i, f := range fins {
		list[i] = f
	}
	return testkit.Object("v1", kind, "walk-fin", name, map[string]any{
		"metadata": map[string]any{"deletionTimestamp": "2026-10-19T10:00:00Z", "finalizers": list},
	})
}

func blockingFixture() *testkit.MemorySource {
	src := testkit.NewMemorySource()
	cond := func(typ, status, msg string) map[string]any {
		return map[string]any{"type": typ, "status": status, "message": msg, "lastTransitionTime": "2026-10-19T10:00:05Z"}
	}
	src.Set(testkit.NamespacesGVR, testkit.Object("v1", "Namespace", "", "walk-fin", map[string]any{
		"metadata": map[string]any{"deletionTimestamp": "2026-10-19T10:00:00Z", "labels": map[string]any{"kubernetes.io/metadata.name": "walk-fin"}},
		"spec":     map[string]any{"finalizers": []any{"kubernetes"}},
		"status": map[string]any{"phase": "Terminating", "conditions": []any{
			cond(kinds.CondDiscoveryFailure, "False", "All resources successfully discovered"),
			cond(kinds.CondParsingFailure, "False", "All legacy kube types successfully parsed"),
			cond(kinds.CondContentFailure, "False", "All content successfully deleted, may be waiting on finalization"),
			cond(kinds.CondContentRemaining, "True", "Some resources are remaining: configmaps. has 2 resource instances, secrets. has 1 resource instances"),
			cond(kinds.CondFinalizersRemaining, "True", "Some content in the namespace has finalizers remaining: example.com/walk-research in 2 resource instances, example.com/other in 1 resource instances"),
		}},
	}))
	src.Set(renderConfigMapsGVR, heldObject("ConfigMap", "walk-cm", "example.com/walk-research", "example.com/other"))
	src.Set(renderConfigMapsGVR, heldObject("ConfigMap", "walk-settings", "example.com/walk-research"))
	src.Set(testkit.SecretsGVR, heldObject("Secret", "walk-secret", "example.com/walk-research"))
	return src
}

func renderNamespaceBlocking(t *testing.T, mode ThemeMode, drawerPNG, confirmPNG string) {
	writer := &finalizerWriter{}
	conn := limitedConnector{enginekit.MemoryConnector{Context: "kind-cogpit", Src: blockingFixture(), Profiles: kinds.Builtin(), Writer: writer}}
	s := newTestEngine(engine.Options{Connector: conn, SnapshotInterval: 5 * time.Millisecond, ClusterSettings: engine.NewClusterSettingsStore("")})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChangeClusterSettings("kind-cogpit", func(cs *engine.ClusterSettings) error {
		cs.SetIntegrations(0, 0, engine.IntAll) // no integrations: nothing asked, nothing looked for
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sh, w, q := newEngineShell(t, s, Options{AutoConnect: "kind-cogpit", Theme: mode})
	q.until(t, "the connection", func() bool { return sh.ui.Connection.State == engine.Connected && len(sh.kinds) > 0 })
	ref := model.ObjectRef{Context: "kind-cogpit", Kind: "namespaces", Name: "walk-fin"}
	mustDispatch(t, s, "ui.openView", map[string]any{"view": "namespaces"})
	mustDispatch(t, s, "ui.select", map[string]any{"refs": []model.ObjectRef{ref}})
	var blocking *model.DetailSection
	q.until(t, "the drawer's Blocking Deletion", func() bool {
		m := sh.drawer.last
		if m == nil {
			return false
		}
		for i, sec := range m.Sections {
			if sec.Title == engine.BlockingTitle && len(sec.Items) >= 5 {
				blocking = &m.Sections[i]
				return true
			}
		}
		return false
	})
	names := make([]string, 0, len(blocking.Items))
	for _, it := range blocking.Items {
		names = append(names, it.Name+" "+it.Value)
	}
	if !slices.Contains(names, "Config Map walk-cm") || !slices.Contains(names, "Secrets 1 remaining; you may not list secrets in walk-fin") {
		t.Fatalf("the section %q", names)
	}
	settle(w)
	savePNG(t, w, drawerPNG)

	// Remove Finalizers from walk-cm's row: the confirmation names the object and, as rows, the
	// finalizers it removes and the cleanup it skips.
	sh.runItemAction(ref, blocking.Items[0].Actions[0])
	q.until(t, "the confirmation", func() bool { return sh.confirmer.current != nil })
	savePNG(t, w, confirmPNG)
	test.Tap(sh.confirmer.current.confirm)
	q.until(t, "the removal", func() bool {
		writer.mu.Lock()
		defer writer.mu.Unlock()
		return slices.Equal(writer.removed, []string{"walk-fin/walk-cm example.com/walk-research,example.com/other"})
	})
}

func TestRenderNamespaceBlockingDark(t *testing.T) {
	renderNamespaceBlocking(t, ThemeDark, "namespace_blocking_dark.png", "confirm_remove_finalizers_dark.png")
}

func TestRenderNamespaceBlockingLight(t *testing.T) {
	renderNamespaceBlocking(t, ThemeLight, "namespace_blocking_light.png", "confirm_remove_finalizers_light.png")
}
