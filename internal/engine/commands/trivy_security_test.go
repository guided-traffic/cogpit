package commands_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
	"github.com/guided-traffic/cogpit/internal/testkit/enginekit"
)

// newImageSecHarness connects to a cluster running Trivy Operator (testkit.ImageSecuritySource),
// the built-in kinds and the custom kinds watched, the Trivy integration on.
func newImageSecHarness(t *testing.T) *harness {
	t.Helper()
	src := testkit.ImageSecuritySource()
	h := &harness{src: src, writer: &fakeWriter{}}
	h.s = engine.New(engine.Options{Connector: enginekit.MemoryConnector{Context: "test", Src: src, Profiles: kinds.Builtin(), CustomResources: true},
		SnapshotInterval: 5 * time.Millisecond})
	if err := commands.RegisterAll(h.s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.s.Close)
	if err := h.s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	return h
}

// TestImageSecurityCommand: trivy.imageSecurity reads what the page shows, the findings cut at the
// limit; the page opens with its default search; its rows are the workloads' (select all takes
// them, view.rows and the CSV export read them); a card's token narrows them through ui.setFilter;
// with the integration off the read is refused.
func TestImageSecurityCommand(t *testing.T) {
	h := newImageSecHarness(t)
	agent := engine.OriginExternalAgent
	var res commands.ImageSecurityResult
	h.must(t, agent, "trivy.imageSecurity", map[string]any{"findings": true, "limit": 3}, &res)
	if res.Running != 4 || res.Scanned != 3 || res.CVEs != 7 || res.Fixable != 3 || len(res.TopImages) != 3 || len(res.TopCVEs) != 5 {
		t.Errorf("model %+v", res.ImageSecurityModel)
	}
	if res.Findings == nil || res.Matched != 8 || res.Returned != 3 || len(res.Findings.Rows) != 3 || res.Findings.Filter != engine.ImageSecurityDefaultFilter {
		t.Fatalf("findings: matched %d, returned %d, %+v", res.Matched, res.Returned, res.Findings)
	}
	all := ""
	h.must(t, agent, "trivy.imageSecurity", map[string]any{"findings": true, "filter": all, "namespaces": []string{"team"}}, &res)
	if res.Matched != 2 || res.Findings.Total != 11 || res.CVEs != 2 {
		t.Errorf("team, every finding: matched %d of %d, %d CVEs", res.Matched, res.Findings.Total, res.CVEs)
	}

	// The page: opened with sev>=high; select all takes the rows' workloads; view.rows and the CSV
	// read the rows the table shows.
	h.must(t, engine.OriginGUI, "ui.openView", map[string]any{"view": engine.ImageSecurity}, nil)
	if f := h.s.UIState().Filter; f != engine.ImageSecurityDefaultFilter {
		t.Fatalf("opened with %q", f)
	}
	waitTable(t, h.s, engine.ImageSecurity, func(t *engine.TableSnapshot) bool { return t.Synced && len(t.Rows) == 8 })
	h.must(t, engine.OriginGUI, "ui.select", map[string]any{"all": true, "noDrawer": true}, nil)
	var kindsSel []string
	for _, r := range h.s.UIState().Selected {
		kindsSel = append(kindsSel, r.Kind+"/"+r.Name)
	}
	slices.Sort(kindsSel)
	if want := []string{"deployments/api", "pods/bare", "statefulsets/db"}; !slices.Equal(slices.Compact(kindsSel), want) {
		t.Errorf("selected %v", kindsSel)
	}
	var rows engine.ViewRows
	h.must(t, agent, "view.rows", map[string]any{}, &rows)
	if rows.Shown != 8 || rows.Total != 11 || len(rows.Rows) != 8 {
		t.Errorf("view.rows: %d shown of %d", rows.Shown, rows.Total)
	}
	var csv commands.CSVResult
	h.must(t, agent, "table.exportCsv", map[string]any{}, &csv)
	if csv.Rows != 8 || !strings.HasPrefix(csv.CSV, "Namespace,Workload,Container,Image,CVE,Severity,Score,Package,Installed,Fixed") {
		t.Errorf("csv %d rows: %.120q", csv.Rows, csv.CSV)
	}
	filter := engine.ImageSecurityFilterWith(h.s.UIState().Filter, res.TopNamespaces[0].Token)
	h.must(t, engine.OriginGUI, "ui.setFilter", map[string]any{"filter": filter}, nil)
	waitTable(t, h.s, engine.ImageSecurity, func(t *engine.TableSnapshot) bool {
		return len(t.Rows) == 2 && t.Rows[0].Ref == model.ObjectRef{Context: "test", Kind: "pods", Namespace: "team", Name: "bare"}
	})

	// Switched off: refused, and the page reads nothing.
	h.s.UpdateUI(func(u *engine.UIState) {
		if u.ClusterSettings == nil {
			u.ClusterSettings = map[string]engine.ClusterSettings{}
		}
		cs := u.ClusterSettings["test"]
		cs.SetIntegrations(0, 0, engine.IntTrivy)
		u.ClusterSettings["test"] = cs
	})
	if err := h.run(t, agent, "trivy.imageSecurity", map[string]any{}, nil); !errors.Is(err, engine.ErrTrivyOff) && (err == nil || !strings.Contains(err.Error(), "Trivy integration is off")) {
		t.Errorf("off: %v", err)
	}
	waitTable(t, h.s, engine.ImageSecurity, func(t *engine.TableSnapshot) bool { return len(t.Rows) == 0 && t.Error != "" })
}
