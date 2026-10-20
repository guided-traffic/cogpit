package fyneui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// TestRegistryMenusAsLens checks the row menus and drawer toolbars the real registry yields
// against Lens's captured menus (2026-10-11), Show Details aside: Node: Shell, Cordon or Uncordon,
// Drain, Edit, Delete; Cron Job: Trigger, Suspend or Resume, Edit, Delete; Pod: Attach Pod, Shell,
// Evict, Share, Logs, Edit, Delete; Deployment: Scale, Restart, Logs. A pair such as Cordon and
// Uncordon are both listed here; the menu shows the one that applies (TestRowMenuNodes). Commands
// Cogpit has not built are named in missing, so that the test shows the gap instead of hiding it.
// Remove Finalizers is Cogpit's own (D-98), between Edit and Delete of every kind, and shown only
// for an object being deleted that finalizers hold. Debug is Cogpit's own too (D-102), after Shell,
// in a dialog of the window's own that is its confirmation (debugdialog.go).
func TestRegistryMenusAsLens(t *testing.T) {
	const rf = engine.RemoveFinalizersCommand
	s := engine.New(engine.Options{})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		kind    string
		want    []string
		missing string
	}{
		{"nodes", []string{"node.shell", "node.cordon", "node.uncordon", "node.drain", "object.edit", rf, "object.delete"}, ""},
		{"cronjobs", []string{"cronjob.trigger", "cronjob.resume", "cronjob.suspend", "object.edit", rf, "object.delete"}, ""},
		{"pods", []string{"pod.attach", "pod.shell", engine.DebugCommand, "pod.evict", "pod.logs", "object.edit", rf, "object.delete"}, "pod.share is not a target"},
		{"deployments", []string{"deployment.scale", "deployment.restart", "workload.logs", "object.edit", rf, "object.delete"}, ""},
		{"statefulsets", []string{"statefulset.restart", "statefulset.scale", "workload.logs", "object.edit", rf, "object.delete"}, ""},
		{"daemonsets", []string{"daemonset.restart", "workload.logs", "object.edit", rf, "object.delete"}, ""},
		{"replicasets", []string{"replicaset.scale", "workload.logs", "object.edit", rf, "object.delete"}, ""},
		{"jobs", []string{"workload.logs", "object.edit", rf, "object.delete"}, ""},
	} {
		var got []string
		for _, c := range s.Registry().Actions(tt.kind) {
			if !c.DrawerOnly { // Copy, first in every drawer (TestCopyIsFirstInEveryDrawer)
				got = append(got, c.ID)
			}
		}
		if strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Errorf("%s: actions %v, want %v (not built: %s)", tt.kind, got, tt.want, tt.missing)
		}
	}
	// What each real command gets from the menus: Scale a dialog for its replicas, Drain one for its
	// options (both have a Prefill), Logs and Shell a submenu of the containers, the others nothing.
	for id, want := range map[string]string{
		"deployment.scale": "dialog:replicas", "statefulset.scale": "dialog:replicas", "replicaset.scale": "dialog:replicas",
		"daemonset.restart": "", "statefulset.restart": "", "workload.logs": "",
		"pod.logs": "containers", "pod.shell": "containers", "pod.attach": "containers",
		engine.DebugCommand: "dialog:image,target,uid",
		"node.drain":        "dialog:ignoreDaemonSets,deleteEmptyDirData,force,gracePeriodSeconds,timeoutSeconds",
		"pod.evict":         "", "node.cordon": "", "cronjob.trigger": "", "object.edit": "", "object.delete": "", engine.RemoveFinalizersCommand: "",
	} {
		c, ok := s.Registry().Get(id)
		if !ok {
			t.Errorf("no command %s", id)
			continue
		}
		got := ""
		switch {
		case len(c.DialogFields()) > 0:
			got = "dialog:" + strings.Join(c.DialogFields(), ",")
		case c.TakesContainer():
			got = "containers"
		}
		if got != want {
			t.Errorf("%s: %q, want %q", id, got, want)
		}
		// Every dialog field is one the generated form can show.
		test.NewTempApp(t)
		for _, name := range c.DialogFields() {
			if _, err := formField(c, name, nil, newTooltip(nil)); err != nil {
				t.Errorf("%s: %v", id, err)
			}
		}
	}
}
