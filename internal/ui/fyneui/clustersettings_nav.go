package fyneui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// The integrations in a cluster's Settings list (the owner's WF22 of 2026-10-18, D-87): under
// Agents a divider, then the heading "INTEGRATIONS" in the look of a setting's heading (upper case,
// small, bold, muted), then one item per integration. The heading is a section too, Integrations,
// which holds what concerns them all (the note while the cluster has no choice, Ask again, every
// integration's switch at a glance): a click opens it, the pointer over it shows the items' hover,
// and it is marked as the items are while it is shown.

// navDivider is the line in the list above the integrations, with room above and below it.
func navDivider() fyne.CanvasObject {
	return inset("", theme.SizeNameInnerPadding, newDivider(false))
}

// newNavHeading turns a list item's button into the integrations' heading: the button keeps the
// click, the hover and the mark of the shown section; the label over it is a heading's.
func newNavHeading(b *widget.Button, label string) fyne.CanvasObject {
	b.SetText("")
	c := colorMuted
	if b.Importance == widget.HighImportance {
		c = theme.ColorNameForegroundOnPrimary
	}
	line := newTextLine(strings.ToUpper(label), c, theme.SizeNameCaptionText, fyne.TextStyle{Bold: true})
	line.truncate = true
	return container.NewStack(b, inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding,
		container.NewVBox(layout.NewSpacer(), line, layout.NewSpacer())))
}
