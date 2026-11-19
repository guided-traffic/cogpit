//go:build !nodemo

package main

import (
	"flag"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/ui/fyneui"
)

// demoFlags are the synthetic demo cluster's flags, in a development build only: the release
// pipeline builds with the tag nodemo, and a release has neither the demo cluster nor these flags
// (nodemo.go, D-67).
type demoFlags struct {
	on     *bool
	pods   *int
	events *float64
}

// registerDemoFlags defines --demo, --demo-pods and --demo-events on the command line.
func registerDemoFlags() demoFlags {
	return demoFlags{
		on:     flag.Bool("demo", false, "connect to the synthetic demo cluster (10,000 pods, 1,000 watch events per second)"),
		pods:   flag.Int("demo-pods", 10000, "pods in the demo cluster"),
		events: flag.Float64("demo-events", 1000, "watch events per second in the demo cluster; 0 for a static cluster"),
	}
}

// offer offers the demo cluster as the context "demo", sized by the flags.
func (f demoFlags) offer(c *engine.ClusterConnector) {
	c.IncludeDemo = true
	c.DemoOptions = demoOptions(*f.pods, *f.events)
}

// demoOptions sizes the demo cluster: --demo-events 0 is a static cluster (NoChurn), where the
// options' zero means the default rate.
func demoOptions(pods int, events float64) engine.DemoOptions {
	return engine.DemoOptions{Pods: pods, EventsPerSecond: events, NoChurn: events == 0}
}

// autoConnect connects the demo cluster at start with --demo, over --context.
func (f demoFlags) autoConnect(opts *fyneui.Options) {
	if *f.on {
		opts.AutoConnect = engine.DemoContext
	}
}
