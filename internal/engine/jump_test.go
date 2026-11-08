package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

func TestParseJump(t *testing.T) {
	for _, tt := range []struct {
		text string
		want JumpQuery
	}{
		{":", JumpQuery{}},
		{":po", JumpQuery{Name: "po"}},
		{":PODS kube-system", JumpQuery{Name: "pods", Namespace: "kube-system"}},
		{":po /web", JumpQuery{Name: "po", Filter: "web"}},
		{":po /web /api", JumpQuery{Name: "po", Filter: "web api"}},
		{":po /", JumpQuery{Name: "po"}},
		{":po @prod", JumpQuery{Name: "po", Cluster: "prod", AtCluster: true}},
		{":po @", JumpQuery{Name: "po", AtCluster: true}},
		{":deploy @prod shop /web", JumpQuery{Name: "deploy", Namespace: "shop", Filter: "web", Cluster: "prod", AtCluster: true}},
		{":po all", JumpQuery{Name: "po", Namespace: JumpAllNamespaces}},
		{":po -A", JumpQuery{Name: "po", Namespace: JumpAllNamespaces}},
		{":@prod", JumpQuery{Cluster: "prod", AtCluster: true}},
		{":po app=web", JumpQuery{Name: "po", Problem: "label selectors are not supported: search with /web"}},
		{":po -l", JumpQuery{Name: "po", Problem: "label selectors are not supported: search with /text"}},
		{":po shop infra", JumpQuery{Name: "po", Namespace: "infra", Problem: "one namespace at a time: the namespace filter takes several"}},
		{":po @a @b", JumpQuery{Name: "po", Cluster: "b", AtCluster: true, Problem: "one cluster at a time"}},
		{":po kube_sys", JumpQuery{Name: "po", Namespace: "kube_sys", Problem: "“kube_sys” is not a namespace name"}},
		{":po kube-", JumpQuery{Name: "po", Namespace: "kube-"}}, // typed so far: the namespaces complete it
	} {
		got, ok := ParseJump(tt.text)
		if !ok || got != tt.want {
			t.Errorf("%q: %+v (%v), want %+v", tt.text, got, ok, tt.want)
		}
	}
	for _, text := range []string{"", "po", " :po", "Switch to"} {
		if _, ok := ParseJump(text); ok {
			t.Errorf("%q read as a jump", text)
		}
	}
}

// jumpRegistry has stand-ins of ui.jump and cluster.connect, as commands/ registers them.
func jumpRegistry(t *testing.T) *Registry {
	t.Helper()
	r := NewRegistry()
	type connectParams struct {
		Context string `json:"context"`
	}
	for _, err := range []error{
		Register(r, Spec[JumpParams]{ID: JumpCommand, Label: "Jump to View", Danger: DangerUI,
			Run: func(context.Context, *Session, JumpParams) (any, error) { return nil, nil }}),
		Register(r, Spec[connectParams]{ID: connectCommandID, Label: "Connect to Cluster", MenuPath: "Cluster/Connect to Cluster",
			Shortcut: "Cmd+P", Danger: DangerUI, Run: func(context.Context, *Session, connectParams) (any, error) { return nil, nil }}),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	return r
}

// jumpIndex is a shown cluster with built-in, virtual, local and custom kinds, its namespaces, and
// four contexts, two of them in one file.
func jumpIndex() JumpIndex {
	views := JumpViewsFor([]JumpKind{
		{Key: "nodes", Title: "Nodes", Kind: "Node", Names: model.KindNames{Singular: "node", ShortNames: []string{"no"}}},
		{Key: "pods", Title: "Pods", Kind: "Pod", Names: model.KindNames{Singular: "pod", ShortNames: []string{"po"}}, Namespaced: true},
		{Key: "poddisruptionbudgets", Title: "Pod Disruption Budgets", Kind: "PodDisruptionBudget",
			Names: model.KindNames{Singular: "poddisruptionbudget", ShortNames: []string{"pdb"}}, Namespaced: true},
		{Key: "deployments", Title: "Deployments", Kind: "Deployment", Names: model.KindNames{Singular: "deployment", ShortNames: []string{"deploy"}}, Namespaced: true},
		{Key: "namespaces", Title: "Namespaces", Kind: "Namespace", Names: model.KindNames{Singular: "namespace", ShortNames: []string{"ns"}}},
		{Key: "events", Title: "Events", Kind: "Event", Names: model.KindNames{Singular: "event", ShortNames: []string{"ev"}}, Namespaced: true},
		{Key: helm.ReleaseKind, Title: "Releases", Kind: "HelmRelease", Namespaced: true},
		{Key: PortForwardKind, Title: "Port Forwards", Kind: "PortForward", Namespaced: true},
		{Key: "certificates.cert-manager.io", Title: "Certificate", Kind: "Certificate",
			Names: model.KindNames{Singular: "certificate", ShortNames: []string{"cert", "certs"}}, Namespaced: true},
	})
	return JumpIndex{
		Context: "kind-cogpit", Views: views,
		Namespaces: []string{"default", "kube-node-lease", "kube-public", "kube-system", "shop"},
		Clusters: []JumpCluster{
			{Context: "kind-cogpit", Name: "kind-cogpit"},
			{Context: "prod-eu [prod.yaml]", Name: "prod-eu"},
			{Context: "prod-us [prod.yaml]", Name: "prod-us"},
			{Context: "dev", Name: "Dev cluster"},
		},
	}
}

func jumpParamsOf(t *testing.T, e PaletteEntry) JumpParams {
	t.Helper()
	var p JumpParams
	if err := json.Unmarshal(e.Params, &p); err != nil {
		t.Fatalf("%s: %v", e.Params, err)
	}
	return p
}

// A jump's entries, best first: a kind by its short name, singular, plural or Kind in any case,
// the pages and Cogpit's kinds by k9s's names, the namespace completed from the shown cluster's,
// the search, another cluster by its context or shown name; what cannot run greyed with the reason.
func TestJumpEntries(t *testing.T) {
	r, ix := jumpRegistry(t), jumpIndex()
	type want struct {
		label  string
		params JumpParams
		reason string
		group  string
	}
	for _, tt := range []struct {
		text  string
		first want
		count int // 0: not checked
	}{
		{":po", want{label: "Switch to Pods", params: JumpParams{View: "pods"}}, 3}, // and Pod Disruption Budgets and Port Forwards, which "po" begins
		{":pod", want{label: "Switch to Pods", params: JumpParams{View: "pods"}}, 0},
		{":PODS", want{label: "Switch to Pods", params: JumpParams{View: "pods"}}, 0},
		{":Pod", want{label: "Switch to Pods", params: JumpParams{View: "pods"}}, 0},
		{":pdb", want{label: "Switch to Pod Disruption Budgets", params: JumpParams{View: "poddisruptionbudgets"}}, 1},
		{":deploy kube-system", want{label: "Switch to Deployments in kube-system", params: JumpParams{View: "deployments", Namespace: "kube-system"}}, 1},
		{":deploy kube", want{label: "Switch to Deployments in kube-node-lease", params: JumpParams{View: "deployments", Namespace: "kube-node-lease"}}, 3},
		{":deploy ys", want{label: "Switch to Deployments in kube-system", params: JumpParams{View: "deployments", Namespace: "kube-system"}}, 1},
		{":deploy nope", want{label: "Switch to Deployments in nope", params: JumpParams{View: "deployments", Namespace: "nope"},
			reason: "no namespace “nope” in kind-cogpit"}, 1},
		{":po all", want{label: "Switch to Pods in all namespaces", params: JumpParams{View: "pods", Namespace: JumpAllNamespaces}}, 0},
		{":deploy shop /web /api", want{label: "Switch to Deployments in shop matching “web api”",
			params: JumpParams{View: "deployments", Namespace: "shop", Filter: "web api"}}, 1},
		{":no kube-system", want{label: "Switch to Nodes in kube-system", params: JumpParams{View: "nodes", Namespace: "kube-system"},
			reason: "Nodes are not namespaced"}, 1},
		{":no all", want{label: "Switch to Nodes in all namespaces", params: JumpParams{View: "nodes", Namespace: JumpAllNamespaces}}, 1},
		{":ns", want{label: "Switch to Namespaces", params: JumpParams{View: "namespaces"}}, 1},
		{":ev", want{label: "Switch to Events", params: JumpParams{View: "events"}}, 1},
		{":cert", want{label: "Switch to Certificate", params: JumpParams{View: "certificates.cert-manager.io"}, group: "cert-manager.io"}, 1},
		{":certificates.cert-manager.io", want{label: "Switch to Certificate", params: JumpParams{View: "certificates.cert-manager.io"}, group: "cert-manager.io"}, 1},
		{":pulses", want{label: "Switch to Cluster Overview", params: JumpParams{View: ClusterOverview}}, 1},
		{":pu", want{label: "Switch to Cluster Overview", params: JumpParams{View: ClusterOverview}}, 0},
		{":wk shop", want{label: "Switch to Workloads Overview in shop", params: JumpParams{View: WorkloadsOverview, Namespace: "shop"}}, 1},
		{":helm", want{label: "Switch to Releases", params: JumpParams{View: helm.ReleaseKind}}, 1},
		{":pf", want{label: "Switch to Port Forwards", params: JumpParams{View: PortForwardKind}}, 1},
		{":xyz", want{label: ":xyz", reason: "no view named “xyz”"}, 1},
		{":xray", want{label: ":xray", reason: "no view named “xray”"}, 1},
		{":po app=web", want{label: ":po app=web", reason: "label selectors are not supported: search with /web"}, 1},
		{":po shop infra", want{label: ":po shop infra", reason: "one namespace at a time: the namespace filter takes several"}, 1},
		{":po @prod-eu", want{label: "Switch to Pods on prod-eu", params: JumpParams{View: "pods", Context: "prod-eu [prod.yaml]"}}, 3},
		{":po @prod-eu [prod.yaml]", want{label: ":po @prod-eu [prod.yaml]", reason: "“[prod.yaml]” is not a namespace name"}, 1},
		{":pods @PROD", want{label: "Switch to Pods on prod-eu", params: JumpParams{View: "pods", Context: "prod-eu [prod.yaml]"}}, 2},
		{":po @dev cluster", want{label: "Switch to Pods in cluster on Dev cluster", params: JumpParams{View: "pods", Namespace: "cluster", Context: "dev"}}, 0},
		{":po @kind-cogpit", want{label: "Switch to Pods", params: JumpParams{View: "pods"}}, 0},
		{":po @nope", want{label: ":po @nope", reason: "no cluster “nope”"}, 1},
		{":cert @dev nope", want{label: "Switch to Certificate in nope on Dev cluster", params: JumpParams{View: "certificates.cert-manager.io", Namespace: "nope", Context: "dev"},
			group: "cert-manager.io"}, 0},
		{":widgets @dev", want{label: "Switch to “widgets” on Dev cluster", params: JumpParams{View: "widgets", Context: "dev"}}, 1},
	} {
		t.Run(tt.text, func(t *testing.T) {
			got := r.JumpEntries(ix, tt.text, nil)
			if len(got) == 0 {
				t.Fatal("no entries")
			}
			e := got[0]
			if e.Command != JumpCommand || e.Danger != DangerUI || e.Label != tt.first.label || e.Reason != tt.first.reason || e.Category != tt.first.group {
				t.Errorf("first %+v, want %+v", e, tt.first)
			}
			if tt.first.reason == "" && jumpParamsOf(t, e) != tt.first.params {
				t.Errorf("params %s, want %+v", e.Params, tt.first.params)
			}
			if tt.count > 0 && len(got) != tt.count {
				t.Errorf("%d entries, want %d: %q", len(got), tt.count, labelsOf(got))
			}
		})
	}
	if got := r.JumpEntries(ix, ":", nil); len(got) != len(ix.Views) {
		t.Errorf(": lists %d entries, want every view (%d)", len(got), len(ix.Views))
	}
	if got := r.JumpEntries(ix, "pods", nil); got != nil {
		t.Errorf("a text without : made jump entries: %q", labelsOf(got))
	}
}

func labelsOf(entries []PaletteEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Label)
	}
	return out
}

// Without a connected cluster the names are not known: the jump needs @, and a name another
// cluster may serve is ui.jump's to resolve there. Namespaces not known are not checked.
func TestJumpEntriesWithoutTheShownCluster(t *testing.T) {
	r, ix := jumpRegistry(t), jumpIndex()
	ix.Context, ix.Views = "", nil
	if got := r.JumpEntries(ix, ":po", nil); len(got) != 1 || got[0].Reason != "no cluster is connected: name one with @" {
		t.Errorf(":po without a cluster: %+v", got)
	}
	got := r.JumpEntries(ix, ":po shop @dev", nil)
	if len(got) != 1 || got[0].Reason != "" || jumpParamsOf(t, got[0]) != (JumpParams{View: "po", Namespace: "shop", Context: "dev"}) {
		t.Errorf(":po shop @dev: %+v", got)
	}
	if got := r.JumpEntries(ix, ":@dev", nil); len(got) != 1 || got[0].Reason != "type a view's name" {
		t.Errorf(":@dev: %+v", got)
	}
	ix = jumpIndex()
	ix.Namespaces = nil
	if got := r.JumpEntries(ix, ":po new-ns", nil); len(got) != 3 || got[0].Reason != "" || jumpParamsOf(t, got[0]).Namespace != "new-ns" {
		t.Errorf("unknown namespaces: %+v", got)
	}
}

// :ctx lists the clusters: the window's list (⌘P's, the caller's own way) while no word follows,
// then every cluster to open, the word narrowing them as @ does.
func TestJumpClusterEntries(t *testing.T) {
	r, ix := jumpRegistry(t), jumpIndex()
	runs := func(id string) bool { return id == connectCommandID }
	got := r.JumpEntries(ix, ":ctx", runs)
	want := []string{"Connect to Cluster", "Open kind-cogpit", "Open prod-eu", "Open prod-us", "Open Dev cluster"}
	if !slices.Equal(labelsOf(got), want) {
		t.Fatalf(":ctx: %q, want %q", labelsOf(got), want)
	}
	if !got[0].Own || string(got[0].Params) != "{}" || got[0].Shortcut != "Cmd+P" || got[0].Category != "Cluster" {
		t.Errorf("the list's entry %+v", got[0])
	}
	if got[2].Command != connectCommandID || string(got[2].Params) != `{"context":"prod-eu [prod.yaml]"}` || got[2].Category != "prod-eu" {
		t.Errorf("a cluster's entry %+v", got[2])
	}
	if got := r.JumpEntries(ix, ":ctx", nil); got[0].Label != "Open kind-cogpit" {
		t.Errorf("without the window's list: %q", labelsOf(got))
	}
	for _, text := range []string{":context prod", ":contexts @prod", ":ctx PROD"} {
		if got := r.JumpEntries(ix, text, runs); !slices.Equal(labelsOf(got), []string{"Open prod-eu", "Open prod-us"}) {
			t.Errorf("%s: %q", text, labelsOf(got))
		}
	}
	if got := r.JumpEntries(ix, ":ctx cluster", runs); !slices.Equal(labelsOf(got), []string{"Open Dev cluster"}) {
		t.Errorf("by the shown name: %q", labelsOf(got))
	}
	if got := r.JumpEntries(ix, ":ctx nope", runs); len(got) != 1 || got[0].Reason != "no cluster “nope”" {
		t.Errorf(":ctx nope: %+v", got)
	}
}

// A name all views share and every cluster: the list stops at jumpMaxEntries.
func TestJumpEntriesCapped(t *testing.T) {
	r, ix := jumpRegistry(t), jumpIndex()
	for i := range 100 {
		ix.Clusters = append(ix.Clusters, JumpCluster{Context: fmt.Sprintf("c%d", i), Name: fmt.Sprintf("c%d", i)})
	}
	if got := r.JumpEntries(ix, ": @", nil); len(got) != jumpMaxEntries {
		t.Errorf("%d entries, want %d", len(got), jumpMaxEntries)
	}
}

// The registry without ui.jump (a test fake's) makes no entries.
func TestJumpEntriesNeedTheCommand(t *testing.T) {
	if got := NewRegistry().JumpEntries(jumpIndex(), ":po", nil); got != nil {
		t.Errorf("entries without ui.jump: %+v", got)
	}
}

// The names come from the cluster: a built-in kind's from discovery (Backend.Names), a custom
// kind's from its definition, which a change of the definition follows; Cogpit's own kinds have
// none of a resource they read (Helm releases read Secrets), only their aliases.
func TestJumpNamesFromTheCluster(t *testing.T) {
	src := testkit.NewMemorySource()
	crd := orderCRD()
	names := crd.Object["spec"].(map[string]any)["names"].(map[string]any)
	names["singular"], names["shortNames"] = "order", []any{"ord"}
	src.Set(crdsGVR, crd)
	s := New(Options{Connector: &namedConnector{crdConnector{src: src}}, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	connectTest(t, s)
	b := s.Backend()
	waitFor(t, "the Order kind", func() bool { return hasKind(s, orderKey) })
	for word, want := range map[string]string{"ord": orderKey, "order": orderKey, "po": "pods", "pod": "pods", "Pods": "pods", "pulses": ClusterOverview} {
		if v, ok := ResolveJumpView(b.JumpViews(), word); !ok || v.View != want {
			t.Errorf("%q: %+v %v, want %s", word, v, ok, want)
		}
	}
	if _, ok := ResolveJumpView(b.JumpViews(), "secret-ish"); ok {
		t.Error("a name nothing has resolved")
	}
	p, _ := b.Profile(orderKey)
	if n := b.KindNames(p); n.Singular != "order" || !slices.Equal(n.ShortNames, []string{"ord"}) {
		t.Errorf("the Order kind's names %+v", n)
	}
	changed := crd.DeepCopy()
	changed.Object["spec"].(map[string]any)["names"].(map[string]any)["shortNames"] = []any{"ordr"}
	src.Set(crdsGVR, changed)
	waitFor(t, "the new short name", func() bool { _, ok := ResolveJumpView(b.JumpViews(), "ordr"); return ok })
	if v, ok := ResolveJumpView(b.JumpViews(), "ord"); ok {
		t.Errorf("the old short name still finds %+v", v)
	}
	releases := helm.NewReleaseProfile()
	if n := b.KindNames(releases); n.Singular != "" || len(n.ShortNames) != 0 {
		t.Errorf("Helm releases took the Secrets' names: %+v", n)
	}
	if v, err := b.ResolveJump(JumpParams{View: "po", Namespace: "shop"}); err != nil || v.View != "pods" {
		t.Errorf("ResolveJump: %+v, %v", v, err)
	}
	if _, err := b.ResolveJump(JumpParams{View: "nope"}); !errors.Is(err, ErrUnknownKind) {
		t.Errorf("an unknown name: %v", err)
	}
}

// namedConnector is crdConnector with the built-in kinds' names discovery gives.
type namedConnector struct{ crdConnector }

func (c *namedConnector) Connect(ctx context.Context, name string) (*Backend, error) {
	b, err := c.crdConnector.Connect(ctx, name)
	if err == nil {
		b.Names = map[schema.GroupResource]model.KindNames{{Resource: "pods"}: {Singular: "pod", ShortNames: []string{"po"}}}
	}
	return b, err
}
