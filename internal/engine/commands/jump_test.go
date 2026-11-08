package commands_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// ui.jump opens a view as k9s's command line names it, for the palette's jump and for agents: a
// kind by its short name, singular or plural in any case; the namespace filter set to one
// namespace, cleared by all, kept without one; the search; another cluster shown, connected first;
// what it cannot open an error that says why, with nothing changed.
func TestJumpCommand(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	agent := engine.OriginExternalAgent
	var st commands.StateResult

	h.must(t, agent, "ui.setNamespace", map[string]any{"namespaces": []string{"infra"}}, nil)
	h.must(t, agent, engine.JumpCommand, map[string]any{"view": "deploy"}, &st)
	if st.View != "deployments" || !slices.Equal(st.Namespaces, []string{"infra"}) || st.Filter != "" {
		t.Errorf("deploy: view %q, namespaces %v, filter %q; want deployments with the filter kept", st.View, st.Namespaces, st.Filter)
	}
	h.must(t, agent, engine.JumpCommand, map[string]any{"view": "PO", "namespace": "shop", "filter": "web-1"}, &st)
	if st.View != "pods" || !slices.Equal(st.Namespaces, []string{"shop"}) || st.Filter != "web-1" {
		t.Errorf("PO shop /web-1: view %q, namespaces %v, filter %q", st.View, st.Namespaces, st.Filter)
	}
	waitTable(t, h.s, "pods", func(s *engine.TableSnapshot) bool { return len(s.Rows) == 1 && s.Rows[0].Ref.Name == "web-1" })
	h.must(t, agent, engine.JumpCommand, map[string]any{"view": "service", "namespace": engine.JumpAllNamespaces}, &st)
	if st.View != "services" || len(st.Namespaces) != 0 || st.Filter != "" {
		t.Errorf("service all: view %q, namespaces %v, filter %q; want every namespace, the search cleared", st.View, st.Namespaces, st.Filter)
	}
	h.must(t, agent, engine.JumpCommand, map[string]any{"view": "pulses"}, &st)
	if st.View != engine.ClusterOverview {
		t.Errorf("pulses: view %q", st.View)
	}

	for _, tt := range []struct {
		params map[string]any
		want   string
	}{
		{map[string]any{"view": "nope"}, "unknown kind"},
		{map[string]any{"view": "ns", "namespace": "shop"}, "Namespaces are not namespaced"},
		{map[string]any{"view": " "}, "no view named"},
	} {
		before := h.s.UIState()
		err := h.run(t, agent, engine.JumpCommand, tt.params, nil)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want an error saying %q", tt.params, err, tt.want)
		}
		if after := h.s.UIState(); after.View != before.View || !slices.Equal(after.Namespaces, before.Namespaces) {
			t.Errorf("%v changed the view to %q in %v", tt.params, after.View, after.Namespaces)
		}
	}
	if err := h.run(t, agent, engine.JumpCommand, map[string]any{"view": "nope"}, nil); !errors.Is(err, engine.ErrUnknownKind) {
		t.Errorf("an unknown name: %v, want ErrUnknownKind", err)
	}
	if err := h.run(t, agent, engine.JumpCommand, map[string]any{"view": "po", "context": "broken"}, nil); err == nil ||
		!strings.Contains(err.Error(), "could not connect to broken: ") {
		t.Errorf("a cluster that does not connect: %v", err)
	}
	h.must(t, agent, "cluster.connect", map[string]any{"context": "test"}, nil) // back from the failed connect's page

	h.must(t, agent, engine.JumpCommand, map[string]any{"view": "svc", "namespace": "infra", "context": "other"}, &st)
	if st.Context != "other" || !st.Connection.State.Live() || st.View != "services" || !slices.Equal(st.Namespaces, []string{"infra"}) {
		t.Errorf("svc infra @other: context %q (%s), view %q, namespaces %v", st.Context, st.Connection.State, st.View, st.Namespaces)
	}
	h.must(t, agent, engine.JumpCommand, map[string]any{"view": "po", "context": "test"}, &st)
	if st.Context != "test" || st.View != "pods" {
		t.Errorf("po @test, connected already: context %q, view %q", st.Context, st.View)
	}
}

// kinds.list says each kind's singular and short names, which ui.jump takes.
func TestKindsListNames(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	var kinds []commands.KindInfo
	h.must(t, engine.OriginExternalAgent, "kinds.list", map[string]any{}, &kinds)
	i := slices.IndexFunc(kinds, func(k commands.KindInfo) bool { return k.Kind == "pods" })
	if i < 0 || kinds[i].Singular != "pod" || !slices.Equal(kinds[i].ShortNames, []string{"po"}) {
		t.Errorf("pods: %+v", kinds[i])
	}
}
