//go:build !nodemo

package main

import (
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/guided-traffic/cogpit/internal/ui/fyneui"
)

// lightStripFPSVar is a development build's frame cap of the light strip's animations (D-new), for
// measuring their load at 60, 30 and 15 frames a second without a rebuild. The release pipeline
// builds with the tag nodemo, and a release has no such variable (lightstripfps_nodemo.go, D-67).
const lightStripFPSVar = "COGPIT_LIGHTSTRIP_FPS"

// lightStripFPS sets the frame cap from COGPIT_LIGHTSTRIP_FPS, an integer from 1 to 60; anything
// else is ignored with a warning, and without it the strip runs at its own cap.
func lightStripFPS(opts *fyneui.Options, logger *slog.Logger) {
	v := strings.TrimSpace(os.Getenv(lightStripFPSVar))
	if v == "" {
		return
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 60 {
		logger.Warn(lightStripFPSVar+" ignored: an integer from 1 to 60", "value", v)
		return
	}
	opts.LightStripFPS = n
}
