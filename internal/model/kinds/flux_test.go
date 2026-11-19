package kinds_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// Flux kind keys the tests name.
const (
	fluxKustomizations = "kustomizations.kustomize.toolkit.fluxcd.io"
	fluxHelmReleases   = "helmreleases.helm.toolkit.fluxcd.io"
	fluxGitRepos       = "gitrepositories.source.toolkit.fluxcd.io"
	fluxHelmRepos      = "helmrepositories.source.toolkit.fluxcd.io"
	fluxAlerts         = "alerts.notification.toolkit.fluxcd.io"
	fluxProviders      = "providers.notification.toolkit.fluxcd.io"
	fluxReceivers      = "receivers.notification.toolkit.fluxcd.io"
	fluxImagePolicies  = "imagepolicies.image.toolkit.fluxcd.io"
	fluxImageRepos     = "imagerepositories.image.toolkit.fluxcd.io"
	fluxExternal       = "externalartifacts.source.toolkit.fluxcd.io"
)

// TestFluxKinds pins the Flux kinds in the navigator's captured order with their sub-groups, and
// which take Suspend, Resume and Reconcile; every one is a curated profile.
func TestFluxKinds(t *testing.T) {
	var got []string
	for _, k := range kinds.FluxKinds() {
		s := k.Title
		if k.Section != "" {
			s = k.Section + " › " + s
		}
		got = append(got, s)
		curatedProfile(t, k.Key)
		if want := strings.HasPrefix(k.Key, "externalartifacts."); k.Reconcile == want {
			t.Errorf("%s: reconcile %v", k.Key, k.Reconcile)
		}
		if want := k.Key != fluxExternal && k.Key != fluxImagePolicies; k.Suspend != want {
			t.Errorf("%s: suspend %v", k.Key, k.Suspend)
		}
	}
	want := "Helm Releases, Kustomizations, Image Automation › Image Repositories, Image Automation › Image Policies, " +
		"Image Automation › Image Update Automations, Notifications › Alerts, Notifications › Providers, Notifications › Receivers, " +
		"Sources › Git Repositories, Sources › OCI Repositories, Sources › Helm Repositories, Sources › Buckets, Sources › External Artifacts"
	if s := strings.Join(got, ", "); s != want {
		t.Errorf("kinds\n got %s\nwant %s", s, want)
	}
	if _, ok := kinds.FluxKindOf("pods"); ok {
		t.Error("pods is not a Flux kind")
	}
}

// TestFluxColumns pins the inferred column order: Name, Namespace, the kind's own, Revision where
// the kind has one, Suspended, Age, Status, Message.
func TestFluxColumns(t *testing.T) {
	tests := map[string]string{
		fluxKustomizations: "Name, Namespace, Source, Path, Revision, Suspended, Age, Status, Message",
		fluxHelmReleases:   "Name, Namespace, Chart, Version, Revision, Suspended, Age, Status, Message",
		fluxGitRepos:       "Name, Namespace, URL, Ref, Revision, Suspended, Age, Status, Message",
		fluxImageRepos:     "Name, Namespace, Image, Last Scan, Tags, Suspended, Age, Status, Message",
		fluxAlerts:         "Name, Namespace, Provider, Severity, Suspended, Age, Status, Message",
	}
	for key, want := range tests {
		var got []string
		for _, c := range curatedProfile(t, key).Columns {
			got = append(got, c.Title)
		}
		if s := strings.Join(got, ", "); s != want {
			t.Errorf("%s: %s", key, s)
		}
	}
}

// TestFluxStatusTexts: the state from the conditions (suspended first, then a failure, then a
// reconciliation in progress, then Ready), the row's token, the message on one line, the links.
func TestFluxStatusTexts(t *testing.T) {
	const (
		ok      = model.StatusOK
		bad     = model.StatusError
		pending = model.StatusPending
		stopped = model.StatusStopped
		unknown = model.StatusUnknown
		none    = model.StatusNone
	)
	tests := []statusText{
		{fluxKustomizations, "ready", "status", "Ready", ok, ok},
		{fluxKustomizations, "failing", "status", "Not Ready", bad, bad}, // Ready False wins over Reconciling True
		{fluxKustomizations, "failing", "message", "kustomize build failed: accumulating resources: missing Resource metadata", none, bad},
		{fluxKustomizations, "suspended", "status", "Suspended", stopped, stopped},
		{fluxKustomizations, "suspended", "suspended", "true", none, stopped},
		{fluxKustomizations, "reconciling", "status", "Reconciling", pending, pending},
		{fluxKustomizations, "stale", "status", "Reconciling", pending, pending}, // generation 3, observed 2
		{fluxKustomizations, "stalled", "status", "Not Ready", bad, bad},
		{fluxKustomizations, "minimal", "status", "Unknown", unknown, unknown},
		{fluxKustomizations, "terminating", "status", "Ready", ok, model.StatusTerminating},
		{fluxKustomizations, "ready", "source", "GitRepository/flux-system", none, ok},
		{fluxKustomizations, "failing", "source", "OCIRepository/platform/platform", none, bad},
		{fluxKustomizations, "ready", "revision", "main@sha1:6f2c1b9e", none, ok},
		{fluxHelmReleases, "ready", "revision", "6.7.1", none, ok},
		{fluxHelmReleases, "v2beta1", "revision", "1.0.0", none, ok},
		{fluxHelmReleases, "suspended", "version", "*", none, stopped},
		{fluxHelmReleases, "chartref", "chart", "OCIRepository/frontend-chart", none, bad},
		{fluxGitRepos, "ready", "url", "https://<redacted>@github.com/acme/k8s-flux-base.git", none, ok},
		{fluxGitRepos, "failing", "url", "https://<redacted>@github.com/acme/missing", none, bad},
		{fluxGitRepos, "ssh", "url", "ssh://git@github.com/acme/charts", none, ok},
		{fluxGitRepos, "ssh", "ref", "tag v2.1.0", none, ok},
		{fluxAlerts, "static", "status", "Ready", ok, ok}, // v1beta3: no status, ready unless suspended
		// An OCI Helm repository has no status since Flux 2.1 and the flux CLI calls it Ready (seen
		// on a real cluster, where Cogpit showed Unknown); a plain one without a status stays
		// unknown.
		{fluxHelmRepos, "oci-static", "status", "Ready", ok, ok},
		{fluxHelmRepos, "oci-static", "message", "", none, ok},
		{fluxHelmRepos, "oci-suspended", "status", "Suspended", stopped, stopped},
		{fluxHelmRepos, "oci", "status", "Ready", ok, ok}, // before 2.1: its own Ready condition
		{fluxHelmRepos, "minimal", "status", "Unknown", unknown, unknown},
		{fluxAlerts, "suspended", "status", "Suspended", stopped, stopped},
		{fluxAlerts, "v1beta2", "status", "Not Ready", bad, bad},
		{fluxImagePolicies, "ready", "latestImage", "ghcr.io/stefanprodan/podinfo:6.7.1@sha256:abcd", none, ok},
		{fluxImagePolicies, "v1beta2", "latestImage", "docker.io/library/nginx:1.27", none, bad},
		{fluxImageRepos, "ready", "tags", "42", none, ok},
		{fluxImageRepos, "minimal", "tags", "-", none, unknown},
		{fluxReceivers, "minimal", "status", "Unknown", unknown, unknown},
	}
	for _, tt := range tests {
		t.Run(tt.kind+"/"+tt.name+"/"+tt.column, func(t *testing.T) {
			p := curatedProfile(t, tt.kind)
			obj := recordedPath(t, tt.kind+"/"+tt.name)
			i := p.ColumnIndex(tt.column)
			if i < 0 {
				t.Fatalf("no column %q", tt.column)
			}
			row := model.BuildRow(p, "test", obj)
			cell := row.Cells[i]
			if text := model.CellText(p.Columns[i], cell, now); text != tt.text || cell.Status != tt.cell || row.Status != tt.row {
				t.Errorf("got %q (cell %q, row %q), want %q (cell %q, row %q)", text, cell.Status, row.Status, tt.text, tt.cell, tt.row)
			}
		})
	}

	links := []struct {
		kind, name, column string
		want               model.ObjectRef
	}{
		{fluxKustomizations, "ready", "source", model.ObjectRef{Kind: fluxGitRepos, Namespace: "flux-system", Name: "flux-system"}},
		{fluxHelmReleases, "ready", "chart", model.ObjectRef{Kind: "helmrepositories.source.toolkit.fluxcd.io", Namespace: "flux-system", Name: "podinfo"}},
		{fluxHelmReleases, "chartref", "chart", model.ObjectRef{Kind: "ocirepositories.source.toolkit.fluxcd.io", Namespace: "shop", Name: "frontend-chart"}},
		{fluxAlerts, "static", "provider", model.ObjectRef{Kind: fluxProviders, Namespace: "flux-system", Name: "slack"}},
		{fluxImagePolicies, "v1beta2", "imageRepository", model.ObjectRef{Kind: fluxImageRepos, Namespace: "images", Name: "nginx"}},
	}
	for _, l := range links {
		p := curatedProfile(t, l.kind)
		row := model.BuildRow(p, "test", recordedPath(t, l.kind+"/"+l.name))
		if got := row.Cells[p.ColumnIndex(l.column)].Link; got == nil || *got != l.want {
			t.Errorf("%s/%s %s: link %+v, want %+v", l.kind, l.name, l.column, got, l.want)
		}
	}
	// A reference to a kind Cogpit has no view for carries no link.
	p := curatedProfile(t, fluxExternal)
	if l := model.BuildRow(p, "test", recordedPath(t, fluxExternal+"/ready")).Cells[p.ColumnIndex("source")].Link; l != nil {
		t.Errorf("an ArtifactGenerator links to %+v", l)
	}
}

// TestFluxStateOf: the dashboard's states match the rows' tokens, and an unknown key is unknown.
func TestFluxStateOf(t *testing.T) {
	for _, k := range kinds.FluxKinds() {
		p := curatedProfile(t, k.Key)
		for _, c := range loadCases(t, k.Key) {
			if c.obj.GetDeletionTimestamp() != nil {
				continue
			}
			state := kinds.FluxStateOf(k.Key, c.obj)
			if row := model.BuildRow(p, "test", c.obj); row.Status != kinds.FluxStateToken(state) {
				t.Errorf("%s/%s: state %s (token %s), row %s", k.Key, c.name, state, kinds.FluxStateToken(state), row.Status)
			}
		}
	}
	if s := kinds.FluxStateOf("pods", recordedPath(t, fluxKustomizations+"/ready")); s != kinds.FluxUnknown {
		t.Errorf("pods: %s", s)
	}
	state, msg, since := kinds.FluxStatusOf(fluxKustomizations, recordedPath(t, fluxKustomizations+"/failing"))
	if state != kinds.FluxNotReady || !strings.HasPrefix(msg, "kustomize build failed") || since.IsZero() {
		t.Errorf("failing: %s %q %v", state, msg, since)
	}
}

// TestFluxKeepsSecretsOut: a source URL's credentials, a Helm release's values, a provider's
// address and proxy and a receiver's webhook path appear in no cell, searchable field or drawer
// (AGENTS.md rule 9; they stay in the object's YAML, H-63).
func TestFluxKeepsSecretsOut(t *testing.T) {
	for _, k := range kinds.FluxKinds() {
		p := curatedProfile(t, k.Key)
		for _, c := range loadCases(t, k.Key) {
			raw, err := json.Marshal(render(t, p, c.obj))
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{"must-never-show", "hunter2", "ghp_", "secret-token", "user:pass"} {
				if strings.Contains(string(raw), secret) {
					t.Errorf("%s/%s shows %q", k.Key, c.name, secret)
				}
			}
		}
	}
}

// TestFluxURL masks the user information of a URL, but not SSH's "git@".
func TestFluxURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/acme/repo":             "https://github.com/acme/repo",
		"https://user:pw@github.com/acme/repo":     "https://<redacted>@github.com/acme/repo",
		"https://token@github.com/a@b":             "https://<redacted>@github.com/a@b",
		"ssh://git@github.com/acme/repo":           "ssh://git@github.com/acme/repo",
		"ssh://deploy:key@host:22/repo?x=a@b#frag": "ssh://<redacted>@host:22/repo?x=a@b#frag",
		"git@github.com:acme/repo.git":             "git@github.com:acme/repo.git",
		"":                                         "",
	} {
		p := curatedProfile(t, fluxGitRepos)
		obj := recordedPath(t, fluxGitRepos+"/minimal").DeepCopy()
		obj.Object["spec"] = map[string]any{"url": in}
		if got := model.BuildRow(p, "test", obj).Cells[p.ColumnIndex("url")].Text; got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

// TestProfileForFlux: a definition at a version the profile reads gets the curated profile (a copy
// at a fallback version); at a version it does not read, the generic one.
func TestProfileForFlux(t *testing.T) {
	shared := curatedProfile(t, fluxHelmReleases)
	k := kinds.CustomKind{Group: "helm.toolkit.fluxcd.io", Version: "v2", Plural: "helmreleases", Kind: "HelmRelease", Namespaced: true}
	if p := kinds.ProfileFor(k); p != shared {
		t.Errorf("v2: %q", p.Title)
	}
	k.Version = "v2beta1"
	if p := kinds.ProfileFor(k); p == shared || p.Title != "Helm Releases" || p.Version != "v2beta1" {
		t.Errorf("v2beta1: %q at %s", p.Title, p.Version)
	}
	k.Version = "v3alpha1"
	if p := kinds.ProfileFor(k); p.Title != "HelmRelease" {
		t.Errorf("an unknown version: %q, want the generic profile", p.Title)
	}
	alerts := kinds.CustomKind{Group: "notification.toolkit.fluxcd.io", Version: "v1beta3", Plural: "alerts", Kind: "Alert", Namespaced: true}
	if p := kinds.ProfileFor(alerts); p.Title != "Alerts" {
		t.Errorf("alerts v1beta3: %q", p.Title)
	}
}

// TestFluxDrawers spot-checks the drawers: the request the controller handled as a time, the
// Helm release's history, a receiver's resources as links.
func TestFluxDrawers(t *testing.T) {
	item := func(sections []model.DetailSection, title, name string) model.DetailItem {
		for _, s := range sections {
			if s.Title != title {
				continue
			}
			for _, it := range s.Items {
				if it.Name == name {
					return it
				}
			}
		}
		return model.DetailItem{}
	}
	hr := curatedProfile(t, fluxHelmReleases)
	d := hr.Detail(recordedPath(t, fluxHelmReleases+"/ready"))
	if it := item(d, model.TitleProperties, "Reconcile Handled"); !model.HasMarks(it.Value) {
		t.Errorf("Reconcile Handled %q: want a time mark", it.Value)
	}
	if len(d) < 2 || d[1].Title != "History" || len(d[1].Table.Rows) != 2 || d[1].Table.Rows[0][0] != "4" {
		t.Errorf("history %+v", d)
	}
	rc := curatedProfile(t, fluxReceivers)
	res := item(rc.Detail(recordedPath(t, fluxReceivers+"/ready")), model.TitleProperties, "Resources")
	if len(res.Chips) != 2 || res.Chips[0].Link == nil || res.Chips[0].Link.Kind != fluxGitRepos || res.Chips[1].Link != nil {
		t.Errorf("receiver resources %+v", res.Chips)
	}
	ks := curatedProfile(t, fluxKustomizations)
	deps := item(ks.Detail(recordedPath(t, fluxKustomizations+"/ready")), model.TitleProperties, "Depends On")
	if len(deps.Chips) != 2 || deps.Chips[1].Text != "platform/crds" || deps.Chips[1].Link.Namespace != "platform" {
		t.Errorf("depends on %+v", deps.Chips)
	}
}
