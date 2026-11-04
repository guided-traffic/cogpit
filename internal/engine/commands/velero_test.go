package commands_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestVeleroDashboardCommand: an agent reads the Velero page through velero.dashboard, from the
// cache, declaring reads only; a click on a row of the open page opens its object's drawer beside it
// (ui.select, mode open), whatever its kind.
func TestVeleroDashboardCommand(t *testing.T) {
	src := seed()
	for _, crd := range testkit.VeleroCRDs() {
		src.Set(testkit.CRDsGVR, crd)
	}
	now := time.Now()
	src.Set(testkit.VeleroLocationsGVR, testkit.VeleroObject("BackupStorageLocation", "default", now, nil,
		map[string]any{"default": true}, map[string]any{"phase": "Unavailable", "message": "connection refused"}))
	backup := "nightly-" + now.Add(-time.Hour).UTC().Format("20060102150405")
	src.Set(testkit.VeleroBackupsGVR, testkit.VeleroObject("Backup", backup, now.Add(-time.Hour), map[string]string{"velero.io/schedule-name": "nightly"},
		map[string]any{"includedNamespaces": []any{"shop"}}, map[string]any{"phase": "PartiallyFailed", "errors": int64(1),
			"startTimestamp": testkit.VeleroTime(now.Add(-time.Hour)), "completionTimestamp": testkit.VeleroTime(now.Add(-50 * time.Minute))}))
	src.Set(testkit.VeleroPodVolumeBackupsGVR, testkit.VeleroObject("PodVolumeBackup", backup+"-x1", now, map[string]string{"velero.io/backup-name": backup},
		map[string]any{"pod": map[string]any{"namespace": "shop", "name": "web-0"}, "volume": "data", "node": "node-1"},
		map[string]any{"phase": "Failed", "message": "exposed PVB is not ready: no expose result is available for the current node"}))
	src.Set(testkit.VeleroSchedulesGVR, testkit.VeleroObject("Schedule", "nightly", now.Add(-time.Hour), nil,
		map[string]any{"schedule": "0 2 * * *", "template": map[string]any{"includedNamespaces": []any{"shop"}}},
		map[string]any{"phase": "Enabled", "lastBackup": testkit.VeleroTime(now.Add(-time.Hour))}))
	h := &harness{src: src, writer: &fakeWriter{}}
	h.s = engine.New(engine.Options{Connector: crdConnector{src: src}, SnapshotInterval: 5 * time.Millisecond})
	if err := commands.RegisterAll(h.s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.s.Close)
	if err := h.s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	agent := engine.OriginExternalAgent
	var res engine.VeleroDashboardModel
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		return h.run(t, agent, "velero.dashboard", map[string]any{}, &res) == nil && res.Served && res.Synced && len(res.Attention) > 0
	}) {
		t.Fatalf("velero_dashboard: %+v", res)
	}
	if res.Attention[0].Problem != "Location unavailable" || res.Attention[0].Detail != "connection refused" ||
		len(res.Schedules) != 1 || !strings.HasPrefix(res.Schedules[0].When, "daily 02:00") || len(res.Tiles) != 4 { // " UTC" where the computer's zone is another
		t.Errorf("model %+v", res)
	}
	// The backup's failed volume backup, with its pod, volume and node for an agent.
	if len(res.FailedVolumes) != 1 || len(res.FailedVolumes[0].Members) != 1 || res.FailedVolumes[0].Members[0].Pod != "web-0" ||
		res.FailedVolumes[0].Members[0].Node != "node-1" || res.FailedVolumes[0].Ref.Name != backup+"-x1" {
		t.Errorf("failed volumes %+v", res.FailedVolumes)
	}
	// A read of the cache: it declares list and watch, nothing that writes (D-151).
	c, _ := h.s.Registry().Get("velero.dashboard")
	for _, v := range c.Verbs {
		if v.Verb != "list" && v.Verb != "watch" {
			t.Errorf("velero.dashboard declares %+v", v)
		}
	}

	h.must(t, engine.OriginGUI, "ui.openView", map[string]any{"view": engine.VeleroDashboard}, nil)
	ref := res.Attention[0].Ref
	h.must(t, engine.OriginGUI, "ui.select", map[string]any{"refs": []model.ObjectRef{ref}, "mode": "open"}, nil)
	if u := h.s.UIState(); u.View != engine.VeleroDashboard || u.Drawer == nil || u.Drawer.Kind != ref.Kind || u.Drawer.Name != "default" {
		t.Errorf("ui %s drawer %+v", u.View, u.Drawer)
	}
}
