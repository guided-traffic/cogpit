//go:build !nodemo

// The demo cluster in the integration tests: a development build's, as the demo cluster is (D-67);
// demo_nodemo_test.go has the release build's.

package integration_test

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/demo"
	"github.com/guided-traffic/cogpit/internal/engine"
)

// demoConnector is a connector that offers the demo cluster sized by o.
func demoConnector(t testing.TB, o demo.Options) *engine.ClusterConnector {
	t.Helper()
	return &engine.ClusterConnector{IncludeDemo: true, DemoOptions: o}
}
