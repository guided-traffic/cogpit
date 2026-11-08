package commands_test

import (
	"context"
	"slices"
	"sort"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

func rowKeyRegistry(t *testing.T) *engine.Registry {
	t.Helper()
	r := engine.NewRegistryFor("darwin")
	if err := commands.RegisterAll(r); err != nil {
		t.Fatal(err)
	}
	return r
}

func commandIDs(cmds []*engine.Command) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.ID
	}
	return out
}

// TestRowKeysAreK9ss: a row key runs k9s's command on each kind, the kind's own before the one of
// every kind (⇧ c on a node cordons, on a Secret it copies the name), and Suspend and Resume share s
// (only one applies to a Cron Job).
func TestRowKeysAreK9ss(t *testing.T) {
	r := rowKeyRegistry(t)
	for _, tc := range []struct {
		kind, key string
		want      []string // best first; only the head of RowKeyActions is compared
	}{
		{"pods", "l", []string{"pod.logs"}},
		{"pods", "s", []string{"pod.shell"}},
		{"pods", "a", []string{"pod.attach"}},
		{"pods", "c", []string{"object.copyName"}},
		{"pods", "e", []string{"object.edit"}},
		{"pods", "Ctrl+D", []string{"object.delete"}},
		{"pods", "Cmd+Backspace", []string{"object.delete"}},
		{"pods", "Delete", []string{"object.delete"}},
		{"deployments", "l", []string{"workload.logs"}},
		{"deployments", "r", []string{"deployment.restart"}},
		{"deployments", "s", []string{"deployment.scale"}},
		{"statefulsets", "r", []string{"statefulset.restart"}},
		{"statefulsets", "s", []string{"statefulset.scale"}},
		{"daemonsets", "r", []string{"daemonset.restart"}},
		{"replicasets", "s", []string{"replicaset.scale"}},
		{"cronjobs", "t", []string{"cronjob.trigger"}},
		{"cronjobs", "s", []string{"cronjob.resume", "cronjob.suspend"}},
		{"nodes", "c", []string{"node.cordon", "object.copyName"}},
		{"nodes", "u", []string{"node.uncordon"}},
		{"nodes", "r", []string{"node.drain"}},
		{"nodes", "s", []string{"node.shell"}},
		{"secrets", "c", []string{"object.copyName"}},
		{"helmreleases", "r", []string{"helm.release.rollback"}},
		{"helmreleases", "Ctrl+D", []string{"helm.release.uninstall", "object.delete"}},
		{"portforwards", "Ctrl+D", []string{"portforward.remove", "object.delete"}},
	} {
		got := commandIDs(r.RowKeyActions(tc.kind, tc.key))
		if len(got) < len(tc.want) || !slices.Equal(got[:len(tc.want)], tc.want) {
			t.Errorf("%s %q: got %v, want %v first", tc.kind, tc.key, got, tc.want)
		}
	}
	for _, tc := range []struct {
		kinds []string
		key   string
		want  string
	}{
		{[]string{"deployments", "statefulsets"}, "r", "workload.restartMany"},
		{[]string{"pods"}, "Ctrl+D", "object.deleteMany"},
		{[]string{"portforwards"}, "Delete", "portforward.removeMany"},
	} {
		got := commandIDs(r.RowKeySelectionActions(tc.kinds, tc.key))
		if len(got) == 0 || got[0] != tc.want {
			t.Errorf("selection of %v %q: got %v, want %s first", tc.kinds, tc.key, got, tc.want)
		}
	}
}

// TestRowKeysDoNotCollide: on no kind do two of its own actions share a row key, but those that
// never both apply (Suspend and Resume of a Cron Job).
func TestRowKeysDoNotCollide(t *testing.T) {
	r := rowKeyRegistry(t)
	exclusive := [][]string{{"cronjob.resume", "cronjob.suspend"}}
	kinds := map[string]bool{}
	for _, c := range r.Commands() {
		for _, k := range c.Kinds {
			kinds[k] = true
		}
	}
	for kind := range kinds {
		keys := map[string][]string{}
		for _, c := range r.Actions(kind) {
			if len(c.Kinds) == 0 {
				continue
			}
			for _, k := range c.RowKeys {
				keys[k] = append(keys[k], c.ID)
			}
		}
		for k, ids := range keys {
			if len(ids) < 2 {
				continue
			}
			sort.Strings(ids)
			if !slices.ContainsFunc(exclusive, func(e []string) bool { return slices.Equal(e, ids) }) {
				t.Errorf("%s: %v share the row key %q", kind, ids, k)
			}
		}
	}
}

// TestRowKeysNeedARow: a row key on a command no row runs is refused.
func TestRowKeysNeedARow(t *testing.T) {
	r := engine.NewRegistry()
	type noParams struct{}
	err := engine.Register(r, engine.Spec[noParams]{
		ID: "test.nowhere", Label: "Nowhere", Danger: engine.DangerUI, RowKeys: []string{"x"},
		Run: func(_ context.Context, _ *engine.Session, _ noParams) (any, error) { return nil, nil },
	})
	if err == nil {
		t.Fatal("registered a row key without an order")
	}
	for _, bad := range []string{" ", "ll", "Ctrl+", "Hyper+D", "Ctrl+d", "Space"} {
		err := engine.Register(r, engine.Spec[noParams]{
			ID: "test.bad", Label: "Bad", Danger: engine.DangerUI, Order: 1, RowKeys: []string{bad},
			Run: func(_ context.Context, _ *engine.Session, _ noParams) (any, error) { return nil, nil },
		})
		if err == nil {
			t.Errorf("row key %q accepted", bad)
		}
	}
}
