//go:build !nodemo

package main

import (
	"log/slog"
	"testing"

	"github.com/guided-traffic/cogpit/internal/ui/fyneui"
)

// A development build's COGPIT_LIGHTSTRIP_FPS caps the light strip's frames for a measurement
// (D-new): an integer from 1 to 60; anything else leaves the strip's own cap.
func TestLightStripFPSFromTheEnvironment(t *testing.T) {
	for v, want := range map[string]int{"": 0, "30": 30, " 15 ": 15, "1": 1, "60": 60, "0": 0, "61": 0, "-5": 0, "fast": 0, "29.97": 0} {
		t.Setenv(lightStripFPSVar, v)
		var opts fyneui.Options
		lightStripFPS(&opts, slog.New(slog.DiscardHandler))
		if opts.LightStripFPS != want {
			t.Errorf("%s=%q: cap %d, want %d", lightStripFPSVar, v, opts.LightStripFPS, want)
		}
	}
}
