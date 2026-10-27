package commands_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

func refNames(refs []model.ObjectRef) []string {
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.Key()
	}
	return out
}

// TestSelectModes: ui.select replaces, adds, toggles and removes objects of the open view, takes
// every row the view shows with all, and leaves the drawer to mode replace.
func TestSelectModes(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	agent := engine.OriginExternalAgent
	h.must(t, agent, "ui.openView", map[string]any{"view": "pods"}, nil)
	waitTable(t, h.s, "pods", func(s *engine.TableSnapshot) bool { return len(s.Rows) == 4 })
	pod := func(ns, name string) model.ObjectRef { return model.ObjectRef{Kind: "pods", Namespace: ns, Name: name} }
	sel := func() []string { return refNames(h.s.UIState().Selected) }

	h.must(t, agent, "ui.select", map[string]any{"refs": []model.ObjectRef{pod("shop", "web-0")}}, nil)
	if u := h.s.UIState(); !slices.Equal(sel(), []string{"shop/web-0"}) || u.Drawer == nil || u.Drawer.Key() != "shop/web-0" {
		t.Fatalf("replace: %v, drawer %v", sel(), u.Drawer)
	}
	h.must(t, agent, "ui.select", map[string]any{"refs": []model.ObjectRef{pod("infra", "web-1"), pod("shop", "web-0")}, "mode": "add"}, nil)
	if !slices.Equal(sel(), []string{"shop/web-0", "infra/web-1"}) {
		t.Fatalf("add: %v", sel())
	}
	h.must(t, agent, "ui.select", map[string]any{"refs": []model.ObjectRef{pod("shop", "web-0"), pod("shop", "web-1")}, "mode": "toggle"}, nil)
	if u := h.s.UIState(); !slices.Equal(sel(), []string{"infra/web-1", "shop/web-1"}) || u.Drawer == nil || u.Drawer.Key() != "shop/web-0" {
		t.Fatalf("toggle: %v, drawer %v (the drawer stays)", sel(), u.Drawer)
	}
	h.must(t, agent, "ui.select", map[string]any{"refs": []model.ObjectRef{pod("infra", "web-1")}, "mode": "remove"}, nil)
	if !slices.Equal(sel(), []string{"shop/web-1"}) {
		t.Fatalf("remove: %v", sel())
	}

	// all: the rows the view shows, after the namespace filter and the search.
	h.must(t, agent, "ui.setNamespace", map[string]any{"namespaces": []string{"infra"}}, nil)
	h.must(t, agent, "ui.select", map[string]any{"all": true, "noDrawer": true}, nil)
	if u := h.s.UIState(); !slices.Equal(sel(), []string{"infra/web-0", "infra/web-1"}) || u.Drawer == nil {
		t.Fatalf("all in infra: %v, drawer %v", sel(), u.Drawer)
	}
	h.must(t, agent, "ui.setNamespace", map[string]any{"namespaces": []string{}}, nil)
	h.must(t, agent, "ui.setFilter", map[string]any{"filter": "web-0"}, nil)
	h.must(t, agent, "ui.select", map[string]any{"all": true, "mode": "add"}, nil)
	if !slices.Equal(sel(), []string{"infra/web-0", "infra/web-1", "shop/web-0"}) {
		t.Fatalf("add all matching web-0: %v", sel())
	}
	h.must(t, agent, "ui.select", map[string]any{"refs": []model.ObjectRef{}, "noDrawer": true}, nil)
	if u := h.s.UIState(); len(u.Selected) != 0 || u.Drawer == nil {
		t.Fatalf("clear without the drawer: %v, drawer %v", sel(), u.Drawer)
	}

	for _, bad := range []struct {
		params map[string]any
		want   string
	}{
		{map[string]any{"mode": "extend"}, "unknown selection mode"},
		{map[string]any{"all": true, "refs": []model.ObjectRef{pod("shop", "web-0")}}, "leave refs out"},
		{map[string]any{"refs": []model.ObjectRef{checkout}}, "not of the open view's kind"},
	} {
		if err := h.run(t, agent, "ui.select", bad.params, nil); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("%v: %v, want %q", bad.params, err, bad.want)
		}
	}
}

// TestSelectOpenKeepsTheCheckedRows: the owner's decision A73: a plain click on a row (ui.select's
// mode open) opens its drawer and keeps the checked rows checked; the clicked row neither joins nor
// leaves the selection; what an agent reads in ui_state follows. Open takes exactly one object.
func TestSelectOpenKeepsTheCheckedRows(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	gui := engine.OriginGUI
	h.must(t, gui, "ui.openView", map[string]any{"view": "pods"}, nil)
	waitTable(t, h.s, "pods", func(s *engine.TableSnapshot) bool { return len(s.Rows) == 4 })
	pod := func(ns, name string) model.ObjectRef { return model.ObjectRef{Kind: "pods", Namespace: ns, Name: name} }
	sel := func() []string { return refNames(h.s.UIState().Selected) }

	// Two rows checked with their checkboxes, no drawer.
	h.must(t, gui, "ui.select", map[string]any{"refs": []model.ObjectRef{pod("shop", "web-0"), pod("infra", "web-1")}, "mode": "toggle", "noDrawer": true}, nil)
	if u := h.s.UIState(); !slices.Equal(sel(), []string{"shop/web-0", "infra/web-1"}) || u.Drawer != nil {
		t.Fatalf("checked: %v, drawer %v", sel(), u.Drawer)
	}

	// A click on an unchecked row: its drawer, the checked rows stay, the row is not checked.
	var st engine.UIState
	h.must(t, gui, "ui.select", map[string]any{"refs": []model.ObjectRef{pod("shop", "web-1")}, "mode": "open"}, &st)
	if !slices.Equal(refNames(st.Selected), []string{"shop/web-0", "infra/web-1"}) || st.Drawer == nil || st.Drawer.Key() != "shop/web-1" {
		t.Fatalf("after a click on shop/web-1: selected %v, drawer %v", refNames(st.Selected), st.Drawer)
	}
	// A click on a checked row: its drawer, it stays checked.
	h.must(t, gui, "ui.select", map[string]any{"refs": []model.ObjectRef{pod("infra", "web-1")}, "mode": "open"}, nil)
	if u := h.s.UIState(); !slices.Equal(sel(), []string{"shop/web-0", "infra/web-1"}) || u.Drawer == nil || u.Drawer.Key() != "infra/web-1" {
		t.Fatalf("after a click on infra/web-1: %v, drawer %v", sel(), u.Drawer)
	}
	// With nothing checked a click checks nothing: the drawer alone.
	h.must(t, gui, "ui.select", map[string]any{"refs": []model.ObjectRef{}, "noDrawer": true}, nil)
	h.must(t, gui, "ui.select", map[string]any{"refs": []model.ObjectRef{pod("infra", "web-0")}, "mode": "open"}, nil)
	if u := h.s.UIState(); len(u.Selected) != 0 || u.Drawer == nil || u.Drawer.Key() != "infra/web-0" {
		t.Fatalf("a click without checked rows: %v, drawer %v", sel(), u.Drawer)
	}

	for _, bad := range []map[string]any{
		{"mode": "open", "refs": []model.ObjectRef{}},
		{"mode": "open", "refs": []model.ObjectRef{pod("shop", "web-0"), pod("shop", "web-1")}},
		{"mode": "open", "all": true},
		{"mode": "open", "refs": []model.ObjectRef{pod("shop", "web-0")}, "noDrawer": true},
	} {
		if err := h.run(t, engine.OriginExternalAgent, "ui.select", bad, nil); err == nil || !strings.Contains(err.Error(), "exactly one") {
			t.Errorf("%v: %v, want the one-object refusal", bad, err)
		}
	}
	if u := h.s.UIState(); len(u.Selected) != 0 || u.Drawer == nil || u.Drawer.Key() != "infra/web-0" {
		t.Fatalf("a refused open changed the state: %v, drawer %v", sel(), u.Drawer)
	}
}

// recorder is a confirmer that answers yes (or no) and keeps every request.
type recorder struct {
	mu     sync.Mutex
	reqs   []engine.ConfirmRequest
	answer bool
	during func(engine.ConfirmRequest) // runs while the dialog would be open
}

func (r *recorder) Confirm(_ context.Context, req engine.ConfirmRequest) (bool, error) {
	r.mu.Lock()
	r.reqs = append(r.reqs, req)
	during := r.during
	r.mu.Unlock()
	if during != nil {
		during(req)
	}
	return r.answer, nil
}

func (r *recorder) requests() []engine.ConfirmRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.reqs)
}

// threeServices puts three Services into the harness's cluster and waits until the cache holds
// them.
func threeServices(t *testing.T, h *harness) []model.ObjectRef {
	t.Helper()
	var refs []model.ObjectRef
	for i, name := range []string{"a", "b", "c"} {
		h.src.Set(testkit.ServicesGVR, testkit.Service("shop", name, "10.96.7."+string(rune('1'+i))))
		refs = append(refs, model.ObjectRef{Kind: "services", Namespace: "shop", Name: name})
	}
	for _, r := range refs {
		cachedRV(t, h, r, "")
	}
	return refs
}

// TestDeleteManyOneConfirmation: Delete of a selection asks once, naming every object, its
// namespace and the context with the UIDs the cache held; each object is deleted with its UID as
// the precondition; the deleted objects leave the selection.
func TestDeleteManyOneConfirmation(t *testing.T) {
	conf := &recorder{answer: true}
	h, srv := editHarness(t, engine.Options{Confirmer: conf})
	refs := threeServices(t, h)
	h.must(t, engine.OriginGUI, "ui.openView", map[string]any{"view": "services"}, nil)
	h.must(t, engine.OriginGUI, "ui.select", map[string]any{"refs": refs, "mode": "add"}, nil)

	var res commands.DeleteManyResult
	h.must(t, engine.OriginGUI, "object.deleteMany", map[string]any{"refs": refs}, &res)
	reqs := conf.requests()
	if len(reqs) != 1 {
		t.Fatalf("%d confirmations, want one", len(reqs))
	}
	req := reqs[0]
	if !slices.Equal(refNames(req.Objects), []string{"shop/a", "shop/b", "shop/c"}) || !slices.Equal(req.Namespaces, []string{"shop"}) ||
		req.Context != "test" || req.Danger != engine.DangerDestructive ||
		!slices.Equal(req.UIDs, []string{"uid-shop-a", "uid-shop-b", "uid-shop-c"}) {
		t.Errorf("confirmation %+v", req)
	}
	if msg := req.Message(); msg != "Delete 3 objects: Service a, Service b, Service c in namespace shop (context test)?" {
		t.Errorf("message %q", msg)
	}
	srv.mu.Lock()
	deletes := slices.Clone(srv.deletes)
	srv.mu.Unlock()
	if !slices.Equal(deletes, []string{"shop/a uid-shop-a", "shop/b uid-shop-b", "shop/c uid-shop-c"}) {
		t.Errorf("deletes %v (each with its confirmed UID)", deletes)
	}
	if !slices.Equal(refNames(res.Deleted), []string{"shop/a", "shop/b", "shop/c"}) {
		t.Errorf("result %+v", res)
	}
	if u := h.s.UIState(); len(u.Selected) != 0 {
		t.Errorf("the deleted objects are still selected: %v", u.Selected)
	}
}

// TestDeleteManyPartialFailure: an object created again under its name while the dialog was open
// is not deleted (its UID is not the confirmed one); the others are, each once; the error names
// both and nothing is retried.
func TestDeleteManyPartialFailure(t *testing.T) {
	conf := &recorder{answer: true}
	h, srv := editHarness(t, engine.Options{Confirmer: conf})
	refs := threeServices(t, h)
	conf.during = func(engine.ConfirmRequest) {
		again := testkit.Service("shop", "b", "10.96.7.9")
		again.SetUID("uid-shop-b-again")
		h.src.Set(testkit.ServicesGVR, again)
	}
	err := h.run(t, engine.OriginGUI, "object.deleteMany", map[string]any{"refs": refs}, nil)
	var de *commands.DeleteManyError
	if !errors.As(err, &de) {
		t.Fatalf("partial failure: %v", err)
	}
	if !slices.Equal(refNames(de.Deleted), []string{"shop/a", "shop/c"}) || len(de.Failed) != 1 || de.Failed[0].Ref.Name != "b" ||
		!strings.Contains(de.Failed[0].Error, "uid-shop-b") {
		t.Errorf("outcome %+v", de)
	}
	if msg := err.Error(); !strings.Contains(msg, "deleted 2 of 3 objects") || !strings.Contains(msg, "not deleted: services/shop/b") || !strings.Contains(msg, "Nothing was retried") {
		t.Errorf("error %q", msg)
	}
	srv.mu.Lock()
	deletes := slices.Clone(srv.deletes)
	srv.mu.Unlock()
	if !slices.Equal(deletes, []string{"shop/a uid-shop-a", "shop/b uid-shop-b", "shop/c uid-shop-c"}) {
		t.Errorf("deletes %v (one attempt each, the replaced b with the confirmed UID)", deletes)
	}
	if _, err := h.s.Lookup(refs[1]); err != nil {
		t.Errorf("the replacement of b is gone: %v", err)
	}
}

// TestDeleteManyNeedsTheAgentsAcceptance: an agent's Delete of a selection is always confirmed
// (D-2), even with agent writes allowed for the context; declined, nothing is deleted; the audit
// log lists every object.
func TestDeleteManyNeedsTheAgentsAcceptance(t *testing.T) {
	conf := &recorder{answer: false}
	audit := &syncBuffer{}
	h, srv := editHarness(t, engine.Options{Confirmer: conf, Audit: audit})
	refs := threeServices(t, h)
	h.s.SetAgentWrites("test", true)
	err := h.run(t, engine.OriginExternalAgent, "object.deleteMany", map[string]any{"refs": refs}, nil)
	if !errors.Is(err, engine.ErrDeclined) {
		t.Fatalf("declined: %v", err)
	}
	if reqs := conf.requests(); len(reqs) != 1 || reqs[0].Origin != engine.OriginExternalAgent || len(reqs[0].Objects) != 3 ||
		!strings.HasPrefix(reqs[0].Message(), "An agent asks to run: Delete 3 objects: ") {
		t.Errorf("confirmations %+v", reqs)
	}
	srv.mu.Lock()
	n := len(srv.deletes)
	srv.mu.Unlock()
	if n != 0 {
		t.Errorf("%d deletes after the user declined", n)
	}
	lines := audit.Lines()
	if len(lines) == 0 || lines[len(lines)-1].Command != "object.deleteMany" || lines[len(lines)-1].OK {
		t.Fatalf("audit %+v", lines)
	}
	for _, name := range []string{`"name":"a"`, `"name":"b"`, `"name":"c"`} {
		if p := string(lines[len(lines)-1].Params); !strings.Contains(p, name) {
			t.Errorf("the audit line does not name %s: %s", name, p)
		}
	}
}

// TestDeleteManyRefusals: no objects, too many, a Service not in the cache and a denied access
// review disable it with the reason.
func TestDeleteManyRefusals(t *testing.T) {
	h, _ := editHarness(t, engine.Options{Confirmer: &recorder{answer: true}})
	refs := threeServices(t, h)
	many := make([]model.ObjectRef, engine.MaxSelection+1)
	for i := range many {
		many[i] = refs[0]
	}
	cmd, _ := h.s.Registry().Get("object.deleteMany")
	for _, tc := range []struct {
		refs []model.ObjectRef
		want string
	}{
		{[]model.ObjectRef{}, "no objects are selected"},
		{many, "at most 500 objects at once, 501 are selected"},
		{append(slices.Clone(refs), model.ObjectRef{Kind: "services", Namespace: "shop", Name: "gone"}), "services/shop/gone"},
	} {
		if err := cmd.Enabled(h.s, engine.SelectionParams(tc.refs)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%d refs: %v, want %q", len(tc.refs), err, tc.want)
		}
	}
	if err := cmd.Enabled(h.s, engine.SelectionParams(refs)); err != nil {
		t.Errorf("three cached Services: %v", err)
	}
	h.access.set(denyAccess{"delete services shop": true})
	if err := cmd.Enabled(h.s, engine.SelectionParams(refs)); !errors.Is(err, engine.ErrNotPermitted) {
		t.Errorf("denied delete in shop: %v", err)
	}
}
