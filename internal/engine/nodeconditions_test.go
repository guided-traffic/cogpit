package engine

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

func conditionNode(name string, conds ...string) *unstructured.Unstructured {
	list := []any{map[string]any{"type": "Ready", "status": "True"}}
	for _, c := range conds {
		list = append(list, map[string]any{"type": c, "status": "True", "lastTransitionTime": "2026-11-01T10:00:00Z"})
	}
	return testkit.Object("v1", "Node", "", name, map[string]any{"status": map[string]any{"conditions": list}})
}

func nodesProfile(t *testing.T) *model.KindProfile {
	t.Helper()
	for _, p := range kinds.Builtin() {
		if p.Key == kindNodes {
			return p
		}
	}
	t.Fatal("no nodes profile")
	return nil
}

// The Overview dates a node's problem as the Nodes list reads its conditions, by the profile's own
// rule: EtcdIsVoter True and an accepted type are none.
func TestNodeProblemSinceReadsTheProfile(t *testing.T) {
	p := nodesProfile(t)
	for _, c := range []struct {
		typ  string
		want bool
	}{{"KernelDeadlock", true}, {"EtcdIsVoter", false}, {"SystemdUnitsReady", false}, {"SysctlChanged", false}} {
		if got := !nodeProblemSince(p, conditionNode("n", c.typ)).IsZero(); got != c.want {
			t.Errorf("%s True dated as a problem: %v, want %v", c.typ, got, c.want)
		}
	}
	if !nodeProblemSince(kinds.NodesAccepting(p, []string{"KernelDeadlock"}), conditionNode("n", "KernelDeadlock")).IsZero() {
		t.Error("an accepted type dated as a problem")
	}
}

// A backend's Nodes profile is the built-in one until the cluster accepts a type, stays the same
// for the same list, and only the Nodes kind changes.
func TestBackendAcceptedNodeConditions(t *testing.T) {
	b := &Backend{Context: "c", Profiles: kinds.Builtin()}
	base, _ := b.Profile(kindNodes)
	pods, _ := b.Profile("pods")
	b.setAcceptedNodeConditions([]string{"FooProblem"})
	own, _ := b.Profile(kindNodes)
	if own == base {
		t.Fatal("no profile of its own")
	}
	b.setAcceptedNodeConditions([]string{"FooProblem"})
	if again, _ := b.Profile(kindNodes); again != own {
		t.Error("the same list made a new profile, which restarts the views")
	}
	if p, _ := b.Profile("pods"); p != pods {
		t.Error("another kind changed")
	}
	if row := model.BuildRow(own, "c", conditionNode("n", "FooProblem")); row.Status != model.StatusOK {
		t.Errorf("accepted type: row %v", row.Status)
	}
	if row := model.BuildRow(base, "c", conditionNode("n", "FooProblem")); row.Status != model.StatusWarning {
		t.Errorf("built-in profile: row %v", row.Status)
	}
	b.setAcceptedNodeConditions(nil)
	if p, _ := b.Profile(kindNodes); p != base {
		t.Error("an empty list kept the cluster's profile")
	}
	empty := &Backend{Context: "c"}
	empty.setAcceptedNodeConditions([]string{"FooProblem"})
	if empty.acceptedNodes.Load() != nil {
		t.Error("a cluster without the Nodes kind got a profile")
	}
}

func TestCheckAcceptedNodeConditions(t *testing.T) {
	for _, bad := range [][]string{{"Ready"}, {"has space"}, {strings.Repeat("x", maxNodeConditionType+1)}, {""}} {
		if checkAcceptedNodeConditions(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if err := checkAcceptedNodeConditions([]string{"example.com/SomeCondition", "FooProblem"}); err != nil {
		t.Error(err)
	}
	if got := keptAcceptedNodeConditions([]string{" B ", "A", "Ready", "A", "has space"}); !slices.Equal(got, []string{"A", "B"}) {
		t.Errorf("kept %q", got)
	}
	if got := keptAcceptedNodeConditions([]string{"Ready"}); got != nil {
		t.Errorf("kept %q", got)
	}
}

func TestRefuseAcceptedNodeConditions(t *testing.T) {
	agent := context.WithValue(context.Background(), originKey{}, OriginExternalAgent)
	gui := context.WithValue(context.Background(), originKey{}, OriginGUI)
	if err := RefuseAcceptedNodeConditions(agent, []string{"X"}); !errors.Is(err, ErrAcceptedNodeConditionsGUIOnly) {
		t.Errorf("agent adding: %v", err)
	}
	for _, clear := range [][]string{nil, {}, {"  "}} {
		if err := RefuseAcceptedNodeConditions(agent, clear); err != nil {
			t.Errorf("agent clearing %q: %v", clear, err)
		}
	}
	if err := RefuseAcceptedNodeConditions(gui, []string{"X"}); err != nil {
		t.Errorf("the user: %v", err)
	}
}

// newSettingsTestSession is newTestSession with the Nodes kind and a settings store.
func newSettingsTestSession(t *testing.T, src *testkit.MemorySource, store *ClusterSettingsStore) *Session {
	t.Helper()
	fc := &fakeConnector{src: src, profiles: append(testProfiles(), nodesProfile(t))}
	s := New(Options{Connector: fc, SnapshotInterval: 5 * time.Millisecond, ClusterSettings: store})
	t.Cleanup(s.Close)
	return s
}

// The open Nodes view follows the list at once, and a saved list applies at the next
// connect.
func TestAcceptedNodeConditionsFollowTheSettings(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.NodesGVR, conditionNode("gke-1", "SysctlChanged", "FooProblem"))
	store := NewClusterSettingsStore(t.TempDir())
	s := newSettingsTestSession(t, src, store)
	connectTest(t, s)
	s.UpdateUI(func(u *UIState) { u.View = kindNodes })
	conditions := func(snap *TableSnapshot) string {
		p, _ := s.Backend().Profile(kindNodes)
		return snap.Rows[0].Cells[p.ColumnIndex("conditions")].Text
	}
	shows := func(want string) {
		t.Helper()
		waitTable(t, s, kindNodes, func(snap *TableSnapshot) bool { return len(snap.Rows) == 1 && conditions(snap) == want })
	}
	shows("Ready FooProblem") // SysctlChanged is built in; FooProblem still warns

	if _, err := s.ChangeClusterSettings("test", func(cs *ClusterSettings) error {
		cs.AcceptedNodeConditions = []string{"FooProblem"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	shows("Ready")

	if _, err := s.ChangeClusterSettings("test", func(cs *ClusterSettings) error {
		cs.AcceptedNodeConditions = nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	shows("Ready FooProblem")

	if _, err := s.ChangeClusterSettings("test", func(cs *ClusterSettings) error {
		cs.AcceptedNodeConditions = []string{"FooProblem"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	shows("Ready")

	again := newSettingsTestSession(t, src, NewClusterSettingsStore(store.dir))
	connectTest(t, again)
	if p, _ := again.Backend().Profile(kindNodes); model.BuildRow(p, "test", conditionNode("n", "FooProblem")).Status != model.StatusOK {
		t.Error("the saved list did not apply at the next connect")
	}
}
