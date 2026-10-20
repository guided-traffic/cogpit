package commands

import (
	"context"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Updates (D-4; engine/updates.go): Help › Check for Updates… checks Cogpit's appcast now with
// Sparkle's own window. A UI command any origin runs on macOS: it changes nothing, and installing
// stays the user's click in Sparkle's window. On Linux (D-108) the check acts (it installs through
// apt after polkit's password prompt, or restarts Cogpit into the release apt installed), so only
// the window runs it there. The automatic checks and the channel are preferences
// (preferences.set's checkForUpdates and updateChannel).

// checkForUpdatesResult is what app.checkForUpdates returns.
type checkForUpdatesResult struct {
	// Started: the check runs; Sparkle's window shows its result to the user (on Linux a toast).
	Started bool `json:"started"`
}

func registerUpdates(g *registrar) {
	add(g, engine.Spec[struct{}]{
		ID: "app.checkForUpdates", Label: "Check for Updates…", MenuPath: "Help/Check for Updates…", Danger: engine.DangerUI,
		Description: "Checks Cogpit's update feed now, as Help › Check for Updates… does in the macOS app: Sparkle's window " +
			"shows the user whether a newer release exists, its release notes and an Install button; nothing is installed " +
			"without the user's click there, and the result is not returned. On Linux, where Cogpit updates through apt, " +
			"the check installs the newer release after an administrator's password or restarts Cogpit into the release apt " +
			"installed, so only the user runs it there, in the window; agents read ui_state's update (available, version, " +
			"restart). Disabled, with the reason, where this Cogpit does not update itself: cogpit-cli, a build without the " +
			"update key, an app not installed from the disk image or the Linux package, Windows. Whether the app also checks " +
			"every hour is the preference checkForUpdates (preferences_set). Returns {started}.",
		Enabled: func(s *engine.Session, _ struct{}) error { return s.UpdatesUnavailable() },
		Run: func(ctx context.Context, s *engine.Session, _ struct{}) (any, error) {
			if err := s.RefuseUpdateCheck(ctx); err != nil {
				return nil, err
			}
			if err := s.CheckForUpdates(); err != nil {
				return nil, err
			}
			return checkForUpdatesResult{Started: true}, nil
		},
	})
}
