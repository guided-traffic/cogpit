//go:build !nodemo

// These tests count the demo cluster's metrics requests: a development build only (D-67).

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/demo"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The names read and write both ways; "all" is every metric; an unknown name is an error for a
// command and dropped from a file.
func TestMetricSetNames(t *testing.T) {
	m, err := ParseMetricSet([]string{"Pods", " network "})
	if err != nil || m != HidePods|HideNetwork {
		t.Fatalf("parse: %v, %v", m, err)
	}
	if all, err := ParseMetricSet([]string{"all"}); err != nil || all != HideAll {
		t.Fatalf("all: %v, %v", all, err)
	}
	if _, err := ParseMetricSet([]string{"gpu"}); err == nil || !strings.Contains(err.Error(), "cluster, nodes, pods") {
		t.Errorf("an unknown name: %v", err)
	}
	b, err := json.Marshal(HideCluster | HideFilesystem | HideCPU)
	if err != nil || string(b) != `["cluster","cpu","filesystem"]` {
		t.Errorf("json %s, %v", b, err)
	}
	var back MetricSet
	if err := json.Unmarshal([]byte(`["filesystem","gpu","cluster"]`), &back); err != nil || back != HideCluster|HideFilesystem {
		t.Errorf("read back %v, %v", back, err)
	}
	if got := MetricNames(0); got == nil || len(got) != 0 {
		t.Errorf("none: %#v", got)
	}
	if got := (HideCPU | HideNodes).Labels(); !slices.Equal(got, []string{"Nodes", "CPU"}) {
		t.Errorf("labels %q", got)
	}
}

// What each place shows of a set: the lists' columns, the drawer's variants, the Overview's charts.
func TestHiddenMetricsPerPlace(t *testing.T) {
	if cpu, mem := HideCPU.listColumns(model.UsagePods); cpu || !mem {
		t.Errorf("pods without CPU: %v %v", cpu, mem)
	}
	if cpu, mem := HideNodes.listColumns(model.UsageNodes); cpu || mem {
		t.Errorf("hidden nodes: %v %v", cpu, mem)
	}
	if cpu, mem := HideNodes.listColumns(model.UsagePods); !cpu || !mem {
		t.Errorf("hidden nodes hide the pods' columns: %v %v", cpu, mem)
	}
	if got := (HideCPU | HideNetwork).drawerVariants(model.UsagePods); !slices.Equal(got, []string{VariantMemory, VariantFilesystem}) {
		t.Errorf("variants %q", got)
	}
	if got := HidePods.drawerVariants(model.UsagePods); len(got) != 0 {
		t.Errorf("hidden pods' variants %q", got)
	}
	if cpu, mem, pods := HideMemory.overviewShown(); !cpu || mem || !pods {
		t.Errorf("overview without memory: %v %v %v", cpu, mem, pods)
	}
	if cpu, mem, pods := HideCluster.overviewShown(); cpu || mem || pods {
		t.Errorf("hidden cluster: %v %v %v", cpu, mem, pods)
	}
	if got := defaultVariant([]string{VariantNetwork, VariantFilesystem}, metrics.Capabilities{}); got != VariantNetwork {
		t.Errorf("default without CPU and memory and without Prometheus: %s", got)
	}
}

// hiddenSession connects the small demo cluster, whose Metrics Server counts what it is asked, with
// short poll intervals and the cluster settings kept in a temporary folder.
func hiddenSession(t *testing.T) *Session {
	t.Helper()
	s := New(Options{
		Connector: &ClusterConnector{
			IncludeDemo: true,
			DemoOptions: demo.Options{Pods: 60, NoChurn: true, Seed: 3, MetricsResolution: 30 * time.Millisecond},
			MetricsOptions: metrics.Options{
				ListInterval: 100 * time.Millisecond, ObjectInterval: 40 * time.Millisecond, TTL: 30 * time.Millisecond,
			},
		},
		ClusterSettings:  NewClusterSettingsStore(filepath.Join(t.TempDir(), "Cogpit")),
		SnapshotInterval: 5 * time.Millisecond,
	})
	t.Cleanup(s.Close)
	if err := s.Connect(context.Background(), DemoContext); err != nil {
		t.Fatal(err)
	}
	return s
}

func hideMetrics(t *testing.T, s *Session, m MetricSet) {
	t.Helper()
	if _, err := s.ChangeClusterSettings(DemoContext, func(cs *ClusterSettings) error {
		cs.HiddenMetrics = m
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func metricsRequests(t *testing.T, s *Session) int64 {
	t.Helper()
	st, ok := s.Backend().DemoStats().(demo.Stats)
	if !ok {
		t.Fatal("no demo stats")
	}
	return st.MetricsRequests
}

func column(snap *TableSnapshot, id string) model.Column {
	for _, c := range snap.Columns {
		if c.ID == id {
			return c
		}
	}
	return model.Column{}
}

// noRequestsFor fails when the demo's Metrics Server is asked anything for a while.
func noRequestsFor(t *testing.T, s *Session, what string) {
	t.Helper()
	time.Sleep(60 * time.Millisecond) // a poll that started before the change may still answer
	before := metricsRequests(t, s)
	time.Sleep(150 * time.Millisecond) // past a list interval and three object intervals; the hub holds no subscription
	if after := metricsRequests(t, s); after != before {
		t.Errorf("%s: %d metrics requests while every metric it shows is hidden", what, after-before)
	}
}

// Hidden metrics leave the lists' columns, the drawer's charts and the Overview, and nothing is
// polled for them: the hub holds no subscription and the demo's Metrics Server is asked nothing.
// Shown again, the polls come back.
func TestHiddenMetricsAreNotPolled(t *testing.T) {
	s := hiddenSession(t)
	hub := s.Backend().Metrics
	var ref model.ObjectRef // a pod the Metrics Server measures
	waitFor(t, "the Pods list's CPU", func() bool {
		snap := s.Table("pods")
		if snap == nil || !snap.Synced {
			return false
		}
		ci := slices.IndexFunc(snap.Columns, func(c model.Column) bool { return c.ID == model.ColumnCPU })
		i := slices.IndexFunc(snap.Rows, func(r model.Row) bool { return cellAt(&r, ci).Text != "" })
		if i >= 0 {
			ref = snap.Rows[i].Ref
		}
		return i >= 0
	})
	if hub.Subscribed() == 0 {
		t.Fatal("the Pods list polls nothing before anything is hidden")
	}

	// The pods' metrics hidden: the list's columns go, its poll stops.
	hideMetrics(t, s, HidePods)
	waitFor(t, "the list's poll released", func() bool { return hub.Subscribed() == 0 })
	waitFor(t, "the columns hidden", func() bool {
		snap := s.Table("pods")
		return snap != nil && column(snap, model.ColumnCPU).Hidden && column(snap, model.ColumnMemory).Hidden
	})
	if !slices.Equal(s.UIState().ClusterSettings[DemoContext].HiddenMetrics.Labels(), []string{"Pods"}) {
		t.Errorf("ui_state %+v", s.UIState().ClusterSettings)
	}

	// A pod's drawer has no Metrics section and polls nothing.
	s.UpdateUI(func(u *UIState) { u.Drawer = &ref })
	m, err := s.Detail(ref)
	if err != nil || m.Metrics {
		t.Fatalf("detail: metrics %v, %v", m != nil && m.Metrics, err)
	}
	om, err := s.ObjectMetrics(ref)
	if err != nil || om.Status != StatusHidden || len(om.Targets) != 0 {
		t.Fatalf("object metrics %+v, %v", om, err)
	}
	if err := s.SetMetricsVariant(ref, "", VariantMemory); !errors.Is(err, ErrNotApplicable) {
		t.Errorf("a hidden variant set: %v", err)
	}
	if hub.Subscribed() != 0 {
		t.Errorf("%d scopes polled with the list and the drawer hidden", hub.Subscribed())
	}
	noRequestsFor(t, s, "the Pods list and a pod's drawer")

	// Only the CPU, the network and the containers hidden: the drawer polls again, offers the
	// rest, and a chart left at its default shows Memory.
	hideMetrics(t, s, HideCPU|HideNetwork|HideContainers)
	waitFor(t, "the drawer's poll", func() bool { return hub.Subscribed() >= 1 })
	waitFor(t, "the drawer's numbers", func() bool {
		om, err = s.ObjectMetrics(ref)
		return err == nil && om.Status == metrics.StatusAvailable && len(om.Targets) > 0 && om.Targets[0].Current != nil
	})
	if !slices.Equal(om.Variants, []string{VariantMemory, VariantFilesystem}) || om.Targets[0].Variant != VariantMemory || len(om.Targets) != 1 {
		t.Errorf("variants %q, the pod's chart shows %s, %d targets (the containers' are hidden)", om.Variants, om.Targets[0].Variant, len(om.Targets))
	}
	if d, _ := s.Detail(ref); !d.Metrics {
		t.Error("the drawer has no Metrics section with Memory visible")
	}
	// The view rebuilds its snapshot with the new columns on its own goroutine: wait for it rather
	// than read the one before (seen under the race job's load).
	waitFor(t, "the Pods list with only CPU hidden", func() bool {
		snap := s.Table("pods")
		return column(snap, model.ColumnCPU).Hidden && !column(snap, model.ColumnMemory).Hidden
	})

	// The Overview with its CPU and Memory hidden polls nothing for its charts; the Pods chart
	// stays, from the cache. Hiding the cluster's place hides that too.
	s.UpdateUI(func(u *UIState) { u.Drawer, u.View = nil, ClusterOverview })
	hideMetrics(t, s, HideCPU|HideMemory)
	waitFor(t, "nothing polled for the Overview", func() bool { return hub.Subscribed() == 0 })
	var ov *ClusterOverviewModel
	waitFor(t, "the Overview's build", func() bool {
		ov, err = s.ClusterOverview(context.Background(), "", nil, true)
		return err == nil && ov.Synced
	})
	if ov.Status != StatusHidden || !ov.Charts[0].Hidden || !ov.Charts[1].Hidden || ov.Charts[2].Hidden || ov.Charts[0].Current != nil {
		t.Errorf("overview: status %s, charts %+v", ov.Status, ov.Charts)
	}
	noRequestsFor(t, s, "the Overview")

	// Shown again: the Overview subscribes to the cluster's metrics.
	hideMetrics(t, s, 0)
	waitFor(t, "the Overview's poll", func() bool { return hub.Subscribed() == 1 })
	hideMetrics(t, s, HideCluster)
	waitFor(t, "the Overview's poll released", func() bool { return hub.Subscribed() == 0 })
	ov, _ = s.ClusterOverview(context.Background(), "", nil, true)
	if ov.Status != StatusHidden || !ov.Charts[2].Hidden {
		t.Errorf("hidden cluster: status %s, pods chart hidden %v", ov.Status, ov.Charts[2].Hidden)
	}

	// Every metric shown again: the Pods list polls once more and shows its columns.
	hideMetrics(t, s, 0)
	s.UpdateUI(func(u *UIState) { u.View = "pods" })
	waitFor(t, "the list polls again", func() bool {
		snap := s.Table("pods")
		return hub.Subscribed() == 1 && snap != nil && !column(snap, model.ColumnCPU).Hidden
	})
}

// The setting is kept with the cluster's other settings, by name, and normalised when read back.
func TestHiddenMetricsKept(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	s := New(Options{ClusterSettings: NewClusterSettingsStore(dir)})
	if _, err := s.ChangeClusterSettings(devCtx, func(cs *ClusterSettings) error {
		cs.HiddenMetrics = HideNodes | HideFilesystem
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ClusterSettingsFile))
	if err != nil || !strings.Contains(string(raw), `"hiddenMetrics": [`) || !strings.Contains(string(raw), `"filesystem"`) {
		t.Fatalf("file: %s, %v", raw, err)
	}
	if _, err := s.ChangeClusterSettings(devCtx, func(cs *ClusterSettings) error {
		cs.HiddenMetrics = 1 << 15
		return nil
	}); err == nil {
		t.Error("an unknown metric kept")
	}
	again := New(Options{ClusterSettings: NewClusterSettingsStore(dir)})
	if cs, _ := again.ClusterSettingsOf(devCtx); cs.HiddenMetrics != HideNodes|HideFilesystem {
		t.Errorf("read back %v", cs.HiddenMetrics)
	}
	v, err := again.ClusterSettingsView(devCtx)
	if err != nil || !slices.Equal(v.HiddenMetrics, []string{"nodes", "filesystem"}) {
		t.Errorf("view %q, %v", v.HiddenMetrics, err)
	}
	if v, _ := again.ClusterSettingsView("other"); v.HiddenMetrics == nil {
		t.Error("a context without settings reports null, not an empty list")
	}
}
