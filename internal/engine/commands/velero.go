package commands

import (
	"context"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The Velero page's read (D-new): agents read what the page shows; ui.openView "velero/dashboard"
// shows it to the user. Nothing is written, nothing created (D-151).

type veleroDashboardParams struct {
	engine.OnCluster
	Namespaces []string `json:"namespaces,omitempty" jsonschema:"namespaces to narrow the page to (a schedule, backup or restore counts when its scope takes one of them in); omitted: the namespace filter the user has set (every namespace for a cluster the window does not show)"`
}

func registerVelero(g *registrar) {
	add(g, engine.Spec[veleroDashboardParams]{
		ID: "velero.dashboard", Label: engine.VeleroDashboardTitle, MenuPath: "View/" + engine.VeleroDashboardTitle, Danger: engine.DangerRead,
		Verbs: cacheReads,
		Description: "Reads the Velero page: the header (Velero's version, namespace, server and node agents ready of " +
			"desired, plugins), the tiles (Schedules, Backups, Restores and Storage counted by state), Needs attention " +
			"(storage locations unavailable, no default location, schedules overdue by Velero's own next-run rule or " +
			"failing validation, a schedule's latest backup failed or partly failed with its counts of errors and " +
			"warnings, failed backups no schedule made, repositories not ready or whose maintenance failed, node agents " +
			"not ready; worst first, then newest), the failed volume backups of those backups (PodVolumeBackups and " +
			"DataUploads in phase Failed, grouped by namespace and reason, each member's pod, volume, node and message), " +
			"the schedules (when in words, next run, the last seven runs with " +
			"missed ones, last completed, duration, scope, TTL, location, state) and the namespaces outside every " +
			"enabled schedule's scope (inferred from the schedules' namespaces, resources and selectors, never proof of " +
			"a backup; those with PVCs first). Read from the cache; nothing is written or created in the cluster. " +
			"ui_openView with view velero/dashboard shows the page to the user.",
		Enabled: func(s *engine.Session, p veleroDashboardParams) error {
			_, err := connectedOn(s, p.Context)
			return err
		},
		Run: func(ctx context.Context, s *engine.Session, p veleroDashboardParams) (any, error) {
			return s.VeleroDashboard(ctx, p.Context, p.Namespaces)
		},
	})
}
