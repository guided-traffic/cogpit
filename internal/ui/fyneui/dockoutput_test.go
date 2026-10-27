package fyneui

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// readThroughWindow reads a tab as the engine does, through the window's reader, while the test
// runs the main queue.
func readThroughWindow(t *testing.T, sh *shell, q *mainQueue, tab engine.DockTab, query engine.DockOutputQuery) engine.DockOutput {
	t.Helper()
	type answer struct {
		out engine.DockOutput
		err error
	}
	done := make(chan answer, 1)
	go func() {
		out, err := dockOutputReader{sh}.ReadDockOutput(context.Background(), tab, query)
		done <- answer{out, err}
	}()
	var a answer
	q.until(t, "the window's answer", func() bool {
		select {
		case a = <-done:
			return true
		default:
			return false
		}
	})
	if a.err != nil {
		t.Fatal(a.err)
	}
	return a.out
}

// TestDockOutputOfALogsTab: a logs tab's output is the lines its view shows: the filter applied,
// resource names and timestamps as the tab shows them (in the user's format), and all of them
// again once the filter is off; held, shown and the status as the tab has them.
func TestDockOutputOfALogsTab(t *testing.T) {
	sh, _, q, f := logsScene(t, ThemeDark, true, engine.LogOptions{ResourceNames: true, Timestamps: true, Search: "payment", Filter: true})
	tab := f.ui.Dock[0]
	q.until(t, "the filter", func() bool { lt := logTabOf(sh); return lt.view.View().Filter })
	out := readThroughWindow(t, sh, q, tab, engine.DockOutputQuery{Limit: engine.DefaultDockLines})
	if len(out.Lines) != 2 || out.Shown != 2 || out.Held != len(demoMerged) || out.More || out.Opening {
		t.Fatalf("lines %q, shown %d, held %d, more %v, opening %v", out.Lines, out.Shown, out.Held, out.More, out.Opening)
	}
	for _, line := range out.Lines {
		if !strings.HasPrefix(line, "checkout-api-7d9f8b6c4-q8w3n/api ") || !strings.Contains(line, "payment provider") ||
			strings.Contains(line, "2026-10-11T") {
			t.Errorf("line %q: not as the tab shows it (names, the user's time format, the filter)", line)
		}
	}
	if out.Status != logTabOf(sh).view.StatusText() || !strings.Contains(out.Status, "2 of 9 lines") {
		t.Errorf("status %q", out.Status)
	}

	f.update(func(u *engine.UIState) { u.Dock[0].Logs = engine.LogOptions{} })
	q.until(t, "the options", func() bool { return !logTabOf(sh).view.View().Filter })
	all := sh.dock.output(tab, engine.DockOutputQuery{Limit: 3})
	if len(all.Lines) != 3 || all.Shown != len(demoMerged) || !all.More || all.Lines[2] != "error budget burn rate 0.4 (ok)" {
		t.Fatalf("newest three without the filter: %q shown %d more %v", all.Lines, all.Shown, all.More)
	}
	older := sh.dock.output(tab, engine.DockOutputQuery{Before: &all.First, Limit: 100})
	if len(older.Lines) != len(demoMerged)-3 || older.More || older.Last >= all.First {
		t.Errorf("before %d: %d lines up to %d, more %v", all.First, len(older.Lines), older.Last, older.More)
	}

	unknown := engine.DockTab{ID: "logs-9", Type: engine.DockLogs}
	if out := sh.dock.output(unknown, engine.DockOutputQuery{Limit: 10}); !out.Opening || len(out.Lines) != 0 {
		t.Errorf("a tab the window does not show: %+v", out)
	}
	if sh.dock.agentRead.Visible() {
		t.Error("the agents switch shows on a logs tab")
	}
}

// withAgentReadCommand answers dock.allowAgentRead as the engine does.
func withAgentReadCommand(f *fakeEngine) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers["dock.allowAgentRead"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p allowAgentReadParams
		if err := json.Unmarshal(inv.Params, &p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) {
			for i := range u.Dock {
				if u.Dock[i].ID == p.ID {
					u.Dock[i].AgentRead = p.Allowed
				}
			}
		})
		return nil, nil
	}
}

// TestDockOutputOfATerminalTab: a terminal tab's output is its screen as text, without escape
// sequences; the strip's switch shows for it, off, asks the engine from the GUI when the user
// turns it on, and follows the engine's state.
func TestDockOutputOfATerminalTab(t *testing.T) {
	skipTerminalUnderRace(t)
	sh, _, q, f := kubectlScene(t, ThemeDark, nil)
	withAgentReadCommand(f)
	tab := f.ui.Dock[0]
	out := readThroughWindow(t, sh, q, tab, engine.DockOutputQuery{Limit: engine.DefaultDockLines})
	want := []string{"~ $ kubectl version --client", "Client Version: v1.30.2", "Kustomize Version: v5.0.4-0.20230601165947-6ce0bf390ce3", "~ $"}
	if !slices.Equal(out.Lines, want) || out.Shown != 4 || out.First != 1 || out.Last != 4 || out.Opening || out.Ended {
		t.Fatalf("screen %q (shown %d, %d..%d, opening %v, ended %v), want %q", out.Lines, out.Shown, out.First, out.Last, out.Opening, out.Ended, want)
	}

	sw := sh.dock.agentRead
	if !sw.Visible() || sw.Checked || sw.Text != agentReadLabel {
		t.Fatalf("the switch: visible %v, checked %v, %q", sw.Visible(), sw.Checked, sw.Text)
	}
	test.Tap(sw)
	q.until(t, "the engine's answer", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.ui.Dock[0].AgentRead
	})
	calls := f.callsOf("dock.allowAgentRead")
	if len(calls) != 1 || calls[0].Origin != engine.OriginGUI {
		t.Fatalf("calls %+v", calls)
	}
	var p allowAgentReadParams
	if err := json.Unmarshal(calls[0].Params, &p); err != nil || p.ID != "terminal-1" || !p.Allowed {
		t.Errorf("params %s", calls[0].Params)
	}
	f.update(func(u *engine.UIState) { u.Dock[0].AgentRead = false }) // turned off elsewhere
	deadline := time.Now().Add(5 * time.Second)
	for sw.Checked && time.Now().Before(deadline) {
		q.drain()
		time.Sleep(time.Millisecond)
	}
	if sw.Checked || len(f.callsOf("dock.allowAgentRead")) != 1 {
		t.Errorf("the switch did not follow the engine (checked %v), or dispatched it back", sw.Checked)
	}
}

// The strip with a terminal tab whose output agents may read.
func TestRenderDockAgentReadDark(t *testing.T) {
	skipTerminalUnderRace(t)
	sh, w, q, f := kubectlScene(t, ThemeDark, nil)
	f.update(func(u *engine.UIState) { u.Dock[0].AgentRead = true })
	q.until(t, "the switch", func() bool { return sh.dock.agentRead.Checked })
	savePNG(t, w, "dock_agent_read_dark.png")
}

// agentReadButton returns the strip's button of a tab. Main goroutine (the test's, under the queue).
func agentReadButton(t *testing.T, sh *shell, id string) *tabButton {
	t.Helper()
	b, ok := sh.dock.agentReadMarks.buttons[id]
	if !ok {
		t.Fatalf("no tab button for %s", id)
	}
	return b
}

// TestAgentReadMarkerFlashes (H-83): a terminal tab agents may read carries the marker; an agent's
// read (EventDockRead) flashes it, a later read starts the flash again, and it ends by itself; a
// tab without the switch neither shows the marker nor flashes. When the switch turned itself off,
// the marker goes and the strip says so beside the switch, until it is on again.
func TestAgentReadMarkerFlashes(t *testing.T) {
	skipTerminalUnderRace(t)
	sh, _, q, f := kubectlScene(t, ThemeDark, nil)
	if b := agentReadButton(t, sh, "terminal-1"); b.agentRead || b.mark.img.Visible() {
		t.Fatal("the marker shows before the switch is on")
	}
	f.emit(engine.Event{Type: engine.EventDockRead, View: "terminal-1"})
	q.drain()
	if agentReadButton(t, sh, "terminal-1").flash {
		t.Error("a tab without the switch flashed")
	}

	f.update(func(u *engine.UIState) { u.Dock[0].AgentRead = true })
	q.until(t, "the marker", func() bool { b := agentReadButton(t, sh, "terminal-1"); return b.agentRead && b.mark.img.Visible() })
	sh.dock.agentReadMarks.flashFor = time.Hour
	f.emit(engine.Event{Type: engine.EventDockRead, View: "terminal-1"})
	q.until(t, "the flash", func() bool { return agentReadButton(t, sh, "terminal-1").flash })
	sh.dock.agentReadMarks.flashFor = 20 * time.Millisecond
	f.emit(engine.Event{Type: engine.EventDockRead, View: "terminal-1"}) // a read during the flash
	q.until(t, "the flash's end", func() bool { return !agentReadButton(t, sh, "terminal-1").flash })
	if b := agentReadButton(t, sh, "terminal-1"); !b.agentRead || !b.mark.img.Visible() {
		t.Error("the marker went with the flash")
	}

	f.update(func(u *engine.UIState) { u.Dock[0].AgentRead, u.Dock[0].AgentReadExpired = false, true })
	q.until(t, "the note", func() bool { return sh.dock.agentReadMarks.note.Visible() })
	if b := agentReadButton(t, sh, "terminal-1"); b.agentRead || b.mark.img.Visible() || sh.dock.agentRead.Checked {
		t.Error("the marker or the switch stayed after the switch turned itself off")
	}
	if sh.dock.agentReadMarks.note.text != agentReadExpiredNote {
		t.Errorf("note %q", sh.dock.agentReadMarks.note.text)
	}
	// Ended at its hour (A3): the note says so.
	f.update(func(u *engine.UIState) { u.Dock[0].AgentReadCapped = true })
	q.until(t, "the hour's note", func() bool { return sh.dock.agentReadMarks.note.text == agentReadCappedNote })
	f.update(func(u *engine.UIState) {
		u.Dock[0].AgentRead, u.Dock[0].AgentReadExpired, u.Dock[0].AgentReadCapped = true, false, false
	})
	q.until(t, "the note gone", func() bool { return !sh.dock.agentReadMarks.note.Visible() })
}

// The strip while an agent's read flashes the tab, and after the switch turned itself off: the
// PNGs of the pull request (AGENTS.md rule 7).
func TestRenderDockAgentReadFlashDark(t *testing.T) { renderDockAgentReadFlash(t, ThemeDark) }
func renderDockAgentReadFlash(t *testing.T, mode ThemeMode) {
	skipTerminalUnderRace(t)
	sh, w, q, f := kubectlScene(t, mode, nil)
	f.update(func(u *engine.UIState) { u.Dock[0].AgentRead = true })
	q.until(t, "the marker", func() bool { return agentReadButton(t, sh, "terminal-1").agentRead })
	sh.dock.agentReadMarks.flashFor = time.Hour
	f.emit(engine.Event{Type: engine.EventDockRead, View: "terminal-1"})
	q.until(t, "the flash", func() bool { return agentReadButton(t, sh, "terminal-1").flash })
	savePNG(t, w, "dock_agent_read_flash_"+string(mode)+".png")
}

func TestRenderDockAgentReadExpiredDark(t *testing.T) { renderDockAgentReadExpired(t, ThemeDark) }
func renderDockAgentReadExpired(t *testing.T, mode ThemeMode) {
	skipTerminalUnderRace(t)
	sh, w, q, f := kubectlScene(t, mode, nil)
	f.update(func(u *engine.UIState) { u.Dock[0].AgentReadExpired = true })
	q.until(t, "the note", func() bool { return sh.dock.agentReadMarks.note.Visible() })
	savePNG(t, w, "dock_agent_read_expired_"+string(mode)+".png")
}

// TestAgentReadSwitchArmsAfterATabChange: right after the switch began to show a tab (an agent can
// make another terminal tab the active one just before a click lands), turning it on does nothing
// for the confirmations' arm delay; turning it off always works.
func TestAgentReadSwitchArmsAfterATabChange(t *testing.T) {
	skipTerminalUnderRace(t)
	sh, _, q, f := kubectlScene(t, ThemeDark, nil)
	withAgentReadCommand(f)
	sh.confirmer.armDelay = time.Hour
	sw := sh.dock.agentRead
	test.Tap(sw)
	q.drain()
	if sw.Checked || len(f.callsOf("dock.allowAgentRead")) != 0 {
		t.Fatalf("turned on within the arm delay: checked %v, calls %d", sw.Checked, len(f.callsOf("dock.allowAgentRead")))
	}
	sh.dock.agentReadSince = time.Now().Add(-2 * time.Hour) // the delay has passed
	test.Tap(sw)
	q.until(t, "the engine's answer", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.ui.Dock[0].AgentRead
	})
	sh.dock.agentReadSince = time.Now() // turning off is never held back
	test.Tap(sw)
	q.until(t, "the switch off", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return !f.ui.Dock[0].AgentRead
	})
}
