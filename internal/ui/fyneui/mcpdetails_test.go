package fyneui

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// The MCP details (F6). The tokens here are fixed fakes, never a real one.
const (
	fakeMCPToken    = "5eed5eed5eed5eed5eed5eed5eed5eed5eed5eed5eed5eed5eed5eed5eed5eed"
	fakeMCPNewToken = "0ddba11c0ddba11c0ddba11c0ddba11c0ddba11c0ddba11c0ddba11c0ddba11c"
	fakeStdio       = "claude mcp remove --scope user cogpit >/dev/null 2>&1; claude mcp add --scope user cogpit -- " +
		"/Applications/Cogpit.app/Contents/MacOS/cogpit-cli mcp"
)

// lockedBuffer is a bytes.Buffer safe for the logger's goroutines.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// captureLogs sends the default logger to a buffer for the test (fyneui's tests run one at a time).
func captureLogs(t *testing.T) *lockedBuffer {
	t.Helper()
	buf := &lockedBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

// detailsTexts are the texts the open MCP details show.
func detailsTexts(t *testing.T, sh *shell) string {
	t.Helper()
	d := sh.mcp.details
	if d == nil {
		t.Fatal("the MCP details are not open")
	}
	var out []string
	var walk func(o fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if o == nil || !o.Visible() {
			return
		}
		switch x := o.(type) {
		case *textLine:
			out = append(out, x.text)
		case *paragraph:
			out = append(out, x.text)
		case *pill:
			out = append(out, x.text)
		case *widget.Button:
			out = append(out, x.Text)
		case *tipButton:
			out = append(out, x.Text, x.tip)
		case *fyne.Container:
			for _, c := range x.Objects {
				walk(c)
			}
		case fyne.Widget:
			for _, c := range test.WidgetRenderer(x).Objects() {
				walk(c)
			}
		}
	}
	walk(d.panel.inner)
	return strings.Join(out, "\n")
}

// toastTexts are the texts of every toast shown.
func toastTexts(sh *shell) string {
	var out []string
	for _, c := range toastCards(sh) {
		for _, p := range findAll[*paragraph](c) {
			out = append(out, p.text)
		}
	}
	return strings.Join(out, "\n")
}

// TestTopBarHasNoMCPButton: the top bar's MCP button is gone (F6); the MCP item's gear in the
// status bar has the details instead.
func TestTopBarHasNoMCPButton(t *testing.T) {
	sh, _, _ := mcpShell(t, &fakeMCP{})
	for _, b := range findAll[*widget.Button](sh.top.root) {
		if strings.Contains(b.Text, "MCP") {
			t.Errorf("the top bar has a button %q", b.Text)
		}
	}
	if sh.status.mcp == nil || sh.status.mcp.gear == nil {
		t.Fatal("the status bar's MCP item has no gear")
	}
}

// TestMCPGearOpensTheDetails: the gear opens the details above the status bar without switching
// the server: the endpoint, the token masked, the stdio and the HTTP registrations, the HTTP one
// with its token masked too; a tap outside the card closes them, the gear again opens them.
func TestMCPGearOpensTheDetails(t *testing.T) {
	ctl := &fakeMCP{token: fakeMCPToken, stdio: fakeStdio}
	sh, w, _ := mcpShell(t, ctl)
	gear := sh.status.mcp.gear
	test.Tap(gear)
	settle(w)
	d := sh.mcp.details
	if d == nil || w.Canvas().Overlays().Top() != d.panel {
		t.Fatal("the gear did not open the details")
	}
	if ctl.log() != "start(user=false)" || !sh.mcp.on {
		t.Errorf("the gear switched the server: calls %s", ctl.log())
	}
	texts := detailsTexts(t, sh)
	for _, want := range []string{"MCP server", "On", "Switch off", "http://127.0.0.1:40001/mcp", mcpTokenMask, "Show",
		"Regenerate token", fakeStdio, mcpStdioText, mcpHTTPTextAlso, `Bearer ` + mcpCommandMask} {
		if !strings.Contains(texts, want) {
			t.Errorf("the details lack %q:\n%s", want, texts)
		}
	}
	if strings.Contains(texts, fakeMCPToken) {
		t.Error("the details show the token before the user asked")
	}
	// The card sits above the status bar, its right edge at the gear's.
	drv := fyne.CurrentApp().Driver()
	card := d.panel.card
	if bottom, bar := card.Position().Y+card.Size().Height, drv.AbsolutePositionForObject(sh.status.root).Y; bottom > bar {
		t.Errorf("the card ends at y=%v, below the status bar's top %v", bottom, bar)
	}
	if right, gx := card.Position().X+card.Size().Width, drv.AbsolutePositionForObject(gear).X+gear.Size().Width; right != gx {
		t.Errorf("the card's right edge %v, the gear's %v", right, gx)
	}

	test.TapCanvas(w.Canvas(), fyne.NewPos(20, 20))
	if sh.mcp.details != nil {
		t.Error("a tap outside the card did not close the details")
	}
	test.Tap(gear)
	if sh.mcp.details == nil {
		t.Error("the gear did not open the details again")
	}
	sh.toggleMCPDetails()
	if sh.mcp.details != nil {
		t.Error("the gear's toggle did not close the details")
	}
}

// TestMCPDetailsTokenShownAndCopied: Show reveals the token until Hide or the panel closes; the
// token and the HTTP registration are copied as credentials (cleared from the clipboard like a
// Secret's value), the endpoint and the stdio registration as plain text; the panel's note says
// so, and no toast and no log line holds the token.
func TestMCPDetailsTokenShownAndCopied(t *testing.T) {
	logs := captureLogs(t)
	sh, w, _ := mcpShell(t, &fakeMCP{token: fakeMCPToken, stdio: fakeStdio})
	sh.openMCPDetails()
	d := sh.mcp.details
	test.Tap(d.reveal)
	if texts := detailsTexts(t, sh); !strings.Contains(texts, fakeMCPToken) || !strings.Contains(texts, "Hide") {
		t.Errorf("Show did not reveal the token:\n%s", texts)
	}
	test.Tap(d.reveal)
	if strings.Contains(detailsTexts(t, sh), fakeMCPToken) {
		t.Error("Hide left the token shown")
	}

	test.Tap(d.copyToken)
	if got := sh.app.Clipboard().Content(); got != fakeMCPToken || sh.clipSecret != fakeMCPToken {
		t.Errorf("the token copy: clipboard holds it %v, marked secret %v", got == fakeMCPToken, sh.clipSecret == fakeMCPToken)
	}
	if !strings.Contains(detailsTexts(t, sh), "Copied the token to the clipboard. The clipboard is cleared in 1 minute.") {
		t.Errorf("the note does not say the clipboard is cleared:\n%s", detailsTexts(t, sh))
	}
	test.Tap(d.copyHTTP)
	if got := sh.app.Clipboard().Content(); !strings.Contains(got, "Bearer "+fakeMCPToken) || sh.clipSecret != got {
		t.Error("the HTTP registration was not copied whole, as a credential")
	}
	test.Tap(d.copyEndpoint)
	if got := sh.app.Clipboard().Content(); got != "http://127.0.0.1:40001/mcp" || sh.clipSecret != "" {
		t.Errorf("the endpoint copy: %q, secret %v", got, sh.clipSecret != "")
	}
	test.Tap(d.copyStdio)
	if got := sh.app.Clipboard().Content(); got != fakeStdio || sh.clipSecret != "" {
		t.Errorf("the stdio copy: %q, secret %v", got, sh.clipSecret != "")
	}

	test.Tap(d.reveal)
	sh.closeMCPDetails()
	sh.openMCPDetails()
	if strings.Contains(detailsTexts(t, sh), fakeMCPToken) {
		t.Error("the token is shown again after the panel closed")
	}
	settle(w)
	if strings.Contains(toastTexts(sh), fakeMCPToken) || strings.Contains(logs.String(), fakeMCPToken) {
		t.Error("the token reached a toast or the log")
	}
}

// TestMCPHelpCopyOpensTheDetails: Help › Copy MCP command copies the command, the HTTP one as a
// credential, and opens the details.
func TestMCPHelpCopyOpensTheDetails(t *testing.T) {
	sh, _, _ := mcpShell(t, &fakeMCP{token: fakeMCPToken})
	sh.copyMCPCommand()
	if got := sh.app.Clipboard().Content(); !strings.Contains(got, fakeMCPToken) || sh.clipSecret != got {
		t.Error("the HTTP command was not copied as a credential")
	}
	if sh.mcp.details == nil || !strings.Contains(detailsTexts(t, sh), "Copied the registration command") {
		t.Error("the details did not open with the note")
	}
}

// TestMCPRegenerateWarnsFirst: Regenerate token asks first, naming what it ends; Cancel leaves the
// token alone; the confirmed click regenerates, the details show the new token masked, and the
// notice names no token.
func TestMCPRegenerateWarnsFirst(t *testing.T) {
	logs := captureLogs(t)
	ctl := &fakeMCP{token: fakeMCPToken, regenToken: fakeMCPNewToken}
	sh, w, q := mcpShell(t, ctl)
	sh.openMCPDetails()
	d := sh.mcp.details
	test.Tap(d.regenerate)
	if d.warning == nil {
		t.Fatal("Regenerate token did not warn")
	}
	texts := strings.Join(overlayTexts(w), "\n")
	for _, want := range []string{"Regenerate the MCP token?", "ends every MCP connection", "need the new token", mcpRegenerateBridge} {
		if !strings.Contains(texts, want) {
			t.Errorf("the warning lacks %q:\n%s", want, texts)
		}
	}
	if strings.Contains(ctl.log(), "regenerate") {
		t.Fatal("the token was regenerated before the user confirmed")
	}
	test.Tap(d.warning.cancel)
	if d.warning != nil || strings.Contains(ctl.log(), "regenerate") || sh.mcp.token != fakeMCPToken {
		t.Fatal("Cancel did not leave the token alone")
	}

	test.Tap(d.regenerate)
	test.Tap(d.warning.confirm)
	q.until(t, "the new token", func() bool { return sh.mcp.token == fakeMCPNewToken && !sh.mcp.regenerating })
	if ctl.log() != "start(user=false),regenerate" || !sh.mcp.on {
		t.Errorf("calls %s, on %v", ctl.log(), sh.mcp.on)
	}
	d = sh.mcp.details
	shown := detailsTexts(t, sh)
	if d == nil || strings.Contains(shown, fakeMCPNewToken) || !strings.Contains(shown, mcpTokenMask) || !strings.Contains(shown, mcpRegeneratedText) {
		t.Errorf("the details after the regenerate:\n%s", shown)
	}
	settle(w)
	toasts := toastTexts(sh)
	if !strings.Contains(toasts, mcpRegeneratedText) {
		t.Errorf("no notice of the regenerate: %q", toasts)
	}
	for _, tok := range []string{fakeMCPToken, fakeMCPNewToken} {
		if strings.Contains(toasts, tok) || strings.Contains(logs.String(), tok) {
			t.Error("a token reached a toast or the log")
		}
	}
}

// TestMCPRegenerateWhileOffAndFailing: with the server off the details say so and still offer
// Regenerate token, which leaves the server off; a failed regenerate says why, in the panel and a
// toast.
func TestMCPRegenerateWhileOffAndFailing(t *testing.T) {
	ctl := &fakeMCP{token: fakeMCPToken, regenToken: fakeMCPNewToken}
	sh, _, q := mcpShell(t, ctl)
	test.Tap(sh.status.mcp)
	q.until(t, "off", func() bool { return !sh.mcp.on && !sh.mcp.busy })
	sh.openMCPDetails()
	texts := detailsTexts(t, sh)
	if !strings.Contains(texts, mcpDetailsOff) || !strings.Contains(texts, "Switch on") || strings.Contains(texts, mcpTokenMask) {
		t.Errorf("the details while off:\n%s", texts)
	}
	d := sh.mcp.details
	test.Tap(d.regenerate)
	test.Tap(d.warning.confirm)
	q.until(t, "the regenerate", func() bool { return strings.HasSuffix(ctl.log(), "regenerate") && !sh.mcp.regenerating })
	if sh.mcp.on || sh.mcp.token != "" {
		t.Error("a regenerate while off switched the server on or handed out a token")
	}

	ctl.mu.Lock()
	ctl.regenErr = errors.New("token file: permission denied")
	ctl.mu.Unlock()
	test.Tap(d.regenerate)
	test.Tap(sh.mcp.details.warning.confirm)
	q.until(t, "the failure", func() bool { return strings.Count(ctl.log(), "regenerate") == 2 && !sh.mcp.regenerating })
	if !strings.Contains(detailsTexts(t, sh), "permission denied") || !strings.Contains(toastTexts(sh), "could not regenerate") {
		t.Error("the failure is not shown")
	}

	test.Tap(sh.mcp.details.switchB)
	q.until(t, "on", func() bool { return sh.mcp.on && !sh.mcp.busy })
	if !strings.Contains(detailsTexts(t, sh), "Switch off") {
		t.Error("the details' switch did not switch the server on")
	}
}

// TestMCPPortTakenNotice: a start that found the kept port taken says so once the window is up and
// in the details, naming the ports and never the token.
func TestMCPPortTakenNotice(t *testing.T) {
	sh, w, _ := mcpShell(t, &fakeMCP{token: fakeMCPToken, taken: 59449})
	sh.showStartNotices()
	settle(w)
	toasts := toastTexts(sh)
	if !strings.Contains(toasts, "port 59449") || !strings.Contains(toasts, "40001") || strings.Contains(toasts, fakeMCPToken) {
		t.Errorf("the notice: %q", toasts)
	}
	sh.openMCPDetails()
	if texts := detailsTexts(t, sh); !strings.Contains(texts, "Port 59449 was taken") {
		t.Errorf("the details do not say the port moved:\n%s", texts)
	}
}

// renderMCPDetails: the details open over the window, the server on with both registrations, the
// token masked; with warn, the warning before Regenerate token over them.
func renderMCPDetails(t *testing.T, mode ThemeMode, warn bool, name string) {
	sh, w, q := newTestShell(t, connectedFake(t, 20), Options{Theme: mode,
		MCP: &fakeMCP{token: fakeMCPToken, stdio: fakeStdio}})
	sh.app.Preferences().SetBool(prefMCPEnabled, true) // the server is opt-in: switched on earlier
	sh.startMCP()
	q.until(t, "the diagnostics", func() bool { return sh.diag.total > 0 })
	settle(w)
	test.Tap(sh.status.mcp.gear)
	if warn {
		test.Tap(sh.mcp.details.regenerate)
	}
	savePNG(t, w, name)
}

func TestRenderMCPDetailsDark(t *testing.T) {
	renderMCPDetails(t, ThemeDark, false, "mcp_details_dark.png")
}
func TestRenderMCPDetailsLight(t *testing.T) {
	renderMCPDetails(t, ThemeLight, false, "mcp_details_light.png")
}
func TestRenderMCPRegenerateDark(t *testing.T) {
	renderMCPDetails(t, ThemeDark, true, "mcp_regenerate_dark.png")
}
func TestRenderMCPRegenerateLight(t *testing.T) {
	renderMCPDetails(t, ThemeLight, true, "mcp_regenerate_light.png")
}
