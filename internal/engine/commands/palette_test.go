package commands_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// paletteViews are the navigator's views of the fixture cluster, as the window passes them.
var paletteViews = []engine.PaletteView{
	{View: "nodes", Title: "Nodes"},
	{View: engine.WorkloadsOverview, Title: "Overview", Group: "Workloads"},
	{View: "pods", Title: "Pods", Group: "Workloads"},
	{View: "deployments", Title: "Deployments", Group: "Workloads"},
	{View: "services", Title: "Services", Group: "Network"},
	{View: "namespaces", Title: "Namespaces"},
	{View: "events", Title: "Events"},
}

// paletteRuns are the commands the window runs its own way (fyneui/palette.go), as far as these
// tests need them.
func paletteRuns(ids ...string) func(string) bool {
	return func(id string) bool { return slices.Contains(ids, id) }
}

func paletteOf(h *harness, runs func(string) bool) []engine.PaletteEntry {
	return h.s.Registry().Palette(h.s, engine.PaletteInput{State: h.s.UIState(), Views: paletteViews, Runs: runs})
}

func entryOf(entries []engine.PaletteEntry, id string) (engine.PaletteEntry, bool) {
	i := slices.IndexFunc(entries, func(e engine.PaletteEntry) bool { return e.Command == id })
	if i < 0 {
		return engine.PaletteEntry{}, false
	}
	return entries[i], true
}

func labelsOf(entries []engine.PaletteEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Label)
	}
	return out
}

// The palette lists what the menus offer in the current state, by the registry's metadata
// (engine/palette.go): the drawer object's actions first, no tool meant for agents, the reads only
// where the window presents them, Switch to every view but the open one, the hotbar's clusters
// with their keys, and the commands that need parameters only where the window asks for them.
func TestPaletteListsWhatTheMenusOffer(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{Hotbar: engine.NewHotbar(t.TempDir())})
	gui := engine.OriginGUI
	h.must(t, gui, "hotbar.addCluster", map[string]any{}, nil)
	pod := model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}
	if _, err := h.s.Objects(context.Background(), "pods", nil); err != nil { // warm: Enabled does not wait
		t.Fatal(err)
	}
	h.must(t, gui, "ui.navigate", map[string]any{"ref": pod}, nil)

	entries := paletteOf(h, nil)
	// The pod's actions, as its drawer's toolbar lists them (Copy first, Edit and Delete last).
	var actions []string
	for _, e := range entries {
		if e.Ref != nil {
			if !e.Ref.Same(pod) || e.Category != "web-0" {
				t.Errorf("%s acts on %v, category %q", e.Command, e.Ref, e.Category)
			}
			actions = append(actions, e.Command)
		}
	}
	want := []string{"object.copyName", "pod.attach", "pod.shell", "pod.debug", "pod.evict", "pod.logs", "object.edit", "object.delete"}
	if !slices.Equal(actions, want) || !slices.Equal(actions, commandsOf(entries[:len(want)])) {
		t.Errorf("object actions %v, want %v first", actions, want)
	}
	for _, id := range []string{
		"ui.state", "kinds.list", "resources.list", "resources.find", "editor.get", "object.get", "metrics.get", // agents' tools
		"ui.palette.open", "ui.palette.close", // the palette's own
		"hotbar.list", "workloads.overview", "table.exportCsv", "cluster.refreshAccess", "kubeconfig.sources", // reads nobody presents
		"pod.portForward", "service.portForward", "ui.navigate", // an object without an Order
		"hotbar.moveCluster", "object.apply", "object.create", "cluster.connect", // parameters without a dialog
		"namespace.create", // the Namespaces view is not open
	} {
		if e, ok := entryOf(entries, id); ok {
			t.Errorf("%s offered: %+v", id, e)
		}
	}

	// Switch to: every view but the open one, in the navigator's order, as ui.openView.
	var views []string
	for _, e := range entries {
		if e.Command == "ui.openView" {
			views = append(views, e.Label+" ["+e.Category+"] "+string(e.Params))
		}
	}
	wantViews := []string{
		`Switch to Nodes [View] {"view":"nodes"}`, `Switch to Overview [Workloads] {"view":"workloads/overview"}`,
		`Switch to Deployments [Workloads] {"view":"deployments"}`, `Switch to Services [Network] {"view":"services"}`,
		`Switch to Namespaces [View] {"view":"namespaces"}`, `Switch to Events [View] {"view":"events"}`,
	}
	if !slices.Equal(views, wantViews) {
		t.Errorf("Switch to entries:\n%s\nwant\n%s", strings.Join(views, "\n"), strings.Join(wantViews, "\n"))
	}

	// The hotbar: its cluster with its F-key, Remove from Hotbar under Hotbar; Add to Hotbar left
	// out for a cluster pinned already (it does not apply). One hotbar, no banks (D-83).
	open, _ := entryOf(entries, "hotbar.openCluster")
	if open.Label != "Open test" || open.Shortcut != "F1" || string(open.Params) != `{"slot":1}` {
		t.Errorf("hotbar tile entry %+v", open)
	}
	for id, label := range map[string]string{"hotbar.removeCluster": "Remove from Hotbar"} {
		if e, ok := entryOf(entries, id); !ok || e.Label != label || e.Category != "Hotbar" || e.Reason != "" || string(e.Params) != "{}" {
			t.Errorf("%s: %+v", id, e)
		}
	}
	if add, ok := entryOf(entries, "hotbar.addCluster"); ok {
		t.Errorf("Add to Hotbar on a pinned cluster: %+v", add)
	}
	for id, shortcut := range map[string]string{"terminal.open": "Cmd+Shift+T", "ui.setFilter": "Cmd+F"} {
		if e, ok := entryOf(entries, id); !ok || e.Shortcut != shortcut || e.Category != "View" {
			t.Errorf("%s: %+v", id, e)
		}
	}

	// Where the window asks for the parameters or presents the result, the command is offered and
	// marked as the window's own.
	runs := paletteRuns("cluster.connect", "table.exportCsv", "namespace.create")
	entries = paletteOf(h, runs)
	for _, id := range []string{"cluster.connect", "table.exportCsv"} {
		if e, ok := entryOf(entries, id); !ok || !e.Own || e.Reason != "" {
			t.Errorf("%s with the window's dialog: %+v", id, e)
		}
	}
	if c, _ := entryOf(entries, "cluster.connect"); c.Shortcut != "Cmd+P" || c.Category != "Cluster" || c.Label != "Connect to Cluster" {
		t.Errorf("cluster.connect %+v", c)
	}

	// A kind's create command comes with its view; without an object there are no actions.
	h.must(t, gui, "ui.openView", map[string]any{"view": "namespaces"}, nil)
	entries = paletteOf(h, runs)
	if e, ok := entryOf(entries, "namespace.create"); !ok || e.Category != "Namespace" || entries[0].Command != "namespace.create" {
		t.Errorf("Create Namespace on the Namespaces view: %+v (first %s)", e, entries[0].Command)
	}
	if slices.ContainsFunc(entries, func(e engine.PaletteEntry) bool { return e.Ref != nil }) {
		t.Errorf("object actions without an object: %v", labelsOf(entries))
	}
	if e, ok := entryOf(entries, "ui.openView"); !ok || e.Label != "Switch to Nodes" {
		t.Errorf("first Switch to %+v", e)
	}
}

func commandsOf(entries []engine.PaletteEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Command)
	}
	return out
}

// An entry runs as a menu item does: the GUI dispatches its command and parameters, and a
// destructive one asks for confirmation naming the object, its namespace and the context.
func TestPaletteRunsLikeAMenu(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	pod := model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}
	if _, err := h.s.Objects(context.Background(), "pods", nil); err != nil {
		t.Fatal(err)
	}
	h.must(t, engine.OriginGUI, "ui.navigate", map[string]any{"ref": pod}, nil)
	entries := engine.FilterPalette(paletteOf(h, nil), "delete")
	if len(entries) == 0 || entries[0].Command != "object.delete" || entries[0].Reason != "" {
		t.Fatalf("filtered for delete: %+v", entries)
	}
	conf := &confirmations{}
	h.s.SetConfirmer(conf)
	run := func(e engine.PaletteEntry) error {
		_, err := h.s.Dispatch(context.Background(), engine.Invocation{ID: e.Command, Params: e.Params, Origin: engine.OriginGUI})
		return err
	}
	if err := run(entries[0]); !errors.Is(err, engine.ErrDeclined) {
		t.Fatalf("declined: %v", err)
	}
	req := conf.last(t)
	if req.Command != "object.delete" || req.Context != "test" || !slices.Equal(req.Namespaces, []string{"shop"}) ||
		len(req.Objects) != 1 || req.Objects[0].Name != "web-0" || req.Danger != engine.DangerDestructive {
		t.Errorf("confirmation %+v", req)
	}
	if calls := h.writer.Calls(); len(calls) != 0 {
		t.Fatalf("a declined delete ran: %v", calls)
	}
	conf.answer = true
	if err := run(entries[0]); err != nil {
		t.Fatal(err)
	}
	if calls := h.writer.Calls(); !slices.Equal(calls, []string{"delete shop/web-0"}) {
		t.Errorf("writes %v", calls)
	}

	// A view switch runs at once, no confirmation.
	asked := len(conf.all())
	sw := engine.FilterPalette(paletteOf(h, nil), "swdep")
	if len(sw) == 0 || sw[0].Label != "Switch to Deployments" {
		t.Fatalf("fuzzy swdep: %v", labelsOf(sw))
	}
	if err := run(sw[0]); err != nil {
		t.Fatal(err)
	}
	if v := h.s.UIState().View; v != "deployments" || len(conf.all()) != asked {
		t.Errorf("after Switch to Deployments: view %s, %d confirmations", v, len(conf.all())-asked)
	}
}

// The palette's open state is UI state: the GUI's ⌘⇧P and Escape and an agent change it the same
// way, and ui_state reports it.
func TestPaletteOpenState(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	agent := engine.OriginExternalAgent
	var st map[string]any
	h.must(t, agent, "ui.palette.open", struct{}{}, nil)
	h.must(t, agent, "ui.state", struct{}{}, &st)
	if st["palette"] != true {
		t.Errorf("ui.state after opening: palette %v", st["palette"])
	}
	h.must(t, engine.OriginGUI, "ui.palette.close", struct{}{}, nil)
	st = nil
	h.must(t, agent, "ui.state", struct{}{}, &st)
	if _, ok := st["palette"]; ok || h.s.UIState().Palette {
		t.Errorf("ui.state after closing: %v", st["palette"])
	}
	c, _ := h.s.Registry().Get("ui.palette.open")
	if c.Shortcut != "Cmd+Shift+P" || c.MenuPath != "View/Command Palette" {
		t.Errorf("ui.palette.open: shortcut %q, menu path %q", c.Shortcut, c.MenuPath)
	}
	raw, _ := json.Marshal(engine.UIState{})
	if strings.Contains(string(raw), "palette") {
		t.Errorf("a closed palette is in the state: %s", raw)
	}
}

// Every cluster and kubeconfig command is under the menu path Cluster/, the window's Cluster menu
// that holds them, so the palette files each under "Cluster" (N121, the owner's decision in the
// audit of 2026-10-16): no command names File/, a menu Cogpit's window does not have.
func TestClusterCommandsUnderTheClusterMenu(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	for _, c := range h.s.Registry().Commands() {
		if first, _, _ := strings.Cut(c.MenuPath, "/"); first == "File" {
			t.Errorf("%s under %q: the window has no File menu", c.ID, c.MenuPath)
		}
	}
	ids := []string{"cluster.connect", "cluster.disconnect", "cluster.reconnect", "cluster.login", "cluster.remove",
		"kubeconfig.sources", "kubeconfig.addFile", "kubeconfig.addFolder", "kubeconfig.remove",
		"cluster.refreshAccess", "cluster.settings.open"}
	for _, id := range ids {
		if c, ok := h.s.Registry().Get(id); !ok || !strings.HasPrefix(c.MenuPath, "Cluster/") {
			t.Errorf("%s: menu path %q, want Cluster/…", id, c.MenuPath)
		}
	}
	// The window runs the kubeconfig commands and Connect its own way (fyneui/palette.go).
	entries := paletteOf(h, paletteRuns("cluster.connect", "cluster.refreshAccess", "kubeconfig.addFile",
		"kubeconfig.addFolder", "kubeconfig.remove", "kubeconfig.sources"))
	var listed []string
	for _, e := range entries {
		if slices.Contains(ids, e.Command) {
			listed = append(listed, e.Command)
			if e.Category != "Cluster" {
				t.Errorf("%s in the palette under %q, want Cluster", e.Command, e.Category)
			}
		}
	}
	for _, id := range []string{"cluster.connect", "cluster.disconnect", "kubeconfig.addFile", "kubeconfig.sources"} {
		if !slices.Contains(listed, id) {
			t.Errorf("%s not in the palette; listed %v", id, listed)
		}
	}
}
