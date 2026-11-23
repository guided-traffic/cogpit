package testkit

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Trivy Operator fixtures for the Image Security page: its VulnerabilityReport definition, reports
// with vulnerability lists shaped as Trivy Operator writes them (synthetic ids, packages and
// versions), and a cluster of running pods some of whose images have a report.

// VulnReportsGVR is Trivy Operator's VulnerabilityReport resource.
var VulnReportsGVR = schema.GroupVersionResource{Group: "aquasecurity.github.io", Version: "v1alpha1", Resource: "vulnerabilityreports"}

// VulnReportsCRD is the VulnerabilityReport definition, established, served at v1alpha1.
func VulnReportsCRD() *unstructured.Unstructured {
	return CRD("aquasecurity.github.io", "vulnerabilityreports", "VulnerabilityReport", true,
		CRDVersion{Name: "v1alpha1", Served: true, Storage: true})
}

// Vuln is one entry of a report's vulnerability list: the id, Trivy's severity word
// ("CRITICAL"), the package, its installed and fixed version ("" without a fix) and the score (0:
// none).
type Vuln struct {
	ID, Severity, Package, Installed, Fixed string
	Score                                   float64
}

// VulnReport builds the report of a workload's container: the workload's kind ("ReplicaSet",
// "StatefulSet", "Pod"), its name, the container, the image as repository and tag, the summary's
// counts (critical, high, medium, low, unknown) and the vulnerability list.
func VulnReport(namespace, kind, workload, container, repo, tag string, counts [5]int64, vulns ...Vuln) *unstructured.Unstructured {
	group := map[string]string{"ReplicaSet": "apps/v1", "StatefulSet": "apps/v1", "DaemonSet": "apps/v1", "Job": "batch/v1", "CronJob": "batch/v1", "Pod": "v1"}[kind]
	list := make([]any, 0, len(vulns))
	for _, v := range vulns {
		m := map[string]any{
			"vulnerabilityID": v.ID, "severity": v.Severity, "resource": v.Package, "installedVersion": v.Installed,
			"fixedVersion": v.Fixed, "title": v.Package + ": synthetic vulnerability", "publishedDate": "2024-03-01T00:00:00Z",
			"lastModifiedDate": "2024-03-02T00:00:00Z", "links": []any{}, "target": "",
		}
		if v.Score > 0 {
			m["score"] = v.Score
		}
		list = append(list, m)
	}
	return Object("aquasecurity.github.io/v1alpha1", "VulnerabilityReport", namespace, strings.ToLower(kind)+"-"+workload+"-"+container, map[string]any{
		"metadata": map[string]any{
			"labels": map[string]any{
				"app.kubernetes.io/managed-by": "trivy-operator", "resource-spec-hash": "85f6f8bfb6",
				"trivy-operator.container.name": container, "trivy-operator.resource.kind": kind,
				"trivy-operator.resource.name": workload, "trivy-operator.resource.namespace": namespace,
			},
			"ownerReferences": []any{map[string]any{"apiVersion": group, "kind": kind, "name": workload, "uid": "u", "controller": true}},
		},
		"report": map[string]any{
			"artifact": map[string]any{"repository": repo, "tag": tag, "digest": ImageDigest(repo + ":" + tag)},
			"registry": map[string]any{"server": "index.docker.io"},
			"scanner":  map[string]any{"name": "Trivy", "vendor": "Aqua Security", "version": "0.75.0"},
			"summary": map[string]any{
				"criticalCount": counts[0], "highCount": counts[1], "mediumCount": counts[2], "lowCount": counts[3],
				"unknownCount": counts[4], "noneCount": int64(0),
			},
			"updateTimestamp": "2026-10-17T09:55:22Z",
			"vulnerabilities": list,
		},
	})
}

// TrivyPod is a running pod of containers "name=image", controlled by the owner (apiVersion,
// kind, name) unless that is empty.
func TrivyPod(namespace, name, phase string, owner []string, containers ...string) *unstructured.Unstructured {
	cs := make([]any, 0, len(containers))
	for _, c := range containers {
		n, image, _ := strings.Cut(c, "=")
		cs = append(cs, map[string]any{"name": n, "image": image})
	}
	meta := map[string]any{}
	if len(owner) == 3 {
		meta["ownerReferences"] = []any{map[string]any{"apiVersion": owner[0], "kind": owner[1], "name": owner[2], "uid": "uid-" + owner[2], "controller": true}}
	}
	return Object("v1", "Pod", namespace, name, map[string]any{"metadata": meta, "spec": map[string]any{"containers": cs}, "status": map[string]any{"phase": phase}})
}

// ImageSecuritySource is a cluster running Trivy Operator for the Image Security page:
//
//   - shop: Deployment api (ReplicaSet api-5d9c4b7f8, container app, shop/api:2.4.1, graded D · 3
//     Critical), StatefulSet db (container postgres, library/postgres:13.4, graded D · 1 Critical),
//     and Deployment web's pod of nginx:1.21 with no report;
//   - team: a bare pod (container shell, library/alpine:3.12, graded C) and a finished one;
//   - CVE-2024-0001 Critical in two images and two of api's packages; CVE-2024-0003 High in api
//     (fixed) and the bare pod (no fix); CVE-2024-0004 Medium in api, High in the bare pod.
//
// Eleven findings, eight of them High or worse; seven CVE ids.
func ImageSecuritySource() *MemorySource {
	src := NewMemorySource()
	src.Set(CRDsGVR, VulnReportsCRD())
	for _, ns := range []string{"shop", "team"} {
		src.Set(NamespacesGVR, Namespace(ns))
	}
	for _, p := range []*unstructured.Unstructured{
		TrivyPod("shop", "api-5d9c4b7f8-x2x4z", "Running", []string{"apps/v1", "ReplicaSet", "api-5d9c4b7f8"}, "app=registry.example.com/shop/api:2.4.1"),
		TrivyPod("shop", "db-0", "Running", []string{"apps/v1", "StatefulSet", "db"}, "postgres=postgres:13.4"),
		TrivyPod("shop", "web-6b7c8d9f4-k2v5n", "Running", []string{"apps/v1", "ReplicaSet", "web-6b7c8d9f4"}, "nginx=nginx:1.21"),
		TrivyPod("team", "bare", "Running", nil, "shell=docker.io/library/alpine:3.12"),
		TrivyPod("team", "done", "Succeeded", nil, "job=busybox:1.37.0"),
	} {
		src.Set(PodsGVR, p)
	}
	for _, r := range []*unstructured.Unstructured{
		VulnReport("shop", "ReplicaSet", "api-5d9c4b7f8", "app", "shop/api", "2.4.1", [5]int64{3, 1, 1, 1, 0},
			Vuln{"CVE-2024-0001", "CRITICAL", "libcurl4", "7.88.1-10", "8.4.0", 9.8},
			Vuln{"CVE-2024-0001", "CRITICAL", "curl", "7.88.1-10", "8.4.0", 9.8},
			Vuln{"CVE-2024-0002", "CRITICAL", "golang.org/x/crypto", "0.21.0", "0.31.0", 9.1},
			Vuln{"CVE-2024-0003", "HIGH", "golang.org/x/net", "0.10.0", "0.17.0", 7.5},
			Vuln{"CVE-2024-0004", "MEDIUM", "openssl", "3.0.11-1", "", 5.3},
			Vuln{"CVE-2024-0005", "LOW", "zlib1g", "1.2.13", "", 0}),
		VulnReport("shop", "StatefulSet", "db", "postgres", "library/postgres", "13.4", [5]int64{1, 1, 0, 0, 1},
			Vuln{"CVE-2024-0001", "CRITICAL", "libcurl4", "7.74.0-1.3", "7.74.0-1.3+deb11u7", 9.8},
			Vuln{"CVE-2024-0006", "HIGH", "libc6", "2.31-13", "", 7.8},
			Vuln{"CVE-2024-0007", "UNKNOWN", "libgnutls30", "3.7.1-5", "", 0}),
		VulnReport("team", "Pod", "bare", "shell", "library/alpine", "3.12", [5]int64{0, 2, 0, 0, 0},
			Vuln{"CVE-2024-0003", "HIGH", "golang.org/x/net", "0.9.0", "", 7.5},
			Vuln{"CVE-2024-0004", "HIGH", "openssl", "1.1.1k-r0", "", 7.4}),
	} {
		src.Set(VulnReportsGVR, r)
	}
	return src
}

// ImageDigest is the synthetic digest of an image reference, one per reference: "sha256:<64 hex>".
func ImageDigest(ref string) string {
	sum := sha256.Sum256([]byte(ref))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// WithDigest sets a report's image digest, for an image whose tag was pushed again.
func WithDigest(report *unstructured.Unstructured, digest string) *unstructured.Unstructured {
	_ = unstructured.SetNestedField(report.Object, digest, "report", "artifact", "digest")
	return report
}
