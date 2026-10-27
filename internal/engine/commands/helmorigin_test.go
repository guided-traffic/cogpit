package commands_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// originHarness is the upgrade harness with two more repositories that list the chart web: lab,
// served over plain http, and edge, the test repository's server with insecure_skip_tls_verify in
// its entry. Neither has credentials, so nothing waits for an allowance (H-40).
func originHarness(t *testing.T) *repoUpgradeHarness {
	t.Helper()
	lab := testkit.NewPlainHelmRepo(t, webChartV("0.1.0"), webChartV("0.3.0"))
	h := newRepoUpgradeHarness(t, engine.NewMemoryHelmJournal(), nil, struct {
		name string
		repo *testkit.HelmRepo
	}{"lab", lab})
	h.addRepo(t, "edge", h.repo.URL)
	h.setInsecure(t, "edge", true)
	return h
}

// setInsecure sets or clears insecure_skip_tls_verify in a repository's entry of Helm's file, as
// an editor would, and has Cogpit read the file (helm.repo.update reads it first).
func (h *repoUpgradeHarness) setInsecure(t *testing.T, name string, insecure bool) {
	t.Helper()
	for _, e := range h.entries {
		if e.Name == name {
			e.InsecureSkipTLSVerify = insecure
		}
	}
	h.helmFile.Write(h.entries...)
	if _, err := h.dispatch(engine.OriginGUI, "helm.repo.update", map[string]any{"names": []string{name}}); err != nil {
		t.Fatal(err)
	}
}

// chartRow is a chart's row as resources_list gives it to agents.
func chartRow(t *testing.T, h *repoUpgradeHarness, id string) string {
	t.Helper()
	res, err := h.dispatch(engine.OriginExternalAgent, "resources.list", map[string]any{"kind": kinds.HelmChartsKey})
	if err != nil {
		t.Fatal(err)
	}
	js, _ := json.Marshal(res)
	var list struct {
		Rows []json.RawMessage `json:"rows"`
	}
	_ = json.Unmarshal(js, &list)
	for _, r := range list.Rows {
		if strings.Contains(string(r), `"name":"`+id+`"`) {
			return string(r)
		}
	}
	t.Fatalf("no row of %s in %s", id, js)
	return ""
}

// A chart from a repository whose content nothing authenticates is marked (H-41, owner
// 2026-10-14 evening, A14): its object says the origin, its Repository cell "lab (http)" or
// "edge (insecure TLS)" in the warning colour and its drawer why, for the window and agents alike;
// the Install tab and the preview carry the origin its archive came from, and an install's
// confirmation shows the warning. A chart that came another way than the confirmation said is
// refused. A repository over https with the certificate checked is not marked.
func TestHelmChartOriginMarked(t *testing.T) {
	h := originHarness(t)
	httpWarning, insecureWarning := kinds.HelmOriginWarning("lab", kinds.HelmOriginHTTP), kinds.HelmOriginWarning("edge", kinds.HelmOriginInsecureTLS)
	for id, want := range map[string]string{"lab.web": `"lab (http)"`, "edge.web": `"edge (insecure TLS)"`, "test.web": `"test"`} {
		if row := chartRow(t, h, id); !strings.Contains(row, want) {
			t.Errorf("%s's row: %s, want the repository %s", id, row, want)
		}
		obj, err := h.s.Lookup(engine.HelmChartRef("test", strings.TrimSuffix(id, ".web"), "web"))
		if err != nil {
			t.Fatal(err)
		}
		origin, _ := obj.Object["spec"].(map[string]any)["origin"].(string)
		if wantOrigin := map[string]string{"lab.web": kinds.HelmOriginHTTP, "edge.web": kinds.HelmOriginInsecureTLS}[id]; origin != wantOrigin {
			t.Errorf("%s's spec.origin %q, want %q", id, origin, wantOrigin)
		}
	}
	got, err := h.dispatch(engine.OriginExternalAgent, "object.get", map[string]any{"ref": engine.HelmChartRef("test", "lab", "web")})
	if err != nil {
		t.Fatal(err)
	}
	if js, _ := json.Marshal(got); !strings.Contains(string(js), httpWarning) {
		t.Errorf("lab.web's drawer does not say why: %s", js)
	}

	// The Install tab and the preview carry the origin its archive came from.
	res, err := h.dispatch(engine.OriginExternalAgent, "helm.chart.installTab", map[string]any{"ref": engine.HelmChartRef("test", "lab", "web")})
	if err != nil {
		t.Fatal(err)
	}
	tab := res.(commands.EditResult).Tab
	if tab.HelmInstall == nil || tab.HelmInstall.Origin != kinds.HelmOriginHTTP || tab.HelmInstall.Repository != "lab" {
		t.Fatalf("the install tab %+v", tab.HelmInstall)
	}
	pre, err := h.dispatch(engine.OriginExternalAgent, "helm.chart.preview", map[string]any{"tab": tab.ID, "namespace": "apps", "name": "lab-web"})
	if err != nil {
		t.Fatal(err)
	}
	if p := pre.(commands.InstallPreview); p.Origin != kinds.HelmOriginHTTP || p.Warning != httpWarning {
		t.Errorf("the preview: origin %q, warning %q", p.Origin, p.Warning)
	}

	// An agent's install from the tab, and inline from each repository: the confirmation's warning.
	installs := len(h.server.recordedInstalls())
	if _, err := h.dispatch(engine.OriginExternalAgent, "helm.chart.install", map[string]any{"tab": tab.ID, "textRevision": h.textRevision(t, tab.ID),
		"namespace": "apps", "name": "lab-web"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ repository, name, want string }{{"test", "checked-web", ""}, {"edge", "edge-web", insecureWarning}} {
		if _, err := h.dispatch(engine.OriginExternalAgent, "helm.chart.install", map[string]any{"repository": c.repository, "chart": "web", "version": "0.2.0",
			"values": "", "namespace": "apps", "name": c.name}); err != nil {
			t.Fatalf("%s: %v", c.repository, err)
		}
	}
	asked := h.confirmations()
	if len(asked) != 3 || asked[0].Warning != httpWarning || asked[1].Warning != "" || asked[2].Warning != insecureWarning {
		for _, a := range asked {
			t.Logf("asked %s: warning %q", a.Command, a.Warning)
		}
		t.Fatalf("%d confirmations", len(asked))
	}
	if n := len(h.server.recordedInstalls()) - installs; n != 3 {
		t.Errorf("%d installs", n)
	}

	// edge's entry loses insecure_skip_tls_verify while the user reads the confirmation: the chart
	// would come checked, not as the warning said; nothing is installed.
	h.mu.Lock()
	h.onAsk = func() {
		h.mu.Lock()
		h.onAsk = nil
		h.mu.Unlock()
		for _, e := range h.entries {
			if e.Name == "edge" {
				e.InsecureSkipTLSVerify = false
			}
		}
		h.helmFile.Write(h.entries...)
		if _, err := h.s.HelmRepos().Sync(); err != nil {
			t.Error(err)
		}
	}
	h.mu.Unlock()
	installs = len(h.server.recordedInstalls())
	_, err = h.dispatch(engine.OriginExternalAgent, "helm.chart.install", map[string]any{"repository": "edge", "chart": "web", "version": "0.2.0",
		"values": "", "namespace": "apps", "name": "edge-two"})
	if !errors.Is(err, engine.ErrTargetsChanged) || len(h.server.recordedInstalls()) != installs {
		t.Errorf("an install whose origin changed while asked: %v (%d installs)", err, len(h.server.recordedInstalls())-installs)
	}
	if last := h.confirmations(); last[len(last)-1].Warning != insecureWarning {
		t.Errorf("the refused install's confirmation: %q", last[len(last)-1].Warning)
	}
}

// An upgrade to a version from a repository whose content nothing authenticates says so (H-41,
// A14): the Upgrade tab carries the repository's origin, an agent's confirmation the warning (none
// for the release's own chart or a checked repository); an origin that changed while the user read
// it refuses the upgrade, and the window's tab is marked anew before an upgrade from it runs.
func TestHelmUpgradeOriginMarked(t *testing.T) {
	h := originHarness(t)
	tab := h.openTab(t, engine.OriginGUI, map[string]any{})
	if c := tab.Tab.Helm; c == nil || c.Repository != "lab" || c.Origin != kinds.HelmOriginHTTP || c.Version != "0.3.0" {
		t.Fatalf("the tab's choice %+v", c)
	}
	h.mu.Lock()
	h.answer = false // the warnings only: nothing upgrades
	h.mu.Unlock()
	for _, c := range []struct {
		params map[string]any
		want   string
	}{
		{map[string]any{"version": "0.3.0", "repository": "lab"}, kinds.HelmOriginWarning("lab", kinds.HelmOriginHTTP)},
		{map[string]any{"version": "0.2.0", "repository": "edge"}, kinds.HelmOriginWarning("edge", kinds.HelmOriginInsecureTLS)},
		{map[string]any{"version": "0.2.0", "repository": "test"}, ""},
		{map[string]any{}, ""}, // the release's own chart: nothing downloaded
	} {
		c.params["ref"], c.params["values"], c.params["revision"] = webRef, "greeting: hi\n", 1
		before := len(h.confirmations())
		if _, err := h.dispatch(engine.OriginExternalAgent, "helm.release.upgrade", c.params); !errors.Is(err, engine.ErrDeclined) {
			t.Fatalf("%v: %v", c.params, err)
		}
		if asked := h.confirmations(); len(asked) != before+1 || asked[before].Warning != c.want {
			t.Errorf("%v: warning %q, want %q", c.params, asked[len(asked)-1].Warning, c.want)
		}
	}

	// edge's entry changes while the user reads the agent's upgrade: refused, nothing upgraded.
	h.mu.Lock()
	h.answer = true
	h.onAsk = func() {
		h.mu.Lock()
		h.onAsk = nil
		h.mu.Unlock()
		for _, e := range h.entries {
			if e.Name == "edge" {
				e.InsecureSkipTLSVerify = false
			}
		}
		h.helmFile.Write(h.entries...)
		if _, err := h.s.HelmRepos().Sync(); err != nil {
			t.Error(err)
		}
	}
	h.mu.Unlock()
	_, err := h.dispatch(engine.OriginExternalAgent, "helm.release.upgrade", map[string]any{"ref": webRef, "values": "greeting: hi\n", "revision": 1,
		"version": "0.2.0", "repository": "edge"})
	if !errors.Is(err, engine.ErrTargetsChanged) || len(h.upgrades()) != 0 {
		t.Errorf("an upgrade whose origin changed while asked: %v (%d upgrades)", err, len(h.upgrades()))
	}

	// The window's tab on edge shows edge's origin; it changes in Helm's file before Save: the save
	// is refused and the tab marked anew; the next save upgrades.
	h.setInsecure(t, "edge", true)
	h.openTab(t, engine.OriginGUI, map[string]any{"repository": "edge", "version": "0.2.0"})
	if c := h.choice(t, tab.Tab.ID); c.Repository != "edge" || c.Origin != kinds.HelmOriginInsecureTLS {
		t.Fatalf("the tab on edge %+v", c)
	}
	h.setInsecure(t, "edge", false)
	save := map[string]any{"ref": webRef, "tab": tab.Tab.ID, "textRevision": h.textRevision(t, tab.Tab.ID), "version": "0.2.0", "repository": "edge"}
	if _, err := h.dispatch(engine.OriginGUI, "helm.release.upgrade", save); !errors.Is(err, engine.ErrTargetsChanged) || len(h.upgrades()) != 0 {
		t.Fatalf("a save after the origin changed: %v (%d upgrades)", err, len(h.upgrades()))
	}
	if c := h.choice(t, tab.Tab.ID); c.Origin != "" {
		t.Errorf("the tab still marks %q", c.Origin)
	}
	if _, err := h.dispatch(engine.OriginGUI, "helm.release.upgrade", save); err != nil {
		t.Fatal(err)
	}
	if up := h.upgrades(); len(up) != 1 || up[0].Chart == nil || up[0].Chart.Repository != "edge" || up[0].Chart.Origin != "" {
		t.Errorf("upgraded %+v", up)
	}
}

// The origin a chart object carries follows Helm's file: an entry that gains or loses
// insecure_skip_tls_verify is marked anew when Cogpit reads the file (every 2 seconds, the harness's
// 20 ms), without waiting for its next index download (the Charts view is not open: nothing
// downloads).
func TestHelmChartOriginFollowsHelmsFile(t *testing.T) {
	h := originHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	origin := func() string {
		obj, err := h.s.Lookup(engine.HelmChartRef("test", "edge", "web"))
		if err != nil {
			return "missing"
		}
		o, _ := obj.Object["spec"].(map[string]any)["origin"].(string)
		return o
	}
	if _, err := h.s.Objects(ctx, kinds.HelmChartsKey, nil); err != nil { // the kind in the cache, as the Charts view has it
		t.Fatal(err)
	}
	if !testkit.WaitFor(ctx, func() bool { return origin() == kinds.HelmOriginInsecureTLS }) {
		t.Fatalf("edge.web's origin %q", origin())
	}
	downloads := h.repo.Requests("/index.yaml")
	for _, insecure := range []bool{false, true} {
		for _, e := range h.entries {
			if e.Name == "edge" {
				e.InsecureSkipTLSVerify = insecure
			}
		}
		h.helmFile.Write(h.entries...)
		want := map[bool]string{true: kinds.HelmOriginInsecureTLS}[insecure]
		if !testkit.WaitFor(ctx, func() bool { return origin() == want }) {
			t.Fatalf("edge.web's origin %q, want %q after insecure_skip_tls_verify became %v", origin(), want, insecure)
		}
	}
	if h.repo.Requests("/index.yaml") != downloads {
		t.Error("the mark waited for a download")
	}
}
