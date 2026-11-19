//go:build nodemo

// The release build's variant of demo_test.go: no demo cluster (D-67), so a test over it skips.

package fyneui

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// demoConnector skips the test: a release build has no demo cluster.
func demoConnector(t testing.TB, _ demoSize) *engine.ClusterConnector {
	t.Helper()
	t.Skip("the demo cluster is a development build's (D-67)")
	return nil
}

// datedDemo has nothing to date: a release build has no demo cluster.
func datedDemo(*engine.ClusterConnector) {}
