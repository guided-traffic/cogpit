package fyneui

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/terminal"
)

// saveKey is ⌘S (Ctrl+S on Windows and Linux) as Fyne's desktop driver hands it to the focused
// widget when no main-menu item has it.
var saveKey = &desktop.CustomShortcut{KeyName: fyne.KeyS, Modifier: fyne.KeyModifierShortcutDefault}

// typeFocused types chars as the desktop driver does: to the focused widget, or to the window's
// handler when nothing has the focus (Fyne's test.TypeOnCanvas types to the window's alone).
func typeFocused(c fyne.Canvas, chars string) {
	for _, r := range chars {
		if f := c.Focused(); f != nil {
			f.TypedRune(r)
		} else if fn := c.OnTypedRune(); fn != nil {
			fn(r)
		}
	}
}

// logBarrier waits until every dispatch the window made in order before it has finished (the
// keys' logs.setOptions and logs.jumpTo go out in order), so that a test can say a key dispatched
// nothing.
func logBarrier(t *testing.T, sh *shell, q *mainQueue) {
	t.Helper()
	done := false
	sh.dispatchInOrder("logs.sources", dockParams{ID: "logs-1"}, func(any, error) { done = true })
	q.until(t, "the dispatches before", func() bool { return done })
}

// jumpTimes are the times of the logs.jumpTo calls, in the order made.
func jumpTimes(t *testing.T, f *fakeEngine) []string {
	t.Helper()
	var out []string
	for _, c := range f.callsOf("logs.jumpTo") {
		var p logJumpParams
		if err := json.Unmarshal(c.Params, &p); err != nil {
			t.Fatal(err)
		}
		out = append(out, p.Time)
	}
	return out
}

// TestLogTabKeys: while the lines have the keyboard, t and w toggle Timestamps and Wrap through
// the engine (logs.setOptions), a quick t t ending as it began; 2 to 6 jump to 1, 5, 15, 30
// minutes and an hour ago (logs.jumpTo), loading older lines as the time field does; 0 follows
// the newest line again; a key the tab does not take dispatches nothing; / puts the keys into the
// search.
func TestLogTabKeys(t *testing.T) {
	sh, w, q, f := logsScene(t, ThemeDark, true, engine.LogOptions{ResourceNames: true})
	lt := logTabOf(sh)
	c := w.Canvas()
	q.until(t, "every line", func() bool { return len(lt.view.ShownLines()) == len(demoMerged) })
	c.Focus(lt.view.FocusTarget())

	typeFocused(c, "t")
	q.until(t, "timestamps from the engine", func() bool { return lt.view.View().Timestamps && lt.timestamps.Checked })
	if calls := f.callsOf("logs.setOptions"); len(calls) != 1 || string(calls[0].Params) != `{"id":"logs-1","timestamps":true}` {
		t.Errorf("t: calls %v", calls)
	}
	typeFocused(c, "w")
	q.until(t, "wrap from the engine", func() bool { return lt.view.View().Wrap && lt.wrap.Checked })
	if calls := f.callsOf("logs.setOptions"); len(calls) != 2 || string(calls[1].Params) != `{"id":"logs-1","wrap":true}` {
		t.Errorf("w: calls %v", calls)
	}
	typeFocused(c, "tt") // in quick succession: written in the order typed
	logBarrier(t, sh, q)
	q.until(t, "timestamps as before", func() bool { return lt.view.View().Timestamps && lt.timestamps.Checked })
	if calls := f.callsOf("logs.setOptions"); len(calls) != 4 || !strings.Contains(string(calls[2].Params), `"timestamps":false`) ||
		!strings.Contains(string(calls[3].Params), `"timestamps":true`) {
		t.Errorf("t t: calls %v", calls)
	}

	// 1m ago is after every line (the fake's clock is 14:30): the tab says so; 5m ago is before the
	// oldest line: the lines since then come first and the jump finds the oldest.
	toasts := len(sh.toasts.Objects)
	typeFocused(c, "2")
	q.until(t, "the message", func() bool { return len(sh.toasts.Objects) > toasts && lt.jump.seen == 1 })
	typeFocused(c, "3")
	q.until(t, "the older line", func() bool { return strings.Contains(lt.view.JumpedLine(), "loading configuration") })
	if reqs := olderOf(sh).requests(); len(reqs) != 1 || !reqs[0].Since.Equal(fixedNow.Add(-5*time.Minute)) {
		t.Errorf("older requests %+v", reqs)
	}
	typeFocused(c, "456")
	q.until(t, "the jumps", func() bool { return lt.jump.seen == 5 && !lt.older.busy })
	if got := jumpTimes(t, f); !slices.Equal(got, []string{"1m ago", "5m ago", "15m ago", "30m ago", "1h ago"}) {
		t.Errorf("jumps %q", got)
	}
	if lt.view.Following() {
		t.Fatal("the tab follows after a jump")
	}
	typeFocused(c, "0")
	if !lt.view.Following() {
		t.Error("0 did not follow the newest line")
	}

	options, jumps := len(f.callsOf("logs.setOptions")), len(f.callsOf("logs.jumpTo"))
	typeFocused(c, "x1T")
	logBarrier(t, sh, q)
	if len(f.callsOf("logs.setOptions")) != options || len(f.callsOf("logs.jumpTo")) != jumps {
		t.Error("a key the tab does not take dispatched")
	}

	typeFocused(c, "/")
	if c.Focused() != lt.search {
		t.Fatalf("/ focused %v, want the search", c.Focused())
	}
}

// TestLogTabKeysLeaveTextFields: the tab's search and time field type the keys as text.
func TestLogTabKeysLeaveTextFields(t *testing.T) {
	sh, w, q, f := logsScene(t, ThemeDark, false, engine.LogOptions{})
	lt := logTabOf(sh)
	c := w.Canvas()
	sh.searchWait = time.Hour // the typing's pause never ends: the search sends nothing
	c.Focus(lt.search)
	typeFocused(c, "tw20")
	c.Focus(lt.jumpField)
	typeFocused(c, "t2")
	logBarrier(t, sh, q)
	if lt.search.Text != "tw20" || lt.jumpField.Text != "t2" {
		t.Errorf("search %q, time field %q", lt.search.Text, lt.jumpField.Text)
	}
	if n, m := len(f.callsOf("logs.setOptions")), len(f.callsOf("logs.jumpTo")); n != 0 || m != 0 || lt.timestamps.Checked || lt.wrap.Checked {
		t.Errorf("%d setOptions and %d jumpTo calls from a text field", n, m)
	}
}

// TestLogTabSaveKey: ⌘S (Ctrl+S) on the lines saves what the tab shows, as the Download button
// does; no main-menu item has the key, which Fyne would ask first.
func TestLogTabSaveKey(t *testing.T) {
	sh, w, q, _ := logsScene(t, ThemeDark, true, engine.LogOptions{ResourceNames: true})
	lt := logTabOf(sh)
	if m := w.MainMenu(); m != nil {
		for _, menu := range m.Items {
			for _, it := range menuItemsDeep(menu.Items) {
				if it.Shortcut != nil && it.Shortcut.ShortcutName() == saveKey.ShortcutName() {
					t.Errorf("the main menu's %q has the logs tab's save key", it.Label)
				}
			}
		}
	}
	p := installPicker(sh)
	p.savePath = filepath.Join(t.TempDir(), "checkout.log")
	w.Canvas().Focus(lt.view.FocusTarget())
	w.Canvas().Focused().(fyne.Shortcutable).TypedShortcut(saveKey)
	untilSaved(t, q, sh, "checkout.log")
	if o := p.lastSave(t); o.Name != "Deployment_checkout-api-demo.log" {
		t.Errorf("save panel %+v", o)
	}
	if want := "(" + shortcutText(logSaveKey, runtime.GOOS == goosMac) + ")"; !strings.HasSuffix(lt.download.tip, want) {
		t.Errorf("the download's tooltip %q does not name %s", lt.download.tip, want)
	}
}

// menuItemsDeep lists a menu's items and those of their submenus.
func menuItemsDeep(items []*fyne.MenuItem) []*fyne.MenuItem {
	var out []*fyne.MenuItem
	for _, it := range items {
		out = append(out, it)
		if it.ChildMenu != nil {
			out = append(out, menuItemsDeep(it.ChildMenu.Items)...)
		}
	}
	return out
}

// TestLogTabKeyHintsAreTheKeys: every key the overview lists for a logs tab is one the tab takes,
// and ⌘S is written as the platform writes it.
func TestLogTabKeyHintsAreTheKeys(t *testing.T) {
	sh, _, _, _ := logsScene(t, ThemeDark, false, engine.LogOptions{})
	lt := logTabOf(sh)
	sh.files = &fakePicker{sh: sh} // ⌘S below opens no panel
	for _, mac := range []bool{true, false} {
		hints := logTabKeyHints(mac)
		keys := map[string]bool{}
		for _, h := range hints {
			keys[h.Keys] = true
			if h.Label == "" {
				t.Errorf("%q has no label", h.Keys)
			}
		}
		save := map[bool]string{true: "⌘S", false: "Ctrl+S"}[mac]
		for _, k := range []string{"t", "w", "2–6", "0", "/", save} {
			if !keys[k] {
				t.Errorf("mac %v: no hint for %s in %+v", mac, k, hints)
			}
		}
	}
	lt.view.SetFollow(false)
	for _, r := range "tw023456" {
		if !lt.typedRune(r) {
			t.Errorf("the tab does not take %q", r)
		}
	}
	if !lt.typedShortcut(saveKey) || lt.typedShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyS, Modifier: fyne.KeyModifierAlt}) {
		t.Error("the tab's shortcuts are not ⌘S alone")
	}
}

// typingTerminals is logsEngine whose terminal sessions record what is typed into them.
type typingTerminals struct {
	logsEngine
	typed *typedText
}

// typedText is what a terminal session was sent.
type typedText struct {
	mu sync.Mutex
	b  strings.Builder
}

func (x *typedText) String() string {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.b.String()
}

// typedStream is a fake session that records its input.
type typedStream struct {
	*fakeStream
	typed *typedText
}

func (s typedStream) Write(p []byte) (int, error) {
	s.typed.mu.Lock()
	defer s.typed.mu.Unlock()
	return s.typed.b.Write(p)
}

func (e typingTerminals) OpenTerminal(ctx context.Context, id string) (terminal.Session, error) {
	s, err := e.fakeEngine.OpenTerminal(ctx, id)
	if err != nil {
		return nil, err
	}
	return typedStream{fakeStream: s.(*fakeStream), typed: e.typed}, nil
}

// TestLogTabKeysStayOutOfTerminals: a terminal tab beside a logs tab gets every key typed into it,
// the logs tab's keys and Ctrl+S included; the logs tab acts on none.
func TestLogTabKeysStayOutOfTerminals(t *testing.T) {
	skipTerminalUnderRace(t)
	f := connectedFake(t, 200)
	f.quietTerminals = true // its output would race the next test under the test driver (quietReader)
	pod := model.ObjectRef{Context: "demo", Kind: kindPods, Namespace: "checkout", Name: "checkout-api-7d9f8b6c4-x2k9p"}
	withLogCommands(t, f, model.ObjectRef{Context: "demo", Kind: "deployments", Namespace: "checkout", Name: "checkout-api"})
	f.ui.Dock = []engine.DockTab{
		{ID: "logs-1", Type: engine.DockLogs, Title: "Logs: " + pod.Name + "/api", Context: "demo", Ref: &pod, Container: "api"},
		{ID: "terminal-1", Type: engine.DockTerminal, Title: "Terminal 1", Context: "demo"},
	}
	f.ui.ActiveDock = "logs-1"
	typed := &typedText{}
	sh, w, q := startShell(t, typingTerminals{logsEngine: newLogsEngine(f), typed: typed}, f, Options{})
	q.until(t, "the log lines", func() bool { lt := logTabOf(sh); return lt != nil && lt.view != nil && len(lt.view.ShownLines()) == 3 })
	lt := logTabOf(sh)
	p := installPicker(sh)
	f.update(func(u *engine.UIState) { u.ActiveDock = "terminal-1" })
	var target fyne.Focusable
	q.until(t, "the terminal", func() bool {
		if c := sh.dock.contents["terminal-1"]; c != nil && c.term != nil {
			target = c.term.FocusTarget()
		}
		return target != nil && sh.dock.active == "terminal-1"
	})
	c := w.Canvas()
	c.Focus(target)
	typeFocused(c, "tw20/")
	c.Focused().(fyne.Shortcutable).TypedShortcut(saveKey)
	q.until(t, "the keys in the terminal", func() bool { return strings.HasPrefix(typed.String(), "tw20/") })
	logBarrier(t, sh, q)
	if n, m := len(f.callsOf("logs.setOptions")), len(f.callsOf("logs.jumpTo")); n != 0 || m != 0 {
		t.Errorf("the logs tab dispatched %d setOptions and %d jumpTo", n, m)
	}
	p.mu.Lock()
	saves := len(p.saves)
	p.mu.Unlock()
	if saves != 0 || lt.timestamps.Checked || lt.wrap.Checked || c.Focused() != target {
		t.Errorf("saves %d, timestamps %v, wrap %v, focus %v", saves, lt.timestamps.Checked, lt.wrap.Checked, c.Focused())
	}
}
