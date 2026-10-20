package fyneui

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/terminal"
	"github.com/guided-traffic/cogpit/internal/ui/fyneui/termview"
)

// The owner's GW7 (gate walk of 2026-10-20): zsh highlights pasted text until Enter, and Cogpit's
// terminal drew the highlight as broken blocks, every other character without it. zsh highlights a
// paste with reverse video (SGR 7); with oh-my-zsh and zsh-syntax-highlighting it sends SGR 7 again
// before every character, which the terminal widget took as a toggle (third_party/fyne-terminal,
// TestReverseVideo). The render shows such a paste of three lines, as zsh 5.9 sent it in a
// pseudo-terminal on 2026-10-20 (the prompt shortened), above a command run before it.

// highlightedPaste is what zsh sends for a line of a highlighted paste with oh-my-zsh and
// zsh-syntax-highlighting: SGR 7 before every character, SGR 27 after the last.
func highlightedPaste(line string) string {
	var b strings.Builder
	for _, r := range line {
		b.WriteString("\x1b[7m")
		b.WriteRune(r)
	}
	b.WriteString("\x1b[27m")
	return b.String()
}

const pastePrompt = "\x1b[01;32m➜  \x1b[36m~\x1b[00m "

var pasteTerminalOutput = pastePrompt + "kubectl get ns\r\n" +
	"NAME              STATUS   AGE\r\n" +
	"default           Active   12d\r\n" +
	"kube-system       Active   12d\r\n" +
	"shop              Active   3d\r\n" +
	pastePrompt + "\x1b[K\x1b[?2004h" +
	highlightedPaste("kubectl get ns") + "\r\r\n" +
	highlightedPaste("kubectl get pods -A | head -3") + "\r\r\n" +
	highlightedPaste("kubectl get nodes")

// pasteEngine is the fake engine with a terminal that shows pasteTerminalOutput.
type pasteEngine struct {
	*fakeEngine
	last *atomic.Pointer[openStream]
}

func (p pasteEngine) OpenTerminal(ctx context.Context, tabID string) (terminal.Session, error) {
	if !strings.HasPrefix(tabID, "terminal-") {
		return p.fakeEngine.OpenTerminal(ctx, tabID)
	}
	p.mu.Lock()
	p.opened[tabID]++
	p.mu.Unlock()
	s := newGatedStream(p.fakeEngine, tabID, pasteTerminalOutput)
	p.last.Store(s)
	return s, nil
}

// pasteScene is the pods view with a terminal tab in the dock showing a highlighted paste.
func pasteScene(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *termview.Terminal) {
	t.Helper()
	f := connectedFake(t, 200)
	f.quietTerminals = true
	f.ui.Context, f.ui.Connection = "kind-cogpit", engine.Connection{State: engine.Connected}
	f.ui.Dock = []engine.DockTab{{ID: "terminal-1", Type: engine.DockTerminal, Title: "Terminal 1", Context: "kind-cogpit"}}
	f.ui.ActiveDock = "terminal-1"
	eng := pasteEngine{fakeEngine: f, last: &atomic.Pointer[openStream]{}}
	sh, w, q := startShell(t, eng, f, Options{Theme: mode})
	// As attachScene: the output flows only once the widget waits in Read, and the test goroutine
	// touches no widget until the widget has processed it all.
	q.until(t, "the terminal to read", func() bool {
		s := eng.last.Load()
		return len(sh.kinds) > 0 && s != nil && s.reading.Load()
	})
	q.drain()
	s := eng.last.Load()
	close(s.gate)
	deadline := time.Now().Add(5 * time.Second)
	for !s.idle.Load() {
		if time.Now().After(deadline) {
			t.Fatal("the terminal did not take its output")
		}
		time.Sleep(time.Millisecond)
	}
	terms := findAll[*termview.Terminal](sh.dock.contents["terminal-1"].obj)
	if len(terms) != 1 {
		t.Fatalf("the terminal tab shows %d terminals", len(terms))
	}
	t.Cleanup(func() {
		_ = terms[0].Close()
		select {
		case <-terms[0].Done():
		case <-time.After(10 * time.Second):
			t.Error("the terminal's widget did not end")
		}
	})
	return sh, w, terms[0]
}

func TestRenderTerminalPasteHighlightDark(t *testing.T) {
	_, w, term := pasteScene(t, ThemeDark)
	if text := term.Text(); !strings.Contains(text, "kubectl get pods -A | head -3\nkubectl get nodes") {
		t.Fatalf("terminal %q", text)
	}
	savePNG(t, w, "terminal_paste_highlight_dark.png")
}

func TestRenderTerminalPasteHighlightLight(t *testing.T) {
	_, w, _ := pasteScene(t, ThemeLight)
	savePNG(t, w, "terminal_paste_highlight_light.png")
}
