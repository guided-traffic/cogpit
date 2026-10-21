package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
)

// One short name for a cluster, everywhere (D-68, the owner, 2026-10-16): the window names a
// context by engine.ClusterName (sh.identity's name), the display name of its Cluster Settings,
// else the part of its name after the last "@". The navigator's root and rows, the hotbar's tiles,
// the status bar, the tabs, the command palette and the welcome page show it (not the top bar, D-54);
// the full context name stays in each tooltip (here: the status bar's), in the welcome page's second
// line and in Cluster Settings.

// tipLine is a line of text with a tooltip: a cluster's short name in the status bar, its full
// name as the tooltip.
type tipLine struct {
	textLine
	sh  *shell
	tip string // "" for none
}

var _ desktop.Hoverable = (*tipLine)(nil)

func newTipLine(sh *shell, c fyne.ThemeColorName, size fyne.ThemeSizeName, style fyne.TextStyle) *tipLine {
	l := &tipLine{textLine: textLine{color: c, size: size, style: style}, sh: sh}
	l.ExtendBaseWidget(l)
	return l
}

// MouseIn implements desktop.Hoverable.
func (l *tipLine) MouseIn(ev *desktop.MouseEvent) {
	if l.sh != nil {
		l.sh.tip.hover(l, l.tip, ev.AbsolutePosition)
	}
}

// MouseMoved implements desktop.Hoverable.
func (l *tipLine) MouseMoved(ev *desktop.MouseEvent) {
	if l.sh != nil {
		l.sh.tip.moved(l, ev.AbsolutePosition)
	}
}

// MouseOut implements desktop.Hoverable.
func (l *tipLine) MouseOut() {
	if l.sh != nil {
		l.sh.tip.hideFor(l)
	}
}
