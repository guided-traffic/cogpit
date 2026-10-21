package fyneui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
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

// newNavHeading turns a list item into the integrations' heading: the item keeps the click, the
// hover and the mark of the shown section; the bracket label over it, "integrations", is a
// heading's (a group's head in the design, in the light of cluster data).
func newNavHeading(b *navItem, label string) fyne.CanvasObject {
	b.Text = ""
	b.Refresh()
	return container.NewStack(b, inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding,
		container.NewVBox(layout.NewSpacer(), newBracketLabel(strings.ToLower(label), colorAccent), layout.NewSpacer())))
}
