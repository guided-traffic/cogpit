package fyneui

import (
	"fmt"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// TestRenderMCPStatusBar draws the status bar wide and narrow, with the server on and off, in both
// themes, to COGPIT_PNG_DIR when that is set (else only captured; never testdata) for a look.
func TestRenderMCPStatusBar(t *testing.T) {
	state := map[bool]string{true: "on", false: "off"}
	size := map[bool]string{true: "wide", false: "narrow"}
	for _, mode := range []ThemeMode{ThemeDark, ThemeLight} {
		for _, on := range []bool{true, false} {
			for _, wide := range []bool{true, false} {
				name := fmt.Sprintf("mcp_status_%s_%s_%s.png", mode, state[on], size[wide])
				t.Run(name, func(t *testing.T) {
					ctl := &fakeMCP{}
					sh, _, q := newTestShell(t, connectedFake(t, 20), Options{Theme: mode, MCP: ctl})
					sh.app.Preferences().SetBool(prefMCPEnabled, true) // the server is opt-in: switched on earlier
					sh.startMCP()
					q.until(t, "the diagnostics", func() bool { return sh.diag.total > 0 })
					if !on {
						test.Tap(sh.status.mcp)
						q.until(t, "off", func() bool { return !sh.mcp.on && !sh.mcp.busy })
					}
					width := float32(1100)
					if !wide {
						width = sh.status.root.MinSize().Width + 70
					}
					w2 := test.NewTempWindow(t, sh.status.root)
					w2.SetPadded(false)
					w2.Resize(fyne.NewSize(width, sh.status.root.MinSize().Height))
					settle(w2)
					lookPNG(t, w2, name)
				})
			}
		}
	}
}
