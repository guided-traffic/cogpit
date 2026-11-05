package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Remove finalizers (WF43, D-98) needs little code of the window's own: the row menu and the
// drawer's toolbar offer it from the registry while an object is being deleted, a Terminating
// namespace's Blocking Deletion section beside each object as an item action, the selection bar
// beside Delete while a selected object is being deleted and holds finalizers (tablecheck.go), and
// the confirmation is the engine's.

// iconUnlock is Remove finalizers' icon: an open padlock, the hold on the object's deletion
// released (Cogpit's drawing, clean-room; the owner found scissors a poor sign, 2026-11-04). A
// shackle lifted out of the body on the left over a body with a keyhole, filled shapes on the 24 unit grid so that
// Fyne draws it in the theme's colours like its own icons.
var iconUnlock = fyne.NewStaticResource("cogpit-unlock.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">`+
	`<path d="M7 6.5a5 5 0 0 1 10 0V11h-2V6.5a3 3 0 0 0-6 0z"/>`+
	`<path fill-rule="evenodd" d="M7 11h10a2 2 0 0 1 2 2v6a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2v-6a2 2 0 0 1 2-2zm5 3a1.5 1.5 0 0 0-.75 2.8v1.7h1.5v-1.7A1.5 1.5 0 0 0 12 14z"/></svg>`))

func init() {
	commandResources[engine.RemoveFinalizersCommand] = func() fyne.Resource { return theme.NewThemedResource(iconUnlock) }
}
