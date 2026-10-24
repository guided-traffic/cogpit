package engine

import (
	"errors"
	"fmt"
	"strings"
)

// A cluster's accent colour (D-80; the owner's F26 of 2026-10-17): beside its icon, every cluster
// may carry a colour the user picks, so that later parts of the window can be marked with it and
// the user sees at a glance whether they work in a development or a production cluster, and does
// not act in production while believing to be in development. It is a setting
// (ClusterSettings.AccentColor, kept in cluster-settings.json), set on Cluster Settings › General
// and read by agents in ui_state's clusterSettings and cluster.settings.get; its first place in the
// window is the light strip under a view's header (D-new, the owner's VW14 of 2026-10-24: lightstrip.go),
// which shows the shown tab's cluster's colour, steady or moving. The choices are the icon
// colour's: one of IconColors or a free "#rrggbb", drawn in the theme's legible shade.
//
// Only the user sets or clears it, in Cogpit's window (cluster.settings.setAccentColor is GUI-only,
// the lead's decision of 2026-10-17): the colour is the user's own safety mark against acting in the
// wrong cluster, and an agent that could change it could make a production cluster look like a
// development one. Agents read it, so that they can tell which cluster the user marked as what.

// ErrAccentColorGUIOnly refuses a change of a cluster's accent colour from anywhere but the user's
// own action in Cogpit's window.
var ErrAccentColorGUIOnly = errors.New("only the user, in Cogpit's own window, can set or clear a cluster's accent colour " +
	"(Cluster Settings › General): it is the user's mark of which cluster this is, and an agent that could change it could " +
	"make a production cluster look like a development one; agents and the CLI read it as accentColor")

// ParseAccentColor reads an accent colour as given: one of IconColors or "#rrggbb", in any case
// and with spaces around; empty is none.
func ParseAccentColor(c string) (string, error) {
	c = strings.ToLower(strings.TrimSpace(c))
	if c != "" && !validIconColor(c) {
		return "", fmt.Errorf("accent colour %q: one of %s, #rrggbb, or empty for none", c, strings.Join(IconColors, ", "))
	}
	return c, nil
}
