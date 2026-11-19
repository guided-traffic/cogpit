package commands_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// kindsConnector connects "test" to a cache over src with the real profiles of the given kinds.
type kindsConnector struct {
	src  *testkit.MemorySource
	keys []string
}

func (c *kindsConnector) Contexts() ([]engine.ContextInfo, error) {
	return []engine.ContextInfo{{Name: "test", Current: true}}, nil
}

func (c *kindsConnector) Connect(context.Context, string) (*engine.Backend, error) {
	var profiles []*model.KindProfile
	for _, k := range c.keys {
		if p, ok := kinds.ByKey(k); ok {
			profiles = append(profiles, p)
		}
	}
	ca := cache.New(c.src, cache.Options{WarmPeriod: time.Minute})
	return &engine.Backend{Context: "test", Namespace: "default", Cache: ca, Profiles: profiles, Close: ca.Close}, nil
}

// overviewCluster is a node that is not Ready and a warning event about a pod at a time of the last
// hour.
func overviewCluster(at time.Time) *testkit.MemorySource {
	src := testkit.NewMemorySource()
	src.Set(testkit.NodesGVR, testkit.Object("v1", "Node", "", "worker-2", map[string]any{
		"status": map[string]any{
			"capacity":   map[string]any{"cpu": "2", "memory": "4Gi", "pods": "110"},
			"conditions": []any{map[string]any{"type": "Ready", "status": "False"}},
		},
	}))
	src.Set(testkit.PodsGVR, testkit.Pod("shop", "web-0"))
	src.Set(testkit.EventsGVR, testkit.Object("v1", "Event", "shop", "web-0.1", map[string]any{
		"involvedObject": map[string]any{"kind": "Pod", "namespace": "shop", "name": "web-0"},
		"reason":         "Unhealthy", "type": "Warning", "message": "Readiness probe failed",
		"lastTimestamp": at.UTC().Format(time.RFC3339),
	}))
	return src
}

// TestClusterOverviewCommands: an agent opens the cluster Overview, reads it, and works its Issues
// with the commands of any view; ui.select there takes an issue's object of any kind and opens
// its drawer over the page, where a kind's view refuses another kind.
func TestClusterOverviewCommands(t *testing.T) {
	src := overviewCluster(time.Now().Add(-time.Minute))
	s := engine.New(engine.Options{Connector: &kindsConnector{src: src, keys: []string{"nodes", "pods", "deployments", "events", "namespaces"}}, SnapshotInterval: 10 * time.Millisecond})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	h := &harness{s: s, src: src}
	agent := engine.OriginExternalAgent

	h.must(t, agent, "ui.openView", map[string]any{"view": engine.ClusterOverview}, nil)
	snap := waitTable(t, s, engine.ClusterOverview, func(s *engine.TableSnapshot) bool { return s.Synced && len(s.Rows) == 2 })
	var st commands.StateResult
	h.must(t, agent, "ui.state", nil, &st)
	if st.View != engine.ClusterOverview || st.ViewTitle != "Overview" || st.Rows != 2 || st.Sort.Column != "age" || !st.Sort.Desc {
		t.Fatalf("state %+v", st)
	}

	var m engine.ClusterOverviewModel
	h.must(t, agent, "cluster.overview", map[string]any{"everyIssue": true}, &m)
	if len(m.Issues) != 2 || m.Issues[0].Source != engine.IssueEvent || m.Issues[0].Message != "Unhealthy: Readiness probe failed" ||
		m.Issues[1].Ref.Kind != "nodes" || m.Nodes != engine.NodesWorker || len(m.Charts) != 3 || m.Charts[0].Capacity != 2 {
		t.Fatalf("cluster.overview %+v", m)
	}

	node := snap.Rows[snap.IndexOf(model.ObjectRef{Context: "test", Kind: "nodes", Name: "worker-2"})].Ref
	h.must(t, agent, "ui.select", map[string]any{"refs": []model.ObjectRef{node}}, nil)
	if u := s.UIState(); u.View != engine.ClusterOverview || u.Drawer == nil || !u.Drawer.Same(node) {
		t.Fatalf("selecting an issue's node: %+v", u)
	}
	h.must(t, agent, "ui.sort", map[string]any{"column": "kind"}, nil)
	if err := h.run(t, agent, "ui.sort", map[string]any{"column": "restarts"}, nil); err == nil {
		t.Fatal("sorted the Issues by a column they do not have")
	}
	h.must(t, agent, "ui.setColumns", map[string]any{"hidden": []string{"age"}}, nil)
	waitTable(t, s, engine.ClusterOverview, func(s *engine.TableSnapshot) bool { return len(hiddenIDs(s)) == 1 && s.Sort.Column == "kind" })
	var csv commands.CSVResult
	h.must(t, agent, "table.exportCsv", nil, &csv)
	if !strings.HasPrefix(csv.CSV, "Name,Kind,Namespace,Message\n") || csv.Rows != 2 {
		t.Fatalf("csv of the Issues %+v", csv)
	}

	// The charts' range and node group: UI state, set by the page's controls and by agents.
	h.must(t, agent, "ui.setOverviewRange", map[string]any{"range": "7d"}, nil)
	h.must(t, agent, "ui.setOverviewNodes", map[string]any{"nodes": "master"}, nil)
	for _, bad := range []struct {
		id     string
		params map[string]any
	}{
		{"ui.setOverviewRange", map[string]any{"range": "90d"}}, {"ui.setOverviewRange", map[string]any{"range": "soon"}},
		{"ui.setOverviewNodes", map[string]any{"nodes": "gpu"}},
	} {
		if err := h.run(t, agent, bad.id, bad.params, nil); err == nil {
			t.Errorf("%s %v was taken", bad.id, bad.params)
		}
	}
	h.must(t, agent, "ui.state", nil, &st)
	if st.Overview != (engine.OverviewChoice{Range: "7d", Nodes: engine.NodesMaster}) {
		t.Fatalf("ui.state's overview %+v", st.Overview)
	}
	m = engine.ClusterOverviewModel{}
	h.must(t, agent, "cluster.overview", map[string]any{"noIssues": true}, &m)
	if m.Range != "7d" || m.Nodes != engine.NodesMaster || len(m.Issues) != 0 || len(m.Charts) != 3 {
		t.Fatalf("cluster.overview after the choice: %+v", m)
	}
	h.must(t, agent, "ui.setOverviewRange", map[string]any{"range": "default"}, nil)
	h.must(t, agent, "ui.setOverviewNodes", nil, nil)
	if o := s.UIState().Overview; o != (engine.OverviewChoice{}) {
		t.Fatalf("back to the defaults: %+v", o)
	}

	h.must(t, agent, "ui.openView", map[string]any{"view": "pods"}, nil)
	if err := h.run(t, agent, "ui.select", map[string]any{"refs": []model.ObjectRef{node}}, nil); err == nil {
		t.Fatal("the Pods view selected a node")
	}
}

// TestClusterOverviewFoldsForAgents: what an agent reads of the Overview's issues is the fold, for
// the open page and without it; everyIssue returns each issue as an entry of its own, as the
// engine's unfolded model has them and in the form they always had; noIssues returns neither; the
// fold's parameters narrow it, and are refused where there is no fold.
func TestClusterOverviewFoldsForAgents(t *testing.T) {
	at := time.Now().Add(-time.Minute)
	src := overviewCluster(at)
	s := engine.New(engine.Options{Connector: &kindsConnector{src: src, keys: []string{"nodes", "pods", "deployments", "events", "namespaces"}}, SnapshotInterval: 10 * time.Millisecond})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	h := &harness{s: s, src: src}
	read := func(params map[string]any) (engine.ClusterOverviewModel, map[string]json.RawMessage) {
		t.Helper()
		var raw json.RawMessage
		h.must(t, engine.OriginBuiltInAgent, "cluster.overview", params, &raw)
		var m engine.ClusterOverviewModel
		fields := map[string]json.RawMessage{}
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		return m, fields
	}
	check := func(where string) {
		t.Helper()
		m, fields := read(nil)
		f := m.Fold
		if _, listed := fields["issues"]; listed || f == nil || f.Issues != 2 || f.Error != 1 || f.Warning != 1 || f.Subjects != 2 || len(m.Charts) != 3 {
			t.Fatalf("%s: the default result is the fold: %s", where, fields["issueFold"])
		}
		node, pod := f.BySubject.Record(0), f.BySubject.Record(1)
		if node["kind"] != "nodes" || node["name"] != "worker-2" || node["level"] != "error" || node["state"] != "NotReady" ||
			pod["kind"] != "pods" || pod["namespace"] != "shop" || pod["name"] != "web-0" || pod["level"] != "warning" ||
			pod["reason"] != "Unhealthy" || pod["message"] != "Readiness probe failed" || f.Causes.Total != 2 {
			t.Errorf("%s: the fold %s", where, fields["issueFold"])
		}

		every, fields := read(map[string]any{"everyIssue": true})
		plain, err := s.ClusterOverview(context.Background(), "", nil, false)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := json.Marshal(plain.Issues)
		if _, folded := fields["issueFold"]; folded || every.Fold != nil || string(fields["issues"]) != string(want) {
			t.Fatalf("%s: everyIssue returned\n%s\nwant\n%s", where, fields["issues"], want)
		}
		// The entries as they always were: the event's with its time to the second in the computer's
		// zone, the node's without one.
		since, _ := json.Marshal(time.Unix(at.Unix(), 0))
		entries := `[{"ref":{"kind":"pods","namespace":"shop","name":"web-0"},"name":"web-0","kind":"Pod","source":"event","severity":"warning",` +
			`"message":"Unhealthy: Readiness probe failed","since":` + string(since) + `},` +
			`{"ref":{"kind":"nodes","name":"worker-2"},"name":"worker-2","kind":"Node","source":"status","severity":"error","message":"NotReady"}]`
		if string(fields["issues"]) != entries {
			t.Errorf("%s: an entry's form changed:\n%s\nwant\n%s", where, fields["issues"], entries)
		}
		var keys []string
		for k := range fields {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		if want := []string{"charts", "groups", "issues", "message", "nodeStates", "nodes", "podStates", "range", "status", "synced"}; !slices.Equal(keys, want) {
			t.Errorf("%s: the unfolded result's fields %q, want %q", where, keys, want)
		}

		if m, fields = read(map[string]any{"noIssues": true}); m.Issues != nil || m.Fold != nil || len(fields["issues"])+len(fields["issueFold"]) != 0 {
			t.Errorf("%s: noIssues returned %s %s", where, fields["issues"], fields["issueFold"])
		}
	}
	check("without the page")
	h.must(t, engine.OriginBuiltInAgent, "ui.openView", map[string]any{"view": engine.ClusterOverview}, nil)
	waitTable(t, s, engine.ClusterOverview, func(s *engine.TableSnapshot) bool { return s.Synced && len(s.Rows) == 2 })
	check("with the page open")

	// The fold's parameters.
	if m, _ := read(map[string]any{"severity": "warning"}); m.Fold.Issues != 1 || m.Fold.BySubject.Cell(0, "name") != "web-0" {
		t.Errorf("severity warning: %+v", m.Fold)
	}
	if m, _ := read(map[string]any{"sources": []string{"status"}, "limit": 1}); m.Fold.Issues != 1 || m.Fold.BySubject.Cell(0, "name") != "worker-2" {
		t.Errorf("sources status: %+v", m.Fold)
	}
	if m, _ := read(map[string]any{"reason": "unhealthy"}); m.Fold.Issues != 1 || m.Fold.Causes.Cell(0, "reason") != "Unhealthy" {
		t.Errorf("reason unhealthy: %+v", m.Fold)
	}
	if m, _ := read(map[string]any{"offset": 1}); m.Fold.Issues != 2 || len(m.Fold.BySubject.Rows) != 1 || m.Fold.BySubject.Offset != 1 {
		t.Errorf("offset 1: %+v", m.Fold.BySubject)
	}
	if m, _ := read(map[string]any{"namespaces": []string{"other"}}); m.Fold.Issues != 1 || m.Fold.BySubject.Cell(0, "kind") != "nodes" {
		t.Errorf("another namespace keeps the node's issue only: %+v", m.Fold)
	}
	m, _ := read(map[string]any{"subject": map[string]any{"kind": "pods", "namespace": "shop", "name": "web-0"}})
	if f := m.Fold; f.Issues != 1 || f.BySubject != nil || f.Members.Total != 1 || f.Members.Cell(0, "message") != "Unhealthy: Readiness probe failed" ||
		f.Members.Cell(0, "source") != "event" || f.Members.Cell(0, "level") != "warning" ||
		f.Members.Cell(0, "since") != at.UTC().Format(time.RFC3339) {
		t.Errorf("a subject's issues: %+v", f.Members)
	}
	for _, bad := range []map[string]any{
		{"everyIssue": true, "severity": "error"}, {"everyIssue": true, "limit": 5}, {"noIssues": true, "reason": "Unhealthy"},
		{"everyIssue": true, "subject": map[string]any{"kind": "pods", "name": "web-0"}},
		{"severity": "fatal"}, {"sources": []string{"alerts"}}, {"limit": -1}, {"offset": -2}, {"subject": map[string]any{"name": "web-0"}},
		{"subject": map[string]any{"kind": "pods", "name": strings.Repeat("n", 30<<10)}},
	} {
		if err := h.run(t, engine.OriginBuiltInAgent, "cluster.overview", bad, nil); err == nil {
			t.Errorf("cluster.overview %v was taken", bad)
		}
	}
}

// TestClusterOverviewWithoutIssues: a cluster with nothing wrong returns an empty list for
// everyIssue, as it always did, and a fold that counts nothing.
func TestClusterOverviewWithoutIssues(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.PodsGVR, testkit.Pod("shop", "web-0"))
	s := engine.New(engine.Options{Connector: &kindsConnector{src: src, keys: []string{"nodes", "pods", "deployments", "events", "namespaces"}}, SnapshotInterval: 10 * time.Millisecond})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	h := &harness{s: s, src: src}
	fields := map[string]json.RawMessage{}
	h.must(t, engine.OriginBuiltInAgent, "cluster.overview", map[string]any{"everyIssue": true}, &fields)
	if string(fields["issues"]) != "[]" || fields["issueFold"] != nil {
		t.Errorf("everyIssue without issues: issues %s, issueFold %s", fields["issues"], fields["issueFold"])
	}
	fields = map[string]json.RawMessage{}
	h.must(t, engine.OriginBuiltInAgent, "cluster.overview", nil, &fields)
	const empty = `{"issues":0,"error":0,"warning":0,"subjects":0,"causes":{"total":0,"columns":[],"rows":[]},"bySubject":{"total":0,"columns":["name","level"],"rows":[]}}`
	if _, listed := fields["issues"]; listed || string(fields["issueFold"]) != empty {
		t.Errorf("the fold without issues: %s", fields["issueFold"])
	}
}
