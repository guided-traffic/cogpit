package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// selNames is a selection as "<namespace>/<name>" in its order.
func selNames(sel []model.ObjectRef) []string {
	out := make([]string, len(sel))
	for i, r := range sel {
		out[i] = r.Namespace + "/" + r.Name
	}
	return out
}

func wantSelected(t *testing.T, what string, u UIState, want ...string) {
	t.Helper()
	if got := selNames(u.Selected); !slices.Equal(got, want) {
		t.Errorf("%s: selected %q, want %q", what, got, want)
	}
}

// Each view keeps its checked rows (N111, the owner's decision in the audit of 2026-10-16): the
// view left takes them along, the view shown has its own; a view shown again in the tab, by
// opening it or by the tab's history, gets them back. ui.state lists the shown view's only.
func TestSelectionRememberedPerView(t *testing.T) {
	s := tabSession(t)
	pod := model.ObjectRef{Context: "test", Kind: "pods", Namespace: "shop", Name: "web-0"}
	svc := model.ObjectRef{Context: "test", Kind: "services", Namespace: "shop", Name: "checkout"}
	s.UpdateUI(func(u *UIState) { u.Selected = []model.ObjectRef{pod} })

	u := s.OpenTab("services", false)
	wantSelected(t, "services opened", u)
	if len(u.Tabs[0].Selected) != 0 {
		t.Errorf("the tab lists the pods' selection as the services': %v", u.Tabs[0].Selected)
	}
	raw, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("web-0")) {
		t.Errorf("ui.state lists the selection the pods view remembers: %s", raw)
	}

	s.UpdateUI(func(u *UIState) { u.Selected = []model.ObjectRef{svc} })
	wantSelected(t, "pods opened again", s.OpenTab("pods", false), "shop/web-0")
	u, err = s.TabHistory(true)
	if err != nil {
		t.Fatal(err)
	}
	wantSelected(t, "back to services", u, "shop/checkout")
	u, err = s.TabHistory(false)
	if err != nil {
		t.Fatal(err)
	}
	wantSelected(t, "forward to pods", u, "shop/web-0")
	// ui.openView of the view the tab shows keeps its selection (it cleared it before).
	wantSelected(t, "pods opened while shown", s.OpenTab("pods", false), "shop/web-0")

	// A command that changes the view in the tab (ui.navigate) leaves the selection to the tab.
	u = s.UpdateUI(func(u *UIState) { u.View = "services" })
	wantSelected(t, "a command's change to services", u, "shop/checkout")
	u = s.UpdateUI(func(u *UIState) { u.View = "pods" })
	wantSelected(t, "and back to pods", u, "shop/web-0")
}

// The remembered selections are the tab's: a new fixed tab starts with none, though the preview
// tab remembers one for its view, and every tab gets its own back.
func TestSelectionRememberedPerTab(t *testing.T) {
	s := tabSession(t)
	pod := model.ObjectRef{Context: "test", Kind: "pods", Namespace: "shop", Name: "web-0"}
	svc := model.ObjectRef{Context: "test", Kind: "services", Namespace: "shop", Name: "checkout"}
	s.OpenTab("services", false)
	s.UpdateUI(func(u *UIState) { u.Selected = []model.ObjectRef{svc} })
	s.OpenTab("pods", false) // the preview tab remembers the services' selection
	s.UpdateUI(func(u *UIState) { u.Selected = []model.ObjectRef{pod} })

	u := s.OpenTab("services", true)
	wantTabs(t, u, "tab-1 pods@test p", "tab-2 services@test *")
	wantSelected(t, "a new fixed tab", u)
	u, err := s.SelectTab(context.Background(), "tab-1")
	if err != nil {
		t.Fatal(err)
	}
	wantSelected(t, "the preview tab again", u, "shop/web-0")
	u, err = s.TabHistory(true)
	if err != nil {
		t.Fatal(err)
	}
	wantSelected(t, "the preview tab's services", u, "shop/checkout")
	if _, err := s.CloseTab("tab-1"); err != nil {
		t.Fatal(err)
	}
	wantSelected(t, "the fixed tab after the preview tab closed", s.UIState())
}

// A remembered selection shown again drops the objects the cache no longer holds: the selection
// bar's Delete never names an object deleted while its view was not shown.
func TestRememberedSelectionDropsGoneObjects(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.PodsGVR, testkit.Pod("shop", "web-0"))
	src.Set(testkit.PodsGVR, testkit.Pod("shop", "web-1"))
	src.Set(testkit.ServicesGVR, testkit.Service("shop", "checkout", "10.96.0.1"))
	s, _ := newTestSession(t, src, 10*time.Millisecond)
	connectTest(t, s)
	waitTable(t, s, "pods", func(ts *TableSnapshot) bool { return ts.Synced && len(ts.Rows) == 2 })
	web0 := model.ObjectRef{Context: "test", Kind: "pods", Namespace: "shop", Name: "web-0"}
	web1 := model.ObjectRef{Context: "test", Kind: "pods", Namespace: "shop", Name: "web-1"}
	s.UpdateUI(func(u *UIState) { u.Selected = []model.ObjectRef{web0, web1} })
	s.OpenTab("services", false)

	src.Delete(testkit.PodsGVR, "shop", "web-0")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool { _, err := s.Lookup(web0); return errors.Is(err, ErrNotFound) }) {
		t.Fatal("the cache still holds shop/web-0")
	}
	wantSelected(t, "pods shown again", s.OpenTab("pods", false), "shop/web-1")
	wantSelected(t, "ui.state", s.UIState(), "shop/web-1")
}

// Every connected cluster has a preview tab of its own (D-117; D-69): each keeps its cluster's
// selection, and a cluster disconnected takes its tab and selections with it.
func TestSelectionRememberedPerCluster(t *testing.T) {
	s := tabSession(t)
	ctx := context.Background()
	pod := model.ObjectRef{Context: "test", Kind: "pods", Namespace: "shop", Name: "web-0"}
	s.UpdateUI(func(u *UIState) { u.Selected = []model.ObjectRef{pod} })

	if err := s.Connect(ctx, "other"); err != nil {
		t.Fatal(err)
	}
	u := s.UIState()
	wantTabs(t, u, "tab-1 pods@test p", "tab-2 pods@other p *")
	wantSelected(t, "the other cluster's pods", u)
	other := model.ObjectRef{Context: "other", Kind: "pods", Namespace: "shop", Name: "web-0"}
	s.UpdateUI(func(u *UIState) { u.Selected = []model.ObjectRef{other} })

	if err := s.Connect(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	u = s.UIState()
	wantTabs(t, u, "tab-1 pods@test p *", "tab-2 pods@other p")
	if len(u.Selected) != 1 || u.Selected[0] != pod {
		t.Errorf("test's pods shown again: selected %v, want %v", u.Selected, pod)
	}

	if err := s.DisconnectContext("other"); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, "other"); err != nil {
		t.Fatal(err)
	}
	u = s.UIState()
	wantTabs(t, u, "tab-1 pods@test p", "tab-3 pods@other p *")
	wantSelected(t, "other connected anew", u)
}
