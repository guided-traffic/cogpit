package commands_test

import (
	"slices"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Agents set a cluster's accessible namespaces freely (a UI command: it only narrows what Cogpit
// reads, D-2): the list replaces the last, comes back sorted, an empty list removes it, and a name
// that is no namespace's is refused.
func TestAccessibleNamespacesCommand(t *testing.T) {
	s, _, _ := settingsSession(t)
	agent := engine.OriginExternalAgent
	if c, ok := s.Registry().Get("cluster.settings.set"); !ok || c.Danger != engine.DangerUI {
		t.Fatalf("cluster.settings.set: %+v", c)
	}
	var v engine.ClusterSettingsView
	if err := dispatch(t, s, agent, "cluster.settings.set", map[string]any{"context": "kind-dev",
		"accessibleNamespaces": []string{"team-b", "team-a"}}, &v); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(v.AccessibleNamespaces, []string{"team-a", "team-b"}) {
		t.Errorf("after the set: %q", v.AccessibleNamespaces)
	}
	if err := dispatch(t, s, agent, "cluster.settings.set", map[string]any{"context": "kind-dev", "terminalNamespace": "team-a"}, &v); err != nil ||
		!slices.Equal(v.AccessibleNamespaces, []string{"team-a", "team-b"}) {
		t.Errorf("a set without the field changed it: %q, %v", v.AccessibleNamespaces, err)
	}
	if err := dispatch(t, s, agent, "cluster.settings.set", map[string]any{"context": "kind-dev",
		"accessibleNamespaces": []string{"Team_A"}}, nil); err == nil {
		t.Error("an invalid namespace accepted")
	}
	v = engine.ClusterSettingsView{}
	if err := dispatch(t, s, agent, "cluster.settings.get", map[string]any{"context": "kind-dev"}, &v); err != nil ||
		!slices.Equal(v.AccessibleNamespaces, []string{"team-a", "team-b"}) {
		t.Errorf("get: %q, %v", v.AccessibleNamespaces, err)
	}
	if err := dispatch(t, s, agent, "cluster.settings.set", map[string]any{"context": "kind-dev",
		"accessibleNamespaces": []string{}}, &v); err != nil || len(v.AccessibleNamespaces) != 0 || v.AccessibleNamespaces == nil {
		t.Errorf("cleared: %#v, %v", v.AccessibleNamespaces, err)
	}
	if got := s.UIState().ClusterSettings["kind-dev"].AccessibleNamespaces; got != nil {
		t.Errorf("UI state after clearing: %q", got)
	}
}
