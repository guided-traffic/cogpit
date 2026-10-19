package commands_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"helm.sh/helm/v4/pkg/release/common"
	repo "helm.sh/helm/v4/pkg/repo/v1"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// Upgrading a release to a chart version from the user's repositories (G11): the Helm Upgrade
// tab offers a repository's versions of the release's chart, the upgrade downloads the chosen
// one (checked against the index's digest) and installs it, crash-safe through the journal.

// repoUpgradeHarness is a session with the release web at revision 1 (chart web 0.1.0) in apps,
// the repository test (web 0.1.0 and 0.2.0, served over TLS by httptest) added, the fake installer
// as the Helm writer, and a confirmer that records, answers and can act while it is asked.
type repoUpgradeHarness struct {
	s       *engine.Session
	src     *testkit.MemorySource
	server  *fakeInstaller
	repo    *testkit.HelmRepo
	journal *engine.MemoryHelmJournal
	audit   *syncBuffer
	// helmFile is Helm's repositories file of the test; entries what addRepo wrote into it.
	helmFile *testkit.HelmRepoFile
	entries  []*repo.Entry

	mu     sync.Mutex
	asked  []engine.ConfirmRequest
	answer bool
	onAsk  func()
	events []engine.Event
}

// newRepoUpgradeHarness builds the harness; seed puts the release Secrets (nil: web at revision
// 1, deployed) and more adds repositories before test, by name.
func newRepoUpgradeHarness(t *testing.T, journal *engine.MemoryHelmJournal, seed func(*helmServer), more ...struct {
	name string
	repo *testkit.HelmRepo
}) *repoUpgradeHarness {
	t.Helper()
	src := testkit.NewMemorySource()
	h := &repoUpgradeHarness{src: src, server: newFakeInstaller(t, src), journal: journal, audit: &syncBuffer{}, answer: true}
	if seed == nil {
		seed = func(s *helmServer) { s.put("apps", "web", 1, common.StatusDeployed) }
	}
	seed(h.server.helmServer)
	h.repo = testkit.NewHelmRepo(t, webChartV("0.1.0"), webChartV("0.2.0"))
	// httptest's TLS servers share one certificate: the first repository's client trusts them all.
	h.helmFile = testkit.NewHelmRepoFile(t)
	repos := engine.NewHelmRepos(h.helmFile.Path, t.TempDir(), engine.WithChartFetcher(&helm.Fetcher{Client: h.repo.Client()}))
	h.s = engine.New(engine.Options{
		Connector: &chartConnector{src: src, actions: h.server}, SnapshotInterval: 10 * time.Millisecond,
		Audit: h.audit, Helm: engine.NewHelmOps(journal), HelmRepos: repos,
		Confirmer: engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
			h.mu.Lock()
			h.asked = append(h.asked, req)
			ans, on := h.answer, h.onAsk
			h.mu.Unlock()
			if on != nil {
				on()
			}
			return ans, nil
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
	for _, m := range more {
		h.addRepo(t, m.name, m.repo.URL)
	}
	h.addRepo(t, "test", h.repo.URL)
	if _, err := h.s.Objects(context.Background(), helm.ReleaseKind, nil); err != nil {
		t.Fatal(err)
	}
	return h
}

// addRepo adds a repository to Helm's repositories file, as helm repo add does, and downloads its
// index.
func (h *repoUpgradeHarness) addRepo(t *testing.T, name, url string) {
	t.Helper()
	h.entries = append(h.entries, &repo.Entry{Name: name, URL: url})
	h.helmFile.Write(h.entries...)
	if _, err := h.dispatch(engine.OriginGUI, "helm.repo.update", map[string]any{"names": []string{name}}); err != nil {
		t.Fatal(err)
	}
}

func (h *repoUpgradeHarness) dispatch(origin engine.Origin, id string, params any) (any, error) {
	raw, _ := json.Marshal(params)
	return h.s.Dispatch(context.Background(), engine.Invocation{ID: id, Params: raw, Origin: origin})
}

func (h *repoUpgradeHarness) openTab(t *testing.T, origin engine.Origin, params map[string]any) commands.EditResult {
	t.Helper()
	params["ref"] = webRef
	res, err := h.dispatch(origin, helm.UpgradeTabCommand, params)
	if err != nil {
		t.Fatal(err)
	}
	return res.(commands.EditResult)
}

// choice is the tab's choice as the dock holds it (what ui.state shows).
func (h *repoUpgradeHarness) choice(t *testing.T, id string) engine.HelmUpgradeTab {
	t.Helper()
	tab, ok := h.s.UIState().DockTab(id)
	if !ok || tab.Helm == nil {
		t.Fatalf("no Helm Upgrade tab %s", id)
	}
	return *tab.Helm
}

func (h *repoUpgradeHarness) textRevision(t *testing.T, id string) uint64 {
	t.Helper()
	res, err := h.dispatch(engine.OriginGUI, "editor.get", map[string]any{"id": id})
	if err != nil {
		t.Fatal(err)
	}
	return res.(engine.EditorState).Revision
}

func (h *repoUpgradeHarness) waitRelease(t *testing.T, rev int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		obj, err := h.s.Lookup(webRef)
		return err == nil && helm.ReleaseRevision(obj) == rev
	}) {
		t.Fatalf("web never at revision %d", rev)
	}
}

func (h *repoUpgradeHarness) upgrades() []engine.HelmUpgradeRequest {
	h.server.mu.Lock()
	defer h.server.mu.Unlock()
	return slices.Clone(h.server.upgrades)
}

func (h *repoUpgradeHarness) confirmations() []engine.ConfirmRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.asked)
}

// The tab on a release whose chart a repository lists offers that repository's versions, newest
// first, the release's own among them, and starts with the newest, which is newer than the
// release's; upgradeTab changes the choice on the open tab and refuses what no repository lists.
func TestHelmUpgradeTabOffersTheRepositorysVersions(t *testing.T) {
	h := newRepoUpgradeHarness(t, engine.NewMemoryHelmJournal(), nil)
	tab := h.openTab(t, engine.OriginGUI, map[string]any{})
	c := tab.Tab.Helm
	if !tab.Created || c == nil || c.Chart != "web" || c.ReleaseVersion != "0.1.0" || c.Repository != "test" ||
		strings.Join(c.Repositories, ",") != "test" || strings.Join(c.Versions, ",") != "0.2.0,0.1.0" || c.Version != "0.2.0" {
		t.Fatalf("tab %+v %+v", tab.Tab, c)
	}
	if got := h.choice(t, tab.Tab.ID); got.Version != "0.2.0" || got.Repository != "test" {
		t.Errorf("the dock's tab %+v", got)
	}
	again := h.openTab(t, engine.OriginExternalAgent, map[string]any{"version": "0.1.0"})
	if again.Created || again.Tab.ID != tab.Tab.ID || h.choice(t, tab.Tab.ID).Version != "0.1.0" {
		t.Errorf("the choice of 0.1.0: %+v, dock %+v", again.Tab, h.choice(t, tab.Tab.ID))
	}
	if again := h.openTab(t, engine.OriginGUI, map[string]any{}); again.Created || h.choice(t, tab.Tab.ID).Version != "0.1.0" {
		t.Errorf("focusing the tab changed its choice: %+v", h.choice(t, tab.Tab.ID))
	}
	for _, params := range []map[string]any{
		{"ref": webRef, "version": "9.9.9"},
		{"ref": webRef, "repository": "nope"},
	} {
		if _, err := h.dispatch(engine.OriginGUI, helm.UpgradeTabCommand, params); err == nil {
			t.Errorf("%v accepted", params)
		}
	}
	if got := h.choice(t, tab.Tab.ID); got.Version != "0.1.0" {
		t.Errorf("a refused choice changed the tab: %+v", got)
	}
}

// Several repositories list the chart: the tab starts with the first whose index lists the
// release's own version, and another repository's versions follow its choice, the release's own
// version kept among them.
func TestHelmUpgradeTabRepositoryChoice(t *testing.T) {
	other := testkit.NewHelmRepo(t, webChartV("0.3.0"), webChartV("0.4.0-rc.1"))
	h := newRepoUpgradeHarness(t, engine.NewMemoryHelmJournal(), nil, struct {
		name string
		repo *testkit.HelmRepo
	}{"aaa", other})
	tab := h.openTab(t, engine.OriginGUI, map[string]any{})
	if c := tab.Tab.Helm; c.Repository != "test" || strings.Join(c.Repositories, ",") != "aaa,test" || c.Version != "0.2.0" {
		t.Fatalf("choice %+v", c)
	}
	h.openTab(t, engine.OriginGUI, map[string]any{"repository": "aaa"})
	if c := h.choice(t, tab.Tab.ID); c.Repository != "aaa" || strings.Join(c.Versions, ",") != "0.3.0,0.1.0,0.4.0-rc.1" || c.Version != "0.3.0" {
		t.Errorf("aaa's choice %+v", c)
	}
}

// From the GUI's tab: the chosen version is downloaded from the repository (once, checked against
// the index's digest) and installed with the tab's values; the release is at the new chart
// version, the journal empty, and the tab rebased on the new revision with that version as the
// release's own. Saving again keeps the chart: nothing more is downloaded.
func TestHelmUpgradeFromARepository(t *testing.T) {
	h := newRepoUpgradeHarness(t, engine.NewMemoryHelmJournal(), nil)
	tab := h.openTab(t, engine.OriginGUI, map[string]any{})
	if _, err := h.dispatch(engine.OriginGUI, "editor.setText", map[string]any{"id": tab.Tab.ID, "yaml": "greeting: two\n"}); err != nil {
		t.Fatal(err)
	}
	res, err := h.dispatch(engine.OriginGUI, "helm.release.upgrade", map[string]any{"ref": webRef, "tab": tab.Tab.ID,
		"textRevision": h.textRevision(t, tab.Tab.ID), "version": "0.2.0", "repository": "test"})
	if err != nil {
		t.Fatal(err)
	}
	if r := res.(commands.HelmResult); r.Revision != 2 || r.Action != "upgraded" {
		t.Errorf("result %+v", r)
	}
	up := h.upgrades()
	if len(up) != 1 || up[0].Chart == nil || up[0].Chart.Version != "0.2.0" || up[0].Chart.Name != "web" || !up[0].Chart.Verified ||
		up[0].ChartVersion != "0.2.0" || up[0].FromRevision != 1 || up[0].Values["greeting"] != "two" {
		t.Fatalf("upgrade %+v", up)
	}
	if n := h.repo.Requests("/web-0.2.0.tgz"); n != 1 {
		t.Errorf("the archive was downloaded %d times", n)
	}
	h.waitRelease(t, 2)
	obj, _ := h.s.Lookup(webRef)
	if _, v := helm.ReleaseChart(obj); v != "0.2.0" {
		t.Errorf("the release's chart is at %s", v)
	}
	if list, _ := h.journal.List(); len(list) != 0 {
		t.Errorf("journal %+v", list)
	}
	if c := h.choice(t, tab.Tab.ID); c.ReleaseVersion != "0.2.0" || c.Version != "0.2.0" {
		t.Errorf("the tab after the upgrade %+v", c)
	}
	res, _ = h.dispatch(engine.OriginGUI, "editor.get", map[string]any{"id": tab.Tab.ID})
	if st := res.(engine.EditorState); st.BaseRevision != 2 || st.Stale {
		t.Errorf("the tab is based on %d (stale %v)", st.BaseRevision, st.Stale)
	}
	var toasts []string
	h.mu.Lock()
	for _, ev := range h.events {
		if ev.Type == engine.EventToast {
			toasts = append(toasts, ev.Message)
		}
	}
	h.mu.Unlock()
	if !slices.Contains(toasts, "Release web upgraded to revision 2, chart web 0.2.0 from test") {
		t.Errorf("toasts %q", toasts)
	}

	if _, err := h.dispatch(engine.OriginGUI, "helm.release.upgrade", map[string]any{"ref": webRef, "tab": tab.Tab.ID,
		"textRevision": h.textRevision(t, tab.Tab.ID)}); err != nil {
		t.Fatal(err)
	}
	if up := h.upgrades(); len(up) != 2 || up[1].Chart != nil || up[1].ChartVersion != "0.2.0" || h.repo.Requests("/web-0.2.0.tgz") != 1 {
		t.Errorf("the second upgrade %+v, downloads %d", up, h.repo.Requests("/web-0.2.0.tgz"))
	}
}

// An agent's upgrade to a repository's version waits for the user, whose confirmation names the
// chart version from and to, the repository and the digest check, and shows inline values; a
// declined one downloads nothing. From a tab without a version the agent takes the tab's choice,
// and a choice changed while the confirmation is open refuses the upgrade (what the user read is
// what runs).
func TestHelmUpgradeFromARepositoryAgents(t *testing.T) {
	h := newRepoUpgradeHarness(t, engine.NewMemoryHelmJournal(), nil)
	params := map[string]any{"ref": webRef, "values": "greeting: agent-s3cr3t\n", "revision": 1, "version": "0.2.0"}
	h.mu.Lock()
	h.answer = false
	h.mu.Unlock()
	if _, err := h.dispatch(engine.OriginExternalAgent, "helm.release.upgrade", params); !errors.Is(err, engine.ErrDeclined) {
		t.Fatalf("declined: %v", err)
	}
	if n := h.repo.Requests("/web-0.2.0.tgz"); n != 0 || len(h.upgrades()) != 0 {
		t.Errorf("a declined upgrade downloaded %d archives, ran %d upgrades", n, len(h.upgrades()))
	}
	asked := h.confirmations()
	if len(asked) != 1 || asked[0].Danger != engine.DangerMutating || asked[0].Context != "test" || len(asked[0].Objects) != 1 ||
		asked[0].Objects[0].Name != "web" || asked[0].Namespaces[0] != "apps" || asked[0].Review != "greeting: agent-s3cr3t\n" ||
		!strings.Contains(asked[0].Detail, "Chart: web 0.1.0 → 0.2.0. Repository: test.") ||
		!strings.Contains(asked[0].Detail, "Archive: checked against the index's digest.") {
		t.Fatalf("confirmation %+v", asked)
	}
	if b, _ := json.Marshal(asked[0]); strings.Contains(string(b), "s3cr3t") {
		t.Errorf("the request serialises the values: %s", b)
	}

	// From a tab, its choice; an agent that changes it while the user is asked has it refused.
	tab := h.openTab(t, engine.OriginExternalAgent, map[string]any{})
	h.mu.Lock()
	h.answer = true
	h.onAsk = func() {
		if _, err := h.dispatch(engine.OriginExternalAgent, helm.UpgradeTabCommand, map[string]any{"ref": webRef, "version": "0.1.0"}); err != nil {
			t.Error(err)
		}
	}
	h.mu.Unlock()
	_, err := h.dispatch(engine.OriginExternalAgent, "helm.release.upgrade", map[string]any{"ref": webRef, "tab": tab.Tab.ID, "textRevision": tab.Revision})
	if !errors.Is(err, engine.ErrTargetsChanged) {
		t.Errorf("a choice changed during the confirmation: %v", err)
	}
	if asked := h.confirmations(); !strings.Contains(asked[len(asked)-1].Detail, "Chart: web 0.1.0 → 0.2.0. Repository: test.") {
		t.Errorf("the tab's choice was not named: %q", asked[len(asked)-1].Detail)
	}
	if len(h.upgrades()) != 0 || h.repo.Requests("/web-0.2.0.tgz") != 0 {
		t.Fatalf("the refused upgrade ran: %+v", h.upgrades())
	}
	h.mu.Lock()
	h.onAsk = nil
	h.mu.Unlock()
	h.openTab(t, engine.OriginExternalAgent, map[string]any{"version": "0.2.0"})
	if _, err := h.dispatch(engine.OriginExternalAgent, "helm.release.upgrade", map[string]any{"ref": webRef, "tab": tab.Tab.ID, "textRevision": tab.Revision}); err != nil {
		t.Fatal(err)
	}
	if up := h.upgrades(); len(up) != 1 || up[0].Chart == nil || up[0].Chart.Version != "0.2.0" {
		t.Errorf("upgrades %+v", up)
	}
	for _, e := range h.audit.Lines() {
		if b, _ := json.Marshal(e); strings.Contains(string(b), "s3cr3t") {
			t.Errorf("audit line with a value: %s", b)
		}
	}
}

// A version no repository lists, an unknown repository, an archive that does not match its
// index's digest, and a version two repositories list without naming one are refused before
// anything is recorded or changed.
func TestHelmUpgradeRepositoryRefusals(t *testing.T) {
	other := testkit.NewHelmRepo(t, webChartV("0.2.0"))
	journal := engine.NewMemoryHelmJournal()
	h := newRepoUpgradeHarness(t, journal, nil, struct {
		name string
		repo *testkit.HelmRepo
	}{"mirror", other})
	base := func(extra map[string]any) map[string]any {
		p := map[string]any{"ref": webRef, "values": "greeting: x\n", "revision": 1}
		for k, v := range extra {
			p[k] = v
		}
		return p
	}
	for _, c := range []struct {
		params map[string]any
		want   string
	}{
		{base(map[string]any{"version": "9.9.9"}), "no chart repository of Helm's repositories file lists this chart version"},
		{base(map[string]any{"repository": "nope"}), "no Helm repository named nope"},
		{base(map[string]any{"version": "0.2.0"}), "give repository"},
	} {
		if _, err := h.dispatch(engine.OriginGUI, "helm.release.upgrade", c.params); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v", c.params, err)
		}
	}
	h.repo.Serve("/web-0.2.0.tgz", h.repo.Archive("web-0.1.0.tgz"))
	_, err := h.dispatch(engine.OriginGUI, "helm.release.upgrade", base(map[string]any{"version": "0.2.0", "repository": "test"}))
	if !errors.Is(err, helm.ErrDigestMismatch) || !strings.Contains(err.Error(), "nothing was done") {
		t.Errorf("an archive that does not match: %v", err)
	}
	if len(h.upgrades()) != 0 {
		t.Errorf("refused upgrades ran: %+v", h.upgrades())
	}
	if list, _ := journal.List(); len(list) != 0 {
		t.Errorf("journal %+v", list)
	}
	// The mirror's archive matches its own index.
	if _, err := h.dispatch(engine.OriginGUI, "helm.release.upgrade", base(map[string]any{"version": "0.2.0", "repository": "mirror"})); err != nil {
		t.Errorf("from the mirror: %v", err)
	}
}

// D-11 for an upgrade to a repository's chart: the journal records it before the SDK runs, with
// the chart as <repository>/<chart> and the version it upgrades to; a failure after the SDK made
// a revision keeps it as failed; an entry the next connect finds with the release left
// pending-upgrade is offered and finished by a rollback to the revision it started from.
func TestHelmUpgradeRepositoryCrashJournal(t *testing.T) {
	journal := engine.NewMemoryHelmJournal()
	h := newRepoUpgradeHarness(t, journal, nil)
	h.server.mu.Lock()
	block, started := make(chan struct{}), make(chan struct{})
	h.server.block, h.server.started = block, started
	h.server.mu.Unlock()
	done := make(chan error, 1)
	go func() {
		_, err := h.dispatch(engine.OriginGUI, "helm.release.upgrade", map[string]any{"ref": webRef, "values": "greeting: x\n", "revision": 1, "version": "0.2.0"})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the upgrade never started")
	}
	list, _ := journal.List()
	if len(list) != 1 || list[0].Action != engine.HelmUpgrade || list[0].State != engine.HelmOpRunning || list[0].Chart != "test/web" ||
		list[0].ChartVersion != "0.2.0" || list[0].FromRevision != 1 {
		t.Errorf("journal while the upgrade runs: %+v", list)
	}
	h.server.mu.Lock()
	h.server.block, h.server.started = nil, nil
	h.server.mu.Unlock()
	close(block)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if list, _ := journal.List(); len(list) != 0 {
		t.Errorf("journal after the upgrade: %+v", list)
	}

	h.waitRelease(t, 2)
	h.server.mu.Lock()
	h.server.failErr = errors.New("cannot patch \"web\": the server rejected it")
	h.server.mu.Unlock()
	if _, err := h.dispatch(engine.OriginGUI, "helm.release.upgrade", map[string]any{"ref": webRef, "values": "greeting: y\n", "revision": 2, "version": "0.1.0", "repository": "test"}); err == nil ||
		!strings.Contains(err.Error(), "may be left pending") {
		t.Errorf("a failed upgrade: %v", err)
	}
	if list, _ := journal.List(); len(list) != 1 || list[0].State != engine.HelmOpFailed || list[0].Chart != "test/web" || list[0].ChartVersion != "0.1.0" ||
		list[0].FromRevision != 2 {
		t.Errorf("journal after a failure: %+v", list)
	}

	// The next start: the release left pending-upgrade by the interrupted upgrade to test/web 0.2.0.
	next := engine.NewMemoryHelmJournal()
	if err := next.Put(engine.PendingHelmOp{Context: "test", Namespace: "apps", Release: "web", Action: engine.HelmUpgrade, FromRevision: 1,
		Chart: "test/web", ChartVersion: "0.2.0", Started: releaseT0, StartedBy: engine.OriginGUI, State: engine.HelmOpRunning}); err != nil {
		t.Fatal(err)
	}
	h2 := newRepoUpgradeHarness(t, next, func(s *helmServer) {
		s.put("apps", "web", 1, common.StatusSuperseded)
		s.put("apps", "web", 2, common.StatusPendingUpgrade)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		h2.mu.Lock()
		defer h2.mu.Unlock()
		for _, ev := range h2.events {
			if ev.Type == engine.EventHelm && strings.Contains(ev.Message, "The upgrade of release web in context test did not finish") {
				return true
			}
		}
		return false
	}) {
		t.Fatal("the interrupted upgrade was not offered")
	}
	if _, err := h2.dispatch(engine.OriginGUI, "helm.release.upgrade", map[string]any{"ref": webRef, "values": "", "revision": 2, "version": "0.2.0"}); err == nil {
		t.Error("an upgrade of a release left pending-upgrade ran")
	}
	if _, err := h2.dispatch(engine.OriginGUI, "helm.release.rollback", map[string]any{"ref": webRef, "revision": 1}); err != nil {
		t.Fatal(err)
	}
	h2.waitRelease(t, 3)
	if list, _ := next.List(); len(list) != 0 {
		t.Errorf("journal after finishing: %+v", list)
	}
}
