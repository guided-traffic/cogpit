package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

var configMapsGVR = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}

// denyListing is an Access that denies listing the named resources ("secrets") everywhere and
// allows the rest.
type denyListing map[string]bool

func (d denyListing) Allowed(_ string, v Verb) (allowed, known bool) {
	return v.Verb != verbList || !d[v.Resource], true
}

// blockingConnector is fakeConnector with an Access on every backend.
type blockingConnector struct {
	*fakeConnector
	access Access
}

func (c *blockingConnector) Connect(ctx context.Context, name string) (*Backend, error) {
	b, err := c.fakeConnector.Connect(ctx, name)
	if err == nil {
		b.Access = c.access
	}
	return b, err
}

// stuckNamespace is walk-fin as the namespace controller leaves it on kind v1.36.1 (2026-10-19),
// naming the kinds remaining.
func stuckNamespace(remaining string, extra ...map[string]any) *unstructured.Unstructured {
	conds := []any{
		map[string]any{"type": kinds.CondDiscoveryFailure, "status": "False", "message": "All resources successfully discovered"},
	}
	if remaining != "" {
		conds = append(conds, map[string]any{"type": kinds.CondContentRemaining, "status": "True", "message": "Some resources are remaining: " + remaining})
	} else {
		conds = append(conds, map[string]any{"type": kinds.CondContentRemaining, "status": "False", "message": "All content successfully removed"})
	}
	for _, e := range extra {
		conds = append(conds, e)
	}
	return testkit.Object("v1", "Namespace", "", "walk-fin", map[string]any{
		"metadata": map[string]any{"deletionTimestamp": "2026-10-19T10:00:00Z"},
		"spec":     map[string]any{"finalizers": []any{"kubernetes"}},
		"status":   map[string]any{"phase": "Terminating", "conditions": conds},
	})
}

// held is an object being deleted that finalizers hold.
func held(apiVersion, kind, ns, name string, fins ...string) *unstructured.Unstructured {
	list := make([]any, len(fins))
	for i, f := range fins {
		list[i] = f
	}
	return testkit.Object(apiVersion, kind, ns, name, map[string]any{
		"metadata": map[string]any{"deletionTimestamp": "2026-10-19T10:00:00Z", "finalizers": list},
		"data":     map[string]any{"k": "v"},
	})
}

// blockingSection returns the drawer model's Blocking Deletion section, nil without one.
func blockingSection(m *DetailModel) *model.DetailSection {
	for i, sec := range m.Sections {
		if sec.Title == BlockingTitle {
			return &m.Sections[i]
		}
	}
	return nil
}

func sectionText(sec *model.DetailSection) string {
	if sec == nil {
		return "<none>"
	}
	var b strings.Builder
	for _, it := range sec.Items {
		b.WriteString(it.Name + ": " + it.Value + "\n")
	}
	return b.String()
}

// waitBlocking waits until the drawer's model of ref has a Blocking Deletion section for which
// cond holds, and returns it.
func waitBlocking(t *testing.T, s *Session, ref model.ObjectRef, cond func(*model.DetailSection) bool) *model.DetailSection {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var sec *model.DetailSection
	if !testkit.WaitFor(ctx, func() bool {
		m, err := s.Detail(ref)
		if err != nil {
			return false
		}
		sec = blockingSection(m)
		return sec != nil && cond(sec)
	}) {
		t.Fatalf("no matching Blocking Deletion section; last:\n%s", sectionText(sec))
	}
	return sec
}

func blockingSession(t *testing.T, src *testkit.MemorySource, access Access) *Session {
	t.Helper()
	fc := &fakeConnector{src: src, profiles: []*model.KindProfile{
		builtinProfile(t, "namespaces"), builtinProfile(t, "configmaps"), builtinProfile(t, "secrets"), builtinProfile(t, "services"), testEvents,
	}}
	s := New(Options{Connector: &blockingConnector{fakeConnector: fc, access: access}, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	connectTest(t, s)
	return s
}

// TestTerminatingNamespaceListsWhatBlocksIt (WF43): the drawer of a Terminating namespace lists,
// per kind its condition names, the objects there that finalizers hold, each linked with its
// finalizers and Remove Finalizers; a kind the user may not list there and one Cogpit has no
// profile of as counted rows; an object whose finalizers go vanishes through the watch; with
// nothing left, the namespace finishes by itself. The kinds are read metadata-only in the
// namespace alone, and released when the drawer closes.
func TestTerminatingNamespaceListsWhatBlocksIt(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.NamespacesGVR, stuckNamespace("configmaps. has 2 resource instances, secrets. has 1 resource instances, widgets.example.com has 3 resource instances"))
	src.Set(configMapsGVR, held("v1", "ConfigMap", "walk-fin", "walk-cm", "example.com/x", "example.com/y"))
	src.Set(configMapsGVR, testkit.Object("v1", "ConfigMap", "walk-fin", "walk-plain", nil))
	src.Set(configMapsGVR, held("v1", "ConfigMap", "elsewhere", "other-cm", "example.com/x")) // another namespace
	src.Set(testkit.SecretsGVR, held("v1", "Secret", "walk-fin", "walk-secret", "example.com/x"))
	s := blockingSession(t, src, denyListing{"secrets": true})

	ref := model.ObjectRef{Context: "test", Kind: "namespaces", Name: "walk-fin"}
	s.UpdateUI(func(u *UIState) { u.View, u.Drawer = "namespaces", &ref })
	sec := waitBlocking(t, s, ref, func(sec *model.DetailSection) bool {
		return strings.Contains(sectionText(sec), "Config Map: walk-cm")
	})
	text := sectionText(sec)
	for _, want := range []string{
		"Config Map: walk-cm\nFinalizers: example.com/x\nexample.com/y\n",
		"Config Maps: 1 more without finalizers, not gone yet\n",
		"Secrets: 1 remaining; you may not list secrets in walk-fin\n",
		"widgets.example.com: 3 remaining; Cogpit has no view of this kind (its definition is not listed)\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
	if strings.Contains(text, "other-cm") || strings.Contains(text, "Nothing blocks") {
		t.Errorf("another namespace's object, or nothing blocks:\n%s", text)
	}
	it := sec.Items[0]
	want := model.ObjectRef{Context: "test", Kind: "configmaps", Namespace: "walk-fin", Name: "walk-cm"}
	if it.Link == nil || *it.Link != want || len(it.Actions) != 1 || it.Actions[0].Command != RemoveFinalizersCommand {
		t.Fatalf("object row %+v", it)
	}
	var p RemoveFinalizersParams
	if err := json.Unmarshal(it.Actions[0].Params, &p); err != nil || p.Ref != want || strings.Join(p.Finalizers, ",") != "example.com/x,example.com/y" {
		t.Errorf("action params %s (%v)", it.Actions[0].Params, err)
	}
	if fins := sec.Items[1]; len(fins.Chips) != 2 {
		t.Errorf("finalizers as chips: %+v", fins)
	}

	// The reads: Config Maps metadata-only in walk-fin, no Secrets (denied), nothing cluster-wide.
	b := s.Backend()
	cmScope := cache.Scope{GVR: configMapsGVR, Namespace: "walk-fin", MetadataOnly: true}
	found := false
	for _, st := range b.Cache.Stats() {
		found = found || (st.Scope == cmScope && st.Handles > 0)
		if st.Scope.GVR == testkit.SecretsGVR || (st.Scope.GVR == configMapsGVR && st.Scope != cmScope) {
			t.Errorf("a read the list must not make: %s", st.Scope)
		}
	}
	if !found {
		t.Error("the Config Maps of walk-fin are not held metadata-only")
	}

	// The finalizers go: the object vanishes from the list by itself; the plain one is deleted
	// too, and the condition still lags: nothing left in the cache, so nothing blocks.
	src.Delete(configMapsGVR, "walk-fin", "walk-cm")
	src.Delete(configMapsGVR, "walk-fin", "walk-plain")
	src.Set(testkit.NamespacesGVR, stuckNamespace("configmaps. has 2 resource instances"))
	sec = waitBlocking(t, s, ref, func(sec *model.DetailSection) bool { return strings.Contains(sectionText(sec), "Nothing blocks") })
	if text := sectionText(sec); len(sec.Items) != 1 || !strings.Contains(text, "no second Delete is needed") {
		t.Errorf("after the last object:\n%s", text)
	}
	// The kinds the condition no longer names are released with the drawer.
	s.UpdateUI(func(u *UIState) { u.Drawer = nil })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		for _, st := range b.Cache.Stats() {
			if st.Scope == cmScope && st.Handles > 0 {
				return false
			}
		}
		return true
	}) {
		t.Error("the drawer's read of walk-fin's Config Maps was not released")
	}
}

// TestBlockingListForAgents: object_get (DetailWait) of a Terminating namespace waits for its
// kinds' lists and returns the section with the rows' Remove Finalizers params; an active
// namespace has none.
func TestBlockingListForAgents(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.NamespacesGVR, stuckNamespace("configmaps. has 1 resource instances"))
	src.Set(testkit.NamespacesGVR, testkit.Object("v1", "Namespace", "", "live", map[string]any{"status": map[string]any{"phase": "Active"}}))
	src.Set(configMapsGVR, held("v1", "ConfigMap", "walk-fin", "walk-cm", "example.com/x"))
	s := blockingSession(t, src, nil)
	m, err := s.DetailWait(context.Background(), model.ObjectRef{Kind: "namespaces", Name: "walk-fin"})
	if err != nil {
		t.Fatal(err)
	}
	sec := blockingSection(m)
	if sec == nil || len(sec.Items) != 2 || sec.Items[0].Value != "walk-cm" || len(sec.Items[0].Actions) != 1 {
		t.Fatalf("agent's section:\n%s", sectionText(sec))
	}
	m, err = s.DetailWait(context.Background(), model.ObjectRef{Kind: "namespaces", Name: "live"})
	if err != nil {
		t.Fatal(err)
	}
	if blockingSection(m) != nil {
		t.Error("an active namespace has a Blocking Deletion section")
	}
}

// TestBlockingListDiscoveryFailure: NamespaceDeletionDiscoveryFailure names the API services that
// are not available, each with its Service linked; a user who may not list them reads why.
func TestBlockingListDiscoveryFailure(t *testing.T) {
	failure := map[string]any{"type": kinds.CondDiscoveryFailure, "status": "True",
		"message": "Discovery failed for some groups, 1 failing: unable to retrieve the complete list of server APIs: metrics.k8s.io/v1beta1: stale GroupVersion discovery: metrics.k8s.io/v1beta1"}
	ns := stuckNamespace("")
	ns.Object["status"].(map[string]any)["conditions"] = []any{failure}
	src := testkit.NewMemorySource()
	src.Set(testkit.NamespacesGVR, ns)
	apiSvc := func(name, status string) *unstructured.Unstructured {
		return testkit.Object("apiregistration.k8s.io/v1", "APIService", "", name, map[string]any{
			"spec":   map[string]any{"service": map[string]any{"namespace": "kube-system", "name": "metrics-server"}},
			"status": map[string]any{"conditions": []any{map[string]any{"type": "Available", "status": status, "reason": "FailedDiscoveryCheck", "message": "failing or missing response"}}},
		})
	}
	src.Set(apiServicesGVR, apiSvc("v1beta1.metrics.k8s.io", "False"))
	src.Set(apiServicesGVR, apiSvc("v1.apps", "True"))
	s := blockingSession(t, src, nil)
	m, err := s.DetailWait(context.Background(), model.ObjectRef{Kind: "namespaces", Name: "walk-fin"})
	if err != nil {
		t.Fatal(err)
	}
	sec := blockingSection(m)
	text := sectionText(sec)
	if !strings.Contains(text, "API Service: v1beta1.metrics.k8s.io not available: FailedDiscoveryCheck: failing or missing response\nServed by: kube-system/metrics-server\n") ||
		strings.Contains(text, "v1.apps") || strings.Contains(text, "Nothing blocks") {
		t.Errorf("discovery failure:\n%s", text)
	}
	if l := sec.Items[1].Link; l == nil || l.Kind != "services" || l.Name != "metrics-server" {
		t.Errorf("served by %+v", sec.Items[1])
	}

	denied := blockingSession(t, src, denyListing{"apiservices": true})
	m, err = denied.DetailWait(context.Background(), model.ObjectRef{Kind: "namespaces", Name: "walk-fin"})
	if err != nil {
		t.Fatal(err)
	}
	if text := sectionText(blockingSection(m)); !strings.Contains(text, "you may not list API services") {
		t.Errorf("denied:\n%s", text)
	}
}
