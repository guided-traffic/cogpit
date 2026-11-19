package kinds

import (
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Policy reports (D-79; resource-parity › Custom resources, beyond-lens › Integrations): the
// Kubernetes Policy Working Group's PolicyReport and ClusterPolicyReport (wgpolicyk8s.io/v1alpha2),
// which Kyverno writes and the Kyverno integration reads (engine/kyverno.go). Since Kyverno 1.10
// a report is about one object, its scope, and holds the result of every rule that applies to it
// (pass, fail, warn, error, skip); a report of an older Kyverno, or of another writer, is about a
// namespace or the cluster and names each result's objects in its resources. Both shapes are read.
//
// The curated profiles below give the reports' views the columns of Kyverno's printer columns
// (Kind, Name, Pass, Fail, Warn, Error, Skip, Age) with the object as a link, and a drawer with
// the results; and, as curated kinds, they come from API discovery for a user who may not list the
// custom resource definitions (A90, engine/customkinds_discovery.go), so that the integration finds
// the reports such a user may read. Version: v1alpha2 only, the one Kyverno serves (checked on a
// real cluster's Kyverno 1.17); the Working Group's v1beta1 is not read.

// The policy reports' kind keys and API group.
const (
	PolicyReportsKey        = "policyreports.wgpolicyk8s.io"
	ClusterPolicyReportsKey = "clusterpolicyreports.wgpolicyk8s.io"
	PolicyReportGroup       = "wgpolicyk8s.io"
	policyReportVersion     = "v1alpha2"
)

// Kyverno's kinds a report's policy names: a ClusterPolicy, or a namespaced Policy ("ns/name").
const (
	kyvernoClusterPoliciesKey = "clusterpolicies.kyverno.io"
	kyvernoPoliciesKey        = "policies.kyverno.io"
	// kyvernoSource is the source Kyverno writes on the results of its ClusterPolicies and Policies.
	kyvernoSource = "kyverno"
	// kyvernoValidatingPolicySource is the source of a ValidatingPolicy's results (policies.kyverno.io).
	kyvernoValidatingPolicySource = "KyvernoValidatingPolicy"
	kyvernoValidatingPoliciesKey  = "validatingpolicies.policies.kyverno.io"
	// managedByLabel is the label Kyverno puts on every report it writes, with the value kyverno.
	managedByLabel = "app.kubernetes.io/managed-by"
)

// The report views' column ids and titles beyond the shared ones.
const (
	idPolicyObject = "object"
	titleMessage   = "Message"
)

// The results a report holds, as the API names them.
const (
	PolicyPass  = "pass"
	PolicyFail  = "fail"
	PolicyWarn  = "warn"
	PolicyError = "error"
	PolicySkip  = "skip"
)

// IsPolicyViolation reports whether a result is a violation of its policy by the object: fail, or
// warn, which Kyverno writes for a failed rule of a policy its author marked as not scored
// (policies.kyverno.io/scored: "false"). An error is the policy engine's failure to apply the rule,
// not the object's; pass and skip are none.
func IsPolicyViolation(result string) bool { return result == PolicyFail || result == PolicyWarn }

// PolicySubject is an object a policy report's result is about.
type PolicySubject struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name,omitempty"`
	UID        string `json:"uid,omitempty"`
}

// Group returns the subject's API group ("" for the core group).
func (s PolicySubject) Group() string {
	group, _, found := strings.Cut(s.APIVersion, "/")
	if !found {
		return ""
	}
	return group
}

// PolicyResult is one result of a policy report.
type PolicyResult struct {
	Policy   string `json:"policy"`
	Rule     string `json:"rule,omitempty"`
	Result   string `json:"result"`
	Message  string `json:"message,omitempty"`
	Severity string `json:"severity,omitempty"`
	Category string `json:"category,omitempty"`
	Source   string `json:"source,omitempty"`
	// Process is how Kyverno found it: "background scan" or "admission review" (its result's
	// properties.process); empty from other writers.
	Process string `json:"process,omitempty"`
	// Time is when the result was written; zero without one.
	Time time.Time `json:"time,omitzero"`
	// Subjects are the objects it is about: the report's scope, else the result's resources.
	Subjects []PolicySubject `json:"-"`
}

// PolicyRef links a result's policy: Kyverno's ClusterPolicy, or its namespaced Policy for a
// policy named "<namespace>/<name>" (source kyverno); a ValidatingPolicy (source
// KyvernoValidatingPolicy, seen on Kyverno 1.19 on kind); nil for a result of another source
// (Kyverno's other CEL kinds, a Kubernetes ValidatingAdmissionPolicy's), whose kind Cogpit has not
// seen a report name. The window drops the link while the cluster does not serve the kind.
func (r PolicyResult) PolicyRef() *model.ObjectRef {
	switch {
	case r.Policy == "":
		return nil
	case r.Source == kyvernoValidatingPolicySource && !strings.Contains(r.Policy, "/"):
		return linkTo(kyvernoValidatingPoliciesKey, "", r.Policy)
	case r.Source != "" && r.Source != kyvernoSource:
		return nil
	}
	if ns, name, found := strings.Cut(r.Policy, "/"); found {
		return linkTo(kyvernoPoliciesKey, ns, name)
	}
	return linkTo(kyvernoClusterPoliciesKey, "", r.Policy)
}

// PolicyReportScope returns the object a report is about (its scope), and whether it names one.
func PolicyReportScope(obj any) (PolicySubject, bool) {
	s := policySubject(model.NestedMap(obj, "scope"))
	return s, s.Name != "" || s.UID != ""
}

func policySubject(m map[string]any) PolicySubject {
	return PolicySubject{
		APIVersion: model.NestedString(m, "apiVersion"), Kind: model.NestedString(m, "kind"),
		Namespace: model.NestedString(m, "namespace"), Name: model.NestedString(m, "name"), UID: model.NestedString(m, "uid"),
	}
}

// PolicyReportResults returns a report's results, each with the objects it is about: the report's
// scope, else its own resources. keep, when set, picks the results to read (the others are not
// parsed); nil reads every one.
func PolicyReportResults(obj any, keep func(result string) bool) []PolicyResult {
	scope, scoped := PolicyReportScope(obj)
	raw := model.NestedSlice(obj, "results")
	out := make([]PolicyResult, 0, len(raw))
	for _, r := range raw {
		res := model.NestedString(r, "result")
		if keep != nil && !keep(res) {
			continue
		}
		pr := PolicyResult{
			Policy: model.NestedString(r, idPolicy), Rule: model.NestedString(r, "rule"), Result: res,
			Message: model.NestedString(r, "message"), Severity: model.NestedString(r, "severity"),
			Category: model.NestedString(r, "category"), Source: model.NestedString(r, "source"),
			Process: model.NestedString(r, "properties", "process"),
		}
		if sec, ok := model.NestedInt64(r, "timestamp", "seconds"); ok && sec > 0 {
			nanos, _ := model.NestedInt64(r, "timestamp", "nanos")
			pr.Time = time.Unix(sec, nanos).UTC()
		}
		if scoped {
			pr.Subjects = []PolicySubject{scope}
		} else {
			for _, s := range model.NestedSlice(r, "resources") {
				if sub := policySubject(asMap(s)); sub.Name != "" || sub.UID != "" {
					pr.Subjects = append(pr.Subjects, sub)
				}
			}
		}
		out = append(out, pr)
	}
	return out
}

// KyvernoReport reports whether Kyverno wrote a report: its managed-by label says so, or (a report
// of a Kyverno that set no label) one of its results' sources is Kyverno's. Reports of other
// policy engines (Kubewarden, an adapter's) are theirs, not the Kyverno integration's.
func KyvernoReport(obj any) bool {
	if model.NestedString(obj, "metadata", "labels", managedByLabel) == kyvernoSource {
		return true
	}
	for _, r := range model.NestedSlice(obj, "results") {
		if strings.HasPrefix(strings.ToLower(model.NestedString(r, "source")), kyvernoSource) {
			return true
		}
	}
	return false
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// policyReportColumns are a report view's columns after Name (and Namespace): Kyverno's printer
// columns with the object as a link.
var policyReportColumns = []model.Column{
	{ID: idKind, Title: titleKind, Type: model.ColumnText},
	{ID: idPolicyObject, Title: "Object", Type: model.ColumnText},
	countColumn(PolicyPass), countColumn(PolicyFail), countColumn(PolicyWarn), countColumn(PolicyError), countColumn(PolicySkip),
	ageColumn,
}

// countColumn is the column of a result's count, titled as Kyverno's printer column: "Fail".
func countColumn(result string) model.Column {
	return model.Column{ID: result, Title: strings.ToUpper(result[:1]) + result[1:], Type: model.ColumnNumber}
}

var policyReports = &model.KindProfile{
	Key:        PolicyReportsKey,
	Group:      PolicyReportGroup,
	Version:    policyReportVersion,
	Resource:   "policyreports",
	Kind:       "PolicyReport",
	Title:      "Policy Reports",
	Namespaced: true,
	Columns:    append([]model.Column{nameColumn, namespaceColumn}, policyReportColumns...),
	Cells: func(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
		return policyReportCells(obj, model.NoRelated)
	},
	Fields: policyReportFields,
	Detail: func(obj *unstructured.Unstructured) []model.DetailSection {
		return policyReportDetail(obj, model.NoRelated)
	},
	// A report about a custom resource (a Certificate) links it through its definition, which the
	// custom kinds' own watch holds.
	Needs:      []model.RelatedIndex{CustomKindsByGroupKind},
	CellsWith:  policyReportCells,
	DetailWith: policyReportDetail,
}

var clusterPolicyReports = &model.KindProfile{
	Key:        ClusterPolicyReportsKey,
	Group:      PolicyReportGroup,
	Version:    policyReportVersion,
	Resource:   "clusterpolicyreports",
	Kind:       "ClusterPolicyReport",
	Title:      "Cluster Policy Reports",
	Namespaced: false,
	Columns:    append([]model.Column{nameColumn}, policyReportColumns...),
	Cells: func(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
		return clusterPolicyReportCells(obj, model.NoRelated)
	},
	Fields: policyReportFields,
	Detail: func(obj *unstructured.Unstructured) []model.DetailSection {
		return policyReportDetail(obj, model.NoRelated)
	},
	Needs:      []model.RelatedIndex{CustomKindsByGroupKind},
	CellsWith:  clusterPolicyReportCells,
	DetailWith: policyReportDetail,
}

func init() { curated = append(curated, policyReports, clusterPolicyReports) }

// policyReportFields are what resources.find searches a report by: its object ("Deployment/web")
// and its results' policies.
var policyReportFields = map[string]fieldFunc{
	idPolicyObject: func(obj *unstructured.Unstructured) []string {
		if s, ok := PolicyReportScope(obj); ok {
			return []string{s.Kind + "/" + s.Name}
		}
		return nil
	},
	idPolicy: func(obj *unstructured.Unstructured) []string {
		var out []string
		for _, r := range PolicyReportResults(obj, nil) {
			out = appendUnique(out, r.Policy)
		}
		return out
	},
}

func policyReportCells(obj *unstructured.Unstructured, rel model.Related) ([]model.Cell, model.Status) {
	cells := append([]model.Cell{nameCell(obj), namespaceCell(obj)}, policyReportSummaryCells(obj, rel)...)
	return cells, model.StatusNone
}

func clusterPolicyReportCells(obj *unstructured.Unstructured, rel model.Related) ([]model.Cell, model.Status) {
	cells := append([]model.Cell{nameCell(obj)}, policyReportSummaryCells(obj, rel)...)
	return cells, model.StatusNone
}

// policyReportSummaryCells are the cells of policyReportColumns: the object, the summary's counts
// (a fail in the warning colour, an error in the error colour) and the age.
func policyReportSummaryCells(obj *unstructured.Unstructured, rel model.Related) []model.Cell {
	scope, _ := PolicyReportScope(obj)
	count := func(result string, status model.Status) model.Cell {
		n, _ := model.NestedInt64(obj, "summary", result)
		c := model.Cell{Num: n}
		if n > 0 {
			c.Status = status
		}
		return c
	}
	return []model.Cell{
		{Text: scope.Kind},
		{Text: scope.Name, Link: policySubjectRef(scope, rel)},
		count(PolicyPass, model.StatusNone),
		count(PolicyFail, model.StatusWarning),
		count(PolicyWarn, model.StatusWarning),
		count(PolicyError, model.StatusError),
		count(PolicySkip, model.StatusNone),
		model.AgeCell(created(obj)),
	}
}

// policySubjectRef links an object a report is about: a built-in kind by its key, a custom
// resource through its definition in rel.
func policySubjectRef(s PolicySubject, rel model.Related) *model.ObjectRef {
	return RefToWith(rel, s.APIVersion, s.Kind, s.Namespace, s.Name)
}

// policyResultStatus colours a result: a fail or a warn in the warning colour, an error in the
// error colour, a pass in the ok colour.
func policyResultStatus(result string) model.Status {
	switch result {
	case PolicyFail, PolicyWarn:
		return model.StatusWarning
	case PolicyError:
		return model.StatusError
	case PolicyPass:
		return model.StatusOK
	}
	return model.StatusNone
}

// policyReportDetail is a report's drawer: its object and summary, then its results as a table.
func policyReportDetail(obj *unstructured.Unstructured, rel model.Related) []model.DetailSection {
	var d detailItems
	if scope, ok := PolicyReportScope(obj); ok {
		d.put(linkedItem("Object", strings.TrimSpace(scope.Kind+" "+scope.Name), policySubjectRef(scope, rel)))
	}
	var counts []string
	for _, r := range []string{PolicyPass, PolicyFail, PolicyWarn, PolicyError, PolicySkip} {
		if n, ok := model.NestedInt64(obj, "summary", r); ok && n > 0 {
			counts = append(counts, strconv.FormatInt(n, 10)+" "+r)
		}
	}
	d.add("Summary", strings.Join(counts, ", "))
	sections := withSection(nil, model.TitleProperties, d)
	results := PolicyReportResults(obj, nil)
	if len(results) == 0 {
		return sections
	}
	cols := []model.Column{
		{ID: idPolicy, Title: "Policy", Type: model.ColumnText},
		{ID: "rule", Title: "Rule", Type: model.ColumnText},
		{ID: "result", Title: "Result", Type: model.ColumnText},
		{ID: "message", Title: titleMessage, Type: model.ColumnText},
	}
	rows := make([][]model.Cell, 0, len(results))
	for _, r := range results {
		rows = append(rows, []model.Cell{
			{Text: r.Policy, Link: r.PolicyRef()},
			{Text: r.Rule},
			{Text: r.Result, Status: policyResultStatus(r.Result)},
			{Text: singleLine(r.Message)},
		})
	}
	return append(sections, model.DetailSection{Title: "Results", Table: model.CellTable(cols, rows)})
}
