package commands_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	repo "helm.sh/helm/v4/pkg/repo/v1"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// docsReadme is the README of the test chart docs at 1.0.0.
const docsReadme = "# Docs\n\nA chart with a **README**.\n\n```sh\nhelm install docs test/docs\n```\n"

// docsChart is the test chart docs at a version, with a README when readme is set.
func docsChart(version, readme string) testkit.TestChart {
	return testkit.TestChart{Name: "docs", Version: version, AppVersion: "2." + version, Description: "A chart with a README",
		Values: "replicas: 1\n", Readme: readme,
		Templates: map[string]string{"templates/cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: {{ .Release.Name }}\n"}}
}

// docsRef is the docs chart's reference.
var docsRef = engine.HelmChartRef("test", "test", "docs")

// chartDetail waits until the GUI's drawer model of ref satisfies ok and returns it.
func chartDetail(t *testing.T, h *chartHarness, ref model.ObjectRef, what string, ok func(*engine.DetailModel) bool) *engine.DetailModel {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var m *engine.DetailModel
	if !testkit.WaitFor(ctx, func() bool {
		var err error
		m, err = h.s.Detail(ref)
		return err == nil && m != nil && m.HelmChart != nil && ok(m)
	}) {
		got, _ := json.Marshal(m)
		t.Fatalf("%s: %s", what, got)
	}
	return m
}

// readmeText is the drawer's README section text.
func readmeText(m *engine.DetailModel) string {
	for _, s := range m.Sections {
		if s.Title == engine.ReadmeSection && len(s.Items) == 1 {
			return s.Items[0].Value
		}
	}
	return ""
}

func loaded(m *engine.DetailModel) bool { return m.HelmChart.Readme.State == engine.ReadmeLoaded }

// sectionTitles are a drawer model's section titles in order.
func sectionTitles(m *engine.DetailModel) []string {
	var out []string
	for _, s := range m.Sections {
		out = append(out, s.Title)
	}
	return out
}

// The chart drawer's README: nothing is downloaded for the list or for a drawer that shows another
// object; the drawer's chart is downloaded once (its archive, checked against the index's digest),
// its README shown before the Versions table and kept in Cogpit's cache folder (owner-only), where
// a second Cogpit finds it without a download. ui.setChartVersion shows another version, its
// properties and its README (a chart without one says so); Install then installs that version; the
// version ends when the drawer shows another object, and an unknown version or a closed drawer is
// refused.
func TestHelmChartDrawerVersion(t *testing.T) {
	h := newChartHarness(t, nil)
	h.repo.Add(docsChart("1.0.0", docsReadme), docsChart("0.9.0", ""))
	h.addRepo(t)
	h.charts(t, 2)
	if _, err := h.dispatch(engine.OriginGUI, "ui.openView", map[string]any{"view": "helmcharts"}); err != nil {
		t.Fatal(err)
	}
	m, err := h.s.Detail(docsRef)
	if err != nil || m.HelmChart == nil || m.HelmChart.Readme.State != engine.ReadmeNotLoaded || h.repo.Requests("/docs-1.0.0.tgz") != 0 {
		t.Fatalf("before the drawer shows it: %+v %v (%d downloads)", m, err, h.repo.Requests("/docs-1.0.0.tgz"))
	}
	if _, err := h.dispatch(engine.OriginGUI, "ui.setChartVersion", map[string]any{"ref": docsRef, "version": "0.9.0"}); !errors.Is(err, engine.ErrDrawerNotOpen) {
		t.Errorf("a version for a closed drawer: %v", err)
	}

	if _, err := h.dispatch(engine.OriginGUI, "ui.navigate", map[string]any{"ref": docsRef}); err != nil {
		t.Fatal(err)
	}
	m = chartDetail(t, h, docsRef, "the README", loaded)
	hc := m.HelmChart
	if hc.Version != "1.0.0" || hc.Listed != "1.0.0" || !slices.Equal(hc.Versions, []string{"1.0.0", "0.9.0"}) || hc.Readme.File != "README.md" ||
		!hc.Readme.Verified || hc.Readme.Size != len(docsReadme) || hc.Readme.Truncated || readmeText(m) != docsReadme {
		t.Errorf("drawer %+v, README %q", hc, readmeText(m))
	}
	if got := sectionTitles(m); !slices.Equal(got, []string{model.TitleProperties, "Description", engine.ReadmeSection, "Versions"}) {
		t.Errorf("sections %v", got)
	}
	if _, err := h.s.Detail(docsRef); err != nil || h.repo.Requests("/docs-1.0.0.tgz") != 1 {
		t.Errorf("downloads of the archive: %d (%v)", h.repo.Requests("/docs-1.0.0.tgz"), err)
	}
	kept, _ := filepath.Glob(filepath.Join(h.cacheDir, "helm", "readmes", "*.json"))
	if len(kept) != 1 {
		t.Fatalf("kept READMEs %v", kept)
	}
	assertMode(t, kept[0], 0o600)
	assertMode(t, filepath.Dir(kept[0]), 0o700)

	var u engine.UIState
	res, err := h.dispatch(engine.OriginExternalAgent, "ui.setChartVersion", map[string]any{"ref": docsRef, "version": "0.9.0"})
	if err != nil {
		t.Fatal(err)
	}
	u = res.(engine.UIState)
	if u.DrawerChartVersion != "0.9.0" {
		t.Errorf("drawerChartVersion %q", u.DrawerChartVersion)
	}
	m = chartDetail(t, h, docsRef, "the other version's README", func(m *engine.DetailModel) bool {
		return m.HelmChart.Version == "0.9.0" && m.HelmChart.Readme.State == engine.ReadmeNone
	})
	if v := m.Sections[0].Items; !slices.ContainsFunc(v, func(it model.DetailItem) bool { return it.Name == "Version" && it.Value == "0.9.0" }) ||
		readmeText(m) != "The chart has no README." || h.repo.Requests("/docs-0.9.0.tgz") != 1 {
		t.Errorf("version 0.9.0: %+v %q (%d downloads)", v, readmeText(m), h.repo.Requests("/docs-0.9.0.tgz"))
	}
	if _, err := h.dispatch(engine.OriginGUI, "ui.setChartVersion", map[string]any{"ref": docsRef, "version": "7.7.7"}); err == nil ||
		!strings.Contains(err.Error(), "no version \"7.7.7\"") {
		t.Errorf("an unknown version: %v", err)
	}
	if got := h.s.DrawerChartVersionOf(docsRef); got != "0.9.0" {
		t.Errorf("the version Install takes: %q", got)
	}
	if _, err := h.dispatch(engine.OriginGUI, "ui.setChartVersion", map[string]any{"ref": docsRef, "version": "1.0.0"}); err != nil ||
		h.s.UIState().DrawerChartVersion != "" {
		t.Errorf("the listed version: %v %q", err, h.s.UIState().DrawerChartVersion)
	}
	if _, err := h.dispatch(engine.OriginGUI, "ui.setChartVersion", map[string]any{"ref": docsRef, "version": "0.9.0"}); err != nil {
		t.Fatal(err)
	}
	web := engine.HelmChartRef("test", "test", "web")
	if _, err := h.dispatch(engine.OriginGUI, "ui.navigate", map[string]any{"ref": web}); err != nil || h.s.UIState().DrawerChartVersion != "" {
		t.Errorf("another object kept the version %q (%v)", h.s.UIState().DrawerChartVersion, err)
	}

	// A second Cogpit on the same cache finds the verified README without a download.
	again := engine.NewHelmRepos(h.helmFile.Path, h.cacheDir, engine.WithChartFetcher(&helm.Fetcher{Client: h.repo.Client()}))
	other := newChartHarnessOn(t, again)
	other.charts(t, 2)
	before := h.repo.Requests("/docs-1.0.0.tgz")
	if _, err := other.dispatch(engine.OriginGUI, "ui.navigate", map[string]any{"ref": docsRef}); err != nil {
		t.Fatal(err)
	}
	if m := chartDetail(t, other, docsRef, "the kept README", loaded); readmeText(m) != docsReadme || h.repo.Requests("/docs-1.0.0.tgz") != before {
		t.Errorf("the kept README %q (%d downloads)", readmeText(m), h.repo.Requests("/docs-1.0.0.tgz")-before)
	}
}

// newChartHarnessOn is a chart harness on repositories made by the test (another Cogpit on the
// same Helm file and cache folder).
func newChartHarnessOn(t *testing.T, repos *engine.HelmRepos) *chartHarness {
	t.Helper()
	h := &chartHarness{src: testkit.NewMemorySource(), repos: repos, audit: &syncBuffer{}, logs: &syncBuffer{}, answer: true}
	h.s = engine.New(engine.Options{Connector: &chartConnector{src: h.src, actions: &fakeWriter{}}, SnapshotInterval: 10 * time.Millisecond,
		Audit: h.audit, HelmRepos: repos})
	if err := commands.RegisterAll(h.s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.s.Close)
	if err := h.s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	return h
}

// Install from the drawer installs the version the drawer shows.
func TestHelmChartDrawerInstallsItsVersion(t *testing.T) {
	h, _ := installHarness(t)
	web := engine.HelmChartRef("test", "test", "web")
	if _, err := h.dispatch(engine.OriginGUI, "ui.navigate", map[string]any{"ref": web}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.dispatch(engine.OriginGUI, "ui.setChartVersion", map[string]any{"ref": web, "version": "0.1.0"}); err != nil {
		t.Fatal(err)
	}
	tab := openInstallTab(t, h, engine.OriginGUI, "")
	if tab.Tab.HelmInstall == nil || tab.Tab.HelmInstall.Version != "0.1.0" {
		t.Errorf("the install tab %+v", tab.Tab.HelmInstall)
	}
}

// unframed is an agent's README section text without the frame around it (A31).
func unframed(text string) string {
	return strings.TrimSuffix(strings.TrimPrefix(text, engine.ReadmeFrameStart+"\n"), "\n"+engine.ReadmeFrameEnd)
}

// An agent's object_get reads a chart's README, waiting for its download as the drawer would: the
// text in the README section, framed as the chart author's text, not instructions (A31, owner
// 2026-10-14 evening); the state in helmChart. The window reads it unframed.
func TestHelmChartReadmeForAgents(t *testing.T) {
	h := newChartHarness(t, nil)
	h.repo.Add(docsChart("1.0.0", docsReadme))
	h.addRepo(t)
	h.charts(t, 2)
	res, err := h.dispatch(engine.OriginExternalAgent, "object.get", map[string]any{"ref": docsRef, "noYaml": true})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(*engine.DetailModel)
	if m.HelmChart == nil || m.HelmChart.Readme.State != engine.ReadmeLoaded || readmeText(m) != engine.ReadmeFrameStart+"\n"+docsReadme+"\n"+engine.ReadmeFrameEnd ||
		h.repo.Requests("/docs-1.0.0.tgz") != 1 {
		t.Errorf("object_get: %+v %q", m.HelmChart, readmeText(m))
	}
	res, err = h.dispatch(engine.OriginGUI, "object.get", map[string]any{"ref": docsRef, "noYaml": true})
	if err != nil {
		t.Fatal(err)
	}
	if got := readmeText(res.(*engine.DetailModel)); got != docsReadme {
		t.Errorf("the window's README: %q", got)
	}
}

// The README's archive is bounded (a chart larger than the bound fails with a reason), its README
// is cut at 256 KiB with the size it had, and a failed download is not kept.
func TestHelmChartReadmeBounds(t *testing.T) {
	long := strings.Repeat("A line of the README, long enough.\n", helm.MaxReadmeBytes/35+500)
	h := newChartHarness(t, nil)
	h.repo.Add(docsChart("1.0.0", long))
	h.addRepo(t)
	h.charts(t, 2)
	res, err := h.dispatch(engine.OriginExternalAgent, "object.get", map[string]any{"ref": docsRef, "noYaml": true})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(*engine.DetailModel)
	text := unframed(readmeText(m))
	if r := m.HelmChart.Readme; r.State != engine.ReadmeLoaded || !r.Truncated || r.Size != len(long) || len(text) > helm.MaxReadmeBytes ||
		!strings.HasPrefix(long, text) {
		t.Errorf("a long README: %+v, %d bytes", r, len(text))
	}

	tls := httptest.NewTLSServer(http.NotFoundHandler()) // httptest's TLS servers share one certificate
	defer tls.Close()
	small := newChartHarness(t, nil, engine.WithChartFetcher(&helm.Fetcher{Client: tls.Client(), MaxChart: 64}))
	small.repo.Add(docsChart("1.0.0", docsReadme))
	small.addRepo(t)
	small.charts(t, 2)
	res, err = small.dispatch(engine.OriginExternalAgent, "object.get", map[string]any{"ref": docsRef, "noYaml": true})
	if err != nil {
		t.Fatal(err)
	}
	m = res.(*engine.DetailModel)
	if r := m.HelmChart.Readme; r.State != engine.ReadmeFailed || !strings.Contains(r.Error, "larger than the 64 B Cogpit downloads") ||
		!strings.Contains(readmeText(m), "could not be loaded") {
		t.Errorf("an archive over the bound: %+v %q", r, readmeText(m))
	}
	if kept, _ := filepath.Glob(filepath.Join(small.cacheDir, "helm", "readmes", "*.json")); len(kept) != 0 {
		t.Errorf("a failed README was kept: %v", kept)
	}
}

// authRecorder serves an archive and counts the requests that carried an Authorization header.
type authRecorder struct {
	mu        sync.Mutex
	requests  int
	withCreds int
}

// A README behind a login: the archive downloads with the repository's credentials from its own
// host; an archive the index puts on another host gets none; no credential reaches the drawer's
// model, the UI state, the events, the audit log or the log.
func TestHelmChartReadmeCredentials(t *testing.T) {
	const user, pass = "readme-user-91", "pw-readme-92"
	h := newChartHarness(t, nil)
	h.repo.Add(docsChart("1.0.0", docsReadme))
	h.repo.RequireBasicAuth(user, pass)
	h.helmFile.Write(&repo.Entry{Name: "test", URL: h.repo.URL, Username: user, Password: pass})
	if _, err := h.dispatch(engine.OriginGUI, "helm.repo.update", struct{}{}); err != nil {
		t.Fatal(err)
	}
	h.charts(t, 2)
	if _, err := h.dispatch(engine.OriginGUI, "ui.navigate", map[string]any{"ref": docsRef}); err != nil {
		t.Fatal(err)
	}
	m := chartDetail(t, h, docsRef, "the README behind the login", loaded)
	if readmeText(m) != docsReadme || h.repo.Authorized("/docs-1.0.0.tgz") != 1 {
		t.Errorf("README %q, downloads with credentials %d", readmeText(m), h.repo.Authorized("/docs-1.0.0.tgz"))
	}

	// The index puts web's archive on another host: it gets no credentials.
	rec := &authRecorder{}
	archive := h.repo.Archive("web-0.2.0.tgz")
	elsewhere := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rec.mu.Lock()
		rec.requests++
		if req.Header.Get("Authorization") != "" {
			rec.withCreds++
		}
		rec.mu.Unlock()
		_, _ = w.Write(archive)
	}))
	defer elsewhere.Close()
	idx, err := repo.LoadIndexFile(writeTemp(t, fetch(t, h, "/index.yaml", user, pass)))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range idx.Entries["web"] {
		v.URLs = []string{elsewhere.URL + "/" + v.Name + "-" + v.Version + ".tgz"}
	}
	out := filepath.Join(t.TempDir(), "index.yaml")
	if err := idx.WriteFile(out, 0o600); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out)
	h.repo.Serve("/index.yaml", data)
	if _, err := h.dispatch(engine.OriginGUI, "helm.repo.update", struct{}{}); err != nil {
		t.Fatal(err)
	}
	web := engine.HelmChartRef("test", "test", "web")
	if _, err := h.dispatch(engine.OriginGUI, "ui.navigate", map[string]any{"ref": web}); err != nil {
		t.Fatal(err)
	}
	chartDetail(t, h, web, "web's README state", func(m *engine.DetailModel) bool {
		return m.HelmChart.Readme.State == engine.ReadmeNone
	})
	rec.mu.Lock()
	if rec.requests != 1 || rec.withCreds != 0 {
		t.Errorf("the other host saw %d requests, %d with credentials", rec.requests, rec.withCreds)
	}
	rec.mu.Unlock()

	detailJSON, _ := json.Marshal(m)
	uiJSON, _ := json.Marshal(h.s.UIState())
	h.mu.Lock()
	evJSON, _ := json.Marshal(h.events)
	h.mu.Unlock()
	for what, text := range map[string]string{"drawer": string(detailJSON), "ui state": string(uiJSON), "events": string(evJSON),
		"audit log": bufText(h.audit), "log": bufText(h.logs)} {
		for _, secret := range []string{pass, user} {
			if strings.Contains(text, secret) {
				t.Errorf("%s holds a credential", what)
			}
		}
	}
}

// fetch downloads a path of the test repository with its credentials.
func fetch(t *testing.T, h *chartHarness, path, user, pass string) []byte {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, h.repo.URL+path, nil)
	req.SetBasicAuth(user, pass)
	resp, err := h.repo.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("%s: %d %v", path, resp.StatusCode, err)
	}
	return data
}

// writeTemp writes data into a temporary file and returns its path.
func writeTemp(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
