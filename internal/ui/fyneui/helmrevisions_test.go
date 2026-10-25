package fyneui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The Revisions tab (plan/helm-revisions.md, a prototype) over the real engine and the demo
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
	s := newTestEngine(engine.Options{Connector: conn, SnapshotInterval: 5 * time.Millisecond})
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
			return r.diff.From == want.Compare && r.diff.To == want.Revision
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
	if !strings.HasPrefix(r.objView.Text(), "# Source: shop-web/templates/") {
		t.Errorf("the object %q", r.objView.Text())
	}
	savePNG(t, h.w, name("manifest"))

	h.set(id, map[string]any{"object": "Secret./demo-shop/shop-web-db"})
	r = h.tab(engine.HelmRevisionsTab{Revision: 3, Mode: engine.RevisionsManifest, Object: "Secret./demo-shop/shop-web-db"})
	if !strings.Contains(r.objView.Text(), helm.MaskedDots) {
		t.Errorf("the Secret is not masked: %q", r.objView.Text())
	}
	savePNG(t, h.w, name("secret_masked"))

	h.set(id, map[string]any{"raw": true})
	r = h.tab(engine.HelmRevisionsTab{Revision: 3, Mode: engine.RevisionsManifest, Raw: true, Object: "Secret./demo-shop/shop-web-db"})
	if !strings.Contains(r.rawView.Text(), helm.MaskedDots) || !strings.Contains(r.rawView.Text(), "kind: Deployment") {
		t.Errorf("manifest.yaml %q", r.rawView.Text())
	}
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
	toolbar := false
	for _, b := range findAll[*tipButton](h.sh.drawer.root) {
		if b.tip == "Revisions" {
			toolbar = true
		}
	}
	if !toolbar {
		t.Error("the drawer's toolbar has no Revisions")
	}

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
