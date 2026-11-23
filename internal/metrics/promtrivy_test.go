package metrics_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/metrics"
)

func sev(severity string, vals ...float64) promTestSeries {
	return s(map[string]string{"severity": severity}, vals...)
}

// A month's curve is one range query of 30 daily points ending now, each image counted once
// (the digest, else the repository and tag) and only in the namespaces asked for; the points are
// the findings by severity, a time without a series has none, and nothing else is asked.
func TestTrivyFindings(t *testing.T) {
	c := newPromCluster(t, false)
	day := float64(24 * 3600)
	// The times are seconds before the query's end: -29 days is the first point, 0 the last.
	c.prom.onRel("sum by (severity)",
		sev("Critical", -29*day, 4, -1*day, 3, 0, 2),
		sev("High", -29*day, 10, 0, 7),
		sev("Medium", 0, 0),
		sev("Low", -29*day, 1.4),
		sev("Unknown", 0, 1),
		sev("Bogus", 0, 5), // counts as Unknown
	)
	prov := alertProm(t, c)
	before := len(c.prom.requests())

	now := time.Unix(1_800_000_000, 0).Add(37 * time.Second)
	got, err := prov.TrivyFindings(context.Background(), now, 30, []string{"shop", "pay.ments", "shop"})
	if err != nil {
		t.Fatal(err)
	}
	end := now.Truncate(time.Minute)
	if !got.Scraped || got.Step != 24*time.Hour || !got.AsOf.Equal(end) {
		t.Fatalf("answer %+v", got)
	}
	want := []metrics.TrivyFindingsPoint{
		{At: end.Add(-29 * 24 * time.Hour), Critical: 4, High: 10, Low: 1},
		{At: end.Add(-24 * time.Hour), Critical: 3},
		{At: end, Critical: 2, High: 7, Unknown: 6},
	}
	if len(got.Points) != len(want) {
		t.Fatalf("points %+v", got.Points)
	}
	for i, p := range got.Points {
		if p != want[i] {
			t.Errorf("point %d: %+v, want %+v", i, p, want[i])
		}
	}
	if got.Points[2].Total() != 15 {
		t.Errorf("total %d", got.Points[2].Total())
	}

	reqs := c.prom.requests()[before:]
	if len(reqs) != 1 || reqs[0].path != "/api/v1/query_range" {
		t.Fatalf("requests %+v", reqs)
	}
	r := reqs[0]
	if r.end != strconv.FormatInt(end.Unix(), 10) || r.start != strconv.FormatInt(end.Add(-29*24*time.Hour).Unix(), 10) || r.step != "86400" {
		t.Errorf("range %s..%s step %s", r.start, r.end, r.step)
	}
	wantQuery := `sum by (severity) (` +
		`max by (image_digest, severity) (trivy_image_vulnerabilities{namespace=~"(pay\\.ments|shop)",image_digest!=""}) or ` +
		`max by (image_repository, image_tag, severity) (trivy_image_vulnerabilities{namespace=~"(pay\\.ments|shop)",image_digest=""}))`
	if r.query != wantQuery {
		t.Errorf("query\n%s\nwant\n%s", r.query, wantQuery)
	}
}

// A week is 28 points 6 hours apart; every namespace names none.
func TestTrivyFindingsWeek(t *testing.T) {
	c := newPromCluster(t, false)
	c.prom.onRel("sum by (severity)", sev("High", -27*6*3600, 2, 0, 1))
	prov := alertProm(t, c)
	before := len(c.prom.requests())
	now := time.Unix(1_800_000_000, 0)
	got, err := prov.TrivyFindings(context.Background(), now, 7, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Step != 6*time.Hour || len(got.Points) != 2 || got.Points[0].High != 2 || !got.Points[0].At.Equal(now.Add(-27*6*time.Hour)) {
		t.Fatalf("answer %+v", got)
	}
	r := c.prom.requests()[before]
	if r.step != "21600" || strings.Contains(r.query, "namespace") {
		t.Errorf("step %s, query %s", r.step, r.query)
	}
}

// No series: an instant query tells a Prometheus that does not scrape Trivy Operator (no point,
// not scraped) from one that has no image in the namespaces (no point, scraped).
func TestTrivyFindingsNotScraped(t *testing.T) {
	c := newPromCluster(t, false)
	prov := alertProm(t, c)
	before := len(c.prom.requests())
	got, err := prov.TrivyFindings(context.Background(), time.Now(), 90, []string{"shop"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Scraped || len(got.Points) != 0 || got.Step != 24*time.Hour {
		t.Fatalf("not scraped: %+v", got)
	}
	reqs := c.prom.requests()[before:]
	if len(reqs) != 2 || reqs[1].path != "/api/v1/query" || reqs[1].query != "count(trivy_image_vulnerabilities)" {
		t.Fatalf("requests %+v", reqs)
	}

	c.prom.onRel("count(trivy_image_vulnerabilities)", s(nil, 0, 120))
	got, err = prov.TrivyFindings(context.Background(), time.Now(), 90, []string{"shop"})
	if err != nil || !got.Scraped || len(got.Points) != 0 {
		t.Fatalf("scraped, no image here: %+v, %v", got, err)
	}
}

// A refusal is the answer's error, and it names the request; namespaces too many for a query are
// refused before anything is sent.
func TestTrivyFindingsFailure(t *testing.T) {
	c := newPromCluster(t, false)
	prov := alertProm(t, c)
	c.prom.mu.Lock()
	c.prom.status = 503
	c.prom.mu.Unlock()
	_, err := prov.TrivyFindings(context.Background(), time.Now(), 30, nil)
	if err == nil || !strings.Contains(err.Error(), "query_range of trivy_image_vulnerabilities over 30 days") {
		t.Fatalf("error %v", err)
	}

	many := make([]string, 600)
	for i := range many {
		many[i] = "namespace-of-a-long-name-" + strconv.Itoa(i)
	}
	before := len(c.prom.requests())
	if _, err := prov.TrivyFindings(context.Background(), time.Now(), 30, many); err == nil || !strings.Contains(err.Error(), "too many namespaces") {
		t.Fatalf("error %v", err)
	}
	if len(c.prom.requests()) != before {
		t.Error("a query too long was sent")
	}
}

func TestTrivyFindingsStep(t *testing.T) {
	for days, want := range map[int]time.Duration{7: 6 * time.Hour, 30: 24 * time.Hour, 90: 24 * time.Hour} {
		if got := metrics.TrivyFindingsStep(days); got != want {
			t.Errorf("%d days: %v", days, got)
		}
	}
}
