// Package fyneui is Cogpit's desktop shell on Fyne: the window layout after Lens (sidebar, view
// header with namespace filter and search, table, detail drawer, dock, status bar), Cogpit's theme
// with its status tokens, the navigator and the confirmation dialog.
//
// Layer L4, and the only package tree that imports Fyne. The shell renders engine state and
// changes it only by dispatching ui.* commands; engine events reach widgets through fyne.Do.
package fyneui

import (
	"context"
	"io"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"github.com/guided-traffic/cogpit/internal/buildinfo"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/terminal"
	"github.com/guided-traffic/cogpit/internal/ui/nativedialog"
)

// Options configure the shell.
type Options struct {
	// Title is the window title; empty means "Cogpit".
	Title string
	// MCP switches the MCP server off and on from the status bar and starts it at launch (unless
	// the user switched it off in an earlier run); nil means the window has no MCP server, as with
	// --no-mcp, and shows no MCP item, button or menu entry.
	MCP MCPControl
	// MCPCommand is the `claude mcp add` command of a server already running that the window
	// cannot switch: with it and without MCP the item only indicates, and the Help menu offers the
	// command. Empty hides them. With MCP set the command comes from the control's Start.
	MCPCommand string
	// AutoConnect connects this context at start (--context, --demo); without it the window starts
	// on the welcome page (D-42).
	AutoConnect string
	// StartWarning, when set, is shown as a warning once the window is up: a crash report the
	// last run left (cmd/cogpit/crash.go).
	StartWarning string
	// PerfReport, when set, measures main-goroutine latency (a jank meter) and reports it.
	PerfReport func(PerfStats)
	// PerfDuration, with PerfReport, scrolls the table through its rows for this long, then
	// reports and quits. Zero measures until the window closes, without scrolling.
	PerfDuration time.Duration
	// PerfMode, with PerfReport, picks the measurement: PerfScroll (the default) or PerfGate.
	PerfMode string
	// Theme picks the palette for the run (--theme), over the Preferences' theme; empty: the
	// Preferences' theme, ThemeDark without one.
	Theme ThemeMode
	// DeepLinks are the cogpit:// links macOS hands the app (D-47), opened once the window is up
	// (deeplink.go); nil: the platform delivers none.
	DeepLinks *engine.DeepLinkQueue
	// Bookmarks keeps the security-scoped bookmarks of what the user picks in the system's open
	// panel and Cogpit reads again in later runs (D-72, filepicker.go); nil keeps none.
	Bookmarks *nativedialog.Store
	// WindowPlace is the file the window's place and size are kept in, the app's
	// <user config dir>/Cogpit/window.json (D-95, windowplace.go); empty keeps nothing, and the
	// window opens at 1440 × 900 in the middle of the screen.
	WindowPlace string
	// menus is where the menus are: this system's place (menusHere), or, for the tests, the
	// system's menu bar as on macOS (menusNative) or the top bar as elsewhere (menusInBar).
	menus menuPlace
}

// PerfStats is what the jank meter measured: how late callbacks posted to the main goroutine
// every frame ran, a proxy for frame time.
type PerfStats struct {
	Samples   int     `json:"samples"`
	P50Ms     float64 `json:"p50Ms"`
	P99Ms     float64 `json:"p99Ms"`
	MaxMs     float64 `json:"maxMs"`
	Over16Ms  int     `json:"over16Ms"`
	DurationS float64 `json:"durationS"`
	// Stalls are the distinct stalls, each with its start (seconds after the scroll phase started)
	// and how long the first callback behind it waited; at most 20.
	Stalls []PerfStall `json:"stalls,omitempty"`
	// Phases covers the whole measurement, start-up included.
	Phases []PerfPhase `json:"phases,omitempty"`
}

// PerfPhase is one phase of a measurement: "startup", "scroll", "views", "yaml". Times are seconds
// since the meter started; stall times are relative to the phase's start.
type PerfPhase struct {
	Name     string  `json:"name"`
	FromS    float64 `json:"fromS"`
	ToS      float64 `json:"toS"`
	Samples  int     `json:"samples"`
	P50Ms    float64 `json:"p50Ms"`
	P99Ms    float64 `json:"p99Ms"`
	MaxMs    float64 `json:"maxMs"`
	Over16Ms int     `json:"over16Ms"`
	// Stalls of the phase, at most 20.
	Stalls []PerfStall `json:"stalls,omitempty"`
	// Switches are the view switches of the "views" phase.
	Switches []PerfSwitch `json:"switches,omitempty"`
	// Keystrokes is the typing of the "yaml" phase.
	Keystrokes *PerfKeys `json:"keystrokes,omitempty"`
}

// PerfSwitch is one view switch: when it was dispatched (seconds since the meter started), how
// long until the table showed the view's rows (-1: never within the dwell time), and the longest
// wait of a main-goroutine callback within a second after it.
type PerfSwitch struct {
	View        string  `json:"view"`
	AtS         float64 `json:"atS"`
	RowsAfterMs float64 `json:"rowsAfterMs"`
	Rows        int     `json:"rows"`
	MaxLateMs   float64 `json:"maxLateMs"`
}

// PerfKeys is the typing into the YAML editor: one character per frame into the middle of the
// document. Work is the widget's own time per keystroke (edit, tokenize, place); the phase's
// callback latencies show whether keystroke and repaint fit a frame.
type PerfKeys struct {
	Count     int     `json:"count"`
	DocBytes  int     `json:"docBytes"`
	SetTextMs float64 `json:"setTextMs"`
	WorkP50Ms float64 `json:"workP50Ms"`
	WorkP99Ms float64 `json:"workP99Ms"`
	WorkMaxMs float64 `json:"workMaxMs"`
}

// PerfStall is one stall of the main goroutine the jank meter saw.
type PerfStall struct {
	AtS float64 `json:"atS"`
	Ms  float64 `json:"ms"`
}

// Engine is the part of *engine.Session the shell uses. It exists so that the shell can be tested
// against a fake with canned data; the desktop app passes the real session to [Run].
type Engine interface {
	Dispatch(ctx context.Context, inv engine.Invocation) (any, error)
	UIState() engine.UIState
	Table(view string) *engine.TableSnapshot
	Detail(ref model.ObjectRef) (*engine.DetailModel, error)
	ObjectMetrics(ref model.ObjectRef) (*engine.ObjectMetrics, error)
	Subscribe(fn func(engine.Event)) (cancel func())
	Diagnostics() engine.Diagnostics
	Registry() *engine.Registry
	Contexts() ([]engine.ContextInfo, error)
	OpenLogs(ctx context.Context, tabID string) (io.ReadCloser, error)
	OpenTerminal(ctx context.Context, tabID string) (terminal.Session, error)
	SetConfirmer(c engine.Confirmer)
	// Chat is the built-in agent's chat (engine/chat.go), nil without one: the window then shows
	// no chat column, no View › Agent Chat and no Preferences › Agent (chat.go).
	Chat() engine.ChatAgent
}

var _ Engine = (*engine.Session)(nil)

// appID is the app's unique ID: its preferences' and its storage's name for Fyne; appName is the
// product's name, the window's default title and the metadata's.
const (
	appID   = "io.guided-traffic.cogpit"
	appName = "Cogpit"
)

// appMetadata is the app's metadata for a build with Fyne's thread check off (the Makefile's
// FYNE_THREAD_CHECKS=off, the tag nothreadcheck; D-103): it declares the app migrated to fyne.Do,
// which Fyne reads once (internal/build.MigratedToFyneDo) and then no longer asks, on every canvas
// refresh, which goroutine it runs on. That question took a stack trace each time and most of a
// fast scroll's frame. A stray call from another goroutine is then not moved to the main
// goroutine and races; every place the window's code runs off it is audited (ui-threads.md).
// Unlike Fyne's own tag migrated_fynedo, the metadata leaves Fyne's caches thread-safe. ok is false
// for a build with the check on, which sets no metadata, as before D-103.
func appMetadata(checks bool) (m fyne.AppMetadata, ok bool) {
	if checks {
		return fyne.AppMetadata{}, false
	}
	// ID and Name set, so that Fyne does not look for a FyneApp.toml beside the binary, which
	// would replace the migrations; Version and Build are Fyne's defaults (About reads buildinfo).
	return fyne.AppMetadata{ID: appID, Name: appName, Version: "0.0.1", Build: 1,
		Migrations: map[string]bool{"fyneDo": true}}, true
}

// Run builds the window for the session, installs the GUI confirmer and runs Fyne's event loop
// until the window closes.
func Run(s *engine.Session, opts Options) error {
	if m, ok := appMetadata(buildinfo.FyneThreadChecks); ok {
		app.SetMetadata(m) // before the app exists: Fyne reads it at the first canvas refresh
	}
	a := app.NewWithID(appID)
	mode, rowHeight := opts.Theme, 0 // --theme for the run, else the Preferences' (preferences.go)
	if p := s.UIState().Preferences; p != nil {
		if mode == "" {
			mode = ThemeMode(p.Theme)
		}
		rowHeight = p.TableRowHeight
	}
	a.Settings().SetTheme(newThemeFor(mode, rowHeight))
	title := opts.Title
	if title == "" {
		title = appName
	}
	w := a.NewWindow(title)
	w.SetPadded(false) // the bars and the sidebar reach the window's edges
	// After the app stops, engine events and tickers may still post work for a moment until the
	// shell unsubscribes; Fyne logs an error for each fyne.Do into a stopped app, so drop them.
	var stopped atomic.Bool
	a.Lifecycle().SetOnStopped(func() { stopped.Store(true) })
	do := func(fn func()) {
		if !stopped.Load() {
			fyne.Do(fn)
		}
	}
	sh := newShell(s, a, w, opts, do)
	sh.setConcealed = setConcealedClipboard // the real app's system clipboard (H-25)
	sh.files = systemPanels{sh}             // the system's file panels where there are some (D-72)
	sh.startMCP()                           // before the menu, which offers the command only while the server runs
	s.SetConfirmer(sh.confirmer)
	s.SetDockOutputReader(dockOutputReader{sh}) // what the dock's tabs show, for dock.read (dockoutput.go)
	w.SetContent(sh.root)
	w.SetOnClosed(func() {
		// The app quits with its window: Fyne then drains its queue and from then on runs a fyne.Do
		// at once on the caller's goroutine (or panics on its closed queue), so nothing more may reach
		// it. The window's close runs on the main goroutine a frame before that, for the close button,
		// a.Quit and macOS's ⌘Q alike (GLFW turns it into the windows' close); OnStopped comes later,
		// on Fyne's lifecycle goroutine. Only Fyne's own SIGINT and SIGTERM handler quits without it,
		// racing the handler below, which quits through a.Quit.
		stopped.Store(true)
		// The dock's streams end here too, while Fyne still queues: the terminal widget calls fyne.Do
		// itself, not through the funnel, and after the drain its reading goroutines would run
		// widget code at once, side by side (the -race run of D-103 found it at quit).
		sh.dock.closeAll()
		sh.clearSecretClipboard() // a copied Secret value does not outlive the window on the clipboard
	})
	// On Windows and Linux the menus are the top bar's; the window keeps them for their keys only
	// from its start on, when Fyne no longer builds its own menu bar later (menubar.go).
	sh.deferMenuKeys = sh.inlineMenus
	sh.setMainMenu()
	// The window's last place and size, whatever Restore the last session says; the first start's
	// 1440 × 900 in the middle of the screen without one (D-95, windowplace.go). A --perf run
	// measures at that size and keeps nothing.
	placeFile := opts.WindowPlace
	if opts.PerfReport != nil {
		placeFile = ""
	}
	place := newWindowPlacer(openWindowPlace(placeFile))
	place.restore(a, w)
	// Cogpit's own title bar on macOS needs the window, which exists once the app started: queued
	// from OnStarted, the install runs after the window's creation (titlebar.go).
	detachLinks := func() {}
	a.Lifecycle().SetOnStarted(func() {
		do(sh.titlebar.install)
		// After the title bar took its room: the kept frame exactly, full screen, its changes kept (D-95).
		do(func() { place.follow(w) })
		do(sh.installAbout) // macOS: About Cogpit in the application menu, GLFW's made by now (appmenu_darwin.go)
		if sh.inlineMenus {
			do(sh.installMenuKeys) // Windows and Linux: the menus' keys, the window up (menubar.go)
		}
		do(sh.showStartNotices) // the crash notice, and what the engine said before the window was up
		// The cogpit:// links that came before the window wait until now (deeplink.go).
		do(func() { detachLinks = sh.attachDeepLinks(opts.DeepLinks) })
	})
	sh.start()
	sh.startConnectionHints() // macOS's wake and network changes check the connection at once (D-58)
	if opts.PerfReport != nil {
		sh.startPerf()
	}
	// Ctrl+C or SIGTERM quits the way closing the window does, so the caller's cleanup runs (the
	// MCP token file is removed, the informers stop) instead of the process ending mid-way.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	ran := make(chan struct{})
	defer close(ran)
	go func() {
		select {
		case <-sig:
			do(a.Quit)
		case <-ran:
		}
	}()
	w.ShowAndRun()
	stopped.Store(true)
	place.finishWindow(w) // what is pending written now; Windows and Linux keep the size here
	detachLinks()
	if sh.perf != nil {
		sh.perf.finish() // the window closed before PerfDuration ended, or there is none
	}
	sh.stop()
	s.SetConfirmer(nil)
	s.SetDockOutputReader(nil)
	return nil
}
