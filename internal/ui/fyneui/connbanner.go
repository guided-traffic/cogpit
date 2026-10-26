package fyneui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// connBanner is the thin strip over the view while the connected cluster's connection is
// interrupted or its login expired (D-58, S3): what is interrupted, since when, Cogpit's next
// attempt, and Reconnect (cluster.reconnect, a new connection from the kubeconfig file, D-81); for
// an expired login of a context with a credential plugin Log in again (cluster.login), Reconnect
// beside it; for one without, Reconnect alone, since nothing but the file can renew it (the owner's
// F28 of 2026-10-17). It reads only the UI state, so it says what an agent reads in ui_state. The
// window stays usable: the views keep the cluster's state as of the interruption, and writes are
// disabled with the reason in their menus and buttons. Lens's behaviour on a lost connection is not
// captured: this is Cogpit's own.
//
// A connected cluster whose API server was upgraded to another minor since the connect
// (Connection.Upgraded, the owner's "Option A" of 2026-10-26) shows the strip in the accent colour:
// the version it runs now, the one Cogpit connected to, that the kinds and API versions are that
// one's, and Reconnect. A patch shows no strip: the navigator's version follows it. The lines are
// selectable (drag, ⌘C).
type connBanner struct {
	sh        *shell
	root      *fyne.Container // hidden while the connection works
	title     *textLine
	detail    *textLine
	action    *textButton
	reconnect *textButton // Reconnect beside Log in again; hidden otherwise
	icon      *widget.Icon
	tint      *bannerTint
	ticker    *time.Ticker // the countdown to the next attempt; nil while hidden
	stop      chan struct{}
}

func newConnBanner(sh *shell) *connBanner {
	b := &connBanner{sh: sh}
	b.title = newTextLine("", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})
	b.title.truncate = true
	b.detail = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	b.detail.truncate = true
	b.action = newPlateButton(reconnectLabel, reconnectIcon(), b.act)
	b.action.Importance = widget.HighImportance
	b.reconnect = newPlateButton(reconnectLabel, reconnectIcon(), func() { b.sh.reconnect(b.sh.ui.Context) })
	b.reconnect.Hide()
	b.icon = widget.NewIcon(themedIcon(theme.WarningIcon(), colorStatusWarning))
	b.tint = newBannerTint(colorStatusWarning)
	copied := func(sel string) { b.sh.copyText(sel, false) }
	content := container.NewBorder(nil, nil, container.NewCenter(b.icon),
		container.NewCenter(container.NewHBox(b.reconnect, b.action)),
		container.NewVBox(newSelLine(b.title, nil, copied), newSelLine(b.detail, nil, copied)))
	b.root = container.NewVBox(container.NewStack(b.tint, inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, content)), newDivider(false))
	b.root.Hide()
	return b
}

// update follows the UI state's connection. Main goroutine only.
func (b *connBanner) update() {
	c := b.sh.ui.Connection
	switch {
	case c.State == engine.Interrupted, c.State == engine.LoginExpired:
	case c.State == engine.Connected && c.Upgraded != nil:
	default:
		b.hide()
		return
	}
	b.fill(c)
	if !b.root.Visible() {
		b.root.Show()
		b.startTicker()
	}
	b.root.Refresh()
}

// fill writes the banner's lines and its button for c.
func (b *connBanner) fill(c engine.Connection) {
	name, _ := splitContextID(b.sh.ui.Context)
	tf := b.sh.timeFormat()
	since := tf.FormatClock(c.Since)
	b.reconnect.Hide()
	if c.State == engine.Connected {
		b.fillUpgraded(name, c)
		return
	}
	if c.State == engine.LoginExpired {
		b.title.SetText(fmt.Sprintf("The login to %s expired · since %s", name, since))
		b.tint.color = colorStatusError
		b.icon.SetResource(themedIcon(theme.ErrorIcon(), colorStatusError))
		if c.CredentialPlugin {
			b.detail.SetText(loginDetail(c, since))
			b.action.SetText("Log in again")
			b.action.SetIcon(theme.LoginIcon())
			b.reconnect.Show() // the file may hold another login than the connection's (D-81)
		} else {
			b.detail.SetText(fileLoginDetail(c, since))
			b.setReconnect()
		}
		switch {
		case c.Reconnecting:
			b.reconnecting()
		case c.LoggingIn:
			b.action.SetText("Logging in…")
			b.action.Disable()
			b.reconnect.Disable()
		default:
			b.action.Enable()
			b.reconnect.Enable()
		}
		b.tint.Refresh()
		return
	}
	b.title.SetText(fmt.Sprintf("The connection to %s is interrupted · since %s", name, since))
	b.detail.SetText(interruptedDetail(c, since, b.sh.now()))
	b.tint.color = colorStatusWarning
	b.icon.SetResource(themedIcon(theme.WarningIcon(), colorStatusWarning))
	b.setReconnect()
	if c.Reconnecting {
		b.reconnecting()
	} else {
		b.action.Enable()
	}
	b.tint.Refresh()
}

// fillUpgraded writes the strip of a cluster upgraded to another minor while connected.
func (b *connBanner) fillUpgraded(name string, c engine.Connection) {
	up := c.Upgraded
	b.title.SetText(fmt.Sprintf("The API server of %s runs %s now · Cogpit connected to %s", name, up.To, up.From))
	b.detail.SetText(upgradedDetail(up))
	b.tint.color = colorAccent
	b.icon.SetResource(themedIcon(theme.InfoIcon(), colorAccent))
	b.setReconnect()
	if c.Reconnecting {
		b.reconnecting()
	} else {
		b.action.Enable()
	}
	b.tint.Refresh()
}

// upgradedDetail is the second line of an upgraded cluster's strip.
func upgradedDetail(up *engine.ServerUpgrade) string {
	return fmt.Sprintf("The kinds and API versions Cogpit knows are the ones %s served: Reconnect loads %s's · "+
		"the tabs, views and filters stay · the connection works meanwhile", up.From, up.To)
}

// setReconnect makes the banner's button Reconnect.
func (b *connBanner) setReconnect() {
	b.action.SetText(reconnectLabel)
	b.action.SetIcon(reconnectIcon())
}

// reconnecting greys the buttons while a reconnect runs.
func (b *connBanner) reconnecting() {
	b.action.SetText(reconnectingText)
	b.action.SetIcon(reconnectIcon())
	b.action.Disable()
	b.reconnect.Hide()
}

// interruptedDetail is the second line while interrupted: why, the next attempt, what the window
// shows meanwhile.
func interruptedDetail(c engine.Connection, since string, now time.Time) string {
	next := "trying now…"
	if !c.NextAttempt.IsZero() {
		next = "next attempt " + attemptIn(c.NextAttempt.Sub(now))
	}
	parts := []string{"Cogpit reconnects by itself: " + next,
		"the views show the cluster as of " + since, "changes are disabled until it is back"}
	if r := strings.TrimSpace(c.Reason); r != "" {
		parts = append([]string{upperFirst(r)}, parts...)
	}
	return strings.Join(parts, " · ")
}

// fileLoginDetail is the second line while the login of a context without a credential plugin is
// expired: only the kubeconfig file can renew it (D-81).
func fileLoginDetail(c engine.Connection, since string) string {
	parts := []string{"Renew the login in the kubeconfig file: Cogpit reconnects by itself once it changes, and Reconnect reads it again now",
		"the views show the cluster as of " + since, "changes are disabled"}
	if r := strings.TrimSpace(c.Reason); r != "" {
		parts = append([]string{upperFirst(r)}, parts...)
	}
	return strings.Join(parts, " · ")
}

// loginDetail is the second line while the login is expired.
func loginDetail(c engine.Connection, since string) string {
	what := "Log in again runs the login program once; it may open your browser"
	if c.NeedsApproval {
		what = "The login program changed since you allowed it: Log in again connects anew and shows it to you first"
	}
	parts := []string{what, "the views show the cluster as of " + since, "changes are disabled"}
	if r := strings.TrimSpace(c.Reason); r != "" {
		parts = append([]string{upperFirst(r)}, parts...)
	}
	return strings.Join(parts, " · ")
}

// attemptIn says how long until the next attempt: "in 8 s", "in 1 min", "in a moment".
func attemptIn(d time.Duration) string {
	switch s := int(d.Round(time.Second) / time.Second); {
	case s <= 0:
		return "in a moment"
	case s < 60:
		return fmt.Sprintf("in %d s", s)
	default:
		return fmt.Sprintf("in %d min", (s+30)/60)
	}
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// act is the button: Reconnect, or Log in again.
func (b *connBanner) act() {
	c := b.sh.ui.Connection
	switch {
	case c.State == engine.Interrupted, c.State == engine.Connected, c.State == engine.LoginExpired && !c.CredentialPlugin:
		b.sh.reconnect(b.sh.ui.Context) // reconnect.go
	case c.State == engine.LoginExpired:
		name := b.sh.ui.Context
		b.sh.dispatch("cluster.login", struct{}{}, func(_ any, err error) {
			switch {
			case errors.Is(err, engine.ErrNeedsNewConnect):
				// The plugin changed: it is shown to the user first, then the context is connected
				// anew (D-26; the section's approval, as a drill into it shows it, D-57), its tabs
				// kept (cluster.reconnect, D-81).
				b.sh.kc.reviewPlugin(name, func(allowed bool) {
					if allowed {
						b.sh.dispatch("cluster.reconnect", contextParams{Context: name}, nil)
					}
				})
			case err != nil:
				b.sh.showError(err)
			}
		})
	}
}

// startTicker redraws the countdown every second while the banner shows.
func (b *connBanner) startTicker() {
	if b.ticker != nil {
		return
	}
	b.ticker = time.NewTicker(time.Second)
	b.stop = make(chan struct{})
	t, stop := b.ticker, b.stop
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-b.sh.stopTickers:
				return
			case <-t.C:
				b.sh.do(func() {
					if b.root.Visible() {
						b.fill(b.sh.ui.Connection)
					}
				})
			}
		}
	}()
}

func (b *connBanner) hide() {
	if b.ticker != nil {
		b.ticker.Stop()
		close(b.stop)
		b.ticker, b.stop = nil, nil
	}
	if b.root.Visible() {
		b.root.Hide()
		b.root.Refresh()
	}
}
