package fyneui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// The settings card's header (the owner's VW11 and VW12): a cluster's page is titled "Cluster
// Settings: <name>", and its icon stands 5 points inside the card's edge above and at the left (the
// card's own 1-point outline before it), the × as far from the right edge; Preferences' header has
// the same room.
func TestSettingsCardHeader(t *testing.T) {
	s, _ := settingsEngine(t)
	mustDispatch(t, s, "cluster.settings.set", map[string]any{"context": "kind-dev", "name": "Dev cluster"})
	sh, w, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": "kind-dev"})
	p := sh.settings
	q.until(t, "the cluster's card", func() bool { return p.visible() && p.view != nil })
	settle(w)
	if p.title.text != "Cluster Settings: Dev cluster" {
		t.Errorf("the title %q", p.title.text)
	}
	edge := sh.app.Settings().Theme().Size(theme.SizeNameInputBorder) // the card's outline
	room := func(o fyne.CanvasObject) fyne.Position { return absPos(o).Subtract(absPos(p.root)) }
	if got, want := room(p.badge), fyne.NewPos(edge+settingsHeaderRoom, edge+settingsHeaderRoom); got != want {
		t.Errorf("the icon at %v from the card's corner, want %v", got, want)
	}
	if right := p.root.Size().Width - (absPos(p.closeB).X - absPos(p.root).X + p.closeB.Size().Width); right != edge+settingsHeaderRoom {
		t.Errorf("the × %v from the card's right edge, want %v", right, edge+settingsHeaderRoom)
	}

	mustDispatch(t, s, "preferences.open", map[string]any{"section": "app"})
	q.until(t, "Preferences", func() bool { return p.visible() && p.title.text == "Preferences" })
	settle(w)
	// The column that keeps the header as high without the icon is empty: the title after the room
	// and the border layout's padding.
	pad := sh.app.Settings().Theme().Size(theme.SizeNamePadding)
	if p.badge.Visible() || room(p.title).X != edge+settingsHeaderRoom+pad {
		t.Errorf("Preferences' title at %v (the icon shown %v)", room(p.title), p.badge.Visible())
	}
}
