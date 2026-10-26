package fyneui

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The Revisions tab (D-131) over the real engine and the demo
// cluster, whose shop-web release has six revisions: values changed, an image bumped, a chart
// version that adds a ConfigMap and drops a Service, a failed upgrade, a rollback.

var shopWeb = model.ObjectRef{Context: engine.DemoContext, Kind: helm.ReleaseKind, Namespace: "demo-shop", Name: "shop-web"}

// revisionsHarness is a window over the demo cluster with shop-web's drawer open.
type revisionsHarness struct {
	t  *testing.T
	s  *engine.Session
	sh *shell
	w  fyne.Window
	q  *mainQueue
}

func newRevisionsHarness(t *testing.T, mode ThemeMode) *revisionsHarness {
	t.Helper()
	if testing.Short() {
		t.Skip("builds a demo cluster")
	}
	conn := &engine.ClusterConnector{IncludeDemo: true}
	conn.DemoOptions.Pods, conn.DemoOptions.Seed, conn.DemoOptions.NoChurn = 400, 7, true
	s := newTestEngine(engine.Options{Connector: conn, SnapshotInterval: 5 * time.Millisecond, Preferences: engine.NewPreferencesStore("")})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(mode))
	w := test.NewTempWindow(t, nil)
	w.SetPadded(false)
	q := &mainQueue{}
	sh := newShell(s, a, w, Options{AutoConnect: engine.DemoContext, Theme: mode}, q.do)
	s.SetConfirmer(sh.confirmer)
	w.SetContent(sh.root)
	w.Resize(fyne.NewSize(1440, 900))
	sh.start()
	t.Cleanup(func() {
		sh.stop()
		q.drain()
	})
	h := &revisionsHarness{t: t, s: s, sh: sh, w: w, q: q}
	h.until("the demo cluster", func() bool { return sh.ui.Connection.State == engine.Connected && len(sh.kinds) > 0 })
	h.dispatch("ui.navigate", map[string]any{"ref": shopWeb})
	h.until("shop-web's drawer", func() bool {
		return sh.ui.Drawer != nil && sh.ui.Drawer.Name == "shop-web" && sh.drawer.last != nil && len(sh.drawer.last.Sections) > 3
	})
	return h
}

func (h *revisionsHarness) until(what string, cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out waiting for %s", what)
		}
		h.q.drain()
		time.Sleep(5 * time.Millisecond)
	}
	h.q.drain()
}

func (h *revisionsHarness) dispatch(id string, params any) any {
	h.t.Helper()
	raw, _ := json.Marshal(params)
	res, err := h.s.Dispatch(context.Background(), engine.Invocation{ID: id, Params: raw, Origin: engine.OriginGUI})
	if err != nil {
		h.t.Fatalf("%s: %v", id, err)
	}
	return res
}

// tab is the Revisions tab once it shows what the engine's state asks for.
func (h *revisionsHarness) tab(want engine.HelmRevisionsTab) *helmRevisionsTab {
	h.t.Helper()
	var r *helmRevisionsTab
	h.until("the Revisions tab", func() bool {
		for _, c := range h.sh.dock.contents {
			if t, ok := c.editor.(*helmRevisionsTab); ok {
				r = t
			}
		}
		if r == nil || r.st != want || r.cur.Revision != want.Revision || len(r.history) == 0 {
			return false
		}
		if want.Compare != 0 {
			from, to := engine.RevisionsDiffSides(h.sh.revisionsDiff(), want.Revision, want.Compare)
			return r.diff.From == from && r.diff.To == to
		}
		return true
	})
	h.sh.setDockHeight(430)
	h.q.drain()
	return r
}

func (h *revisionsHarness) set(id string, p map[string]any) {
	p["id"] = id
	h.dispatch("ui.helm.revisions.set", p)
}

// Every way into the tab and every mode, as the morning's review sees them: the drawer's History
// with its links, an old revision's values, its values compared with the current one, the line
// diff unfolded, the manifest's objects, a changed object's diff, a masked Secret, manifest.yaml.
func TestRenderHelmRevisionsDark(t *testing.T) { renderHelmRevisions(t, ThemeDark, "dark", true) }

// The light theme once, the two compared views.
func TestRenderHelmRevisionsLight(t *testing.T) { renderHelmRevisions(t, ThemeLight, "light", false) }

func renderHelmRevisions(t *testing.T, mode ThemeMode, suffix string, all bool) {
	h := newRevisionsHarness(t, mode)
	name := func(scene string) string { return "helm_revisions_" + scene + "_" + suffix + ".png" }

	if all {
		h.until("the History's links", func() bool { return revisionLinks(h.sh) >= 6 })
		settle(h.w)
		h.sh.drawer.scroll.ScrollToBottom()
		h.q.drain()
		savePNG(t, h.w, name("drawer"))
	}

	res := h.dispatch(commands.RevisionsCommand, map[string]any{"ref": shopWeb, "revision": 5})
	id := res.(commands.DockResult).Tab.ID
	r := h.tab(engine.HelmRevisionsTab{Revision: 5, Compare: 6, Mode: engine.RevisionsValues})
	if len(r.diff.Changes) == 0 || !strings.Contains(r.keysHead.text, "differ between revision 6 and revision 5") {
		t.Errorf("values compared: %+v %q", r.diff.Changes, r.keysHead.text)
	}
	savePNG(t, h.w, name("values_compared"))

	h.set(id, map[string]any{"lineDiff": true})
	h.tab(engine.HelmRevisionsTab{Revision: 5, Compare: 6, Mode: engine.RevisionsValues, LineDiff: true})
	if all {
		savePNG(t, h.w, name("values_linediff"))
	}

	h.set(id, map[string]any{"revision": 3, "compare": 6, "mode": "manifest", "object": "Deployment.apps/demo-shop/shop-web"})
	r = h.tab(engine.HelmRevisionsTab{Revision: 3, Compare: 6, Mode: engine.RevisionsManifest, LineDiff: true, Object: "Deployment.apps/demo-shop/shop-web"})
	if r.diff.Added+r.diff.Removed+r.diff.Changed == 0 || !engine.DiffChanged(r.diff.Lines) {
		t.Errorf("manifest compared: %+v", r.diff)
	}
	savePNG(t, h.w, name("object_diff"))
	if !all {
		return
	}

	zero := 0
	h.set(id, map[string]any{"compare": zero, "mode": "values", "lineDiff": false})
	r = h.tab(engine.HelmRevisionsTab{Revision: 3, Mode: engine.RevisionsValues, Object: "Deployment.apps/demo-shop/shop-web"})
	if !strings.Contains(r.valuesView.Text(), "replicaCount") {
		t.Errorf("revision 3's values %q", r.valuesView.Text())
	}
	savePNG(t, h.w, name("values"))

	h.set(id, map[string]any{"all": true})
	r = h.tab(engine.HelmRevisionsTab{Revision: 3, Mode: engine.RevisionsValues, All: true, Object: "Deployment.apps/demo-shop/shop-web"})
	if !strings.Contains(r.valuesView.Text(), "autoscaling:") {
		t.Errorf("revision 3's values, the chart's defaults included: %q", r.valuesView.Text())
	}
	savePNG(t, h.w, name("values_all"))

	h.set(id, map[string]any{"all": false, "mode": "manifest"})
	r = h.tab(engine.HelmRevisionsTab{Revision: 3, Mode: engine.RevisionsManifest, Object: "Deployment.apps/demo-shop/shop-web"})
	// The object's YAML is filled by the render after the tab's state (Windows' UI tests read it
	// before that, 2026-10-26): wait for it.
	h.until("the object", func() bool { return strings.HasPrefix(r.objView.Text(), "# Source: shop-web/templates/") })
	savePNG(t, h.w, name("manifest"))

	h.set(id, map[string]any{"object": "Secret./demo-shop/shop-web-db"})
	r = h.tab(engine.HelmRevisionsTab{Revision: 3, Mode: engine.RevisionsManifest, Object: "Secret./demo-shop/shop-web-db"})
	h.until("the Secret masked", func() bool {
		return strings.Contains(r.objView.Text(), "shop-web-db") && strings.Contains(r.objView.Text(), helm.MaskedDots)
	})
	savePNG(t, h.w, name("secret_masked"))

	h.set(id, map[string]any{"raw": true})
	r = h.tab(engine.HelmRevisionsTab{Revision: 3, Mode: engine.RevisionsManifest, Raw: true, Object: "Secret./demo-shop/shop-web-db"})
	h.until("manifest.yaml", func() bool {
		return strings.Contains(r.rawView.Text(), helm.MaskedDots) && strings.Contains(r.rawView.Text(), "kind: Deployment")
	})
	savePNG(t, h.w, name("manifest_yaml"))
}

// revisionLinks counts the drawer's links into the Revisions tab: one per revision of the History
// and "Open revisions".
func revisionLinks(sh *shell) int {
	n := 0
	for _, l := range findAll[*linkText](sh.drawer.root) {
		if l.text == "Open revisions" || isDigits(l.text) {
			n++
		}
	}
	return n
}

func isDigits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

// The drawer's ways in: a revision's number in the History opens the tab at that revision,
// compared with the newest; "Open revisions" opens it at the newest, not compared; the drawer's
// toolbar offers the command. An agent's ui.helm.revisions.set moves the window's tab. A Secret's
// Show is the window's: the engine's state does not change, and the object's YAML shows its values.
func TestRevisionsTabFromTheDrawer(t *testing.T) {
	h := newRevisionsHarness(t, ThemeDark)
	h.until("the History's links", func() bool { return revisionLinks(h.sh) >= 6 })
	link := func(text string) *linkText {
		for _, l := range findAll[*linkText](h.sh.drawer.root) {
			if l.text == text {
				return l
			}
		}
		t.Fatalf("no link %q in the drawer", text)
		return nil
	}
	link("3").Tapped(nil)
	r := h.tab(engine.HelmRevisionsTab{Revision: 3, Compare: 6, Mode: engine.RevisionsValues})
	if !strings.Contains(r.keysHead.text, "between revision 6 and revision 3") {
		t.Errorf("revision 3 compared: %q", r.keysHead.text)
	}
	link("Open revisions").Tapped(nil)
	r = h.tab(engine.HelmRevisionsTab{Revision: 6, Mode: engine.RevisionsValues})
	if !strings.Contains(r.valuesView.Text(), "replicaCount") {
		t.Errorf("revision 6's values %q", r.valuesView.Text())
	}
	tabs := 0
	for _, d := range h.sh.ui.Dock {
		if d.Type == engine.DockHelmRevisions {
			tabs++
		}
	}
	if tabs != 1 {
		t.Errorf("%d Revisions tabs, want one per release", tabs)
	}
	var toolbar *tipButton
	for _, b := range findAll[*tipButton](h.sh.drawer.root) {
		if b.tip == "Revisions" {
			toolbar = b
		}
	}
	if toolbar == nil {
		t.Fatal("the drawer's toolbar has no Revisions")
	}
	menu := h.sh.rowMenu(shopWeb, h.sh.evaluateActions(shopWeb, nil))
	if !slices.ContainsFunc(menu.Items, func(it *fyne.MenuItem) bool { return it.Label == "Revisions" }) {
		t.Errorf("the row menu has no Revisions: %v", menuLabels(menu))
	}
	zero := 0
	h.set(r.tab.ID, map[string]any{"revision": 2, "compare": zero})
	h.tab(engine.HelmRevisionsTab{Revision: 2, Mode: engine.RevisionsValues})
	toolbar.OnTapped() // the newest again, not compared
	r = h.tab(engine.HelmRevisionsTab{Revision: 6, Mode: engine.RevisionsValues})

	raw, _ := json.Marshal(map[string]any{"id": r.tab.ID, "mode": "manifest", "object": "Secret./demo-shop/shop-web-db"})
	if _, err := h.s.Dispatch(context.Background(), engine.Invocation{ID: "ui.helm.revisions.set", Params: raw, Origin: engine.OriginExternalAgent}); err != nil {
		t.Fatal(err)
	}
	r = h.tab(engine.HelmRevisionsTab{Revision: 6, Mode: engine.RevisionsManifest, Object: "Secret./demo-shop/shop-web-db"})
	if strings.Contains(r.objView.Text(), "password: ") && !strings.Contains(r.objView.Text(), helm.MaskedDots) {
		t.Fatalf("the Secret shows before Show: %q", r.objView.Text())
	}
	before := h.sh.ui.Revision
	r.objShow.OnTapped()
	h.q.drain()
	if strings.Contains(r.objView.Text(), helm.MaskedDots) || h.sh.ui.Revision != before {
		t.Errorf("Show: %q, the engine's state moved %d → %d", r.objView.Text(), before, h.sh.ui.Revision)
	}
}

// Preferences › App › Revision diffs turns the open tab's comparison round: from the compared
// revision to the shown one by default, from the older to the newer, from the shown to the
// compared; the key list's columns name the sides.
func TestRevisionsDiffPreference(t *testing.T) {
	h := newRevisionsHarness(t, ThemeDark)
	res := h.dispatch(commands.RevisionsCommand, map[string]any{"ref": shopWeb, "revision": 6, "compare": 3})
	id := res.(commands.DockResult).Tab.ID
	want := engine.HelmRevisionsTab{Revision: 6, Compare: 3, Mode: engine.RevisionsValues}
	for _, c := range []struct {
		way            string
		from, to       int
		fromCol, toCol string
	}{
		{"", 3, 6, "REVISION 3 (COMPARED)", "REVISION 6 (SHOWN)"},
		{engine.RevisionsShownToCompared, 6, 3, "REVISION 6 (SHOWN)", "REVISION 3 (COMPARED)"},
		{engine.RevisionsOlderToNewer, 3, 6, "REVISION 3 (COMPARED)", "REVISION 6 (SHOWN)"},
	} {
		h.dispatch("preferences.set", map[string]any{"revisionsDiff": c.way})
		h.until("the preference "+c.way, func() bool { return h.sh.revisionsDiff() == c.way })
		r := h.tab(want)
		if r.diff.From != c.from || r.diff.To != c.to || r.keyCols.from.text != c.fromCol || r.keyCols.to.text != c.toCol {
			t.Errorf("%q: %d → %d, columns %q %q", c.way, r.diff.From, r.diff.To, r.keyCols.from.text, r.keyCols.to.text)
		}
		for _, k := range r.diff.Changes {
			if k.Path == "image.tag" && (k.From != map[bool]string{true: "2.4.0", false: "2.3.1"}[c.from == 6] || k.To != map[bool]string{true: "2.3.1", false: "2.4.0"}[c.from == 6]) {
				t.Errorf("%q: image.tag %q → %q", c.way, k.From, k.To)
			}
		}
	}
	h.set(id, map[string]any{"revision": 5, "compare": 6})
	h.tab(engine.HelmRevisionsTab{Revision: 5, Compare: 6, Mode: engine.RevisionsValues})
	if r := h.tab(engine.HelmRevisionsTab{Revision: 5, Compare: 6, Mode: engine.RevisionsValues}); r.diff.From != 5 || r.diff.To != 6 {
		t.Errorf("older to newer, revision 5 against 6: %d → %d", r.diff.From, r.diff.To)
	}
}

// Every text of the tab worth copying is selectable and ⌘C copies it (the owner, 2026-10-26): a
// changed key's path and values, a revision's chart version and time, an object's name, a diff's
// line, the status line; the copy icon copies what the tab shows, the changed keys or the diff, not
// an older text.
func TestRevisionsTabTextsSelectAndCopy(t *testing.T) {
	h := newRevisionsHarness(t, ThemeDark)
	res := h.dispatch(commands.RevisionsCommand, map[string]any{"ref": shopWeb, "revision": 3})
	id := res.(commands.DockResult).Tab.ID
	r := h.tab(engine.HelmRevisionsTab{Revision: 3, Compare: 6, Mode: engine.RevisionsValues})
	clip := h.sh.app.Clipboard()
	copyOf := func(what string, s interface {
		DoubleTapped(*fyne.PointEvent)
		TypedShortcut(fyne.Shortcut)
		SelectedText() string
	}, want string) {
		t.Helper()
		s.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(2, 2)})
		s.TypedShortcut(&fyne.ShortcutCopy{})
		if got := clip.Content(); s.SelectedText() == "" || got != want {
			t.Errorf("%s: selected %q, copied %q, want %q", what, s.SelectedText(), got, want)
		}
	}
	// A list keeps rows it does not show, empty: the ones that show a text.
	var key *keyRow
	h.until("a key's row", func() bool {
		for _, k := range findAll[*keyRow](r.keysList) {
			if k.path.line.text != "" {
				key = k
			}
		}
		return key != nil
	})
	copyOf("a key's path", key.path, key.path.line.text)
	rev := h.revisionRow(r, 0)
	copyOf("a revision's chart version", rev.chart, rev.chart.line.text)
	copyOf("a revision's time", rev.when, strings.Fields(rev.when.line.text)[0])
	copyOf("the status line", r.status, strings.Fields(r.status.line.text)[0])

	r.copyShown()
	if got := clip.Content(); !strings.HasPrefix(got, "revision 6 (compared) → revision 3 (shown)\n") || !strings.Contains(got, "image.tag: 2.4.0 → 2.3.1\n") {
		t.Errorf("the copy icon on the changed keys: %q", got)
	}
	h.set(id, map[string]any{"lineDiff": true})
	r = h.tab(engine.HelmRevisionsTab{Revision: 3, Compare: 6, Mode: engine.RevisionsValues, LineDiff: true})
	r.copyShown()
	if got := clip.Content(); !strings.Contains(got, "-   tag: 2.4.0\n") || !strings.Contains(got, "+   tag: 2.3.1\n") {
		t.Errorf("the copy icon on the line diff: %q", got)
	}
	var l *diffLineWidget
	h.until("a diff line", func() bool {
		for _, x := range findAll[*diffLineWidget](r.valuesDiff.list) {
			if x.row.skipped == 0 && strings.TrimSpace(x.text.Text) != "" {
				l = x
			}
		}
		return l != nil
	})
	l.anchor, l.caret = 0, len([]rune(l.text.Text))
	l.TypedShortcut(&fyne.ShortcutCopy{})
	if got := clip.Content(); got != l.text.Text || got == "" {
		t.Errorf("a diff line: copied %q, want %q", got, l.text.Text)
	}

	h.set(id, map[string]any{"mode": "manifest", "object": "Deployment.apps/demo-shop/shop-web"})
	r = h.tab(engine.HelmRevisionsTab{Revision: 3, Compare: 6, Mode: engine.RevisionsManifest, LineDiff: true, Object: "Deployment.apps/demo-shop/shop-web"})
	obj := h.objectRow(r, func(o *objectRow) bool { return o.name.line.text != "" })
	copyOf("an object's name", obj.name, obj.name.line.text)
	r.copyShown()
	if got := clip.Content(); !strings.Contains(got, "- ") || !strings.Contains(got, "+ ") {
		t.Errorf("the copy icon on an object's diff: %q", got)
	}
}

// A click on a row's text worth copying picks the row at once, as a click beside it does (the
// owner, 2026-10-26: the revisions could hardly be clicked, the rows being selectable text); a
// drag over it selects and picks nothing.
func TestRevisionsRowTextsPassTheirClick(t *testing.T) {
	h := newRevisionsHarness(t, ThemeDark)
	h.dispatch(commands.RevisionsCommand, map[string]any{"ref": shopWeb, "revision": 3})
	r := h.tab(engine.HelmRevisionsTab{Revision: 3, Compare: 6, Mode: engine.RevisionsValues})
	press := &desktop.MouseEvent{Button: desktop.MouseButtonPrimary}
	press.Position = fyne.NewPos(2, 2)
	click := func(s *selLine) {
		s.MouseDown(press)
		s.MouseUp(press)
	}

	rev := h.revisionRow(r, 1)
	want := r.history[rev.id].Revision
	rev.chart.MouseDown(press)
	rev.chart.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(30, 2)}, Dragged: fyne.NewDelta(28, 0)})
	rev.chart.MouseUp(press)
	rev.chart.DragEnd()
	h.q.drain()
	if r.st.Revision != 3 || rev.chart.SelectedText() == "" {
		t.Errorf("a drag over revision %d's version: revision %d shown, selected %q", want, r.st.Revision, rev.chart.SelectedText())
	}
	click(rev.chart)
	h.until("the revision clicked by its version", func() bool { return r.st.Revision == want && r.cur.Revision == want })

	rev = h.revisionRow(r, 2)
	want = r.history[rev.id].Revision
	click(rev.when)
	h.until("the revision clicked by its time", func() bool { return r.st.Revision == want && r.cur.Revision == want })

	h.set(r.tab.ID, map[string]any{"mode": "manifest", "compare": 0})
	h.until("the manifest", func() bool { return r.st.Mode == engine.RevisionsManifest && len(r.objs) > 1 })
	obj := h.objectRow(r, func(o *objectRow) bool { return o.id > 0 && o.name.line.text != "" })
	key := r.objs[obj.id].Key()
	click(obj.name)
	h.until("the object clicked by its name", func() bool { return r.st.Object == key })
}

// revisionRow is the revision list's row that shows its i-th revision; a list keeps rows it does
// not show, empty.
func (h *revisionsHarness) revisionRow(r *helmRevisionsTab, i int) *revisionRow {
	h.t.Helper()
	var row *revisionRow
	h.until("a revision's row", func() bool {
		for _, x := range findAll[*revisionRow](r.revList) {
			if x.id == i && x.chart.line.text != "" {
				row = x
			}
		}
		return row != nil
	})
	return row
}

// objectRow is a row of the objects' table that shows an object as ok asks.
func (h *revisionsHarness) objectRow(r *helmRevisionsTab, ok func(*objectRow) bool) *objectRow {
	h.t.Helper()
	var row *objectRow
	h.until("an object's row", func() bool {
		for _, x := range findAll[*objectRow](r.objList) {
			if ok(x) {
				row = x
			}
		}
		return row != nil
	})
	return row
}
