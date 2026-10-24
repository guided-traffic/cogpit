//go:build nodemo

package main

import (
	"log/slog"

	"github.com/guided-traffic/cogpit/internal/ui/fyneui"
)

// lightStripFPS: a release build (the tag nodemo) has no COGPIT_LIGHTSTRIP_FPS; the light strip
// runs at its own frame cap (lightstripfps.go has the development build's).
func lightStripFPS(*fyneui.Options, *slog.Logger) {}
