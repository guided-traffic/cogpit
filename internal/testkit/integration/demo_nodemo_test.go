//go:build nodemo

// The release build's variant of demo_test.go: no demo cluster (D-67), so a test over it skips.

package integration_test

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/demo"
	"github.com/guided-traffic/cogpit/internal/engine"
)

// demoConnector skips the test: a release build has no demo cluster.
func demoConnector(t testing.TB, _ demo.Options) *engine.ClusterConnector {
	t.Helper()
	t.Skip("the demo cluster is a development build's (D-67)")
	return nil
}
