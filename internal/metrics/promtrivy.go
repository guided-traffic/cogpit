package metrics

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

// The Image Security page's findings over time (engine/imagesecurity_trend.go): Trivy Operator's
// VulnerabilityReports keep no history, so the curve comes from a Prometheus that scrapes the
// operator's metric trivy_image_vulnerabilities. Trivy Operator writes one series per report
// (a workload's container) and severity, its value the number of the report's vulnerabilities of
// that severity, with the labels namespace, name, resource_kind, resource_name, container_name,
// image_registry, image_repository, image_tag, image_digest and severity (Critical, High, Medium,
// Low, Unknown) (trivy-operator, pkg/metrics/collector.go, collectVulnerabilityReports). The page
// counts an image once however many workloads and replicas run it, so the query keeps one series
// per image and severity before it sums: an image is its digest, or its repository and tag when
// the report has no digest. The unit is a finding: a vulnerability of an image.
//
// The namespace label is the report's only when the scrape honours the metric's labels (Trivy
// Operator's chart sets honorLabels on its ServiceMonitor); a scrape that does not puts the
// operator's own namespace there, and a namespace filter then finds nothing.

// trivyMetric is Trivy Operator's metric of the vulnerabilities per report and severity.
const trivyMetric = "trivy_image_vulnerabilities"

// TrivyFindingsStep is the time between two points of a curve of days: 6 hours for a week (28
// points), a day for longer ranges (30 or 90 points). The reports change when an image is
// rescanned, about daily, so a finer step shows nothing more.
func TrivyFindingsStep(days int) time.Duration {
	if days <= 7 {
		return 6 * time.Hour
	}
	return 24 * time.Hour
}

// TrivyFindings is Prometheus's answer for the Image Security page's curve.
type TrivyFindings struct {
	// Scraped says the Prometheus has Trivy Operator's metric at all: in the range's points, or
	// now in any namespace. Without it Points is empty and the page says the metric is not scraped.
	Scraped bool
	// Points are the findings at each step, oldest first, the last at AsOf. A time at which the
	// Prometheus has no series of the namespaces (before its retention, a scrape gap) has no point.
	Points []TrivyFindingsPoint
	// Step is the time between two points (TrivyFindingsStep).
	Step time.Duration
	// AsOf is the last point's time: the request's time, cut to the minute.
	AsOf time.Time
}

// TrivyFindingsPoint is the findings at one time, by severity.
type TrivyFindingsPoint struct {
	At                                   time.Time
	Critical, High, Medium, Low, Unknown int
}

// Total is the point's findings of every severity.
func (p TrivyFindingsPoint) Total() int { return p.Critical + p.High + p.Medium + p.Low + p.Unknown }

// add counts n findings of a severity as Trivy Operator writes it; an unexpected word counts as
// Unknown.
func (p *TrivyFindingsPoint) add(severity string, n int) {
	switch strings.ToLower(severity) {
	case "critical":
		p.Critical += n
	case "high":
		p.High += n
	case "medium":
		p.Medium += n
	case "low":
		p.Low += n
	default:
		p.Unknown += n
	}
}

// trivyNamespaceMatcher is the matcher of the namespaces, "" for every namespace. The names are
// quoted for a regular expression and for the string literal.
func trivyNamespaceMatcher(namespaces []string) string {
	if len(namespaces) == 0 {
		return ""
	}
	ns := slices.Clone(namespaces)
	slices.Sort(ns)
	return "namespace=~" + quoteLabel(alternation(slices.Compact(ns)))
}

// trivyFindingsQuery sums the findings by severity, each image once: the series of an image with a
// digest grouped by the digest, those without one by repository and tag, the larger value of a
// group kept (the same image scans the same; the copies of a series a Prometheus pair or a second
// scrape of the operator adds are dropped alike).
func trivyFindingsQuery(namespaces []string) string {
	ns := trivyNamespaceMatcher(namespaces)
	byDigest := selector(trivyMetric, ns, `image_digest!=""`)
	byTag := selector(trivyMetric, ns, `image_digest=""`)
	return "sum by (severity) (max by (image_digest, severity) (" + byDigest + ") or max by (image_repository, image_tag, severity) (" + byTag + "))"
}

// trivyScrapedQuery asks whether the Prometheus has the metric now, in any namespace.
func trivyScrapedQuery() string { return "count(" + trivyMetric + ")" }

// errTrivyQueryTooLong says the namespaces do not fit a query.
var errTrivyQueryTooLong = errors.New("too many namespaces to name in a Prometheus query")

// TrivyFindings asks the Prometheus for the findings in the namespaces (empty: every namespace)
// over the last days: one range query of a point per TrivyFindingsStep ending at now. When it
// answers no series, an instant query tells a Prometheus that does not scrape Trivy Operator from
// namespaces it has no images of. An error names the request.
func (p *Prometheus) TrivyFindings(ctx context.Context, now time.Time, days int, namespaces []string) (TrivyFindings, error) {
	days = max(days, 1)
	step := TrivyFindingsStep(days)
	n := int(time.Duration(days) * 24 * time.Hour / step)
	end := now.Truncate(time.Minute)
	start := end.Add(-time.Duration(n-1) * step)
	out := TrivyFindings{Step: step, AsOf: end}
	expr := trivyFindingsQuery(namespaces)
	what := fmt.Sprintf("GET /api/v1/query_range of %s over %d days, a point every %s", trivyMetric, days, promDuration(step))
	if !fitsGroup(expr) {
		return out, fmt.Errorf("%s: %w (%d)", what, errTrivyQueryTooLong, len(namespaces))
	}
	series, err := p.queryRange(ctx, expr, start, end, step)
	if err != nil {
		return out, fmt.Errorf("%s: %w", what, err)
	}
	points := make([]TrivyFindingsPoint, n)
	has := make([]bool, n)
	for _, s := range series {
		for _, pt := range s.points {
			i := int(math.Round(float64(pt.t-start.UnixMilli()) / float64(step.Milliseconds())))
			if i < 0 || i >= n {
				continue
			}
			has[i] = true
			points[i].add(s.labels["severity"], int(math.Round(max(pt.v, 0))))
		}
	}
	for i := range points {
		if has[i] {
			points[i].At = start.Add(time.Duration(i) * step)
			out.Points = append(out.Points, points[i])
		}
	}
	if len(out.Points) > 0 {
		out.Scraped = true
		return out, nil
	}
	scraped, err := p.queryInstant(ctx, trivyScrapedQuery())
	if err != nil {
		return out, fmt.Errorf("GET /api/v1/query of %s: %w", trivyScrapedQuery(), err)
	}
	for _, s := range scraped {
		for _, pt := range s.points {
			out.Scraped = out.Scraped || pt.v > 0
		}
	}
	return out, nil
}
