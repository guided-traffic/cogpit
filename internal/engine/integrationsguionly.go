package engine

import (
	"context"
	"errors"
)

// Who switches a cluster's integrations on (H-126, the owner's decision of 2026-10-19 afternoon:
// "Nein"). An integration switched on is looked for and read with the user's identity, and the
// window's question about a new cluster's integrations asks the user for that consent before
// anything is looked for (D-82). Only the user gives it, in Cogpit's window:
//
//   - the question's commands that look for or choose, cluster.integrations.step, .lookFor and
//     .answer, are GUI-only;
//   - switching an integration on anywhere else is refused for every other origin, before anything is
//     read, written or asked: cluster.settings.set's integrations named true and a metricsSource
//     that names a provider (auto, prometheus, metrics-server), and preferences.set's
//     integrationDefaults named true, which is what the question's Check all looks for
//     (RefuseIntegrationsOn);
//   - agents and the CLI may switch an integration off, which only stops reads (the lead's decision,
//     2026-10-19), but not on a cluster whose question the window still has to ask: there the
//     switch would end the question with what the user ticked so far as the cluster's choice, or
//     with none, which is answering it (Session.RefuseIntegrationSwitches).
//
// The question's Later (cluster.integrations.later) and Ask again (cluster.integrations.ask) stay
// open to agents: both make Cogpit read less, never more (the lead's decision, 2026-10-19).

// ErrIntegrationsGUIOnly refuses switching an integration on, or answering the question of a
// cluster's integrations, from anywhere but the user's own action in Cogpit's window.
var ErrIntegrationsGUIOnly = errors.New("only the user, in Cogpit's own window, can switch a cluster's integrations on or answer " +
	"the question about a new cluster's integrations (Cluster Settings › Integrations, the question's dialog): an integration " +
	"switched on is looked for and read with the user's identity, and that consent is the user's; agents and the CLI may " +
	"switch integrations off on a cluster the user has chosen them for")

// RefuseIntegrationsOn refuses switching integrations on from anywhere but the user's own action
// in Cogpit's window.
func RefuseIntegrationsOn(ctx context.Context, on IntegrationSet) error {
	if on&IntAll != 0 && OriginOf(ctx) != OriginGUI {
		return ErrIntegrationsGUIOnly
	}
	return nil
}

// MetricsSourceOn returns the metrics integrations a Metrics source switches on, as
// ParseMetricsSource keeps it: both for Automatic (""), the one it names for prometheus or
// metrics-server, none for none.
func MetricsSourceOn(source string) IntegrationSet {
	return IntMetrics &^ integrationsOffOfSource(source)
}

// RefuseIntegrationSwitches refuses, from anywhere but the user's own action in Cogpit's window, a
// change of a context's integrations that switches one on, and any change of a context whose
// question the window still has to ask (no choice of its own, in a session that asks before it
// looks, not the demo cluster): there the change would be the question's answer.
func (s *Session) RefuseIntegrationSwitches(ctx context.Context, contextID string, on IntegrationSet) error {
	if OriginOf(ctx) == OriginGUI {
		return nil
	}
	if err := RefuseIntegrationsOn(ctx, on); err != nil {
		return err
	}
	u := s.UIState()
	if u.IntegrationsAskFirst && !u.IntegrationsDecided(contextID) && !u.demoCluster(contextID) {
		return ErrIntegrationsGUIOnly
	}
	return nil
}
