package commands_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// hiddenIDs returns the ids of a snapshot's hidden columns.
func hiddenIDs(t *engine.TableSnapshot) []string {
	var out []string
	for _, c := range t.Columns {
		if c.Hidden {
			out = append(out, c.ID)
		}
	}
	return out
}

// TestSetColumns: an agent hides and shows columns as the header's ⋮ does; the state is per view,
// visible in ui_state, applied to the view's snapshot, and validated.
func TestSetColumns(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	agent := engine.OriginExternalAgent
	h.must(t, agent, "ui.openView", map[string]any{"view": "pods"}, nil)

	h.must(t, agent, "ui.setColumns", map[string]any{"hidden": []string{"node", "namespace"}}, nil)
	var st commands.StateResult
	h.must(t, agent, "ui.state", nil, &st)
	if got := st.HiddenColumns["pods"]; !slices.Equal(got, []string{"namespace", "node"}) {
		t.Fatalf("hiddenColumns %v, want the ids in the columns' order", st.HiddenColumns)
	}
	waitTable(t, h.s, "pods", func(s *engine.TableSnapshot) bool {
		return slices.Equal(hiddenIDs(s), []string{"namespace", "node"}) && len(s.Rows) == 4
	})

	// Another view keeps its own columns.
	h.must(t, agent, "ui.setColumns", map[string]any{"view": "services", "hidden": []string{"clusterIP"}}, nil)
	if u := h.s.UIState(); !slices.Equal(u.HiddenColumns["services"], []string{"clusterIP"}) || len(u.HiddenColumns["pods"]) != 2 {
		t.Fatalf("hiddenColumns %v", u.HiddenColumns)
	}

	for _, bad := range []struct {
		params map[string]any
		want   string
	}{
		{map[string]any{"hidden": []string{"ip"}}, `no column "ip"`},
		{map[string]any{"hidden": []string{"name", "namespace", "node"}}, "at least one column"},
		{map[string]any{"view": "widgets"}, "unknown kind"},
	} {
		if err := h.run(t, agent, "ui.setColumns", bad.params, nil); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("%v: %v, want %q", bad.params, err, bad.want)
		}
	}

	// Showing every column is the default for pods: the entry goes; so does a reset.
	h.must(t, agent, "ui.setColumns", map[string]any{"hidden": []string{}}, nil)
	h.must(t, agent, "ui.setColumns", map[string]any{"view": "services", "reset": true}, nil)
	if u := h.s.UIState(); len(u.HiddenColumns) != 0 {
		t.Fatalf("hiddenColumns %v after showing every column", u.HiddenColumns)
	}
	waitTable(t, h.s, "pods", func(s *engine.TableSnapshot) bool { return len(hiddenIDs(s)) == 0 })
}

// TestExportCSV: an agent exports the open view as the table shows it: visible columns, filtered
// and sorted rows; only the open view can be exported.
func TestExportCSV(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	agent := engine.OriginExternalAgent
	h.must(t, agent, "ui.openView", map[string]any{"view": "services"}, nil)
	h.must(t, agent, "ui.setNamespace", map[string]any{"namespaces": []string{"shop"}}, nil)
	h.must(t, agent, "ui.sort", map[string]any{"column": "name", "desc": true}, nil)
	h.must(t, agent, "ui.setColumns", map[string]any{"hidden": []string{"namespace"}}, nil)
	waitTable(t, h.s, "services", func(s *engine.TableSnapshot) bool {
		return len(s.Rows) == 2 && s.Sort.Desc && len(hiddenIDs(s)) == 1
	})

	var res commands.CSVResult
	h.must(t, agent, "table.exportCsv", nil, &res)
	want := "Name,Cluster IP\ncheckout,10.96.100.100\ncart,10.96.100.101\n"
	if res.CSV != want || res.Rows != 2 || res.View != "services" || res.Filename != "services-test.csv" {
		t.Fatalf("export %+v, want csv %q", res, want)
	}
	if err := h.run(t, agent, "table.exportCsv", map[string]any{"view": "pods"}, nil); err == nil || !strings.Contains(err.Error(), "only the open view") {
		t.Fatalf("exporting a view that is not open: %v", err)
	}
}

// TestSetGroups: the Definitions view's group filter is UI state, trimmed and without duplicates.
func TestSetGroups(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	h.must(t, engine.OriginExternalAgent, "ui.setGroups", map[string]any{"groups": []string{" cert-manager.io", "", "cert-manager.io", "example.com"}}, nil)
	var st commands.StateResult
	h.must(t, engine.OriginExternalAgent, "ui.state", nil, &st)
	if !slices.Equal(st.Groups, []string{"cert-manager.io", "example.com"}) {
		t.Fatalf("groups %v", st.Groups)
	}
	h.must(t, engine.OriginGUI, "ui.setGroups", map[string]any{}, nil)
	if g := h.s.UIState().Groups; len(g) != 0 {
		t.Fatalf("groups %v after clearing", g)
	}
}

// columnIDsIn returns a snapshot's column ids in the order the table shows them.
func columnIDsIn(t *engine.TableSnapshot) []string {
	var out []string
	for _, i := range t.DisplayOrder() {
		out = append(out, t.Columns[i].ID)
	}
	return out
}

// TestSetColumnOrder: an agent arranges a view's columns as dragging a header does; the order is
// per view, visible in ui_state, applied to the view's snapshot (its columns keep their indexes)
// and to its CSV export, validated, and reset.
func TestSetColumnOrder(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	agent := engine.OriginExternalAgent
	h.must(t, agent, "ui.openView", map[string]any{"view": "services"}, nil)
	before := waitTable(t, h.s, "services", func(s *engine.TableSnapshot) bool { return len(s.Rows) == 3 })
	natural := columnIDsIn(before)
	if before.Order != nil || len(natural) < 3 {
		t.Fatalf("a new view's order %v, columns %v", before.Order, natural)
	}
	last := natural[len(natural)-1]

	h.must(t, agent, "ui.setColumnOrder", map[string]any{"order": []string{last, "name"}}, nil)
	var st commands.StateResult
	h.must(t, agent, "ui.state", nil, &st)
	want := append([]string{last, "name"}, slices.DeleteFunc(slices.Clone(natural), func(id string) bool { return id == last || id == "name" })...)
	if got := st.ColumnOrder["services"]; !slices.Equal(got, want) {
		t.Fatalf("columnOrder %v, want %v", st.ColumnOrder, want)
	}
	snap := waitTable(t, h.s, "services", func(s *engine.TableSnapshot) bool { return slices.Equal(columnIDsIn(s), want) })
	for i, c := range snap.Columns {
		if c.ID != natural[i] {
			t.Fatalf("the snapshot's columns moved: %d is %s, want %s", i, c.ID, natural[i])
		}
	}
	var csv commands.CSVResult
	h.must(t, agent, "ui.setColumns", map[string]any{"hidden": []string{"namespace"}}, nil)
	waitTable(t, h.s, "services", func(s *engine.TableSnapshot) bool { return len(hiddenIDs(s)) == 1 && s.Order != nil })
	h.must(t, agent, "table.exportCsv", nil, &csv)
	header, _, _ := strings.Cut(csv.CSV, "\n")
	if titles := strings.Split(header, ","); titles[0] != snap.Columns[slices.Index(natural, last)].Title || titles[1] != "Name" || slices.Contains(titles, "Namespace") {
		t.Fatalf("CSV header %q does not follow the order without the hidden column", header)
	}

	// Another view keeps its own order; the kind's order is no entry; reset drops one.
	h.must(t, agent, "ui.setColumnOrder", map[string]any{"view": "pods", "order": []string{"node"}}, nil)
	h.must(t, agent, "ui.setColumnOrder", map[string]any{"view": "services", "order": natural}, nil)
	if u := h.s.UIState(); u.ColumnOrder["services"] != nil || u.ColumnOrder["pods"][0] != "node" {
		t.Fatalf("columnOrder %v", u.ColumnOrder)
	}
	h.must(t, agent, "ui.setColumnOrder", map[string]any{"view": "pods", "reset": true}, nil)
	if u := h.s.UIState(); len(u.ColumnOrder) != 0 {
		t.Fatalf("columnOrder %v after a reset", u.ColumnOrder)
	}
	waitTable(t, h.s, "services", func(s *engine.TableSnapshot) bool { return s.Order == nil })

	for _, bad := range []struct {
		params map[string]any
		want   string
	}{
		{map[string]any{"order": []string{"ip"}}, `no column "ip"`},
		{map[string]any{"order": []string{"name", "name"}}, "listed twice"},
		{map[string]any{"view": "widgets", "order": []string{"name"}}, "unknown kind"},
	} {
		if err := h.run(t, agent, "ui.setColumnOrder", bad.params, nil); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("%v: %v, want %q", bad.params, err, bad.want)
		}
	}
}

// TestCSVFilenameNamesTheCluster (WF19): the suggested file name is the view and the cluster's
// short name (D-68), never the kubeconfig file the context comes from.
func TestCSVFilenameNamesTheCluster(t *testing.T) {
	for _, c := range []struct{ view, cluster, want string }{
		{"configmaps", engine.ClusterName("kind-cogpit [walk-kind.yaml]", engine.ClusterSettings{}), "configmaps-kind-cogpit.csv"},
		{"pods", engine.ClusterName("kubernetes-admin@prod-eu [dev.yaml]", engine.ClusterSettings{}), "pods-prod-eu.csv"},
		{"pods", engine.ClusterName("x [a.yaml]", engine.ClusterSettings{Name: "Shop (EU)"}), "pods-Shop_EU.csv"},
		{"workloads/overview", "arn:aws:eks:eu-central-1:1:cluster/shop", "workloads_overview-arn_aws_eks_eu-central-1_1_cluster_shop.csv"},
		{"services", "", "services.csv"},
	} {
		if got := commands.CSVFilename(c.view, c.cluster); got != c.want {
			t.Errorf("csvFilename(%q, %q) = %q, want %q", c.view, c.cluster, got, c.want)
		}
	}
}
