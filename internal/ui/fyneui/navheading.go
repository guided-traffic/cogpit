package fyneui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// A heading in the navigator's tree (navHeading; the owner, 2026-11-03: a divider below the items,
// then "Integrations" with Flux CD and Argo CD under it): the divider and the heading's name in
// small bold lower case in the muted colour, without the bracket under it (the owner, at his look:
// "ohne diesen Balken und nur in Grau"). It is only a heading: no click, no hover, no fold, and no
// key reaches it; the groups below it are top-level ones that fold as any group does. A search
// keeps it while it keeps one of its items, and a search for its name keeps all of them.

// heading returns a heading's object, made once.
func (t *navTree) heading(id string) fyne.CanvasObject {
	if h := t.heads[id]; h != nil {
		return h
	}
	label := newTextLine(strings.ToLower(t.s.titles[id]), colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{Bold: true})
	h := container.NewVBox(navDivider(), inset(theme.SizeNamePadding, theme.SizeNameInnerPadding, label))
	t.heads[id] = h
	return h
}
