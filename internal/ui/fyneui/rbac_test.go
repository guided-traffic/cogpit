package fyneui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// The Access Control views rendered through the real kind profiles from synthetic RBAC objects:
// the Cluster Role Bindings list with its warning icons and Bindings column, a binding's drawer
// (its cluster role missing, subjects as a table with a service account link) and a cluster role's
// drawer with its rules table. The related cluster roles come from the fixture, as the engine
// gives them from the cache. Every name is made up. PNGs: testdata/{clusterrolebindings_list,
// clusterrolebinding_drawer,clusterrole_drawer}_*.png.

const rbacGroup = "rbac.authorization.k8s.io"

// rbacKinds are the Access Control kinds as kinds.list reports them.
var rbacKinds = []kindInfo{
	{Kind: "serviceaccounts", Title: "Service Accounts", APIKind: "ServiceAccount", Version: "v1", Namespaced: true},
	{Kind: "clusterroles", Title: "Cluster Roles", APIKind: "ClusterRole", Group: rbacGroup, Version: "v1"},
	{Kind: "roles", Title: "Roles", APIKind: "Role", Group: rbacGroup, Version: "v1", Namespaced: true},
	{Kind: "clusterrolebindings", Title: "Cluster Role Bindings", APIKind: "ClusterRoleBinding", Group: rbacGroup, Version: "v1"},
	{Kind: "rolebindings", Title: "Role Bindings", APIKind: "RoleBinding", Group: rbacGroup, Version: "v1", Namespaced: true},
}

func rbacRule(groups, resources, verbs []any) map[string]any {
	return map[string]any{"apiGroups": groups, "resources": resources, "verbs": verbs}
}

func fixtureClusterRoleObjects() []*unstructured.Unstructured {
	read := []any{"get", "list", "watch"}
	all := []any{"*"}
	role := func(name string, age time.Duration, meta map[string]any, fields map[string]any) *unstructured.Unstructured {
		m := map[string]any{"name": name, "uid": "uid-cr-" + name, "creationTimestamp": ts(age)}
		for k, v := range meta {
			m[k] = v
		}
		obj := map[string]any{"apiVersion": rbacGroup + "/v1", "kind": "ClusterRole", "metadata": m}
		for k, v := range fields {
			obj[k] = v
		}
		return &unstructured.Unstructured{Object: obj}
	}
	bootstrap := map[string]any{"annotations": map[string]any{"rbac.authorization.kubernetes.io/autoupdate": "true"},
		"labels": map[string]any{"kubernetes.io/bootstrapping": "rbac-defaults"}}
	days := func(n int) time.Duration { return time.Duration(n) * 24 * time.Hour }
	return []*unstructured.Unstructured{
		role("cluster-admin", days(160), bootstrap, map[string]any{"rules": []any{
			rbacRule(all, all, all), map[string]any{"nonResourceURLs": all, "verbs": all}}}),
		role("view", days(160), bootstrap, map[string]any{
			"aggregationRule": map[string]any{"clusterRoleSelectors": []any{map[string]any{"matchLabels": map[string]any{"rbac.authorization.k8s.io/aggregate-to-view": "true"}}}},
			"rules":           []any{rbacRule([]any{""}, []any{"configmaps", "endpoints", "pods", "services"}, read)}}),
		role("edit", days(160), bootstrap, map[string]any{
			"aggregationRule": map[string]any{"clusterRoleSelectors": []any{map[string]any{"matchLabels": map[string]any{"rbac.authorization.k8s.io/aggregate-to-edit": "true"}}}},
			"rules":           []any{rbacRule([]any{"apps"}, []any{"deployments", "statefulsets"}, []any{"create", "delete", "patch", "update"})}}),
		role("system:node", days(160), bootstrap, map[string]any{"rules": []any{rbacRule([]any{""}, []any{"nodes"}, read)}}),
		role("system:coredns", days(160), bootstrap, map[string]any{"rules": []any{rbacRule([]any{""}, []any{"endpoints", "services", "pods", "namespaces"}, []any{"list", "watch"})}}),
		role("system:kube-scheduler", days(160), bootstrap, map[string]any{"rules": []any{rbacRule([]any{""}, []any{"pods/binding"}, []any{"create"})}}),
		role("system:public-info-viewer", days(160), bootstrap, map[string]any{"rules": []any{
			map[string]any{"nonResourceURLs": []any{"/healthz", "/livez", "/readyz", "/version"}, "verbs": []any{"get"}}}}),
		role("metrics-collector", days(51), nil, map[string]any{"rules": []any{
			rbacRule([]any{""}, []any{"nodes", "nodes/metrics", "pods", "services"}, read),
			rbacRule([]any{"apps"}, []any{"deployments", "statefulsets", "daemonsets"}, read),
			map[string]any{"apiGroups": []any{""}, "resources": []any{"configmaps"}, "resourceNames": []any{"metrics-collector-config"}, "verbs": []any{"get", "update"}},
			rbacRule([]any{"monitoring.coreos.com"}, []any{"servicemonitors", "podmonitors", "prometheusrules"}, read),
			map[string]any{"nonResourceURLs": []any{"/metrics", "/metrics/cadvisor"}, "verbs": []any{"get"}},
		}}),
		role("ingress-nginx", days(88), nil, map[string]any{"rules": []any{
			rbacRule([]any{""}, []any{"configmaps", "endpoints", "nodes", "pods", "secrets", "namespaces"}, []any{"list", "watch"}),
			rbacRule([]any{"networking.k8s.io"}, []any{"ingresses", "ingressclasses"}, read),
			rbacRule([]any{"networking.k8s.io"}, []any{"ingresses/status"}, []any{"update"}),
		}}),
		role("cert-manager-controller-issuers", days(88), nil, map[string]any{"rules": []any{
			rbacRule([]any{"cert-manager.io"}, []any{"issuers", "issuers/status"}, []any{"update", "patch"}),
			rbacRule([]any{""}, []any{"secrets"}, []any{"get", "list", "watch", "create", "update", "delete"}),
		}}),
		role("tenant-admin", days(26), nil, map[string]any{
			"aggregationRule": map[string]any{"clusterRoleSelectors": []any{map[string]any{"matchLabels": map[string]any{"rbac.example.com/aggregate-to-tenant-admn": "true"}}}}}),
		role("reporting-reader", days(10), nil, map[string]any{"rules": []any{}}),
		role("legacy-dashboard", days(400), nil, map[string]any{"rules": []any{rbacRule(all, all, read)}}),
	}
}

func fixtureClusterRoleBindingObjects() []*unstructured.Unstructured {
	type subject struct{ kind, name, ns string }
	sa := func(ns, name string) subject { return subject{"ServiceAccount", name, ns} }
	group := func(name string) subject { return subject{"Group", name, ""} }
	user := func(name string) subject { return subject{"User", name, ""} }
	days := func(n int) time.Duration { return time.Duration(n) * 24 * time.Hour }
	binding := func(name, role string, age time.Duration, bootstrap bool, subs ...subject) *unstructured.Unstructured {
		meta := map[string]any{"name": name, "uid": "uid-crb-" + name, "creationTimestamp": ts(age)}
		if bootstrap {
			meta["annotations"] = map[string]any{"rbac.authorization.kubernetes.io/autoupdate": "true"}
		}
		var list []any
		for _, s := range subs {
			e := map[string]any{"kind": s.kind, "name": s.name}
			if s.ns != "" {
				e["namespace"] = s.ns
			} else {
				e["apiGroup"] = rbacGroup
			}
			list = append(list, e)
		}
		obj := map[string]any{"apiVersion": rbacGroup + "/v1", "kind": "ClusterRoleBinding", "metadata": meta,
			"roleRef": map[string]any{"apiGroup": rbacGroup, "kind": "ClusterRole", "name": role}}
		if list != nil {
			obj["subjects"] = list
		}
		return &unstructured.Unstructured{Object: obj}
	}
	return []*unstructured.Unstructured{
		binding("cluster-admin", "cluster-admin", days(160), true, group("system:masters")),
		binding("kubeadm:cluster-admins", "cluster-admin", days(160), false, group("kubeadm:cluster-admins")),
		binding("system:node", "system:node", days(160), true),
		binding("system:coredns", "system:coredns", days(160), true, sa("kube-system", "coredns")),
		binding("system:kube-scheduler", "system:kube-scheduler", days(160), true, user("system:kube-scheduler")),
		binding("system:public-info-viewer", "system:public-info-viewer", days(160), true, group("system:authenticated"), group("system:unauthenticated")),
		binding("metrics-collector", "metrics-collector", days(51), false, sa("monitoring", "metrics-collector"), group("oncall:sre")),
		binding("ingress-nginx", "ingress-nginx", days(88), false, sa("ingress-nginx", "ingress-nginx")),
		binding("cert-manager-controller-issuers", "cert-manager-controller-issuers", days(88), false, sa("cert-manager", "cert-manager")),
		binding("backup-operator", "backup-operator", days(10), false, sa("backup", "velero"), group("oncall:sre")),
		binding("tenant-a-admins", "tenant-admin", days(26), false, group("tenant-a:admins"), user("bob@example.com"), user("carol@example.com")),
		binding("ci-deployer", "edit", days(40), false, sa("ci", "ci-deployer")),
		binding("legacy-dashboard", "legacy-dashboard", days(400), false),
	}
}

func fixtureServiceAccountObjects() []*unstructured.Unstructured {
	var out []*unstructured.Unstructured
	for _, a := range [][2]string{{"backup", "velero"}, {"ci", "ci-deployer"}, {"monitoring", "metrics-collector"}, {"kube-system", "coredns"}} {
		out = append(out, &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1", "kind": "ServiceAccount",
			"metadata": map[string]any{"name": a[1], "namespace": a[0], "uid": "uid-sa-" + a[1], "creationTimestamp": ts(30 * 24 * time.Hour)},
		}})
	}
	return out
}

// rbacRelated serves the related-objects indexes from fixture objects, as the engine does from the
// cache.
type rbacRelated map[string]map[string][]*unstructured.Unstructured

func newRBACRelated(ix model.RelatedIndex, objs []*unstructured.Unstructured) rbacRelated {
	byKey := map[string][]*unstructured.Unstructured{}
	for _, o := range objs {
		for _, k := range ix.Keys(o) {
			byKey[k] = append(byKey[k], o)
		}
	}
	return rbacRelated{ix.ID(): byKey}
}

func (r rbacRelated) Listed(ix model.RelatedIndex) bool { return len(r[ix.ID()]) > 0 }

func (r rbacRelated) Objects(ix model.RelatedIndex, key string) []*unstructured.Unstructured {
	return r[ix.ID()][key]
}

// rbacFake is a fake connected to the demo context with the Access Control kinds, whose views and
// drawers are built by the real profiles with the fixture's cluster roles as related objects.
func rbacFake(t testing.TB, view string) *fakeEngine {
	f := connectedFake(t, 1)
	f.kinds = append(f.kinds, rbacKinds...)
	roles := fixtureClusterRoleObjects()
	rel := newRBACRelated(kinds.ClusterRolesByName, roles)
	for key, objs := range map[string][]*unstructured.Unstructured{
		"clusterroles": roles, "clusterrolebindings": fixtureClusterRoleBindingObjects(), "serviceaccounts": fixtureServiceAccountObjects(),
	} {
		p, _ := kinds.ByKey(key)
		rows := make([]model.Row, 0, len(objs))
		for _, o := range objs {
			rows = append(rows, model.BuildRowWith(p, "demo", o, rel))
			ref := model.ObjectRef{Context: "demo", Kind: key, Namespace: o.GetNamespace(), Name: o.GetName()}
			m, err := engine.BuildDetailWith(p, ref, o, nil, fixedNow, rel)
			if err != nil {
				t.Fatal(err)
			}
			f.details[ref.String()] = m
		}
		slices.SortFunc(rows, func(a, b model.Row) int { return strings.Compare(a.Ref.Name, b.Ref.Name) })
		f.tables[key] = &engine.TableSnapshot{
			View: key, Context: "demo", Title: p.Title, Namespaced: p.Namespaced, Columns: p.Columns, Rows: rows,
			Sort: engine.SortSpec{Column: "name"}, Total: len(rows), Revision: 1, Synced: true,
		}
	}
	f.ui.View = view
	return f
}

func renderRBACList(t *testing.T, view string, mode ThemeMode, name string) {
	f := rbacFake(t, view)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the "+view+" view", func() bool {
		return len(sh.kinds) > 0 && sh.view.snap != nil && sh.view.snap.View == view && sh.diag.total > 0
	})
	settle(w)
	savePNG(t, w, name)
}

func renderRBACDrawer(t *testing.T, view string, ref model.ObjectRef, mode ThemeMode, name string) {
	f := rbacFake(t, view)
	registerProbes(t, f)
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the drawer", func() bool {
		return len(sh.kinds) > 0 && sh.drawer.last != nil && len(sh.drawer.actions.Objects) > 0 && sh.diag.total > 0
	})
	settle(w)
	savePNG(t, w, name)
}

var (
	missingRoleBindingRef = model.ObjectRef{Context: "demo", Kind: "clusterrolebindings", Name: "backup-operator"}
	collectorRoleRef      = model.ObjectRef{Context: "demo", Kind: "clusterroles", Name: "metrics-collector"}
)

func TestRenderClusterRoleBindingsListDark(t *testing.T) {
	renderRBACList(t, "clusterrolebindings", ThemeDark, "clusterrolebindings_list_dark.png")
}

func TestRenderClusterRoleBindingDrawerDark(t *testing.T) {
	renderRBACDrawer(t, "clusterrolebindings", missingRoleBindingRef, ThemeDark, "clusterrolebinding_drawer_dark.png")
}

func TestRenderClusterRoleDrawerDark(t *testing.T) {
	renderRBACDrawer(t, "clusterroles", collectorRoleRef, ThemeDark, "clusterrole_drawer_dark.png")
}

// TestClusterRoleBindingDrawerLinks: in a binding's drawer a ServiceAccount subject is a link to
// the account, the cluster role is a link while it exists and plain text when it does not.
func TestClusterRoleBindingDrawerLinks(t *testing.T) {
	f := rbacFake(t, "clusterrolebindings")
	ref := model.ObjectRef{Context: "demo", Kind: "clusterrolebindings", Name: "metrics-collector"}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && len(sh.drawer.details.Objects) > 2 })

	links := drawerLinks(sh)
	if links["ClusterRole metrics-collector"] == nil || links["metrics-collector"] == nil {
		t.Fatalf("links %v, texts %q", keys(links), drawerTexts(sh))
	}
	if links["oncall:sre"] != nil {
		t.Error("a group is not an object to link to")
	}
	test.Tap(links["metrics-collector"])
	// The link dispatches on the shell's goroutine: the call is recorded as Dispatch starts, the UI
	// state changes after. Wait for the change, so that nothing of this shell touches the fake
	// once the second one starts on it.
	account := model.ObjectRef{Context: "demo", Kind: "serviceaccounts", Namespace: "monitoring", Name: "metrics-collector"}
	q.until(t, "ui.navigate", func() bool { d := f.UIState().Drawer; return d != nil && *d == account })
	if calls := f.callsOf("ui.navigate"); len(calls) != 1 || decodeParams[refParams](t, calls[0]).Ref != account {
		t.Errorf("subject link: %d navigations, want one to %+v", len(calls), account)
	}
	sh.stop()

	// The binding whose cluster role does not exist names it as text and says why it warns.
	f.update(func(u *engine.UIState) {
		u.Selected, u.Drawer = []model.ObjectRef{missingRoleBindingRef}, &missingRoleBindingRef
	})
	sh2, _, q2 := newTestShell(t, f, Options{})
	q2.until(t, "the drawer", func() bool { return sh2.drawer.last != nil && len(sh2.drawer.details.Objects) > 2 })
	if drawerLinks(sh2)["ClusterRole backup-operator"] != nil {
		t.Error("a cluster role that does not exist is a link")
	}
	if !pillWith(sh2, "Cluster role backup-operator not found") {
		t.Errorf("no warning pill in %q", drawerTexts(sh2))
	}
}

// pillWith reports whether the drawer shows a pill whose text starts with prefix.
func pillWith(sh *shell, prefix string) bool {
	for _, p := range findAll[*pill](sh.drawer.details) {
		if strings.HasPrefix(p.text, prefix) {
			return true
		}
	}
	return false
}

// TestPodServiceAccountLinkLive: once the cluster serves Service Accounts and Cogpit has their
// view, a pod drawer's Service Account is a link to the account.
func TestPodServiceAccountLinkLive(t *testing.T) {
	f := kindsFake(t, "pods")
	f.kinds = append(f.kinds, rbacKinds...)
	p := richPod(t)
	ref := model.ObjectRef{Context: "demo", Kind: "pods", Namespace: p.ns, Name: p.name}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && len(sh.drawer.details.Objects) > 2 })
	account := strings.Split(p.name, "-")[0]
	link := drawerLinks(sh)[account]
	if link == nil {
		t.Fatalf("the service account %q is not a link: %v", account, keys(drawerLinks(sh)))
	}
	test.Tap(link)
	q.until(t, "ui.navigate", func() bool { return len(f.callsOf("ui.navigate")) == 1 })
	if got := decodeParams[refParams](t, f.callsOf("ui.navigate")[0]).Ref; got != (model.ObjectRef{Context: "demo", Kind: "serviceaccounts", Namespace: p.ns, Name: account}) {
		t.Errorf("service account link %+v", got)
	}
}
