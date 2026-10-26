package commands

import (
	"context"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The window's zoom (D-134; engine/zoom.go): VS Code's Zoom In, Zoom Out and Reset Zoom with VS
// Code's keys on each platform (its windowActions.ts, the owner's wish of 2026-10-26: "die exakt
// gleichen Tastenkombinationen wie VSCode auf der jeweiligen Plattform"). Zoom In is Cmd+= with
// Cmd+Shift+= and the keypad's + (Fyne names the + key of a German keyboard as the keypad's);
// Zoom Out Cmd+- with Cmd+Shift+- and the keypad's -, on Linux without Cmd+Shift+-, which is Go
// Forward there; Reset Zoom VS Code's Cmd+Numpad0, which Fyne cannot tell from Cmd+0
// (glfwKeyToKeyName reads both as 0), so Cmd+0 resets too. The level is a preference
// (Preferences.ZoomLevel); the window applies it (fyneui/zoom.go).

// ZoomResult is the zoom commands' result.
type ZoomResult struct {
	ZoomLevel int    `json:"zoomLevel"`
	Zoom      string `json:"zoom"` // as the status bar shows it: "120%"
}

// zoomResult is the result for kept preferences.
func zoomResult(p engine.Preferences) ZoomResult {
	return ZoomResult{ZoomLevel: p.ZoomLevel, Zoom: engine.ZoomPercent(p.ZoomLevel)}
}

// zoomTo keeps the level step returns for the kept one.
func zoomTo(s *engine.Session, step func(level int) int) (any, error) {
	p, err := s.Preferences()
	if err != nil {
		return nil, err
	}
	if p, err = s.SetZoomLevel(step(p.ZoomLevel)); err != nil {
		return nil, err
	}
	return zoomResult(p), nil
}

// zoomEnabled disables a zoom command where it would not change the level.
func zoomEnabled(s *engine.Session, at func(level int) error) error {
	p, err := s.Preferences()
	if err != nil {
		return err
	}
	return at(p.ZoomLevel)
}

const zoomScales = " The whole window scales, text, icons, room and panels alike, by 1.2 per level (VS Code's zoom), " +
	"from 50% to 210%; the level is kept in the preferences (zoomLevel) and outlives the run. Returns the zoom level and " +
	"the zoom as the status bar shows it."

func registerZoom(g *registrar) {
	add(g, engine.Spec[struct{}]{
		ID: "ui.zoom.in", Label: "Zoom In", MenuPath: "View/Zoom In", Danger: engine.DangerUI,
		Shortcut: "Cmd+=", MoreKeys: []string{"Cmd+Shift+=", "Cmd+Plus"},
		Description: "Makes the window one zoom level larger, as the status bar's magnifier with + and VS Code's Zoom In do; " +
			"disabled where the window's content would no longer fit the window, which a larger window or full screen allows." + zoomScales,
		Enabled: func(s *engine.Session, _ struct{}) error { return zoomEnabled(s, s.ZoomInError) },
		Run: func(_ context.Context, s *engine.Session, _ struct{}) (any, error) {
			return zoomTo(s, func(level int) int { return level + 1 })
		},
	})
	add(g, engine.Spec[struct{}]{
		ID: "ui.zoom.out", Label: "Zoom Out", MenuPath: "View/Zoom Out", Danger: engine.DangerUI,
		Shortcut: "Cmd+-", MoreKeys: []string{"Cmd+Shift+-"},
		PlatformKeys: map[string][]string{"linux": {"Cmd+-"}},
		Description:  "Makes the window one zoom level smaller, as the status bar's magnifier with − and VS Code's Zoom Out do." + zoomScales,
		Enabled: func(s *engine.Session, _ struct{}) error {
			return zoomEnabled(s, func(level int) error {
				if level <= engine.MinZoomLevel {
					return engine.ErrZoomMin
				}
				return nil
			})
		},
		Run: func(_ context.Context, s *engine.Session, _ struct{}) (any, error) {
			return zoomTo(s, func(level int) int { return level - 1 })
		},
	})
	add(g, engine.Spec[struct{}]{
		ID: "ui.zoom.reset", Label: "Reset Zoom", MenuPath: "View/Reset Zoom", Danger: engine.DangerUI,
		Shortcut:    "Cmd+0",
		Description: "Sets the window's zoom back to 100%, as a click on the status bar's zoom and VS Code's Reset Zoom do." + zoomScales,
		Enabled: func(s *engine.Session, _ struct{}) error {
			return zoomEnabled(s, func(level int) error {
				if level == 0 {
					return engine.ErrZoomReset
				}
				return nil
			})
		},
		Run: func(_ context.Context, s *engine.Session, _ struct{}) (any, error) {
			return zoomTo(s, func(int) int { return 0 })
		},
	})
}
