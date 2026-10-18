//go:build kind

package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model/k8seol"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestKindClusterVersion (D-91) connects the engine to the kind cluster and checks, reading only:
// the UI state's cluster version is the one the API server's GET /version answered, assessed by
// the release table built in; every node's Version cell carries its kubelet's tooltip, which
// agents read as the rows' tips (view_rows, resources_list), and a kind kubelet is the API
// server's minor, inside the skew policy; the node drawer has its Upstream support.
func TestKindClusterVersion(t *testing.T) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn, err := cluster.Connect(ctx, cluster.Options{KubeconfigPaths: []string{kubeconfig}, Context: contextName})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	want := conn.ServerVersion()

	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}}})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, contextName); err != nil {
		t.Fatal(err)
	}
	cv := s.UIState().ClusterVersion
	sup := k8seol.SupportOf(want)
	if cv == nil || cv.Version != want || cv.Minor == "" || cv.Minor != sup.Minor || cv.EndOfLife != sup.EndOfLife || cv.State == k8seol.Unknown {
		t.Fatalf("the cluster version %+v, want %s (%+v)", cv, want, sup)
	}
	t.Logf("the API server %s: %s, end of life %s (%d days), estimated %v", cv.Version, cv.State, cv.EndOfLife, cv.DaysLeft, cv.Estimated)

	dispatch := func(id string, params any) json.RawMessage {
		t.Helper()
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		res, err := s.Dispatch(ctx, engine.Invocation{ID: id, Params: raw, Origin: engine.OriginExternalAgent})
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		out, err := json.Marshal(res)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	var state struct {
		ClusterVersion *engine.ClusterVersion `json:"clusterVersion"`
	}
	if err := json.Unmarshal(dispatch("ui.state", nil), &state); err != nil || state.ClusterVersion == nil || state.ClusterVersion.Version != want {
		t.Fatalf("ui_state's clusterVersion %+v (%v)", state.ClusterVersion, err)
	}

	dispatch("ui.openView", map[string]any{"view": "nodes"})
	if !testkit.WaitFor(ctx, func() bool { tbl := s.Table("nodes"); return tbl != nil && tbl.Synced && len(tbl.Rows) > 0 }) {
		t.Fatal("the Nodes view never listed")
	}
	var rows engine.ViewRows
	if err := json.Unmarshal(dispatch("view.rows", map[string]any{}), &rows); err != nil || len(rows.Rows) == 0 {
		t.Fatalf("view_rows: %+v (%v)", rows, err)
	}
	for _, r := range rows.Rows {
		tip := r.Tips["version"]
		if !strings.HasPrefix(tip, "Upstream ") || strings.Contains(tip, "skew") {
			t.Errorf("%s: the version's tip %q", r.Ref.Name, tip)
		}
	}
	var list commands.ListResult
	if err := json.Unmarshal(dispatch("resources.list", map[string]any{"kind": "nodes"}), &list); err != nil || len(list.Rows) == 0 ||
		!strings.HasPrefix(list.Rows[0].Tips["Version"], "Upstream ") {
		t.Fatalf("resources_list: %+v (%v)", list, err)
	}
	m, err := s.DetailWait(ctx, rows.Rows[0].Ref)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, sec := range m.Sections {
		for _, it := range sec.Items {
			found = found || (it.Name == "Upstream support" && it.Value != "")
		}
	}
	if !found {
		t.Errorf("the node drawer has no Upstream support: %+v", m.Sections)
	}
	t.Logf("node %s: %s", rows.Rows[0].Ref.Name, rows.Rows[0].Tips["version"])
}
