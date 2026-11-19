package kinds_test

import (
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utiljson "k8s.io/apimachinery/pkg/util/json"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

func FuzzArgoCD(f *testing.F) {
	profiles := append(kinds.ArgoProfiles(), kinds.ArgoSettings()...)
	for _, key := range []string{kinds.ArgoApplicationsKey, kinds.ArgoApplicationSetKey, kinds.ArgoProjectsKey, "argocdsettings"} {
		for _, c := range loadCases(f, key) {
			f.Add(c.json)
		}
	}
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"spec":{"sources":[7,{"repoURL":1}],"destination":"x","syncPolicy":{"automated":null}},"status":{"resources":[null],"history":[{"id":"x"}]}}`))
	f.Add([]byte(`{"kind":"Secret","metadata":{"labels":{"argocd.argoproj.io/secret-type":"cluster"}},"data":{"server":7,"name":"!!"}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, decode := range []func([]byte, any) error{utiljson.Unmarshal, json.Unmarshal} {
			var m map[string]any
			if decode(data, &m) != nil || m == nil {
				continue
			}
			obj := &unstructured.Unstructured{Object: m}
			for _, p := range profiles {
				objs := []*unstructured.Unstructured{obj}
				if p.Derive != nil {
					objs = p.Derive(objs)
				}
				for _, o := range objs {
					if err := exercise(p, o); err != nil {
						t.Fatal(err)
					}
				}
			}
			_ = kinds.ArgoAppActivity(obj)
			_ = kinds.ArgoOperationBusy(obj)
			_ = kinds.ArgoAppMessage(obj)
		}
	})
}

// columnTitles lists a profile's column titles, hidden ones in parentheses.
func columnTitles(p *model.KindProfile) string {
	var got []string
	for _, c := range p.Columns {
		title := c.Title
		if c.Hidden {
			title = "(" + title + ")"
		}
		got = append(got, title)
	}
	return strings.Join(got, ", ")
}

// TestArgoCDColumns pins the columns of Lens's documentation ("Using Argo CD", read, not captured)
// and Cogpit's hidden ones after them.
func TestArgoCDColumns(t *testing.T) {
	want := map[string]string{
		kinds.ArgoApplicationsKey:   "Name, Namespace, Sync, Health, Project, Destination, Revision, Age, (Repository), (Target Revision), (Path/Chart), (Message)",
		kinds.ArgoApplicationSetKey: "Name, Namespace, Generators, Age, (Project), (Applications), (Status)",
		kinds.ArgoProjectsKey:       "Name, Namespace, Description, Source Repos, Age, (Destinations)",
		kinds.ArgoRepositoriesKey:   "Name, Type, URL, Project, Age, (Namespace), (Display Name), (Kind), (Authentication)",
		kinds.ArgoClustersKey:       "Name, Server, Age, (Namespace), (Display Name), (Namespaces), (Cluster Resources), (Project)",
	}
	for _, p := range append(kinds.ArgoProfiles(), kinds.ArgoSettings()...) {
		if got := columnTitles(p); got != want[p.Key] {
			t.Errorf("%s: columns %s", p.Key, got)
		}
	}
	for _, p := range kinds.ArgoProfiles() {
		if curatedProfile(t, p.Key) != p {
			t.Errorf("%s is not among the curated profiles", p.Key)
		}
	}
}

// TestArgoCDStatusTexts states the health and sync mapping outright: health first, then sync.
func TestArgoCDStatusTexts(t *testing.T) {
	const (
		none    = model.StatusNone
		ok      = model.StatusOK
		pending = model.StatusPending
		warning = model.StatusWarning
		bad     = model.StatusError
		unknown = model.StatusUnknown
		app     = kinds.ArgoApplicationsKey
		set     = kinds.ArgoApplicationSetKey
	)
	tests := []statusText{
		{app, "healthy", "sync", "Synced", ok, ok},
		{app, "healthy", "health", "Healthy", ok, ok},
		{app, "healthy", "destination", "in-cluster/shop", none, ok},
		{app, "healthy", "revision", "4f2c9e1", none, ok},
		{app, "healthy", "project", "team-shop", none, ok},
		{app, "out-of-sync", "sync", "OutOfSync", warning, warning}, // Healthy, then sync: a warning
		{app, "out-of-sync", "destination", "prod-eu/ingress", none, warning},
		{app, "out-of-sync", "path", "ingress-nginx", none, warning}, // a chart in place of a path
		{app, "out-of-sync", "revision", "4.11.2", none, warning},    // a chart version stays whole
		{app, "failing", "health", "Degraded", bad, bad},
		{app, "failing", "message", "Failed to load target state: failed to generate manifest for source 1 of 1: rpc error: kustomize build failed", none, bad},
		{app, "failing", "destination", "https://10.0.0.1:6443/payments", none, bad},
		{app, "multi-source", "health", "Progressing", pending, pending},
		{app, "multi-source", "repository", "https://prometheus-community.github.io/helm-charts, https://git.example.com/platform/k8s-base-argo.git", none, pending},
		{app, "multi-source", "revision", "65.1.0, 1234567", none, pending},
		{app, "multi-source", "message", "waiting for healthy state of apps/Deployment/grafana", none, pending},
		{app, "minimal", "sync", "", none, unknown}, // not compared yet
		{app, "minimal", "health", "", none, unknown},
		{app, "minimal", "destination", "in-cluster", none, unknown},
		{app, "terminating", "health", "Suspended", model.StatusStopped, model.StatusTerminating},
		{set, "healthy", "generators", "matrix, list", none, ok},
		{set, "healthy", "status", "ParametersGenerated ResourcesUpToDate", ok, ok},
		{set, "healthy", "applications", "-", none, ok}, // no related objects outside a view
		{set, "failing", "status", "ErrorOccurred", bad, bad},
		{set, "failing", "project", "previews", none, bad},
		{set, "minimal", "status", "", pending, pending},
		{kinds.ArgoProjectsKey, "healthy", "sourceRepos", "https://git.example.com/platform/k8s-base-argo.git, https://charts.example.com/stable", none, none},
		{kinds.ArgoProjectsKey, "healthy", "destinations", "in-cluster/shop, prod-eu/shop-*", none, none},
		{kinds.ArgoProjectsKey, "healthy", "description", "The shop team's applications: storefront, checkout and their jobs", none, none},
	}
	checkCuratedTexts(t, tests)
}

// checkCuratedTexts is checkStatusTexts for curated profiles.
func checkCuratedTexts(t *testing.T, tests []statusText) {
	t.Helper()
	objects := map[string]*unstructured.Unstructured{}
	for _, tt := range tests {
		if _, done := objects[tt.kind+"/"+tt.name]; done {
			continue
		}
		for _, c := range loadCases(t, tt.kind) {
			objects[tt.kind+"/"+c.name] = c.obj
		}
	}
	for _, tt := range tests {
		t.Run(tt.kind+"/"+tt.name+"/"+tt.column, func(t *testing.T) {
			p := curatedProfile(t, tt.kind)
			i := p.ColumnIndex(tt.column)
			if i < 0 {
				t.Fatalf("no column %q", tt.column)
			}
			row := model.BuildRow(p, "test", objects[tt.kind+"/"+tt.name])
			cell := row.Cells[i]
			if text := model.CellText(p.Columns[i], cell, now); text != tt.text || cell.Status != tt.cell || row.Status != tt.row {
				t.Errorf("got %q (cell %q, row %q), want %q (cell %q, row %q)", text, cell.Status, row.Status, tt.text, tt.cell, tt.row)
			}
		})
	}
}

// TestArgoOperationBusy: a requested operation and a running or terminating one refuse a new
// sync; a finished one does not.
func TestArgoOperationBusy(t *testing.T) {
	cases := map[string]bool{"healthy": false, "failing": false, "multi-source": true, "terminating": true, "minimal": false}
	for _, c := range loadCases(t, kinds.ArgoApplicationsKey) {
		want, ok := cases[c.name]
		if !ok {
			continue
		}
		if got := kinds.ArgoOperationBusy(c.obj) != ""; got != want {
			t.Errorf("%s: busy %v (%q), want %v", c.name, got, kinds.ArgoOperationBusy(c.obj), want)
		}
	}
}

// TestArgoAppActivity: the last operation and the history's syncs, newest first, each once.
func TestArgoAppActivity(t *testing.T) {
	obj := recordedPath(t, filepath.Join(kinds.ArgoApplicationsKey, "healthy"))
	got := kinds.ArgoAppActivity(obj)
	if len(got) != 2 {
		t.Fatalf("activity %+v", got)
	}
	if got[0].Text != "Sync succeeded by the controller (automated)" || got[0].Revision != "4f2c9e1" || got[0].Status != model.StatusOK ||
		!got[0].At.Equal(time.Date(2026, 10, 11, 11, 40, 12, 0, time.UTC)) {
		t.Errorf("first %+v", got[0])
	}
	if got[1].Text != "Deployed by alice" || got[1].Revision != "0a1b2c3" {
		t.Errorf("second %+v", got[1])
	}
	failing := kinds.ArgoAppActivity(recordedPath(t, filepath.Join(kinds.ArgoApplicationsKey, "failing")))
	if len(failing) != 1 || failing[0].Status != model.StatusError || !strings.HasPrefix(failing[0].Text, "Sync failed by bob: one or more objects") {
		t.Errorf("failing %+v", failing)
	}
}

// settingsGolden is what a settings case renders: the objects each settings view derives from it.
type settingsGolden struct {
	Repositories []golden `json:"repositories"`
	Clusters     []golden `json:"clusters"`
	YAML         []any    `json:"objects"`
}

// TestArgoSettingsGolden derives the settings views' objects from recorded Secrets and renders
// them: rows, fields, drawers and the derived objects (what object_get's YAML shows).
func TestArgoSettingsGolden(t *testing.T) {
	repos, clusters := kinds.ArgoSettings()[0], kinds.ArgoSettings()[1]
	for _, c := range loadCases(t, "argocdsettings") {
		t.Run(c.name, func(t *testing.T) {
			g := settingsGolden{Repositories: []golden{}, Clusters: []golden{}, YAML: []any{}}
			for _, o := range repos.Derive([]*unstructured.Unstructured{c.obj}) {
				g.Repositories = append(g.Repositories, render(t, repos, o))
				g.YAML = append(g.YAML, o.Object)
			}
			for _, o := range clusters.Derive([]*unstructured.Unstructured{c.obj}) {
				g.Clusters = append(g.Clusters, render(t, clusters, o))
				g.YAML = append(g.YAML, o.Object)
			}
			checkGolden(t, strings.TrimSuffix(c.path, ".yaml")+".golden.json", encode(t, g))
		})
	}
}

// argoCredentials are the credential values of the recorded settings Secrets, in every form they
// could leak in: none may reach a row, a field, a drawer or a derived object (D-35).
var argoCredentials = []string{
	"fake-repo-password-hunter2", "deploy-bot", "fakesshkeymaterial", "OPENSSH PRIVATE KEY", "ghs_faketoken123",
	"x-access-token", "fakegithubappkey", "RSA PRIVATE KEY", "654321", "123456", "fake-cluster-bearer-token",
	"ZmFrZWNh", "ZmFrZWNlcnQ=", "ZmFrZWtleQ==", "bearerToken", "tlsClientConfig", "fake-admin-password",
	"fake-server-secret-key", "fake-last-applied-password", "argocd-secret",
}

// TestArgoSettingsNeverShowCredentials: whatever a settings view derives from Argo CD's Secrets
// carries the allowed keys' values and the other keys' names, never another value; a Secret
// without the label is not shown at all.
func TestArgoSettingsNeverShowCredentials(t *testing.T) {
	var all []*unstructured.Unstructured
	for _, c := range loadCases(t, "argocdsettings") {
		all = append(all, c.obj)
	}
	forbidden := append([]string(nil), argoCredentials...)
	for _, f := range argoCredentials {
		forbidden = append(forbidden, base64.StdEncoding.EncodeToString([]byte(f)))
	}
	for _, p := range kinds.ArgoSettings() {
		objs := p.Derive(all)
		if len(objs) == 0 {
			t.Fatalf("%s derives nothing", p.Key)
		}
		for _, o := range objs {
			g := render(t, p, o)
			b, _ := json.Marshal(struct {
				G golden
				O any
			}{g, o.Object})
			for _, f := range forbidden {
				if strings.Contains(string(b), f) {
					t.Errorf("%s/%s carries %q", p.Key, o.GetName(), f)
				}
			}
			if o.GetAPIVersion() != model.LocalGroup+"/v1" || o.GetKind() == "Secret" {
				t.Errorf("%s: derived %s %s", p.Key, o.GetAPIVersion(), o.GetKind())
			}
		}
	}
	// The repositories show the token-bearing URL redacted, the SSH one whole, and say how each
	// authenticates by the names of its keys.
	repos := kinds.ArgoSettings()[0].Derive(all)
	byName := map[string]*unstructured.Unstructured{}
	for _, o := range repos {
		byName[o.GetName()] = o
	}
	if len(repos) != 3 {
		t.Fatalf("%d repositories, want 3 (no cluster, no unlabelled Secret)", len(repos))
	}
	if u := model.NestedString(byName["creds-github"], "spec", "url"); u != "https://<redacted>@github.com/example" {
		t.Errorf("credential template URL %q", u)
	}
	if u := model.NestedString(byName["repo-pay-deploy"], "spec", "url"); u != "git@git.example.com:pay/deploy.git" {
		t.Errorf("SSH URL %q", u)
	}
	if keys := model.NestedStrings(byName["repo-k8s-base-argo"], "spec", "hiddenKeys"); strings.Join(keys, ",") != "insecure,password,username" {
		t.Errorf("hidden keys %v", keys)
	}
}

// TestArgoSyncOperation: the operation argocd app sync writes without options (Argo CD 3.5.5's
// Server.Sync): the source the user confirmed, a copy of the spec's (the controller then syncs it
// even if the spec changes before it runs), each source's target revision, prune as asked, the
// Application's own sync options and retry (copies), Cogpit as the initiator.
func TestArgoSyncOperation(t *testing.T) {
	healthy := recordedPath(t, filepath.Join(kinds.ArgoApplicationsKey, "healthy"))
	op := kinds.ArgoSyncOperation(healthy, true)
	b, _ := json.Marshal(op)
	want := `{"initiatedBy":{"username":"cogpit"},"retry":{"limit":5},"sync":{"prune":true,"revision":"main",` +
		`"source":{"path":"apps/shop","repoURL":"https://git.example.com/platform/k8s-base-argo.git","targetRevision":"main"},` +
		`"syncOptions":["CreateNamespace=true","ServerSideApply=true"]}}`
	if string(b) != want {
		t.Errorf("operation %s, want %s", b, want)
	}
	op["retry"].(map[string]any)["limit"] = int64(9)
	if n, _ := model.NestedInt64(healthy, "spec", "syncPolicy", "retry", "limit"); n != 5 {
		t.Error("the operation aliases the cached object's retry")
	}
	op["sync"].(map[string]any)["source"].(map[string]any)["path"] = "elsewhere"
	if p := model.NestedString(healthy, "spec", "source", "path"); p != "apps/shop" {
		t.Error("the operation aliases the cached object's source")
	}
	multi := kinds.ArgoSyncOperation(recordedPath(t, filepath.Join(kinds.ArgoApplicationsKey, "multi-source")), false)
	want = `{"initiatedBy":{"username":"cogpit"},"sync":{"prune":false,"revisions":["65.1.0","main"],"sources":[` +
		`{"chart":"kube-prometheus-stack","helm":{"valueFiles":["$values/monitoring/values.yaml"]},"repoURL":"https://prometheus-community.github.io/helm-charts","targetRevision":"65.1.0"},` +
		`{"ref":"values","repoURL":"https://git.example.com/platform/k8s-base-argo.git","targetRevision":"main"}]}}`
	if b, _ := json.Marshal(multi); string(b) != want {
		t.Errorf("multi-source operation %s", b)
	}
	// A Helm value set to null (it removes a chart's default) stays null in the copied source.
	app := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{"source": map[string]any{
		"repoURL": "https://charts.example.com/stable", "chart": "web", "targetRevision": "1.0.0",
		"helm": map[string]any{"valuesObject": map[string]any{"resources": nil, "replicaCount": int64(2)}},
	}}}}
	b, _ = json.Marshal(kinds.ArgoSyncOperation(app, false))
	if !strings.Contains(string(b), `"valuesObject":{"replicaCount":2,"resources":null}`) {
		t.Errorf("the null value is lost: %s", b)
	}
}

// TestArgoV3ResourceHealthAndProject (checked against a live Argo CD 3.5 controller on a real
// cluster): Argo CD 3 keeps the resources' health in its resource tree
// (status.resourceHealthSource: appTree), so the Resources table leaves out the Health column it
// cannot fill and says where the health is; and an Application outside Argo CD's namespace links
// to its project in the namespace the controller runs in (status.controllerNamespace).
func TestArgoV3ResourceHealthAndProject(t *testing.T) {
	app := recordedPath(t, filepath.Join(kinds.ArgoApplicationsKey, "other-namespace"))
	p := curatedProfile(t, kinds.ArgoApplicationsKey)
	row := model.BuildRow(p, "test", app)
	if link := row.Cells[p.ColumnIndex("project")].Link; link == nil || link.Namespace != "argocd" || link.Name != "team-billing" {
		t.Errorf("project link %+v", link)
	}
	if ns := kinds.ArgoProjectNamespace(app); ns != "argocd" {
		t.Errorf("project namespace %q", ns)
	}
	var res *model.DetailSection
	sections := p.DetailSections(app, model.NoRelated)
	for i := range sections {
		if sections[i].Title == "Resources" {
			res = &sections[i]
		}
		for _, it := range sections[i].Items {
			if it.Name == "Project" && (it.Link == nil || it.Link.Namespace != "argocd") {
				t.Errorf("the drawer's project link %+v", it.Link)
			}
		}
	}
	if res == nil || res.Table == nil {
		t.Fatalf("no Resources table: %+v", sections)
	}
	if got := strings.Join(res.Table.Columns, ","); got != "Kind,Namespace,Name,Sync" {
		t.Errorf("columns %s", got)
	}
	if len(res.Items) != 1 || res.Items[0].Value != kinds.ArgoResourceHealthNote {
		t.Errorf("items %+v", res.Items)
	}
	// Before the controller has written its namespace, the Application's own.
	unstructured.RemoveNestedField(app.Object, "status", "controllerNamespace")
	if ns := kinds.ArgoProjectNamespace(app); ns != "team-billing" {
		t.Errorf("project namespace without the controller's %q", ns)
	}
	// Resources that carry a health (Argo CD 2, or 3 with the health persisted) keep the column.
	healthy := recordedPath(t, filepath.Join(kinds.ArgoApplicationsKey, "healthy"))
	for _, s := range p.DetailSections(healthy, model.NoRelated) {
		if s.Title == "Resources" && (len(s.Table.Columns) != 5 || len(s.Items) != 0) {
			t.Errorf("the healthy Application's resources %+v", s)
		}
	}
}

func TestRedactURLUserinfo(t *testing.T) {
	for in, want := range map[string]string{
		"https://git.example.com/a.git":            "https://git.example.com/a.git",
		"https://user:pass@git.example.com/a.git":  "https://<redacted>@git.example.com/a.git",
		"https://token@github.com/x":               "https://<redacted>@github.com/x",
		"HTTP://u:p@h:8080/x?y=a@b":                "HTTP://<redacted>@h:8080/x?y=a@b",
		"ssh://git@git.example.com/x.git":          "ssh://git@git.example.com/x.git",
		"ssh://git:secret@git.example.com/x.git":   "ssh://<redacted>@git.example.com/x.git",
		"git@git.example.com:org/repo.git":         "git@git.example.com:org/repo.git",
		"user:pw@host/path":                        "<redacted>@host/path",
		"oci://registry.example.com/charts":        "oci://registry.example.com/charts",
		"https://kubernetes.default.svc":           "https://kubernetes.default.svc",
		"https://a@b@host/x":                       "https://<redacted>@host/x",
		"registry.example.com/charts@sha256:abcd0": "registry.example.com/charts@sha256:abcd0",
	} {
		if got := kinds.RedactURLUserinfo(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}
