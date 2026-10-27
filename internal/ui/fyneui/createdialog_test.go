package fyneui

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// The create dialogs (G7) against the fake engine: the "+" of each kind's view opens its dialog,
// the dialog offers roles and service accounts from the cache (resources.list), sends the
// command's parameters, greys Create when the access reviews deny it, and stays open with the
// reason when the create fails. Every parameter set a dialog sends is checked against the real
// registry. PNGs: testdata/create_{namespace,rolebinding}_dialog_{dark,light}.png,
// create_{role,resourcequota}_dialog_dark.png.

// listingFake serves resources.list from the fake's tables for every kind but namespaces (which
// the fake itself serves from its namespace list), as the engine serves it from the cache.
type listingFake struct{ *fakeEngine }

func (l listingFake) Dispatch(ctx context.Context, inv engine.Invocation) (any, error) {
	var p createListParams
	if inv.ID != "resources.list" || json.Unmarshal(normalize(inv.Params), &p) != nil || p.Kind == kindNamespaces {
		return l.fakeEngine.Dispatch(ctx, inv)
	}
	l.mu.Lock()
	l.calls = append(l.calls, inv)
	snap := l.tables[p.Kind]
	l.mu.Unlock()
	rows := []map[string]any{}
	if snap != nil {
		for _, r := range snap.Rows {
			if len(p.Namespaces) == 0 || slices.Contains(p.Namespaces, r.Ref.Namespace) {
				rows = append(rows, map[string]any{"ref": r.Ref})
			}
		}
	}
	return map[string]any{"rows": rows}, nil
}

// newCreateShell is newTestShell on the listing fake.
func newCreateShell(t testing.TB, f *fakeEngine, mode ThemeMode) (*shell, fyne.Window, *mainQueue) {
	t.Helper()
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(mode))
	w := test.NewTempWindow(t, nil)
	w.SetPadded(false)
	q := &mainQueue{}
	sh := newShell(listingFake{f}, a, w, Options{Theme: mode}, q.do)
	sh.now = func() time.Time { return fixedNow }
	sh.kc.startDir = t.TempDir
	sh.confirmer.armDelay = 0
	f.SetConfirmer(sh.confirmer)
	w.SetContent(sh.root)
	w.Resize(fyne.NewSize(1440, 900))
	sh.start()
	t.Cleanup(func() {
		sh.stop()
		q.drain()
	})
	return sh, w, q
}

// createProbeParams takes every create dialog's parameters.
type createProbeParams struct {
	Name        string            `json:"name,omitempty"`
	Namespace   string            `json:"namespace,omitempty"`
	Rules       []map[string]any  `json:"rules,omitempty"`
	RoleRef     map[string]any    `json:"roleRef,omitempty"`
	ClusterRole string            `json:"clusterRole,omitempty"`
	Subjects    []map[string]any  `json:"subjects,omitempty"`
	Hard        map[string]string `json:"hard,omitempty"`
	Type        string            `json:"type,omitempty"`        // Create Secret's
	Data        []map[string]any  `json:"data,omitempty"`        // Create Secret's
	Labels      []map[string]any  `json:"labels,omitempty"`      // Create Secret's
	Annotations []map[string]any  `json:"annotations,omitempty"` // Create Secret's
}

// registerCreateProbes registers a probe of each create command, labelled as the real one, enabled
// unless the test disables it (fakeEngine.disabled).
func registerCreateProbes(t testing.TB, f *fakeEngine) {
	t.Helper()
	reg := engine.NewRegistry()
	if err := commands.RegisterAll(reg); err != nil {
		t.Fatal(err)
	}
	for _, spec := range createDialogs {
		id := spec.command
		c, ok := reg.Get(id)
		if !ok {
			t.Fatalf("the registry has no %s", id)
		}
		if err := engine.Register(f.reg, engine.Spec[createProbeParams]{ID: id, Label: c.Label, Danger: engine.DangerRead,
			Enabled: func(*engine.Session, createProbeParams) error { return f.reason(id) },
			Run:     func(context.Context, *engine.Session, createProbeParams) (any, error) { return nil, nil }}); err != nil {
			t.Fatal(err)
		}
	}
}

// createKinds are the kinds with a create dialog as kinds.list reports them (Namespaces are in the
// base fixture).
var createKinds = append(slices.Clone(rbacKinds),
	kindInfo{Kind: "resourcequotas", Title: "Resource Quotas", APIKind: "ResourceQuota", Version: "v1", Namespaced: true},
	kindInfo{Kind: kindSecrets, Title: "Secrets", APIKind: "Secret", Version: "v1", Namespaced: true})

// createFake is the RBAC fake with every create dialog's kind, their probes, the namespaces and
// the roles and role bindings of namespace ci in the cache, showing view.
func createFake(t testing.TB, view string) *fakeEngine {
	f := rbacFake(t, view)
	f.kinds = append(f.kinds, createKinds[len(rbacKinds):]...)
	registerCreateProbes(t, f)
	f.namespaces = []string{"ci", "default", "monitoring", "payments"}
	obj := func(kind, ns, name string, age time.Duration, fields map[string]any) *unstructured.Unstructured {
		apiVersion := "v1"
		if strings.Contains(kind, "Role") {
			apiVersion = rbacGroup + "/v1"
		}
		o := map[string]any{"apiVersion": apiVersion, "kind": kind,
			"metadata": map[string]any{"name": name, "namespace": ns, "uid": "uid-" + ns + "-" + name, "creationTimestamp": ts(age)}}
		if ns == "" {
			delete(o["metadata"].(map[string]any), "namespace")
		}
		for k, v := range fields {
			o[k] = v
		}
		return &unstructured.Unstructured{Object: o}
	}
	day := 24 * time.Hour
	var nss []*unstructured.Unstructured
	for i, n := range f.namespaces {
		nss = append(nss, obj("Namespace", "", n, time.Duration(30+i)*day, map[string]any{"status": map[string]any{"phase": "Active"}}))
	}
	f.tables[kindNamespaces] = kindSnapshot(t, kindNamespaces, nss)
	read := rbacRule([]any{""}, []any{"pods", "pods/log"}, []any{"get", "list", "watch"})
	f.tables["roles"] = kindSnapshot(t, "roles", []*unstructured.Unstructured{
		obj("Role", "ci", "deployer", 12*day, map[string]any{"rules": []any{rbacRule([]any{"apps"}, []any{"deployments"}, []any{"get", "patch"})}}),
		obj("Role", "ci", "pod-reader", 40*day, map[string]any{"rules": []any{read}}),
		obj("Role", "monitoring", "scraper", 40*day, map[string]any{"rules": []any{read}}),
	})
	ref := func(kind, name string) map[string]any {
		return map[string]any{"apiGroup": rbacGroup, "kind": kind, "name": name}
	}
	f.tables["rolebindings"] = kindSnapshot(t, "rolebindings", []*unstructured.Unstructured{
		obj("RoleBinding", "ci", "deployer", 12*day, map[string]any{"roleRef": ref("Role", "deployer"),
			"subjects": []any{map[string]any{"kind": "ServiceAccount", "name": "ci-deployer", "namespace": "ci"}}}),
		obj("RoleBinding", "monitoring", "scraper", 40*day, map[string]any{"roleRef": ref("Role", "scraper"),
			"subjects": []any{map[string]any{"kind": "ServiceAccount", "name": "metrics-collector", "namespace": "monitoring"}}}),
	})
	f.tables["resourcequotas"] = kindSnapshot(t, "resourcequotas", []*unstructured.Unstructured{
		obj("ResourceQuota", "payments", "compute", 9*day, map[string]any{"spec": map[string]any{"hard": map[string]any{"pods": "20"}},
			"status": map[string]any{"hard": map[string]any{"pods": "20"}, "used": map[string]any{"pods": "7"}}}),
	})
	f.tables[kindSecrets] = kindSnapshot(t, kindSecrets, []*unstructured.Unstructured{
		obj("Secret", "ci", "registry-pull", 3*day, map[string]any{"type": "kubernetes.io/dockerconfigjson", "data": map[string]any{".dockerconfigjson": "e30="}}),
	})
	f.ui.Namespaces = []string{"ci"}
	return f
}

// openForm opens a kind's create dialog and waits until it shows and its access check answered.
func openForm(t *testing.T, sh *shell, q *mainQueue, kind string) *createForm {
	t.Helper()
	q.until(t, "the kinds", func() bool { return len(sh.kinds) > 0 })
	form, ok := sh.createDialog(kind)
	if !ok {
		t.Fatalf("no create dialog for %s", kind)
	}
	q.until(t, "the "+kind+" dialog", func() bool { return form.shown })
	return form
}

// realRegistry is a session with Cogpit's real commands, to check the parameters a dialog sends.
func realRegistry(t *testing.T) *engine.Session {
	t.Helper()
	s := newTestEngine(engine.Options{})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	return s
}

// sent returns the parameters of the one call of a command, checked against the real registry.
func sent(t *testing.T, f *fakeEngine, s *engine.Session, id string) string {
	t.Helper()
	calls := f.callsOf(id)
	if len(calls) != 1 {
		t.Fatalf("%s called %d times", id, len(calls))
	}
	cmd, _ := s.Registry().Get(id)
	var perr *engine.ParamsError
	if err := cmd.Enabled(s, calls[0].Params); errors.As(err, &perr) {
		t.Errorf("%s %s: the registry refuses the dialog's parameters: %v", id, calls[0].Params, err)
	}
	return string(calls[0].Params)
}

// TestCreateButtonOpensTheKindsDialog: on the views with a create dialog the "+" opens it, titled
// by the command; elsewhere it opens a Create resource tab.
func TestCreateButtonOpensTheKindsDialog(t *testing.T) {
	f := createFake(t, "pods")
	sh, w, q := newCreateShell(t, f, ThemeDark)
	q.until(t, "the kinds", func() bool { return len(sh.kinds) > 0 })
	kinds := make([]string, 0, len(createDialogs))
	for k := range createDialogs {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		sh.dispatch("ui.openView", openViewParams{View: kind}, nil)
		q.until(t, "the "+kind+" view", func() bool { return sh.ui.View == kind && sh.view.create.Visible() })
		sh.view.create.Tapped(nil)
		cmd, _ := f.reg.Get(createDialogs[kind].command)
		q.until(t, "the "+kind+" dialog", func() bool { return slices.Contains(overlayTexts(w), cmd.Label) })
		var cancel *widget.Button
		for _, b := range findAll[*widget.Button](w.Canvas().Overlays().Top()) {
			if b.Text == "Cancel" {
				cancel = b
			}
		}
		test.Tap(cancel)
		q.until(t, "the "+kind+" dialog to close", func() bool { return w.Canvas().Overlays().Top() == nil })
	}
	if n := len(f.callsOf("resource.createTab")); n != 0 {
		t.Errorf("a dialog view opened %d Create resource tabs", n)
	}
	// The engine opens the tab; without an answer the button would try again without a template,
	// and a second call can land before the first is seen.
	f.mu.Lock()
	f.handlers["resource.createTab"] = func(context.Context, engine.Invocation) (any, error) { return nil, nil }
	f.mu.Unlock()
	sh.dispatch("ui.openView", openViewParams{View: "pods"}, nil)
	q.until(t, "the pods view", func() bool { return sh.ui.View == "pods" })
	sh.view.create.Tapped(nil)
	q.until(t, "a Create resource tab", func() bool { return len(f.callsOf("resource.createTab")) > 0 })
	q.drain()
	if calls := f.callsOf("resource.createTab"); len(calls) != 1 || decodeParams[createTabParams](t, calls[0]).Template != "Pod" {
		t.Errorf("Create resource tabs %+v, want one from the Pod template", calls)
	}
}

// TestNamespaceDialogCreates: the name typed is sent; the dialog closes once the command succeeded.
func TestNamespaceDialogCreates(t *testing.T) {
	f := createFake(t, kindNamespaces)
	sh, _, q := newCreateShell(t, f, ThemeDark)
	form := openForm(t, sh, q, kindNamespaces)
	if form.ns != nil || form.create.Disabled() {
		t.Errorf("namespace field %v, create disabled %v", form.ns, form.create.Disabled())
	}
	form.name.SetText(" team-x ")
	test.Tap(form.create)
	q.until(t, "the create and the dialog to close", func() bool { return len(f.callsOf("namespace.create")) == 1 && !form.shown })
	if got := sent(t, f, realRegistry(t), "namespace.create"); got != `{"name":"team-x"}` {
		t.Errorf("sent %s", got)
	}
}

// TestRoleBindingDialogOffersTheCache: the roles of the binding's namespace, or the cluster roles,
// and the namespace's service accounts are offered from the cache; the binding is named after its
// role until the user types a name; the subjects go out as rows.
func TestRoleBindingDialogOffersTheCache(t *testing.T) {
	f := createFake(t, "rolebindings")
	sh, _, q := newCreateShell(t, f, ThemeDark)
	form := openForm(t, sh, q, "rolebindings")
	if form.namespace() != "ci" {
		t.Errorf("namespace %q, want the filter's ci", form.namespace())
	}
	q.until(t, "the roles of ci", func() bool { return slices.Equal(form.offered[form.roleName], []string{"deployer", "pod-reader"}) })
	form.roleKind.SetSelected(roleKindClusterRole)
	q.until(t, "the cluster roles", func() bool { return slices.Contains(form.offered[form.roleName], "view") })
	form.roleName.SetText("view")
	if form.name.Text != "view" {
		t.Errorf("name %q, want the role's", form.name.Text)
	}
	subjects := form.editors[0]
	row := subjects.rows[0]
	row.fields[0].(*selectField).SetSelected(subjectServiceAccount)
	q.until(t, "ci's service accounts", func() bool {
		return slices.Equal(form.offered[row.fields[1].(*fieldSelectEntry)], []string{"ci-deployer"})
	})
	row.fields[1].(*fieldSelectEntry).SetText("ci-deployer")
	group := subjects.add()
	group.fields[0].(*selectField).SetSelected(subjectGroup)
	group.fields[1].(*fieldSelectEntry).SetText("oncall:sre")
	if !group.fields[2].(*fieldSelectEntry).Disabled() {
		t.Error("a group's namespace is editable")
	}
	subjects.add() // an empty row is left out
	test.Tap(form.create)
	q.until(t, "the create", func() bool { return len(f.callsOf("roleBinding.create")) == 1 && !form.shown })
	want := `{"name":"view","namespace":"ci","roleRef":{"kind":"ClusterRole","name":"view"},` +
		`"subjects":[{"kind":"ServiceAccount","name":"ci-deployer"},{"kind":"Group","name":"oncall:sre"}]}`
	if got := sent(t, f, realRegistry(t), "roleBinding.create"); got != want {
		t.Errorf("sent %s\nwant %s", got, want)
	}
}

// TestCreateDialogsSendRulesAndLimits: a role's rows become rules (empty API groups the core
// group, a cluster role's /path a non-resource URL); a quota's rows its hard limits, a resource
// limited twice refused in the dialog.
func TestCreateDialogsSendRulesAndLimits(t *testing.T) {
	f := createFake(t, "roles")
	sh, _, q := newCreateShell(t, f, ThemeDark)
	s := realRegistry(t)
	set := func(r *editorRow, texts ...string) {
		for i, txt := range texts {
			switch w := r.fields[i].(type) {
			case *fieldEntry:
				w.SetText(txt)
			case *fieldSelectEntry:
				w.SetText(txt)
			}
		}
	}

	role := openForm(t, sh, q, "roles")
	role.name.SetText("reader")
	set(role.editors[0].rows[0], "", "pods, pods/log", "get, list")
	set(role.editors[0].add(), "apps, batch", "deployments", "*")
	test.Tap(role.create)
	q.until(t, "the role", func() bool { return len(f.callsOf("role.create")) == 1 })
	want := `{"name":"reader","namespace":"ci","rules":[{"apiGroups":[""],"resources":["pods","pods/log"],"verbs":["get","list"]},` +
		`{"apiGroups":["apps","batch"],"resources":["deployments"],"verbs":["*"]}]}`
	if got := sent(t, f, s, "role.create"); got != want {
		t.Errorf("role: sent %s\nwant %s", got, want)
	}

	cr := openForm(t, sh, q, "clusterroles")
	cr.name.SetText("health")
	set(cr.editors[0].rows[0], "", "/healthz, /readyz", "get")
	test.Tap(cr.create)
	q.until(t, "the cluster role", func() bool { return len(f.callsOf("clusterRole.create")) == 1 })
	if got := sent(t, f, s, "clusterRole.create"); got != `{"name":"health","rules":[{"nonResourceURLs":["/healthz","/readyz"],"verbs":["get"]}]}` {
		t.Errorf("cluster role: sent %s", got)
	}

	quota := openForm(t, sh, q, "resourcequotas")
	quota.name.SetText("compute")
	set(quota.editors[0].rows[0], "pods", "10")
	set(quota.editors[0].add(), "pods", "12")
	test.Tap(quota.create)
	if quota.message != "pods is limited twice" || len(f.callsOf("resourceQuota.create")) != 0 {
		t.Errorf("a resource limited twice: %q", quota.message)
	}
	set(quota.editors[0].rows[1], "requests.cpu", "2")
	test.Tap(quota.create)
	q.until(t, "the quota", func() bool { return len(f.callsOf("resourceQuota.create")) == 1 })
	if got := sent(t, f, s, "resourceQuota.create"); got != `{"hard":{"pods":"10","requests.cpu":"2"},"name":"compute","namespace":"ci"}` {
		t.Errorf("quota: sent %s", got)
	}
}

// TestServiceAccountAndClusterRoleBindingDialogs: a service account lands in the filter's
// namespace; a cluster role binding names its cluster role, and a service account subject its
// namespace, offered from the cache.
func TestServiceAccountAndClusterRoleBindingDialogs(t *testing.T) {
	f := createFake(t, "serviceaccounts")
	sh, _, q := newCreateShell(t, f, ThemeDark)
	s := realRegistry(t)

	sa := openForm(t, sh, q, "serviceaccounts")
	sa.name.SetText("builder")
	test.Tap(sa.create)
	q.until(t, "the service account", func() bool { return len(f.callsOf("serviceAccount.create")) == 1 })
	if got := sent(t, f, s, "serviceAccount.create"); got != `{"name":"builder","namespace":"ci"}` {
		t.Errorf("service account: sent %s", got)
	}

	crb := openForm(t, sh, q, "clusterrolebindings")
	q.until(t, "the cluster roles", func() bool { return slices.Contains(crb.offered[crb.roleName], "cluster-admin") })
	crb.roleName.SetText("metrics-collector")
	row := crb.editors[0].rows[0]
	row.fields[0].(*selectField).SetSelected(subjectServiceAccount)
	row.fields[2].(*fieldSelectEntry).SetText("monitoring")
	q.until(t, "monitoring's service accounts", func() bool {
		return slices.Equal(crb.offered[row.fields[1].(*fieldSelectEntry)], []string{"metrics-collector"})
	})
	row.fields[1].(*fieldSelectEntry).SetText("metrics-collector")
	test.Tap(crb.create)
	q.until(t, "the cluster role binding", func() bool { return len(f.callsOf("clusterRoleBinding.create")) == 1 })
	want := `{"clusterRole":"metrics-collector","name":"metrics-collector","subjects":[{"kind":"ServiceAccount","name":"metrics-collector","namespace":"monitoring"}]}`
	if got := sent(t, f, s, "clusterRoleBinding.create"); got != want {
		t.Errorf("cluster role binding: sent %s\nwant %s", got, want)
	}
}

// TestCreateDialogNotPermitted: when the access reviews deny the create, Create is greyed and the
// dialog says why.
func TestCreateDialogNotPermitted(t *testing.T) {
	f := createFake(t, "rolebindings")
	const reason = "not permitted: you may not create rolebindings.rbac.authorization.k8s.io in ci"
	f.disabled["roleBinding.create"] = reason
	sh, _, q := newCreateShell(t, f, ThemeDark)
	form := openForm(t, sh, q, "rolebindings")
	q.until(t, "the denial", func() bool { return form.denied != "" })
	if !form.create.Disabled() || form.status.text != reason || form.status.color != colorStatusError {
		t.Errorf("create disabled %v, status %q", form.create.Disabled(), form.status.text)
	}
	form.submit() // Enter in the name field
	if len(f.callsOf("roleBinding.create")) != 0 {
		t.Error("a denied create was sent")
	}
}

// TestCreateDialogStaysOpenOnError: a create the command refuses keeps the dialog open with the
// reason and Create enabled again.
func TestCreateDialogStaysOpenOnError(t *testing.T) {
	f := createFake(t, kindNamespaces)
	f.handlers["namespace.create"] = func(context.Context, engine.Invocation) (any, error) {
		return nil, errors.New(`namespaces/ci was not created: namespaces "ci" already exists`)
	}
	sh, _, q := newCreateShell(t, f, ThemeDark)
	form := openForm(t, sh, q, kindNamespaces)
	form.name.SetText("ci")
	test.Tap(form.create)
	q.until(t, "the error", func() bool { return strings.Contains(form.message, "already exists") })
	if !form.shown || form.create.Disabled() || form.status.color != colorStatusError {
		t.Errorf("shown %v, create disabled %v, colour %s", form.shown, form.create.Disabled(), form.status.color)
	}
}

// renderCreateDialog renders a view with its create dialog open, filled in by fill.
func renderCreateDialog(t *testing.T, view string, mode ThemeMode, name string, fill func(*createForm, *mainQueue)) {
	f := createFake(t, view)
	sh, w, q := newCreateShell(t, f, mode)
	q.until(t, "the "+view+" view", func() bool {
		return len(sh.kinds) > 0 && sh.view.snap != nil && sh.view.snap.View == view && sh.diag.total > 0
	})
	form := openForm(t, sh, q, view)
	fill(form, q)
	q.drain()
	sh.win.Canvas().Unfocus()
	form.fit()
	settle(w)
	savePNG(t, w, name)
}

func fillNamespace(form *createForm, _ *mainQueue) { form.name.SetText("team-payments") }

func TestRenderCreateNamespaceDialogDark(t *testing.T) {
	renderCreateDialog(t, kindNamespaces, ThemeDark, "create_namespace_dialog_dark.png", fillNamespace)
}

func fillRoleBinding(form *createForm, q *mainQueue) {
	form.roleKind.SetSelected(roleKindClusterRole)
	form.roleName.SetText("view")
	subjects := form.editors[0]
	sa := subjects.rows[0]
	sa.fields[0].(*selectField).SetSelected(subjectServiceAccount)
	sa.fields[1].(*fieldSelectEntry).SetText("ci-deployer")
	g := subjects.add()
	g.fields[0].(*selectField).SetSelected(subjectGroup)
	g.fields[1].(*fieldSelectEntry).SetText("oncall:sre")
	u := subjects.add()
	u.fields[1].(*fieldSelectEntry).SetText("carol@example.com")
	q.drain()
}

func TestRenderCreateRoleBindingDialogDark(t *testing.T) {
	renderCreateDialog(t, "rolebindings", ThemeDark, "create_rolebinding_dialog_dark.png", fillRoleBinding)
}

func TestRenderCreateRoleDialogDark(t *testing.T) {
	renderCreateDialog(t, "roles", ThemeDark, "create_role_dialog_dark.png", func(form *createForm, _ *mainQueue) {
		form.name.SetText("release-manager")
		rules := form.editors[0]
		r := rules.rows[0]
		r.fields[1].(*fieldEntry).SetText("pods, pods/log")
		r.fields[2].(*fieldEntry).SetText("get, list, watch")
		r2 := rules.add()
		r2.fields[0].(*fieldEntry).SetText("apps")
		r2.fields[1].(*fieldEntry).SetText("deployments, replicasets")
		r2.fields[2].(*fieldEntry).SetText("get, patch")
	})
}

func TestRenderCreateResourceQuotaDialogDark(t *testing.T) {
	renderCreateDialog(t, "resourcequotas", ThemeDark, "create_resourcequota_dialog_dark.png", func(form *createForm, _ *mainQueue) {
		form.name.SetText("compute")
		form.setNamespace("payments")
		limits := form.editors[0]
		for i, kv := range [][2]string{{"pods", "20"}, {"requests.cpu", "4"}, {"limits.memory", "8Gi"}} {
			r := limits.rows[0]
			if i > 0 {
				r = limits.add()
			}
			r.fields[0].(*fieldSelectEntry).SetText(kv[0])
			r.fields[1].(*fieldEntry).SetText(kv[1])
		}
	})
}
