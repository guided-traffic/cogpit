//go:build nodemo

package main

import (
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/ui/fyneui"
)

// demoFlags: a release build (the tag nodemo) has no demo cluster, so there is no --demo,
// --demo-pods or --demo-events, and the welcome page offers no context "demo" (owner, D-67; demo.go
// has the development build's).
type demoFlags struct{}

func registerDemoFlags() demoFlags { return demoFlags{} }

func (demoFlags) offer(*engine.ClusterConnector) {}

func (demoFlags) autoConnect(*fyneui.Options) {}
