package engine

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The Image Security page's CVEs table (ImageSecurity; the findings table is
// imagesecurity_model.go's): one row per CVE id among the findings that the namespaces and the
// search keep, with how often it occurs in the cluster and where. An image counts once however many
// workloads, namespaces and replicas run it (imageIdentity: its digest, else repository and tag);
// Containers counts the workloads' containers that run it (imageSecReport.placeKey); Runs in names
// them. A row is no object: a click shows the findings table narrowed to the CVE
// (trivy.imageSecurityTable), and the rows have no checkbox and no menu.

// The CVEs table's column ids that the findings table does not have.
const (
	isColumnImages     = "images"
	isColumnContainers = "containers"
	isColumnRunsIn     = "runsIn"
)

// imageSecCVETitle is the CVEs table's heading; the others its columns' titles that the findings
// table and the report share.
const (
	imageSecCVETitle = "CVEs"
	titleSeverity    = "Severity"
	titleCVE         = "CVE"
	titleContainers  = "Containers"
)

// imageSecCVEProfile is the profile of the CVEs table. Its key is the page's, so that no kind is
// mistaken for it; its rows' references name the CVE with it as their kind.
var imageSecCVEProfile = &model.KindProfile{
	Key: ImageSecurity, Title: imageSecCVETitle,
	Columns: []model.Column{
		{ID: isColumnSeverity, Title: titleSeverity, Type: model.ColumnNumber, Width: 84},
		{ID: isColumnCVE, Title: titleCVE, Type: model.ColumnText, Width: 132},
		{ID: isColumnScore, Title: "Score", Type: model.ColumnNumber, Width: 56},
		{ID: isColumnPackage, Title: "Package", Type: model.ColumnText, Width: 150},
		{ID: isColumnFixed, Title: "Fixed", Type: model.ColumnText, Width: 120},
		{ID: isColumnImages, Title: "Images", Type: model.ColumnNumber, Width: 64},
		{ID: isColumnContainers, Title: titleContainers, Type: model.ColumnNumber, Width: 84},
		{ID: isColumnRunsIn, Title: "Runs in", Type: model.ColumnText, Width: 320},
	},
}

// imageSecCVESort is the CVEs table's order until the user sorts: the worst severity first; ties
// as the build ordered them (the most images, then the most containers, then the id).
var imageSecCVESort = SortSpec{Column: isColumnSeverity, Desc: true}

// runsInTipLines bounds the containers a Runs in cell's tooltip lists; the rest are counted.
const runsInTipLines = 30

// placeKey names the container a report is of as the CVEs table counts containers: the namespace,
// the workload as the page writes it and the container. Two reports of one Deployment's
// ReplicaSets are one container.
func (r *imageSecReport) placeKey() string {
	return r.r.Namespace + "/" + r.workload + "/" + r.r.Container
}

// placeName is the container as the Runs in cell writes it: "shop/api/server", the namespace, the
// workload's name and the container.
func (r *imageSecReport) placeName() string {
	_, name, ok := strings.Cut(r.workload, " ")
	if !ok {
		name = r.workload
	}
	return r.r.Namespace + "/" + name + "/" + r.r.Container
}

// imageSecCVE is one CVE id with its findings among those the namespaces and the search keep.
type imageSecCVE struct {
	id       string
	sev      int // the worst a finding gives it
	score    float64
	hasScore bool
	pkgs     []string // the packages, in the order the findings name them
	fixed    []string // the fixed versions, likewise; none: no fix
	images   map[string]struct{}
	places   []imageSecPlace
}

// imageSecPlace is a container a CVE is found in: its first report, and every finding of the CVE
// in the container's reports (one per package and version).
type imageSecPlace struct {
	key      string
	report   int
	findings []int
}

// collectCVEs groups the findings of the namespaces (empty: all) that the query keeps by their
// CVE id, the worst first: by severity, then the most images, the most containers, the id. The
// places of a CVE come in the build's order of the reports (namespace, workload, container).
func collectCVEs(fb *imageSecBuild, nsSet map[string]struct{}, q *findingQuery) []*imageSecCVE {
	byID := map[string]*imageSecCVE{}
	var out []*imageSecCVE
	placeAt := map[string]map[string]int{} // CVE id → place key → index in its places
	for i := range fb.findings {
		f := &fb.findings[i]
		r := &fb.reports[f.report]
		if len(nsSet) > 0 {
			if _, in := nsSet[r.r.Namespace]; !in {
				continue
			}
		}
		if !q.keeps(f, r) {
			continue
		}
		c := byID[f.cve]
		if c == nil {
			c = &imageSecCVE{id: f.cve, sev: f.sev, images: map[string]struct{}{}}
			byID[f.cve] = c
			placeAt[f.cve] = map[string]int{}
			out = append(out, c)
		}
		c.sev = max(c.sev, f.sev)
		if f.hasScore && (!c.hasScore || f.score > c.score) {
			c.score, c.hasScore = f.score, true
		}
		if !slices.Contains(c.pkgs, f.pkg) {
			c.pkgs = append(c.pkgs, f.pkg)
		}
		if f.fixed != "" && !slices.Contains(c.fixed, f.fixed) {
			c.fixed = append(c.fixed, f.fixed)
		}
		c.images[r.id] = struct{}{}
		key := r.placeKey()
		at, ok := placeAt[f.cve][key]
		if !ok {
			at = len(c.places)
			placeAt[f.cve][key] = at
			c.places = append(c.places, imageSecPlace{key: key, report: f.report})
		}
		c.places[at].findings = append(c.places[at].findings, i)
	}
	slices.SortFunc(out, func(a, b *imageSecCVE) int {
		return cmp.Or(cmp.Compare(b.sev, a.sev), cmp.Compare(len(b.images), len(a.images)), cmp.Compare(len(b.places), len(a.places)),
			cmp.Compare(a.id, b.id))
	})
	return out
}

// cveRow is a CVE as a row of the CVEs table. Only the Severity cell carries a status (D-180).
func cveRow(context string, c *imageSecCVE, fb *imageSecBuild) model.Row {
	score := model.Cell{Text: noScore, Num: -1}
	if c.hasScore {
		score = model.Cell{Text: strconv.FormatFloat(c.score, 'f', 1, 64), Num: int64(c.score*10 + 0.5)}
	}
	names := make([]string, len(c.places))
	for i, pl := range c.places {
		names[i] = fb.reports[pl.report].placeName()
	}
	tip := names[:min(len(names), runsInTipLines)]
	tipText := strings.Join(tip, "\n")
	if more := len(names) - len(tip); more > 0 {
		tipText += "\n+" + strconv.Itoa(more) + " more"
	}
	return model.Row{
		Ref: model.ObjectRef{Context: context, Kind: ImageSecurity, Name: c.id},
		Cells: []model.Cell{
			{Text: trivySeverities[c.sev], Num: int64(c.sev), Status: severityStatus(c.sev)},
			{Text: c.id},
			score,
			{Text: strings.Join(c.pkgs, ", ")},
			{Text: strings.Join(c.fixed, ", ")},
			{Text: strconv.Itoa(len(c.images)), Num: int64(len(c.images))},
			{Text: strconv.Itoa(len(c.places)), Num: int64(len(c.places))},
			{Text: strings.Join(names, ", "), Tip: tipText},
		},
	}
}

// imageSecCVETable is the CVEs table: the CVE ids of the findings in the namespaces that the
// search's tokens keep, those whose row the search's words match, sorted. Total counts the CVE ids
// of every finding.
func imageSecCVETable(context string, fb *imageSecBuild, p viewParams) *TableSnapshot {
	q := parseFindingQuery(p.filter)
	textParams := p
	textParams.filter = q.text
	match, filterErr := textParams.matcher()
	if filterErr == "" {
		filterErr = q.err
	}
	rows := []model.Row{}
	for _, c := range collectCVEs(fb, setOf(p.namespaces), &q) {
		row := cveRow(context, c, fb)
		if !match.All() && !match.Match(row) {
			continue
		}
		rows = append(rows, row)
	}
	sortImageSecRows(imageSecCVEProfile, rows, p.sort)
	snap := &TableSnapshot{
		View: ImageSecurity, Context: context, Title: ImageSecurityTitle, Namespaced: true,
		Columns: ColumnsShown(imageSecCVEProfile.Columns, p.hidden, p.hiddenSet), Order: ColumnOrder(imageSecCVEProfile.Columns, p.order),
		Rows: rows, Sort: p.sort,
		Namespaces: slices.Clone(p.namespaces), Filter: p.filter, FilterOptions: p.filterOpts, FilterError: filterErr,
		Total: fb.cves, Synced: fb.synced, Error: fb.errText,
	}
	if fb.reason != "" {
		snap.Error = fb.reason
	}
	return snap
}

// imageSecProfileOf is the profile of the page's table a view names; nil for another view.
func imageSecProfileOf(view string) *model.KindProfile {
	switch view {
	case ImageSecurity:
		return imageSecCVEProfile
	case ImageSecurityFindings:
		return imageSecProfile
	}
	return nil
}

// ImageSecurityFilterFor is the search a switch of the page's table carries over (ui
// trivy.imageSecurityTable): to the findings with a CVE, the search with that CVE's token in place
// of one it had; to the CVEs, the search without its cve: tokens, which would leave one row.
func ImageSecurityFilterFor(filter, view, cve string) string {
	if view == ImageSecurityFindings {
		if cve == "" {
			return filter
		}
		return ImageSecurityFilterWith(filter, tokenCVE+cve)
	}
	var out []string
	for _, word := range strings.Fields(filter) {
		if !strings.HasPrefix(strings.ToLower(word), tokenCVE) {
			out = append(out, word)
		}
	}
	return strings.Join(out, " ")
}

// ShowImageSecurityTable shows the Image Security page's other table in the active tab, as a step
// of its history (Back returns): view ImageSecurity for the CVEs, ImageSecurityFindings for the
// findings, with cve the findings of that CVE. The search carries over (ImageSecurityFilterFor);
// the table's own sort comes back. It fails unless the page is the open view.
func (s *Session) ShowImageSecurityTable(view, cve string) (UIState, error) {
	if !IsImageSecurity(view) {
		return UIState{}, fmt.Errorf("unknown table %q of the Image Security page", view)
	}
	sort := s.ViewSort(view)
	var err error
	st := s.UpdateUI(func(u *UIState) {
		if !IsImageSecurity(u.View) {
			err = errors.New("the Image Security page is not the open view: open it with ui_openView trivy/image-security")
			return
		}
		u.Filter = ImageSecurityFilterFor(u.Filter, view, cve)
		if u.View != view {
			u.MarkStep()
			u.View, u.Sort, u.Drawer = view, sort, nil
			u.SetStates(nil)
		}
	})
	return st, err
}

// ImageSecurityFilterSeverity is a search with its severity tokens (sev>=, sev:) replaced by one
// that keeps a severity ("Critical"): a click on the severity band's segment.
func ImageSecurityFilterSeverity(filter, severity string) string {
	var out []string
	for _, word := range strings.Fields(filter) {
		lower := strings.ToLower(word)
		if !strings.HasPrefix(lower, tokenSev) && !strings.HasPrefix(lower, tokenSevAtLeast) {
			out = append(out, word)
		}
	}
	return strings.Join(append(out, tokenSev+strings.ToLower(severity)), " ")
}

// ShowImageSecurityImage opens the Image Security page's CVEs (as ui.openView does) with its search
// on one image, every severity: "image:shop/api:2.4.1", the image as the page names it. A pod
// drawer's grade card links here.
func (s *Session) ShowImageSecurityImage(image string) (UIState, error) {
	image = strings.TrimSpace(image)
	if image == "" || strings.ContainsAny(image, " \t\n") {
		return UIState{}, fmt.Errorf("an image is a name without blanks, as the page writes it (shop/api:2.4.1), not %q", image)
	}
	s.OpenTab(ImageSecurity, false)
	return s.UpdateUI(func(u *UIState) {
		if u.View == ImageSecurity {
			u.Filter = tokenImage + image
		}
	}), nil
}
