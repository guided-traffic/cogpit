// Package update is the desktop app's updater (D-4; docs/developer/architecture.md §11): on macOS
// Sparkle 2, which the app bundle carries in Contents/Frameworks and which checks Cogpit's signed
// appcast, shows Sparkle's own window when an update is there, verifies the disk image's EdDSA
// signature and Apple's code signature, replaces the app and relaunches it. On Linux apt, which
// the .deb points at Cogpit's signed APT repository (apt_linux.go, D-108). On Windows WinSparkle,
// Sparkle's Windows port, which the setup installs beside cogpit.exe; it checks the setup's EdDSA
// signature and runs it, and the setup starts the new release (winsparkle_windows.go, D-111).
// cogpit-cli has no updater: the app's installation brings it.
//
// The binary does not link Sparkle. New loads Sparkle.framework at run time from the running app's
// bundle (dlopen through NSBundle) and finds SPUStandardUpdaterController with NSClassFromString,
// through cgo in sparkle_darwin.m; so every build of the module, its tests and the Linux CI work
// without the framework, and an app without it, or without the update key in its Info.plist,
// simply has no updater. Updater.Unavailable then says why, in the words the Help menu shows.
//
// The engine drives it (engine/updates.go): the preference "Check for updates automatically" is
// Cogpit's, and the engine hands it to Start and SetAutomaticChecks, which set Sparkle's
// automaticallyChecksForUpdates; with them on, Start also checks once at once, so that every start
// checks whenever the last check was (the owner, 2026-10-16), where Sparkle's own launch check
// waits out its interval. Help › Check for Updates… calls CheckForUpdates. Sparkle runs on
// the main thread: every call is queued onto the main dispatch queue, which the app's event loop
// drains, so no method blocks and any goroutine may call them.
//
// The Update button (F7): Sparkle's standard user driver gets a delegate that takes over showing
// the updates scheduled checks find (Sparkle's gentle reminders): Sparkle shows no window by
// itself, it reports the update, and AvailableUpdate says so until the update session ends, so
// that the window shows its Update button, whose click brings Sparkle's window with that update up
// (CheckForUpdates). OnUpdateChange tells the engine.
//
// Layer L4: it imports nothing of Cogpit's but internal/buildinfo (L0, the version this process
// runs, on Linux and Windows); cmd/cogpit wires it into the engine.
package update

import (
	"errors"
	"slices"
	"sync"
)

// The reasons this Cogpit cannot update itself, as Unavailable returns them.
var (
	// ErrNotMacOS: only the macOS app, the Linux package and the Windows setup's app update
	// themselves (and the macOS app only when built with cgo).
	ErrNotMacOS = errors.New("not macOS, Linux or Windows: Cogpit updates itself on macOS, and on Linux and Windows when installed from its package or setup")
	// ErrNoBundle: the process is not an app bundle's executable (go run, bin/cogpit).
	ErrNoBundle = errors.New("not installed from the disk image: this Cogpit does not run as an app bundle")
	// ErrNoFramework: the bundle carries no Sparkle.framework.
	ErrNoFramework = errors.New("not installed from the disk image: this app bundle carries no Sparkle.framework")
	// ErrNotInstalled: the app runs from its disk image or a translocated (quarantined) copy, which
	// Sparkle cannot replace.
	ErrNotInstalled = errors.New("not installed yet: Cogpit runs from its disk image or a quarantined folder; drag it into Applications first")
	// ErrNoFeed: the bundle's Info.plist names no appcast (SUFeedURL).
	ErrNoFeed = errors.New("this build has no update feed")
	// ErrNoKey: the bundle's Info.plist has no public update key (SUPublicEDKey): a build made
	// without SPARKLE_PUBLIC_ED_KEY, such as every local one.
	ErrNoKey = errors.New("this build has no update key")
	// ErrNotStarted: CheckForUpdates before Start.
	ErrNotStarted = errors.New("the updater has not started yet")
	// ErrNotFromPackage: on Linux, the process is not /usr/bin/cogpit of the installed package
	// cogpit (a build of its own, a copy elsewhere), which apt does not update.
	ErrNotFromPackage = errors.New("not installed from the package: this Cogpit is not /usr/bin/cogpit of the installed .deb, which apt updates")
	// ErrNoRepository: on Linux, the package's sources file is gone (the user deleted it), so apt
	// knows of no new release.
	ErrNoRepository = errors.New("no package repository: /etc/apt/sources.list.d/cogpit.sources, which the package installs, is missing, " +
		"so apt finds no new release of Cogpit; reinstall the package from Cogpit's releases to set it up again")
	// ErrNotFromSetup: on Windows, the process is not the cogpit.exe Cogpit's setup installed (a
	// build of its own, a copy elsewhere), which WinSparkle does not update.
	ErrNotFromSetup = errors.New("not installed from the setup: this Cogpit is not the cogpit.exe Cogpit's setup installed, which updates itself")
	// ErrNoWinSparkle: on Windows, WinSparkle.dll is missing beside cogpit.exe or does not load.
	ErrNoWinSparkle = errors.New("WinSparkle.dll, which Cogpit's setup installs beside cogpit.exe, is missing or does not load; " +
		"install Cogpit again from its setup")
)

// The levels of the messages OnUpdateMessage passes on: the engine's toast levels.
const (
	LevelInfo    = "info"
	LevelWarning = "warning"
	LevelError   = "error"
)

// Updater is the app's updater. Its zero value has none (ErrNotMacOS); New returns the real one.
type Updater struct {
	err error // why there is none: nil once Sparkle is loaded

	mu      sync.Mutex
	started bool
}

// New loads Sparkle from the running app's bundle when it can: a macOS app bundle with
// Contents/Frameworks/Sparkle.framework, SUFeedURL and SUPublicEDKey in its Info.plist, installed
// (not on a read-only volume, not translocated). Nothing is started and nothing reaches the
// network; Unavailable says why when it cannot.
func New() *Updater { return &Updater{err: load()} }

// Unavailable returns why this Cogpit cannot update itself, nil when it can.
func (u *Updater) Unavailable() error {
	switch {
	case u == nil:
		return ErrNotMacOS
	case u.err != nil:
		return u.err
	}
	return startFailure()
}

// Start starts Sparkle with its scheduled checks on or off, once; later calls do nothing. Sparkle
// starts when the app's event loop next runs, after the app finished launching, and with the
// checks on runs one of them at once, its find for the Update button.
func (u *Updater) Start(automaticChecks bool) {
	if u.Unavailable() != nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.started {
		u.started = true
		start(automaticChecks)
	}
}

// CheckForUpdates checks the appcast now, as the user asked: Sparkle shows its window with the
// result, and an update installs only when the user clicks Install in it.
func (u *Updater) CheckForUpdates() error {
	if err := u.Unavailable(); err != nil {
		return err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.started {
		return ErrNotStarted
	}
	check()
	return nil
}

// SetAutomaticChecks turns Sparkle's scheduled checks on or off; before Start it does nothing
// (Start takes the value).
func (u *Updater) SetAutomaticChecks(on bool) {
	if u.Unavailable() != nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.started {
		setAutomaticChecks(on)
	}
}

// AvailableUpdate returns the version of the newer release Sparkle holds for the user, ok while
// it holds one: from a scheduled check's find, which Sparkle leaves to Cogpit's Update button
// instead of showing its window (its gentle reminders, F7), or from a check the user started,
// until the update session ends (the user installed, skipped or dismissed it, or it failed). Never
// without an updater.
func (u *Updater) AvailableUpdate() (version string, ok bool) {
	if u.Unavailable() != nil {
		return "", false
	}
	return notice.get()
}

// OnUpdateChange registers fn, called on a goroutine of its own after AvailableUpdate changed; fn
// reads AvailableUpdate itself. Nothing is registered without an updater.
func (u *Updater) OnUpdateChange(fn func()) {
	if u.Unavailable() != nil || fn == nil {
		return
	}
	notice.listen(fn)
}

// RestartPending reports whether the newer release AvailableUpdate names is installed already, so
// that a restart of Cogpit uses it: on Linux apt replaces the files under the running app (the
// Software Updater, unattended-upgrades or the Update button's helper), and CheckForUpdates then
// restarts Cogpit. Never on macOS, where Sparkle relaunches the app itself.
func (u *Updater) RestartPending() bool {
	if u.Unavailable() != nil {
		return false
	}
	return notice.restartPending()
}

// OnUpdateMessage registers fn, called on a goroutine of its own with what an update the user
// started came to (installed, already the newest, cancelled, failed), for the window's
// notifications; level is LevelInfo, LevelWarning or LevelError. On macOS Sparkle's own window
// says it and fn is never called.
func (u *Updater) OnUpdateMessage(fn func(level, message string)) {
	if u.Unavailable() != nil || fn == nil {
		return
	}
	messages.listen(fn)
}

// OnShutdownRequest registers fn, called on a goroutine of its own when an update the user started
// needs Cogpit to quit so that it can install: on Windows once WinSparkle has started the setup,
// which waits for cogpit.exe to be free and then starts the new release. fn closes the window as
// the user would, so that the caller's cleanup runs. Nothing is registered without an updater, nor
// on macOS and Linux, where Sparkle relaunches the app itself and apt's restart quits it.
func (u *Updater) OnShutdownRequest(fn func()) {
	if u.Unavailable() != nil || fn == nil || shutdownHook == nil {
		return
	}
	shutdownHook(fn)
}

// shutdownHook keeps OnShutdownRequest's functions where an updater asks for them
// (winsparkle_windows.go); nil elsewhere.
var shutdownHook func(fn func())

// CheckActs reports whether CheckForUpdates acts instead of showing a window of its own: on Linux
// it installs through apt after polkit's password prompt, or restarts Cogpit (apt_linux.go); on
// macOS Sparkle shows its window and installs only on the user's click there.
func (u *Updater) CheckActs() bool { return u.Unavailable() == nil && checkActs }

// checkActs: this system's CheckForUpdates acts (apt_linux.go sets it).
var checkActs bool

// notice is what the updater reported last (Sparkle's user driver delegate, sparkle_darwin.m; apt,
// apt_linux.go): one per process, as Sparkle's controller is.
var notice updateNotice

// updateNotice holds the update the updater reports and the functions that follow it.
type updateNotice struct {
	mu        sync.Mutex
	available bool
	version   string
	restart   bool // installed already: a restart uses it (Linux)
	listeners []func()
}

func (n *updateNotice) get() (string, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.version, n.available
}

func (n *updateNotice) restartPending() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.available && n.restart
}

func (n *updateNotice) listen(fn func()) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.listeners = append(n.listeners, fn)
}

// set keeps what Sparkle reported and tells the listeners when it changed, each on a goroutine of
// its own: Sparkle reports on the main thread inside AppKit's event dispatch, which must not wait.
func (n *updateNotice) set(available bool, version string) { n.setState(available, version, false) }

// setState is set with restart: the release is installed already and a restart uses it.
func (n *updateNotice) setState(available bool, version string, restart bool) {
	if !available {
		version, restart = "", false
	}
	n.mu.Lock()
	changed := n.available != available || n.version != version || n.restart != restart
	n.available, n.version, n.restart = available, version, restart
	fns := slices.Clone(n.listeners)
	n.mu.Unlock()
	if !changed {
		return
	}
	for _, fn := range fns {
		go fn()
	}
}

// messages passes what an update the user started came to on to the functions OnUpdateMessage
// registered.
var messages updateMessages

type updateMessages struct {
	mu        sync.Mutex
	listeners []func(level, message string)
}

func (m *updateMessages) listen(fn func(level, message string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listeners = append(m.listeners, fn)
}
