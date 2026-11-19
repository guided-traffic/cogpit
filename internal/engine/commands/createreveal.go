package commands

import (
	"context"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// After a create in the window, the new object is shown (A102, owner): its kind's view, the list
// scrolled to its row (the window scrolls to the selection) and its drawer, as ui.navigate shows an
// object. The lead's night decisions for the owner to confirm: only the user's own create moves the
// window (an agent's create leaves the window where the user has it), and only a create of exactly
// one object (several: the toast names them; which one to show is no clear choice).

// revealCreated navigates to the object a create made, under the rules above. A navigation that
// fails leaves the create's result as it is.
func revealCreated(ctx context.Context, s *engine.Session, res engine.CreateResult) {
	if engine.OriginOf(ctx) != engine.OriginGUI || res.Created != 1 || res.Failed != 0 {
		return
	}
	for _, it := range res.Items {
		if it.Created {
			_, _ = navigate(ctx, s, refParams{Ref: it.Ref})
			return
		}
	}
}
