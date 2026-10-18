package engine

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/k8seol"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// versionConnector connects "test" to a cluster whose API server reports version, as the demo
// cluster's backend does (no connection: Backend.Version), with the built-in Nodes profile.
type versionConnector struct {
	src     cache.Source
	version string
}

func (c *versionConnector) Contexts() ([]ContextInfo, error) {
	return []ContextInfo{{Name: "test"}}, nil
}

func (c *versionConnector) Connect(context.Context, string) (*Backend, error) {
	ch := cache.New(c.src, cache.Options{WarmPeriod: time.Minute})
	nodes, _ := kinds.ByKey("nodes")
	return &Backend{Context: "test", Namespace: "default", Cache: ch, Profiles: append(testProfiles(), nodes),
		Version: c.version, Close: ch.Close}, nil
}

// testClock is a clock the test moves.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time  { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *testClock) set(t time.Time) { c.mu.Lock(); c.now = t; c.mu.Unlock() }

// testReleases is the release table of the tests: upstream's of 2026-10-18 from 1.31 on, so that
// a refresh of the table built in moves no expectation.
func testReleases() *k8seol.Table {
	return &k8seol.Table{Source: "test", Fetched: "2026-10-18", Minors: []k8seol.Minor{
		{Minor: "1.31", EndOfLife: "2025-11-11", FinalPatch: "1.31.14"},
		{Minor: "1.32", EndOfLife: "2026-02-28", FinalPatch: "1.32.13"},
		{Minor: "1.33", EndOfLife: "2026-06-28", FinalPatch: "1.33.13"},
		{Minor: "1.34", Released: "2025-08-27", MaintenanceFrom: "2026-08-27", EndOfLife: "2026-10-27"},
		{Minor: "1.35", Released: "2025-12-17", MaintenanceFrom: "2026-12-28", EndOfLife: "2027-02-28"},
		{Minor: "1.36", Released: "2026-04-22", MaintenanceFrom: "2027-04-28", EndOfLife: "2027-06-28"},
		{Minor: "1.37", Released: "2026-08-26", MaintenanceFrom: "2027-08-28", EndOfLife: "2027-10-28"},
	}}
}

func versionSession(t *testing.T, src cache.Source, server string, clk *testClock) *Session {
	t.Helper()
	s := New(Options{Connector: &versionConnector{src: src, version: server}, SnapshotInterval: 5 * time.Millisecond, Now: clk.Now,
		K8sReleases: testReleases()})
	t.Cleanup(s.Close)
	connectTest(t, s)
	return s
}

func kubeletNode(name, version string) map[string]any {
	return map[string]any{"status": map[string]any{"nodeInfo": map[string]any{"kubeletVersion": version}}}
}

// The shown cluster's version in the UI state, assessed against the table built in: on
// 2026-10-18 1.34 is in its maintenance mode, nine days before its end of life (2026-10-27);
// gone while the cluster is not live; ui_state's copy assessed as of the read, past the end of
// life once the clock passed it.
func TestClusterVersionInUIState(t *testing.T) {
	clk := &testClock{now: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)}
	s := versionSession(t, testkit.NewMemorySource(), "v1.34.2", clk)
	cv := s.UIState().ClusterVersion
	if cv == nil || cv.Context != "test" || cv.Version != "v1.34.2" || cv.Minor != "1.34" || cv.EndOfLife != "2026-10-27" ||
		cv.MaintenanceFrom != "2026-08-27" || cv.State != k8seol.Maintenance || cv.DaysLeft != 9 || cv.Estimated ||
		cv.TableNewest != "1.37" || cv.TableFetched != "2026-10-18" {
		t.Fatalf("cluster version %+v", cv)
	}
	clk.set(time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC))
	if now := s.ClusterVersionNow(cv); now.State != k8seol.EndOfLife || now.DaysLeft != -1 || cv.State != k8seol.Maintenance {
		t.Errorf("as of the read: %+v (the state's own %s)", now, cv.State)
	}
	if s.ClusterVersionNow(nil) != nil {
		t.Error("no version, yet one as of now")
	}
	s.UpdateUI(func(u *UIState) { u.Connection = Connection{State: Disconnected} })
	if cv := s.UIState().ClusterVersion; cv != nil {
		t.Errorf("not connected, still %+v", cv)
	}
	// Clone copies it.
	u := UIState{ClusterVersion: &ClusterVersion{Version: "v1.36.1"}}
	c := u.Clone()
	c.ClusterVersion.Version = "changed"
	if u.ClusterVersion.Version != "v1.36.1" {
		t.Error("Clone shares the cluster version")
	}
}

// The Nodes list's Version column: a kubelet in maintenance mode in the pending colour, one past
// its end of life and one newer than the API server "<version> ⚠", each with its tooltip, which
// agents read as the row's tips; one inside the policy and supported keeps its text and gets the
// tooltip only. The drawer marks its Kubelet version and adds Upstream support and Version skew.
func TestNodesVersionCheck(t *testing.T) {
	clk := &testClock{now: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)}
	src := testkit.NewMemorySource()
	for name, v := range map[string]string{"maint": "v1.34.2", "ended": "v1.33.4", "newer": "v1.37.1", "behind": "v1.32.9", "fine": "v1.35.3"} {
		src.Set(testkit.NodesGVR, testkit.Object("v1", "Node", "", name, kubeletNode(name, v)))
	}
	src.Set(testkit.NodesGVR, testkit.Object("v1", "Node", "", "bare", nil))
	s := versionSession(t, src, "v1.36.1", clk)
	openView(s, "nodes")
	snap := waitTable(t, s, "nodes", func(t *TableSnapshot) bool {
		return t.Synced && len(t.Rows) == 6 && cellOf(t, "fine", "version").Tip != ""
	})

	warn := func(name, text, tip string) {
		t.Helper()
		c := cellOf(snap, name, "version")
		if c.Text != text || c.Status != model.StatusWarning || c.Parts == nil || len(*c.Parts) != 2 || (*c.Parts)[1].Text != "⚠" || c.Tip != tip {
			t.Errorf("%s: %+v (parts %v)\nwant %q, tip %q", name, c, c.Parts, text, tip)
		}
	}
	warn("ended", "v1.33.4 ⚠", "Upstream end of life since 2026-06-28")
	warn("newer", "v1.37.1 ⚠", "Upstream support until 2027-10-28 (375 days)\nKubelet 1.37 is newer than the API server 1.36: outside the version skew policy")
	warn("behind", "v1.32.9 ⚠", "Upstream end of life since 2026-02-28\nKubelet 1.32 is 4 minors behind the API server 1.36: the version skew policy allows 3")
	if c := cellOf(snap, "maint", "version"); c.Text != "v1.34.2" || c.Status != model.StatusPending || c.Parts != nil ||
		c.Tip != "Upstream support until 2026-10-27 (9 days), maintenance mode since 2026-08-27" {
		t.Errorf("maint: %+v", c)
	}
	if c := cellOf(snap, "fine", "version"); c.Text != "v1.35.3" || c.Status != model.StatusNone || c.Tip != "Upstream support until 2027-02-28 (133 days)" {
		t.Errorf("fine: %+v", c)
	}
	if c := cellOf(snap, "bare", "version"); c != (model.Cell{}) {
		t.Errorf("a node without a kubelet version: %+v", c)
	}
	if n := len(snap.Columns); n != len(mustProfile(t, s, "nodes").Columns) {
		t.Errorf("Nodes has %d columns: the check added one", n)
	}

	// Agents: the cell's text as drawn, its tooltip in the row's tips.
	rows := RowsIn(snap, nil, 0, 100, clk.Now(), s.TimeFormat())
	tips := map[string]string{}
	for _, r := range rows.Rows {
		tips[r.Ref.Name] = r.Tips[nodeVersionColumn]
	}
	if !strings.HasPrefix(tips["ended"], "Upstream end of life since") || tips["bare"] != "" {
		t.Errorf("view rows' tips %v", tips)
	}
	// resources_list builds the rows as the view shows them: the check's cells too.
	objs, err := s.ObjectsIn(context.Background(), "", "nodes", nil)
	if err != nil {
		t.Fatal(err)
	}
	cols, built := s.BuildRowsShown(context.Background(), s.Backend(), mustProfile(t, s, "nodes"), objs)
	vi := mustProfile(t, s, "nodes").ColumnIndex(nodeVersionColumn)
	marked := 0
	for _, r := range built {
		if vi < len(r.Cells) && strings.HasSuffix(r.Cells[vi].Text, " ⚠") && r.Cells[vi].Tip != "" {
			marked++
		}
	}
	if marked != 3 || len(cols) != len(mustProfile(t, s, "nodes").Columns) {
		t.Errorf("BuildRowsShown: %d rows marked, want 3", marked)
	}

	// The drawer.
	items := func(name string) map[string]model.DetailItem {
		t.Helper()
		m, err := s.Detail(model.ObjectRef{Context: "test", Kind: "nodes", Name: name})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]model.DetailItem{}
		var order []string
		for _, sec := range m.Sections {
			if sec.Title != model.TitleProperties {
				continue
			}
			for _, it := range sec.Items {
				out[it.Name] = it
				order = append(order, it.Name)
			}
		}
		i := strings.Index(strings.Join(order, "|"), kubeletVersionItem+"|"+upstreamSupportItem)
		if _, ok := out[kubeletVersionItem]; ok && i < 0 {
			t.Errorf("%s: Upstream support does not follow the Kubelet version: %v", name, order)
		}
		return out
	}
	ended := items("ended")
	if it := ended[kubeletVersionItem]; it.Value != "v1.33.4" || it.Status != model.StatusWarning {
		t.Errorf("ended's kubelet version %+v", it)
	}
	if it := ended[upstreamSupportItem]; it.Value != "ended 2026-06-28" || it.Status != model.StatusWarning {
		t.Errorf("ended's support %+v", it)
	}
	if _, ok := ended[versionSkewItem]; ok {
		t.Error("ended has a skew item inside the policy")
	}
	newer := items("newer")
	if it := newer[versionSkewItem]; it.Status != model.StatusWarning || it.Value != "newer than the API server 1.36" {
		t.Errorf("newer's skew %+v", it)
	}
	if it := newer[upstreamSupportItem]; it.Status != model.StatusNone || it.Value != "until 2027-10-28 (375 days)" {
		t.Errorf("newer's support %+v", it)
	}
	if it := items("maint")[kubeletVersionItem]; it.Status != model.StatusPending {
		t.Errorf("maint's kubelet version %+v", it)
	}
	if it := items("fine")[kubeletVersionItem]; it.Status != model.StatusNone {
		t.Errorf("fine's kubelet version %+v", it)
	}
	if _, ok := items("bare")[upstreamSupportItem]; ok {
		t.Error("a node without a kubelet version has an Upstream support item")
	}
}

// A backend without an API server version (a connection that read none) gets no check: no
// cluster version, no tooltips, no drawer items.
func TestNoVersionNoCheck(t *testing.T) {
	clk := &testClock{now: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)}
	src := testkit.NewMemorySource()
	src.Set(testkit.NodesGVR, testkit.Object("v1", "Node", "", "ended", kubeletNode("ended", "v1.31.4")))
	s := versionSession(t, src, "", clk)
	if cv := s.UIState().ClusterVersion; cv != nil {
		t.Errorf("cluster version %+v", cv)
	}
	openView(s, "nodes")
	snap := waitTable(t, s, "nodes", func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 1 })
	if c := cellOf(snap, "ended", "version"); c.Tip != "" || c.Text != "v1.31.4" {
		t.Errorf("cell %+v", c)
	}
	m, err := s.Detail(model.ObjectRef{Context: "test", Kind: "nodes", Name: "ended"})
	if err != nil {
		t.Fatal(err)
	}
	for _, sec := range m.Sections {
		for _, it := range sec.Items {
			if it.Name == upstreamSupportItem {
				t.Errorf("an Upstream support item %+v", it)
			}
		}
	}
}

// mergeCell: an overlay's tooltip joins the kind's a line each, and its parts redraw the cell.
func TestMergeCellTipAndParts(t *testing.T) {
	parts := &[]model.CellPart{{Text: "v1.31.4"}, {Text: "⚠", Status: model.StatusWarning}}
	got := mergeCell(model.Cell{Text: "v1.31.4", Tip: "kind's"}, model.Cell{Text: "v1.31.4 ⚠", Parts: parts, Status: model.StatusWarning, Tip: "overlay's"})
	if got.Text != "v1.31.4 ⚠" || got.Parts != parts || got.Status != model.StatusWarning || got.Tip != "kind's\noverlay's" {
		t.Errorf("merged %+v", got)
	}
	got = mergeCell(model.Cell{Text: "v1.35.3"}, model.Cell{Status: model.StatusPending, Tip: "soon"})
	if got.Text != "v1.35.3" || got.Status != model.StatusPending || got.Tip != "soon" || got.Parts != nil {
		t.Errorf("merged %+v", got)
	}
}
