package commands_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The create dialogs' commands (G7) against the fake writer, which acts as an API server on the
// harness's source (objects_fake_test.go): dry run then create, the confirmation agents need,
// the access reviews' refusal, and the checks the commands make before anything is sent.

var (
	serviceAccountsGVR     = schema.GroupVersionResource{Version: "v1", Resource: "serviceaccounts"}
	resourceQuotasGVR      = schema.GroupVersionResource{Version: "v1", Resource: "resourcequotas"}
	rolesGVR               = schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"}
	clusterRolesGVR        = schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}
	roleBindingsGVR        = schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings"}
	clusterRoleBindingsGVR = schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterrolebindings"}
)

func init() {
	// The create commands are part of the registry the GUI and MCP rely on (TestRegistryComplete),
	// listed here rather than in the shared map so that parallel work does not collide in it.
	for id, d := range map[string]engine.Danger{
		"namespace.create": engine.DangerMutating, "serviceAccount.create": engine.DangerMutating,
		"role.create": engine.DangerMutating, "clusterRole.create": engine.DangerMutating,
		"roleBinding.create":        engine.DangerMutating, // destructive for cluster-admin and wildcard roles (Escalate)
		"clusterRoleBinding.create": engine.DangerDestructive,
		"resourceQuota.create":      engine.DangerMutating,
	} {
		wantCommands[id] = d
	}
	// The fake discovery serves the kinds the dialogs create (the cluster roles are served already).
	for _, m := range []struct {
		gvr        schema.GroupVersionResource
		kind       string
		namespaced bool
	}{
		{serviceAccountsGVR, "ServiceAccount", true}, {resourceQuotasGVR, "ResourceQuota", true},
		{rolesGVR, "Role", true}, {roleBindingsGVR, "RoleBinding", true}, {clusterRoleBindingsGVR, "ClusterRoleBinding", false},
	} {
		fakeKinds = append(fakeKinds, cluster.Mapping{GVR: m.gvr, GVK: m.gvr.GroupVersion().WithKind(m.kind), Namespaced: m.namespaced})
	}
}

// createConnector is the harness's connector with Cogpit's real profiles of the kinds the dialogs
// create, so that kind keys and cached roles are as in the app.
type createConnector struct{ *fakeConnector }

func (c createConnector) Connect(ctx context.Context, name string) (*engine.Backend, error) {
	b, err := c.fakeConnector.Connect(ctx, name)
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"serviceaccounts", "resourcequotas", "roles", "clusterroles", "rolebindings", "clusterrolebindings"} {
		if p, ok := kinds.ByKey(key); ok {
			b.Profiles = append(b.Profiles, p)
		}
	}
	return b, nil
}

// createHarness is a harness whose writer acts on its source, connected to "test".
func createHarness(t *testing.T, opts engine.Options) (*harness, *objectServer) {
	t.Helper()
	src := seed()
	h := &harness{src: src, writer: &fakeWriter{}}
	opts.Connector = createConnector{&fakeConnector{src: src, writer: h.writer}}
	opts.SnapshotInterval = 10 * time.Millisecond
	h.s = engine.New(opts)
	if err := commands.RegisterAll(h.s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.s.Close)
	if err := h.s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	return h, serveObjects(h)
}

// equalJSON reports whether v marshals to the same JSON as want (map keys sorted).
func equalJSON(t *testing.T, v any, want string) bool {
	t.Helper()
	got, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var w any
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatal(err)
	}
	norm, _ := json.Marshal(w)
	return string(got) == string(norm)
}

// created returns the objects the fake server was asked to create for real, by "kind namespace/name".
func (srv *objectServer) created() map[string]*unstructured.Unstructured {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	out := map[string]*unstructured.Unstructured{}
	for i, o := range srv.creates {
		if i%2 == 1 { // every create is a dry run first, then the create
			out[o.GetKind()+" "+o.GetNamespace()+"/"+o.GetName()] = o
		}
	}
	return out
}

// TestCreateDialogsDryRunThenCreate creates one object of each kind from the GUI: each is checked
// by a server-side dry run, then created, in the namespace given, with the fields the parameters
// name in Kubernetes' form.
func TestCreateDialogsDryRunThenCreate(t *testing.T) {
	var asked []engine.ConfirmRequest
	h, srv := createHarness(t, engine.Options{Confirmer: engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
		asked = append(asked, req)
		return true, nil
	})})
	gui := engine.OriginGUI
	steps := []struct {
		id     string
		params map[string]any
		ref    model.ObjectRef
	}{
		{"namespace.create", map[string]any{"name": "team-x"}, model.ObjectRef{Kind: "namespaces", Name: "team-x"}},
		{"serviceAccount.create", map[string]any{"name": "ci", "namespace": "shop"}, model.ObjectRef{Kind: "serviceaccounts", Namespace: "shop", Name: "ci"}},
		{"role.create", map[string]any{"name": "reader", "namespace": "shop", "rules": []any{
			map[string]any{"apiGroups": []string{""}, "resources": []string{"pods", " pods/log "}, "verbs": []string{"get", "list", "list"}},
			map[string]any{"apiGroups": []string{"apps"}, "resources": []string{"deployments"}, "resourceNames": []string{"web"}, "verbs": []string{"get"}},
		}}, model.ObjectRef{Kind: "roles", Namespace: "shop", Name: "reader"}},
		{"clusterRole.create", map[string]any{"name": "system:health-reader", "rules": []any{
			map[string]any{"nonResourceURLs": []string{"/healthz", "/readyz"}, "verbs": []string{"get"}},
		}}, model.ObjectRef{Kind: "clusterroles", Name: "system:health-reader"}},
		{"roleBinding.create", map[string]any{"name": "ci-reads", "namespace": "shop", "roleRef": map[string]any{"kind": "role", "name": "reader"},
			"subjects": []any{map[string]any{"kind": "ServiceAccount", "name": "ci"}, map[string]any{"kind": "user", "name": "alice@example.com"}}},
			model.ObjectRef{Kind: "rolebindings", Namespace: "shop", Name: "ci-reads"}},
		{"clusterRoleBinding.create", map[string]any{"name": "health-readers", "clusterRole": "system:health-reader",
			"subjects": []any{map[string]any{"kind": "Group", "name": "oncall"}, map[string]any{"kind": "ServiceAccount", "name": "ci", "namespace": "shop"}}},
			model.ObjectRef{Kind: "clusterrolebindings", Name: "health-readers"}},
		{"resourceQuota.create", map[string]any{"name": "small", "namespace": "shop", "hard": map[string]string{"pods": " 10 ", "requests.cpu": "500m", "limits.memory": "4Gi"}},
			model.ObjectRef{Kind: "resourcequotas", Namespace: "shop", Name: "small"}},
	}
	var want []string
	for _, st := range steps {
		var item engine.CreateItem
		h.must(t, gui, st.id, st.params, &item)
		st.ref.Context = "test"
		if !item.Created || item.Ref != st.ref {
			t.Errorf("%s: %+v, want %v created", st.id, item, st.ref)
		}
		key := st.ref.Namespace + "/" + st.ref.Name
		want = append(want, "create(dry) "+key, "create "+key)
	}
	if got := h.writer.Calls(); !slices.Equal(got, want) {
		t.Errorf("writes:\n got %v\nwant %v", got, want)
	}
	// The GUI's own mutating creates run without a question; the cluster-wide grant is confirmed,
	// naming what it grants to whom, in the owner's words, and not saying it cannot be undone (A101).
	if len(asked) != 1 || asked[0].Command != "clusterRoleBinding.create" || asked[0].Danger != engine.DangerDestructive ||
		!asked[0].Undoable ||
		!slices.Equal(asked[0].Effects, []engine.Effect{{Name: "Grants", Value: "ClusterRole → system:health-reader", Tone: engine.EffectNames},
			{Name: "To", Value: "Group → oncall\nServiceAccount → shop/ci", Tone: engine.EffectNames},
			{Name: "Takes effect", Value: "immediately, cluster-wide", Tone: engine.EffectWarning}}) {
		t.Errorf("confirmations: %+v", asked)
	}

	objs := srv.created()
	if sa := objs["ServiceAccount shop/ci"]; sa == nil || sa.GetAPIVersion() != "v1" {
		t.Errorf("service account: %v", sa)
	}
	role := objs["Role shop/reader"]
	rules, _, _ := unstructured.NestedSlice(role.Object, "rules")
	if len(rules) != 2 || !equalJSON(t, rules[0], `{"apiGroups":[""],"resources":["pods","pods/log"],"verbs":["get","list"]}`) ||
		!equalJSON(t, rules[1], `{"apiGroups":["apps"],"resourceNames":["web"],"resources":["deployments"],"verbs":["get"]}`) {
		t.Errorf("role rules: %v", rules)
	}
	cr := objs["ClusterRole /system:health-reader"]
	if rules, _, _ := unstructured.NestedSlice(cr.Object, "rules"); len(rules) != 1 || !equalJSON(t, rules[0], `{"nonResourceURLs":["/healthz","/readyz"],"verbs":["get"]}`) {
		t.Errorf("cluster role rules: %v", rules)
	}
	rb := objs["RoleBinding shop/ci-reads"]
	if ref, _, _ := unstructured.NestedMap(rb.Object, "roleRef"); !equalJSON(t, ref, `{"apiGroup":"rbac.authorization.k8s.io","kind":"Role","name":"reader"}`) {
		t.Errorf("role ref: %v", ref)
	}
	if subs, _, _ := unstructured.NestedSlice(rb.Object, "subjects"); !equalJSON(t, subs,
		`[{"kind":"ServiceAccount","name":"ci","namespace":"shop"},{"apiGroup":"rbac.authorization.k8s.io","kind":"User","name":"alice@example.com"}]`) {
		t.Errorf("subjects: %v", subs)
	}
	crb := objs["ClusterRoleBinding /health-readers"]
	if ref, _, _ := unstructured.NestedMap(crb.Object, "roleRef"); !equalJSON(t, ref, `{"apiGroup":"rbac.authorization.k8s.io","kind":"ClusterRole","name":"system:health-reader"}`) {
		t.Errorf("cluster role ref: %v", ref)
	}
	q := objs["ResourceQuota shop/small"]
	if hard, _, _ := unstructured.NestedMap(q.Object, "spec", "hard"); !equalJSON(t, hard, `{"limits.memory":"4Gi","pods":"10","requests.cpu":"500m"}`) {
		t.Errorf("quota: %v", hard)
	}
}

// TestCreateDialogsNamespaceDefault: without a namespace an object lands in the namespace filter's
// single namespace, else in the context's default namespace, as object.create places it; the
// dialog's prefill says so.
func TestCreateDialogsNamespaceDefault(t *testing.T) {
	h, _ := createHarness(t, engine.Options{})
	gui := engine.OriginGUI
	var item engine.CreateItem
	h.must(t, gui, "serviceAccount.create", map[string]any{"name": "a"}, &item)
	if item.Ref.Namespace != "default" {
		t.Errorf("without a filter: %v", item.Ref)
	}
	h.must(t, gui, "ui.setNamespace", map[string]any{"namespaces": []string{"infra"}}, nil)
	h.must(t, gui, "resourceQuota.create", map[string]any{"name": "q", "hard": map[string]string{"pods": "1"}}, &item)
	if item.Ref.Namespace != "infra" {
		t.Errorf("with the filter on infra: %v", item.Ref)
	}
	cmd, _ := h.s.Registry().Get("roleBinding.create")
	pre, err := cmd.Prefill(h.s, []byte(`{"name":"","roleRef":{"kind":"","name":""},"subjects":[]}`))
	if err != nil || !strings.Contains(string(pre), `"namespace":"infra"`) {
		t.Errorf("prefill %s, %v", pre, err)
	}
}

// TestCreateDialogsAgentsNeedAcceptance: an agent's create waits for the user (D-2), who reads the
// object, its namespace, the context and what it grants; a declined one writes nothing. With agent
// writes allowed for the context the mutating creates run, but a grant of everything still asks.
func TestCreateDialogsAgentsNeedAcceptance(t *testing.T) {
	audit := &syncBuffer{}
	var asked []engine.ConfirmRequest
	answer := false
	h, _ := createHarness(t, engine.Options{Audit: audit, Confirmer: engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
		asked = append(asked, req)
		return answer, nil
	})})
	agent := engine.OriginExternalAgent
	h.must(t, engine.OriginGUI, "ui.openView", map[string]any{"view": "pods"}, nil)
	rb := map[string]any{"name": "ci-admin", "namespace": "shop", "roleRef": map[string]any{"kind": "ClusterRole", "name": "admin"},
		"subjects": []any{map[string]any{"kind": "ServiceAccount", "name": "ci"}}}
	if err := h.run(t, agent, "roleBinding.create", rb, nil); !errors.Is(err, engine.ErrDeclined) {
		t.Fatalf("declined: %v", err)
	}
	if len(h.writer.Calls()) != 0 {
		t.Errorf("a declined create wrote %v", h.writer.Calls())
	}
	req := asked[0]
	if req.Danger != engine.DangerMutating || req.Context != "test" || !slices.Equal(req.Namespaces, []string{"shop"}) ||
		len(req.Objects) != 1 || req.Objects[0].Kind != "rolebindings" || req.Objects[0].Name != "ci-admin" ||
		req.Detail != "Grants: ClusterRole → admin. To: ServiceAccount → shop/ci. Takes effect: immediately, in namespace shop." {
		t.Errorf("confirmation %+v", req)
	}
	if !strings.Contains(req.Message(), "An agent asks to run: Create Role Binding Role Binding ci-admin in namespace shop (context test)? Grants: ClusterRole → admin.") {
		t.Errorf("message %q", req.Message())
	}
	answer = true
	if err := h.run(t, agent, "role.create", map[string]any{"name": "r", "namespace": "shop",
		"rules": []any{map[string]any{"apiGroups": []string{"", "apps"}, "resources": []string{"pods", "deployments"}, "verbs": []string{"get", "list"}}}}, nil); err != nil {
		t.Fatal(err)
	}
	if d := asked[len(asked)-1].Detail; d != "Rules: get, list on pods, deployments (apps)." {
		t.Errorf("role detail %q", d)
	}
	lines := audit.Lines()
	if n := len(lines); n < 2 || lines[n-1].Command != "role.create" || !lines[n-1].OK || lines[n-2].Phase != "requested" {
		t.Errorf("audit %+v", lines)
	}

	// Agent writes allowed for the context: the mutating ones run unasked, but every RBAC object an
	// agent asks for waits for the user (H-27; roles and cluster roles too since A1), whatever its
	// rules or role, and a grant of everything asks as a destructive command.
	h.s.SetAgentWrites("test", true)
	n := len(asked)
	h.must(t, agent, "namespace.create", map[string]any{"name": "agent-made"}, nil)
	if len(asked) != n {
		t.Errorf("asked %+v with agent writes allowed", asked[n:])
	}
	h.must(t, agent, "role.create", map[string]any{"name": "r2", "namespace": "shop"}, nil)
	h.must(t, agent, "clusterRole.create", map[string]any{"name": "cr2"}, nil)
	if len(asked) != n+2 || asked[n].Command != "role.create" || asked[n+1].Command != "clusterRole.create" ||
		!asked[n].AgentsAsk || !asked[n+1].AgentsAsk || asked[n].Danger != engine.DangerMutating {
		t.Fatalf("an agent's role and cluster role with agent writes allowed: asked %+v", asked[n:])
	}
	n = len(asked)
	h.must(t, agent, "roleBinding.create", rb, nil)
	if len(asked) != n+1 || asked[n].Command != "roleBinding.create" || asked[n].Danger != engine.DangerMutating ||
		!asked[n].AgentsAsk || asked[n].Detail == "" {
		t.Fatalf("an agent's role binding to admin with agent writes allowed: asked %+v", asked[n:])
	}
	admin := map[string]any{"name": "ci-root", "namespace": "shop", "roleRef": map[string]any{"kind": "ClusterRole", "name": "cluster-admin"},
		"subjects": []any{map[string]any{"kind": "ServiceAccount", "name": "ci"}}}
	h.must(t, agent, "roleBinding.create", admin, nil)
	crb := map[string]any{"name": "ci-view", "clusterRole": "view", "subjects": []any{map[string]any{"kind": "ServiceAccount", "name": "ci", "namespace": "shop"}}}
	h.must(t, agent, "clusterRoleBinding.create", crb, nil)
	if len(asked) != n+3 || asked[n+1].Danger != engine.DangerDestructive || asked[n+2].Command != "clusterRoleBinding.create" {
		t.Errorf("grants of everything: asked %+v", asked[n:])
	}
	// The GUI's own binding to admin runs without a dialog, as before; its binding to
	// cluster-admin asks too.
	gui := map[string]any{"name": "gui-admin", "namespace": "shop", "roleRef": map[string]any{"kind": "ClusterRole", "name": "admin"},
		"subjects": []any{map[string]any{"kind": "ServiceAccount", "name": "ci"}}}
	h.must(t, engine.OriginGUI, "roleBinding.create", gui, nil)
	if len(asked) != n+3 {
		t.Errorf("the GUI's own binding to admin asked: %+v", asked[n+3:])
	}
	admin["name"] = "gui-root"
	h.must(t, engine.OriginGUI, "roleBinding.create", admin, nil)
	if len(asked) != n+4 || asked[n+3].Origin != engine.OriginGUI || asked[n+3].AgentsAsk {
		t.Errorf("the GUI's binding to cluster-admin: asked %+v", asked[n:])
	}
	// The MCP tool says so.
	if c, _ := h.s.Registry().Get("roleBinding.create"); !c.Escalates || !c.AgentsAsk {
		t.Errorf("roleBinding.create: escalates %v, agentsAsk %v", c.Escalates, c.AgentsAsk)
	}
}

// TestRoleBindingToAWildcardRoleEscalates: a role Cogpit's cache holds whose rules allow every verb
// on every resource is treated like cluster-admin; one it does not hold, or a narrower one, is not.
func TestRoleBindingToAWildcardRoleEscalates(t *testing.T) {
	var asked []engine.ConfirmRequest
	h, _ := createHarness(t, engine.Options{Confirmer: engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
		asked = append(asked, req)
		return true, nil
	})})
	h.src.Set(clusterRolesGVR, testkit.Object("rbac.authorization.k8s.io/v1", "ClusterRole", "", "everything", map[string]any{
		"rules": []any{map[string]any{"apiGroups": []any{"*"}, "resources": []any{"*"}, "verbs": []any{"*"}}}}))
	h.src.Set(clusterRolesGVR, testkit.Object("rbac.authorization.k8s.io/v1", "ClusterRole", "", "pods-everything", map[string]any{
		"rules": []any{map[string]any{"apiGroups": []any{""}, "resources": []any{"pods"}, "verbs": []any{"*"}}}}))
	bind := func(name, role string) {
		t.Helper()
		h.must(t, engine.OriginGUI, "roleBinding.create", map[string]any{"name": name, "namespace": "shop",
			"roleRef": map[string]any{"kind": "ClusterRole", "name": role}, "subjects": []any{map[string]any{"kind": "Group", "name": "devs"}}}, nil)
	}
	bind("before-cached", "everything") // the cluster roles are not cached yet: no watch is started for this
	if len(asked) != 0 {
		t.Errorf("asked before the cache held the role: %+v", asked)
	}
	if _, err := h.s.Objects(context.Background(), "clusterroles", nil); err != nil {
		t.Fatal(err)
	}
	bind("narrow", "pods-everything")
	bind("wide", "everything")
	if len(asked) != 1 || asked[0].Objects[0].Name != "wide" || asked[0].Danger != engine.DangerDestructive {
		t.Errorf("asked %+v", asked)
	}
}

// TestCreateDialogsFollowAccessReviews: a create the access reviews deny is greyed "not permitted"
// in the namespace it would land in, for every origin; elsewhere it is offered.
func TestCreateDialogsFollowAccessReviews(t *testing.T) {
	h, _ := createHarness(t, engine.Options{})
	h.s.Backend().Access = denyAccess{"create rolebindings shop": true, "create namespaces ": true}
	for _, tt := range []struct {
		id     string
		params map[string]any
		reason string
	}{
		{"roleBinding.create", map[string]any{"name": "b", "namespace": "shop", "roleRef": map[string]any{"kind": "Role", "name": "r"},
			"subjects": []any{map[string]any{"kind": "User", "name": "u"}}}, "not permitted: you may not create rolebindings.rbac.authorization.k8s.io in shop"},
		{"namespace.create", map[string]any{"name": "n"}, "not permitted: you may not create namespaces"},
	} {
		for _, origin := range []engine.Origin{engine.OriginGUI, engine.OriginExternalAgent} {
			err := h.run(t, origin, tt.id, tt.params, nil)
			var dis *engine.DisabledError
			if !errors.As(err, &dis) || dis.Reason != tt.reason {
				t.Errorf("%s from %s: %v, want disabled: %s", tt.id, origin, err, tt.reason)
			}
		}
	}
	cmd, _ := h.s.Registry().Get("roleBinding.create")
	if err := cmd.Enabled(h.s, []byte(`{"name":"b","namespace":"infra","roleRef":{"kind":"Role","name":"r"},"subjects":[]}`)); err != nil {
		t.Errorf("in infra: %v", err)
	}
	if len(h.writer.Calls()) != 0 {
		t.Errorf("writes %v", h.writer.Calls())
	}
}

// TestCreateDialogsCheckTheirParameters: what the API server would refuse is refused before
// anything is sent, and an agent's bad request before the user is asked.
func TestCreateDialogsCheckTheirParameters(t *testing.T) {
	asked := 0
	h, _ := createHarness(t, engine.Options{Confirmer: engine.ConfirmFunc(func(context.Context, engine.ConfirmRequest) (bool, error) {
		asked++
		return true, nil
	})})
	user := []any{map[string]any{"kind": "User", "name": "u"}}
	role := map[string]any{"kind": "Role", "name": "r"}
	for _, tt := range []struct {
		id     string
		params map[string]any
		want   string
	}{
		{"namespace.create", map[string]any{"name": ""}, "the namespace needs a name"},
		{"namespace.create", map[string]any{"name": "Team_X"}, `namespace name "Team_X" is not valid: a lowercase RFC 1123 label`},
		{"namespace.create", map[string]any{"name": strings.Repeat("a", 64)}, "must be no more than 63 characters"},
		{"namespace.create", map[string]any{"name": "x", "labels": map[string]any{"a": "b"}}, `unexpected additional properties ["labels"]`},
		{"serviceAccount.create", map[string]any{"name": "ci", "namespace": "Shop"}, `namespace name "Shop" is not valid`},
		{"role.create", map[string]any{"name": "a/b", "namespace": "shop"}, `role name "a/b" is not valid: may not contain '/'`},
		{"role.create", map[string]any{"name": "r", "rules": []any{map[string]any{"apiGroups": []string{""}, "resources": []string{"pods"}, "verbs": []string{}}}}, "rule 1 needs at least one verb"},
		{"role.create", map[string]any{"name": "r", "rules": []any{map[string]any{"resources": []string{"pods"}, "verbs": []string{"get"}}}}, `rule 1 needs API groups: "" for the core group`},
		{"role.create", map[string]any{"name": "r", "rules": []any{map[string]any{"apiGroups": []string{""}, "verbs": []string{"get"}}}}, "rule 1 needs resources"},
		{"role.create", map[string]any{"name": "r", "rules": []any{map[string]any{"nonResourceURLs": []string{"/healthz"}, "verbs": []string{"get"}}}}, "rule 1: only a cluster role has non-resource URLs"},
		{"role.create", map[string]any{"name": "r", "rules": []any{map[string]any{"apiGroups": []string{""}, "resources": []string{"pods"}, "verbs": []string{"get", ""}}}}, "rule 1: verbs: an empty entry"},
		{"role.create", map[string]any{"name": "r", "rules": []any{map[string]any{"apiGroups": []string{""}, "resources": []string{"config maps"}, "verbs": []string{"get"}}}}, `rule 1: resources: "config maps" has spaces in it`},
		{"clusterRole.create", map[string]any{"name": "c", "rules": []any{map[string]any{"nonResourceURLs": []string{"/x"}, "resources": []string{"pods"}, "verbs": []string{"get"}}}}, "either non-resource URLs or API groups and resources"},
		{"clusterRole.create", map[string]any{"name": "c", "rules": []any{map[string]any{"nonResourceURLs": []string{"healthz"}, "verbs": []string{"get"}}}}, `non-resource URL "healthz" is not a path`},
		{"roleBinding.create", map[string]any{"name": "b", "roleRef": role, "subjects": []any{}}, "a binding needs at least one subject"},
		{"roleBinding.create", map[string]any{"name": "b", "roleRef": map[string]any{"kind": "Policy", "name": "r"}, "subjects": user}, `role kind "Policy": want Role or ClusterRole`},
		{"roleBinding.create", map[string]any{"name": "b", "roleRef": map[string]any{"kind": "Role", "name": ""}, "subjects": user}, "the binding's role: the role needs a name"},
		{"roleBinding.create", map[string]any{"name": "b", "roleRef": role, "subjects": []any{map[string]any{"kind": "Robot", "name": "r2"}}}, `subject 1: kind "Robot": want User, Group or ServiceAccount`},
		{"roleBinding.create", map[string]any{"name": "b", "roleRef": role, "subjects": []any{map[string]any{"kind": "User", "name": " "}}}, "subject 1 needs a name"},
		{"roleBinding.create", map[string]any{"name": "b", "roleRef": role, "subjects": []any{map[string]any{"kind": "Group", "name": "g", "namespace": "shop"}}}, "subject 1: a Group has no namespace"},
		{"roleBinding.create", map[string]any{"name": "b", "roleRef": role, "subjects": []any{map[string]any{"kind": "ServiceAccount", "name": "CI"}}}, `subject 1: service account name "CI" is not valid`},
		{"clusterRoleBinding.create", map[string]any{"name": "b", "clusterRole": "view", "subjects": []any{map[string]any{"kind": "ServiceAccount", "name": "ci"}}}, "subject 1: a service account in a cluster role binding needs its namespace"},
		{"clusterRoleBinding.create", map[string]any{"name": "b", "clusterRole": "", "subjects": user}, "the binding's role: the clusterrole needs a name"},
		{"resourceQuota.create", map[string]any{"name": "q", "hard": map[string]string{}}, "a resource quota needs at least one hard limit"},
		{"resourceQuota.create", map[string]any{"name": "q", "hard": map[string]string{"pods": "lots"}}, `hard limit pods: "lots" is not a quantity`},
		{"resourceQuota.create", map[string]any{"name": "q", "hard": map[string]string{"requests cpu": "1"}}, `hard limit "requests cpu": a resource name`},
	} {
		for _, origin := range []engine.Origin{engine.OriginGUI, engine.OriginBuiltInAgent} {
			err := h.run(t, origin, tt.id, tt.params, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("%s %v from %s: %v, want %q", tt.id, tt.params, origin, err, tt.want)
			}
		}
	}
	if asked != 0 || len(h.writer.Calls()) != 0 {
		t.Errorf("asked %d times, wrote %v", asked, h.writer.Calls())
	}
}

// TestCreateDialogRefusedByTheDryRun: what only the API server knows (a resource name it does not
// take as a quota's) stops at the dry run; nothing is created and the error says why.
func TestCreateDialogRefusedByTheDryRun(t *testing.T) {
	h, srv := createHarness(t, engine.Options{})
	srv.mu.Lock()
	srv.failNext = apierrors.NewInvalid(schema.GroupKind{Kind: "ResourceQuota"}, "q", field.ErrorList{
		field.Invalid(field.NewPath("spec", "hard").Key("gadgets"), "gadgets", "must be a standard resource for quota")})
	srv.mu.Unlock()
	err := h.run(t, engine.OriginGUI, "resourceQuota.create", map[string]any{"name": "q", "namespace": "shop", "hard": map[string]string{"gadgets": "1"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "resourcequotas/shop/q was not created") || !strings.Contains(err.Error(), "must be a standard resource for quota") {
		t.Errorf("refused dry run: %v", err)
	}
	if got := h.writer.Calls(); !slices.Equal(got, []string{"create(dry) shop/q"}) {
		t.Errorf("writes %v", got)
	}
}

// TestCreateCommandsDescribeThemselves: kinds, verbs and schemas as agents read them.
func TestCreateCommandsDescribeThemselves(t *testing.T) {
	r := engine.NewRegistry()
	if err := commands.RegisterAll(r); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]struct {
		kind, group, resource string
		required              []string
	}{
		"namespace.create":          {"namespaces", "", "namespaces", []string{"name"}},
		"serviceAccount.create":     {"serviceaccounts", "", "serviceaccounts", []string{"name"}},
		"role.create":               {"roles", "rbac.authorization.k8s.io", "roles", []string{"name"}},
		"clusterRole.create":        {"clusterroles", "rbac.authorization.k8s.io", "clusterroles", []string{"name"}},
		"roleBinding.create":        {"rolebindings", "rbac.authorization.k8s.io", "rolebindings", []string{"name", "roleRef", "subjects"}},
		"clusterRoleBinding.create": {"clusterrolebindings", "rbac.authorization.k8s.io", "clusterrolebindings", []string{"clusterRole", "name", "subjects"}},
		"resourceQuota.create":      {"resourcequotas", "", "resourcequotas", []string{"hard", "name"}},
	} {
		c, ok := r.Get(id)
		if !ok {
			t.Errorf("no %s", id)
			continue
		}
		req := slices.Sorted(slices.Values(c.Schema.Required))
		if !slices.Equal(c.Kinds, []string{want.kind}) || len(c.Verbs) != 1 ||
			c.Verbs[0] != (engine.Verb{Verb: "create", Group: want.group, Resource: want.resource}) || !slices.Equal(req, want.required) || c.Order != 0 {
			t.Errorf("%s: kinds %v verbs %+v required %v order %d", id, c.Kinds, c.Verbs, req, c.Order)
		}
	}
}

// TestCreateShowsTheNewObject (A102): after the user's create of one object, its kind's view
// opens with the object selected and its drawer open (the window scrolls to the selection); an
// agent's create leaves the window where the user has it.
func TestCreateShowsTheNewObject(t *testing.T) {
	h, _ := createHarness(t, engine.Options{Confirmer: engine.ConfirmFunc(func(context.Context, engine.ConfirmRequest) (bool, error) {
		return true, nil
	})})
	h.must(t, engine.OriginGUI, "ui.openView", map[string]any{"view": "pods"}, nil)
	h.must(t, engine.OriginExternalAgent, "namespace.create", map[string]any{"name": "agent-made"}, nil)
	if u := h.s.UIState(); u.View != "pods" || u.Drawer != nil {
		t.Errorf("an agent's create moved the window: view %q, drawer %+v", u.View, u.Drawer)
	}
	h.must(t, engine.OriginGUI, "namespace.create", map[string]any{"name": "user-made"}, nil)
	u := h.s.UIState()
	if u.View != "namespaces" || u.Drawer == nil || u.Drawer.Name != "user-made" || len(u.Selected) != 0 {
		t.Errorf("after the user's create: view %q, drawer %+v, selected %+v", u.View, u.Drawer, u.Selected)
	}
}
