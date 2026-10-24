package engine

import (
	"context"
	"errors"
)

// Who switches a cluster's integrations: only the user, in Cogpit's window, both ways (H-126, the
// owner's decision of 2026-10-19 afternoon: "Nein"; H-133, the owner's decision of 2026-10-20:
// "GUI-only, beide Richtungen", built 2026-10-24). An integration switched on is looked for and read
// with the user's identity, and the window's question about a new cluster's integrations asks the
// user for that consent before anything is looked for (D-82); an integration switched off takes its
// alerts, image ratings or policy marks out of the window, which does not say who switched it, so a
// user who does not open Cluster Settings may read "no alerts" as none firing (H-133). So:
//
//   - the question's commands that look for or choose, cluster.integrations.step, .lookFor and
//     .answer, are GUI-only, and so is Ask again, cluster.integrations.ask: it forgets the user's
//     choice, which switches every integration of the cluster off until the user answers again
//     (the lead's decision, 2026-10-24; with Later after it, an agent could hide a cluster's
//     integrations for the run behind a dialog's flash);
//   - every other switch of an integration, on or off, is refused for every other origin, before
//     anything is read, written or asked: cluster.settings.set's integrations (true or false) and
//     its metricsSource (any value, none included: it switches the metrics integrations), and
//     preferences.set's integrationDefaults (true or false: the default for new clusters is what
//     the question's Check all looks for) (RefuseIntegrationSwitches).
//
// The question's Later (cluster.integrations.later) stays open to agents: it only puts off a
// question nobody has answered, whose cluster reads nothing meanwhile, so it hides nothing the user
// chose to see (the lead's decision, 2026-10-19, kept with H-133 on 2026-10-24).

// ErrIntegrationsGUIOnly refuses switching an integration on or off, or answering the question of a
// cluster's integrations, from anywhere but the user's own action in Cogpit's window.
var ErrIntegrationsGUIOnly = errors.New("only the user, in Cogpit's own window, can switch a cluster's integrations on or off, " +
	"change the default integrations for new clusters, or answer the question about a cluster's integrations or ask it " +
	"again (Cluster Settings › Integrations, Preferences, the question's dialog): an integration switched on is looked for " +
	"and read with the user's identity, and one switched off takes its alerts, image ratings or policy marks out of " +
	"the window; both are the user's choice")

// RefuseIntegrationSwitches refuses a switch of integrations, on or off, a cluster's or the default
// for new clusters, from anywhere but the user's own action in Cogpit's window.
func RefuseIntegrationSwitches(ctx context.Context) error {
	if OriginOf(ctx) != OriginGUI {
		return ErrIntegrationsGUIOnly
	}
	return nil
}
