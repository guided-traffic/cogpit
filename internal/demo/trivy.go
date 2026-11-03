package demo

import (
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utiljson "k8s.io/apimachinery/pkg/util/json"
)

// Trivy Operator's VulnerabilityReports (aquasecurity.github.io/v1alpha1), so that the demo
// cluster shows the Trivy integration: one report per Deployment's current ReplicaSet and
// container, shaped as Trivy Operator writes them (labels naming the workload and the container,
// the owner reference, the artifact, the OS, the summary, the vulnerability list). Everything in
// them is synthetic: the CVE ids, packages, versions and scores are drawn, not looked up, and
// stand for no real vulnerability. Images are drawn alike wherever they run: an image's list
// depends on the image alone, as a scan's does. The OS layers of the users' images share one
// catalogue, so that some CVEs are widespread; their Go modules draw from another. A Deployment
// whose rollout is stuck on an image that cannot be pulled has no report (Trivy cannot scan its
// current revision), nor do the pods started by hand and the CronJobs' runs: their images are the
// ones not scanned.
//
// The reports are drawn with a generator of their own, seeded from Options.Seed and each image,
// so that adding them leaves the rest of the cluster as it was.

// vulnReportsGVR is the reports' resource.
var vulnReportsGVR = schema.GroupVersionResource{Group: "aquasecurity.github.io", Version: "v1alpha1", Resource: "vulnerabilityreports"}

// trivyAPIVersion and trivyKind name the reports' kind, apiKindReplicaSet the workload they are of.
const (
	trivyAPIVersion   = "aquasecurity.github.io/v1alpha1"
	trivyKind         = "VulnerabilityReport"
	apiKindReplicaSet = "ReplicaSet"
)

// vulnReportsCRD is Trivy Operator's VulnerabilityReport definition, established, with its printer
// columns.
const vulnReportsCRD = `{
  "apiVersion": "apiextensions.k8s.io/v1", "kind": "CustomResourceDefinition",
  "metadata": {"name": "vulnerabilityreports.aquasecurity.github.io"},
  "spec": {
    "group": "aquasecurity.github.io", "scope": "Namespaced",
    "names": {"plural": "vulnerabilityreports", "singular": "vulnerabilityreport", "kind": "VulnerabilityReport",
      "listKind": "VulnerabilityReportList", "shortNames": ["vuln", "vulns"], "categories": ["all"]},
    "versions": [{
      "name": "v1alpha1", "served": true, "storage": true,
      "schema": {"openAPIV3Schema": {"type": "object", "x-kubernetes-preserve-unknown-fields": true}},
      "additionalPrinterColumns": [
        {"name": "Repository", "type": "string", "jsonPath": ".report.artifact.repository"},
        {"name": "Tag", "type": "string", "jsonPath": ".report.artifact.tag"},
        {"name": "Scanner", "type": "string", "jsonPath": ".report.scanner.name"},
        {"name": "Age", "type": "date", "jsonPath": ".metadata.creationTimestamp"},
        {"name": "Critical", "type": "integer", "jsonPath": ".report.summary.criticalCount", "priority": 1},
        {"name": "High", "type": "integer", "jsonPath": ".report.summary.highCount", "priority": 1},
        {"name": "Medium", "type": "integer", "jsonPath": ".report.summary.mediumCount", "priority": 1},
        {"name": "Low", "type": "integer", "jsonPath": ".report.summary.lowCount", "priority": 1},
        {"name": "Unknown", "type": "integer", "jsonPath": ".report.summary.unknownCount", "priority": 1}
      ]
    }]
  },
  "status": {"conditions": [
    {"type": "NamesAccepted", "status": "True", "reason": "NoConflicts"},
    {"type": "Established", "status": "True", "reason": "InitialNamesAccepted"}
  ]}
}`

// CustomResourceDefinitions returns the definitions of the custom kinds the demo cluster serves:
// Trivy Operator's VulnerabilityReport. The demo does not serve the definitions themselves; the
// engine makes its views from them.
func CustomResourceDefinitions() []*unstructured.Unstructured {
	var obj map[string]any
	if err := utiljson.Unmarshal([]byte(vulnReportsCRD), &obj); err != nil {
		panic(err) // a constant: the demo's own tests parse it
	}
	return []*unstructured.Unstructured{{Object: obj}}
}

// demoVuln is one synthetic vulnerability of the catalogues.
type demoVuln struct {
	id, pkg, installed, fixed, severity string
	score                               float64
}

// The severities as Trivy writes them, with the share of a catalogue each takes and its scores.
var demoSeverities = []struct {
	word     string
	share    float64
	min, max float64
}{
	{"CRITICAL", 0.03, 9.0, 9.8}, {"HIGH", 0.17, 7.0, 8.9}, {"MEDIUM", 0.40, 4.0, 6.9}, {"LOW", 0.35, 1.8, 3.9}, {"UNKNOWN", 0.05, 0, 0},
}

// The packages the catalogues draw from: a Debian base's, an Alpine base's, Go modules.
var (
	debianPackages = []string{
		"libc6", "libssl3", "openssl", "libcurl4", "curl", "zlib1g", "libgnutls30", "libxml2", "libexpat1", "libsqlite3-0",
		"tar", "gzip", "bash", "perl-base", "libgcrypt20", "libsystemd0", "libpcre2-8-0", "libtasn1-6", "ncurses-base", "login",
		"libkrb5-3", "libldap-2.5-0", "libnghttp2-14", "libssh2-1", "libzstd1", "liblzma5", "libpam0g", "util-linux", "apt", "dpkg",
	}
	alpinePackages = []string{"musl", "busybox", "libcrypto3", "libssl3", "zlib", "libexpat", "sqlite-libs", "libcurl", "ssl_client", "c-ares"}
	goModules      = []string{
		"golang.org/x/net", "golang.org/x/crypto", "golang.org/x/text", "golang.org/x/oauth2", "google.golang.org/grpc",
		"google.golang.org/protobuf", "github.com/golang-jwt/jwt/v4", "github.com/docker/docker", "github.com/opencontainers/runc",
		"github.com/hashicorp/go-retryablehttp", "github.com/go-jose/go-jose/v3", "stdlib", "k8s.io/apimachinery", "github.com/gin-gonic/gin",
	}
)

// The catalogues: drawn once, the same for every demo cluster.
var (
	debianCatalog = drawCatalog(1, debianPackages, 90, styleDeb)
	alpineCatalog = drawCatalog(2, alpinePackages, 30, styleApk)
	goCatalog     = drawCatalog(3, goModules, 70, styleGo)
)

// The version styles of the catalogues.
const (
	styleDeb = iota
	styleApk
	styleGo
)

// drawCatalog draws n vulnerabilities of the packages, their versions in the style's form.
func drawCatalog(seed uint64, pkgs []string, n, style int) []demoVuln {
	r := rand.New(rand.NewPCG(seed, 0x7472697679)) // "trivy"
	out := make([]demoVuln, 0, n)
	seen := map[string]bool{}
	for len(out) < n {
		id := fmt.Sprintf("CVE-%d-%d", 2021+r.IntN(5), 1000+r.IntN(48000))
		if seen[id] {
			continue
		}
		seen[id] = true
		v := demoVuln{id: id, pkg: pkgs[r.IntN(len(pkgs))]}
		u, acc := r.Float64(), 0.0
		for _, s := range demoSeverities {
			acc += s.share
			if u < acc || s.word == "UNKNOWN" {
				v.severity = s.word
				if s.max > 0 {
					v.score = float64(int((s.min+r.Float64()*(s.max-s.min))*10+0.5)) / 10
				}
				break
			}
		}
		v.installed, v.fixed = drawVersions(r, style)
		out = append(out, v)
	}
	return out
}

// drawVersions draws an installed version and, for most, the version that fixes it.
func drawVersions(r *rand.Rand, style int) (installed, fixed string) {
	major, minor, patch, rev := r.IntN(4), r.IntN(30), r.IntN(20), 1+r.IntN(9)
	fix := r.Float64()
	switch style {
	case styleGo:
		installed = fmt.Sprintf("v%d.%d.%d", major, minor, patch)
		if fix < 0.75 {
			fixed = fmt.Sprintf("v%d.%d.0", major, minor+1+r.IntN(3))
		}
	case styleApk:
		installed = fmt.Sprintf("%d.%d.%d-r%d", 1+major, minor, patch, rev)
		if fix < 0.6 {
			fixed = fmt.Sprintf("%d.%d.%d-r%d", 1+major, minor, patch, rev+1+r.IntN(3))
		}
	default:
		installed = fmt.Sprintf("%d.%d.%d-%d", 1+major, minor, patch, rev)
		if fix < 0.55 {
			fixed = fmt.Sprintf("%d.%d.%d-%d+deb12u%d", 1+major, minor, patch, rev, 1+r.IntN(6))
		}
	}
	return installed, fixed
}

// demoImage is how one image scans: its OS, and its vulnerabilities.
type demoImage struct {
	osFamily, osName string
	vulns            []demoVuln
}

// scanImage draws an image's scan: its base, how stale it is (the share of the base's catalogue
// it carries), and, for the users' services, its Go modules' vulnerabilities. One image in twenty
// is a legacy one, long not rebuilt.
func scanImage(seed int64, image string) demoImage {
	h := fnv.New64a()
	_, _ = h.Write([]byte(image))
	r := rand.New(rand.NewPCG(uint64(seed), h.Sum64()))
	var im demoImage
	base := debianCatalog
	stale, modules := 0.03+0.40*r.Float64()*r.Float64(), 0.01+0.07*r.Float64()
	if r.Float64() < 0.05 {
		stale, modules = 0.85, 0.25
	}
	switch {
	case strings.Contains(image, "alpine"), r.Float64() < 0.3:
		im.osFamily, im.osName, base = "alpine", "3.19.4", alpineCatalog
	case r.Float64() < 0.15:
		im.osFamily, im.osName, base = "debian", "12.8", nil // distroless: no OS packages to speak of
	default:
		im.osFamily, im.osName = "debian", "12.8"
	}
	for _, v := range base {
		if r.Float64() < stale {
			im.vulns = append(im.vulns, v)
		}
	}
	if strings.HasPrefix(image, "registry.example.com/") {
		for _, v := range goCatalog {
			if r.Float64() < modules {
				im.vulns = append(im.vulns, v)
			}
		}
	}
	return im
}

// buildVulnerabilityReports puts a report for every container of every Deployment's ReplicaSet
// Trivy can scan. Called with c.mu held, after everything else (it does not draw from c.rng).
func (c *Cluster) buildVulnerabilityReports(now time.Time) {
	scans := map[string]demoImage{}
	for _, d := range c.w.deploys {
		if d.current != 0 || d.templates[0] == nil {
			continue // a rollout stuck on an image that cannot be pulled: its current revision is not scanned
		}
		rs := d.name + "-" + d.rsHash[0]
		for _, ct := range d.templates[0].containers {
			im, ok := scans[ct.image]
			if !ok {
				im = scanImage(c.opts.Seed, ct.image)
				scans[ct.image] = im
			}
			r := rand.New(rand.NewPCG(uint64(c.opts.Seed), fnvOf(d.namespace+"/"+rs+"/"+ct.name)))
			scanned := now.Add(-time.Duration(5*60+r.IntN(20*3600)) * time.Second)
			c.put(kindVulnerabilityReports, vulnerabilityReport(d, rs, ct, im, scanned))
		}
	}
}

func fnvOf(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

// The report's shape as Trivy Operator writes it (its v1alpha1 types, the fields the demo fills).
type (
	trivyReportObject struct {
		APIVersion string          `json:"apiVersion"`
		Kind       string          `json:"kind"`
		Metadata   trivyReportMeta `json:"metadata"`
		Report     trivyReportBody `json:"report"`
	}
	trivyReportMeta struct {
		Name              string            `json:"name"`
		Namespace         string            `json:"namespace"`
		UID               string            `json:"uid"`
		CreationTimestamp string            `json:"creationTimestamp"`
		Labels            map[string]string `json:"labels"`
		OwnerReferences   []any             `json:"ownerReferences"`
	}
	trivyReportBody struct {
		UpdateTimestamp string           `json:"updateTimestamp"`
		Scanner         trivyScanner     `json:"scanner"`
		Registry        trivyRegistry    `json:"registry"`
		Artifact        trivyArtifact    `json:"artifact"`
		OS              trivyOS          `json:"os"`
		Summary         map[string]int64 `json:"summary"`
		Vulnerabilities []trivyFinding   `json:"vulnerabilities"`
	}
	trivyScanner struct {
		Name    string `json:"name"`
		Vendor  string `json:"vendor"`
		Version string `json:"version"`
	}
	trivyRegistry struct {
		Server string `json:"server"`
	}
	trivyArtifact struct {
		Repository string `json:"repository"`
		Tag        string `json:"tag"`
		Digest     string `json:"digest"`
		MimeType   string `json:"mimeType"`
	}
	trivyOS struct {
		Family string `json:"family"`
		Name   string `json:"name"`
	}
	trivyFinding struct {
		VulnerabilityID  string   `json:"vulnerabilityID"`
		Resource         string   `json:"resource"`
		InstalledVersion string   `json:"installedVersion"`
		FixedVersion     string   `json:"fixedVersion"`
		Severity         string   `json:"severity"`
		Score            *float64 `json:"score,omitempty"`
		Title            string   `json:"title"`
		PublishedDate    string   `json:"publishedDate"`
		LastModifiedDate string   `json:"lastModifiedDate"`
		Links            []string `json:"links"`
	}
)

// vulnerabilityReport renders the report of one container of a Deployment's ReplicaSet.
func vulnerabilityReport(d *deployment, rs string, ct container, im demoImage, scanned time.Time) *unstructured.Unstructured {
	registry, repo, tag := splitImage(ct.image)
	var counts [len("CHMLU")]int64
	list := make([]trivyFinding, 0, len(im.vulns))
	for _, v := range im.vulns {
		counts[strings.IndexByte("CHMLU", v.severity[0])]++
		f := trivyFinding{
			VulnerabilityID: v.id, Resource: v.pkg, InstalledVersion: v.installed, FixedVersion: v.fixed, Severity: v.severity,
			Title:         v.pkg + ": a synthetic vulnerability of the demo cluster",
			PublishedDate: "2024-02-01T00:00:00Z", LastModifiedDate: "2024-06-01T00:00:00Z", Links: []string{},
		}
		if v.score > 0 {
			score := v.score
			f.Score = &score
		}
		list = append(list, f)
	}
	obj := trivyReportObject{
		APIVersion: trivyAPIVersion, Kind: trivyKind,
		Metadata: trivyReportMeta{
			Name: "replicaset-" + rs + "-" + ct.name, Namespace: d.namespace, UID: digest("vulnreport", d.namespace, rs, ct.name)[:32],
			CreationTimestamp: ts(scanned),
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "trivy-operator", "resource-spec-hash": digest("spec", d.namespace, rs)[:10],
				"trivy-operator.container.name": ct.name, "trivy-operator.resource.kind": apiKindReplicaSet,
				"trivy-operator.resource.name": rs, "trivy-operator.resource.namespace": d.namespace,
			},
			OwnerReferences: ownerRef("apps/v1", apiKindReplicaSet, rs, d.rsUID[0]),
		},
		Report: trivyReportBody{
			UpdateTimestamp: ts(scanned),
			Scanner:         trivyScanner{Name: "Trivy", Vendor: "Aqua Security", Version: "0.58.1"},
			Registry:        trivyRegistry{Server: registry},
			Artifact: trivyArtifact{Repository: repo, Tag: tag, Digest: "sha256:" + digest("image", ct.image),
				MimeType: "application/vnd.oci.image.manifest.v1+json"},
			OS: trivyOS{Family: im.osFamily, Name: im.osName},
			Summary: map[string]int64{"criticalCount": counts[0], "highCount": counts[1], "mediumCount": counts[2], "lowCount": counts[3],
				"unknownCount": counts[4], "noneCount": 0},
			Vulnerabilities: list,
		},
	}
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&obj)
	if err != nil {
		panic(err) // the types above convert
	}
	return &unstructured.Unstructured{Object: u}
}

// splitImage splits an image reference as Trivy writes it: the registry (Docker Hub as
// index.docker.io), the repository (Docker Hub's official images under library/) and the tag.
func splitImage(image string) (registry, repo, tag string) {
	repo, tag = image, "latest"
	if i := strings.LastIndexByte(repo, ':'); i > strings.LastIndexByte(repo, '/') {
		repo, tag = repo[:i], repo[i+1:]
	}
	registry = "index.docker.io"
	if i := strings.IndexByte(repo, '/'); i >= 0 && strings.ContainsAny(repo[:i], ".:") {
		registry, repo = repo[:i], repo[i+1:]
	}
	if registry == "index.docker.io" && !strings.Contains(repo, "/") {
		repo = "library/" + repo
	}
	return registry, repo, tag
}
