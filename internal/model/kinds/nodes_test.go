package kinds_test

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/model"
)

// TestNodeWarnings: a cordoned Ready node gets no warning icon and no warning row (capture,
// 2026-10-11); which nodes do warn is inferred: not Ready, or a problem condition.
func TestNodeWarnings(t *testing.T) {
	checkStatusTexts(t, []statusText{
		{"nodes", "cordoned", "signals", "", model.StatusNone, model.StatusOK},
		{"nodes", "healthy", "signals", "", model.StatusNone, model.StatusOK},
		{"nodes", "failing", "signals", "NotReady, DiskPressure, MemoryPressure", model.StatusWarning, model.StatusError},
		{"nodes", "minimal", "signals", "", model.StatusNone, model.StatusUnknown},
	})
}
