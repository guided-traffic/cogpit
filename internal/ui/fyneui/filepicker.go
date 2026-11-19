package fyneui

import (
	"errors"
	"log/slog"

	"fyne.io/fyne/v2/driver"

	"github.com/guided-traffic/cogpit/internal/ui/nativedialog"
)

// Every file chooser of the window goes through the shell's filePicker (D-72, the owner's order):
// on macOS the system's own panels, NSOpenPanel and NSSavePanel as a sheet on Cogpit's window
// (internal/ui/nativedialog), so that Cogpit lists no folder of its own to draw a chooser and macOS
// grants what the user picks without a privacy prompt. Where there is no system panel (Windows and
// Linux until M11, Fyne's test driver, which has no NSWindow) open and save report false and each
// chooser shows its Fyne dialog as before: Cogpit's own "Sync Files and Folders" browser
// (syncpicker.go), since Fyne's dialogs choose a file or a folder, never both, and Fyne's file
// dialogs for the rest. Tests inject a recorder (filepicker_test.go).
//
// The choosers: Sync Files and Folders (kubeconfigs.go), a cluster's icon and a folder for a
// terminal's working directory or for kubectl's binaries (the settings pages), a chat
// connection's CA file (Preferences › Agent), and the three saves: a table's CSV, the Create tab's
// YAML and a log tab's lines (filepicker_choosers.go).

// filePicker shows the system's file panels.
type filePicker interface {
	// open shows the open panel; done gets the chosen items on the main goroutine and is never
	// called for a cancel. It reports false when no system panel was shown: the caller shows its
	// own dialog.
	open(o nativedialog.OpenOptions, done func([]nativedialog.Pick)) bool
	// save shows the save panel; done gets the chosen path on the main goroutine, never for a
	// cancel; the panel asked before replacing a file, the caller writes it. False as for open.
	save(o nativedialog.SaveOptions, done func(path string)) bool
}

// noPanels is the picker of a window without the system's panels: every chooser shows its Fyne
// dialog. The shell starts with it; Run puts the system's in its place.
type noPanels struct{}

func (noPanels) open(nativedialog.OpenOptions, func([]nativedialog.Pick)) bool { return false }
func (noPanels) save(nativedialog.SaveOptions, func(string)) bool              { return false }

// systemPanels are the system's panels on the shell's window.
type systemPanels struct{ sh *shell }

func (p systemPanels) open(o nativedialog.OpenOptions, done func([]nativedialog.Pick)) bool {
	sh := p.sh
	return panelShown(nativedialog.Open(sh.nsWindow(), o, func(picks []nativedialog.Pick) {
		if len(picks) > 0 {
			sh.do(func() { done(picks) })
		}
	}))
}

func (p systemPanels) save(o nativedialog.SaveOptions, done func(string)) bool {
	sh := p.sh
	return panelShown(nativedialog.Save(sh.nsWindow(), o, func(path string) {
		if path != "" {
			sh.do(func() { done(path) })
		}
	}))
}

// panelShown says whether a panel's request is answered: shown, or dropped because the window
// already shows one. Anything else falls back to the chooser's own dialog.
func panelShown(err error) bool {
	switch {
	case err == nil, errors.Is(err, nativedialog.ErrBusy):
		return true
	case !errors.Is(err, nativedialog.ErrUnsupported) && !errors.Is(err, nativedialog.ErrNoWindow):
		slog.Warn("the system's file panel is not available; Cogpit shows its own", "err", err)
	}
	return false
}

// nsWindow returns the shell window's NSWindow on macOS, else 0 (another platform, Fyne's test
// driver, a window that is gone). RunNative runs its function at once, on the main goroutine.
func (sh *shell) nsWindow() uintptr {
	var win uintptr
	if nw, ok := sh.win.(driver.NativeWindow); ok {
		nw.RunNative(func(ctx any) {
			if mc, ok := ctx.(driver.MacWindowContext); ok {
				win = mc.NSWindow
			}
		})
	}
	return win
}

// keepAccess stores the bookmarks the panel made of the picks under use (nativedialog.Store),
// off the main goroutine: the next start resolves them before anything reads the items. Without
// a store (tests, a run without a configuration directory) nothing is kept.
func (sh *shell) keepAccess(use string, access nativedialog.Access, picks []nativedialog.Pick) {
	store := sh.bookmarks
	if store == nil {
		return
	}
	go func() {
		defer sh.recoverTo("keeping a bookmark")
		for _, p := range picks {
			if len(p.Bookmark) == 0 {
				if p.BookmarkErr != "" {
					slog.Warn("no bookmark of a picked item; macOS may ask again for it at the next start",
						"use", use, "err", p.BookmarkErr)
				}
				continue
			}
			if err := store.Keep(use, p.Path, access, p.Bookmark); err != nil {
				slog.Warn("keeping a bookmark", "use", use, "err", err)
			}
		}
	}()
}
