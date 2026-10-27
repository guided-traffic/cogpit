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

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// fakeHotbar gives a fake engine the hotbar.* commands. Changes run through the engine's own
// operations (engine.HotbarState) on f.ui.Hotbar, as Session.ChangeHotbar does; hotbar.list adds
// what each tile shows, as Session.HotbarView does, with missing and exec set by the test.
type fakeHotbar struct {
	f       *fakeEngine
	missing map[string]bool
	exec    map[string]string
}

// withHotbar gives the fake a hotbar of these contexts, top to bottom (one hotbar, D-83).
func withHotbar(f *fakeEngine, contexts ...string) *fakeHotbar {
	fh := &fakeHotbar{f: f, missing: map[string]bool{}, exec: map[string]string{}}
	tiles := make([]engine.HotbarTile, 0, len(contexts))
	for _, c := range contexts {
		tiles = append(tiles, engine.HotbarTile{Context: c})
	}
	f.ui.Hotbar = &engine.HotbarState{Clusters: tiles}
	change := func(op func(h *engine.HotbarState, inv engine.Invocation) error) func(context.Context, engine.Invocation) (any, error) {
		return func(_ context.Context, inv engine.Invocation) (any, error) {
			f.mu.Lock()
			next := f.ui.Hotbar.Clone()
			f.mu.Unlock()
			if err := op(&next, inv); err != nil {
				return nil, err
			}
			f.update(func(u *engine.UIState) { u.Hotbar = &next })
			return map[string]any{"hotbar": fh.view()}, nil
		}
	}
	params := func(inv engine.Invocation, p any) error { return json.Unmarshal(normalize(inv.Params), p) }
	f.handlers["hotbar.list"] = func(context.Context, engine.Invocation) (any, error) {
		return map[string]any{"hotbar": fh.view()}, nil
	}
	f.handlers["hotbar.addCluster"] = change(func(h *engine.HotbarState, inv engine.Invocation) error {
		var p hotbarClusterParams
		if err := params(inv, &p); err != nil {
			return err
		}
		if p.Context == "" {
			p.Context = f.UIState().Context
		}
		return h.AddCluster(p.Context)
	})
	f.handlers["hotbar.removeCluster"] = change(func(h *engine.HotbarState, inv engine.Invocation) error {
		var p hotbarClusterParams
		if err := params(inv, &p); err != nil {
			return err
		}
		return h.RemoveCluster(p.Context)
	})
	f.handlers["hotbar.moveCluster"] = change(func(h *engine.HotbarState, inv engine.Invocation) error {
		var p hotbarMoveParams
		if err := params(inv, &p); err != nil {
			return err
		}
		return h.MoveCluster(p.Context, p.Position)
	})
	f.handlers["hotbar.openCluster"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p hotbarSlotParams
		if err := params(inv, &p); err != nil {
			return nil, err
		}
		f.mu.Lock()
		t, err := f.ui.Hotbar.Tile(p.Slot)
		f.mu.Unlock()
		if err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) {
			u.Context, u.Connection, u.View = t.Context, engine.Connection{State: engine.Connected}, "pods"
		})
		return map[string]any{"context": t.Context, "state": engine.Connected}, nil
	}
	return fh
}

// view is the hotbar as hotbar.list returns it.
func (fh *fakeHotbar) view() engine.HotbarState {
	u := fh.f.UIState()
	v := u.Hotbar.Clone()
	for i := range v.Clusters {
		t := &v.Clusters[i]
		t.Label, t.Connection = engine.ContextLabel(t.Context), engine.Disconnected
		if c, ok := u.ConnectionOf(t.Context); ok { // every connected cluster's state (D-69)
			t.Connection = c.Connection.State
		}
		if t.Context == u.Context {
			t.Connection = u.Connection.State
		}
		t.Missing, t.Exec = fh.missing[t.Context], fh.exec[t.Context]
	}
	return v
}

// setHotbar changes the fake's hotbar as an agent's command would: these contexts, top to bottom.
func (fh *fakeHotbar) setHotbar(contexts ...string) {
	tiles := make([]engine.HotbarTile, 0, len(contexts))
	for _, c := range contexts {
		tiles = append(tiles, engine.HotbarTile{Context: c})
	}
	fh.f.update(func(u *engine.UIState) { u.Hotbar = &engine.HotbarState{Clusters: tiles} })
}

func tilesOf(h *hotbar) []*hotbarTile { return findAll[*hotbarTile](h.tiles) }

// hotbarScene is the shell connected to kind-cogpit with a hotbar of six clusters (kind-cogpit
// connected, legacy-onprem missing).
func hotbarScene(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue, *fakeEngine, *fakeHotbar) {
	f := connectedFake(t, 10000)
	f.ui.Context = "kind-cogpit"
	f.ui.Connection = engine.Connection{State: engine.Connected}
	fh := withHotbar(f, "kind-cogpit", "prod-eu-central", "staging-us-east", "legacy-onprem [.kube/old.yaml]", "dev-hfi", "demo")
	fh.missing["legacy-onprem [.kube/old.yaml]"] = true
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the hotbar", func() bool { return sh.hot.root.Visible() && len(tilesOf(sh.hot)) == 6 && len(sh.kinds) > 0 })
	return sh, w, q, f, fh
}

// Without a hotbar in the engine the bar is hidden and nothing asks for one: the shell looks as it
// did before the hotbar.
func TestHotbarHiddenWithoutAHotbar(t *testing.T) {
	f := connectedFake(t, 10)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	if sh.hot.root.Visible() || len(f.callsOf("hotbar.list")) != 0 {
		t.Errorf("visible %v, hotbar.list called %d times", sh.hot.root.Visible(), len(f.callsOf("hotbar.list")))
	}
	if sh.hot.contextItem("demo") != nil {
		t.Error("a hotbar item without a hotbar")
	}
}

// The bar shows the hotbar's tiles: a tile per cluster with its initials and label, the connected
// one active with a green dot, the missing one marked, the full id with state and key as tooltip;
// no bank switcher (D-83), the "+" under the last tile.
func TestHotbarShowsItsTiles(t *testing.T) {
	sh, _, q, f, fh := hotbarScene(t, ThemeDark)
	tl := tilesOf(sh.hot)
	if tl[0].tile.Label != "kind-cogpit" || !tl[0].active() || tl[1].active() || tl[0].slot != 1 {
		t.Errorf("first tiles: %+v %+v", tl[0].tile, tl[1].tile)
	}
	if !tl[3].tile.Missing || tl[3].tile.Label != "legacy-onprem" {
		t.Errorf("the missing tile: %+v", tl[3].tile)
	}
	if got := tl[3].tipText(); got != "legacy-onprem [.kube/old.yaml]\nNo kubeconfig source offers this context any more · F4" {
		t.Errorf("tooltip %q", got)
	}
	if got := tl[0].tipText(); got != "kind-cogpit\nConnected · F1" {
		t.Errorf("tooltip %q", got)
	}
	r := test.WidgetRenderer(tl[0]).(*hotbarTileRenderer)
	if r.initials.Text != "KC" || !r.dot.Visible() || r.card.Visible() || r.still.Visible() {
		t.Errorf("initials %q, dot %v, card %v", r.initials.Text, r.dot.Visible(), r.card.Visible())
	}
	if objs := sh.hot.tiles.Objects; objs[len(objs)-1] != sh.hot.add {
		t.Error("the + is not under the last tile")
	}
	if !sh.hot.add.disabled || sh.hot.add.tip != "kind-cogpit is on the hotbar already" {
		t.Errorf("+: %v %q", sh.hot.add.disabled, sh.hot.add.tip)
	}
	// An agent unpins the connected cluster: the bar follows the UI state, and the "+" pins it.
	fh.setHotbar("prod-eu-central", "staging-us-east")
	q.until(t, "two tiles", func() bool { return len(tilesOf(sh.hot)) == 2 })
	if sh.hot.add.disabled || sh.hot.add.tip != "Add kind-cogpit to the hotbar" {
		t.Errorf("+ without kind-cogpit: %v %q", sh.hot.add.disabled, sh.hot.add.tip)
	}
	test.Tap(sh.hot.add)
	q.until(t, "the cluster pinned", func() bool { return len(tilesOf(sh.hot)) == 3 && tilesOf(sh.hot)[2].tile.Context == "kind-cogpit" })
	if p := decodeParams[hotbarClusterParams](t, f.callsOf("hotbar.addCluster")[0]); p.Context != "" {
		t.Errorf("hotbar.addCluster %+v", p)
	}
}

// A new hotbar updates the tiles in place: a context keeps its widget (its tooltip stays), a moved
// tile's widget moves, a removed one's goes and its tooltip with it.
func TestHotbarUpdatesTilesInPlace(t *testing.T) {
	sh, w, q, _, fh := hotbarScene(t, ThemeDark)
	settle(w)
	before := map[string]*hotbarTile{}
	for _, tl := range tilesOf(sh.hot) {
		before[tl.tile.Context] = tl
	}
	staging := before["staging-us-east"]
	sh.tip.delay = time.Millisecond
	sh.tip.hover(staging, staging.tipText(), middleOf(staging))
	q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() != "" })

	fh.setHotbar("demo", "staging-us-east", "kind-cogpit", "prod-eu-central", "dev-hfi") // legacy-onprem removed, demo first
	q.until(t, "the new order", func() bool {
		return slices.Equal(contextsOf(sh.hot), []string{"demo", "staging-us-east", "kind-cogpit", "prod-eu-central", "dev-hfi"})
	})
	for i, tl := range tilesOf(sh.hot) {
		if before[tl.tile.Context] != tl || tl.slot != i+1 {
			t.Errorf("%s: a new widget or slot %d", tl.tile.Context, tl.slot)
		}
	}
	if sh.tip.owner != staging || sh.tip.visibleText() == "" {
		t.Error("the tooltip of a tile that stayed went")
	}
	sh.tip.hover(before["dev-hfi"], "x", middleOf(before["dev-hfi"]))
	fh.setHotbar("demo", "staging-us-east")
	q.until(t, "two tiles", func() bool { return len(tilesOf(sh.hot)) == 2 })
	if sh.tip.owner != nil {
		t.Errorf("the tooltip of a removed tile stays: %T", sh.tip.owner)
	}
}

// A click opens the tile's cluster through hotbar.openCluster, and the bar shows the new connection;
// the connected cluster's tile and a missing one dispatch nothing (the missing one says why).
func TestHotbarTileClickOpensTheCluster(t *testing.T) {
	sh, _, q, f, _ := hotbarScene(t, ThemeDark)
	test.Tap(tilesOf(sh.hot)[0]) // connected already
	test.Tap(tilesOf(sh.hot)[3]) // missing
	q.drain()
	if n := len(f.callsOf("hotbar.openCluster")); n != 0 {
		t.Errorf("hotbar.openCluster dispatched %d times", n)
	}
	if !slices.ContainsFunc(sh.toasts.Objects, func(o fyne.CanvasObject) bool { return o != nil }) {
		t.Error("no toast for the missing tile")
	}
	test.Tap(tilesOf(sh.hot)[1])
	q.until(t, "the connection", func() bool { return sh.ui.Context == "prod-eu-central" && tilesOf(sh.hot)[1].active() })
	if p := decodeParams[hotbarSlotParams](t, f.callsOf("hotbar.openCluster")[0]); p.Slot != 2 {
		t.Errorf("hotbar.openCluster %+v", p)
	}
	if tilesOf(sh.hot)[0].active() {
		t.Error("the old cluster's tile is still active")
	}
}

// A tile whose login program waits for the user's approval shows it first, as the section does:
// nothing connects before the user allowed it.
func TestHotbarTileAsksForTheLoginProgramFirst(t *testing.T) {
	f := connectedFake(t, 10)
	fh := withHotbar(f, "eks [eks.yaml]")
	fh.exec["eks [eks.yaml]"] = engine.ExecRequired
	f.handlers["kubeconfig.pendingApprovals"] = func(context.Context, engine.Invocation) (any, error) {
		return nil, errors.New("no plugin in this fake")
	}
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the hotbar", func() bool { return len(tilesOf(sh.hot)) == 1 })
	test.Tap(tilesOf(sh.hot)[0])
	q.until(t, "the approval asked", func() bool { return len(f.callsOf("kubeconfig.pendingApprovals")) == 1 })
	q.drain()
	if n := len(f.callsOf("hotbar.openCluster")); n != 0 {
		t.Errorf("connected before the approval: %d", n)
	}
}

// The tile's menu: Connect, Move Up (not for the first), Move Down (not for the last), Remove from
// Hotbar, each through its command.
func TestHotbarTileMenu(t *testing.T) {
	sh, _, q, f, _ := hotbarScene(t, ThemeDark)
	tl := tilesOf(sh.hot)
	tl[0].TappedSecondary(&fyne.PointEvent{AbsolutePosition: fyne.NewPos(30, 100)})
	items := sh.hot.menuModel.Items
	labels := []string{}
	for _, it := range items {
		labels = append(labels, it.Label)
	}
	if strings.Join(labels, ",") != "Connect,,Move Up,Move Down,,Remove from Hotbar" {
		t.Fatalf("menu %v", labels)
	}
	if !items[0].Disabled || !items[2].Disabled || items[3].Disabled {
		t.Errorf("connected tile: Connect %v, Move Up %v, Move Down %v", items[0].Disabled, items[2].Disabled, items[3].Disabled)
	}
	items[3].Action() // Move Down
	q.until(t, "the move", func() bool { return tilesOf(sh.hot)[1].tile.Context == "kind-cogpit" })
	if p := decodeParams[hotbarMoveParams](t, f.callsOf("hotbar.moveCluster")[0]); p.Context != "kind-cogpit" || p.Position != 2 {
		t.Errorf("hotbar.moveCluster %+v", p)
	}
	last := tilesOf(sh.hot)[5]
	last.TappedSecondary(&fyne.PointEvent{})
	if !sh.hot.menuModel.Items[3].Disabled {
		t.Error("Move Down on the last tile")
	}
	sh.hot.menuModel.Items[5].Action() // Remove from Hotbar
	q.until(t, "the removal", func() bool { return len(tilesOf(sh.hot)) == 5 })
	if p := decodeParams[hotbarClusterParams](t, f.callsOf("hotbar.removeCluster")[0]); p.Context != "demo" {
		t.Errorf("hotbar.removeCluster %+v", p)
	}
}

// F1–F12 open the first twelve tiles: from the window when nothing has the focus, and from the
// table when it has; a key without a tile does nothing; a shifted F-key is not the hotbar's
// (⇧F1–⇧F12 showed a bank until D-83).
func TestHotbarFunctionKeys(t *testing.T) {
	sh, w, q, f, _ := hotbarScene(t, ThemeDark)
	w.Canvas().OnTypedKey()(&fyne.KeyEvent{Name: fyne.KeyF3})
	q.until(t, "F3", func() bool { return sh.ui.Context == "staging-us-east" })
	sh.view.table.TypedKey(&fyne.KeyEvent{Name: fyne.KeyF2})
	q.until(t, "F2 from the table", func() bool { return sh.ui.Context == "prod-eu-central" })
	if !sh.hot.key(fyne.KeyF12, false) {
		t.Error("F12 without a tile was passed on")
	}
	if sh.hot.key(fyne.KeyF2, true) || sh.hot.key(fyne.KeyA, false) {
		t.Error("⇧F2 or a letter taken by the hotbar")
	}
	q.drain()
	if n := len(f.callsOf("hotbar.openCluster")); n != 2 {
		t.Errorf("%d opens, want 2 (F12 has no tile, ⇧F2 is no hotbar key)", n)
	}
}

// The hotbar holds more than twelve tiles (D-83): F12 opens the twelfth, the thirteenth has no key
// in its tooltip and opens with a click; the bar scrolls, and the "+" is reachable below the last.
func TestHotbarPastTwelveScrolls(t *testing.T) {
	f := connectedFake(t, 10)
	var ids []string
	for i := range 20 {
		ids = append(ids, fmt.Sprintf("c%02d", i+1))
		f.contexts = append(f.contexts, engine.ContextInfo{Name: ids[i]})
	}
	withHotbar(f, ids...)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "twenty tiles", func() bool { return len(tilesOf(sh.hot)) == 20 && len(sh.kinds) > 0 })
	settle(w)
	tl := tilesOf(sh.hot)
	if got := tl[11].tipText(); !strings.HasSuffix(got, " · F12") {
		t.Errorf("the twelfth's tooltip %q", got)
	}
	if got := tl[12].tipText(); strings.Contains(got, " · F") {
		t.Errorf("the thirteenth's tooltip %q names a key", got)
	}
	s := sh.hot.scroll
	if s.Content.MinSize().Height <= s.Size().Height {
		t.Fatalf("twenty tiles fit: content %v, bar %v", s.Content.MinSize(), s.Size())
	}
	visible := func(o fyne.CanvasObject) bool {
		top, at := absolutePos(s).Y, absolutePos(o).Y
		return at >= top && at+o.Size().Height <= top+s.Size().Height
	}
	if visible(sh.hot.add) {
		t.Error("the + shows before the bar scrolled")
	}
	s.ScrollToBottom()
	if !visible(sh.hot.add) || !visible(tl[19]) {
		t.Errorf("after scrolling down the + at %v, the bar %v at %v", absolutePos(sh.hot.add), s.Size(), absolutePos(s))
	}
	sh.hot.key(fyne.KeyF12, false)
	q.until(t, "F12", func() bool { return sh.ui.Context == "c12" })
	test.Tap(tl[16])
	q.until(t, "the seventeenth", func() bool { return sh.ui.Context == "c17" })
	if p := decodeParams[hotbarSlotParams](t, f.callsOf("hotbar.openCluster")[1]); p.Slot != 17 {
		t.Errorf("hotbar.openCluster %+v", p)
	}
}

// "Add to Hotbar" is in a context's menu: the navigator's section rows (right-click) and the
// Cluster menu, as "Remove from Hotbar" when the hotbar holds the context.
func TestHotbarAddFromTheContextMenus(t *testing.T) {
	sh, _, q, f, fh := hotbarScene(t, ThemeDark)
	cluster := sh.mainMenu().Items[0]
	if cluster.Label != "Cluster" || cluster.Items[1].Label != "Remove from Hotbar" {
		t.Errorf("Cluster menu: %q %q", cluster.Label, cluster.Items[1].Label)
	}

	n := sh.side.sec
	q.until(t, "the section's rows", func() bool { return n.loaded && len(n.rows) == 5 })
	n.rows["demo"].TappedSecondary(&fyne.PointEvent{AbsolutePosition: fyne.NewPos(400, 300)}) // on the hotbar already
	if got := n.menuModel.Items[1].Label; got != "Remove from Hotbar" {
		t.Errorf("demo's menu: %q", got)
	}
	fh.setHotbar("prod-eu-central") // an agent unpinned the others
	q.until(t, "one tile", func() bool { return len(tilesOf(sh.hot)) == 1 })
	n.rows["staging-us-east"].TappedSecondary(&fyne.PointEvent{AbsolutePosition: fyne.NewPos(400, 300)})
	items := n.menuModel.Items
	if items[0].Label != "Connect" || items[1].Label != "Add to Hotbar" || items[1].Disabled {
		t.Fatalf("staging's menu: %q %q", items[0].Label, items[1].Label)
	}
	items[1].Action()
	q.until(t, "pinned", func() bool { return len(tilesOf(sh.hot)) == 2 })
	if p := decodeParams[hotbarClusterParams](t, f.callsOf("hotbar.addCluster")[0]); p.Context != "staging-us-east" {
		t.Errorf("hotbar.addCluster %+v", p)
	}
	// The Cluster menu follows: kind-cogpit is not on the hotbar any more.
	if got := sh.mainMenu().Items[0].Items[1].Label; got != "Add to Hotbar" {
		t.Errorf("Cluster menu: %q", got)
	}
}

// The parameters the bar sends are what the real registry accepts (it refuses unknown fields), and
// the bank commands are gone (D-83).
func TestHotbarParamsMatchTheRegistry(t *testing.T) {
	s := newTestEngine(engine.Options{Hotbar: engine.NewHotbar(t.TempDir())})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	for id, params := range map[string]any{
		"hotbar.list":          struct{}{},
		"hotbar.addCluster":    hotbarClusterParams{Context: "a"},
		"hotbar.removeCluster": hotbarClusterParams{Context: "a"},
		"hotbar.moveCluster":   hotbarMoveParams{Context: "a", Position: 2},
		"hotbar.openCluster":   hotbarSlotParams{Slot: 1},
	} {
		cmd, ok := s.Registry().Get(id)
		if !ok {
			t.Errorf("no command %s", id)
			continue
		}
		raw, _ := json.Marshal(params)
		var pe *engine.ParamsError
		if err := cmd.Enabled(s, raw); errors.As(err, &pe) {
			t.Errorf("%s %s: %v", id, raw, err)
		}
	}
	for _, id := range []string{"hotbar.switchBank", "hotbar.createBank", "hotbar.renameBank", "hotbar.removeBank"} {
		if _, ok := s.Registry().Get(id); ok {
			t.Errorf("%s is still registered", id)
		}
	}
}

func TestRenderHotbarEmptyDark(t *testing.T) {
	renderHotbarEmpty(t, ThemeDark, "hotbar_empty_dark.png")
}

// renderHotbarEmpty: a first start's hotbar, empty, the "+" to pin the connected cluster.
func renderHotbarEmpty(t *testing.T, mode ThemeMode, name string) {
	f := connectedFake(t, 10000)
	withHotbar(f)
	sh, w, q := newTestShell(t, f, Options{Theme: mode, MCPCommand: "claude mcp add cogpit -- cogpit-cli mcp"})
	q.until(t, "the hotbar", func() bool { return sh.hot.root.Visible() && len(sh.kinds) > 0 && sh.diag.total > 0 })
	savePNG(t, w, name)
}

func TestRenderHotbarTilesDark(t *testing.T) {
	renderHotbarTiles(t, ThemeDark, "hotbar_tiles_dark.png")
}
func TestRenderHotbarTilesLight(t *testing.T) {
	renderHotbarTiles(t, ThemeLight, "hotbar_tiles_light.png")
}

// renderHotbarTiles: six clusters, kind-cogpit connected, legacy-onprem missing with its tooltip
// shown; no bank switcher at the bar's foot (D-83).
func renderHotbarTiles(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, _, _ := hotbarScene(t, mode)
	q.until(t, "diagnostics", func() bool { return sh.diag.total > 0 })
	settle(w)
	missing := tilesOf(sh.hot)[3]
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(missing).AddXY(missing.Size().Width/2, 20)
	sh.tip.delay = time.Millisecond
	sh.tip.hover(missing, missing.tipText(), pos)
	q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() != "" })
	savePNG(t, w, name)
}
