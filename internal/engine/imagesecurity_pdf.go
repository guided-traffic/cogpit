package engine

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/pdfreport"
)

// The Image Security page as a PDF (D-178, the owner's): a text report of what the page shows for
// the window's namespaces and search: the header line, the severity band, the three cards, the
// findings curve where Prometheus has one, then every CVE the search keeps, each with all the
// containers it runs in. pdfreport draws it; this file fills its input from the page's reading
// (imageSecReading), the cache only.

// ImageSecurityPDF is a rendered report: the document, the file name to suggest, and how many CVEs
// it lists.
type ImageSecurityPDF struct {
	PDF      []byte
	Filename string
	CVEs     int
}

// ImageSecurityPDF renders the Image Security page of a context ("" the shown one) for the
// window's namespaces and the page's search as the window has them (the default search when the
// page is not open), its CVEs in the CVEs table's order. It fails as ImageSecurity does.
func (s *Session) ImageSecurityPDF(ctx context.Context, contextID string) (*ImageSecurityPDF, error) {
	b, err := s.TargetBackend(contextID)
	if err != nil {
		return nil, err
	}
	u := s.UIState()
	if !u.IntegrationOn(b.Context, IntegrationTrivy) {
		return nil, ErrTrivyOff
	}
	fb, err := s.imageSecReading(ctx, b, &u)
	if err != nil {
		return nil, err
	}
	namespaces := s.windowNamespaces(b)
	params := viewParams{namespaces: namespaces, filter: ImageSecurityDefaultFilter, sort: imageSecCVESort}
	if b.Context == u.Context && IsImageSecurity(u.View) {
		params.filter, params.filterOpts = u.Filter, u.FilterOptions
		if u.View == ImageSecurity {
			params.sort = u.Sort
		}
	}
	m := imageSecModel(fb, namespaces)
	if m.Error != "" && m.Reports == 0 {
		return nil, errorString(m.Error)
	}
	now := s.Now()
	tf := s.TimeFormat()
	cs, _ := s.ClusterSettingsOf(b.Context)
	cluster := ClusterName(b.Context, cs)
	r := pdfreport.ImageSecurity{
		Cluster: cluster, Generated: now.In(zoneOr(tf.Zone)), TimeLayout: tf.Layout.OrDefault().Date + " " + tf.Layout.OrDefault().Time,
		Namespaces: namespacesText(namespaces), Search: params.filter, Header: m.HeadLine(now),
		CVEsTotal: m.CVEs, Fixable: m.Fixable,
		TopImagesColumns:     []string{"Grade", isTitleImage, trivySeverities[sevCritical], trivySeverities[sevHigh]},
		TopCVEsColumns:       []string{titleCVE, titleSeverity, "Images", titleContainers},
		TopNamespacesColumns: []string{"Grade", titleNamespace, trivySeverities[sevCritical], trivySeverities[sevHigh]},
	}
	for _, sv := range m.Severities {
		r.Severities = append(r.Severities, pdfreport.SeverityCount{Name: sv.Severity, Count: sv.Count, Level: pdfLevel(sv.Severity)})
	}
	for _, im := range m.TopImages {
		r.TopImages = append(r.TopImages, pdfreport.TopRow{Cells: []string{im.Grade.String(), im.Image, itoa64(im.Critical), itoa64(im.High)}, Level: gradeLevel(im.Grade)})
	}
	for _, c := range m.TopCVEs {
		r.TopCVEs = append(r.TopCVEs, pdfreport.TopRow{Cells: []string{c.ID, c.Severity, strconv.Itoa(c.Images), strconv.Itoa(c.Containers)},
			Level: pdfLevel(c.Severity), LevelCell: 1})
	}
	for _, n := range m.TopNamespaces {
		r.TopNamespaces = append(r.TopNamespaces, pdfreport.TopRow{Cells: []string{n.Grade.String(), n.Namespace, strconv.Itoa(n.Critical), strconv.Itoa(n.High)},
			Level: gradeLevel(n.Grade)})
	}
	if t := s.imageSecurityTrend(b, u); t != nil {
		r.Trend = pdfTrend(t)
	}
	r.CVEs = pdfCVEs(b.Context, fb, params)
	doc, err := pdfreport.Render(r)
	if err != nil {
		return nil, err
	}
	name := "image-security-" + fileWord(cluster) + "-" + now.In(zoneOr(tf.Zone)).Format("2006-01-02") + ".pdf"
	return &ImageSecurityPDF{PDF: doc, Filename: name, CVEs: len(r.CVEs)}, nil
}

// pdfCVEs are the CVEs table's rows the search keeps, in its order, each with every container it
// is found in.
func pdfCVEs(context string, fb *imageSecBuild, p viewParams) []pdfreport.CVE {
	q := parseFindingQuery(p.filter)
	byID := map[string]*imageSecCVE{}
	for _, c := range collectCVEs(fb, setOf(p.namespaces), &q) {
		byID[c.id] = c
	}
	snap := imageSecCVETable(context, fb, p)
	out := make([]pdfreport.CVE, 0, len(snap.Rows))
	for _, row := range snap.Rows {
		c := byID[row.Ref.Name]
		if c == nil {
			continue
		}
		e := pdfreport.CVE{ID: c.id, Severity: trivySeverities[c.sev], Level: pdfLevel(trivySeverities[c.sev]), Score: row.Cells[2].Text,
			Packages: strings.Join(c.pkgs, ", "), Fixed: strings.Join(c.fixed, ", "), Images: len(c.images), Containers: len(c.places)}
		for _, pl := range c.places {
			r := &fb.reports[pl.report]
			installed, fixed := placeVersions(fb, pl)
			e.RunsIn = append(e.RunsIn, pdfreport.Place{Namespace: r.r.Namespace, Workload: r.workload, Container: r.r.Container, Image: r.image,
				Installed: installed, Fixed: fixed})
		}
		out = append(out, e)
	}
	return out
}

// placeVersions are a container's installed and fixed versions of a CVE's packages, each distinct
// version once, joined.
func placeVersions(fb *imageSecBuild, pl imageSecPlace) (installed, fixed string) {
	var ins, fix []string
	for _, i := range pl.findings {
		f := &fb.findings[i]
		if f.installed != "" && !slices.Contains(ins, f.installed) {
			ins = append(ins, f.installed)
		}
		if f.fixed != "" && !slices.Contains(fix, f.fixed) {
			fix = append(fix, f.fixed)
		}
	}
	return strings.Join(ins, ", "), strings.Join(fix, ", ")
}

// HeadLine is the line beside the page's title: "150 images running · 142 scanned · 8 not
// scanned · newest scan 2h ago"; the running images only while the page reads the pods; "" while
// it reads nothing.
func (m *ImageSecurityModel) HeadLine(now time.Time) string {
	if m.Error != "" && m.Reports == 0 {
		return ""
	}
	var parts []string
	if m.PodsRead {
		noun := " images running"
		if m.Running == 1 {
			noun = " image running"
		}
		parts = append(parts, model.FormatCount(int64(m.Running))+noun, model.FormatCount(int64(m.Scanned))+" scanned",
			model.FormatCount(int64(m.NotScanned))+" not scanned")
	}
	if !m.NewestScan.IsZero() {
		parts = append(parts, "newest scan "+model.FormatAge(now.Sub(m.NewestScan))+" ago")
	}
	return strings.Join(parts, " · ")
}

// pdfTrend is the page's findings curve as the report draws it.
func pdfTrend(t *ImageSecurityTrend) *pdfreport.Trend {
	out := &pdfreport.Trend{Title: "Findings over time (" + t.Source + ", " + strconv.Itoa(t.Days) + " days)", Note: t.Note}
	if t.Note == "" {
		out.Note = "a CVE counts once per image"
	}
	for _, p := range t.Points {
		out.Points = append(out.Points, pdfreport.TrendPoint{At: p.At, Critical: p.Critical, High: p.High, Medium: p.Medium, Low: p.Low, Unknown: p.Unknown})
	}
	return out
}

// pdfLevel is a severity word's level in the report.
func pdfLevel(sev string) pdfreport.Level {
	switch sev {
	case trivySeverities[sevCritical]:
		return pdfreport.LevelCritical
	case trivySeverities[sevHigh]:
		return pdfreport.LevelHigh
	case trivySeverities[sevMedium]:
		return pdfreport.LevelMedium
	case trivySeverities[sevLow]:
		return pdfreport.LevelLow
	}
	return pdfreport.LevelUnknown
}

// gradeLevel colours a grade in the report as the page does (D-180): E red, D amber, the rest
// plain.
func gradeLevel(g model.ImageGrade) pdfreport.Level {
	switch g {
	case model.GradeE:
		return pdfreport.LevelCritical
	case model.GradeD:
		return pdfreport.LevelHigh
	}
	return pdfreport.LevelNone
}

// namespacesText names the namespaces the report covers.
func namespacesText(namespaces []string) string {
	if len(namespaces) == 0 {
		return "All namespaces"
	}
	return strings.Join(namespaces, ", ")
}

// fileWord is a name as a file name's word: letters, digits, dots, dashes and underscores, every
// other run one underscore.
func fileWord(s string) string {
	var b strings.Builder
	under := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
			under = false
		case !under:
			b.WriteByte('_')
			under = true
		}
	}
	return strings.Trim(b.String(), "_")
}

func zoneOr(z *time.Location) *time.Location {
	if z == nil {
		return time.UTC
	}
	return z
}

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }

// errorString is a model's error text as an error.
type errorString string

func (e errorString) Error() string { return string(e) }
