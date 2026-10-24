package commands

import (
	"context"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A cluster's accent colour (D-80; engine/accentcolor.go): Cluster Settings › General. Only the user
// sets or clears it, in Cogpit's window: the command is GUI-only, so Dispatch refuses agents and the
// CLI before anything is read or written, and no agent can make a production cluster look like a
// development one. Agents read it as accentColor in ui_state and cluster_settings_get.

type accentColorParams struct {
	Context string `json:"context,omitempty" jsonschema:"context id as cluster_contexts lists it; empty or omitted: the connected context"`
	Color   string `json:"color" jsonschema:"the accent colour: one of cyan, blue, violet, magenta, red, orange, yellow, green (the theme's accents) or any colour as #rrggbb; empty: none"`
}

// refuseAccentColor refuses a change of the accent colour from anywhere but the window (Dispatch
// refuses it first; Run checks again).
func refuseAccentColor(ctx context.Context) error {
	if engine.OriginOf(ctx) != engine.OriginGUI {
		return engine.ErrAccentColorGUIOnly
	}
	return nil
}

func registerAccentColor(g *registrar) {
	add(g, engine.Spec[accentColorParams]{
		ID: "cluster.settings.setAccentColor", Label: "Set Accent Colour", Danger: engine.DangerUI,
		GUIOnly: engine.ErrAccentColorGUIOnly,
		Description: "Sets or clears a context's accent colour, as Cluster Settings › General does: the user's mark of which " +
			"cluster it is (a production cluster in red, say), so that the user sees at a glance which cluster they act in: " +
			"the light strip under the window's tabs shows the shown tab's cluster's colour (cluster_settings_setLightStrip " +
			"chooses how; without a colour the strip is frosted glass). Only the user, in " +
			"Cogpit's window, can set or clear it: refused for agents and the CLI, so that no agent can make a production " +
			"cluster look like a development one. Agents read it as accentColor in cluster_settings_get and in ui_state's " +
			"clusterSettings." + clusterSettingsOnly + " Returns the settings.",
		Enabled: func(s *engine.Session, p accentColorParams) error {
			_, err := settingsContext(s, p.Context)
			return err
		},
		Refuse: func(ctx context.Context, _ *engine.Session, _ accentColorParams) error { return refuseAccentColor(ctx) },
		Run: func(ctx context.Context, s *engine.Session, p accentColorParams) (any, error) {
			if err := refuseAccentColor(ctx); err != nil {
				return nil, err
			}
			c, err := engine.ParseAccentColor(p.Color)
			if err != nil {
				return nil, err
			}
			return changeSettings(s, p.Context, func(cs *engine.ClusterSettings) error {
				cs.AccentColor = c
				return nil
			})
		},
	})
}
