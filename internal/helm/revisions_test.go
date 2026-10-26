package helm

import (
	"encoding/base64"
	"strings"
	"testing"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/release/common"
)

const revisionManifest = `---
# Source: web/templates/service.yaml
apiVersion: v1
kind: Service
metadata:
  name: web
spec:
  ports:
  - port: 80
---
# Source: web/templates/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  replicas: 2
---
# Source: web/templates/secret.yaml
apiVersion: v1
kind: Secret
metadata:
  name: web-db
type: Opaque
data:
  password: aHVudGVyMg==
stringData:
  user: admin
---
# Source: web/templates/clusterrole.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: web-reader
  namespace: ignored
`

// A revision's record decodes into its values, user-supplied or all, and its manifest's objects in
// Helm's install order, each with its template and its YAML in the Kubernetes order; a Secret's
// data masked with its size.
func TestRevisionOf(t *testing.T) {
	s := secretFor(t, testRelease{ns: "apps", name: "web", rev: 2, status: common.StatusSuperseded,
		values: map[string]any{"replicaCount": 2, "image": map[string]any{"tag": "1.0"}}, manifest: revisionManifest})
	rls, err := DecodeSecret(s)
	if err != nil {
		t.Fatal(err)
	}
	rls.Chart = &chart.Chart{Metadata: rls.Chart.Metadata, Values: map[string]any{"replicaCount": 1, "service": map[string]any{"port": 80}}}
	s.Object["data"] = map[string]any{"release": base64.StdEncoding.EncodeToString([]byte(encode(t, rls, true)))}

	r, err := RevisionOf(s, false)
	if err != nil {
		t.Fatal(err)
	}
	if r.Revision != 2 || r.Status != "superseded" || r.Chart != "web" || r.ChartVersion != "1.2.0" || r.All {
		t.Errorf("revision %+v", r)
	}
	if r.Values != "image:\n  tag: \"1.0\"\nreplicaCount: 2\n" {
		t.Errorf("user-supplied values %q", r.Values)
	}
	if r.Manifest != revisionManifest {
		t.Errorf("manifest %q", r.Manifest)
	}
	var kinds []string
	for _, o := range r.Objects {
		kinds = append(kinds, o.Kind+" "+o.Namespace+"/"+o.Name+" "+o.Source)
	}
	want := []string{
		"Secret apps/web-db web/templates/secret.yaml",
		"ClusterRole /web-reader web/templates/clusterrole.yaml",
		"Service apps/web web/templates/service.yaml",
		"Deployment apps/web web/templates/deployment.yaml",
	}
	if strings.Join(kinds, "|") != strings.Join(want, "|") {
		t.Errorf("objects\n%q, want\n%q", kinds, want)
	}
	sec := r.Objects[0]
	if !sec.Secret || !strings.HasPrefix(sec.YAML, "apiVersion: v1\nkind: Secret\nmetadata:\n  name: web-db\n") || !strings.Contains(sec.YAML, "aHVudGVyMg==") {
		t.Errorf("secret YAML %q", sec.YAML)
	}
	if strings.Contains(sec.Masked, "aHVudGVyMg") || strings.Contains(sec.Masked, "admin") ||
		!strings.Contains(sec.Masked, "password: •••••••• (7 bytes)") || !strings.Contains(sec.Masked, "user: •••••••• (5 bytes)") {
		t.Errorf("masked %q", sec.Masked)
	}
	if r.Objects[3].Secret || r.Objects[3].Masked != "" {
		t.Errorf("a Deployment masked: %+v", r.Objects[3])
	}

	all, err := RevisionOf(s, true)
	if err != nil {
		t.Fatal(err)
	}
	if !all.All || all.Values != "image:\n  tag: \"1.0\"\nreplicaCount: 2\nservice:\n  port: 80\n" {
		t.Errorf("all values %q", all.Values)
	}

	if _, err := RevisionOf(releaseSecret("not a payload"), false); err == nil || strings.Contains(err.Error(), "payload\"") {
		t.Errorf("a record that does not decode: %v", err)
	}
	if RecordName("web", 12) != "sh.helm.release.v1.web.v12" {
		t.Error(RecordName("web", 12))
	}
}

// The changed values between two revisions, by key path: changed, set on one side only, lists
// element by element, a map in a list by its keys.
func TestValueChanges(t *testing.T) {
	from := Revision{values: map[string]any{"a": map[string]any{"b": 1, "c": "x"}, "list": []any{1, 2}, "gone": true, "same": "s",
		"hosts": []any{map[string]any{"host": "a.example"}}}}
	to := Revision{values: map[string]any{"a": map[string]any{"b": 2, "c": "x", "d": "multi\nline"}, "list": []any{1, 3, 4}, "same": "s", "new": map[string]any{"k": "v"},
		"hosts": []any{map[string]any{"host": "a.example"}, map[string]any{"host": "b.example"}}}}
	got, cut := ValueChanges(from, to)
	want := []ValueChange{
		{Path: "a.b", From: "1", To: "2"},
		{Path: "a.d", To: `"multi\nline"`},
		{Path: "gone", From: "true"},
		{Path: "hosts[1]", To: `{"host":"b.example"}`},
		{Path: "list[1]", From: "2", To: "3"},
		{Path: "list[2]", To: "4"},
		{Path: "new", To: `{"k":"v"}`},
	}
	if cut || len(got) != len(want) {
		t.Fatalf("changes %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("change %d: %+v, want %+v", i, got[i], want[i])
		}
	}
}

// Objects are marked against the compared revision: added, changed, the same, and removed after
// the others.
func TestObjectChanges(t *testing.T) {
	from, _ := ManifestObjects(revisionManifest, "apps")
	to, _ := ManifestObjects(strings.Replace(strings.Replace(revisionManifest, "replicas: 2", "replicas: 3", 1),
		"kind: Service\nmetadata:\n  name: web", "kind: ConfigMap\nmetadata:\n  name: web", 1), "apps")
	var marks []string
	for _, c := range ObjectChanges(from, to) {
		marks = append(marks, c.Change+" "+c.Kind+" "+c.Name)
		if c.Change == ObjectChanged && (!strings.Contains(c.Was, "replicas: 2") || !strings.Contains(c.YAML, "replicas: 3")) {
			t.Errorf("changed %+v", c)
		}
	}
	want := []string{"= Secret web-db", "+ ConfigMap web", "= ClusterRole web-reader", "~ Deployment web", "- Service web"}
	if strings.Join(marks, "|") != strings.Join(want, "|") {
		t.Errorf("marks %q, want %q", marks, want)
	}
}

// The masked manifest keeps every document but a Secret's values, and the Secret's Source line.
func TestMaskManifest(t *testing.T) {
	m := MaskManifest(revisionManifest, func(n int) string { return "<withheld>" })
	if strings.Contains(m, "aHVudGVyMg") || strings.Contains(m, "admin") || !strings.Contains(m, "# Source: web/templates/secret.yaml\napiVersion: v1\nkind: Secret") ||
		!strings.Contains(m, "replicas: 2") || strings.Count(m, "<withheld>") != 2 || strings.Count(m, "---\n") != 4 {
		t.Errorf("masked manifest\n%s", m)
	}
}
