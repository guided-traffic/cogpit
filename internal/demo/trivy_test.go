package demo

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The demo's VulnerabilityReports: one per container of every Deployment's ReplicaSet but those
// stuck on an image that cannot be pulled, each naming its workload and container as Trivy
// Operator does and matching the image its pods run; the summary counts the list; the same image
// scans the same wherever it runs; the same seed draws the same reports; some CVEs are in many
// images; some running images have no report.
func TestDemoVulnerabilityReports(t *testing.T) {
	c := newTestCluster(t, Options{Seed: 7, Pods: 2_000, Deployments: 120})
	reports := c.store[kindVulnerabilityReports]
	if len(reports) < 120 {
		t.Fatalf("%d reports for 120 deployments", len(reports))
	}
	byImage := map[string]string{}
	images := map[string]map[string]bool{} // CVE → images
	findings := 0
	for key, obj := range reports {
		r, ok := model.ParseVulnerabilityReport(obj)
		if !ok || r.Kind != "ReplicaSet" || r.Container == "" {
			t.Fatalf("%s: %+v", key, r)
		}
		list := model.NestedSlice(obj.Object, "report", "vulnerabilities")
		findings += len(list)
		var counts model.VulnerabilityCounts
		for _, v := range list {
			m := v.(map[string]any)
			for _, f := range []string{"vulnerabilityID", "resource", "installedVersion", "severity", "title"} {
				if model.NestedString(m, f) == "" {
					t.Fatalf("%s: a vulnerability without %s: %v", key, f, m)
				}
			}
			switch model.NestedString(m, "severity") {
			case "CRITICAL":
				counts.Critical++
			case "HIGH":
				counts.High++
			case "MEDIUM":
				counts.Medium++
			case "LOW":
				counts.Low++
			default:
				counts.Unknown++
			}
			id := model.NestedString(m, "vulnerabilityID")
			if images[id] == nil {
				images[id] = map[string]bool{}
			}
			images[id][r.Image()] = true
		}
		if counts != r.Counts {
			t.Errorf("%s: summary %+v, list %+v", key, r.Counts, counts)
		}
		js, _ := json.Marshal(list)
		if prev, ok := byImage[r.Image()]; ok && prev != string(js) {
			t.Errorf("%s scans otherwise in %s", r.Image(), key)
		}
		byImage[r.Image()] = string(js)
	}
	if findings < 20*len(reports)/2 {
		t.Errorf("%d findings in %d reports", findings, len(reports))
	}
	widest := 0
	for _, imgs := range images {
		widest = max(widest, len(imgs))
	}
	if widest < 10 {
		t.Errorf("the most widespread CVE is in %d images", widest)
	}

	// Each running pod of a scanned ReplicaSet matches its report; a stuck rollout's has none.
	unscanned := 0
	for _, d := range c.w.deploys {
		rs := d.name + "-" + d.rsHash[0]
		for _, ct := range d.templates[0].containers {
			obj, ok := reports[objKey(d.namespace, "replicaset-"+rs+"-"+ct.name)]
			if !ok {
				unscanned++
				if d.current == 0 {
					t.Errorf("%s/%s: no report", rs, ct.name)
				}
				continue
			}
			if r, _ := model.ParseVulnerabilityReport(obj); !r.Matches(ct.image) {
				t.Errorf("%s/%s: report of %s for %s", rs, ct.name, r.Image(), ct.image)
			}
		}
	}
	if unscanned == 0 {
		t.Error("every Deployment's images are scanned")
	}

	again := newTestCluster(t, Options{Seed: 7, Pods: 2_000, Deployments: 120})
	if !sameStores(t, c, again) {
		t.Error("the same seed drew other reports")
	}
	keys := func(c *Cluster) []string {
		var out []string
		for k := range c.store[kindPods] {
			out = append(out, k)
		}
		slices.Sort(out)
		return out
	}
	if !slices.Equal(keys(c), keys(again)) {
		t.Error("the pods differ")
	}
}
