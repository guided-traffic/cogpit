package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// imageSecSession connects to a cluster running Trivy Operator (testkit.ImageSecuritySource) with
// the integration on, once the reports' kind is known.
func imageSecSession(t *testing.T, src *testkit.MemorySource) *Session {
	t.Helper()
	s := New(Options{Connector: &crdConnector{src: src}, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	connectTest(t, s)
	waitFor(t, "the reports' kind", func() bool { _, ok := s.Backend().Profile(trivyReportsKind); return ok })
	return s
}

// findingCells are a table's rows as "<workload>/<container> <cve> <severity> <package>".
func findingCells(t *TableSnapshot) []string {
	out := make([]string, 0, len(t.Rows))
	for _, r := range t.Rows {
		out = append(out, r.Cells[1].Text+"/"+r.Cells[2].Text+" "+r.Cells[4].Text+" "+r.Cells[5].Text+" "+r.Cells[7].Text)
	}
	return out
}

// The page's model (D-new): the header counts the distinct images of the running pods and those a
// report covers; Vulnerabilities counts distinct CVE ids under the worst severity a finding gives
// them and those with a fix; Top images ranks by grade, then Critical, then High; Top CVEs by
// severity, then the images affected; Top namespaces by the worst grade; each entry names its token.
func TestImageSecurityModel(t *testing.T) {
	s := imageSecSession(t, testkit.ImageSecuritySource())
	m, _, err := s.ImageSecurity(context.Background(), "", nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !m.Synced || !m.PodsRead || m.Running != 4 || m.Scanned != 3 || m.NotScanned != 1 || m.Reports != 3 {
		t.Errorf("header %+v", m)
	}
	if want := time.Date(2026, 10, 17, 9, 55, 22, 0, time.UTC); !m.NewestScan.Equal(want) {
		t.Errorf("newest scan %v", m.NewestScan)
	}
	if m.CVEs != 7 || m.Fixable != 3 {
		t.Errorf("%d CVEs, %d fixable", m.CVEs, m.Fixable)
	}
	var sev []string
	for _, c := range m.Severities {
		sev = append(sev, fmt.Sprintf("%s=%d/%s", c.Severity, c.Count, c.Status))
	}
	// CVE-2024-0004 is Medium in shop/api, High in the bare pod: High. Unknown shows, as one has it.
	if want := []string{"Critical=2/error", "High=3/warning", "Medium=0/", "Low=1/", "Unknown=1/"}; !slices.Equal(sev, want) {
		t.Errorf("severities %v, want %v", sev, want)
	}
	var images []string
	for _, im := range m.TopImages {
		images = append(images, fmt.Sprintf("%s %s %d %d %s", im.Grade, im.Image, im.Critical, im.High, im.Token))
	}
	if want := []string{
		"D shop/api:2.4.1 3 1 image:shop/api:2.4.1", "D library/postgres:13.4 1 1 image:library/postgres:13.4",
		"C library/alpine:3.12 0 2 image:library/alpine:3.12",
	}; !slices.Equal(images, want) {
		t.Errorf("top images %q", images)
	}
	var cves []string
	for _, c := range m.TopCVEs {
		cves = append(cves, fmt.Sprintf("%s %s %d %s", c.Severity, c.ID, c.Images, c.Token))
	}
	if want := []string{
		"Critical CVE-2024-0001 2 cve:CVE-2024-0001", "Critical CVE-2024-0002 1 cve:CVE-2024-0002",
		"High CVE-2024-0003 2 cve:CVE-2024-0003", "High CVE-2024-0004 2 cve:CVE-2024-0004", "High CVE-2024-0006 1 cve:CVE-2024-0006",
	}; !slices.Equal(cves, want) {
		t.Errorf("top CVEs %q", cves)
	}
	var spaces []string
	for _, n := range m.TopNamespaces {
		spaces = append(spaces, fmt.Sprintf("%s %s %d %d %s", n.Grade, n.Namespace, n.Critical, n.High, n.Token))
	}
	if want := []string{"D shop 2 3 ns:shop", "C team 0 2 ns:team"}; !slices.Equal(spaces, want) {
		t.Errorf("top namespaces %q", spaces)
	}

	// One namespace: the cards count it alone.
	m, _, err = s.ImageSecurity(context.Background(), "", []string{"team"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if m.Running != 1 || m.Scanned != 1 || m.CVEs != 2 || m.Fixable != 0 || len(m.TopImages) != 1 || len(m.TopNamespaces) != 1 {
		t.Errorf("team only %+v", m)
	}
}

// The findings table: one row per finding, its workload named as Kubernetes users name it (a
// ReplicaSet's Deployment), the row a reference to the workload; the tokens narrow it, the rest of
// the search is the views' search; Total counts every finding; the order is the worst severity
// first and stable.
func TestImageSecurityFindings(t *testing.T) {
	s := imageSecSession(t, testkit.ImageSecuritySource())
	read := func(filter string) *TableSnapshot {
		t.Helper()
		_, snap, err := s.ImageSecurity(context.Background(), "", nil, &filter, ImageSecurityFindings)
		if err != nil {
			t.Fatal(err)
		}
		return snap
	}
	all := read("")
	if all.Total != 11 || len(all.Rows) != 11 || all.FilterError != "" {
		t.Fatalf("all: %d of %d, %q", len(all.Rows), all.Total, all.FilterError)
	}
	first := all.Rows[0]
	if first.Ref != (model.ObjectRef{Context: "test", Kind: "deployments", Namespace: "shop", Name: "api"}) || first.Status != model.StatusNone {
		t.Errorf("first row %+v", first)
	}
	if got := []string{first.Cells[0].Text, first.Cells[1].Text, first.Cells[2].Text, first.Cells[3].Text, first.Cells[4].Text,
		first.Cells[5].Text, first.Cells[6].Text, first.Cells[7].Text, first.Cells[8].Text, first.Cells[9].Text}; !slices.Equal(got,
		[]string{"shop", "Deployment api", "app", "shop/api:2.4.1", "CVE-2024-0001", "Critical", "9.8", "curl", "7.88.1-10", "8.4.0"}) {
		t.Errorf("first row's cells %q", got)
	}
	if first.Cells[5].Status != model.StatusError {
		t.Errorf("Critical's status %q", first.Cells[5].Status)
	}
	for _, r := range all.Rows {
		for i, c := range r.Cells {
			if c.Status != model.StatusNone && i != 5 {
				t.Errorf("%s: a status on column %d", r.Ref, i)
			}
		}
		switch r.Cells[5].Text {
		case "High":
			if r.Cells[5].Status != model.StatusWarning {
				t.Errorf("High's status %q", r.Cells[5].Status)
			}
		case "Medium", "Low", "Unknown":
			if r.Cells[5].Status != model.StatusNone {
				t.Errorf("%s's status %q", r.Cells[5].Text, r.Cells[5].Status)
			}
		}
	}
	last := all.Rows[len(all.Rows)-1]
	if last.Cells[5].Text != "Unknown" || last.Cells[6].Text != noScore || last.Cells[1].Text != "StatefulSet db" ||
		last.Ref != (model.ObjectRef{Context: "test", Kind: "statefulsets", Namespace: "shop", Name: "db"}) {
		t.Errorf("last row %q %+v", findingCells(&TableSnapshot{Rows: []model.Row{last}}), last.Ref)
	}

	def := read(ImageSecurityDefaultFilter)
	want := []string{
		"Deployment api/app CVE-2024-0001 Critical curl", "Deployment api/app CVE-2024-0001 Critical libcurl4",
		"Deployment api/app CVE-2024-0002 Critical golang.org/x/crypto", "StatefulSet db/postgres CVE-2024-0001 Critical libcurl4",
		"Deployment api/app CVE-2024-0003 High golang.org/x/net", "StatefulSet db/postgres CVE-2024-0006 High libc6",
		"Pod bare/shell CVE-2024-0003 High golang.org/x/net", "Pod bare/shell CVE-2024-0004 High openssl",
	}
	if got := findingCells(def); !slices.Equal(got, want) || def.Total != 11 {
		t.Errorf("sev>=high: %q (total %d)", got, def.Total)
	}
	if again := read(ImageSecurityDefaultFilter); !slices.Equal(findingCells(again), want) {
		t.Errorf("a second build ordered the rows otherwise: %q", findingCells(again))
	}

	for filter, n := range map[string]int{
		"sev>=critical":                 4,
		"SEV:low":                       1,
		"sev:low sev:unknown":           2, // one key twice: either
		"fixable":                       5,
		"sev>=high fixable":             5,
		"ns:team":                       2,
		"ns:team ns:shop":               11,
		"image:postgres":                3,
		"pkg:curl":                      3, // curl and libcurl4
		"cve:cve-2024-0003":             2,
		"cve:CVE-2024-0003 ns:shop":     1,
		"libcurl":                       2, // the views' search: any cell
		"libcurl team":                  0,
		"ns:":                           11, // a token being typed filters nothing
		"sev>=high image:shop/api 0.17": 1,
	} {
		if got := read(filter); len(got.Rows) != n || got.FilterError != "" {
			t.Errorf("%q: %d rows %q, want %d (filter error %q)", filter, len(got.Rows), findingCells(got), n, got.FilterError)
		}
	}
	bad := read("sev>=severe")
	if len(bad.Rows) != 11 || !strings.Contains(bad.FilterError, "critical, high, medium, low or unknown") {
		t.Errorf("a severity that is none: %d rows, %q", len(bad.Rows), bad.FilterError)
	}

	// A card's entry replaces the token of its key, the rest of the search stays.
	for _, c := range [][3]string{
		{"sev>=high", "ns:shop", "sev>=high ns:shop"},
		{"sev>=high ns:team libc", "ns:shop", "sev>=high libc ns:shop"},
		{"", "cve:CVE-2024-0001", "cve:CVE-2024-0001"},
		{"image:a IMAGE:b x", "image:shop/api:2.4.1", "x image:shop/api:2.4.1"},
	} {
		if got := ImageSecurityFilterWith(c[0], c[1]); got != c[2] {
			t.Errorf("%q + %q = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}

// The page in the window: it opens with sev>=high, which the user may clear; its worker reads the
// reports in the trimmed scope (the vulnerabilities' titles and links dropped) and the pods while
// it is open, and nothing once it is left; switched off, the page reads nothing and says why, and
// on again it reads.
func TestImageSecurityPage(t *testing.T) {
	src := testkit.ImageSecuritySource()
	s := imageSecSession(t, src)
	b := s.Backend()
	u := s.OpenTab(ImageSecurity, false)
	if u.Filter != ImageSecurityDefaultFilter || u.View != ImageSecurity {
		t.Fatalf("opened with %q on %q", u.Filter, u.View)
	}
	cves := waitTable(t, s, ImageSecurity, func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 5 })
	if cves.Title != ImageSecurityTitle || cves.Sort != imageSecCVESort || cves.Total != 7 {
		t.Errorf("CVEs %q sorted %+v, total %d", cves.Title, cves.Sort, cves.Total)
	}
	// A CVE's findings: the findings table with its token, a step of the tab's history; the CVEs
	// again without it.
	u, err := s.ShowImageSecurityTable(ImageSecurityFindings, "CVE-2024-0001")
	if err != nil || u.View != ImageSecurityFindings || u.Filter != "sev>=high cve:CVE-2024-0001" || u.Sort != imageSecSort {
		t.Fatalf("a CVE's findings: %v, %q %q %+v", err, u.View, u.Filter, u.Sort)
	}
	waitTable(t, s, ImageSecurityFindings, func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 3 })
	if u, _ = s.ShowImageSecurityTable(ImageSecurity, ""); u.View != ImageSecurity || u.Filter != "sev>=high" {
		t.Fatalf("back to the CVEs: %q %q", u.View, u.Filter)
	}
	if u, _ = s.ShowImageSecurityTable(ImageSecurityFindings, ""); u.Filter != "sev>=high" {
		t.Fatalf("the findings: %q", u.Filter)
	}
	snap := waitTable(t, s, ImageSecurityFindings, func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 8 })
	if snap.Title != ImageSecurityTitle || snap.Sort != imageSecSort || snap.Total != 11 {
		t.Errorf("snapshot %q sorted %+v, total %d", snap.Title, snap.Sort, snap.Total)
	}
	if s.state.imageSecTableOf(ImageSecurity) != nil {
		t.Error("the CVEs' snapshot is served while the findings show")
	}
	trimmed := func() bool {
		for _, is := range b.Cache.Stats() {
			if is.Scope.GVR == testkit.VulnReportsGVR && is.Scope.Trim == trivyFindingsTrim && is.Handles > 0 {
				return true
			}
		}
		return false
	}
	if !trimmed() {
		t.Error("the page does not read the reports' trimmed scope")
	}
	page := func() *imageSecWorker { s.state.mu.Lock(); defer s.state.mu.Unlock(); return s.state.imageSec }
	for _, obj := range page().src.reports.List() {
		for _, v := range model.NestedSlice(obj.Object, "report", "vulnerabilities") {
			for k := range v.(map[string]any) {
				if !findingFields[k] {
					t.Errorf("%s kept %q", obj.GetName(), k)
				}
			}
		}
	}

	// The search: cleared, every finding; a change of a report reaches the page.
	s.UpdateUI(func(u *UIState) { u.Filter = "" })
	waitTable(t, s, ImageSecurityFindings, func(t *TableSnapshot) bool { return len(t.Rows) == 11 })
	src.Set(testkit.VulnReportsGVR, testkit.VulnReport("team", "Pod", "bare", "shell", "library/alpine", "3.12", [5]int64{}))
	waitTable(t, s, ImageSecurityFindings, func(t *TableSnapshot) bool { return len(t.Rows) == 9 && t.Total == 9 })

	// Switched off: no read, the page says why; on again, it reads.
	setTrivy(s, false)
	off := waitTable(t, s, ImageSecurityFindings, func(t *TableSnapshot) bool { return len(t.Rows) == 0 && t.Error != "" })
	if !strings.Contains(off.Error, "Trivy integration is off") {
		t.Errorf("off: %q", off.Error)
	}
	waitFor(t, "no read of the trimmed reports", func() bool { return !trimmed() })
	if _, _, err := s.ImageSecurity(context.Background(), "", nil, nil, ""); !errors.Is(err, ErrTrivyOff) {
		t.Errorf("an agent's read while off: %v", err)
	}
	setTrivy(s, true)
	waitTable(t, s, ImageSecurityFindings, func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 9 && t.Error == "" })

	// Left: the handles go.
	s.OpenTab("pods", false)
	waitFor(t, "the page's read released", func() bool { return page() == nil && !trimmed() })
}

// A cluster without Trivy Operator: the page reads nothing and says the reports are not served;
// an agent's read is refused.
func TestImageSecurityNotServed(t *testing.T) {
	src := testkit.ImageSecuritySource()
	src.Delete(testkit.CRDsGVR, "", "vulnerabilityreports.aquasecurity.github.io")
	s := newCRDSession(t, src)
	waitFor(t, "the definitions listed", func() bool { set := s.Backend().customSet(); return set != nil && set.synced })
	s.OpenTab(ImageSecurity, false)
	snap := waitTable(t, s, ImageSecurity, func(t *TableSnapshot) bool { return t.Error != "" })
	if snap.Error != trivyNotServed+"." || len(snap.Rows) != 0 {
		t.Errorf("not served: %q, %d rows", snap.Error, len(snap.Rows))
	}
	if _, _, err := s.ImageSecurity(context.Background(), "", nil, nil, ""); !errors.Is(err, ErrNotApplicable) {
		t.Errorf("an agent's read: %v", err)
	}
}

// The workload a report names, as the page writes and opens it.
func TestTrivyWorkloadRef(t *testing.T) {
	for _, c := range []struct {
		kind, name, ref, text string
	}{
		{"ReplicaSet", "api-5d9c4b7f8", "deployments/shop/api", "Deployment api"},
		{"ReplicaSet", "made-by-hand", "replicasets/shop/made-by-hand", "ReplicaSet made-by-hand"},
		{"ReplicaSet", "web-abc", "replicasets/shop/web-abc", "ReplicaSet web-abc"}, // a vowel: no pod template hash
		{"StatefulSet", "db", "statefulsets/shop/db", "StatefulSet db"},
		{"CronJob", "nightly", "cronjobs/shop/nightly", "CronJob nightly"},
		{"Pod", "bare", "pods/shop/bare", "Pod bare"},
		{"Cluster", "pg", trivyReportsKind + "/shop/cluster-pg-app", "Cluster pg"},
	} {
		r := model.VulnerabilityReport{Namespace: "shop", Name: strings.ToLower(c.kind) + "-" + c.name + "-app", Kind: c.kind, Workload: c.name, Container: "app"}
		ref, text := trivyWorkloadRef("", r)
		if ref.String() != c.ref || text != c.text {
			t.Errorf("%s %s: %s %q", c.kind, c.name, ref, text)
		}
	}
}

// The page's trim keeps the fields the page shows and the rest of the report.
func TestTrimVulnerabilityFindings(t *testing.T) {
	obj := testkit.VulnReport("shop", "Pod", "bare", "shell", "library/alpine", "3.12", [5]int64{0, 1},
		testkit.Vuln{ID: "CVE-2024-0003", Severity: "HIGH", Package: "golang.org/x/net", Installed: "0.9.0", Score: 7.5})
	trimVulnerabilityFindings(obj)
	v := model.NestedSlice(obj.Object, "report", "vulnerabilities")[0].(map[string]any)
	if len(v) != 6 || v["vulnerabilityID"] != "CVE-2024-0003" || v["score"] != 7.5 || v["title"] != nil {
		t.Errorf("kept %v", v)
	}
	if model.NestedString(obj.Object, "report", "artifact", "repository") != "library/alpine" || obj.GetLabels()[model.TrivyLabelContainerName] != "shell" {
		t.Error("the trim took more than the vulnerabilities' fields")
	}
	trimVulnerabilityFindings(&unstructured.Unstructured{Object: map[string]any{"report": "x"}}) // no panic on another shape
}
