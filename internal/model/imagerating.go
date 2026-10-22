package model

import (
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Image grades (D-78, D-113): a container image's grade from Trivy Operator's VulnerabilityReport
// (aquasecurity.github.io/v1alpha1), shown as a shield in Docker Scout's manner. The owner's request
// (2026-10-22): the Pods list shows each pod's worst grade as a symbol only, the pod's drawer each
// container's grade in its own section, as a symbol and a detail card. His rules of 2026-10-22, from
// the report's own fields only (report.summary and report.os.eosl):
//
//   - S: no vulnerability of any severity, and the OS is not at its end of life;
//   - A: only Low or Unknown (Unknown counts as Low), at most GradeAMaxLow of them;
//   - B: Medium, or more than GradeAMaxLow Low or Unknown;
//   - C: High;
//   - D: 1 to GradeDMaxCritical Critical;
//   - E: more than GradeDMaxCritical Critical;
//   - an OS at its end of life (report.os.eosl) grades C at best: Trivy warns that it may miss
//     vulnerabilities there, since the distribution publishes no more advisories;
//   - N/A: no report of the image.
//
// The thresholds of D and E are provisional: the owner looks at them again on real images.

// The labels Trivy Operator puts on a report (seen on Trivy Operator 0.35.0, chart 0.37.0, on kind,
// 2026-10-17): the workload it scanned, by kind, name and namespace, and the container. A name that
// is no valid label value (longer than 63 characters) goes into the annotation of the same key
// instead, the label then holding a hash under TrivyLabelResourceNameHash.
const (
	TrivyLabelResourceKind      = "trivy-operator.resource.kind"
	TrivyLabelResourceName      = "trivy-operator.resource.name"
	TrivyLabelResourceNameHash  = "trivy-operator.resource.name-hash"
	TrivyLabelResourceNamespace = "trivy-operator.resource.namespace"
	TrivyLabelContainerName     = "trivy-operator.container.name"
)

// ImageGrade is an image's grade. The zero value is GradeNone, no grade (nothing to draw); a
// greater grade from GradeS on is a worse one.
type ImageGrade int

// The grades, N/A first, then from the best to the worst.
const (
	GradeNone ImageGrade = iota // no grade: a cell or section without one
	GradeNA                     // no report of the image
	GradeS
	GradeA
	GradeB
	GradeC
	GradeD
	GradeE
)

// The thresholds of the grades (the owner's, 2026-10-22; D and E provisional).
const (
	// GradeAMaxLow is the most Low and Unknown vulnerabilities an image of grade A has; more is B.
	GradeAMaxLow = 10
	// GradeDMaxCritical is the most Critical vulnerabilities an image of grade D has; more is E.
	GradeDMaxCritical = 5
)

var gradeNames = [...]string{"", "N/A", "S", "A", "B", "C", "D", "E"}

func (g ImageGrade) valid() bool { return g >= GradeNone && g <= GradeE }

// String is the grade as the shield writes it: "S", "D", "N/A"; "" for none.
func (g ImageGrade) String() string {
	if !g.valid() {
		return ""
	}
	return gradeNames[g]
}

// MarshalText writes the grade as String does.
func (g ImageGrade) MarshalText() ([]byte, error) { return []byte(g.String()), nil }

// UnmarshalText reads a grade as String writes it; an unknown text reads as GradeNone.
func (g *ImageGrade) UnmarshalText(b []byte) error {
	*g = GradeNone
	for i, n := range gradeNames {
		if n == string(b) {
			*g = ImageGrade(i)
		}
	}
	return nil
}

// VulnerabilityCounts are the vulnerabilities of one image by severity (a report's summary).
type VulnerabilityCounts struct {
	Critical int64 `json:"critical"`
	High     int64 `json:"high"`
	Medium   int64 `json:"medium"`
	Low      int64 `json:"low"`
	Unknown  int64 `json:"unknown"`
}

// Total is the number of vulnerabilities of every severity.
func (c VulnerabilityCounts) Total() int64 { return c.Critical + c.High + c.Medium + c.Low + c.Unknown }

// LowOrUnknown is the number of Low and Unknown vulnerabilities, which the grades count together.
func (c VulnerabilityCounts) LowOrUnknown() int64 { return c.Low + c.Unknown }

// Summary is every severity's count, the worst first: "2 Critical · 45 High · 28 Medium · 4 Low ·
// 3 Unknown".
func (c VulnerabilityCounts) Summary() string {
	return strconv.FormatInt(c.Critical, 10) + " Critical · " + strconv.FormatInt(c.High, 10) + " High · " +
		strconv.FormatInt(c.Medium, 10) + " Medium · " + strconv.FormatInt(c.Low, 10) + " Low · " +
		strconv.FormatInt(c.Unknown, 10) + " Unknown"
}

// Grade is the grade of an image with these counts whose OS is (eosl) or is not at its end of life.
func (c VulnerabilityCounts) Grade(eosl bool) ImageGrade {
	g := c.counted()
	if eosl && g < GradeC {
		return GradeC
	}
	return g
}

// counted is the grade by the counts alone.
func (c VulnerabilityCounts) counted() ImageGrade {
	switch low := c.LowOrUnknown(); {
	case c.Critical > GradeDMaxCritical:
		return GradeE
	case c.Critical > 0:
		return GradeD
	case c.High > 0:
		return GradeC
	case c.Medium > 0, low > GradeAMaxLow:
		return GradeB
	case low > 0:
		return GradeA
	}
	return GradeS
}

// basis is the number that decides an image's grade and what it counts: "Critical", "High", "Low
// or Unknown"; the end of life when it capped the grade. S and N/A count nothing.
func (c VulnerabilityCounts) basis(g ImageGrade, eosl bool) (int64, string) {
	if eosl && g == GradeC && c.counted() < GradeC {
		return 0, "OS end of life"
	}
	switch g {
	case GradeE, GradeD:
		return c.Critical, "Critical"
	case GradeC:
		return c.High, "High"
	case GradeB:
		if c.Medium > 0 {
			return c.Medium, "Medium"
		}
		return c.LowOrUnknown(), c.lowWord()
	case GradeA:
		return c.LowOrUnknown(), c.lowWord()
	case GradeS:
		return 0, "no known vulnerabilities"
	}
	return 0, "not scanned"
}

// lowWord names what LowOrUnknown counts: "Low", "Unknown", or both.
func (c VulnerabilityCounts) lowWord() string {
	switch {
	case c.Low > 0 && c.Unknown > 0:
		return "Low or Unknown"
	case c.Unknown > 0:
		return "Unknown"
	}
	return "Low"
}

// GradeText is a grade as the window writes it beside or for its shield: the grade and the count
// that decides it, "D · 2 Critical", "C · OS end of life", "S · no known vulnerabilities",
// "N/A · not scanned".
func GradeText(g ImageGrade, c VulnerabilityCounts, eosl bool) string {
	n, what := c.basis(g, eosl)
	if n > 0 {
		what = strconv.FormatInt(n, 10) + " " + what
	}
	return g.String() + " · " + what
}

// GradeReason is the rule a grade stands for, as the drawer's card says it under the grade.
func GradeReason(g ImageGrade, c VulnerabilityCounts, eosl bool) string {
	switch g {
	case GradeS:
		return "No known vulnerabilities, and the OS is supported"
	case GradeA:
		return "Only Low or Unknown, at most " + strconv.Itoa(GradeAMaxLow)
	case GradeB:
		if c.Medium > 0 {
			return "Medium found, no High or Critical"
		}
		return "More than " + strconv.Itoa(GradeAMaxLow) + " Low or Unknown"
	case GradeC:
		if eosl && c.counted() < GradeC {
			return "OS end of life: no more security updates, the scan may miss vulnerabilities"
		}
		return "High found, no Critical"
	case GradeD:
		return "1 to " + strconv.Itoa(GradeDMaxCritical) + " Critical"
	case GradeE:
		return "More than " + strconv.Itoa(GradeDMaxCritical) + " Critical"
	}
	return "No Trivy report for this image"
}

// GradeRank is a grade's sort key: the grade, then the count that decides it, so that "D · 4
// Critical" sorts above "D · 2 Critical", which sorts above "C · 120 High", and N/A below S. None is 0.
func GradeRank(g ImageGrade, c VulnerabilityCounts, eosl bool) int64 {
	if !g.valid() {
		return 0
	}
	n, _ := c.basis(g, eosl)
	return int64(g)*rankStep + min(n, rankStep-1)
}

// rankStep separates the grades in GradeRank: a count beyond it is cut there.
const rankStep = 10_000_000

// GradeCard is a container's image grade in the container's section of a pod's drawer (D-113): the
// grade and its text and rule, the counts (nil for N/A), the report's OS and whether it is at its
// end of life, when it was scanned ("3h ago (<a time mark>)", written by TimeFormat.WriteSections),
// and the report's object. The drawer draws the grade as a shield beside the container's name and
// the card under its items; agents read it in object_get as it is.
type GradeCard struct {
	Grade   ImageGrade           `json:"grade"`
	Text    string               `json:"text"`
	Reason  string               `json:"reason"`
	Counts  *VulnerabilityCounts `json:"counts,omitempty"`
	OS      string               `json:"os,omitempty"`
	EOSL    bool                 `json:"eosl,omitempty"`
	Scanned string               `json:"scanned,omitempty"`
	Report  *ObjectRef           `json:"report,omitempty"`
}

// VulnerabilityReport is what Cogpit reads of one Trivy Operator VulnerabilityReport: the workload
// and container it is of, the image it scanned, its OS, the counts and when. The vulnerabilities themselves
// are not read: the report's own view and YAML show them.
type VulnerabilityReport struct {
	// Namespace and Name name the report object.
	Namespace string
	Name      string
	// Kind, Workload and Container name what was scanned: the workload's kind ("ReplicaSet",
	// "StatefulSet", "CronJob", "Pod"), its name, and the container's name in its pod template.
	Kind      string
	Workload  string
	Container string
	// Registry, Repository, Tag and Digest are the image scanned: "index.docker.io",
	// "library/nginx", "1.16.1", "sha256:…".
	Registry   string
	Repository string
	Tag        string
	Digest     string
	Counts     VulnerabilityCounts
	// OSFamily and OSName are the image's OS as Trivy found it ("alpine", "3.20.10"; empty for none),
	// EOSL whether that OS is at its end of life (report.os.eosl, written only when true).
	OSFamily string
	OSName   string
	EOSL     bool
	// Scanned is report.updateTimestamp, else the object's creation.
	Scanned time.Time
}

// ParseVulnerabilityReport reads a report object; ok is false for one that names no workload or
// container (not Trivy Operator's, or a shape it does not write).
func ParseVulnerabilityReport(obj *unstructured.Unstructured) (VulnerabilityReport, bool) {
	if obj == nil {
		return VulnerabilityReport{}, false
	}
	labels, ann := obj.GetLabels(), obj.GetAnnotations()
	r := VulnerabilityReport{
		Namespace: obj.GetNamespace(), Name: obj.GetName(),
		Kind: labels[TrivyLabelResourceKind], Workload: labels[TrivyLabelResourceName],
		Container: labels[TrivyLabelContainerName],
	}
	if r.Workload == "" {
		r.Workload = ann[TrivyLabelResourceName] // a name too long for a label
	}
	if r.Workload == "" { // the report's controller: the workload it was made for
		for _, o := range obj.GetOwnerReferences() {
			if o.Controller != nil && *o.Controller && o.Kind == r.Kind {
				r.Workload = o.Name
			}
		}
	}
	if ns := labels[TrivyLabelResourceNamespace]; ns != "" && r.Namespace == "" {
		r.Namespace = ns
	}
	if r.Kind == "" || r.Workload == "" || r.Container == "" {
		return VulnerabilityReport{}, false
	}
	report := NestedMap(obj.Object, "report")
	r.Registry = NestedString(report, "registry", "server")
	r.Repository = NestedString(report, "artifact", "repository")
	r.Tag = NestedString(report, "artifact", "tag")
	r.Digest = NestedString(report, "artifact", "digest")
	r.OSFamily = NestedString(report, "os", "family")
	r.OSName = NestedString(report, "os", "name")
	r.EOSL, _ = NestedBool(report, "os", "eosl")
	summary := NestedMap(report, "summary")
	r.Counts = VulnerabilityCounts{
		Critical: count(summary, "criticalCount"), High: count(summary, "highCount"),
		Medium: count(summary, "mediumCount"), Low: count(summary, "lowCount"),
		Unknown: count(summary, "unknownCount"),
	}
	if r.Scanned = NestedTime(report, "updateTimestamp"); r.Scanned.IsZero() {
		r.Scanned = obj.GetCreationTimestamp().Time
	}
	return r, true
}

// count reads a summary's count; a missing, negative or malformed one is 0.
func count(summary map[string]any, field string) int64 {
	n, _ := NestedInt64(summary, field)
	return max(n, 0)
}

// Grade is the report's image's grade.
func (r VulnerabilityReport) Grade() ImageGrade { return r.Counts.Grade(r.EOSL) }

// OS is the image's OS as Trivy names it: "alpine 3.20.10"; "" when it found none.
func (r VulnerabilityReport) OS() string { return strings.TrimSpace(r.OSFamily + " " + r.OSName) }

// Image is the scanned image as one reference: "library/nginx:1.16.1", the repository and its tag,
// else its digest.
func (r VulnerabilityReport) Image() string {
	switch {
	case r.Tag != "":
		return r.Repository + ":" + r.Tag
	case r.Digest != "":
		return r.Repository + "@" + r.Digest
	}
	return r.Repository
}

// Matches reports whether the report can be of a container's image (its spec's image reference):
// the same repository, and the same tag when both name one. Trivy Operator replaces a report when
// the workload's pod template changes, so a report of another image is one not replaced yet: its
// rating is not the container's. The registry is not compared (Trivy writes Docker Hub as
// index.docker.io, a mirror as itself), nor the digest (an image index's and a platform's differ).
func (r VulnerabilityReport) Matches(image string) bool {
	repo, tag := ImageRepository(image)
	if repo == "" || r.Repository == "" {
		return true // nothing to compare
	}
	if repo != strings.TrimPrefix(r.Repository, "library/") {
		return false
	}
	return tag == "" || r.Tag == "" || tag == r.Tag
}

// ImageRepository splits a container's image reference into its repository, without the registry
// and without Docker Hub's "library/", and its tag: "nginx:1.16.1" and
// "docker.io/library/nginx:1.16.1@sha256:…" are ("nginx", "1.16.1"); "ghcr.io/org/app" is ("org/app",
// ""). An image without a tag or a digest is pulled as "latest".
func ImageRepository(image string) (repo, tag string) {
	image = strings.TrimSpace(image)
	digest := ""
	if i := strings.IndexByte(image, '@'); i >= 0 {
		image, digest = image[:i], image[i+1:]
	}
	if i := strings.LastIndexByte(image, ':'); i > strings.LastIndexByte(image, '/') {
		image, tag = image[:i], image[i+1:]
	}
	if i := strings.IndexByte(image, '/'); i >= 0 {
		if host := image[:i]; strings.ContainsAny(host, ".:") || host == "localhost" {
			image = image[i+1:]
		}
	}
	if tag == "" && digest == "" && image != "" {
		tag = "latest"
	}
	return strings.TrimPrefix(image, "library/"), tag
}
