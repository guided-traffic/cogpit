package fyneui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// A heading in the navigator's tree (navHeading; the owner, 2026-11-03: a divider below the items,
// then "Integrations" with Flux CD and Argo CD under it): the divider and the heading's name as a
// bracket label in the accent colour, the look Cluster Settings gives its integrations (D-87). It
// is only a heading: no click, no hover, no fold, and no key reaches it; the groups below it are
// top-level ones that fold as any group does. A search keeps it while it keeps one of its items, and
// a search for its name keeps all of them.

// heading returns a heading's object, made once.
func (t *navTree) heading(id string) fyne.CanvasObject {
	if h := t.heads[id]; h != nil {
		return h
	}
	label := newBracketLabel(strings.ToLower(t.s.titles[id]), colorAccent)
	h := container.NewVBox(navDivider(), inset(theme.SizeNamePadding, theme.SizeNameInnerPadding, label))
	t.heads[id] = h
	return h
}
