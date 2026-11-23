package engine

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The Image Security page's model (imagesecurity.go has its worker and its read): what Trivy
// Operator's VulnerabilityReports say about the images the cluster runs. The header counts the
// images of the running pods and those a report covers; four cards summarise the findings of the
// selected namespaces (the CVEs by severity, the worst images, the worst CVEs, the worst
// namespaces); the table lists every finding, one row per CVE, workload and container, narrowed by
// the page's search (plain text and the tokens parseFindingQuery reads). Everything comes from the
// cache.

// Severities, Trivy's words as the page writes them, the worst last: the index is the level that
// sev>= compares.
var trivySeverities = [...]string{"Unknown", "Low", "Medium", "High", "Critical"}

// The severity levels the page's colours and order name.
const (
	sevUnknown = iota
	sevLow
	sevMedium
	sevHigh
	sevCritical
)

// severityLevel reads a severity word, any case; ok is false for another word.
func severityLevel(word string) (int, bool) {
	for i, s := range trivySeverities {
		if strings.EqualFold(word, s) {
			return i, true
		}
	}
	return sevUnknown, false
}

// severityStatus is a severity's status token, the colour the page gives it (D-calm): Critical
// the error's, High the warning's, the others none.
func severityStatus(level int) model.Status {
	switch level {
	case sevCritical:
		return model.StatusError
	case sevHigh:
		return model.StatusWarning
	}
	return model.StatusNone
}

// imageSecFinding is one entry of a report's vulnerability list.
type imageSecFinding struct {
	report    int // the index of its report in imageSecBuild.reports
	cve       string
	sev       int
	score     float64
	hasScore  bool
	pkg       string
	installed string
	fixed     string // empty: no fix
}

// imageSecReport is one report as the page reads it: the workload and container it is of, the
// image, and where its findings are in imageSecBuild.findings.
type imageSecReport struct {
	r        model.VulnerabilityReport
	ref      model.ObjectRef // the workload (trivyWorkloadRef); the report itself for a kind Cogpit does not open
	workload string          // "Deployment api"
	image    string          // model.VulnerabilityReport.Image: "library/nginx:1.21", its digest after it while another image has the same name (imageSecNames)
	id       string          // the image's identity (imageIdentity)
	from, to int             // its findings: findings[from:to]
}

// imageSecPod is a running pod's images as the header counts them: each image's key
// (imageKey) and whether a report covers it.
type imageSecPod struct {
	namespace string
	images    []imageSecPodImage
}

type imageSecPodImage struct {
	key     string
	scanned bool
}

// imageSecBuild is one reading of the page's reports and pods, for the model and the table of
// any namespaces and search.
type imageSecBuild struct {
	reports  []imageSecReport // by namespace, workload, container
	findings []imageSecFinding
	cves     int // the distinct CVE ids of all findings
	pods     []imageSecPod
	// podsRead is false when the page reads no pods (the user may not list them): the header then
	// counts no running images.
	podsRead bool
	synced   bool
	errText  string
	// reason says why the page reads nothing (the integration off, the reports not served); "" when
	// it reads.
	reason string
}

// newImageSecBuild reads report objects (the page's trimmed scope, trivyFindingsTrim) and, with
// podsRead, the pods.
func newImageSecBuild(context string, reports, pods []*unstructured.Unstructured, podsRead bool) *imageSecBuild {
	fb := &imageSecBuild{podsRead: podsRead}
	parsed := make([]imageSecReport, 0, len(reports))
	lists := make([][]any, 0, len(reports))
	for _, obj := range reports {
		r, ok := model.ParseVulnerabilityReport(obj)
		if !ok {
			continue
		}
		ref, workload := trivyWorkloadRef(context, r)
		parsed = append(parsed, imageSecReport{r: r, ref: ref, workload: workload, image: r.Image(), id: imageIdentity(r)})
		lists = append(lists, model.NestedSlice(obj.Object, "report", "vulnerabilities"))
	}
	imageSecNames(parsed)
	order := make([]int, len(parsed))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int {
		x, y := &parsed[a], &parsed[b]
		return cmp.Or(cmp.Compare(x.r.Namespace, y.r.Namespace), cmp.Compare(x.workload, y.workload),
			cmp.Compare(x.r.Container, y.r.Container), cmp.Compare(x.r.Name, y.r.Name))
	})
	cves := map[string]struct{}{}
	for _, i := range order {
		rep := parsed[i]
		rep.from = len(fb.findings)
		idx := len(fb.reports)
		for _, v := range lists[i] {
			if f, ok := parseFinding(v); ok {
				f.report = idx
				fb.findings = append(fb.findings, f)
				cves[f.cve] = struct{}{}
			}
		}
		rep.to = len(fb.findings)
		own := fb.findings[rep.from:rep.to]
		slices.SortFunc(own, func(a, b imageSecFinding) int {
			return cmp.Or(cmp.Compare(b.sev, a.sev), cmp.Compare(a.cve, b.cve), cmp.Compare(a.pkg, b.pkg), cmp.Compare(a.installed, b.installed))
		})
		fb.reports = append(fb.reports, rep)
	}
	fb.cves = len(cves)
	if podsRead {
		idx, _ := newTrivyReportIndex(reports)
		fb.pods = runningPodImages(idx, pods)
	}
	return fb
}

// parseFinding reads one entry of report.vulnerabilities; ok is false for one without an id.
func parseFinding(v any) (imageSecFinding, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return imageSecFinding{}, false
	}
	f := imageSecFinding{
		cve: model.NestedString(m, "vulnerabilityID"), pkg: model.NestedString(m, "resource"),
		installed: model.NestedString(m, "installedVersion"), fixed: strings.TrimSpace(model.NestedString(m, "fixedVersion")),
	}
	if f.cve == "" {
		return imageSecFinding{}, false
	}
	f.sev, _ = severityLevel(model.NestedString(m, "severity"))
	switch s := m["score"].(type) {
	case float64:
		f.score, f.hasScore = s, true
	case int64:
		f.score, f.hasScore = float64(s), true
	}
	return f, true
}

// imageKey names an image across pods and reports: its repository without the registry and its
// tag (model.ImageRepository), so that "nginx:1.21" and "docker.io/library/nginx:1.21" are one.
func imageKey(image string) string {
	repo, tag := model.ImageRepository(image)
	return repo + ":" + tag
}

// imageIdentity is what makes two reports' images one image for the page's counts (D-new, the
// owner's): the digest Trivy Operator wrote (report.artifact.digest), else the repository and tag
// (imageKey). The same image in several workloads or namespaces is one image; a tag pushed again
// while pods still run the old one is two, each with its own findings; one digest under two names
// (a mirror) is one.
func imageIdentity(r model.VulnerabilityReport) string {
	if r.Digest != "" {
		return r.Digest
	}
	return imageKey(r.Image())
}

// shortDigest is the start of a digest that tells images apart on screen: "sha256:1a2b3c4d5e6f".
func shortDigest(d string) string {
	const keep = len("sha256:") + 12
	if len(d) > keep {
		return d[:keep]
	}
	return d
}

// imageSecNames writes the digest after an image's name where two images (identities) share the
// name, so that their rows and card entries tell them apart: "shop/api:2.4@sha256:1a2b3c4d5e6f".
func imageSecNames(reports []imageSecReport) {
	ids := map[string]map[string]struct{}{}
	for i := range reports {
		r := &reports[i]
		if ids[r.image] == nil {
			ids[r.image] = map[string]struct{}{}
		}
		ids[r.image][r.id] = struct{}{}
	}
	for i := range reports {
		r := &reports[i]
		if len(ids[r.image]) > 1 && r.r.Digest != "" && r.r.Tag != "" {
			r.image += "@" + shortDigest(r.r.Digest)
		}
	}
}

// runningPodImages is every running pod's images, each marked scanned when a report covers it
// (the Trivy integration's own matching, trivyReportIndex.podImages).
func runningPodImages(idx trivyReportIndex, pods []*unstructured.Unstructured) []imageSecPod {
	out := make([]imageSecPod, 0, len(pods))
	for _, pod := range pods {
		if model.NestedString(pod.Object, "status", "phase") != "Running" {
			continue
		}
		p := imageSecPod{namespace: pod.GetNamespace()}
		for _, im := range idx.podImages(pod) {
			key := imageKey(im.image)
			if im.report != nil {
				key = imageIdentity(*im.report) // as the cards count it
			}
			p.images = append(p.images, imageSecPodImage{key: key, scanned: im.report != nil})
		}
		out = append(out, p)
	}
	return out
}

// apiKindDeployment is the Deployment's API kind, which the page names.
const apiKindDeployment = "Deployment"

// The workload kinds a report names, as kind keys; a ReplicaSet a Deployment made is the
// Deployment's (deploymentOfReplicaSet).
var trivyWorkloadKinds = map[string]string{
	apiKindDeployment: logKindDeployments, apiKindReplicaSet: logKindReplicaSets, apiKindStatefulSet: logKindStatefulSets,
	apiKindDaemonSet: logKindDaemonSets, trivyKindCronJob: kindCronJobs, trivyKindJob: logKindJobs, trivyKindPod: logKindPods,
	"ReplicationController": "replicationcontrollers",
}

// trivyWorkloadRef is the workload a report is of, as a reference and as the page writes it
// ("Deployment api"): a ReplicaSet whose name ends in a pod template hash is its Deployment's; a
// kind Cogpit has no view of refers to the report itself.
func trivyWorkloadRef(context string, r model.VulnerabilityReport) (model.ObjectRef, string) {
	kind, name := r.Kind, r.Workload
	if kind == apiKindReplicaSet {
		if d := deploymentOfReplicaSet(name); d != "" {
			kind, name = apiKindDeployment, d
		}
	}
	text := kind + " " + name
	key, ok := trivyWorkloadKinds[kind]
	if !ok {
		return model.ObjectRef{Context: context, Kind: trivyReportsKind, Namespace: r.Namespace, Name: r.Name}, text
	}
	return model.ObjectRef{Context: context, Kind: key, Namespace: r.Namespace, Name: name}, text
}

// podTemplateHashChars are the characters of a pod template hash (Kubernetes' safe encoding, no
// vowels), which the Deployment controller puts after "<deployment>-" in its ReplicaSets' names.
const podTemplateHashChars = "bcdfghjklmnpqrstvwxz2456789"

// deploymentOfReplicaSet returns the Deployment a ReplicaSet's name says made it; "" for another
// name. The ReplicaSets are not read for it, as the CronJob of a Job is found by its name
// (cronJobOfJob).
func deploymentOfReplicaSet(rs string) string {
	i := strings.LastIndexByte(rs, '-')
	if i <= 0 || i == len(rs)-1 {
		return ""
	}
	for _, c := range rs[i+1:] {
		if !strings.ContainsRune(podTemplateHashChars, c) {
			return ""
		}
	}
	return rs[:i]
}

// The page's search tokens (parseFindingQuery), each a whitespace-free word of the search text.
const (
	tokenSevAtLeast = "sev>="
	tokenSev        = "sev:"
	tokenFixable    = "fixable"
	tokenNamespace  = "ns:"
	tokenImage      = "image:"
	tokenPackage    = "pkg:"
	tokenCVE        = "cve:"
)

// findingTokenKeys are the keys a token starts with, the longest first so that "sev>=" is not read
// as "sev:".
var findingTokenKeys = []string{tokenSevAtLeast, tokenSev, tokenNamespace, tokenImage, tokenPackage, tokenCVE}

// findingQuery is the page's search: the tokens, and the rest as the views' search takes it.
// Tokens of different keys must all hold; several tokens of one key are alternatives.
type findingQuery struct {
	sevAtLeast []int
	sevIs      []int
	fixable    bool
	namespaces []string
	images     []string // lowered substrings
	packages   []string // lowered substrings
	cves       []string // lowered ids
	text       string   // the words that are no token, joined by single spaces
	err        string   // why a token filters nothing out ("" when every token applies)
}

// parseFindingQuery reads the search text: every word that is a token (findingTokenKeys, any case,
// and "fixable") narrows the findings; the other words are the views' search. A token without a
// value (being typed) filters nothing; a severity token with a word that is no severity filters
// nothing and says why.
func parseFindingQuery(filter string) findingQuery {
	var q findingQuery
	var rest []string
	for _, word := range strings.Fields(filter) {
		lower := strings.ToLower(word)
		if lower == tokenFixable {
			q.fixable = true
			continue
		}
		key := ""
		for _, k := range findingTokenKeys {
			if strings.HasPrefix(lower, k) {
				key = k
				break
			}
		}
		if key == "" {
			rest = append(rest, word)
			continue
		}
		q.add(key, word[len(key):])
	}
	q.text = strings.Join(rest, " ")
	return q
}

// add takes one token's value.
func (q *findingQuery) add(key, value string) {
	if value == "" {
		return
	}
	lower := strings.ToLower(value)
	switch key {
	case tokenSevAtLeast, tokenSev:
		level, ok := severityLevel(value)
		if !ok {
			q.err = key + value + ": a severity is critical, high, medium, low or unknown"
			return
		}
		if key == tokenSev {
			q.sevIs = append(q.sevIs, level)
		} else {
			q.sevAtLeast = append(q.sevAtLeast, level)
		}
	case tokenNamespace:
		q.namespaces = append(q.namespaces, value)
	case tokenImage:
		q.images = append(q.images, lower)
	case tokenPackage:
		q.packages = append(q.packages, lower)
	case tokenCVE:
		q.cves = append(q.cves, lower)
	}
}

// keeps reports whether a finding of a report passes the tokens.
func (q *findingQuery) keeps(f *imageSecFinding, r *imageSecReport) bool {
	if q.fixable && f.fixed == "" {
		return false
	}
	if (len(q.sevAtLeast) > 0 || len(q.sevIs) > 0) && !q.severityKeeps(f.sev) {
		return false
	}
	if len(q.namespaces) > 0 && !slices.Contains(q.namespaces, r.r.Namespace) {
		return false
	}
	if len(q.cves) > 0 && !slices.Contains(q.cves, strings.ToLower(f.cve)) {
		return false
	}
	return containsAny(r.image, q.images) && containsAny(f.pkg, q.packages)
}

func (q *findingQuery) severityKeeps(level int) bool {
	for _, min := range q.sevAtLeast {
		if level >= min {
			return true
		}
	}
	return slices.Contains(q.sevIs, level)
}

// containsAny reports whether s contains one of the lowered substrings, or there are none.
func containsAny(s string, subs []string) bool {
	if len(subs) == 0 {
		return true
	}
	s = strings.ToLower(s)
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// ImageSecurityFilterWith returns a search text with a token (from an entry of the page's cards:
// "image:shop/api:2.4.1", "cve:CVE-2024-45337", "ns:shop") in place of the tokens of its key it
// had, the other words kept: a click on a card's entry narrows the table to it.
func ImageSecurityFilterWith(filter, token string) string {
	lower := strings.ToLower(token)
	key := ""
	for _, k := range findingTokenKeys {
		if strings.HasPrefix(lower, k) {
			key = k
			break
		}
	}
	var out []string
	for _, word := range strings.Fields(filter) {
		if key != "" && strings.HasPrefix(strings.ToLower(word), key) {
			continue
		}
		out = append(out, word)
	}
	return strings.Join(append(out, token), " ")
}

// The findings table's column ids.
const (
	isColumnWorkload  = "workload"
	isColumnContainer = "container"
	isColumnImage     = "image"
	isColumnCVE       = "cve"
	isColumnSeverity  = "severity"
	isColumnScore     = "score"
	isColumnPackage   = "package"
	isColumnInstalled = "installed"
	isColumnFixed     = "fixed"
)

// imageSecProfile is the profile of the findings table: one row per finding, rows of the
// workloads' kinds (MixedKinds). Its key is the page's with that table, so that no kind is
// mistaken for it.
var imageSecProfile = &model.KindProfile{
	Key: ImageSecurityFindings, Title: imageSecTableTitle, Namespaced: true,
	Columns: []model.Column{
		{ID: columnNamespace, Title: titleNamespace, Type: model.ColumnText, Width: 96},
		{ID: isColumnWorkload, Title: "Workload", Type: model.ColumnText, Width: 160},
		{ID: isColumnContainer, Title: "Container", Type: model.ColumnText, Width: 90},
		{ID: isColumnImage, Title: isTitleImage, Type: model.ColumnText, Width: 170},
		{ID: isColumnCVE, Title: titleCVE, Type: model.ColumnText, Width: 126},
		{ID: isColumnSeverity, Title: titleSeverity, Type: model.ColumnNumber, Width: 84},
		{ID: isColumnScore, Title: "Score", Type: model.ColumnNumber, Width: 56},
		{ID: isColumnPackage, Title: "Package", Type: model.ColumnText, Width: 150},
		{ID: isColumnInstalled, Title: "Installed", Type: model.ColumnText, Width: 100},
		{ID: isColumnFixed, Title: "Fixed", Type: model.ColumnText, Width: 110},
	},
}

// imageSecTableTitle is the table's title: the page's table heading names its rows; isTitleImage
// its Image column's.
const (
	imageSecTableTitle = "Findings"
	isTitleImage       = "Image"
)

// imageSecSort is the table's order until the user sorts: the worst severity first.
var imageSecSort = SortSpec{Column: isColumnSeverity, Desc: true}

// noScore is the Score cell of a finding without one.
const noScore = "—"

// findingRow is a finding as a row of the table. The row names the workload (a click opens its
// drawer); only the Severity cell carries a status, Critical's and High's (D-calm); the row none.
func findingRow(f *imageSecFinding, r *imageSecReport) model.Row {
	score := model.Cell{Text: noScore, Num: -1}
	if f.hasScore {
		score = model.Cell{Text: strconv.FormatFloat(f.score, 'f', 1, 64), Num: int64(f.score*10 + 0.5)}
	}
	return model.Row{
		Ref: r.ref,
		Cells: []model.Cell{
			{Text: r.r.Namespace},
			{Text: r.workload},
			{Text: r.r.Container},
			{Text: r.image},
			{Text: f.cve},
			{Text: trivySeverities[f.sev], Num: int64(f.sev), Status: severityStatus(f.sev)},
			score,
			{Text: f.pkg},
			{Text: f.installed},
			{Text: f.fixed},
		},
	}
}

// imageSecTable is the page's table the view names: the CVEs (ImageSecurity, imageSecCVETable) or
// the findings (ImageSecurityFindings).
func imageSecTable(context, view string, fb *imageSecBuild, p viewParams) *TableSnapshot {
	if view == ImageSecurityFindings {
		return imageSecFindingsTable(context, fb, p)
	}
	return imageSecCVETable(context, fb, p)
}

// imageSecFindingsTable is the findings table: the findings in the namespaces that the search
// keeps, sorted. Total counts every finding.
func imageSecFindingsTable(context string, fb *imageSecBuild, p viewParams) *TableSnapshot {
	nsSet := setOf(p.namespaces)
	q := parseFindingQuery(p.filter)
	textParams := p
	textParams.filter = q.text
	match, filterErr := textParams.matcher()
	if filterErr == "" {
		filterErr = q.err
	}
	rows := []model.Row{}
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
		row := findingRow(f, r)
		if !match.All() && !match.Match(row) {
			continue
		}
		rows = append(rows, row)
	}
	sortImageSecRows(imageSecProfile, rows, p.sort)
	snap := &TableSnapshot{
		View: ImageSecurityFindings, Context: context, Title: ImageSecurityTitle, Namespaced: true,
		Columns: ColumnsShown(imageSecProfile.Columns, p.hidden, p.hiddenSet), Order: ColumnOrder(imageSecProfile.Columns, p.order),
		Rows: rows, Sort: p.sort,
		Namespaces: slices.Clone(p.namespaces), Filter: p.filter, FilterOptions: p.filterOpts, FilterError: filterErr,
		Total: len(fb.findings), Synced: fb.synced, Error: fb.errText,
	}
	if fb.reason != "" {
		snap.Error = fb.reason
	}
	return snap
}

// sortImageSecRows sorts a table's rows by the column: stable, so that rows do not change places
// between two builds. Findings' ties go by their workload, then as the build ordered them
// (namespace, workload, container, the worst severity first); CVEs' ties as collectCVEs ordered
// them (the most images, then the most containers).
func sortImageSecRows(prof *model.KindProfile, rows []model.Row, spec SortSpec) {
	ci := prof.ColumnIndex(spec.Column)
	if ci < 0 {
		return
	}
	typ := prof.Columns[ci].Type
	if prof == imageSecCVEProfile { // a CVE's row names no object: ties keep the worst first
		slices.SortStableFunc(rows, func(a, b model.Row) int { return compareCells(&a, &b, ci, typ, spec.Desc) })
		return
	}
	slices.SortStableFunc(rows, func(a, b model.Row) int { return compareRows(&a, &b, ci, typ, spec.Desc) })
}

// ImageSecurityModel is the page's upper part: the header's counts and the four cards, for the
// selected namespaces (the search narrows the table only).
type ImageSecurityModel struct {
	// Namespaces are the namespaces counted; empty: all.
	Namespaces []string `json:"namespaces,omitempty"`
	// PodsRead is false when the page reads no pods (the user may not list them): Running, Scanned
	// and NotScanned are then 0 and mean nothing.
	PodsRead bool `json:"podsRead"`
	// Running counts the distinct images of the running pods' containers (a scanned one by its
	// report's digest, else by repository and tag), Scanned those of them a report covers,
	// NotScanned the others.
	Running    int `json:"running"`
	Scanned    int `json:"scanned"`
	NotScanned int `json:"notScanned"`
	// Reports counts the reports; NewestScan is the latest scan among them (zero without one).
	Reports    int       `json:"reports"`
	NewestScan time.Time `json:"newestScan,omitzero"`
	// CVEs counts the distinct CVE ids, Fixable those with a fixed version in one of their
	// findings; Severities counts the ids by their severity (the worst a finding gives them),
	// Critical to Low, then Unknown when there is one.
	CVEs       int                     `json:"cves"`
	Fixable    int                     `json:"fixable"`
	Severities []ImageSecuritySeverity `json:"severities"`
	// TopImages are the five worst images: by grade (D-113), then Critical, then High.
	TopImages []ImageSecurityImage `json:"topImages"`
	// TopCVEs are the five worst CVEs: by severity, then by the images they affect, then by the
	// containers, then by id.
	TopCVEs []ImageSecurityCVE `json:"topCves"`
	// TopNamespaces are the five worst namespaces: by their worst image's grade, then Critical,
	// then High.
	TopNamespaces []ImageSecurityNamespace `json:"topNamespaces"`
	// Trend is the findings curve from Prometheus (imagesecurity_trend.go); nil where there is no
	// Prometheus to ask or an integration is off.
	Trend *ImageSecurityTrend `json:"trend,omitempty"`
	// Synced is false while the reports or the pods are still being listed.
	Synced bool `json:"synced"`
	// Error says why the page shows nothing (the integration off, the reports not served), or what
	// a read met; empty when it reads.
	Error string `json:"error,omitempty"`
}

// ImageSecuritySeverity is one severity of the Vulnerabilities card.
type ImageSecuritySeverity struct {
	Severity string `json:"severity"` // "Critical"
	Count    int    `json:"count"`
	// Status is the severity's colour (D-calm): error for Critical, warning for High, none else.
	Status model.Status `json:"status,omitempty"`
}

// ImageSecurityImage is one entry of Top images: the image (repository and tag), its grade and
// the counts it is graded by (the report's summary, as the Pods' Signals column and the drawer have
// them), and the search token a click on it sets.
type ImageSecurityImage struct {
	Image    string           `json:"image"`
	Grade    model.ImageGrade `json:"grade"`
	Critical int64            `json:"critical"`
	High     int64            `json:"high"`
	Token    string           `json:"token"`
}

// ImageSecurityCVE is one entry of Top CVEs.
type ImageSecurityCVE struct {
	ID       string       `json:"id"`
	Severity string       `json:"severity"`
	Status   model.Status `json:"status,omitempty"`
	// Images counts the distinct images it is found in (by digest, else repository and tag;
	// replicas and the same image in several workloads count once), Containers the workloads'
	// containers that run them.
	Images     int    `json:"images"`
	Containers int    `json:"containers"`
	Token      string `json:"token"`
}

// ImageSecurityNamespace is one entry of Top namespaces: its worst image's grade, and its distinct
// CVE ids of severity Critical and High.
type ImageSecurityNamespace struct {
	Namespace string           `json:"namespace"`
	Grade     model.ImageGrade `json:"grade"`
	Critical  int              `json:"critical"`
	High      int              `json:"high"`
	Token     string           `json:"token"`
}

// imageSecTop is how many entries a card lists.
const imageSecTop = 5

// cveTally is what the cards count of one CVE id: its images (imageIdentity) and its containers
// (imageSecReport.placeKey).
type cveTally struct {
	sev        int
	fixable    bool
	images     map[string]struct{}
	containers map[string]struct{}
}

// imageTally is one image of Top images: its worst report, and its name as the rows write it.
type imageTally struct {
	r    model.VulnerabilityReport
	rank int64
	name string
}

// nsTally is one namespace of Top namespaces.
type nsTally struct {
	rank           int64
	grade          model.ImageGrade
	cves           map[string]struct{}
	critical, high int
}

// imageSecModel counts a build's reports, findings and pods in the namespaces (empty: all).
func imageSecModel(fb *imageSecBuild, namespaces []string) *ImageSecurityModel {
	nsSet := setOf(namespaces)
	in := func(ns string) bool {
		if len(nsSet) == 0 {
			return true
		}
		_, ok := nsSet[ns]
		return ok
	}
	m := &ImageSecurityModel{Namespaces: slices.Clone(namespaces), PodsRead: fb.podsRead, Synced: fb.synced, Error: fb.errText,
		Severities: []ImageSecuritySeverity{}, TopImages: []ImageSecurityImage{}, TopCVEs: []ImageSecurityCVE{}, TopNamespaces: []ImageSecurityNamespace{}}
	if fb.reason != "" {
		m.Error = fb.reason
	}
	m.Running, m.Scanned = countRunning(fb.pods, in)
	m.NotScanned = m.Running - m.Scanned
	cves := map[string]*cveTally{}
	images := map[string]*imageTally{}
	spaces := map[string]*nsTally{}
	for i := range fb.reports {
		r := &fb.reports[i]
		if !in(r.r.Namespace) {
			continue
		}
		m.Reports++
		if r.r.Scanned.After(m.NewestScan) {
			m.NewestScan = r.r.Scanned
		}
		tallyReport(r, fb.findings[r.from:r.to], cves, images, spaces)
	}
	m.CVEs = len(cves)
	m.Severities, m.Fixable = severityCounts(cves)
	m.TopImages = topImages(images)
	m.TopCVEs = topCVEs(cves)
	m.TopNamespaces = topNamespaces(spaces, cves)
	return m
}

// countRunning counts the distinct images of the running pods in the namespaces, and those a
// report covers in one of them.
func countRunning(pods []imageSecPod, in func(string) bool) (running, scanned int) {
	seen := map[string]bool{} // key → scanned
	for _, p := range pods {
		if !in(p.namespace) {
			continue
		}
		for _, im := range p.images {
			seen[im.key] = seen[im.key] || im.scanned
		}
	}
	for _, s := range seen {
		running++
		if s {
			scanned++
		}
	}
	return running, scanned
}

// tallyReport adds a report and its findings to the cards' tallies.
func tallyReport(r *imageSecReport, findings []imageSecFinding, cves map[string]*cveTally, images map[string]*imageTally, spaces map[string]*nsTally) {
	rank := reportRank(r.r)
	if cur := images[r.id]; cur == nil || worseReport(r.r, cur.r) {
		images[r.id] = &imageTally{r: r.r, rank: rank, name: r.image}
	}
	ns := spaces[r.r.Namespace]
	if ns == nil {
		ns = &nsTally{rank: -1, cves: map[string]struct{}{}}
		spaces[r.r.Namespace] = ns
	}
	if rank > ns.rank {
		ns.rank, ns.grade = rank, r.r.Grade()
	}
	for i := range findings {
		f := &findings[i]
		t := cves[f.cve]
		if t == nil {
			t = &cveTally{sev: f.sev, images: map[string]struct{}{}, containers: map[string]struct{}{}}
			cves[f.cve] = t
		}
		t.sev = max(t.sev, f.sev)
		t.fixable = t.fixable || f.fixed != ""
		t.images[r.id] = struct{}{}
		t.containers[r.placeKey()] = struct{}{}
		ns.cves[f.cve] = struct{}{}
	}
}

// severityCounts counts the CVE ids by severity, Critical to Low and Unknown when there is one,
// and those with a fix.
func severityCounts(cves map[string]*cveTally) ([]ImageSecuritySeverity, int) {
	var by [len(trivySeverities)]int
	fixable := 0
	for _, t := range cves {
		by[t.sev]++
		if t.fixable {
			fixable++
		}
	}
	out := make([]ImageSecuritySeverity, 0, len(trivySeverities))
	for level := sevCritical; level >= sevLow; level-- {
		out = append(out, ImageSecuritySeverity{Severity: trivySeverities[level], Count: by[level], Status: severityStatus(level)})
	}
	if by[sevUnknown] > 0 {
		out = append(out, ImageSecuritySeverity{Severity: trivySeverities[sevUnknown], Count: by[sevUnknown]})
	}
	return out, fixable
}

func topImages(images map[string]*imageTally) []ImageSecurityImage {
	ranked := make([]*imageTally, 0, len(images))
	for _, t := range images {
		ranked = append(ranked, t)
	}
	slices.SortFunc(ranked, func(a, b *imageTally) int {
		return cmp.Or(cmp.Compare(b.rank, a.rank), cmp.Compare(b.r.Counts.Critical, a.r.Counts.Critical),
			cmp.Compare(b.r.Counts.High, a.r.Counts.High), cmp.Compare(a.name, b.name))
	})
	out := make([]ImageSecurityImage, 0, min(len(ranked), imageSecTop))
	for _, t := range ranked[:min(len(ranked), imageSecTop)] {
		out = append(out, ImageSecurityImage{Image: t.name, Grade: t.r.Grade(), Critical: t.r.Counts.Critical, High: t.r.Counts.High, Token: tokenImage + t.name})
	}
	return out
}

func topCVEs(cves map[string]*cveTally) []ImageSecurityCVE {
	out := make([]ImageSecurityCVE, 0, len(cves))
	for id, t := range cves {
		out = append(out, ImageSecurityCVE{ID: id, Severity: trivySeverities[t.sev], Status: severityStatus(t.sev), Images: len(t.images),
			Containers: len(t.containers), Token: tokenCVE + id})
	}
	slices.SortFunc(out, func(a, b ImageSecurityCVE) int {
		return cmp.Or(cmp.Compare(cves[b.ID].sev, cves[a.ID].sev), cmp.Compare(b.Images, a.Images), cmp.Compare(b.Containers, a.Containers),
			cmp.Compare(a.ID, b.ID))
	})
	return out[:min(len(out), imageSecTop)]
}

// topNamespaces ranks the namespaces; a CVE counts in its severity across the selection, as the
// Vulnerabilities card counts it.
func topNamespaces(spaces map[string]*nsTally, cves map[string]*cveTally) []ImageSecurityNamespace {
	out := make([]ImageSecurityNamespace, 0, len(spaces))
	for name, t := range spaces {
		for id := range t.cves {
			switch cves[id].sev {
			case sevCritical:
				t.critical++
			case sevHigh:
				t.high++
			}
		}
		out = append(out, ImageSecurityNamespace{Namespace: name, Grade: t.grade, Critical: t.critical, High: t.high, Token: tokenNamespace + name})
	}
	slices.SortFunc(out, func(a, b ImageSecurityNamespace) int {
		return cmp.Or(cmp.Compare(spaces[b.Namespace].rank, spaces[a.Namespace].rank), cmp.Compare(b.Critical, a.Critical),
			cmp.Compare(b.High, a.High), cmp.Compare(a.Namespace, b.Namespace))
	})
	return out[:min(len(out), imageSecTop)]
}
