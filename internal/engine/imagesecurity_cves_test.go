package engine

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// cveCells are a CVEs table's rows as "<severity> <cve> <score> <packages> | <fixed> | <images>
// <containers> <runs in>".
func cveCells(t *TableSnapshot) []string {
	out := make([]string, 0, len(t.Rows))
	for _, r := range t.Rows {
		c := r.Cells
		out = append(out, c[0].Text+" "+c[1].Text+" "+c[2].Text+" "+c[3].Text+" | "+c[4].Text+" | "+c[5].Text+" "+c[6].Text+" "+c[7].Text)
	}
	return out
}

// The CVEs table (D-new): one row per CVE id among the findings the search keeps, the worst
// severity a finding gives it, its highest score, its packages and fixed versions, how many images
// (by digest) and containers it is found in and which; the worst first; no row is an object of a
// kind.
func TestImageSecurityCVEs(t *testing.T) {
	s := imageSecSession(t, testkit.ImageSecuritySource())
	read := func(filter string) *TableSnapshot {
		t.Helper()
		_, snap, err := s.ImageSecurity(context.Background(), "", nil, &filter, ImageSecurity)
		if err != nil {
			t.Fatal(err)
		}
		return snap
	}
	all := read("")
	want := []string{
		"Critical CVE-2024-0001 9.8 curl, libcurl4 | 8.4.0, 7.74.0-1.3+deb11u7 | 2 2 shop/api/app, shop/db/postgres",
		"Critical CVE-2024-0002 9.1 golang.org/x/crypto | 0.31.0 | 1 1 shop/api/app",
		"High CVE-2024-0003 7.5 golang.org/x/net | 0.17.0 | 2 2 shop/api/app, team/bare/shell",
		"High CVE-2024-0004 7.4 openssl |  | 2 2 shop/api/app, team/bare/shell",
		"High CVE-2024-0006 7.8 libc6 |  | 1 1 shop/db/postgres",
		"Low CVE-2024-0005 — zlib1g |  | 1 1 shop/api/app",
		"Unknown CVE-2024-0007 — libgnutls30 |  | 1 1 shop/db/postgres",
	}
	if got := cveCells(all); !slices.Equal(got, want) || all.Total != 7 || all.View != ImageSecurity {
		t.Errorf("CVEs:\n%s\ntotal %d, view %q", strings.Join(got, "\n"), all.Total, all.View)
	}
	first := all.Rows[0]
	if first.Ref != (model.ObjectRef{Context: "test", Kind: ImageSecurity, Name: "CVE-2024-0001"}) || first.Cells[0].Status != model.StatusError {
		t.Errorf("first row %+v", first)
	}
	if tip := first.Cells[7].Tip; tip != "shop/api/app\nshop/db/postgres" {
		t.Errorf("Runs in's tooltip %q", tip)
	}
	for filter, n := range map[string]int{
		ImageSecurityDefaultFilter: 5,
		"ns:team":                  2, // the CVEs found in team, counted there alone
		"fixable":                  3,
		"pkg:openssl":              1,
		"bare":                     2, // the views' search: Runs in names the containers
		"cve:CVE-2024-0006":        1,
	} {
		if got := read(filter); len(got.Rows) != n {
			t.Errorf("%q: %d rows %q, want %d", filter, len(got.Rows), cveCells(got), n)
		}
	}
	if got := cveCells(read("ns:team")); got[0] != "High CVE-2024-0003 7.5 golang.org/x/net |  | 1 1 team/bare/shell" {
		t.Errorf("team's CVE-2024-0003 counted %q", got[0])
	}
}

// An image is its digest (the owner's): the same image in two workloads counts once, with two
// containers; a tag pushed again while a workload still runs the old image is two images; the
// cards and the header count alike.
func TestImageSecurityCountsByDigest(t *testing.T) {
	src := testkit.ImageSecuritySource()
	// A second Deployment running api's image, and a third running api's tag pushed again.
	src.Set(testkit.VulnReportsGVR, testkit.VulnReport("team", "ReplicaSet", "api-copy-7c9d5b6f8", "app", "shop/api", "2.4.1", [5]int64{1},
		testkit.Vuln{ID: "CVE-2024-0002", Severity: "CRITICAL", Package: "golang.org/x/crypto", Installed: "0.21.0", Fixed: "0.31.0", Score: 9.1}))
	src.Set(testkit.VulnReportsGVR, testkit.WithDigest(testkit.VulnReport("team", "ReplicaSet", "api-old-5c8b9d7f6", "app", "shop/api", "2.4.1", [5]int64{1},
		testkit.Vuln{ID: "CVE-2024-0002", Severity: "CRITICAL", Package: "golang.org/x/crypto", Installed: "0.20.0", Fixed: "0.31.0", Score: 9.1}),
		testkit.ImageDigest("shop/api:2.4.1 pushed again")))
	s := imageSecSession(t, src)
	filter := "cve:CVE-2024-0002"
	m, snap, err := s.ImageSecurity(context.Background(), "", nil, &filter, ImageSecurity)
	if err != nil {
		t.Fatal(err)
	}
	if got := cveCells(snap); len(got) != 1 || !strings.HasPrefix(got[0], "Critical CVE-2024-0002 9.1 golang.org/x/crypto | 0.31.0 | 2 3 ") {
		t.Errorf("CVE-2024-0002: %q", got)
	}
	var top []string
	for _, im := range m.TopImages {
		top = append(top, im.Image)
	}
	pushed := "shop/api:2.4.1@" + shortDigest(testkit.ImageDigest("shop/api:2.4.1 pushed again"))
	if !slices.Contains(top, pushed) || !slices.Contains(top, "shop/api:2.4.1@"+shortDigest(testkit.ImageDigest("shop/api:2.4.1"))) {
		t.Errorf("top images %q: the two images of one tag are not told apart", top)
	}
	for _, c := range m.TopCVEs {
		if c.ID == "CVE-2024-0002" && (c.Images != 2 || c.Containers != 3) {
			t.Errorf("Top CVEs counts CVE-2024-0002 in %d images, %d containers", c.Images, c.Containers)
		}
	}
	// One severity: the most containers first, not the lower id.
	critical := "sev:critical"
	_, snap, _ = s.ImageSecurity(context.Background(), "", nil, &critical, ImageSecurity)
	if got := cveCells(snap); len(got) != 2 || !strings.Contains(got[0], "CVE-2024-0002") {
		t.Errorf("Critical, the widest first: %q", got)
	}
}

// The searches a switch of the table and a click on the band carry over.
func TestImageSecurityFilters(t *testing.T) {
	for _, c := range [][4]string{
		{"sev>=high ns:shop", ImageSecurityFindings, "CVE-1", "sev>=high ns:shop cve:CVE-1"},
		{"sev>=high cve:CVE-1", ImageSecurityFindings, "CVE-2", "sev>=high cve:CVE-2"},
		{"sev>=high cve:CVE-1 x", ImageSecurity, "", "sev>=high x"},
		{"sev>=high", ImageSecurityFindings, "", "sev>=high"},
	} {
		if got := ImageSecurityFilterFor(c[0], c[1], c[2]); got != c[3] {
			t.Errorf("%q to %s with %q: %q, want %q", c[0], c[1], c[2], got, c[3])
		}
	}
	for _, c := range [][3]string{
		{"sev>=high ns:shop", "Critical", "ns:shop sev:critical"},
		{"sev:low SEV:unknown x", "Medium", "x sev:medium"},
		{"", "Low", "sev:low"},
	} {
		if got := ImageSecurityFilterSeverity(c[0], c[1]); got != c[2] {
			t.Errorf("%q + %s: %q, want %q", c[0], c[1], got, c[2])
		}
	}
}

// The page as a PDF: the window's search (the page's default while it is not open), every CVE it
// keeps, the file named after the cluster and the day.
func TestImageSecurityPDF(t *testing.T) {
	s := imageSecSession(t, testkit.ImageSecuritySource())
	doc, err := s.ImageSecurityPDF(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	day := s.Now().In(zoneOr(s.TimeFormat().Zone)).Format("2006-01-02")
	if !strings.HasPrefix(string(doc.PDF), "%PDF") || doc.CVEs != 5 || doc.Filename != "image-security-test-"+day+".pdf" {
		t.Errorf("%d CVEs in %q, %d bytes starting %q", doc.CVEs, doc.Filename, len(doc.PDF), doc.PDF[:min(len(doc.PDF), 8)])
	}
	s.OpenTab(ImageSecurity, false)
	s.UpdateUI(func(u *UIState) { u.Filter = "ns:team" })
	waitTable(t, s, ImageSecurity, func(t *TableSnapshot) bool { return t.Synced && t.Filter == "ns:team" })
	if doc, err = s.ImageSecurityPDF(context.Background(), ""); err != nil || doc.CVEs != 2 {
		t.Errorf("the page's search: %d CVEs, %v", doc.CVEs, err)
	}
	setTrivy(s, false)
	if _, err := s.ImageSecurityPDF(context.Background(), ""); !errors.Is(err, ErrTrivyOff) {
		t.Errorf("off: %v", err)
	}
}

// A pod drawer's link: the page's CVEs searched for one image, every severity; a tag ends the
// match.
func TestImageSecurityShowImage(t *testing.T) {
	s := imageSecSession(t, testkit.ImageSecuritySource())
	u, err := s.ShowImageSecurityImage("shop/api:2.4.1")
	if err != nil || u.View != ImageSecurity || u.Filter != "image:shop/api:2.4.1" {
		t.Fatalf("%v: %q %q", err, u.View, u.Filter)
	}
	waitTable(t, s, ImageSecurity, func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 5 })
	if _, err := s.ShowImageSecurityImage("a b"); err == nil {
		t.Error("an image with a blank was taken")
	}
	for _, c := range []struct {
		image, value string
		want         bool
	}{
		{"shop/api:2.4.1", "shop/api:2.4.1", true},
		{"shop/api:2.4.10", "shop/api:2.4.1", false},
		{"shop/api:2.4.1@sha256:1a2b", "shop/api:2.4.1", true},
		{"registry/shop/api:2.4.1", "shop/api:2.4.1", true},
		{"shop/api:2.4.10", "shop/api", true}, // no tag: contains
		{"Shop/API:2.4.1", "shop/api:2.4.1", true},
	} {
		if got := imageMatches(c.image, []string{c.value}); got != c.want {
			t.Errorf("%q in %q: %v", c.value, c.image, got)
		}
	}
}
