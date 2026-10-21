package fyneui

import (
	"runtime"

	"fyne.io/fyne/v2"
)

// The window's Help and Window menus (ui-parity › Application menu; G13). Lens's own items are
// documented in part: Help › Welcome (with "Open Onboarding Wizard...", not a target: the welcome
// page offers its steps) and a Window menu with Minimize, on macOS Zoom, Bring All to Front and the
// list of windows, on Windows and Linux Close Window. Cogpit's, read in Fyne 2.8.1's and GLFW 3.4's
// source (not tried in a real window):
//   - Help: Welcome (ui.welcome.open), Documentation (Cogpit's README on GitHub, in the browser),
//     Copy MCP command while the window has an MCP server, Check for Updates…, and on Windows and
//     Linux About Cogpit (about.go). On macOS About Cogpit is the application menu's (A60,
//     appmenu_darwin.go): GLFW's own item, which shows Cogpit's dialog instead of AppKit's panel.
//     No item is labelled exactly "About": Fyne would move it into the application menu and take
//     over GLFW's item, whose place a menu rebuild then loses (menu_darwin.m, insertDarwinMenuItem
//     and resetDarwinMenu). Documentation and About are window actions, not engine commands, as the
//     status bar's link (D-23): no agent opens a browser or a dialog for itself.
//   - Window on macOS: GLFW's own menu (cocoa_init.m, createMenuBar): Minimize ⌘M, Zoom, Bring All
//     to Front, Enter Full Screen ⌃⌘F, and AppKit's list of windows (NSApp's windowsMenu); Fyne keeps
//     it between Cogpit's menus and Help when it rebuilds the menu bar (resetDarwinMenu keeps items
//     it did not add). Cogpit adds none, which would be a second Window menu.
//   - Window on Windows and Linux: Close Window and Toggle Full Screen (Cogpit's). Minimize is
//     missing: Fyne 2.8.1's window has no minimise call and keeps GLFW's window private.
//   - Quit stays the platform's on macOS (GLFW's ⌘Q); elsewhere Cogpit adds it to the first menu,
//     as Fyne's own menu bar did (menubar.go, since the menus moved into the top bar).

// goosMac is runtime.GOOS on macOS.
const goosMac = "darwin"

// aboutItemLabel is About's menu item: in Help on Windows and Linux, in the application menu on
// macOS (appmenu_darwin.m gives GLFW's item the same title).
const aboutItemLabel = "About Cogpit"

// helpMenu is the Help menu on this computer's system.
func (sh *shell) helpMenu() *fyne.Menu { return sh.helpMenuOn(runtime.GOOS) }

// helpMenuOn is the Help menu on goos: About Cogpit at its end, but not on macOS, whose application
// menu has it.
func (sh *shell) helpMenuOn(goos string) *fyne.Menu {
	var items []*fyne.MenuItem
	if c, ok := sh.eng.Registry().Get("ui.welcome.open"); ok {
		items = append(items, fyne.NewMenuItem(c.Label, func() { sh.dispatch(c.ID, struct{}{}, nil) }))
	}
	for _, item := range []*fyne.MenuItem{sh.auditMenuItem(), sh.diagnosticsMenuItem()} { // auditlog.go, diagnostics.go
		if item != nil {
			items = append(items, item)
		}
	}
	items = append(items, fyne.NewMenuItem("Documentation", sh.openDocumentation))
	if sh.hasMCP() {
		sh.mcpMenu = fyne.NewMenuItem("Copy MCP command", sh.copyMCPCommand)
		sh.mcpMenu.Disabled = !sh.mcp.on
		items = append(items, fyne.NewMenuItemSeparator(), sh.mcpMenu)
	}
	items = append(items, fyne.NewMenuItemSeparator())
	if item := sh.updatesMenuItem(); item != nil { // Check for Updates… (preferences_updates.go)
		items = append(items, item)
	}
	if goos != goosMac {
		items = append(items, fyne.NewMenuItem(aboutItemLabel, sh.showAbout))
	}
	if n := len(items); items[n-1].IsSeparator { // nothing after the last separator
		items = items[:n-1]
	}
	return fyne.NewMenu("Help", items...)
}

// windowMenu is Cogpit's Window menu on goos: nil on macOS, where GLFW's stays.
func (sh *shell) windowMenu(goos string) *fyne.Menu {
	if goos == goosMac {
		return nil
	}
	full := fyne.NewMenuItem("Toggle Full Screen", func() { sh.win.SetFullScreen(!sh.win.FullScreen()) })
	closeW := fyne.NewMenuItem("Close Window", sh.win.Close)
	return fyne.NewMenu("Window", full, fyne.NewMenuItemSeparator(), closeW)
}
