package commands_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The values of the agent view's cluster (H-82, H-63).
const (
	dbPass      = "pw-ScrubMe-4711-xyz" // a Secret's value
	hrValuePass = "inline-hr-pass-0815" // a Helm release's inline value
	gitToken    = "ghp_inlineGitToken42"
	hookToken   = "hook-token-9a7b5c3d"
)

var (
	gitReposGVR  = schema.GroupVersionResource{Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "gitrepositories"}
	providersGVR = schema.GroupVersionResource{Group: "notification.toolkit.fluxcd.io", Version: "v1beta3", Resource: "providers"}
)

// agentViewConnector connects "test" with the Secrets and ConfigMaps profiles, the test profiles
// and the custom kinds watch (Flux's definitions).
type agentViewConnector struct{ src *testkit.MemorySource }

func (c agentViewConnector) Contexts() ([]engine.ContextInfo, error) {
	return []engine.ContextInfo{{Name: "test", Current: true}}, nil
}

func (c agentViewConnector) Connect(context.Context, string) (*engine.Backend, error) {
	ch := cache.New(c.src, cache.Options{WarmPeriod: time.Minute})
	secrets, _ := kinds.ByKey("secrets")
	cms, _ := kinds.ByKey("configmaps")
	return &engine.Backend{Context: "test", Namespace: "default", Cache: ch, Close: ch.Close, CustomResources: true,
		Profiles: append([]*model.KindProfile{secrets, cms}, testProfiles()...), Conn: &cluster.Conn{}, Actions: &fakeWriter{}}, nil
}

func newAgentViewHarness(t *testing.T) *harness {
	t.Helper()
	src := fluxSource()
	src.Set(testkit.SecretsGVR, testkit.Secret("shop", "db", nil, map[string]string{"password": dbPass, "username": "postgres"}))
	src.Set(configMapsGVR, testkit.Object("v1", "ConfigMap", "shop", "app", map[string]any{"data": map[string]any{
		"dsn": "postgres://app:" + dbPass + "@db:5432/app", "user": "postgres"}}))
	hr := fluxObject("helm.toolkit.fluxcd.io/v2", "HelmRelease", "shop", "podinfo", false, "True")
	hr.Object["spec"].(map[string]any)["values"] = map[string]any{"password": hrValuePass, "replicas": int64(2)}
	src.Set(hrGVR, hr)
	for _, d := range []struct {
		gvr  schema.GroupVersionResource
		kind string
	}{{gitReposGVR, "GitRepository"}, {providersGVR, "Provider"}} {
		src.Set(fluxCRDsGVR, testkit.CRD(d.gvr.Group, d.gvr.Resource, d.kind, true, testkit.CRDVersion{Name: d.gvr.Version, Served: true, Storage: true}))
	}
	src.Set(gitReposGVR, testkit.Object("source.toolkit.fluxcd.io/v1", "GitRepository", "flux-system", "apps", map[string]any{
		"spec": map[string]any{"url": "https://flux:" + gitToken + "@github.com/acme/apps", "interval": "1m"}}))
	src.Set(providersGVR, testkit.Object("notification.toolkit.fluxcd.io/v1beta3", "Provider", "flux-system", "slack", map[string]any{
		"spec": map[string]any{"type": "slack", "address": "https://hooks.slack.com/services/" + hookToken}}))
	h := &harness{src: src, writer: &fakeWriter{}}
	h.s = engine.New(engine.Options{Connector: agentViewConnector{src: src}, SnapshotInterval: 5 * time.Millisecond})
	if err := commands.RegisterAll(h.s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.s.Close)
	if err := h.s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		for _, r := range []model.ObjectRef{hrRef(), gitRef(), providerRef()} {
			if _, err := h.s.Lookup(r); err != nil {
				return false
			}
		}
		return true
	}) {
		t.Fatal("the Flux objects never reached the cache")
	}
	return h
}

func appCMRef() model.ObjectRef {
	return model.ObjectRef{Kind: "configmaps", Namespace: "shop", Name: "app"}
}
func gitRef() model.ObjectRef {
	return model.ObjectRef{Kind: "gitrepositories.source.toolkit.fluxcd.io", Namespace: "flux-system", Name: "apps"}
}
func providerRef() model.ObjectRef {
	return model.ObjectRef{Kind: "providers.notification.toolkit.fluxcd.io", Namespace: "flux-system", Name: "slack"}
}

// objectText is object.get's result as JSON text.
func objectText(t *testing.T, h *harness, origin engine.Origin, ref model.ObjectRef) string {
	t.Helper()
	var out json.RawMessage
	h.must(t, origin, "object.get", map[string]any{"ref": ref}, &out)
	return string(out)
}

// TestAgentViewReplacesCachedSecretValues (H-82, owner 2026-10-14): an agent reads the cached
// Secrets' values as «secret <ns>/<name>/<key>» in an object's YAML and drawer, an editor's text and
// diff and a logs tab's lines; the window reads them as they are. The window's read starts no
// Secrets watch; an agent's first read takes the Secrets and is filtered against them already (the
// owner's option b, 2026-10-14). An agent's text that carries a placeholder back is refused.
func TestAgentViewReplacesCachedSecretValues(t *testing.T) {
	h := newAgentViewHarness(t)
	const placeholder = "«secret shop/db/password»"
	secretScope := cache.Scope{GVR: testkit.SecretsGVR}

	// The window's read: the value, and no Secrets watch for it.
	if got := objectText(t, h, engine.OriginGUI, appCMRef()); !strings.Contains(got, dbPass) || strings.Contains(got, placeholder) {
		t.Fatalf("the window's read before any Secret is cached: %s", got)
	}
	if h.src.Watching(secretScope) {
		t.Fatal("the window's read started a Secrets watch")
	}

	// An agent's first read takes the Secrets, waits for them and reads the placeholder; the
	// Secrets stay watched (warm) after it.
	agentCM := objectText(t, h, engine.OriginExternalAgent, appCMRef())
	if !strings.Contains(agentCM, placeholder) {
		t.Fatalf("the agent's first read was not filtered: %s", agentCM)
	}
	// An informer opens its watch after its list, which the read waited for: the watch may come a
	// moment after the read (testkit.Settled).
	waitUntil(t, "a Secrets watch after the agent's read", func() bool { return h.src.Watching(secretScope) })
	if strings.Contains(agentCM, dbPass) {
		t.Errorf("object.get gives an agent the value: %s", agentCM)
	}
	if !strings.Contains(agentCM, "user: postgres") {
		t.Errorf("a name key's value was replaced: %s", agentCM)
	}
	if gui := objectText(t, h, engine.OriginGUI, appCMRef()); !strings.Contains(gui, dbPass) || strings.Contains(gui, placeholder) {
		t.Errorf("the window's read: %s", gui)
	}

	// An editor's text and diff.
	var edit commands.EditResult
	h.must(t, engine.OriginExternalAgent, "object.edit", map[string]any{"ref": appCMRef()}, &edit)
	if strings.Contains(edit.Text, dbPass) || !strings.Contains(edit.Text, placeholder) {
		t.Fatalf("object.edit: %s", edit.Text)
	}
	var st engine.EditorState
	h.must(t, engine.OriginBuiltInAgent, "editor.get", map[string]any{"id": edit.Tab.ID}, &st)
	if strings.Contains(st.Text, dbPass) || !strings.Contains(st.Text, placeholder) {
		t.Errorf("editor.get: %s", st.Text)
	}
	h.must(t, engine.OriginGUI, "editor.get", map[string]any{"id": edit.Tab.ID}, &st)
	if !strings.Contains(st.Text, dbPass) {
		t.Errorf("the window's editor.get: %s", st.Text)
	}
	err := h.run(t, engine.OriginExternalAgent, "editor.setText", map[string]any{"id": edit.Tab.ID, "yaml": edit.Text + "# more\n"}, nil)
	if !errors.Is(err, engine.ErrAgentMarkerWrite) {
		t.Fatalf("an agent's text with a placeholder: %v", err)
	}
	h.must(t, engine.OriginGUI, "editor.setText", map[string]any{"id": edit.Tab.ID, "yaml": strings.Replace(st.Text, "user: postgres", "user: app", 1)}, nil)
	var diff engine.EditDiff
	h.must(t, engine.OriginExternalAgent, "editor.diff", map[string]any{"id": edit.Tab.ID}, &diff)
	raw, _ := json.Marshal(diff)
	if !diff.Changed || strings.Contains(string(raw), dbPass) || !strings.Contains(string(raw), placeholder) {
		t.Errorf("editor.diff: %s", raw)
	}

	// A logs tab's lines.
	w := &fakeWindow{}
	h.s.SetDockOutputReader(w)
	pod := model.ObjectRef{Context: "test", Kind: "pods", Namespace: "shop", Name: "web-0"}
	id := openDockTab(t, h, "pod.logs", map[string]any{"ref": pod})
	w.show(id, "starting", "connecting to postgres://app:"+dbPass+"@db:5432/app", "ready")
	out, err := readDock(t, h, engine.OriginExternalAgent, map[string]any{"id": id})
	if err != nil || len(out.Lines) != 3 || out.Lines[1] != "connecting to postgres://app:"+placeholder+"@db:5432/app" || out.Lines[0] != "starting" {
		t.Fatalf("dock.read for an agent: %q %v", out.Lines, err)
	}
	if out, _ := readDock(t, h, engine.OriginGUI, map[string]any{"id": id}); !strings.Contains(out.Lines[1], dbPass) {
		t.Errorf("dock.read for the window: %q", out.Lines)
	}
}

// TestAgentViewWithholdsFluxCredentials (H-63, owner 2026-10-14): a Helm release's inline values,
// a Provider's address and the user information in a source's URL are withheld from the YAML an
// agent reads (object.get, object.edit, editor.get, editor.diff); the window reads them, and an
// agent's text that carries the marker back is refused.
func TestAgentViewWithholdsFluxCredentials(t *testing.T) {
	h := newAgentViewHarness(t)
	for _, c := range []struct {
		ref    model.ObjectRef
		secret string
		want   string
	}{
		{hrRef(), hrValuePass, "values: '" + model.Withheld + "'"},
		{providerRef(), hookToken, "address: '" + model.Withheld + "'"},
		{gitRef(), gitToken, "url: https://<withheld>@github.com/acme/apps"},
	} {
		var m engine.DetailModel
		h.must(t, engine.OriginExternalAgent, "object.get", map[string]any{"ref": c.ref}, &m)
		raw, _ := json.Marshal(m)
		if strings.Contains(string(raw), c.secret) || !strings.Contains(m.YAML, c.want) {
			t.Errorf("%s for an agent: %s", c.ref.Kind, m.YAML)
		}
		h.must(t, engine.OriginGUI, "object.get", map[string]any{"ref": c.ref}, &m)
		if !strings.Contains(m.YAML, c.secret) {
			t.Errorf("%s for the window: %s", c.ref.Kind, m.YAML)
		}
	}

	var edit commands.EditResult
	h.must(t, engine.OriginExternalAgent, "object.edit", map[string]any{"ref": hrRef()}, &edit)
	if strings.Contains(edit.Text, hrValuePass) || !strings.Contains(edit.Text, model.Withheld) {
		t.Fatalf("object.edit: %s", edit.Text)
	}
	var st engine.EditorState
	h.must(t, engine.OriginGUI, "editor.get", map[string]any{"id": edit.Tab.ID}, &st)
	if !strings.Contains(st.Text, hrValuePass) {
		t.Fatalf("the window's editor.get: %s", st.Text)
	}
	h.must(t, engine.OriginGUI, "editor.setText", map[string]any{"id": edit.Tab.ID, "yaml": strings.Replace(st.Text, hrValuePass, "changed-by-user-1", 1)}, nil)
	var diff engine.EditDiff
	h.must(t, engine.OriginExternalAgent, "editor.diff", map[string]any{"id": edit.Tab.ID}, &diff)
	raw, _ := json.Marshal(diff)
	if !diff.Changed || strings.Contains(string(raw), hrValuePass) || strings.Contains(string(raw), "changed-by-user-1") {
		t.Errorf("editor.diff for an agent: %s", raw)
	}
	h.must(t, engine.OriginGUI, "editor.diff", map[string]any{"id": edit.Tab.ID}, &diff)
	raw, _ = json.Marshal(diff)
	if !strings.Contains(string(raw), "changed-by-user-1") {
		t.Errorf("editor.diff for the window: %s", raw)
	}
	err := h.run(t, engine.OriginExternalAgent, "editor.setText", map[string]any{"id": edit.Tab.ID, "yaml": edit.Text}, nil)
	if !errors.Is(err, engine.ErrAgentMarkerWrite) {
		t.Errorf("an agent's text with the marker: %v", err)
	}
}

// refusingSecretsConnector is agentViewConnector over a source that refuses to list Secrets, as
// the API server refuses a user without the permission.
type refusingSecretsConnector struct{ src cache.Source }

func (c refusingSecretsConnector) Contexts() ([]engine.ContextInfo, error) {
	return []engine.ContextInfo{{Name: "test", Current: true}}, nil
}

func (c refusingSecretsConnector) Connect(context.Context, string) (*engine.Backend, error) {
	ch := cache.New(c.src, cache.Options{WarmPeriod: time.Minute})
	secrets, _ := kinds.ByKey("secrets")
	cms, _ := kinds.ByKey("configmaps")
	return &engine.Backend{Context: "test", Namespace: "default", Cache: ch, Close: ch.Close,
		Profiles: append([]*model.KindProfile{secrets, cms}, testProfiles()...), Conn: &cluster.Conn{}, Actions: &fakeWriter{}}, nil
}

// TestAgentReadWithoutSecretsAccess (H-82, option b): for a user who may not list Secrets, an
// agent's read does not wait for them: the refusal says there is nothing to replace, and the read
// answers at once, the value as printed (H-82 names this case).
func TestAgentReadWithoutSecretsAccess(t *testing.T) {
	mem := testkit.NewMemorySource()
	mem.Set(configMapsGVR, testkit.Object("v1", "ConfigMap", "shop", "app", map[string]any{"data": map[string]any{"dsn": "postgres://app:" + dbPass + "@db/app"}}))
	src := testkit.RefusingSource{Inner: mem, Refuse: map[schema.GroupVersionResource]error{
		testkit.SecretsGVR: apierrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, "", errors.New("the user may not list secrets")),
	}}
	h := &harness{writer: &fakeWriter{}}
	h.s = engine.New(engine.Options{Connector: refusingSecretsConnector{src: src}, SnapshotInterval: 5 * time.Millisecond})
	if err := commands.RegisterAll(h.s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.s.Close)
	if err := h.s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	got := objectText(t, h, engine.OriginExternalAgent, appCMRef())
	if took := time.Since(start); took > 5*time.Second || !strings.Contains(got, dbPass) {
		t.Errorf("after %v: %s", took, got)
	}
}
