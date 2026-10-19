package engine

import (
	"context"
	"errors"
)

// A cluster's display name and icon are the user's (H-125, the owner's decision of 2026-10-19
// afternoon: "Nein, sollen sie nicht können."). They are what the window shows of which cluster this
// is: the name in the hotbar, the navigator, the tab bar, the title bar and the status bar (D-68),
// the icon on the hotbar's tile and the tabs. An agent that could change them could rename a
// production cluster "dev" and colour its icon green, so only the user changes them, in Cogpit's
// window, as only the user sets the accent colour (D-80, accentcolor.go): cluster.settings.set's
// name and iconColor are refused for every other origin, set or cleared, before anything is read,
// written or asked (RefuseClusterIdentity), and cluster.settings.resetIcon and
// cluster.settings.uploadIcon are GUI-only. Agents read them (cluster.settings.get, ui_state's
// clusterSettings), so that they can tell the user which cluster they act in.

// ErrClusterIdentityGUIOnly refuses a change of a cluster's display name or icon from anywhere but
// the user's own action in Cogpit's window.
var ErrClusterIdentityGUIOnly = errors.New("only the user, in Cogpit's own window, can change a cluster's display name or its icon " +
	"(Cluster Settings › General): they are how the window shows which cluster this is, and an agent that could change them could " +
	"make a production cluster look like a development one; agents and the CLI read them in cluster_settings_get and ui_state")

// RefuseClusterIdentity refuses a display name or an icon colour, set or cleared, from anywhere but
// the user's own action in Cogpit's window.
func RefuseClusterIdentity(ctx context.Context, name, iconColor *string) error {
	if (name != nil || iconColor != nil) && OriginOf(ctx) != OriginGUI {
		return ErrClusterIdentityGUIOnly
	}
	return nil
}
