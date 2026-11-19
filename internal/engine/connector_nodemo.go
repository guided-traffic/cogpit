//go:build nodemo

// A release build: the release pipeline builds with the tag nodemo (the Makefile's DIST_TAGS), so
// neither binary contains the synthetic demo cluster (owner, D-67). The connector offers no context
// "demo" whatever IncludeDemo says, and a kubeconfig context of that name is one like any other
// (connector_demo.go has the development build's).

package engine

// DemoBuilt reports whether this build has the synthetic demo cluster: a release build has not.
const DemoBuilt = false

// DemoOptions size nothing in a build without the demo cluster.
type DemoOptions struct{}

// connectDemo is never reached: demoOffered is false in this build.
func (c *ClusterConnector) connectDemo() (*Backend, error) {
	return nil, errDemoUnavailable
}
