package fyneui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
)

// auditEngine is a real engine over the settings kubeconfig whose audit log is a file holding a
// few entries of earlier runs: an external agent's Scale that ran, its Delete the user declined, a
// built-in agent's Restart still requested, and the user's switch of the MCP server.
func auditEngine(t *testing.T) (*engine.Session, string) {
	t.Helper()
	s0, _ := settingsEngine(t) // for its kubeconfig file's contexts
	s0.Close()
	path := filepath.Join(t.TempDir(), "audit.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	at := fixedNow.Add(-10 * time.Minute)
	web := model.ObjectRef{Context: "kind-dev", Kind: "deployments", Namespace: "shop", Name: "web"}
	for _, e := range []engine.AuditEntry{
		{Time: at, Origin: engine.OriginGUI, Command: "mcp.switch", Params: json.RawMessage(`{"enabled":true}`), Phase: "done", OK: true},
		{Time: at.Add(time.Minute), Origin: engine.OriginExternalAgent, Command: "deployment.scale", Context: "kind-dev",
			Params: mustJSON(t, map[string]any{"ref": web, "replicas": 3}), Phase: "requested", OK: true},
		{Time: at.Add(time.Minute + time.Second), Origin: engine.OriginExternalAgent, Command: "deployment.scale", Context: "kind-dev",
			Params: mustJSON(t, map[string]any{"ref": web, "replicas": 3}), Phase: "done", OK: true},
		{Time: at.Add(2 * time.Minute), Origin: engine.OriginExternalAgent, Command: "object.delete", Context: "kind-dev",
			Params: mustJSON(t, map[string]any{"ref": model.ObjectRef{Context: "kind-dev", Kind: "pods", Namespace: "shop", Name: "web-7d9f-x2x4z"}}),
			Phase:  "done", Error: "command object.delete: the user declined the command"},
		{Time: at.Add(3 * time.Minute), Origin: engine.OriginBuiltInAgent, Command: "deployment.restart", Context: "kind-dev",
			Params: mustJSON(t, map[string]any{"ref": web}), Phase: "requested", OK: true},
	} {
		line, _ := json.Marshal(e)
		_, _ = f.Write(append(line, '\n'))
	}
	kc := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(kc, []byte(settingsKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newTestEngine(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kc}}, Audit: f, AuditPath: path,
		ClusterSettings: engine.NewClusterSettingsStore(filepath.Join(t.TempDir(), "Cogpit"))})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, path
}

// auditTabOf is the dock's Audit Log, once open.
func auditTabOf(sh *shell) *auditLogTab {
	for _, c := range sh.dock.contents {
		if a, ok := c.editor.(*auditLogTab); ok {
			return a
		}
	}
	return nil
}

// auditRows are the list's rows as drawn: each row's cells.
func auditRows(a *auditLogTab) [][]string {
	var out [][]string
	for i := range a.entries {
		row := a.newRow()
		a.updateRow(i, row)
		var cells []string
		for _, c := range row.(*fyne.Container).Objects[0].(*fyne.Container).Objects {
			cells = append(cells, c.(*textLine).text)
		}
		out = append(out, cells)
	}
	return out
}

// Help › Audit Log opens a read-only dock tab listing the audit log newest first (time, origin,
// command, context, objects, outcome); its origin filter is the tab's engine state; an agent's new
// command shows at the top without a click; the palette offers it through the registry.
func TestAuditLogTab(t *testing.T) {
	s, _ := auditEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	sh.zone = time.UTC
	item := sh.auditMenuItem()
	if item == nil || item.Label != "Audit Log" {
		t.Fatalf("Help › Audit Log: %+v", item)
	}
	if help := sh.helpMenu(); !slices.ContainsFunc(help.Items, func(i *fyne.MenuItem) bool { return i.Label == "Audit Log" }) {
		t.Error("the Help menu has no Audit Log")
	}
	item.Action()
	q.until(t, "the Audit Log", func() bool { a := auditTabOf(sh); return a != nil && len(a.entries) == 5 })
	a := auditTabOf(sh)
	rows := auditRows(a)
	want := [][]string{ // from today (the shell's clock): the time alone
		{"14:23:00", "Built-in agent", "deployment.restart", "kind-dev", "deployments shop/web", "requested"},
		{"14:22:00", "External agent", "object.delete", "kind-dev", "pods shop/web-7d9f-x2x4z", "command object.delete: the user declined the command"},
		{"14:21:01", "External agent", "deployment.scale", "kind-dev", "deployments shop/web", "ok"},
		{"14:21:00", "External agent", "deployment.scale", "kind-dev", "deployments shop/web", "requested"},
		{"14:20:00", "You (window)", "mcp.switch", "", "", "ok"},
	}
	for i := range want {
		if i >= len(rows) || !slices.Equal(rows[i], want[i]) {
			t.Fatalf("rows:\n%q\nwant:\n%q", rows, want)
		}
	}
	if a.summary.text != "5 entries, newest first" {
		t.Errorf("summary %q", a.summary.text)
	}

	// The filter: the tab's state in the engine, read again.
	a.origin.SetSelected("Agents")
	q.until(t, "agents only", func() bool {
		u := sh.ui.Dock
		return len(u) == 1 && u[0].Audit.Origin == engine.AuditAgents && len(a.entries) == 4 && a.loaded == engine.AuditAgents
	})

	// An agent's command appears without a click.
	raw, _ := json.Marshal(map[string]any{"context": "staging"})
	if _, err := s.Dispatch(context.Background(), engine.Invocation{ID: "cluster.settings.get", Params: raw, Origin: engine.OriginExternalAgent}); err != nil {
		t.Fatal(err)
	}
	q.until(t, "the new entry on top", func() bool { return len(a.entries) == 5 && a.entries[0].Command == "cluster.settings.get" })

	// Behind another dock tab the log is not read; shown again, it is.
	mustDispatch(t, s, "diagnostics.open", struct{}{})
	q.until(t, "the Diagnostics tab in front", func() bool { return diagnosticsTabOf(sh) != nil && !a.shown() })
	seq := a.seq
	if _, err := s.Dispatch(context.Background(), engine.Invocation{ID: "cluster.settings.get", Params: raw, Origin: engine.OriginExternalAgent}); err != nil {
		t.Fatal(err)
	}
	q.until(t, "the log marked stale", func() bool { return a.stale })
	if a.seq != seq {
		t.Error("the hidden Audit Log read the file")
	}
	mustDispatch(t, s, "dock.activate", map[string]any{"id": a.tab.ID})
	q.until(t, "the log read when shown", func() bool { return a.shown() && !a.stale && len(a.entries) == 6 })

	// The palette offers it, from the registry.
	entries := s.Registry().Palette(s, engine.PaletteInput{State: s.UIState()})
	if !slices.ContainsFunc(entries, func(e engine.PaletteEntry) bool { return e.Command == "audit.open" && e.Category == "Help" }) {
		t.Error("the palette does not offer the Audit Log")
	}
}

// The Audit Log, dark and light: the PNGs of the pull request (AGENTS.md rule 7).
func TestRenderAuditLogDark(t *testing.T)  { renderAuditLog(t, ThemeDark, "auditlog_dark.png") }
func TestRenderAuditLogLight(t *testing.T) { renderAuditLog(t, ThemeLight, "auditlog_light.png") }

func renderAuditLog(t *testing.T, mode ThemeMode, name string) {
	s, _ := auditEngine(t)
	sh, w, q := newEngineShell(t, s, Options{Theme: mode})
	sh.zone = time.UTC
	mustDispatch(t, s, "audit.open", struct{}{})
	q.until(t, "the Audit Log", func() bool { a := auditTabOf(sh); return a != nil && len(a.entries) == 5 && sh.dock.open() })
	var list *widget.List
	if a := auditTabOf(sh); a != nil {
		list = a.list
	}
	if list == nil {
		t.Fatal("no list")
	}
	q.drain()
	savePNG(t, w, name)
}
