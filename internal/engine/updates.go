package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Updates (D-4, G14; docs/security/updates.md): the macOS app updates itself through Sparkle 2, the
// Linux package through apt (D-108), which the binary wires in (internal/update) as an Updater; the
// engine does no OS calls itself.
// Two preferences are Cogpit's, kept with the others and shown in UIState.Preferences:
// CheckForUpdates ("Check for updates automatically", on by default as in Lens), which the engine
// hands to the updater at start and whenever it changes, so that Cogpit's preference and not
// Sparkle's own prompt decides whether it checks, at every start and every hour; and UpdateChannel, only Stable until
// Cogpit publishes prereleases. app.checkForUpdates (Help › Check for Updates…) checks now with
// Sparkle's own window; installing stays the user's click in it.
//
// Who may change them (the lead's default, 2026-10-14, for the owner to confirm): the user turns
// the automatic checks on and off; an agent or the CLI may turn them on but not off, since off
// leaves a copy with known flaws unpatched (the reverse of Load chart icons, which an agent may
// only turn off: there off is the safer side). Any origin may run app.checkForUpdates: it changes
// nothing, and Sparkle asks the user before it installs anything.
//
// The Update button (F7, the owner's finding of 2026-10-15): an updater that implements
// UpdateNotifier tells when it holds a newer release for the user; UIState.Update says so, and the
// window shows an Update button left of the layout toggles, which runs app.installUpdate: on macOS
// it brings Sparkle's window with that release up, as app.checkForUpdates does; on Windows
// (UpdateInstaller, D-111) it installs the release at once, the click being the user's decision.
// Only the window runs app.installUpdate. A session without such an updater never shows it.
//
// On Linux (D-108) the check acts instead of showing a window (UpdateActor): it installs the
// release through apt after polkit asked for the password, or restarts Cogpit into the release
// apt installed under it (UpdateRestarter, UIState.Update.Restart: the button reads Restart to
// Update). Only the user's own click in the window starts that (the lead's decision, 2026-10-20):
// an agent or the CLI would put a password prompt or a restart in front of the user unasked. What
// it came to (installed, the newest already, cancelled, failed) the updater reports
// (UpdateReporter), and the window shows it as a toast.

// Update channels. Stable is the appcast's default channel; Beta (Sparkle's channel "beta") comes
// with the first prerelease.
const (
	UpdateChannelStable = "stable"
	UpdateChannelBeta   = "beta"
)

// Updater is the app's self-update (internal/update.Updater, Sparkle 2 on macOS), wired by the
// binary with SetUpdater. Its methods do not block.
type Updater interface {
	// Unavailable returns why this Cogpit cannot update itself ("this build has no update key",
	// "not installed from the disk image", "not macOS"), nil when it can.
	Unavailable() error
	// Start starts the updater once, its scheduled checks on or off.
	Start(automaticChecks bool)
	// CheckForUpdates checks now, with the updater's own window.
	CheckForUpdates() error
	// SetAutomaticChecks turns the scheduled checks on or off.
	SetAutomaticChecks(on bool)
}

// UpdateNotifier is an Updater that tells when it knows of a newer release (internal/update:
// Sparkle's gentle reminders, F7): UIState.Update follows it, and the window shows its Update button
// while one is available. An Updater without it never shows the button.
type UpdateNotifier interface {
	// AvailableUpdate returns the newer release's version while the updater holds one for the
	// user, ok false when it holds none.
	AvailableUpdate() (version string, ok bool)
	// OnUpdateChange registers fn, called on any goroutine after AvailableUpdate changed; fn reads
	// AvailableUpdate itself and must not block.
	OnUpdateChange(fn func())
}

// UpdateState is what the session knows of a newer release (UIState.Update): Available while the
// updater holds one for the user, Version its version as the signed appcast names it ("" when the
// text is not a version's: maxUpdateVersion bytes of letters, digits, spaces and ".+-_()").
type UpdateState struct {
	Available bool   `json:"available,omitempty"`
	Version   string `json:"version,omitempty"`
	// Restart: the release is installed already (Linux: apt replaced the files under the running
	// app), and app.checkForUpdates restarts Cogpit into it.
	Restart bool `json:"restart,omitempty"`
}

// UpdateRestarter is an UpdateNotifier that knows when the release it names is installed already,
// so that a restart of Cogpit uses it (internal/update on Linux).
type UpdateRestarter interface {
	RestartPending() bool
}

// UpdateReporter is an Updater that reports what a check the user started came to; the session
// shows each report as a toast. fn is called on any goroutine and must not block.
type UpdateReporter interface {
	OnUpdateMessage(fn func(level, message string))
}

// UpdateActor is an Updater whose CheckForUpdates acts instead of showing a window of its own (on
// Linux it installs through apt after polkit's password prompt, or restarts Cogpit): only the GUI
// runs app.checkForUpdates then.
type UpdateActor interface {
	CheckActs() bool
}

// UpdateInstaller is an Updater that installs the release it holds without its window's questions
// when the user clicks the Update button (app.installUpdate): on Windows WinSparkle checks,
// downloads, verifies and runs the setup in one go, where its window would ask twice more (the
// owner, 2026-10-21, D-111). An Updater without it shows its window as app.checkForUpdates does.
type UpdateInstaller interface {
	InstallUpdate() error
}

// maxUpdateVersion bounds the version text UIState.Update carries.
const maxUpdateVersion = 64

// ErrNoUpdater: a session without an updater (cogpit-cli, a headless engine) does not update
// itself.
var ErrNoUpdater = errors.New("this Cogpit does not update itself: cogpit-cli and headless sessions have no updater; " +
	"download a new release or update it with your package manager")

// ErrUpdateChecksGUIOnly refuses turning the automatic update checks off from anywhere but the
// user's own action in Cogpit's window.
var ErrUpdateChecksGUIOnly = errors.New("only the user, in Cogpit's own window, can turn off the automatic update checks; " +
	"agents and the CLI may only turn them on (off leaves this copy without the fixes of newer releases)")

// ErrUpdateCheckGUIOnly refuses app.checkForUpdates from an agent or the CLI where the check acts
// (UpdateActor).
var ErrUpdateCheckGUIOnly = errors.New("only the user, in Cogpit's own window, starts this update: on Linux it installs " +
	"through apt after an administrator's password, or restarts Cogpit into the installed release; agents read ui_state's update")

// ErrInstallUpdateGUIOnly refuses app.installUpdate from an agent or the CLI: on Windows it
// installs and restarts Cogpit without asking again.
var ErrInstallUpdateGUIOnly = errors.New("only the user's click on the Update button in Cogpit's window installs a release: " +
	"on Windows it installs it and restarts Cogpit at once; agents run app.checkForUpdates, which shows the release in the updater's window")

// ErrNoUpdateAvailable: app.installUpdate while the updater holds no newer release.
var ErrNoUpdateAvailable = errors.New("no newer release is known: Help › Check for Updates… looks for one")

// ErrNoBetaChannel refuses the Beta channel while there is none.
var ErrNoBetaChannel = errors.New("update channel beta: Cogpit publishes no prereleases yet, so only the stable channel exists")

// updaterSlot holds the session's updater. Its own lock orders a start against preference changes.
type updaterSlot struct {
	mu sync.Mutex
	u  Updater
	// notice serialises applying what an UpdateNotifier knows (updateNoticeChanged).
	notice sync.Mutex
}

// SetUpdater wires the app's updater in and starts it with the preference's automatic checks,
// unless it says it cannot update this Cogpit. Called once by the desktop binary.
func (s *Session) SetUpdater(u Updater) {
	s.updates.mu.Lock() // before the preference is read: a change meanwhile waits and follows (updatePrefsChanged)
	defer s.updates.mu.Unlock()
	p, _ := s.Preferences()
	s.updates.u = u
	if u == nil {
		return
	}
	if err := u.Unavailable(); err != nil {
		s.log.Info("updates: none for this Cogpit", "reason", err)
		return
	}
	if n, ok := u.(UpdateNotifier); ok {
		n.OnUpdateChange(func() { s.updateNoticeChanged(n) })
	}
	if r, ok := u.(UpdateReporter); ok {
		r.OnUpdateMessage(func(level, message string) {
			s.Emit(Event{Type: EventToast, Level: toastLevel(level), Message: message})
		})
	}
	u.Start(p.CheckForUpdates)
}

// updateNoticeChanged puts what the updater knows now into UIState.Update. It reads the updater
// under its own lock when it applies it, so that notifications that run out of order still leave
// the newest state; an unchanged state costs no revision.
func (s *Session) updateNoticeChanged(n UpdateNotifier) {
	s.updates.notice.Lock()
	defer s.updates.notice.Unlock()
	next := UpdateState{}
	if version, ok := n.AvailableUpdate(); ok {
		next = UpdateState{Available: true, Version: cleanUpdateVersion(version)}
		if r, ok := n.(UpdateRestarter); ok {
			next.Restart = r.RestartPending()
		}
	}
	s.mu.Lock()
	same := s.ui.Update == next
	s.mu.Unlock()
	if !same {
		s.UpdateUI(func(u *UIState) { u.Update = next })
	}
}

// cleanUpdateVersion keeps a version text the window and agents may show: at most maxUpdateVersion
// bytes of letters, digits, spaces and ".+-_()"; "" for anything else. The appcast is signed
// (SURequireSignedFeed), so this guards against a mistake, not an attacker.
func cleanUpdateVersion(v string) string {
	v = strings.TrimSpace(v)
	if len(v) > maxUpdateVersion {
		return ""
	}
	for _, r := range v {
		ok := r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || strings.ContainsRune(" .+-_()", r)
		if !ok {
			return ""
		}
	}
	return v
}

// updater returns the session's updater; nil without one.
func (s *Session) updater() Updater {
	s.updates.mu.Lock()
	defer s.updates.mu.Unlock()
	return s.updates.u
}

// UpdatesUnavailable returns why this Cogpit cannot check for updates, nil when it can: the
// enabled state of app.checkForUpdates.
func (s *Session) UpdatesUnavailable() error {
	u := s.updater()
	if u == nil {
		return ErrNoUpdater
	}
	return u.Unavailable()
}

// RefuseUpdateCheck refuses app.checkForUpdates from another origin than the GUI where the check
// acts (UpdateActor).
func (s *Session) RefuseUpdateCheck(ctx context.Context) error {
	if a, ok := s.updater().(UpdateActor); ok && a.CheckActs() && OriginOf(ctx) != OriginGUI {
		return ErrUpdateCheckGUIOnly
	}
	return nil
}

// CheckForUpdates checks for updates now, with the updater's own window.
func (s *Session) CheckForUpdates() error {
	if err := s.UpdatesUnavailable(); err != nil {
		return err
	}
	return s.updater().CheckForUpdates()
}

// UpdateInstallUnavailable returns why app.installUpdate cannot run now, nil when it can: an
// updater, and a newer release it holds (UIState.Update).
func (s *Session) UpdateInstallUnavailable() error {
	if err := s.UpdatesUnavailable(); err != nil {
		return err
	}
	s.mu.Lock()
	available := s.ui.Update.Available
	s.mu.Unlock()
	if !available {
		return ErrNoUpdateAvailable
	}
	return nil
}

// InstallUpdate installs the newer release the Update button names (app.installUpdate): through
// the updater's UpdateInstaller where it has one (Windows), else as CheckForUpdates, whose window
// offers the release (macOS) or which installs it (Linux).
func (s *Session) InstallUpdate() error {
	if err := s.UpdateInstallUnavailable(); err != nil {
		return err
	}
	u := s.updater()
	if i, ok := u.(UpdateInstaller); ok {
		return i.InstallUpdate()
	}
	return u.CheckForUpdates()
}

// updatePrefsChanged hands a change of the automatic checks to the updater.
func (s *Session) updatePrefsChanged(old, next Preferences) {
	if old.CheckForUpdates == next.CheckForUpdates {
		return
	}
	s.updates.mu.Lock()
	defer s.updates.mu.Unlock()
	if s.updates.u != nil && s.updates.u.Unavailable() == nil {
		s.updates.u.SetAutomaticChecks(next.CheckForUpdates)
	}
}

// RefuseUpdateChecks refuses a preferences change that turns the automatic update checks off from
// another origin than the GUI.
func RefuseUpdateChecks(ctx context.Context, on *bool) error {
	if on != nil && !*on && OriginOf(ctx) != OriginGUI {
		return ErrUpdateChecksGUIOnly
	}
	return nil
}

// CheckUpdateChannel refuses what is not an update channel Cogpit offers: stable ("" means it
// too). Beta is refused for every origin until prereleases exist; once it is offered, only the
// user is to choose it (the lead's default: a prerelease is less tested code).
func CheckUpdateChannel(ch string) error {
	switch ch {
	case "", UpdateChannelStable:
		return nil
	case UpdateChannelBeta:
		return ErrNoBetaChannel
	}
	return fmt.Errorf("update channel %q: only %s", ch, UpdateChannelStable)
}
