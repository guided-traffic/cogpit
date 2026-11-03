package commands_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// A81: the logs of an Argo CD Application and of a Flux Kustomization in one merged tab.

// commandState says how a command shows for its parameters: enabled, hidden or greyed with why.
func commandState(t *testing.T, s *engine.Session, id string, params any) string {
	t.Helper()
	cmd, ok := s.Registry().Get(id)
	if !ok {
		t.Fatalf("no command %s", id)
	}
	raw, _ := json.Marshal(params)
	switch err := cmd.Enabled(s, raw); {
	case err == nil:
		return "enabled"
	case errors.Is(err, engine.ErrNotApplicable):
		return "hidden: " + err.Error()
	default:
		return "greyed: " + err.Error()
	}
}

// TestArgoCDLogs (A81): Logs of an Argo CD Application opens one tab on the Application, over the
// workloads its status.resources lists; not offered for an Application that deploys to another
// cluster or lists no workload.
func TestArgoCDLogs(t *testing.T) {
	h := newArgoHarness(t)
	resources := []any{
		map[string]any{"group": "apps", "kind": "Deployment", "namespace": "shop", "name": "web"},
		map[string]any{"group": "", "kind": "Service", "namespace": "shop", "name": "web"},
	}
	shop := h.app(t, map[string]any{"status": map[string]any{"resources": resources}}, "shop")
	remote := h.app(t, map[string]any{
		"spec":   map[string]any{"project": "default", "destination": map[string]any{"server": "https://prod.example.com", "namespace": "shop"}},
		"status": map[string]any{"resources": resources},
	}, "remote")
	empty := h.app(t, map[string]any{"status": map[string]any{"resources": resources[1:]}}, "empty")

	cmd, _ := h.s.Registry().Get("argocd.logs")
	if cmd.Label != "Logs" || cmd.Danger != engine.DangerRead || len(cmd.Kinds) != 1 || cmd.Kinds[0] != kinds.ArgoApplicationsKey {
		t.Errorf("argocd.logs: %+v", cmd)
	}
	if got := commandState(t, h.s, "argocd.logs", map[string]any{"ref": shop}); got != "enabled" {
		t.Errorf("shop: %s", got)
	}
	if got := commandState(t, h.s, "argocd.logs", map[string]any{"ref": remote}); got != "greyed: remote deploys to another cluster (https://prod.example.com), whose logs Cogpit does not read through this one" {
		t.Errorf("remote: %s", got)
	}
	if got := commandState(t, h.s, "argocd.logs", map[string]any{"ref": empty}); !strings.HasPrefix(got, "hidden: ") || !strings.Contains(got, "names no workload") {
		t.Errorf("empty: %s", got)
	}
	res, err := h.dispatch(engine.OriginExternalAgent, "argocd.logs", map[string]any{"ref": shop})
	if err != nil {
		t.Fatal(err)
	}
	tab := res.(commands.DockResult).Tab
	if tab.Title != "Logs: Application shop" || tab.Ref.Kind != kinds.ArgoApplicationsKey || tab.Type != engine.DockLogs {
		t.Errorf("tab %+v", tab)
	}
}

// TestFluxLogs (A81): Logs of a Flux Kustomization opens one tab on it, over the workloads its
// status.inventory lists, and activates it when it is open; the tab lists its workloads and narrows
// to one (logs.sources, logs.setSource with workload); greyed for one whose inventory lists none.
func TestFluxLogs(t *testing.T) {
	h, _ := newFluxHarness(t, engine.Options{})
	obj := fluxObject("kustomize.toolkit.fluxcd.io/v1", "Kustomization", "flux-system", "shop", false, "True")
	obj.Object["status"].(map[string]any)["inventory"] = map[string]any{"entries": []any{
		map[string]any{"id": "default_web_apps_Deployment", "v": "v1"},
		map[string]any{"id": "default_web__Service", "v": "v1"},
	}}
	h.src.Set(ksGVR, obj)
	shop := model.ObjectRef{Context: "test", Kind: ksKey, Namespace: "flux-system", Name: "shop"}
	waitUntil(t, "the Kustomization", func() bool { _, err := h.s.Lookup(shop); return err == nil })
	if got := commandState(t, h.s, "flux.logs", map[string]any{"ref": shop}); got != "enabled" {
		t.Errorf("shop: %s", got)
	}
	if got := commandState(t, h.s, "flux.logs", map[string]any{"ref": ksRef("apps")}); !strings.Contains(got, "names no workload") {
		t.Errorf("apps, no inventory: %s", got)
	}
	if got := commandState(t, h.s, "flux.logs", map[string]any{"ref": hrRef()}); !strings.HasPrefix(got, "hidden: ") {
		t.Errorf("a Helm release: %s", got)
	}
	var res commands.DockResult
	h.must(t, engine.OriginGUI, "flux.logs", map[string]any{"ref": shop}, &res)
	if res.Tab.Title != "Logs: Kustomization shop" || !res.Created {
		t.Errorf("tab %+v", res)
	}
	var again commands.DockResult
	h.must(t, engine.OriginExternalAgent, "flux.logs", map[string]any{"ref": shop}, &again)
	if again.Created || again.Tab.ID != res.Tab.ID || len(h.s.UIState().Dock) != 1 {
		t.Errorf("a second Logs %+v, dock %d", again, len(h.s.UIState().Dock))
	}
	var src engine.LogSources
	h.must(t, engine.OriginExternalAgent, "logs.sources", map[string]any{"id": res.Tab.ID}, &src)
	if strings.Join(src.Workloads, ",") != "web" {
		t.Errorf("sources %+v", src)
	}
	var narrowed engine.DockTab
	h.must(t, engine.OriginExternalAgent, "logs.setSource", map[string]any{"id": res.Tab.ID, "workload": "web"}, &narrowed)
	if narrowed.Logs.Workload != "web" {
		t.Errorf("narrowed %+v", narrowed.Logs)
	}
}
