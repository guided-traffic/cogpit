package fyneui

import (
	"image/color"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
	"github.com/guided-traffic/cogpit/internal/testkit/enginekit"
)

// The Velero page in the window (velero.go), over the real engine and the demo cluster's Velero
// (demo/velero.go): the header's line, the tiles, the three tables and their clicks, and the calm
// rule: no colour but the error's and the warning's on the page. PNGs: velero_{dark,light}.png.

// veleroDemoShell is the shell on the demo cluster, small but with every namespace its Velero
// names, on the Velero page.
func veleroDemoShell(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue) {
	t.Helper()
	conn := demoConnector(t, demoSize{Pods: 300, Deployments: 40, Services: 20, Namespaces: 20, Nodes: 6, Events: 40, NoChurn: true})
	conn.KubeconfigPaths = []string{filepath.Join(t.TempDir(), "none")}
	s := newTestEngine(engine.Options{Connector: conn, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	sh, w, q := newEngineShell(t, s, Options{AutoConnect: engine.DemoContext, Theme: mode})
	q.until(t, "the demo's Velero", func() bool {
		return sh.ui.Connection.State == engine.Connected && slices.ContainsFunc(sh.kinds, func(k kindInfo) bool { return k.Kind == engine.VeleroBackupsKey })
	})
	sh.dispatch("ui.openView", openViewParams{View: engine.VeleroDashboard}, nil)
	q.until(t, "the Velero page", func() bool {
		d := sh.view.velero.data
		return d != nil && d.Served && d.Synced && len(d.Schedules) == 5 && len(sh.view.velero.box.Objects) > 0
	})
	settle(w)
	return sh, w, q
}

func TestRenderVeleroDark(t *testing.T) {
	sh, w, _ := veleroDemoShell(t, ThemeDark)
	w.Resize(fyne.NewSize(1440, 1340)) // the whole page, Outside every schedule included
	checkVeleroPage(t, sh)
	savePNG(t, w, "velero_dark.png")
}

func TestRenderVeleroLight(t *testing.T) {
	sh, w, _ := veleroDemoShell(t, ThemeLight)
	checkVeleroCalm(t, sh)
	savePNG(t, w, "velero_light.png")
}

// checkVeleroPage checks what the page draws: the header's line, the tiles' titles, the tables'
// rows in the model's order, and the calm rule.
func checkVeleroPage(t *testing.T, sh *shell) {
	t.Helper()
	p := sh.view.velero
	if got := p.sub.line.text; got != "1.17.1 · namespace velero · server 1/1 · node agents 6/6 · plugins aws 1.13.0" {
		t.Errorf("header line %q", got)
	}
	if !p.root.Visible() || !slices.Contains(sh.view.heading.Objects, fyne.CanvasObject(p.sub)) ||
		!slices.Equal(sh.view.tools.Objects, []fyne.CanvasObject{sh.view.nsBox}) {
		t.Error("the header is not the page's: its line beside the title, the namespace filter alone")
	}
	texts := map[string]bool{}
	for _, l := range findAll[*textLine](p.root) {
		texts[strings.TrimSuffix(l.text, " UTC")] = true // the cron's zone is named where the computer's is another
	}
	for _, want := range []string{"Schedules", "Backups", "Restores", "Storage", "Location unavailable", "Overdue", "Maintenance failed",
		"nightly-all", "daily 02:00", "demo-ml", veleroDotsFooter, veleroOutsideFooter} {
		if !texts[want] {
			t.Errorf("the page draws no %q", want)
		}
	}
	if n := len(findAll[*veleroDots](p.root)); n != 5 {
		t.Errorf("%d rows of dots, want one per schedule", n)
	}
	checkVeleroCalm(t, sh)
}

// checkVeleroCalm checks the calm rule on the page: every text in the foreground, muted or faint
// colour, or in the error's or the warning's; every bar segment and dot grey, dim, or one of them.
func checkVeleroCalm(t *testing.T, sh *shell) {
	t.Helper()
	p := sh.view.velero
	allowed := []fyne.ThemeColorName{theme.ColorNameForeground, colorMuted, theme.ColorNamePlaceHolder, colorStatusError, colorStatusWarning}
	signals := 0
	for _, l := range findAll[*textLine](p.root) {
		if !slices.Contains(allowed, l.color) {
			t.Errorf("%q in %s", l.text, l.color)
		}
		if l.color == colorStatusError || l.color == colorStatusWarning {
			signals++
		}
	}
	if signals == 0 {
		t.Error("nothing on the page needs action: the demo's problems are not drawn")
	}
	th := sh.app.Settings().Theme()
	marks := []color.Color{}
	for _, n := range []fyne.ThemeColorName{theme.ColorNameDisabled, colorSlotDim, colorMuted, colorStatusError, colorStatusWarning} {
		marks = append(marks, themeColor(th, n))
	}
	for _, b := range findAll[*veleroBar](p.root) {
		for _, l := range b.lines {
			if !slices.Contains(marks, themeColor(th, veleroBarTone(l.Status))) {
				t.Errorf("a bar's %s in another colour", l.Label)
			}
		}
	}
	for _, d := range findAll[*veleroDots](p.root) {
		for _, r := range d.runs {
			if tone, _ := veleroDotLook(r.State); !slices.Contains(marks, themeColor(th, tone)) {
				t.Errorf("a %s dot in %s", r.State, tone)
			}
		}
	}
}

// TestVeleroPageClicks: a tile's title opens its kind's view, whose header is a view's again; a row
// opens its object's drawer beside the page; a namespace row the namespace's.
func TestVeleroPageClicks(t *testing.T) {
	sh, _, q := veleroDemoShell(t, ThemeDark)
	p := sh.view.velero
	rows := findAll[*pageRow](p.root)
	if len(rows) == 0 {
		t.Fatal("no rows")
	}
	rows[0].Tapped(nil) // Needs attention's first: the unavailable location
	q.until(t, "the location's drawer", func() bool {
		d := sh.ui.Drawer
		return sh.ui.View == engine.VeleroDashboard && d != nil && d.Kind == engine.VeleroLocationsKey && d.Name == "secondary"
	})
	last := rows[len(rows)-1] // Outside every schedule's last
	last.Tapped(nil)
	q.until(t, "the namespace's drawer", func() bool { d := sh.ui.Drawer; return d != nil && d.Kind == kindNamespaces })
	for _, tt := range findAll[*tapTarget](p.root) {
		if lines := findAll[*textLine](tt); len(lines) == 1 && lines[0].text == "Backups" {
			tt.Tapped(nil)
		}
	}
	q.until(t, "the Backups view", func() bool { return sh.ui.View == engine.VeleroBackupsKey })
	q.until(t, "the page's body to go", func() bool { return !p.root.Visible() && p.data == nil })
	// The view's header again: its count and search, the status filter.
	tv := sh.view
	if !slices.Contains(tv.heading.Objects, fyne.CanvasObject(tv.count)) || !slices.Contains(tv.tools.Objects, fyne.CanvasObject(tv.searchBox)) ||
		slices.Contains(tv.heading.Objects, fyne.CanvasObject(p.sub)) {
		t.Error("the view's header did not come back")
	}
}

// TestVeleroNavigatorEntry: the page's entry under Integrations shows while the cluster serves
// Velero's Backups, after Argo CD's group, and goes with them.
func TestVeleroNavigatorEntry(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.NamespacesGVR, testkit.Namespace("default"))
	conn := enginekit.MemoryConnector{Context: "kind-velero", Src: src, Profiles: kinds.Builtin(), CustomResources: true}
	s := newTestEngine(engine.Options{Connector: conn, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	sh, _, q := newEngineShell(t, s, Options{AutoConnect: "kind-velero", Theme: ThemeDark})
	q.until(t, "the kinds", func() bool { return sh.ui.Connection.State == engine.Connected && len(sh.kinds) > 0 })
	side := sh.side
	integrations := headingPrefix + "integrations"
	if slices.Contains(side.children[integrations], engine.VeleroDashboard) {
		t.Fatal("the Velero page shows without Velero")
	}
	for _, crd := range testkit.VeleroCRDs() {
		src.Set(testkit.CRDsGVR, crd)
	}
	for _, crd := range testkit.ArgoCRDs() {
		src.Set(testkit.CRDsGVR, crd)
	}
	q.until(t, "the Velero entry", func() bool {
		kids := side.children[integrations]
		return len(kids) == 2 && kids[0] == groupPrefix+argoGroup && kids[1] == engine.VeleroDashboard
	})
	if side.titles[engine.VeleroDashboard] != "Velero" || side.depth(engine.VeleroDashboard) != 0 || navIcon(engine.VeleroDashboard) == nil {
		t.Errorf("entry %q depth %d", side.titles[engine.VeleroDashboard], side.depth(engine.VeleroDashboard))
	}
	src.Delete(testkit.CRDsGVR, "", engine.VeleroBackupsKey)
	q.until(t, "the entry to go", func() bool { return !slices.Contains(side.children[integrations], engine.VeleroDashboard) })
}
