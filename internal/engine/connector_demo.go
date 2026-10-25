//go:build !nodemo

// The synthetic demo cluster, in a development build only: the release pipeline builds with the
// tag nodemo (the Makefile's DIST_TAGS), which leaves this file and so package demo out of both
// binaries (connector_nodemo.go, D-67).

package engine

import (
	"context"
	"fmt"
	"runtime/debug"
	"slices"
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/demo"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// DemoBuilt reports whether this build has the synthetic demo cluster: a development build has it,
// a release build (the tag nodemo, D-67) has not.
const DemoBuilt = true

// DemoOptions size the demo cluster (ClusterConnector.DemoOptions).
type DemoOptions = demo.Options

// demoProfiles keeps the built-in profiles of the kinds the demo cluster serves, as discovery does
// for a real cluster: a kind the demo has no objects for is not offered, rather than shown as an
// error. Helm's releases and the Applications follow as for a real cluster (Connect): the demo's
// Secrets and ConfigMaps are Helm's release records, and each call makes a releases profile with
// its own memo, one per backend.
func demoProfiles() []*model.KindProfile {
	served := map[schema.GroupVersionResource]bool{}
	for _, gvr := range demo.Resources() {
		served[gvr] = true
	}
	profiles := servedProfiles(kinds.Builtin(), served)
	if slices.ContainsFunc(profiles, isSecrets) {
		profiles = append(slices.Clip(profiles), helm.NewReleaseProfile())
	}
	return withApplications(profiles)
}

// connectDemo builds the synthetic cluster and starts its churn.
func (c *ClusterConnector) connectDemo() (*Backend, error) {
	dc := demo.New(c.DemoOptions)
	src := dc.Source()
	if src == nil {
		return nil, errDemoUnavailable
	}
	ch := cache.New(src, cache.Options{WarmPeriod: c.WarmPeriod, Logger: c.Logger})
	churn, cancel := context.WithCancel(context.Background())
	log := c.logger()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error("engine: demo churn panicked", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			}
		}()
		dc.Start(churn)
	}()
	var once sync.Once
	return &Backend{
		Context: DemoContext, Namespace: "default", Cache: ch, Profiles: demoProfiles(), Demo: true,
		Metrics:   metrics.NewHub(metrics.Fixed(dc.Metrics()), c.metricsOptions()),
		DemoStats: func() any { return dc.Stats() },
		Version:   demo.ServerVersion, // its kube-apiserver's (D-91)
		Close: func() {
			once.Do(func() {
				cancel()
				dc.Stop()
				ch.Close()
			})
		},
	}, nil
}
