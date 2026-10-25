package engine

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// A cluster's light strip (D-122, the owner's VW14 of 2026-10-24): the strip under the window's
// tabs shows the accent colour (accentcolor.go) of the cluster whose tab is shown, so that the user
// sees at a glance where they work ("Bin ich Beispielsweise auf Prod, wünsche ich mir einen Roten
// Akzent umd direkt zu wissen dass ich hier vorsichtig sein muss"). Without an accent colour the
// strip is off, frosted glass, whatever this says. With one it shows the colour steady (static, the
// default) or moving: breathing, a scanner, or a flow (his pick of the Steam Machine's light bar's
// modes, 2026-10-24). ClusterSettings.LightStrip keeps the choice, "" for static, as lightStrip in
// cluster-settings.json; the window draws it (fyneui/lightstrip.go).
//
// Only the user chooses it, in Cogpit's window (cluster.settings.setLightStrip is GUI-only, the
// lead's decision of 2026-10-24): an agent that could calm a production cluster's strip down would
// undo the user's own warning, the harm H-125 names for the accent colour. Agents read it.

// The light strip's animations, as lightStrip names them. LightStripStatic is kept as "" (the
// default, left out of the file).
const (
	LightStripStatic    = "static"
	LightStripBreathing = "breathing"
	LightStripScanner   = "scanner"
	LightStripFlow      = "flow"
)

// LightStrips are the light strip's animations in the order the window offers them.
var LightStrips = []string{LightStripStatic, LightStripBreathing, LightStripScanner, LightStripFlow}

// ErrLightStripGUIOnly refuses a change of a cluster's light strip from anywhere but the user's own
// action in Cogpit's window.
var ErrLightStripGUIOnly = errors.New("only the user, in Cogpit's own window, can choose a cluster's light strip " +
	"(Cluster Settings › General): it shows the user's own mark of which cluster this is, and an agent that could calm a " +
	"production cluster's strip down would undo the user's warning; agents and the CLI read it as lightStrip")

// ErrLightStripNoAccent disables the light strip's choice of a cluster without an accent colour:
// the strip shows the accent colour, and without one it is frosted glass.
var ErrLightStripNoAccent = errors.New("set an accent colour first: the light strip shows the cluster's accent colour, and without one it is frosted glass")

// ParseLightStrip reads a light strip's animation as given, in any case and with spaces around:
// one of LightStrips, empty for static. Static is returned as "", the way the settings keep it.
func ParseLightStrip(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s != "" && !slices.Contains(LightStrips, s) {
		return "", fmt.Errorf("light strip %q: one of %s", s, strings.Join(LightStrips, ", "))
	}
	if s == LightStripStatic {
		return "", nil
	}
	return s, nil
}

// checkLightStrip refuses a kept light strip that is not "" (static) or one of the animations.
func checkLightStrip(s string) error {
	if s == "" || (s != LightStripStatic && slices.Contains(LightStrips, s)) {
		return nil
	}
	return fmt.Errorf("light strip %q: one of %s", s, strings.Join(LightStrips[1:], ", "))
}

// LightStripOf returns a cluster's light strip by name: its animation, static when none is set.
func LightStripOf(cs ClusterSettings) string {
	if cs.LightStrip == "" {
		return LightStripStatic
	}
	return cs.LightStrip
}
