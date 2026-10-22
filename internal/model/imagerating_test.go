package model

import (
	"encoding/json"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// trivyReport is a VulnerabilityReport shaped as Trivy Operator 0.35.0 writes it (kind,
// 2026-10-17): synthetic names, the vulnerabilities left out.
func trivyReport(kind, workload, container string, summary map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "aquasecurity.github.io/v1alpha1", "kind": "VulnerabilityReport",
		"metadata": map[string]any{
			"name": "replicaset-web-5d9c-app", "namespace": "shop", "creationTimestamp": "2026-10-17T09:00:00Z",
			"labels": map[string]any{
				"app.kubernetes.io/managed-by": "trivy-operator", "resource-spec-hash": "85f6f8bfb6",
				TrivyLabelContainerName: container, TrivyLabelResourceKind: kind,
				TrivyLabelResourceName: workload, TrivyLabelResourceNamespace: "shop",
			},
			"ownerReferences": []any{map[string]any{
				"apiVersion": "apps/v1", "kind": kind, "name": workload, "uid": "u-1", "controller": true,
			}},
		},
		"report": map[string]any{
			"artifact": map[string]any{"repository": "library/nginx", "tag": "1.16.1", "digest": "sha256:d20a"},
			"registry": map[string]any{"server": "index.docker.io"},
			"scanner":  map[string]any{"name": "Trivy", "vendor": "Aqua Security", "version": "0.75.0"},
			"summary":  summary, "updateTimestamp": "2026-10-17T09:55:22Z",
			"vulnerabilities": []any{},
		},
	}}
}

// TestImageGrades checks the owner's rules (2026-10-22) on the counts Trivy 0.69.3 found in real
// images the same day (the Image Rating Lab's sample) and at each threshold.
func TestImageGrades(t *testing.T) {
	cases := []struct {
		name string
		c    VulnerabilityCounts
		eosl bool
		want ImageGrade
		text string
	}{
		{"redis:8-alpine", VulnerabilityCounts{}, false, GradeS, "S · no known vulnerabilities"},
		{"busybox:1.37 (no package database)", VulnerabilityCounts{}, false, GradeS, "S · no known vulnerabilities"},
		{"distroless/static", VulnerabilityCounts{Unknown: 1}, false, GradeA, "A · 1 Unknown"},
		{"ubuntu:24.04", VulnerabilityCounts{Medium: 9, Low: 4}, false, GradeB, "B · 9 Medium"},
		{"golang:1.25-alpine", VulnerabilityCounts{High: 2, Medium: 7, Low: 12}, false, GradeC, "C · 2 High"},
		{"nginx:1.29-alpine", VulnerabilityCounts{High: 43, Medium: 83, Low: 50, Unknown: 2}, false, GradeC, "C · 43 High"},
		{"node-exporter", VulnerabilityCounts{Critical: 1, High: 39, Medium: 25, Low: 1, Unknown: 2}, false, GradeD, "D · 1 Critical"},
		{"prometheus", VulnerabilityCounts{Critical: 4, High: 92, Medium: 61, Low: 7, Unknown: 4}, false, GradeD, "D · 4 Critical"},
		{"velero", VulnerabilityCounts{Critical: 6, High: 135, Medium: 148, Low: 63, Unknown: 4}, false, GradeE, "E · 6 Critical"},
		{"alpine:3.20 (end of life)", VulnerabilityCounts{}, true, GradeC, "C · OS end of life"},
		// The thresholds.
		{"A at most", VulnerabilityCounts{Low: 6, Unknown: 4}, false, GradeA, "A · 10 Low or Unknown"},
		{"B beyond", VulnerabilityCounts{Low: 11}, false, GradeB, "B · 11 Low"},
		{"D at most", VulnerabilityCounts{Critical: 5}, false, GradeD, "D · 5 Critical"},
		{"end of life keeps worse", VulnerabilityCounts{Critical: 2}, true, GradeD, "D · 2 Critical"},
		{"end of life over High", VulnerabilityCounts{High: 3}, true, GradeC, "C · 3 High"},
	}
	for _, c := range cases {
		g := c.c.Grade(c.eosl)
		if g != c.want || GradeText(g, c.c, c.eosl) != c.text {
			t.Errorf("%s: %v %q", c.name, g, GradeText(g, c.c, c.eosl))
		}
		if GradeReason(g, c.c, c.eosl) == "" {
			t.Errorf("%s: no reason", c.name)
		}
	}
	if r := GradeReason(GradeC, VulnerabilityCounts{}, true); r != "OS end of life: no more security updates, the scan may miss vulnerabilities" {
		t.Errorf("end of life's reason %q", r)
	}
	if r := GradeReason(GradeC, VulnerabilityCounts{High: 3}, true); r != "High found, no Critical" {
		t.Errorf("High's reason under an end of life %q", r)
	}
	if GradeText(GradeNA, VulnerabilityCounts{}, false) != "N/A · not scanned" || GradeReason(GradeNA, VulnerabilityCounts{}, false) != "No Trivy report for this image" {
		t.Error("N/A")
	}
	if s := (VulnerabilityCounts{Critical: 2, High: 45, Medium: 28, Low: 4, Unknown: 3}).Summary(); s != "2 Critical · 45 High · 28 Medium · 4 Low · 3 Unknown" {
		t.Errorf("summary %q", s)
	}
	// The rank: the grade, then the count that decides it; N/A below S; none 0.
	ranks := []int64{
		GradeRank(GradeNone, VulnerabilityCounts{}, false),
		GradeRank(GradeNA, VulnerabilityCounts{}, false),
		GradeRank(GradeS, VulnerabilityCounts{}, false),
		GradeRank(GradeA, VulnerabilityCounts{Low: 2}, false),
		GradeRank(GradeC, VulnerabilityCounts{}, true),
		GradeRank(GradeC, VulnerabilityCounts{High: 120}, false),
		GradeRank(GradeD, VulnerabilityCounts{Critical: 2}, false),
		GradeRank(GradeD, VulnerabilityCounts{Critical: 4}, false),
		GradeRank(GradeE, VulnerabilityCounts{Critical: 1 << 40}, false),
	}
	for i := 1; i < len(ranks); i++ {
		if ranks[i] <= ranks[i-1] {
			t.Errorf("ranks out of order: %v", ranks)
		}
	}
	if ranks[0] != 0 || GradeRank(ImageGrade(42), VulnerabilityCounts{}, false) != 0 || ImageGrade(42).String() != "" {
		t.Errorf("none ranks %d", ranks[0])
	}
	b, _ := json.Marshal(map[string]ImageGrade{"g": GradeNA, "h": GradeD})
	var back map[string]ImageGrade
	if string(b) != `{"g":"N/A","h":"D"}` || json.Unmarshal(b, &back) != nil || back["g"] != GradeNA || back["h"] != GradeD {
		t.Errorf("JSON %s, %v", b, back)
	}
	// A cell without a grade carries none in JSON.
	if b, _ := json.Marshal(Cell{Text: "x"}); string(b) != `{"text":"x"}` {
		t.Errorf("cell JSON %s", b)
	}
}

func TestParseVulnerabilityReport(t *testing.T) {
	obj := trivyReport("ReplicaSet", "web-5d9c", "app", map[string]any{
		"criticalCount": int64(1), "highCount": int64(2), "mediumCount": float64(3), "lowCount": int64(-4), "noneCount": int64(0),
	})
	r, ok := ParseVulnerabilityReport(obj)
	want := VulnerabilityCounts{Critical: 1, High: 2, Medium: 3}
	if !ok || r.Kind != "ReplicaSet" || r.Workload != "web-5d9c" || r.Container != "app" || r.Namespace != "shop" ||
		r.Name != "replicaset-web-5d9c-app" || r.Counts != want || r.Registry != "index.docker.io" ||
		r.Image() != "library/nginx:1.16.1" || !r.Scanned.Equal(time.Date(2026, 10, 17, 9, 55, 22, 0, time.UTC)) {
		t.Fatalf("%+v, %v", r, ok)
	}

	if r.OS() != "" || r.EOSL {
		t.Errorf("no OS: %q %v", r.OS(), r.EOSL)
	}
	// The OS and its end of life (report.os, eosl written only when true).
	_ = unstructured.SetNestedMap(obj.Object, map[string]any{"family": "alpine", "name": "3.20.10", "eosl": true}, "report", "os")
	if r, _ := ParseVulnerabilityReport(obj); r.OS() != "alpine 3.20.10" || !r.EOSL || r.Grade() != GradeD { // a Critical: worse than the end of life's C
		t.Errorf("OS %q, end of life %v, grade %v", r.OS(), r.EOSL, r.Grade())
	}
	// A workload name too long for a label: the annotation of the same key.
	long := trivyReport("StatefulSet", "", "db", nil)
	labels := long.GetLabels()
	delete(labels, TrivyLabelResourceName)
	labels[TrivyLabelResourceNameHash] = "6b8f"
	long.SetLabels(labels)
	long.SetAnnotations(map[string]string{TrivyLabelResourceName: "a-statefulset-whose-name-is-much-longer-than-sixty-three-characters-x"})
	if r, ok := ParseVulnerabilityReport(long); !ok || r.Workload != "a-statefulset-whose-name-is-much-longer-than-sixty-three-characters-x" ||
		r.Counts != (VulnerabilityCounts{}) || r.Grade() != GradeS {
		t.Errorf("long name: %+v, %v", r, ok)
	}
	// Neither: the report's controller of that kind.
	noName := trivyReport("StatefulSet", "", "db", nil)
	unstructured.RemoveNestedField(noName.Object, "metadata", "labels", TrivyLabelResourceName)
	noName.SetOwnerReferences(nil)
	if _, ok := ParseVulnerabilityReport(noName); ok {
		t.Error("a report that names no workload was read")
	}
	owned := trivyReport("StatefulSet", "db", "db", nil)
	unstructured.RemoveNestedField(owned.Object, "metadata", "labels", TrivyLabelResourceName)
	if r, ok := ParseVulnerabilityReport(owned); !ok || r.Workload != "db" {
		t.Errorf("by its controller: %+v, %v", r, ok)
	}
	// No update time: the creation time.
	unstructured.RemoveNestedField(owned.Object, "report", "updateTimestamp")
	if r, _ := ParseVulnerabilityReport(owned); !r.Scanned.Equal(time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("scanned %v", r.Scanned)
	}
	// Hostile shapes never panic.
	for _, o := range []map[string]any{
		{}, {"report": "x"}, {"report": map[string]any{"summary": []any{1}}},
		{"metadata": map[string]any{"labels": map[string]any{TrivyLabelResourceKind: 1}}},
	} {
		ParseVulnerabilityReport(&unstructured.Unstructured{Object: o})
	}
	if _, ok := ParseVulnerabilityReport(nil); ok {
		t.Error("nil")
	}
}

func TestImageMatches(t *testing.T) {
	for _, c := range []struct {
		image, repo, tag string
	}{
		{"nginx:1.16.1", "nginx", "1.16.1"},
		{"docker.io/library/nginx:1.16.1", "nginx", "1.16.1"},
		{"nginx", "nginx", "latest"},
		{"nginx@sha256:abc", "nginx", ""},
		{"registry.example:5000/team/app:2.0@sha256:abc", "team/app", "2.0"},
		{"localhost/app", "app", "latest"},
		{"ghcr.io/org/app", "org/app", "latest"},
		{"bitnami/redis:7", "bitnami/redis", "7"},
	} {
		if repo, tag := ImageRepository(c.image); repo != c.repo || tag != c.tag {
			t.Errorf("%s: %q %q", c.image, repo, tag)
		}
	}
	r := VulnerabilityReport{Repository: "library/nginx", Tag: "1.16.1"}
	for image, want := range map[string]bool{
		"nginx:1.16.1": true, "docker.io/library/nginx:1.16.1": true, "nginx@sha256:abc": true,
		"nginx:1.17": false, "nginx": false, "httpd:1.16.1": false, "": true,
	} {
		if r.Matches(image) != want {
			t.Errorf("%q matches: %v", image, !want)
		}
	}
	if !(VulnerabilityReport{Repository: "org/app", Digest: "sha256:abc"}).Matches("ghcr.io/org/app@sha256:abc") {
		t.Error("by digest")
	}
}
