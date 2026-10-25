package commands

import (
	"context"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A cluster's light strip (D-122; engine/lightstrip.go): Cluster Settings › General, under the
// accent colour. Only the user chooses it, in Cogpit's window: the command is GUI-only, so Dispatch
// refuses agents and the CLI before anything is read or written, and no agent can calm a production
// cluster's strip down. Agents read it as lightStrip in ui_state and cluster_settings_get.

type lightStripParams struct {
	Context   string `json:"context,omitempty" jsonschema:"context id as cluster_contexts lists it; empty or omitted: the connected context"`
	Animation string `json:"animation" jsonschema:"how the strip under a view's header shows the cluster's accent colour while its tab is shown: static (steady, the default), breathing, scanner or flow; empty: static"`
}

// refuseLightStrip refuses a change of the light strip from anywhere but the window (Dispatch
// refuses it first; Run checks again).
func refuseLightStrip(ctx context.Context) error {
	if engine.OriginOf(ctx) != engine.OriginGUI {
		return engine.ErrLightStripGUIOnly
	}
	return nil
}

func registerLightStrip(g *registrar) {
	add(g, engine.Spec[lightStripParams]{
		ID: "cluster.settings.setLightStrip", Label: "Set Light Strip", Danger: engine.DangerUI,
		GUIOnly: engine.ErrLightStripGUIOnly,
		Description: "Chooses how the light strip under the window's view header shows a context's accent colour while one of its tabs " +
			"is shown, as Cluster Settings › General does: static (steady, the default), breathing, scanner or flow. Without " +
			"an accent colour the strip is frosted glass, so the choice is disabled until one is set. Only the user, in " +
			"Cogpit's window, can choose it: refused for agents and the CLI, so that no agent can calm a production " +
			"cluster's strip down. Agents read it as lightStrip in cluster_settings_get and in ui_state's clusterSettings." +
			clusterSettingsOnly + " Returns the settings.",
		Enabled: func(s *engine.Session, p lightStripParams) error {
			id, err := settingsContext(s, p.Context)
			if err != nil {
				return err
			}
			if cs, _ := s.ClusterSettingsOf(id); cs.AccentColor == "" {
				return engine.ErrLightStripNoAccent
			}
			return nil
		},
		Refuse: func(ctx context.Context, _ *engine.Session, _ lightStripParams) error { return refuseLightStrip(ctx) },
		Run: func(ctx context.Context, s *engine.Session, p lightStripParams) (any, error) {
			if err := refuseLightStrip(ctx); err != nil {
				return nil, err
			}
			anim, err := engine.ParseLightStrip(p.Animation)
			if err != nil {
				return nil, err
			}
			return changeSettings(s, p.Context, func(cs *engine.ClusterSettings) error {
				if cs.AccentColor == "" {
					return engine.ErrLightStripNoAccent
				}
				cs.LightStrip = anim
				return nil
			})
		},
	})
}
