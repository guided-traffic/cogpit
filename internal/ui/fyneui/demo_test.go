//go:build !nodemo

// The demo cluster in the UI's tests: a development build's, as the demo cluster is (D-67);
// demo_nodemo_test.go has the release build's.

package fyneui

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// demoConnector is a connector that offers the demo cluster of that size (newTestEngine dates its
// objects from fixedNow).
func demoConnector(t testing.TB, size demoSize) *engine.ClusterConnector {
	t.Helper()
	c := &engine.ClusterConnector{IncludeDemo: true}
	c.DemoOptions.Pods, c.DemoOptions.Seed, c.DemoOptions.NoChurn = size.Pods, size.Seed, size.NoChurn
	c.DemoOptions.Deployments = size.Deployments
	c.DemoOptions.EventsPerSecond, c.DemoOptions.MetricsResolution = size.EventsPerSecond, size.MetricsResolution
	return c
}

// datedDemo dates the objects of a connector's demo cluster from fixedNow unless the test dated
// them.
func datedDemo(c *engine.ClusterConnector) {
	if c.IncludeDemo && c.DemoOptions.Now.IsZero() {
		c.DemoOptions.Now = fixedNow
	}
}
