package fyneui

import (
	"errors"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Updates (D-4; engine/updates.go): Preferences › App › Check for updates automatically and Update
// channel, and Help › Check for Updates…. Lens's App section has the channel (Stable, Beta, Alpha;
// its documentation's list); Cogpit offers Stable until it publishes prereleases, and the row says
// so. Where this copy does not update itself (a development build, the app still on its disk
// image, cogpit-cli's engine), the check row says why and the Help item is greyed with the short
// reason. The update notice and its one-click install are Sparkle's own window, not Fyne's.

// checkForUpdatesCommand is Help › Check for Updates….
const checkForUpdatesCommand = "app.checkForUpdates"

// updateChannelChoices are the channel select's choices: its label and the preference's value.
var updateChannelChoices = []struct{ label, value string }{
	{"Stable", engine.UpdateChannelStable},
}

// shortReason is the part of a reason before its first colon: "this build has no update key",
// "not installed from the disk image", "not macOS" (internal/update's reasons are written so).
func shortReason(err error) string {
	head, _, _ := strings.Cut(err.Error(), ":")
	return strings.TrimSpace(head)
}

// updatesSettings returns Preferences › App's two update settings and the function that shows the
// preferences in them.
func (p *settingsPage) updatesSettings() ([]fyne.CanvasObject, func()) {
	check := widget.NewCheck("At every start and every hour; a new release installs with one click", func(on bool) {
		p.setPreference("checkForUpdates", on)
	})
	why := newParagraph("", colorMuted, settingsHelpWidth) // wrapped at word boundaries, never cut short
	why.size = theme.SizeNameCaptionText
	why.Hide()
	go func() { // Enabled is evaluated off the main goroutine, as every command's
		defer p.sh.recoverTo("the update preference") // D-11: a panic here would end the app
		err := p.sh.commandEnabled(checkForUpdatesCommand, struct{}{})
		if err == nil || errors.Is(err, errUnavailable) || err.Error() == "" {
			return
		}
		text := err.Error()
		text = strings.ToUpper(text[:1]) + text[1:]
		p.sh.do(func() {
			why.SetText(text)
			why.Show()
		})
	}()
	labels := make([]string, len(updateChannelChoices))
	for i, c := range updateChannelChoices {
		labels[i] = c.label
	}
	channel := newSelectField(labels, func(label string) {
		for _, c := range updateChannelChoices {
			if c.label == label {
				p.setPreference("updateChannel", c.value)
			}
		}
	})
	sync := func() {
		pr := p.prefs()
		check.SetChecked(pr.CheckForUpdates)
		for _, c := range updateChannelChoices {
			if c.value == pr.UpdateChannel || pr.UpdateChannel == "" && c.value == engine.UpdateChannelStable {
				channel.SetSelected(c.label)
			}
		}
	}
	return []fyne.CanvasObject{
		p.setting("checkForUpdates", "Check for updates automatically",
			"Help › Check for Updates… checks now. Nothing is installed without your click.", container.NewVBox(check, why)),
		p.setting("updateChannel", "Update channel", "Stable: Cogpit's releases. Beta comes once Cogpit publishes prereleases.", channel),
	}, sync
}

// updatesMenuItem is Help › Check for Updates…: greyed, with the short reason in its label, where
// this Cogpit does not update itself (a native menu item has no tooltip). The menu is built again
// when the connection changes; the command checks again when it runs.
func (sh *shell) updatesMenuItem() *fyne.MenuItem {
	c, ok := sh.eng.Registry().Get(checkForUpdatesCommand)
	if !ok {
		return nil
	}
	item := fyne.NewMenuItem(c.Label, func() { sh.dispatch(c.ID, struct{}{}, nil) })
	// Its Enabled reads the updater's state only, never the cache: cheap on the main goroutine.
	if err := sh.commandEnabled(c.ID, struct{}{}); err != nil {
		item.Disabled = true
		item.Label = c.Label + " (" + shortReason(err) + ")"
	}
	return item
}
