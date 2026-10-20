//go:build !darwin

package main

import (
	"log/slog"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// installDeepLinks returns no queue: only the macOS app registers the cogpit URL scheme so far
// (D-47). The Linux package (D-108) registers none yet: there a link starts a second process, which
// needs a hand-over to the running Cogpit that does not exist (G22); Windows packages come with M11.
func installDeepLinks(*slog.Logger) *engine.DeepLinkQueue { return nil }
