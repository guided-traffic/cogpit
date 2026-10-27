package commands_test

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	repo "helm.sh/helm/v4/pkg/repo/v1"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// webChartV is the test repository's chart web at a version: a ConfigMap from .Values.greeting.
func webChartV(version string) testkit.TestChart {
	return testkit.TestChart{
		Name: "web", Version: version, AppVersion: "1." + version, Description: "A web server",
		Values: "# The greeting the ConfigMap holds.\ngreeting: hello\npassword: \"\"\n",
		Templates: map[string]string{"templates/cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: {{ .Release.Name }}\n" +
			"data:\n  greeting: {{ .Values.greeting | quote }}\n"},
	}
}

// chartHarness is a session with the Helm repositories of a temporary Helm repositories file, a
// local repository served over TLS, the Helm releases, the local kinds and a recording confirmer.
type chartHarness struct {
	s        *engine.Session
	src      *testkit.MemorySource
	repo     *testkit.HelmRepo
	repos    *engine.HelmRepos
	helmFile *testkit.HelmRepoFile
	cacheDir string
	audit    *syncBuffer
	logs     *syncBuffer
	journal  *engine.MemoryHelmJournal

	mu     sync.Mutex
	asked  []engine.ConfirmRequest
	answer bool
	events []engine.Event
}

// chartConnector connects "test" with the test profiles, Helm's releases and the local kinds; its
// backend's writer is actions.
type chartConnector struct {
	src     *testkit.MemorySource
	actions engine.Actions
}

func (c *chartConnector) Contexts() ([]engine.ContextInfo, error) {
	return []engine.ContextInfo{{Name: "test", Current: true}}, nil
}

func (c *chartConnector) Connect(_ context.Context, name string) (*engine.Backend, error) {
	ch := cache.New(c.src, cache.Options{WarmPeriod: time.Minute})
	profiles := append(append(testProfiles(), helm.NewReleaseProfile()), kinds.Local()...)
	return &engine.Backend{Context: name, Namespace: "default", Cache: ch, Close: ch.Close, Profiles: profiles,
		Conn: &cluster.Conn{}, Actions: c.actions}, nil
}

func newChartHarness(t *testing.T, actions engine.Actions, opts ...engine.HelmReposOption) *chartHarness {
	t.Helper()
	return newChartHarnessWith(t, engine.NewMemoryHelmJournal(), testkit.NewMemorySource(), actions, opts...)
}

// newChartHarnessWith is newChartHarness on a journal and a source of the test's own.
func newChartHarnessWith(t *testing.T, journal *engine.MemoryHelmJournal, src *testkit.MemorySource, actions engine.Actions, opts ...engine.HelmReposOption) *chartHarness {
	t.Helper()
	h := &chartHarness{src: src, helmFile: testkit.NewHelmRepoFile(t), cacheDir: t.TempDir(), audit: &syncBuffer{}, logs: &syncBuffer{},
		journal: journal, answer: true}
	h.repo = testkit.NewHelmRepo(t, webChartV("0.1.0"), webChartV("0.2.0"))
	if actions == nil {
		actions = &fakeWriter{}
	}
	opts = append([]engine.HelmReposOption{engine.WithChartFetcher(&helm.Fetcher{Client: h.repo.Client()}),
		engine.WithRepositoryFilePoll(20 * time.Millisecond)}, opts...)
	h.repos = engine.NewHelmRepos(h.helmFile.Path, h.cacheDir, opts...)
	h.s = engine.New(engine.Options{
		Connector: &chartConnector{src: h.src, actions: actions}, SnapshotInterval: 10 * time.Millisecond,
		Audit: h.audit, Helm: engine.NewHelmOps(h.journal), HelmRepos: h.repos,
		Logger: slog.New(slog.NewTextHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Confirmer: engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.asked = append(h.asked, req)
			return h.answer, nil
		}),
	})
	if err := commands.RegisterAll(h.s.Registry()); err != nil {
		t.Fatal(err)
	}
	h.s.Subscribe(func(ev engine.Event) {
		h.mu.Lock()
		h.events = append(h.events, ev)
		h.mu.Unlock()
	})
	t.Cleanup(h.s.Close)
	if err := h.s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *chartHarness) dispatch(origin engine.Origin, id string, params any) (any, error) {
	raw, _ := json.Marshal(params)
	return h.s.Dispatch(context.Background(), engine.Invocation{ID: id, Params: raw, Origin: origin})
}

func (h *chartHarness) confirmations() []engine.ConfirmRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.asked)
}

// charts waits until the helmcharts kind holds want objects and returns their names.
func (h *chartHarness) charts(t *testing.T, want int) []string {
	t.Helper()
	return h.chartsWithin(t, want, 5*time.Second)
}

func (h *chartHarness) chartsWithin(t *testing.T, want int, d time.Duration) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	var names []string
	if !testkit.WaitFor(ctx, func() bool {
		objs, err := h.s.Objects(ctx, kinds.HelmChartsKey, nil)
		if err != nil {
			return false
		}
		names = names[:0]
		for _, o := range objs {
			names = append(names, o.GetName())
		}
		return len(names) == want
	}) {
		t.Fatalf("charts %v, want %d within %s", names, want, d)
	}
	slices.Sort(names)
	return names
}

// list dispatches helm.repo.list.
func (h *chartHarness) list(t *testing.T, origin engine.Origin) engine.HelmReposList {
	t.Helper()
	res, err := h.dispatch(origin, "helm.repo.list", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	return res.(engine.HelmReposList)
}

// addRepo writes the test repository into Helm's file as "test", as helm repo add does, and
// downloads its index (helm.repo.update).
func (h *chartHarness) addRepo(t *testing.T) engine.HelmRepoInfo {
	t.Helper()
	h.helmFile.Write(h.repo.Entry("test"))
	res, err := h.dispatch(engine.OriginGUI, "helm.repo.update", map[string]any{"names": []string{"test"}})
	if err != nil {
		t.Fatal(err)
	}
	infos := res.([]engine.HelmRepoInfo)
	if len(infos) != 1 {
		t.Fatalf("updated %+v", infos)
	}
	return infos[0]
}

// files lists the regular files under root, relative to it.
func files(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, _ error) error {
		if d != nil && !d.IsDir() {
			out = append(out, filepath.ToSlash(strings.TrimPrefix(p, root)))
		}
		return nil
	})
	return out
}

// The repositories are Helm's file's: helm.repo.list names the file, its status and its
// repositories; an update downloads an index into Cogpit's own cache (owner-only), keyed by the
// repository's name and URL, and its charts become the helmcharts kind. A second Cogpit reads the
// cached index without a download. Nothing is written into Helm's folder; the night run's own
// list and indexes are left where they are, unread.
func TestHelmReposFromHelmsFile(t *testing.T) {
	h := newChartHarness(t, nil)
	if l := h.list(t, engine.OriginGUI); l.File != h.helmFile.Path || l.Status != engine.HelmFileMissing || len(l.Repositories) != 0 ||
		!strings.Contains(l.Hint, "helm repo add") {
		t.Errorf("without Helm's file: %+v", l)
	}
	oldIndex := filepath.Join(h.cacheDir, "helm", "test-index.yaml") // the night run's index
	if err := os.MkdirAll(filepath.Dir(oldIndex), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldIndex, []byte("apiVersion: v1\nentries: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	info := h.addRepo(t)
	if info.Name != "test" || info.URL != h.repo.URL || info.Charts != 1 || info.Updated.IsZero() || info.Error != "" || info.HasCredentials {
		t.Errorf("updated %+v", info)
	}
	if names := h.charts(t, 1); names[0] != "test.web" {
		t.Errorf("charts %v", names)
	}
	web, err := h.s.Lookup(engine.HelmChartRef("test", "test", "web"))
	if err != nil {
		t.Fatal(err)
	}
	spec, _ := web.Object["spec"].(map[string]any)
	if spec["version"] != "0.2.0" || spec["repository"] != "test" || len(spec["versions"].([]any)) != 2 || spec["digest"] == "" {
		t.Errorf("chart object %v", spec)
	}
	l := h.list(t, engine.OriginExternalAgent)
	if l.Status != engine.HelmFileOK || len(l.Repositories) != 1 || l.Repositories[0].Charts != 1 || l.Hint != "" {
		t.Errorf("list %+v", l)
	}
	cached := files(filepath.Join(h.cacheDir, "helm", "indexes"))
	if len(cached) != 1 || !strings.HasSuffix(cached[0], "-index.yaml") {
		t.Fatalf("Cogpit's cache: %v", cached)
	}
	assertMode(t, filepath.Join(h.cacheDir, "helm", "indexes", cached[0]), 0o600)
	if got := files(filepath.Dir(h.helmFile.Path)); len(got) != 1 || got[0] != "/repositories.yaml" {
		t.Errorf("Helm's folder holds %v: Cogpit wrote into it", got)
	}
	if data, err := os.ReadFile(oldIndex); err != nil || string(data) != "apiVersion: v1\nentries: {}\n" {
		t.Errorf("the night run's index: %q %v", data, err)
	}

	// A second Cogpit on the same file and cache reads the cached index without a download.
	before := h.repo.Requests("/index.yaml")
	again := engine.NewHelmRepos(h.helmFile.Path, h.cacheDir, engine.WithChartFetcher(&helm.Fetcher{Client: h.repo.Client()}))
	list, err := again.List()
	if err != nil || len(list.Repositories) != 1 || list.Repositories[0].Charts != 1 || h.repo.Requests("/index.yaml") != before {
		t.Errorf("reloaded %+v %v (%d downloads)", list, err, h.repo.Requests("/index.yaml")-before)
	}

	// An update downloads the index again and lists a new version; nobody is asked.
	h.repo.Add(webChartV("0.3.0"))
	if _, err := h.dispatch(engine.OriginExternalAgent, "helm.repo.update", struct{}{}); err != nil || h.repo.Requests("/index.yaml") != before+1 {
		t.Fatalf("update: %v (%d downloads)", err, h.repo.Requests("/index.yaml")-before)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		o, err := h.s.Lookup(engine.HelmChartRef("test", "test", "web"))
		return err == nil && o.Object["spec"].(map[string]any)["version"] == "0.3.0"
	}) {
		t.Error("the update's new version is not listed")
	}
	if len(h.confirmations()) != 0 {
		t.Errorf("an update asked: %+v", h.confirmations())
	}
	if _, err := h.dispatch(engine.OriginGUI, "helm.repo.update", map[string]any{"names": []string{"nope"}}); err == nil ||
		!strings.Contains(err.Error(), "no Helm repository named nope") {
		t.Errorf("an update of an unknown repository: %v", err)
	}
}

// Helm's file is taken over as it changes: a repository added, given another URL or removed there
// is in Cogpit within 5 seconds, with the Charts view open: a new repository's index downloads at
// once, a changed URL never shows the old URL's charts, a removed repository's charts leave the
// helmcharts kind. The changes are followed at the harness's poll; the 5 seconds (D-29) are the
// default poll and the file's settle of at most 250 ms.
func TestHelmRepoFileChangesWithinFiveSeconds(t *testing.T) {
	if took := engine.DefaultHelmRepositoryFilePoll + 250*time.Millisecond; took >= 5*time.Second {
		t.Errorf("a change of Helm's file is read %s after it was written, want under 5 s", took)
	}
	h := newChartHarness(t, nil)
	other := testkit.NewHelmRepo(t, testkit.TestChart{Name: "api", Version: "1.0.0",
		Templates: map[string]string{"templates/cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n"}})
	if _, err := h.dispatch(engine.OriginGUI, "ui.openView", map[string]any{"view": kinds.HelmChartsKey}); err != nil {
		t.Fatal(err)
	}
	h.list(t, engine.OriginGUI)

	h.helmFile.Write(h.repo.Entry("test"))
	if names := h.chartsWithin(t, 1, 5*time.Second); names[0] != "test.web" {
		t.Errorf("added: %v", names)
	}

	h.helmFile.Write(&repo.Entry{Name: "test", URL: other.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		objs, err := h.s.Objects(ctx, kinds.HelmChartsKey, nil)
		return err == nil && len(objs) == 1 && objs[0].GetName() == "test.api"
	}) {
		t.Errorf("the changed URL's charts are not listed within 5 s: %v", h.chartsWithin(t, 1, time.Second))
	}
	if other.Requests("/index.yaml") != 1 {
		t.Errorf("the new URL's index was downloaded %d times", other.Requests("/index.yaml"))
	}

	h.helmFile.Write()
	h.chartsWithin(t, 0, 5*time.Second)
	if l := h.list(t, engine.OriginGUI); len(l.Repositories) != 0 || l.Status != engine.HelmFileOK || l.Hint == "" {
		t.Errorf("after the removal: %+v", l)
	}
}

// A file that does not parse keeps the last good list and says why, without quoting the file; a
// removed file is an empty list with the hint; an entry Cogpit cannot use is listed with its reason
// and gets no download.
func TestHelmRepoFileErrors(t *testing.T) {
	h := newChartHarness(t, nil)
	h.addRepo(t)
	h.charts(t, 1)

	h.helmFile.WriteText("repositories:\n- name: test\n  password: pw-broken-71\n  url: [unclosed\n")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var l engine.HelmReposList
	if !testkit.WaitFor(ctx, func() bool { l = h.list(t, engine.OriginExternalAgent); return l.Status == engine.HelmFileError }) {
		t.Fatalf("a broken file: %+v", l)
	}
	if len(l.Repositories) != 1 || l.Repositories[0].Charts != 1 || !strings.Contains(l.Error, "does not parse") ||
		strings.Contains(l.Error, "pw-broken-71") || strings.Contains(l.Error, "unclosed") {
		t.Errorf("a broken file: %+v", l)
	}
	h.charts(t, 1)

	h.helmFile.Write(h.repo.Entry("test"), &repo.Entry{Name: "with space", URL: h.repo.URL}, &repo.Entry{Name: "oci", URL: "oci://registry.example.com/charts"})
	if !testkit.WaitFor(ctx, func() bool {
		l = h.list(t, engine.OriginGUI)
		return l.Status == engine.HelmFileOK && len(l.Repositories) == 3
	}) {
		t.Fatalf("fixed: %+v", l)
	}
	before := h.repo.Requests("/index.yaml")
	infos, err := h.dispatch(engine.OriginGUI, "helm.repo.update", struct{}{})
	if err != nil || len(infos.([]engine.HelmRepoInfo)) != 1 || h.repo.Requests("/index.yaml") != before+1 {
		t.Errorf("update with unused entries: %+v %v (%d downloads)", infos, err, h.repo.Requests("/index.yaml")-before)
	}
	for _, r := range h.list(t, engine.OriginGUI).Repositories[1:] {
		if !r.Unused || r.Error == "" || r.Charts != 0 {
			t.Errorf("unused entry %+v", r)
		}
	}
	if _, err := h.dispatch(engine.OriginGUI, "helm.repo.update", map[string]any{"names": []string{"with space"}}); err == nil ||
		!strings.Contains(err.Error(), "letters, digits") {
		t.Errorf("an update of an unused entry: %v", err)
	}

	if err := os.Remove(h.helmFile.Path); err != nil {
		t.Fatal(err)
	}
	if !testkit.WaitFor(ctx, func() bool { l = h.list(t, engine.OriginGUI); return l.Status == engine.HelmFileMissing }) {
		t.Fatalf("a removed file: %+v", l)
	}
	if len(l.Repositories) != 0 || !strings.Contains(l.Hint, "helm repo add") {
		t.Errorf("a removed file: %+v", l)
	}
	h.charts(t, 0)
}

// A repository behind a login is used with the credentials Helm's file holds; they never reach
// helm.repo.list's result, an error, a toast, the audit log or the log, for agents or the window,
// also when a download fails.
func TestHelmRepoCredentialsNeverShown(t *testing.T) {
	const user, pass, wrong = "chart-user-81", "pw-chart-82", "pw-wrong-83"
	h := newChartHarness(t, nil)
	h.repo.RequireBasicAuth(user, pass)
	withUser := strings.Replace(h.repo.URL, "https://", "https://"+user+":"+wrong+"@", 1)
	h.helmFile.Write(&repo.Entry{Name: "test", URL: h.repo.URL, Username: user, Password: pass},
		&repo.Entry{Name: "denied", URL: withUser + "/denied"})
	// A partial failure is the result's error and a warning toast, not the command's error.
	for _, origin := range []engine.Origin{engine.OriginExternalAgent, engine.OriginGUI} {
		res, err := h.dispatch(origin, "helm.repo.update", struct{}{})
		if err != nil {
			t.Fatal(err)
		}
		infos := res.([]engine.HelmRepoInfo)
		if len(infos) != 2 || infos[0].Error != "" || !strings.Contains(infos[1].Error, "401") {
			t.Errorf("%s: %+v", origin, infos)
		}
	}
	h.charts(t, 1)
	if h.repo.Authorized("/index.yaml") != 2 || h.repo.Authorized("/denied/index.yaml") != 2 {
		t.Errorf("downloads with credentials: %d, denied %d", h.repo.Authorized("/index.yaml"), h.repo.Authorized("/denied/index.yaml"))
	}
	l := h.list(t, engine.OriginExternalAgent)
	if len(l.Repositories) != 2 || !l.Repositories[0].HasCredentials || !l.Repositories[1].HasCredentials || l.Repositories[1].Error == "" {
		t.Errorf("list %+v", l)
	}
	res, err := h.dispatch(engine.OriginExternalAgent, "helm.chart.installTab", map[string]any{"ref": engine.HelmChartRef("test", "test", "web")})
	if err != nil {
		t.Fatalf("the chart behind the login: %v", err)
	}
	listJSON, _ := json.Marshal(l)
	resJSON, _ := json.Marshal(res)
	h.mu.Lock()
	evJSON, _ := json.Marshal(h.events)
	h.mu.Unlock()
	notes, _ := h.dispatch(engine.OriginExternalAgent, "notifications.list", struct{}{})
	notesJSON, _ := json.Marshal(notes)
	for what, text := range map[string]string{"list": string(listJSON), "install tab": string(resJSON), "events and toasts": string(evJSON),
		"notifications": string(notesJSON), "audit log": bufText(h.audit), "log": bufText(h.logs)} {
		for _, secret := range []string{pass, wrong, user} {
			if strings.Contains(text, secret) {
				t.Errorf("the %s holds %q:\n%s", what, secret, text)
			}
		}
	}
	if !strings.Contains(bufText(h.audit), "helm.repo.update") {
		t.Errorf("the agent's update is not in the audit log: %s", bufText(h.audit))
	}
}

// The repositories are added and removed with the helm CLI only (owner, D-29): no command adds or
// removes one, so nothing an agent, the CLI or the window sends writes Helm's file.
func TestNoCommandWritesHelmsFile(t *testing.T) {
	h := newChartHarness(t, nil)
	for _, id := range []string{"helm.repo.add", "helm.repo.remove"} {
		if _, ok := h.s.Registry().Get(id); ok {
			t.Errorf("%s is registered", id)
		}
		if _, err := h.dispatch(engine.OriginGUI, id, map[string]any{"name": "x", "url": h.repo.URL}); err == nil {
			t.Errorf("%s ran", id)
		}
	}
	if _, err := os.Stat(h.helmFile.Path); !os.IsNotExist(err) {
		t.Errorf("Helm's file was written: %v", err)
	}
}

// An index is downloaded again only while the Charts view or a Helm Install tab is open and its
// time-to-live has passed (architecture §5): never while neither is shown.
func TestHelmIndexRefreshedOnlyWhileShown(t *testing.T) {
	h := newChartHarness(t, nil, engine.WithIndexTTL(40*time.Millisecond, 10*time.Millisecond))
	h.addRepo(t)
	if _, err := h.dispatch(engine.OriginGUI, "ui.openView", map[string]any{"view": "pods"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if n := h.repo.Requests("/index.yaml"); n != 1 {
		t.Fatalf("%d downloads with the Charts view closed, want the update's 1", n)
	}
	if _, err := h.dispatch(engine.OriginGUI, "ui.openView", map[string]any{"view": kinds.HelmChartsKey}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool { return h.repo.Requests("/index.yaml") >= 3 }) {
		t.Fatalf("%d downloads with the Charts view open", h.repo.Requests("/index.yaml"))
	}
	if _, err := h.dispatch(engine.OriginGUI, "ui.openView", map[string]any{"view": "pods"}); err != nil {
		t.Fatal(err)
	}
	// A download under way as the view closed may still reach the server: counted once the
	// downloads stayed the same for longer than an index lives (40 ms), which polls that went on
	// would not allow.
	n, since := h.repo.Requests("/index.yaml"), time.Now()
	if !testkit.WaitFor(ctx, func() bool {
		if m := h.repo.Requests("/index.yaml"); m != n {
			n, since = m, time.Now()
		}
		return time.Since(since) >= 60*time.Millisecond
	}) {
		t.Fatalf("the downloads went on after the Charts view closed: %d", n)
	}
	time.Sleep(200 * time.Millisecond)
	if m := h.repo.Requests("/index.yaml"); m != n {
		t.Errorf("%d downloads after the Charts view closed", m-n)
	}

	// A repository new in Helm's file is not downloaded while nothing shows charts.
	other := testkit.NewHelmRepo(t, webChartV("0.9.0"))
	h.helmFile.Write(h.repo.Entry("test"), other.Entry("other"))
	var last engine.HelmReposList
	if !testkit.WaitFor(ctx, func() bool { last = h.list(t, engine.OriginGUI); return len(last.Repositories) == 2 }) {
		t.Fatalf("the new repository is not listed: Helm's file %s %q", last.Status, last.Error)
	}
	time.Sleep(100 * time.Millisecond)
	if n := other.Requests("/index.yaml"); n != 0 {
		t.Errorf("%d downloads of a new repository with the Charts view closed", n)
	}
}

// TestChartDrawerStartsNoEventsWatch: a chart is an object Cogpit makes itself, so no event
// involves it; its drawer has no Events section and starts no cluster-wide events watch.
func TestChartDrawerStartsNoEventsWatch(t *testing.T) {
	h := newChartHarness(t, nil)
	h.addRepo(t)
	h.charts(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, err := h.s.DetailWait(ctx, engine.HelmChartRef("test", "test", "web"))
	if err != nil {
		t.Fatal(err)
	}
	if !m.NoEvents || len(m.Events) != 0 {
		t.Errorf("chart drawer: no events %v, events %v", m.NoEvents, m.Events)
	}
	if evp, ok := h.s.Backend().Profile("events"); ok && h.s.Backend().Cached(evp) {
		t.Error("the chart's drawer started the events watch")
	}
}

// bufText is what a syncBuffer holds.
func bufText(b *syncBuffer) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}
