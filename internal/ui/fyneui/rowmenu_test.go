package fyneui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Probe commands shaped like the row-menu commands the parallel work registers (node and workload
// actions, Edit): the menus must come out as Lens's once those exist (captures, 2026-10-11).

type probeRef struct {
	Ref model.ObjectRef `json:"ref"`
}

type probeContainer struct {
	Ref       model.ObjectRef `json:"ref"`
	Container string          `json:"container,omitempty"`
}

// probeScale has the real Scale's parameters: replicas optional for agents (a dialog starts from
// the cache), 0 to 65535, the description written for agents (commands/workloads.go).
type probeScale struct {
	Ref      model.ObjectRef `json:"ref"`
	Replicas *uint16         `json:"replicas,omitempty" jsonschema:"the desired number of pods, 0 to 65535 (required to run; a dialog starts from the current count)"`
}

// probeScaleHint is the probe's dialog hint, the real Scale's words.
const probeScaleHint = "The number of pods. Drag up to 30 or type up to 65,535; 0 stops every pod and is confirmed first."

// cordonedNode and suspendedJob are the objects the probes treat as cordoned and suspended.
const (
	cordonedNode = "worker-a-03"
	suspendedJob = "nightly-report"
)

func registerProbes(t testing.TB, f *fakeEngine) {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	run := func(context.Context, *engine.Session, probeRef) (any, error) { return nil, nil }
	runC := func(context.Context, *engine.Session, probeContainer) (any, error) { return nil, nil }
	targets := func(_ *engine.Session, p probeRef) ([]model.ObjectRef, error) { return []model.ObjectRef{p.Ref}, nil }
	notApplicableUnless := func(cond func(model.ObjectRef) bool, why string) func(*engine.Session, probeRef) error {
		return func(_ *engine.Session, p probeRef) error {
			if !cond(p.Ref) {
				return fmt.Errorf("%s: %w", why, engine.ErrNotApplicable)
			}
			return nil
		}
	}
	isCordoned := func(r model.ObjectRef) bool { return r.Name == cordonedNode }
	isSuspended := func(r model.ObjectRef) bool { return r.Name == suspendedJob }
	reason := func(id string) func(*engine.Session, probeRef) error {
		return func(*engine.Session, probeRef) error { return f.reason(id) }
	}
	for _, spec := range []engine.Spec[probeRef]{
		{ID: "node.shell", Label: "Shell", Kinds: []string{"nodes"}, Order: 100, Danger: engine.DangerDestructive, Targets: targets, Run: run},
		{ID: "node.cordon", Label: "Cordon", Kinds: []string{"nodes"}, Order: 200, Danger: engine.DangerMutating, Targets: targets, Run: run,
			Enabled: notApplicableUnless(func(r model.ObjectRef) bool { return !isCordoned(r) }, "already cordoned")},
		{ID: "node.uncordon", Label: "Uncordon", Kinds: []string{"nodes"}, Order: 200, Danger: engine.DangerMutating, Targets: targets, Run: run,
			Enabled: notApplicableUnless(isCordoned, "not cordoned")},
		{ID: "node.drain", Label: "Drain", Kinds: []string{"nodes"}, Order: 300, Danger: engine.DangerDestructive, Targets: targets, Run: run,
			Enabled: reason("node.drain")},
		{ID: "pod.evict", Label: "Evict", Kinds: []string{"pods"}, Order: 300, Danger: engine.DangerDestructive, Targets: targets, Run: run},
		{ID: "cronjob.trigger", Label: "Trigger", Kinds: []string{"cronjobs"}, Order: 100, Danger: engine.DangerMutating, Targets: targets, Run: run},
		{ID: "cronjob.suspend", Label: "Suspend", Kinds: []string{"cronjobs"}, Order: 200, Danger: engine.DangerMutating, Targets: targets, Run: run,
			Enabled: notApplicableUnless(func(r model.ObjectRef) bool { return !isSuspended(r) }, "already suspended")},
		{ID: "cronjob.resume", Label: "Resume", Kinds: []string{"cronjobs"}, Order: 200, Danger: engine.DangerMutating, Targets: targets, Run: run,
			Enabled: notApplicableUnless(isSuspended, "not suspended")},
		{ID: "object.edit", Label: "Edit", Order: engine.OrderEdit, Danger: engine.DangerRead, Run: run},
	} {
		must(engine.Register(f.reg, spec))
	}
	must(engine.Register(f.reg, engine.Spec[probeContainer]{ID: "pod.attach", Label: "Attach Pod", Kinds: []string{"pods"}, Order: 100,
		Danger: engine.DangerRead, Run: runC}))
	must(engine.Register(f.reg, engine.Spec[probeScale]{ID: "deployment.scale", Label: "Scale", Kinds: []string{"deployments"}, Order: 100,
		Danger:   engine.DangerMutating,
		Targets:  func(_ *engine.Session, p probeScale) ([]model.ObjectRef, error) { return []model.ObjectRef{p.Ref}, nil },
		Escalate: func(_ *engine.Session, p probeScale) bool { return p.Replicas != nil && *p.Replicas == 0 },
		Dialog:   map[string]engine.DialogField{"replicas": {Hint: probeScaleHint, SliderMin: 1, SliderMax: 30}},
		Prefill: func(_ *engine.Session, p probeScale) (probeScale, error) {
			if p.Ref.Name == "checkout-api" && p.Replicas == nil {
				n := uint16(3) // what the cache says
				p.Replicas = &n
			}
			return p, nil
		},
		Run: func(context.Context, *engine.Session, probeScale) (any, error) { return nil, nil }}))
}

// cellAt returns the cell on screen of a row and table column, scrolling the row into view. It
// walks the table's rendered cells: the pool keeps cells with the ids they last showed.
func cellAt(t testing.TB, sh *shell, row, col int) *tableCell {
	t.Helper()
	for range 2 {
		for _, c := range findAll[*tableCell](sh.view.table) {
			if c.row == row && c.col == col && c.Visible() {
				return c
			}
		}
		sh.view.scrollTo(row)
		settle(sh.win)
	}
	t.Fatalf("no visible cell at row %d, column %d", row, col)
	return nil
}

// columnOf returns the table column of a column id in the open view.
func columnOf(t testing.TB, sh *shell, id string) int {
	t.Helper()
	for i, ci := range sh.view.cols {
		if sh.view.snap.Columns[ci].ID == id {
			return firstDataColumn + i
		}
	}
	t.Fatalf("no visible column %s", id)
	return -1
}

// openMenu right-clicks a row and waits for its actions menu.
func openMenu(t *testing.T, sh *shell, q *mainQueue, row int) *fyne.Menu {
	t.Helper()
	sh.view.menu, sh.view.menuModel = nil, nil
	test.TapSecondary(cellAt(t, sh, row, 0))
	q.until(t, "the row menu", func() bool { return sh.view.menu != nil && sh.view.menuModel != nil })
	return sh.view.menuModel
}

func labels(m *fyne.Menu) []string {
	out := make([]string, 0, len(m.Items))
	for _, it := range m.Items {
		out = append(out, it.Label)
	}
	return out
}

func item(t *testing.T, m *fyne.Menu, label string) *fyne.MenuItem {
	t.Helper()
	for _, it := range m.Items {
		if it.Label == label || strings.HasPrefix(it.Label, label+" — ") {
			return it
		}
	}
	t.Fatalf("no menu item %q in %q", label, labels(m))
	return nil
}

func settledShell(t *testing.T, f *fakeEngine, view string) (*shell, fyne.Window, *mainQueue) {
	t.Helper()
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the "+view+" view", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil && sh.view.snap.View == view })
	settle(w)
	return sh, w, q
}

// TestRowMenuNodes: Lens's node menu (capture of a cordoned node): Show Details, Shell, Cordon or
// Uncordon, Drain, Edit, Delete. node.shell is not built, so its place is empty. Uncordon takes
// Cordon's place (hidden, not greyed); Drain is greyed with its reason.
func TestRowMenuNodes(t *testing.T) {
	f := kindsFake(t, "nodes")
	registerProbes(t, f)
	f.disabled["node.drain"] = "the demo cluster cannot be changed"
	sh, _, q := settledShell(t, f, "nodes")

	m := openMenu(t, sh, q, rowOf(t, f, "nodes", "worker-a-01"))
	want := []string{"Show Details", "Shell", "Cordon", "Drain — the demo cluster cannot be changed", "Edit", "Delete"}
	if got := labels(m); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("menu %q, want %q", got, want)
	}
	if !item(t, m, "Drain").Disabled || item(t, m, "Cordon").Disabled || item(t, m, "Delete").Disabled {
		t.Error("only Drain should be disabled")
	}

	m = openMenu(t, sh, q, rowOf(t, f, "nodes", cordonedNode))
	want[2] = "Uncordon"
	if got := labels(m); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("cordoned node menu %q, want %q", got, want)
	}
	item(t, m, "Uncordon").Action()
	q.until(t, "node.uncordon", func() bool { return len(f.callsOf("node.uncordon")) == 1 })
	if p := decodeParams[probeRef](t, f.callsOf("node.uncordon")[0]); p.Ref.Name != cordonedNode || p.Ref.Context != "demo" {
		t.Errorf("uncordon params %+v", p)
	}
}

// TestRowMenuCronJobs: Lens's cron job menu: Show Details, Trigger, Suspend or Resume, Edit, Delete.
func TestRowMenuCronJobs(t *testing.T) {
	f := kindsFake(t, "pods")
	registerProbes(t, f)
	sh, _, _ := settledShell(t, f, "pods")
	for _, tt := range []struct{ name, toggle string }{{"backup", "Suspend"}, {suspendedJob, "Resume"}} {
		ref := model.ObjectRef{Context: "demo", Kind: "cronjobs", Namespace: "ops", Name: tt.name}
		got := labels(sh.rowMenu(ref, sh.evaluateActions(ref, nil)))
		want := []string{"Show Details", "Trigger", tt.toggle, "Edit", "Delete"}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("%s: menu %q, want %q", tt.name, got, want)
		}
	}
}

// TestRowMenuPodsContainerSubmenu: the pod menu as Lens's (Show Details, Attach Pod ›, Shell ›,
// Evict, Logs ›, Edit, Delete; Share is not a target); a command with a container opens a submenu
// of the containers, then the init containers.
func TestRowMenuPodsContainerSubmenu(t *testing.T) {
	f := kindsFake(t, "pods")
	registerProbes(t, f)
	sh, _, q := settledShell(t, f, "pods")

	var pod podSpec
	for _, p := range fixturePodSpecs() {
		if len(p.containers) == 2 && len(p.init) == 1 && p.state == podOK {
			pod = p
			break
		}
	}
	if pod.name == "" {
		t.Fatal("no fixture pod with two containers and an init container")
	}
	m := openMenu(t, sh, q, rowOf(t, f, "pods", pod.name))
	want := []string{"Show Details", "Attach Pod", "Shell", "Evict", "Logs", "Edit", "Delete"}
	if got := labels(m); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("pod menu %q, want %q", got, want)
	}
	for _, label := range []string{"Attach Pod", "Shell", "Logs"} {
		sub := item(t, m, label).ChildMenu
		if sub == nil || strings.Join(labels(sub), ",") != "app,envoy,migrate" {
			t.Fatalf("%s submenu %v, want the containers then the init containers", label, sub)
		}
	}
	if item(t, m, "Evict").ChildMenu != nil {
		t.Error("Evict takes no container and opens no submenu")
	}
	// (Logs, not Shell: the fake's shell tab would start a terminal on the test driver.)
	item(t, m, "Logs").ChildMenu.Items[1].Action()
	q.until(t, "pod.logs", func() bool { return len(f.callsOf("pod.logs")) == 1 })
	if p := decodeParams[containerParams](t, f.callsOf("pod.logs")[0]); p.Container != "envoy" || p.Ref.Name != pod.name {
		t.Errorf("logs params %+v", p)
	}

	// Show Details opens the drawer as a click on the row does, keeping the checked rows (A73).
	item(t, m, "Show Details").Action()
	q.until(t, "ui.select", func() bool { return len(f.callsOf("ui.select")) == 1 })
	if p := decodeParams[selectModeParams](t, f.callsOf("ui.select")[0]); len(p.Refs) != 1 || p.Refs[0].Name != pod.name || p.NoDrawer || p.Mode != "open" {
		t.Errorf("show details %+v", p)
	}
}

// TestMenuColumnOpensMenuWithoutSelecting: the ⋮ at the row's end opens the menu; the row is not
// selected.
func TestMenuColumnOpensMenuWithoutSelecting(t *testing.T) {
	f := kindsFake(t, "nodes")
	sh, _, q := settledShell(t, f, "nodes")
	c := cellAt(t, sh, 2, sh.view.menuColumn())
	if c.content.kind != cellMenu || c.iconRes == nil {
		t.Fatalf("the last column is not the ⋮: %+v", c.content)
	}
	test.Tap(c)
	q.until(t, "the row menu", func() bool { return sh.view.menuModel != nil })
	if got := labels(sh.view.menuModel); got[0] != "Show Details" {
		t.Errorf("menu %q", got)
	}
	if n := len(f.callsOf("ui.select")); n != 0 {
		t.Errorf("the ⋮ selected the row (%d ui.select)", n)
	}
}

// TestScaleDialogPrefilled: a command whose schema needs more than the object (Scale's replicas)
// opens a dialog generated from the schema, prefilled from the cache, its field named with the
// command's hint behind a "?" (GW6), a slider beside the count (GW2), and dispatches its values.
func TestScaleDialogPrefilled(t *testing.T) {
	f := kindsFake(t, "deployments")
	registerProbes(t, f)
	sh, w, q := settledShell(t, f, "deployments")
	m := openMenu(t, sh, q, rowOf(t, f, "deployments", "checkout-api"))
	item(t, m, "Scale").Action()
	var entry *widget.Entry
	q.until(t, "the dialog", func() bool {
		top := w.Canvas().Overlays().Top()
		if top == nil {
			return false
		}
		entries := entriesIn(top)
		if len(entries) == 1 {
			entry = entries[0]
		}
		return entry != nil
	})
	if entry.Text != "3" {
		t.Fatalf("replicas prefilled %q, want the cached 3", entry.Text)
	}
	top := w.Canvas().Overlays().Top()
	names := findAll[*fieldName](top)
	sliders := slidersIn(top)
	if len(names) != 1 || names[0].text != "Replicas" || names[0].hint != probeScaleHint || len(sliders) != 1 || sliders[0].Value != 3 {
		t.Fatalf("names %+v, sliders %d", names, len(sliders))
	}
	var confirm *widget.Button
	for _, b := range findAll[*widget.Button](top) {
		if b.Text == "Scale" {
			confirm = b
		}
	}
	if confirm == nil || confirm.Disabled() {
		t.Fatal("no enabled Scale button")
	}
	entry.SetText("x")
	if !confirm.Disabled() {
		t.Error("Scale is armed with a count that is not a number")
	}
	entry.SetText("5")
	if confirm.Disabled() || sliders[0].Value != 5 {
		t.Fatalf("Scale disabled %v, slider %v after typing 5", confirm.Disabled(), sliders[0].Value)
	}
	test.Tap(confirm)
	q.until(t, "deployment.scale", func() bool { return len(f.callsOf("deployment.scale")) == 1 })
	p := decodeParams[probeScale](t, f.callsOf("deployment.scale")[0])
	if p.Replicas == nil || *p.Replicas != 5 || p.Ref.Name != "checkout-api" || p.Ref.Kind != "deployments" {
		t.Errorf("scale params %+v", p)
	}
}

// tapOnText taps a cell on its text, where a link is.
func tapOnText(w fyne.Window, c *tableCell) {
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(c.text).AddXY(2, c.text.Size().Height/2)
	test.TapCanvas(w.Canvas(), pos)
}

// TestLinkClicks: a link in a cell is followed and does not select the row; a Namespace link
// narrows the namespace filter; a link to a kind the cluster does not serve is an error toast.
func TestLinkClicks(t *testing.T) {
	f := kindsFake(t, "pods")
	sh, w, q := settledShell(t, f, "pods")
	row := f.tables["pods"].Rows[0]

	tapOnText(w, cellAt(t, sh, 0, columnOf(t, sh, "node")))
	q.until(t, "ui.navigate", func() bool { return len(f.callsOf("ui.navigate")) == 1 })
	if p := decodeParams[refParams](t, f.callsOf("ui.navigate")[0]); p.Ref != (model.ObjectRef{Context: "demo", Kind: "nodes", Name: "worker-a-01"}) {
		t.Errorf("node link %+v", p.Ref)
	}
	q.until(t, "the nodes view", func() bool { return sh.view.snap != nil && sh.view.snap.View == "nodes" })

	go f.update(func(u *engine.UIState) { u.View, u.Selected, u.Drawer = "pods", nil, nil })
	q.until(t, "the pods view", func() bool { return sh.view.snap != nil && sh.view.snap.View == "pods" })
	settle(w)
	tapOnText(w, cellAt(t, sh, 0, columnOf(t, sh, "namespace")))
	q.until(t, "ui.setNamespace", func() bool { return len(f.callsOf("ui.setNamespace")) == 1 })
	if p := decodeParams[namespacesParams](t, f.callsOf("ui.setNamespace")[0]); len(p.Namespaces) != 1 || p.Namespaces[0] != row.Ref.Namespace {
		t.Errorf("namespace link %+v, want only %s", p, row.Ref.Namespace)
	}

	tapOnText(w, cellAt(t, sh, 0, columnOf(t, sh, "controlledBy")))
	q.until(t, "the owner link", func() bool { return len(f.callsOf("ui.navigate")) == 2 })
	calls := f.callsOf("ui.navigate")
	if p := decodeParams[refParams](t, calls[len(calls)-1]); p.Ref.Kind != "replicasets" || p.Ref.Namespace != row.Ref.Namespace {
		t.Errorf("owner link %+v", p.Ref)
	}
	if n := len(f.callsOf("ui.select")); n != 0 {
		t.Errorf("a link click selected the row (%d ui.select)", n)
	}
	// The navigate's own state first, so that the switch back below comes after it.
	q.until(t, "the owner's view", func() bool { return sh.ui.View == "replicasets" })

	// A link to a kind the cluster does not serve (or Cogpit has no view for) is plain text; one
	// that fails anyway ends in an error toast, not a crash.
	go f.update(func(u *engine.UIState) { u.View, u.Selected, u.Drawer = "pods", nil, nil })
	q.until(t, "the pods view again", func() bool { return sh.view.snap != nil && sh.view.snap.View == "pods" })
	sh.kinds = slices.DeleteFunc(slices.Clone(sh.kinds), func(k kindInfo) bool { return k.Kind == "replicasets" })
	sh.view.table.Refresh()
	settle(w)
	if c := cellAt(t, sh, 0, columnOf(t, sh, "controlledBy")); c.content.link != nil || c.full != "ReplicaSet" {
		t.Errorf("a link to an unserved kind is still a link: %+v", c.content)
	}
	toasts := len(sh.toasts.Objects)
	f.mu.Lock()
	f.kinds = sh.kinds
	f.mu.Unlock()
	sh.followLink(model.ObjectRef{Kind: "replicasets", Namespace: row.Ref.Namespace, Name: "x"}, "demo", false)
	q.until(t, "the error toast", func() bool { return len(sh.toasts.Objects) > toasts })

	// Next to the text, a link cell selects its row as any cell does.
	settle(w)
	c := cellAt(t, sh, 1, columnOf(t, sh, "node"))
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(c).AddXY(c.Size().Width-3, c.Size().Height/2)
	test.TapCanvas(w.Canvas(), pos)
	q.until(t, "ui.select", func() bool { return len(f.callsOf("ui.select")) == 1 })
}

// TestTooltips: a cell whose text is cut off shows the full text after a hover delay, a warning
// icon its reason; leaving the table closes it.
func TestTooltips(t *testing.T) {
	f := kindsFake(t, "events")
	sh, w, q := settledShell(t, f, "events")
	sh.tip.delay = 5 * time.Millisecond

	var cut *tableCell
	for _, c := range findAll[*tableCell](sh.view.table) {
		if c.Visible() && c.truncated && c.col == columnOf(t, sh, "message") {
			cut = c
			break
		}
	}
	if cut == nil {
		t.Fatal("no cut-off message on screen")
	}
	at := func(c *tableCell) fyne.Position {
		return fyne.CurrentApp().Driver().AbsolutePositionForObject(c).AddXY(c.Size().Width/2, c.Size().Height/2)
	}
	test.MoveMouse(w.Canvas(), at(cut))
	q.until(t, "the message tooltip", func() bool { return sh.tip.visibleText() == cut.full })
	if !strings.HasPrefix(cut.full, cut.text.Text[:len(cut.text.Text)-len("…")]) {
		t.Errorf("shown %q is not a prefix of %q", cut.text.Text, cut.full)
	}
	test.MoveMouse(w.Canvas(), fyne.NewPos(20, 600)) // the sidebar
	q.until(t, "the tooltip to close", func() bool { return sh.tip.visibleText() == "" })

	// A cell that fits shows none.
	var fits *tableCell
	for _, c := range findAll[*tableCell](sh.view.table) {
		if c.Visible() && !c.truncated && c.full != "" && c.content.kind == cellText && c.col == columnOf(t, sh, "type") {
			fits = c
			break
		}
	}
	test.MoveMouse(w.Canvas(), at(fits))
	q.drain()
	if sh.tip.owner != nil {
		t.Errorf("a cell that fits asks for a tooltip: %q", sh.tip.text)
	}

	go f.update(func(u *engine.UIState) { u.View = "pods" })
	q.until(t, "the pods view", func() bool { return sh.view.snap != nil && sh.view.snap.View == "pods" })
	settle(w)
	crash := podNamed(t, podCrashLoop)
	warn := cellAt(t, sh, rowOf(t, f, "pods", crash.name), columnOf(t, sh, "warning"))
	// A row in an abnormal state shows a spine and a status pill: its warning icon would say it a
	// third time, so the reason moves to the tooltip of the status word (Dark Cockpit).
	if warn.content.kind != cellText || warn.iconRes != nil && warn.icon.Visible() || warn.text.Text != "" {
		t.Fatalf("warning cell %+v", warn.content)
	}
	status := cellAt(t, sh, rowOf(t, f, "pods", crash.name), columnOf(t, sh, "status"))
	test.MoveMouse(w.Canvas(), at(status))
	q.until(t, "the warning tooltip", func() bool {
		return sh.tip.visibleText() == "CrashLoopBackOff; Containers not ready: app; 412 restarts"
	})

	// Scrolling moves the cells under the pointer: the tooltip closes. ScrollTo moves the offset
	// as the wheel does; a synthetic wheel event would also arm Fyne's scroll-bar timer, whose
	// fyne.Do the test driver runs on the timer's goroutine, racing the next test's widgets.
	sh.view.table.ScrollTo(widget.TableCellID{Row: 400, Col: 0})
	sh.view.table.ScrollTo(widget.TableCellID{Row: 0, Col: 0})
	q.drain()
	if sh.tip.visibleText() != "" {
		t.Error("the tooltip stayed open while the table scrolled")
	}
}

// TestDrawerToolbarFromRegistry: the drawer's title bar shows the same actions as the row menu, as
// icons named by their tooltips (Lens: a node's Shell, Cordon or Uncordon, Drain, Edit, Delete).
func TestDrawerToolbarFromRegistry(t *testing.T) {
	f := kindsFake(t, "nodes")
	registerProbes(t, f)
	f.disabled["node.drain"] = "the demo cluster cannot be changed"
	ref := model.ObjectRef{Context: "demo", Kind: "nodes", Name: cordonedNode}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the toolbar", func() bool { return len(sh.drawer.actions.Objects) == 5 })
	var tips []string
	for _, o := range sh.drawer.actions.Objects {
		tips = append(tips, o.(*tipButton).tip)
	}
	want := []string{"Shell", "Uncordon", "Drain: the demo cluster cannot be changed", "Edit", "Delete"}
	if strings.Join(tips, "|") != strings.Join(want, "|") {
		t.Fatalf("toolbar %q, want %q", tips, want)
	}
	if b := sh.drawer.actions.Objects[0].(*tipButton); b.Icon == nil || b.Text != "" {
		t.Errorf("the toolbar shows icons only: %q %v", b.Text, b.Icon)
	}
}

// TestEvaluateActionsSkipsParamErrors: a command whose stub parameters the schema refuses is shown
// (Dispatch validates the dialog's values), not hidden and not greyed with a schema error.
func TestEvaluateActionsSkipsParamErrors(t *testing.T) {
	f := kindsFake(t, "pods")
	type strict struct {
		Ref  model.ObjectRef `json:"ref"`
		Mode string          `json:"mode" jsonschema:"one of fast or safe"`
	}
	if err := engine.Register(f.reg, engine.Spec[strict]{ID: "pod.probeStrict", Label: "Strict", Kinds: []string{"pods"}, Order: 400, Danger: engine.DangerRead,
		Enabled: func(_ *engine.Session, p strict) error {
			if p.Mode == "" {
				return &engine.ParamsError{Command: "pod.probeStrict", Err: errors.New("mode is required")}
			}
			return nil
		},
		Run: func(context.Context, *engine.Session, strict) (any, error) { return nil, nil }}); err != nil {
		t.Fatal(err)
	}
	sh, _, _ := settledShell(t, f, "pods")
	ref := f.tables["pods"].Rows[0].Ref
	for _, a := range sh.evaluateActions(ref, []string{"app"}) {
		if a.cmd.ID == "pod.probeStrict" {
			if a.reason != "" || !a.dialog {
				t.Errorf("strict action %+v", a)
			}
			var p map[string]any
			_ = json.Unmarshal(a.prefill, &p)
			if p["mode"] != "" {
				t.Errorf("prefill %v", p)
			}
			return
		}
	}
	t.Fatal("the strict command is missing from the actions")
}
