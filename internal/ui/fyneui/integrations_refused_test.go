package fyneui

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
	"github.com/guided-traffic/cogpit/internal/testkit/enginekit"
)

// The integrations' pages of a user who may not list their kinds (D-177, D-178, D-179), with the models
// the engine builds for that user (TestBudgetImageSecurityRefusedReports,
// TestBudgetVeleroRefusedKinds): what could not be read shows no count, the refusal is said once.

const (
	imageSecMayNotList = "Cogpit may not list vulnerabilityreports.aquasecurity.github.io"
	veleroMayNotList   = "Cogpit may not list backups.velero.io"
)

// pageColumns are an open page's columns as the engine builds them over src, view open.
func pageColumns(t *testing.T, src *testkit.MemorySource, view string) []model.Column {
	t.Helper()
	s := newTestEngine(engine.Options{
		Connector:        enginekit.MemoryConnector{Context: "kind-columns", Src: src, Profiles: kinds.Builtin(), CustomResources: true},
		SnapshotInterval: 5 * time.Millisecond,
	})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := s.Connect(ctx, "kind-columns"); err != nil {
		t.Fatal(err)
	}
	setTrivyOn(s, true)
	var cols []model.Column
	if !testkit.WaitFor(ctx, func() bool {
		s.OpenTab(view, false)
		if ts := s.Table(view); ts != nil && len(ts.Columns) > 0 {
			cols = ts.Columns
			return true
		}
		return false
	}) {
		t.Fatalf("the %s page never built", view)
	}
	return cols
}

// refusedPageFake is a fake engine on view whose model command (id) answers m and whose table is
// refused: no rows, its error line errLine.
func refusedPageFake(t *testing.T, view, id string, m any, cols []model.Column, errLine string) *fakeEngine {
	t.Helper()
	f := connectedFake(t, 1)
	f.ui.View = view
	f.handlers[id] = func(context.Context, engine.Invocation) (any, error) { return m, nil }
	f.setTable(view, &engine.TableSnapshot{View: view, Context: "demo", Namespaced: true, Columns: cols,
		Rows: []model.Row{}, Synced: true, Error: errLine, Revision: 1})
	return f
}

// TestRenderImageSecurityMayNotListDark: the Image Security page of a user who may not list the
// reports: no cards, no header line, the error line names the reports once and the table claims
// nothing.
func TestRenderImageSecurityMayNotListDark(t *testing.T) {
	cols := pageColumns(t, testkit.ImageSecuritySource(), engine.ImageSecurity)
	f := refusedPageFake(t, engine.ImageSecurity, "trivy.imageSecurity",
		engine.ImageSecurityModel{PodsRead: true, Synced: true, Error: imageSecMayNotList}, cols, imageSecMayNotList)
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	refusedTableShown(t, sh, q, imageSecMayNotList)
	q.until(t, "the page's reading", func() bool { return sh.view.imageSec.data != nil })
	if len(sh.view.imageSec.box.Objects) != 0 || sh.view.imageSec.sub.text != "" {
		t.Errorf("the refused page shows cards or the header line %q", sh.view.imageSec.sub.text)
	}
	savePNG(t, w, "image_security_may_not_list_dark.png")
}

// TestRenderVeleroMayNotListDark: the Velero page of a user who may not list Velero's kinds: each
// tile counts "—" in the muted colour and names its kind once, the page's own error line repeats
// none of them, no tile says "None", and the sections built from them count "—" and claim nothing.
func TestRenderVeleroMayNotListDark(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.NamespacesGVR, testkit.Namespace("default"))
	for _, crd := range testkit.VeleroCRDs() {
		src.Set(testkit.CRDsGVR, crd)
	}
	cols := pageColumns(t, src, engine.VeleroDashboard)
	tile := func(kind, title string, keys ...string) engine.VeleroTile {
		t := engine.VeleroTile{Kind: kind, Title: title, Synced: true}
		for _, k := range keys {
			if t.Error != "" {
				t.Error += "; "
			}
			t.Error += "Cogpit may not list " + k
		}
		return t
	}
	m := engine.VeleroDashboardModel{Served: true, Synced: true, Unread: []string{engine.VeleroSchedulesKey, engine.VeleroBackupsKey,
		engine.VeleroRestoresKey, engine.VeleroLocationsKey, engine.VeleroSnapshotLocationsKey, engine.VeleroRepositoriesKey}, Tiles: []engine.VeleroTile{
		tile(engine.VeleroSchedulesKey, "Schedules", engine.VeleroSchedulesKey),
		tile(engine.VeleroBackupsKey, "Backups", engine.VeleroBackupsKey),
		tile(engine.VeleroRestoresKey, "Restores", engine.VeleroRestoresKey),
		tile(engine.VeleroLocationsKey, "Storage", engine.VeleroLocationsKey, engine.VeleroSnapshotLocationsKey, engine.VeleroRepositoriesKey),
	}}
	f := refusedPageFake(t, engine.VeleroDashboard, "velero.dashboard", m, cols, "")
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the tiles", func() bool { return slices.Contains(visibleTexts(sh.view.velero.root), veleroMayNotList) })
	texts := visibleTexts(sh.view.velero.root)
	if n := countOf(texts, noCount); n != 4 {
		t.Errorf("%d tiles count %q, want 4: %q", n, noCount, texts)
	}
	for _, claim := range []string{"Nothing needs attention.", "No schedules.", "Every namespace is in scope of a schedule."} {
		if slices.Contains(texts, claim) {
			t.Errorf("the page claims %q of what it could not read", claim)
		}
	}
	if n := countOf(texts, veleroMayNotList); n != 1 {
		t.Errorf("the Backups' refusal shows %d times, want once: %q", n, texts)
	}
	if slices.Contains(texts, "None") || slices.Contains(texts, "0") {
		t.Errorf("a refused tile claims a count: %q", texts)
	}
	savePNG(t, w, "velero_may_not_list_dark.png")
}

// countOf counts the strings equal to s.
func countOf(list []string, s string) int {
	n := 0
	for _, x := range list {
		if x == s {
			n++
		}
	}
	return n
}
