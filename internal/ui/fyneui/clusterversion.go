package fyneui

import (
	"runtime"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/k8seol"
)

// The cluster's Kubernetes version in the navigator's root row (D-91, the owner's WF30 of
// 2026-10-18): the API server's version on a line of its own under the user, so that neither the
// cluster's name nor the user is cut for it in the navigator's default width, in the muted colour
// while upstream supports its minor, in the pending colour during the minor's maintenance mode
// (its last two months) and in the warning colour with a ⚠ past its end of life. Its tooltip says
// until when upstream supports it and how many days are left, or since when it ended, "about …
// estimated" for a minor newer than this Cogpit's release table, and the day of the table; dates
// in the computer's format (D-31). Shown while the cluster is live (UIState.ClusterVersion).
//
// And the gear before the root's dot (D-92, the owner's WF29): Cluster Settings of the shown
// cluster, as ⌘⇧, and the row's menu open them.

// versionLine is what the root row shows of a cluster's version on now's day: its text, colour and
// tooltip.
func versionLine(cv *engine.ClusterVersion, now time.Time, tf model.TimeFormat) (string, fyne.ThemeColorName, string) {
	now, date := engine.VersionDates(tf, now)
	tip := cv.Text(now, date)
	if d, err := k8seol.ParseDate(cv.TableFetched); err == nil {
		tip += "\nUpstream dates as of " + date(d)
	}
	switch cv.StateAt(now) {
	case k8seol.EndOfLife:
		return cv.Version + " ⚠", colorStatusWarning, tip
	case k8seol.Maintenance:
		return cv.Version, statusColorName(model.StatusPending), tip
	}
	return cv.Version, colorMuted, tip
}

// showVersion shows the root's cluster version, or hides it: no context, not live, not known yet.
func (s *sidebar) showVersion(ui engine.UIState) {
	cv := ui.ClusterVersion
	if cv == nil || ui.Context == "" || cv.Context != ui.Context {
		s.versionTip = ""
		if s.version.Visible() {
			s.version.Hide()
			s.versionLineChanged()
		}
		return
	}
	text, c, tip := versionLine(cv, s.sh.now(), s.sh.timeFormat())
	s.version.SetText(text)
	s.version.SetColor(c)
	s.versionTip = tip
	if !s.version.Visible() {
		s.version.Show()
		s.versionLineChanged()
	}
}

// versionLineChanged lays the root row and the tree out again: the row grew or shrank by the
// version's line.
func (s *sidebar) versionLineChanged() {
	s.rootRow.body.Refresh()
	if s.tree != nil {
		s.tree.scroll.Refresh()
	}
}

// versionChanged reports whether the shown cluster's version changed between two states (a
// reconnect to an upgraded API server, a day passing).
func versionChanged(st, old engine.UIState) bool {
	a, b := st.ClusterVersion, old.ClusterVersion
	if a == nil || b == nil {
		return a != b
	}
	return *a != *b
}

// settingsTip is the root's gear's tooltip: "Cluster Settings (⌘⇧,)".
func (sh *shell) settingsTip() string {
	tip := "Cluster Settings"
	if c, ok := sh.eng.Registry().Get("cluster.settings.open"); ok {
		if sc := shortcutText(c.Shortcut, runtime.GOOS == goosMac); sc != "" {
			tip += " (" + sc + ")"
		}
	}
	return tip
}

// newRootSettings is the root's gear: Cluster Settings of the cluster the root shows; nil where
// the registry has no such command.
func newRootSettings(s *sidebar) *navIconButton {
	if _, ok := s.sh.eng.Registry().Get("cluster.settings.open"); !ok {
		return nil
	}
	return newNavIconButton(s.sh, theme.SettingsIcon(), s.sh.settingsTip(), func() {
		if ctx := s.sh.ui.Context; ctx != "" {
			s.sh.openClusterSettings(ctx)
		}
	})
}
