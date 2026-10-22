package fyneui

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/terminal"
)

// The keyboard focus of the dock's tabs (the owner's alpha walk of 2026-10-22, AW5, AW6, AW8;
// dockfocus.go): a tab the user opens or shows from the window takes the keys on its main widget,
// a tab an agent opens leaves them where they are.

// editOnTab is the Edit tab the fake's object.edit opens.
const editOnTab = "edit-aw5"

// answerEdit makes the fake's object.edit open an Edit tab on the object, as the engine's does, and
// answer with the engine's EditResult (commands/edit.go).
func answerEdit(f *fakeEngine, ed *editorFake) {
	f.handlers["object.edit"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p refParams
		if err := json.Unmarshal(normalize(inv.Params), &p); err != nil {
			return nil, err
		}
		ref := p.Ref
		tab := engine.DockTab{ID: editOnTab, Type: engine.DockEdit, Title: "Pod " + ref.Name, Context: ref.Context, Ref: &ref}
		ed.setText(tab.ID, serviceYAML) // before the tab appears: the window reads it when it opens the tab
		f.update(func(u *engine.UIState) {
			u.Dock = append(u.Dock, tab)
			u.ActiveDock = tab.ID
		})
		return commands.EditResult{Tab: tab, Created: true, Text: serviceYAML, Revision: 1}, nil
	}
}

// editTabOf waits for an Edit tab's editor to hold its text.
func editTabOf(t testing.TB, sh *shell, q *mainQueue, id string) *editorTab {
	t.Helper()
	var et *editorTab
	q.until(t, "the Edit tab", func() bool {
		if c := sh.dock.contents[id]; c != nil {
			et, _ = c.editor.(*editorTab)
		}
		return et != nil && !et.busy && et.pane.view.Text() == serviceYAML
	})
	return et
}

// windowKeyRun is what the window does with one of its own keys (menukeys.go) when the focused
// widget does not take it: ⌘⇧T's terminal.open.
func windowKeyRun(t *testing.T, id string) func(*shell) {
	t.Helper()
	for _, k := range windowKeys {
		if k.id == id {
			return k.run
		}
	}
	t.Fatalf("no window key %s", id)
	return nil
}

// TestRowMenuEditGivesTheEditorTheKeys is the owner's AW5 and AW6: ⋮ › Edit opens the Edit tab with
// the keys in its editor, so that typing and ⌘F work at once. Before, the keys stayed in the list
// (Fyne keeps the focus of the table under the pointer through the ⋮ and the menu's overlay), and
// ⌘F, routed by where the keys are (D-106), focused the list's search instead of the tab's find bar.
func TestRowMenuEditGivesTheEditorTheKeys(t *testing.T) {
	f := kindsFake(t, kindPods)
	registerProbes(t, f)
	ed := newEditorFake(f)
	answerEdit(f, ed)
	sh, w, q := settledShell(t, f, kindPods)
	canvas := w.Canvas()

	m := openMenu(t, sh, q, 0)
	sh.view.menu.Hide()         // a tap on an item closes the menu before its action, as Fyne's menu does
	canvas.Focus(sh.view.table) // the keys in the list, as after a click on a row
	item(t, m, "Edit").Action()
	et := editTabOf(t, sh, q, editOnTab)
	q.until(t, "the editor to have the keys", func() bool { return canvas.Focused() == et.pane.view })

	test.Type(canvas.Focused(), "#") // typing goes into the editor at once
	if et.pane.view.Text() == serviceYAML {
		t.Error("typing did not reach the editor")
	}
	cmdF(t, sh) // AW6
	if !et.find.visible() || canvas.Focused() != et.find.find {
		t.Fatalf("⌘F after ⋮ › Edit: find bar %v, focus %T", et.find.visible(), canvas.Focused())
	}
}

// TestRowMenuLogsGiveTheLogTheKeys: ⋮ › Logs puts the keys on the log's lines (⌘A, ⌘C, "/" for its
// search), as a click on them does.
func TestRowMenuLogsGiveTheLogTheKeys(t *testing.T) {
	f := kindsFake(t, kindPods)
	registerProbes(t, f)
	sh, w, q := settledShell(t, f, kindPods)
	canvas := w.Canvas()

	m := openMenu(t, sh, q, 0)
	sh.view.menu.Hide()
	canvas.Focus(sh.view.table)
	if logs := item(t, m, "Logs"); logs.ChildMenu != nil {
		logs.ChildMenu.Items[0].Action() // the pod's first container
	} else {
		logs.Action()
	}
	var lt *logTab
	q.until(t, "the log to have the keys", func() bool {
		c := sh.dock.contents[sh.dock.active]
		if c != nil {
			lt = c.logs
		}
		return lt != nil && lt.view != nil && canvas.Focused() == lt.view.FocusTarget()
	})
}

// TestNewTerminalGetsTheKeys is the owner's AW8: ⌘⇧T, and the terminal chip in the dock's strip,
// open a terminal with the keys in it once its session runs.
func TestNewTerminalGetsTheKeys(t *testing.T) {
	skipTerminalUnderRace(t)
	f := connectedFake(t, 20)
	f.quietTerminals = true // their output would race the next test under the test driver (quietReader)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the list and the chip", func() bool { return sh.view.snap != nil && sh.dock.terminal.Visible() })
	canvas := w.Canvas()
	for _, way := range []struct {
		name string
		open func()
	}{
		{"⌘⇧T", func() { windowKeyRun(t, "terminal.open")(sh) }},
		{"the terminal chip", func() { test.Tap(sh.dock.terminal) }},
	} {
		canvas.Focus(sh.view.table)
		n := len(f.callsOf("terminal.open"))
		way.open()
		q.until(t, way.name+": a terminal with the keys", func() bool {
			c := sh.dock.contents[sh.dock.active]
			return len(f.callsOf("terminal.open")) == n+1 && c != nil && c.term != nil && canvas.Focused() == c.term.FocusTarget()
		})
	}
}

// TestStripTabClickGivesTheTabTheKeys: a click on a tab in the dock's strip, another one or the
// one shown, gives its editor the keys, as a click on a VS Code panel's tab does.
func TestStripTabClickGivesTheTabTheKeys(t *testing.T) {
	sh, w, q, f, ed, et := editScene(t, ThemeDark)
	ed.setText("create-2", "apiVersion: v1\nkind: ConfigMap\n")
	f.update(func(u *engine.UIState) { // a second tab, opened where the window did not ask for the keys
		u.Dock = append(u.Dock, engine.DockTab{ID: "create-2", Type: engine.DockCreate, Title: "Create resource", Context: "demo"})
		u.ActiveDock = "create-2"
	})
	var ct *createTab
	q.until(t, "the Create tab", func() bool {
		if c := sh.dock.contents["create-2"]; c != nil {
			ct, _ = c.editor.(*createTab)
		}
		return ct != nil && sh.dock.active == "create-2"
	})
	canvas := w.Canvas()
	tab := func(id string) *tabButton {
		for _, o := range sh.dock.tabRow.Objects {
			if b, ok := o.(*tabButton); ok && b.id == id {
				return b
			}
		}
		t.Fatalf("no tab %s in the strip", id)
		return nil
	}

	canvas.Focus(sh.view.table)
	test.Tap(tab("edit-1"))
	q.until(t, "the Edit tab's editor to have the keys", func() bool {
		return sh.dock.active == "edit-1" && canvas.Focused() == et.pane.view
	})
	canvas.Focus(sh.view.search)
	test.Tap(tab("edit-1")) // the tab shown: its editor takes the keys back
	q.until(t, "the keys back in the editor", func() bool { return canvas.Focused() == et.pane.view })
	test.Tap(tab("create-2"))
	q.until(t, "the Create tab's editor to have the keys", func() bool {
		return sh.dock.active == "create-2" && canvas.Focused() == ct.pane.view
	})
}

// TestAnAgentsTabLeavesTheKeys is the edge case the rule decides (D-115): a tab an agent opens or
// shows (over MCP, or the built-in agent) does not take the keys from where the user put them;
// the window sees only the state, never a result of its own.
func TestAnAgentsTabLeavesTheKeys(t *testing.T) {
	skipTerminalUnderRace(t)
	f := connectedFake(t, 20)
	f.quietTerminals = true
	ed := newEditorFake(f)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the list", func() bool { return sh.view.snap != nil })
	canvas := w.Canvas()
	canvas.Focus(sh.view.search) // the user types in the list's search

	ed.setText("edit-9", serviceYAML)
	r := checkoutRef
	f.update(func(u *engine.UIState) {
		u.Dock = append(u.Dock, engine.DockTab{ID: "edit-9", Type: engine.DockEdit, Title: "Service checkout", Context: "demo", Ref: &r})
		u.ActiveDock = "edit-9"
	})
	editTabOf(t, sh, q, "edit-9")
	f.update(func(u *engine.UIState) {
		u.Dock = append(u.Dock, engine.DockTab{ID: "terminal-10", Type: engine.DockTerminal, Title: "Terminal 10", Context: "demo"})
		u.ActiveDock = "terminal-10"
	})
	q.until(t, "the agent's terminal", func() bool { c := sh.dock.contents["terminal-10"]; return c != nil && c.term != nil })
	q.drain()
	if canvas.Focused() != sh.view.search {
		t.Fatalf("an agent's tabs took the keys: focus %T", canvas.Focused())
	}
}

// gatedTerminals holds every terminal session back until gate closes, as a pod shell's connection
// that takes its time.
type gatedTerminals struct {
	*fakeEngine
	gate chan struct{}
}

func (g gatedTerminals) OpenTerminal(ctx context.Context, id string) (terminal.Session, error) {
	select {
	case <-g.gate:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return g.fakeEngine.OpenTerminal(ctx, id)
}

// TestATerminalStillOpeningLeavesKeysMovedMeanwhile: a terminal whose session starts later takes
// the keys only if they are still where they were when the user opened it; keys the user moved
// meanwhile (a click into the list's search, typing there) stay where they are.
func TestATerminalStillOpeningLeavesKeysMovedMeanwhile(t *testing.T) {
	skipTerminalUnderRace(t)
	for _, moved := range []bool{false, true} {
		f := connectedFake(t, 20)
		f.quietTerminals = true
		eng := gatedTerminals{fakeEngine: f, gate: make(chan struct{})}
		var once sync.Once
		release := func() { once.Do(func() { close(eng.gate) }) }
		t.Cleanup(release)
		sh, w, q := startShell(t, eng, f, Options{})
		q.until(t, "the list", func() bool { return sh.view.snap != nil })
		canvas := w.Canvas()
		canvas.Focus(sh.view.table)
		answered := false // ⌘⇧T's dispatch (menukeys.go), with a done that runs after the window's own handling of the answer
		sh.dispatch("terminal.open", struct{}{}, func(any, error) { answered = true })
		q.until(t, "the tab, its session opening", func() bool {
			c := sh.dock.contents[sh.dock.active]
			return answered && c != nil && c.tab.Type == engine.DockTerminal && c.term == nil
		})
		if moved {
			canvas.Focus(sh.view.search)
		}
		release()
		var c *dockContent
		q.until(t, "the session", func() bool { c = sh.dock.contents[sh.dock.active]; return c != nil && c.term != nil })
		q.drain()
		want := c.term.FocusTarget()
		if moved {
			want = sh.view.search
		}
		if got := canvas.Focused(); got != want {
			t.Errorf("keys moved meanwhile %v: focus %T, want %T", moved, got, want)
		}
	}
}

// spyCanvas records what the window's canvas is asked to repaint and to focus.
type spyCanvas struct {
	fyne.Canvas
	refreshed []fyne.CanvasObject
	focused   []fyne.Focusable
}

func (c *spyCanvas) Refresh(o fyne.CanvasObject) {
	c.refreshed = append(c.refreshed, o)
	c.Canvas.Refresh(o)
}

func (c *spyCanvas) Focus(o fyne.Focusable) {
	c.focused = append(c.focused, o)
	c.Canvas.Focus(o)
}

// spyWindow is a window whose canvas is a spyCanvas.
type spyWindow struct {
	fyne.Window
	canvas *spyCanvas
}

func (w spyWindow) Canvas() fyne.Canvas { return w.canvas }

// TestFindBarOpensThroughTheWindowsCanvas is AW6's cause in a real window, which Fyne's test driver
// does not model: in Fyne 2.8.2's desktop driver Container.Show sets the bar visible without asking
// for a repaint (container.go), and CanvasForObject is nil for a field that was never drawn (the
// canvas registers an object when a frame walks the visible tree, internal/driver/common/canvas.go),
// so the first ⌘F neither repainted the bar nor focused its field: the bar showed at the next
// repaint something else asked for, seconds later, and the keys stayed in the editor. The bar now
// asks the window's own canvas for both.
func TestFindBarOpensThroughTheWindowsCanvas(t *testing.T) {
	sh, w, _, _, _, et := editScene(t, ThemeDark)
	spy := &spyCanvas{Canvas: w.Canvas()}
	sh.win = spyWindow{Window: w, canvas: spy}
	et.find.open()
	if !slices.Contains(spy.refreshed, et.find.root) {
		t.Error("opening the find bar asked the window for no repaint of it")
	}
	if !slices.Contains(spy.focused, fyne.Focusable(et.find.find)) {
		t.Error("opening the find bar did not focus its field through the window's canvas")
	}
}

// TestRestoredDraftGetsTheKeys: Restore on the drafts banner opens the draft's Edit tab with the
// keys in its editor, as ⋮ › Edit does; its answer is the tab's EditorState, which the window's
// own editor reads (editor.get) answer too, so it asks the keys itself (drafts.go).
func TestRestoredDraftGetsTheKeys(t *testing.T) {
	sh, w, q, f, d := draftsScene(t, ThemeDark)
	ed := newEditorFake(f)
	ref := checkoutRef
	tab := engine.DockTab{ID: "edit-7", Type: engine.DockEdit, Title: "Service checkout", Context: "demo", Ref: &ref}
	f.handlers["editor.restoreDraft"] = func(context.Context, engine.Invocation) (any, error) {
		ed.setText(tab.ID, serviceYAML)
		f.update(func(u *engine.UIState) {
			u.Dock = append(u.Dock, tab)
			u.ActiveDock = tab.ID
		})
		return engine.EditorState{Tab: tab, Text: serviceYAML, Revision: 1}, nil
	}
	d.set(fixtureDrafts())
	q.until(t, "the banner", func() bool { return sh.drafts.root.Visible() && len(sh.drafts.drafts) == 2 })
	canvas := w.Canvas()
	test.Tap(draftsButtons(sh.drafts)["Restore"][0])
	et := editTabOf(t, sh, q, tab.ID)
	q.until(t, "the restored editor to have the keys", func() bool { return canvas.Focused() == et.pane.view })
}

// TestOpenedTabResults: the engine's answers that name the tab they opened or showed hand it to
// the window; the editor's state does not, since the window's own editor.get answers with it at
// every load of an editor tab and an agent's text (editorPane.remote).
func TestOpenedTabResults(t *testing.T) {
	attach := engine.DockTab{ID: "attach-3"}
	for _, res := range []any{commands.DockResult{Tab: attach}, commands.EditResult{Tab: attach}, commands.DebugResult{Tab: &attach}} {
		if o, ok := res.(openedTab); !ok || o.OpenedTab().ID != attach.ID {
			t.Errorf("%T does not name its tab", res)
		}
	}
	if (commands.DebugResult{}).OpenedTab().ID != "" { // the watch did not show the container in time: no tab
		t.Error("a debug container without its Attach tab names one")
	}
	if _, ok := any(engine.EditorState{}).(openedTab); ok {
		t.Error("editor.get's answer would ask the keys at every load")
	}
}
