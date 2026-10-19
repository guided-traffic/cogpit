package fyneui

import (
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Remove finalizers (WF43, D-98) needs no code of the window's own: the row menu and the drawer's
// toolbar offer it from the registry while an object is being deleted, a Terminating namespace's
// Blocking Deletion section beside each object as an item action, and the confirmation is the
// engine's. Its toolbar icon is scissors (Cogpit's choice: the finalizers are cut off).
func init() { commandIcons[engine.RemoveFinalizersCommand] = theme.IconNameContentCut }
