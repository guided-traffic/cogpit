package fyneui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
	"github.com/guided-traffic/cogpit/internal/testkit/enginekit"
)

// imageSecShell runs the shell over the real engine and a connector, connected to its context.
func imageSecShell(t *testing.T, mode ThemeMode, conn engine.Connector, context string) (*shell, fyne.Window, *mainQueue, *engine.Session) {
	t.Helper()
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
	sh := newShell(s, a, w, Options{AutoConnect: context, Theme: mode}, q.do)
	sh.now = func() time.Time { return fixedNow }
	s.SetConfirmer(sh.confirmer)
	w.SetContent(sh.root)
	w.Resize(fyne.NewSize(1440, 900))
	sh.start()
	t.Cleanup(func() {
		sh.stop()
		q.drain()
	})
	q.until(t, "the cluster's kinds", func() bool { return sh.ui.Connection.State == engine.Connected && len(sh.kinds) > 0 })
	return sh, w, q, s
}

// trivyMemoryShell is the shell on a cluster running Trivy Operator (testkit.ImageSecuritySource).
func trivyMemoryShell(t *testing.T) (*shell, *mainQueue, *engine.Session, *testkit.MemorySource) {
	t.Helper()
	src := testkit.ImageSecuritySource()
	sh, _, q, s := imageSecShell(t, ThemeDark, enginekit.MemoryConnector{Context: "kind-trivy", Src: src, Profiles: kinds.Builtin(), CustomResources: true}, "kind-trivy")
	q.until(t, "the reports' kind", func() bool { return sh.side.titles[engine.ImageSecurity] != "" })
	return sh, q, s, src
}

// setTrivyOn switches the Trivy integration for the shown cluster, as Cluster Settings does.
func setTrivyOn(s *engine.Session, on bool) {
	s.UpdateUI(func(u *engine.UIState) {
		if u.ClusterSettings == nil {
			u.ClusterSettings = map[string]engine.ClusterSettings{}
		}
		cs := u.ClusterSettings[u.Context]
		if on {
			cs.SetIntegrations(0, engine.IntTrivy, 0)
		} else {
			cs.SetIntegrations(0, 0, engine.IntTrivy)
		}
		u.ClusterSettings[u.Context] = cs
	})
}

// TestImageSecurityInTheNavigator: the page is an entry of its own under Integrations while the
// Trivy integration is on and the cluster serves the reports; switched off, or the reports'
// definition gone, it goes.
func TestImageSecurityInTheNavigator(t *testing.T) {
	sh, q, s, src := trivyMemoryShell(t)
	side := sh.side
	integrations := headingPrefix + "integrations"
	if got := titlesOf(side, side.children[integrations]); !slices.Equal(got, []string{"Image Security"}) {
		t.Fatalf("Integrations %v", got)
	}
	if side.parents[engine.ImageSecurity] != "" || side.depth(engine.ImageSecurity) != 0 {
		t.Errorf("not a top-level entry: parent %q", side.parents[engine.ImageSecurity])
	}
	side.activate(engine.ImageSecurity)
	q.until(t, "the page", func() bool { return sh.ui.View == engine.ImageSecurity })
	if sh.ui.Filter != engine.ImageSecurityDefaultFilter {
		t.Errorf("the page opened with %q", sh.ui.Filter)
	}

	setTrivyOn(s, false)
	q.until(t, "the entry to go", func() bool { return side.titles[engine.ImageSecurity] == "" })
	if _, ok := side.children[integrations]; ok {
		t.Errorf("the heading stayed with nothing under it: %v", side.children[integrations])
	}
	setTrivyOn(s, true)
	q.until(t, "the entry back", func() bool { return side.titles[engine.ImageSecurity] != "" })

	src.Delete(testkit.CRDsGVR, "", "vulnerabilityreports.aquasecurity.github.io")
	q.until(t, "the entry to go with the reports' kind", func() bool { return side.titles[engine.ImageSecurity] == "" })
}

// TestImageSecurityPageInTheWindow: the header line counts the running images, the cards show the
// model, a card's entry narrows the table through the search, the table's rows are the findings
// coloured by D-calm (Critical red, High amber, the rest grey, no pill), and a row opens its
// workload's drawer asking for its container's section.
func TestImageSecurityPageInTheWindow(t *testing.T) {
	sh, q, _, _ := trivyMemoryShell(t)
	tv := sh.view
	// The first view's rows first, so that the page takes over a table that drew status pills.
	q.until(t, "the first view's rows", func() bool { return tv.snap != nil && tv.snap.Synced && len(tv.snap.Rows) > 0 })
	q.drain()
	sh.side.activate(engine.ImageSecurity)
	q.until(t, "the cards and the findings", func() bool {
		return tv.imageSec.data != nil && len(tv.imageSec.data.TopImages) == 3 && tv.snap != nil && tv.snap.View == engine.ImageSecurity && len(tv.snap.Rows) == 8
	})
	if tv.title.text != "Image Security" || tv.eventsTitle.text != "Findings" || tv.search.PlaceHolder != "CVE, package, image…" {
		t.Errorf("page %q, table %q, search %q", tv.title.text, tv.eventsTitle.text, tv.search.PlaceHolder)
	}
	if got := tv.imageSec.sub.text; !strings.HasPrefix(got, "4 images running · 3 scanned · 1 not scanned · newest scan ") {
		t.Errorf("header line %q", got)
	}
	if !slices.Contains(tv.heading.Objects, fyne.CanvasObject(tv.imageSec.sub)) || tv.states.box.Visible() {
		t.Error("the header line is not beside the title, or the status filter shows")
	}
	texts := visibleTexts(tv.imageSec.root)
	for _, want := range []string{"Vulnerabilities", "7 CVEs · 3 fixable", "Top images", "shop/api:2.4.1", "Top CVEs", "CVE-2024-0001", "Top namespaces", "team"} {
		if !slices.Contains(texts, want) {
			t.Errorf("the cards lack %q: %q", want, texts)
		}
	}
	q.drain()
	settle(sh.win)
	lookPNG(t, sh.win, "image_security_fixture_dark.png") // the grey shield of C beside D's
	badges := findAll[*gradeBadge](tv.imageSec.root)
	if len(badges) != 5 || !badges[0].calm {
		t.Errorf("%d calm shields", len(badges))
	}

	// A namespace's entry: the table narrows to it, the search keeps its sev token.
	taps := findAll[*tapTarget](tv.imageSec.root)
	if len(taps) != 3+5+2 {
		t.Fatalf("%d entries", len(taps))
	}
	taps[len(taps)-1].Tapped(nil) // team
	q.until(t, "the namespace's findings", func() bool { return sh.ui.Filter == "sev>=high ns:team" && len(tv.snap.Rows) == 2 })
	if tv.search.Text != "sev>=high ns:team" {
		t.Errorf("the search field shows %q", tv.search.Text)
	}

	// The table's colours: the severity in its calm colour, the installed version grey, no pill.
	sev := slices.IndexFunc(tv.snap.Columns, func(c model.Column) bool { return c.ID == "severity" })
	inst := slices.IndexFunc(tv.snap.Columns, func(c model.Column) bool { return c.ID == "installed" })
	if imageSecTone(&tv.snap.Columns[sev], model.Cell{Status: model.StatusError}) != colorStatusError ||
		imageSecTone(&tv.snap.Columns[sev], model.Cell{Status: model.StatusWarning}) != colorStatusWarning ||
		imageSecTone(&tv.snap.Columns[sev], model.Cell{}) != colorMuted || imageSecTone(&tv.snap.Columns[inst], model.Cell{}) != colorMuted ||
		imageSecTone(&tv.snap.Columns[0], model.Cell{}) != "" {
		t.Error("the cells' tones")
	}
	q.drain()
	// The cells the table draws: one it dropped (the first view's Status column) stays in tv.cells
	// with its last content and visible, outside the table's tree.
	for _, c := range findAll[*tableCell](tv.table) {
		if c.Visible() && c.content.pill {
			t.Errorf("a pill in row %d, column %d", c.row, c.col)
		}
	}

	// A row opens its workload's drawer and asks for the container's section.
	tv.rowTapped(widget.TableCellID{Row: 0, Col: firstDataColumn})
	ref := model.ObjectRef{Context: "kind-trivy", Kind: "pods", Namespace: "team", Name: "bare"}
	q.until(t, "the drawer", func() bool { return sh.ui.Drawer != nil && *sh.ui.Drawer == ref })
	if w := sh.drawer.want; w != nil && (w.ref != ref || w.section != "shell") {
		t.Errorf("asked for %+v", *w)
	}
}

func renderImageSecurity(t *testing.T, mode ThemeMode, name string) {
	if testing.Short() {
		t.Skip("builds a demo cluster")
	}
	conn := demoConnector(t, demoSize{Pods: 1200, Deployments: 90, NoChurn: true, Seed: 11})
	sh, w, q, _ := imageSecShell(t, mode, conn, engine.DemoContext)
	sh.side.activate(engine.ImageSecurity)
	q.until(t, "the cards and the findings", func() bool {
		tv := sh.view
		return tv.imageSec.data != nil && len(tv.imageSec.data.TopImages) == 5 && tv.snap != nil && tv.snap.View == engine.ImageSecurity &&
			tv.snap.Synced && len(tv.snap.Rows) > 20
	})
	q.drain()
	settle(w)
	savePNG(t, w, name)
}

func TestRenderImageSecurityDark(t *testing.T) {
	renderImageSecurity(t, ThemeDark, "image_security_dark.png")
}

func TestRenderImageSecurityLight(t *testing.T) {
	renderImageSecurity(t, ThemeLight, "image_security_light.png")
}
