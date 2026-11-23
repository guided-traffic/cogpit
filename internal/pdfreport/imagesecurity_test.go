package pdfreport

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var generated = time.Date(2026, 11, 23, 14, 5, 0, 0, time.UTC)

// sample is a report like a small cluster's: every section filled.
func sample() ImageSecurity {
	r := ImageSecurity{
		Cluster:    "prod-eu-1",
		Generated:  generated,
		TimeLayout: "2006-01-02 15:04",
		Namespaces: "All namespaces",
		Search:     "",
		Header:     "93 images running · 81 scanned · 12 not scanned · newest scan 6m ago",
		Severities: []SeverityCount{
			{"Critical", 14, LevelCritical}, {"High", 61, LevelHigh}, {"Medium", 118, LevelMedium},
			{"Low", 52, LevelLow}, {"Unknown", 3, LevelUnknown},
		},
		CVEsTotal: 248, Fixable: 131,
		TopImagesColumns: []string{"Grade", "Image", "Critical", "High"},
		TopImages: []TopRow{
			{Cells: []string{"E", "search/billing-sync:3.5", "10", "11"}, Level: LevelCritical},
			{Cells: []string{"E", "shop/checkout-api:2.14.0", "6", "19"}, Level: LevelCritical},
			{Cells: []string{"D", "library/nginx:1.25.3", "0", "9"}, Level: LevelHigh},
			{Cells: []string{"C", "ghcr.io/acme/very-long-registry-path/component-with-a-long-name:1.0.0-rc.1", "0", "4"}},
			{Cells: []string{"B", "quay.io/prometheus/node-exporter:v1.8.2", "0", "1"}},
		},
		TopCVEsColumns: []string{"CVE", "Severity", "Images"},
		TopCVEs: []TopRow{
			{Cells: []string{"CVE-2024-45337", "Critical", "12"}, Level: LevelCritical, LevelCell: 1},
			{Cells: []string{"CVE-2023-44487", "Critical", "9"}, Level: LevelCritical, LevelCell: 1},
			{Cells: []string{"CVE-2024-6119", "High", "21"}, Level: LevelHigh, LevelCell: 1},
			{Cells: []string{"GHSA-v778-237x-gjrc", "High", "4"}, Level: LevelHigh, LevelCell: 1},
			{Cells: []string{"CVE-2024-2511", "Medium", "30"}, Level: LevelMedium, LevelCell: 1},
		},
		TopNamespacesColumns: []string{"Grade", "Namespace", "Critical", "High"},
		TopNamespaces: []TopRow{
			{Cells: []string{"E", "search", "10", "14"}, Level: LevelCritical},
			{Cells: []string{"E", "shop", "6", "22"}, Level: LevelCritical},
			{Cells: []string{"D", "ingress-nginx", "0", "9"}, Level: LevelHigh},
			{Cells: []string{"C", "monitoring", "0", "5"}},
		},
		Trend: &Trend{Title: "Findings over time (Prometheus, 30 days)", Note: "counts a CVE once per image"},
	}
	for d := range 30 {
		r.Trend.Points = append(r.Trend.Points, TrendPoint{
			At:       generated.AddDate(0, 0, d-29),
			Critical: 20 - d/3, High: 70 - d/2 + d%4, Medium: 120 + d%7, Low: 50 + d%3, Unknown: d % 2,
		})
	}
	r.CVEs = cves(60, 6)
	r.CVEs[0].RunsIn[0].Namespace = "müller-prüfung"
	return r
}

// cves makes n CVE rows with up to places containers each.
func cves(n, places int) []CVE {
	sev := []struct {
		name  string
		level Level
	}{{"Critical", LevelCritical}, {"High", LevelHigh}, {"Medium", LevelMedium}, {"Low", LevelLow}}
	out := make([]CVE, 0, n)
	for i := range n {
		s := sev[min(i*len(sev)/max(n, 1), len(sev)-1)]
		c := CVE{
			ID: fmt.Sprintf("CVE-2024-%05d", 1000+i), Severity: s.name, Level: s.level,
			Score: fmt.Sprintf("%.1f", 9.8-float64(i%40)/5), Packages: "libssl3, openssl",
			Fixed: "3.0.15-1~deb12u1",
		}
		if i%5 == 3 {
			c.Fixed, c.Score = "", "—"
			c.Packages = "linux-libc-dev, linux-headers-amd64, linux-image-6.1.0-28-cloud-amd64, firmware-misc-nonfree"
		}
		k := 1 + i%places
		for j := range k {
			p := Place{
				Namespace: []string{"shop", "billing", "search", "monitoring"}[j%4],
				Workload:  fmt.Sprintf("Deployment/service-%02d", j),
				Container: fmt.Sprintf("app-%d", j),
				Image:     fmt.Sprintf("registry.example.com/team/service-%02d:1.%d.%d", j, i%9, j),
				Installed: "3.0.11-1~deb12u2",
				Fixed:     c.Fixed,
			}
			c.RunsIn = append(c.RunsIn, p)
		}
		c.Containers = k
		c.Images = max(1, k-1)
		out = append(out, c)
	}
	return out
}

func TestRenderSmall(t *testing.T) {
	r := sample()
	b, err := Render(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b, []byte("%PDF-")) {
		t.Fatalf("not a PDF: %q", b[:min(len(b), 16)])
	}
	d := readPDF(t, b)
	if n := d.pages(); n < 2 || n > 10 {
		t.Errorf("pages = %d, want 2 to 10", n)
	}
	for _, want := range []string{
		"Image Security", "prod-eu-1", "CVE-2024-01000", "app-0", "müller-prüfung",
		"93 images running · 81 scanned", "248 CVEs · 131 fixable",
		"3.0.11-1~deb12u2 → 3.0.15-1~deb12u1", "page 1 of ", "Findings over time (Prometheus, 30 days)",
		"no fix", "—", "Top namespaces",
	} {
		if !d.contains(want) {
			t.Errorf("the document lacks %q", want)
		}
	}
	writeSample(t, "", b)
}

// writeSample writes the document where COGPIT_PDF_OUT names (a .pdf path; a suffix goes before
// its extension), to look at it.
func writeSample(t *testing.T, suffix string, b []byte) {
	out := os.Getenv("COGPIT_PDF_OUT")
	if out == "" {
		return
	}
	out = strings.TrimSuffix(out, ".pdf") + suffix + ".pdf"
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRenderEveryPageHasItsFooter(t *testing.T) {
	r := sample()
	r.CVEs = cves(200, 8)
	b, err := Render(r)
	if err != nil {
		t.Fatal(err)
	}
	d := readPDF(t, b)
	n := d.pages()
	for p := 1; p <= n; p++ {
		if !d.contains(fmt.Sprintf("page %d of %d", p, n)) {
			t.Errorf("page %d lacks its footer", p)
		}
	}
	// A CVE whose containers run over a page break names itself again.
	if !d.contains("(continued)") {
		t.Log("no CVE ran over a page break")
	}
}

func TestRenderContinuedCVE(t *testing.T) {
	r := ImageSecurity{Cluster: "c", Generated: generated, CVEs: cves(1, 1)}
	c := &r.CVEs[0]
	for i := range 80 {
		c.RunsIn = append(c.RunsIn, Place{Namespace: "ns", Workload: "w", Container: fmt.Sprintf("c-%03d", i), Image: "i", Installed: "1"})
	}
	b, err := Render(r)
	if err != nil {
		t.Fatal(err)
	}
	d := readPDF(t, b)
	if d.pages() < 2 {
		t.Fatalf("pages = %d, want 2 or more", d.pages())
	}
	for _, want := range []string{c.ID + " (continued)", "c-079", "Severity"} {
		if !d.contains(want) {
			t.Errorf("the document lacks %q", want)
		}
	}
	writeSample(t, "-continued", b)
}

func TestRenderEmptyAndSparse(t *testing.T) {
	for name, r := range map[string]ImageSecurity{
		"zero":         {},
		"no trend":     {Cluster: "c", Severities: []SeverityCount{{"Critical", 0, LevelCritical}}},
		"empty trend":  {Trend: &Trend{Title: "Findings over time", Note: "Prometheus holds no samples yet"}},
		"single point": {Trend: &Trend{Points: []TrendPoint{{At: generated, High: 3}}}},
		"short span": {Trend: &Trend{Points: []TrendPoint{
			{At: generated, High: 3}, {At: generated.Add(time.Hour), Critical: 1, High: 2},
		}}},
		"ragged rows": {
			TopImagesColumns: []string{"A"},
			TopImages:        []TopRow{{Cells: []string{"x", "y", "z", "w"}, Level: LevelCritical, LevelCell: 9}},
			TopCVEs:          []TopRow{{}},
			CVEs:             []CVE{{}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			b, err := Render(r)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(b, []byte("%PDF-")) {
				t.Fatal("not a PDF")
			}
		})
	}
}

func TestFitNeverOverflows(t *testing.T) {
	w, err := newWriter(pdfInfoForTest())
	if err != nil {
		t.Fatal(err)
	}
	huge := strings.Repeat("registry.example.com/ä/→/", 20000)
	for _, s := range []string{
		"", " ", "a", huge, "\x00\t\n\r", "\xff\xfe invalid", "漢字とかな", "😀😀😀",
		strings.Repeat("W", 500), strings.Repeat("·", 3000),
	} {
		for _, maxW := range []float64{-1, 0, 0.5, 2, 10, 277} {
			for _, fam := range []string{regular, bold} {
				got := w.fit(s, fam, 8, maxW)
				if gw := w.faces[fam].width(got, 8); gw > maxW+1e-9 && got != "" {
					t.Fatalf("fit(%.20q, %v) = %.20q, %.2f mm wide", s, maxW, got, gw)
				}
				if got != "" && got != s && !strings.HasSuffix(got, ellipsis) && len(s) <= maxRunes {
					if cleaned, _ := w.clean(s, fam); cleaned != got {
						t.Fatalf("fit(%.20q, %v) = %.20q: cut without an ellipsis", s, maxW, got)
					}
				}
			}
		}
	}
	// Drawing the same values does not fail either.
	w.newPage()
	for _, s := range []string{huge, "\x00", "漢字", "😀"} {
		w.text(s, margin, margin, 100, 5, regular, 8, colText, left)
	}
	if _, err := w.bytes(); err != nil {
		t.Fatal(err)
	}
}

func TestFontsHaveTheReportsSymbols(t *testing.T) {
	w, err := newWriter(pdfInfoForTest())
	if err != nil {
		t.Fatal(err)
	}
	for _, fam := range []string{regular, bold} {
		for _, r := range "·→—…“”äöüÄÖÜß~" {
			if !w.faces[fam].has(r) {
				t.Errorf("%s lacks %q", fam, r)
			}
		}
	}
	// A glyph the fonts lack is drawn as "?".
	if got, _ := w.clean("a漢b", regular); got != "a?b" {
		t.Errorf("clean = %q", got)
	}
}

func TestNiceCeil(t *testing.T) {
	for in, want := range map[int]int{0: 2, 1: 2, 2: 2, 3: 4, 7: 8, 10: 10, 11: 12, 13: 20, 99: 100, 199: 200, 201: 250, 260: 300, 1001: 1200} {
		if got := niceCeil(in); got != want {
			t.Errorf("niceCeil(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestAxisLayout(t *testing.T) {
	days := []TrendPoint{{At: generated}, {At: generated.AddDate(0, 0, 3)}}
	hours := []TrendPoint{{At: generated}, {At: generated.Add(3 * time.Hour)}}
	for _, c := range []struct {
		layout string
		pts    []TrendPoint
		want   string
	}{
		{"2006-01-02 15:04", days, "2006-01-02"},
		{"02.01.2006, 15:04", days, "02.01.2006"},
		{"Jan 2, 2006 3:04 PM", days, "Jan 2, 2006"},
		{"2006-01-02 15:04", hours, "2006-01-02 15:04"},
		{"15:04", days, "2006-01-02"},
	} {
		if got := axisLayout(c.layout, c.pts); got != c.want {
			t.Errorf("axisLayout(%q) = %q, want %q", c.layout, got, c.want)
		}
	}
}

// large is the report of a big cluster: 2,000 CVEs, each in 20 containers.
func large() ImageSecurity {
	r := sample()
	r.CVEs = cves(2000, 20)
	for i := range r.CVEs {
		c := &r.CVEs[i]
		k := len(c.RunsIn)
		for j := k; j < 20; j++ {
			c.RunsIn = append(c.RunsIn, c.RunsIn[j%k])
		}
		c.Containers = 20
	}
	return r
}

func TestRenderLarge(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("renders 40,000 container lines: not under -short or the race detector")
	}
	r := large()
	start := time.Now()
	b, err := Render(r)
	if err != nil {
		t.Fatal(err)
	}
	took := time.Since(start)
	d := readPDF(t, b)
	n := d.pages()
	// 2,000 CVE rows and 40,000 container lines at 4.1 mm on 174 mm a page: about 1,000 pages.
	if n < 800 || n > 1500 {
		t.Errorf("pages = %d, want about 1,000", n)
	}
	last := r.CVEs[len(r.CVEs)-1].ID
	if !d.contains(last) || !d.contains(fmt.Sprintf("page %d of %d", n, n)) {
		t.Errorf("the document lacks the last CVE %s or the last footer", last)
	}
	t.Logf("%d CVEs, %d container lines: %d pages, %.1f MB, %v", len(r.CVEs), 20*len(r.CVEs), n, float64(len(b))/1e6, took)
}

func BenchmarkRenderLarge(b *testing.B) {
	r := large()
	for b.Loop() {
		if _, err := Render(r); err != nil {
			b.Fatal(err)
		}
	}
}
