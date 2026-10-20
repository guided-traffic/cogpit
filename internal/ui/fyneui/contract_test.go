package fyneui

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
)

// TestDispatchedParamsMatchTheRegistry checks the shell's command parameters against the real
// registry: the engine decodes parameters strictly (unknown fields are refused), so a field the
// shell sends that a command does not declare would turn a click into an error. The shell's own
// tests run against a fake engine and cannot see that.
func TestDispatchedParamsMatchTheRegistry(t *testing.T) {
	s := engine.New(engine.Options{})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	ref := model.ObjectRef{Context: "demo", Kind: "pods", Namespace: "default", Name: "web-0"}
	cases := []struct {
		id     string
		params any
	}{
		{"cluster.connect", contextParams{Context: "demo"}},
		{"kinds.list", struct{}{}},
		{"resources.list", listParams{Kind: kindNamespaces, Limit: nsPageSize, Offset: nsPageSize}}, // the filter's pages (nsgone.go)
		{"ui.openView", openViewParams{View: "pods"}},
		{"ui.setNamespace", namespacesParams{Namespaces: []string{}}},
		{"ui.setFilter", filterParams{Filter: "web"}},
		{"ui.setFilter", filterParams{Filter: "^web-", MatchCase: new(bool), Regex: &[]bool{true}[0]}},
		{"ui.sort", sortParams{Column: "name", Desc: true}},
		{"ui.sortDrawerTable", drawerSortParams{Section: "Pods", Column: "memory", Desc: true}},
		{"ui.select", selectParams{Refs: []model.ObjectRef{ref}}},
		{"ui.select", selectModeParams{Refs: []model.ObjectRef{ref}, Mode: "open"}}, // a plain click on a row (A73)
		{"ui.closeDrawer", struct{}{}},
		{"ui.navigate", refParams{Ref: ref}},
		{"dock.activate", dockParams{ID: "logs-1"}},
		{"dock.close", dockParams{ID: "logs-1"}},
		{"terminal.open", struct{}{}},
		{"pod.logs", containerParams{Ref: ref}},
		{"pod.shell", containerParams{Ref: ref, Container: "app"}},
		{"deployment.restart", refParams{Ref: ref}},
		{"object.delete", refParams{Ref: ref}},
	}
	cases = append(cases, helmDispatches()...)
	cases = append(cases, helmInstallDispatches()...)
	// The row menus and the drawer's toolbar send ObjectParams for every action of a kind.
	for _, kind := range []string{kindPods, "deployments", "services", "nodes", "namespaces", "events"} {
		for _, cmd := range s.Registry().Actions(kind) {
			r := ref
			r.Kind = kind
			cases = append(cases, struct {
				id     string
				params any
			}{cmd.ID, cmd.ObjectParams(r, "app")})
		}
	}
	for _, c := range cases {
		cmd, ok := s.Registry().Get(c.id)
		if !ok {
			t.Errorf("the shell dispatches %s, which the registry does not have", c.id)
			continue
		}
		raw, err := json.Marshal(c.params)
		if err != nil {
			t.Fatal(err)
		}
		var pe *engine.ParamsError
		if err := cmd.Enabled(s, raw); errors.As(err, &pe) {
			t.Errorf("%s %s: the registry refuses the shell's parameters: %v", c.id, raw, err)
		}
	}
}
