package engine

import (
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// ratingCells returns the Pods view's Image Rating column: its index, and each pod's cell by name.
func ratingCells(snap *TableSnapshot) (int, map[string]model.Cell) {
	ci := slices.IndexFunc(snap.Columns, func(c model.Column) bool { return c.ID == trivyColumnID })
	out := map[string]model.Cell{}
	for _, r := range snap.Rows {
		if ci >= 0 && ci < len(r.Cells) {
			out[r.Ref.Name] = r.Cells[ci]
		}
	}
	return ci, out
}

// The Pods list's Image Rating (D-78's second phase, D-new's grades): a pod's worst image grade, its
// text the grade and the count that decides it, sorted by the grade, then that count; N/A for a pod
// without a report, sorted below S; the column without a header title (its Title for agents) and of
// a fixed width; gone while the integration is off.
func TestTrivyPodsColumn(t *testing.T) {
	src := trivyFixture()
	// Another pod of the same rating with fewer findings: sorted after the one with more.
	src.Set(testkit.PodsGVR, trivyPod("team", "lone", nil, "app=nginx:1.16.1"))
	src.Set(vulnReportsGVR, vulnReport("team", "Pod", "lone", "app", "library/nginx", "1.16.1", 2, 5))
	s := newCRDSession(t, src)
	b := s.Backend()
	waitFor(t, "the reports listed", func() bool { return trivyOn(b).synced() })
	s.UpdateUI(func(u *UIState) { u.View, u.Sort = "pods", SortSpec{Column: trivyColumnID, Desc: true} })
	snap := waitTable(t, s, "pods", func(ts *TableSnapshot) bool {
		_, cells := ratingCells(ts)
		return ts.Synced && cells["web-5d9c-a"].Text != "" && ts.Sort.Column == trivyColumnID
	})
	ci, cells := ratingCells(snap)
	if col := snap.Columns[ci]; col.Title != "Image Rating" || col.Type != model.ColumnGrade || col.Width != trivyColumnWidth {
		t.Errorf("column %+v", col)
	}
	if cols := trivyOn(b).columns(testPods); len(cols) != 1 || cols[0].After != specContainers {
		t.Errorf("the column is not placed right after Containers: %+v", cols)
	}
	for name, want := range map[string]model.Cell{
		"web-5d9c-a":         {Text: "E · 39 Critical", Grade: model.GradeE},
		"lone":               {Text: "D · 2 Critical", Grade: model.GradeD},
		"nightly-29000000-x": {Text: "D · 1 Critical", Grade: model.GradeD},
		"bare":               {Text: "D · 1 Critical", Grade: model.GradeD},
		"once-y":             {Text: "S · no known vulnerabilities", Grade: model.GradeS},
		"db-0":               {Text: "N/A · not scanned", Grade: model.GradeNA},
	} {
		if got := cells[name]; got.Text != want.Text || got.Grade != want.Grade || got.Status != model.StatusNone || got.Tip == "" {
			t.Errorf("%s: %+v, want %q %v", name, got, want.Text, want.Grade)
		}
	}
	if tip := cells["web-5d9c-a"].Tip; !strings.HasPrefix(tip, "39 Critical · ") || !strings.Contains(tip, "The worst of 2 images: nginx:1.16.1") {
		t.Errorf("web's tooltip %q", tip)
	}
	var order []string
	for _, r := range snap.Rows {
		order = append(order, r.Ref.Name)
	}
	if !slices.Equal(order[:2], []string{"web-5d9c-a", "lone"}) || order[len(order)-2] != "once-y" || order[len(order)-1] != "db-0" {
		t.Errorf("sorted by the grade, descending, N/A last: %v", order)
	}

	// A new report reaches the list.
	src.Set(testkit.PodsGVR, trivyPod("shop", "db-0", nil, "db=postgres:16"))
	src.Set(vulnReportsGVR, vulnReport("shop", "Pod", "db-0", "db", "library/postgres", "16", 0, 0, 0, 3))
	waitTable(t, s, "pods", func(ts *TableSnapshot) bool {
		_, cells := ratingCells(ts)
		return cells["db-0"].Text == "A · 3 Low" && cells["db-0"].Grade == model.GradeA
	})

	// Off: the column is gone.
	setTrivy(s, false)
	waitTable(t, s, "pods", func(ts *TableSnapshot) bool { ci, _ := ratingCells(ts); return ts.Synced && ci < 0 })
	if cols := s.ViewColumns(testPods); slices.ContainsFunc(cols, func(c model.Column) bool { return c.ID == trivyColumnID }) {
		t.Error("off, the view still has the column")
	}
}

// The integration reads the reports in a trimmed scope of its own (cache.Scope.Trim): its objects
// hold no vulnerability list, while the reports' own view reads them whole.
func TestTrivyReadsTrimmedReports(t *testing.T) {
	src := trivyFixture()
	whole := vulnReport("team", "Pod", "bare", "shell", "library/alpine", "3.12", 1)
	whole.Object["report"].(map[string]any)["vulnerabilities"] = []any{map[string]any{
		"vulnerabilityID": "CVE-2022-37434", "severity": "CRITICAL", "title": "zlib: heap-based buffer over-read",
	}}
	src.Set(vulnReportsGVR, whole)
	s := newCRDSession(t, src)
	b := s.Backend()
	waitFor(t, "the reports listed", func() bool { return trivyOn(b).synced() })
	tr := trivyOn(b)
	if sc := tr.h.Scope(); sc.Trim != trivyTrim {
		t.Fatalf("scope %+v", sc)
	}
	o, ok := tr.h.Get("team", "pod-bare-shell")
	if !ok {
		t.Fatal("no trimmed report")
	}
	if _, has := o.Object["report"].(map[string]any)["vulnerabilities"]; has {
		t.Error("the trimmed report keeps its vulnerabilities")
	}
	if r, ok := model.ParseVulnerabilityReport(o); !ok || r.Counts.Critical != 1 || r.Tag != "3.12" {
		t.Errorf("the trimmed report's summary: %+v", r)
	}
	p, _ := b.Profile(trivyReportsKind)
	h := b.Acquire(p)
	defer h.Release()
	waitFor(t, "the reports' own scope", h.HasSynced)
	o, _ = h.Get("team", "pod-bare-shell")
	if v := model.NestedSlice(o.Object, "report", "vulnerabilities"); len(v) != 1 {
		t.Errorf("the reports' own view lost the vulnerabilities: %v", v)
	}
}

func TestTrivyWorstImage(t *testing.T) {
	r := func(counts ...int64) *model.VulnerabilityReport {
		counts = append(counts, 0, 0, 0, 0, 0)
		return &model.VulnerabilityReport{Counts: model.VulnerabilityCounts{Critical: counts[0], High: counts[1], Medium: counts[2], Low: counts[3], Unknown: counts[4]}}
	}
	images := []podImage{{image: "a", report: r(0, 120)}, {image: "b"}, {image: "c", report: r(3)}, {image: "d", report: r(39)}}
	if w, ok := worstImage(images); !ok || w.image != "d" {
		t.Errorf("worst %+v", w)
	}
	if _, ok := worstImage([]podImage{{image: "b"}}); ok {
		t.Error("an image without a report is no grade")
	}
	// An end of life grades C at best: worse than a few Low, better than a Critical.
	eosl := r()
	eosl.EOSL = true
	if w, _ := worstImage([]podImage{{image: "low", report: r(0, 0, 0, 2)}, {image: "old", report: eosl}}); w.image != "old" {
		t.Errorf("worst of a Low and an end of life %+v", w)
	}
	if w, _ := worstImage([]podImage{{image: "crit", report: r(1)}, {image: "old", report: eosl}}); w.image != "crit" {
		t.Errorf("worst of a Critical and an end of life %+v", w)
	}
}
