package commands_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"helm.sh/helm/v4/pkg/release/common"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// fakeInstaller is the cluster side of an install, as the Helm SDK would act on the source's
// release Secrets: a preview names a ConfigMap, an install stores revision 1 deployed with the
// values. It is the release actions' helmServer too, so that an uninstall finishes an install.
type fakeInstaller struct {
	*helmServer

	imu      sync.Mutex
	installs []helm.InstallRequest
	previews int
	offline  []bool // each preview's Offline
	err      error
}

var _ engine.HelmInstaller = (*fakeInstaller)(nil)

func newFakeInstaller(t testing.TB, src *testkit.MemorySource) *fakeInstaller {
	return &fakeInstaller{helmServer: &helmServer{fakeWriter: &fakeWriter{}, src: src, t: t}}
}

func (f *fakeInstaller) PreviewInstall(_ context.Context, req helm.InstallRequest) (helm.InstallPreview, error) {
	f.imu.Lock()
	f.previews++
	f.offline = append(f.offline, req.Offline)
	err := f.err
	f.imu.Unlock()
	if err != nil {
		return helm.InstallPreview{}, err
	}
	return helm.InstallPreview{Objects: []helm.RenderedObject{{APIVersion: "v1", Kind: "ConfigMap", Namespace: req.Namespace, Name: req.Name}}}, nil
}

func (f *fakeInstaller) InstallChart(_ context.Context, req helm.InstallRequest) (int, error) {
	f.imu.Lock()
	f.installs = append(f.installs, req)
	err := f.err
	f.imu.Unlock()
	if err != nil {
		return 0, err
	}
	f.mu.Lock()
	if f.revs == nil {
		f.revs = map[string]int{}
	}
	if f.drivers == nil {
		f.drivers = map[string]helm.Driver{}
	}
	f.revs[req.Namespace+"/"+req.Name] = 1
	driver := req.Driver
	if driver == "" {
		driver = helm.DriverSecret
	}
	f.drivers[req.Namespace+"/"+req.Name] = driver // the storage driver the request names (helmdriver_test.go)
	f.mu.Unlock()
	f.src.Set(gvrOf(driver), releaseRecordWith(f.t, driver, req.Namespace, req.Name, 1, common.StatusDeployed, req.Chart.Version, req.Values))
	return 1, nil
}

func (f *fakeInstaller) recordedInstalls() []helm.InstallRequest {
	f.imu.Lock()
	defer f.imu.Unlock()
	return slices.Clone(f.installs)
}

// installHarness is a chart harness with the repository added and the fake installer.
func installHarness(t *testing.T) (*chartHarness, *fakeInstaller) {
	t.Helper()
	src := testkit.NewMemorySource()
	fi := newFakeInstaller(t, src)
	h := newChartHarnessWith(t, engine.NewMemoryHelmJournal(), src, fi)
	h.addRepo(t)
	h.charts(t, 1)
	if _, err := h.s.Objects(context.Background(), helm.ReleaseKind, nil); err != nil {
		t.Fatal(err)
	}
	return h, fi
}

func openInstallTab(t *testing.T, h *chartHarness, origin engine.Origin, version string) commands.EditResult {
	t.Helper()
	res, err := h.dispatch(origin, "helm.chart.installTab", map[string]any{"ref": engine.HelmChartRef("test", "test", "web"), "version": version})
	if err != nil {
		t.Fatal(err)
	}
	return res.(commands.EditResult)
}

func (h *chartHarness) tabRevision(t *testing.T, id string) uint64 {
	t.Helper()
	res, err := h.dispatch(engine.OriginGUI, "editor.get", map[string]any{"id": id})
	if err != nil {
		t.Fatal(err)
	}
	return res.(engine.EditorState).Revision
}

func (h *chartHarness) waitRelease(t *testing.T, namespace, name string, rev int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ref := engine.HelmReleaseRef("test", namespace, name)
	if !testkit.WaitFor(ctx, func() bool {
		obj, err := h.s.Lookup(ref)
		return err == nil && helm.ReleaseRevision(obj) == rev
	}) {
		t.Fatalf("release %s/%s never at revision %d", namespace, name, rev)
	}
}

// Install sits in a chart's row menu and drawer and opens a "Helm Install: <chart>" tab with the
// chart's values.yaml from the archive it downloaded (checked against the index's digest), the
// repository's versions and a generated release name; it is focused, not opened twice, and another
// version reloads it. Agents read its values withheld.
func TestHelmInstallTab(t *testing.T) {
	h, _ := installHarness(t)
	if acts := h.s.Registry().Actions(kinds.HelmChartsKey); !slices.ContainsFunc(acts, func(c *engine.Command) bool { return c.ID == "helm.chart.installTab" }) {
		t.Error("Install is not in the chart's row menu")
	}
	tab := openInstallTab(t, h, engine.OriginGUI, "")
	hi := tab.Tab.HelmInstall
	if !tab.Created || tab.Tab.Type != engine.DockHelmInstall || tab.Tab.Title != "Helm Install: web" || hi == nil ||
		hi.Repository != "test" || hi.Chart != "web" || hi.Version != "0.2.0" || strings.Join(hi.Versions, ",") != "0.2.0,0.1.0" ||
		!hi.Verified || len(hi.Digest) != 64 || hi.Namespace != "default" || !strings.HasPrefix(hi.ReleaseName, "web-") || len(hi.ReleaseName) != 9 ||
		tab.Text != webChartV("0.2.0").Values {
		t.Fatalf("tab %+v %+v", tab, hi)
	}
	if again := openInstallTab(t, h, engine.OriginGUI, ""); again.Created || again.Tab.ID != tab.Tab.ID {
		t.Errorf("opened twice: %+v", again.Tab)
	}
	older := openInstallTab(t, h, engine.OriginGUI, "0.1.0")
	if older.Created || older.Tab.ID != tab.Tab.ID || older.Tab.HelmInstall.Version != "0.1.0" || older.Tab.HelmInstall.ReleaseName != hi.ReleaseName {
		t.Errorf("another version: %+v", older.Tab.HelmInstall)
	}
	if ui := h.s.UIState(); len(ui.Dock) != 1 || ui.Dock[0].HelmInstall.Version != "0.1.0" || ui.ActiveDock != tab.Tab.ID {
		t.Errorf("dock %+v", ui.Dock)
	}
	res, err := h.dispatch(engine.OriginExternalAgent, "editor.get", map[string]any{"id": tab.Tab.ID})
	if err != nil || res.(engine.EditorState).Text != model.Withheld {
		t.Errorf("an agent's editor.get: %+v %v", res, err)
	}
	if agent := openInstallTab(t, h, engine.OriginExternalAgent, ""); agent.Text != model.Withheld {
		t.Errorf("an agent's installTab: %q", agent.Text)
	}
}

// From the tab: the preview names what the chart renders; the install takes the tab's chart and
// the values at the text revision the user reviewed, records nothing once it finished, and the
// release appears; a name in use, a stale text revision and none at all are refused.
func TestHelmInstallFromTab(t *testing.T) {
	h, fi := installHarness(t)
	tab := openInstallTab(t, h, engine.OriginGUI, "")
	if _, err := h.dispatch(engine.OriginGUI, "editor.setText", map[string]any{"id": tab.Tab.ID, "yaml": "greeting: hi\npassword: s3cr3t-pw\n"}); err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"tab": tab.Tab.ID, "namespace": "apps", "name": "web"}
	res, err := h.dispatch(engine.OriginGUI, "helm.chart.preview", params)
	if err != nil {
		t.Fatal(err)
	}
	pre := res.(commands.InstallPreview)
	if pre.Chart != "web" || pre.Version != "0.2.0" || !pre.Verified || len(pre.Objects) != 1 || pre.Objects[0].Kind != "ConfigMap" ||
		pre.Release.Name != "web" || pre.Release.Kind != helm.ReleaseKind || pre.Offline {
		t.Errorf("preview %+v", pre)
	}
	// An agent's preview renders without the cluster: a chart's lookup cannot reach it.
	res, err = h.dispatch(engine.OriginExternalAgent, "helm.chart.preview", params)
	if err != nil || !res.(commands.InstallPreview).Offline {
		t.Errorf("an agent's preview: %+v %v", res, err)
	}
	fi.imu.Lock()
	if !slices.Equal(fi.offline, []bool{false, true}) {
		t.Errorf("previews offline %v", fi.offline)
	}
	fi.imu.Unlock()
	if _, err := h.dispatch(engine.OriginGUI, "helm.chart.install", params); err == nil || !strings.Contains(err.Error(), "textRevision") {
		t.Errorf("an install without the text revision: %v", err)
	}
	params["textRevision"] = h.tabRevision(t, tab.Tab.ID) - 1
	if _, err := h.dispatch(engine.OriginGUI, "helm.chart.install", params); !errors.Is(err, engine.ErrTargetsChanged) {
		t.Errorf("an install at a stale text revision: %v", err)
	}
	params["textRevision"] = h.tabRevision(t, tab.Tab.ID)
	res, err = h.dispatch(engine.OriginGUI, "helm.chart.install", params)
	if err != nil {
		t.Fatal(err)
	}
	if r := res.(commands.HelmResult); r.Action != "installed" || r.Revision != 1 || r.Ref.Name != "web" || r.Ref.Namespace != "apps" {
		t.Errorf("result %+v", r)
	}
	ins := fi.recordedInstalls()
	if len(ins) != 1 || ins[0].Chart.Version != "0.2.0" || !ins[0].Chart.Verified || ins[0].Values["password"] != "s3cr3t-pw" || ins[0].Namespace != "apps" || ins[0].CreateNamespace {
		t.Fatalf("installs %+v", ins)
	}
	h.waitRelease(t, "apps", "web", 1)
	if list, _ := h.journal.List(); len(list) != 0 {
		t.Errorf("journal after the install: %+v", list)
	}
	if len(h.confirmations()) != 0 {
		t.Errorf("the GUI's install asked: %+v", h.confirmations())
	}
	if _, err := h.dispatch(engine.OriginGUI, "helm.chart.install", params); err == nil || !strings.Contains(err.Error(), "exists in namespace apps") {
		t.Errorf("a second install of web: %v", err)
	}
	h.mu.Lock()
	for _, ev := range h.events {
		if strings.Contains(ev.Message, "s3cr3t") {
			t.Errorf("an event quotes a value: %q", ev.Message)
		}
	}
	h.mu.Unlock()
	for _, bad := range []map[string]any{
		{"tab": tab.Tab.ID, "namespace": "Apps", "name": "x"},
		{"tab": tab.Tab.ID, "namespace": "apps", "name": "X_1"},
		{"tab": tab.Tab.ID, "repository": "test", "namespace": "apps", "name": "x"},
		{"repository": "test", "chart": "web", "namespace": "apps", "name": "x", "values": ""},
	} {
		if _, err := h.dispatch(engine.OriginGUI, "helm.chart.install", bad); err == nil {
			t.Errorf("%v installed", bad)
		}
	}
}

// An agent's install with inline values waits for the user's acceptance, which names the release,
// its namespace and context, the chart, its version and the digest check, and shows the values
// (D-27); the audit log never holds them. Declined, nothing is installed.
func TestHelmInstallAgents(t *testing.T) {
	h, fi := installHarness(t)
	params := map[string]any{"repository": "test", "chart": "web", "version": "0.1.0", "values": "password: agent-s3cr3t\n", "namespace": "apps", "name": "api"}
	h.answer = false
	if _, err := h.dispatch(engine.OriginExternalAgent, "helm.chart.install", params); !errors.Is(err, engine.ErrDeclined) {
		t.Fatalf("declined: %v", err)
	}
	if len(fi.recordedInstalls()) != 0 {
		t.Fatal("a declined install ran")
	}
	asked := h.confirmations()
	if len(asked) != 1 {
		t.Fatalf("confirmations %+v", asked)
	}
	a := asked[0]
	if a.Danger != engine.DangerMutating || a.Context != "test" || len(a.Objects) != 1 || a.Objects[0].Kind != helm.ReleaseKind || a.Objects[0].Name != "api" ||
		!slices.Equal(a.Namespaces, []string{"apps"}) || !strings.Contains(a.Detail, "Chart: test/web 0.1.0.") ||
		!strings.Contains(a.Detail, "Archive: checked against the index's digest.") || a.Review != "password: agent-s3cr3t\n" {
		t.Errorf("confirmation %+v (message %q)", a, a.Message())
	}
	h.answer = true
	if _, err := h.dispatch(engine.OriginExternalAgent, "helm.chart.install", params); err != nil {
		t.Fatal(err)
	}
	h.waitRelease(t, "apps", "api", 1)
	if ins := fi.recordedInstalls(); len(ins) != 1 || ins[0].Chart.Version != "0.1.0" || ins[0].Values["password"] != "agent-s3cr3t" {
		t.Errorf("installs %+v", ins)
	}
	// The agent's own tab: its text audited by size only.
	tab := openInstallTab(t, h, engine.OriginExternalAgent, "")
	if _, err := h.dispatch(engine.OriginExternalAgent, "editor.setText", map[string]any{"id": tab.Tab.ID, "yaml": "password: tab-s3cr3t\n"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(h.audit.Lines())
	for _, leak := range []string{"agent-s3cr3t", "tab-s3cr3t"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("%q in the audit log", leak)
		}
	}
	if !slices.ContainsFunc(h.audit.Lines(), func(e engine.AuditEntry) bool { return e.Command == "helm.chart.install" && e.Phase == "requested" }) {
		t.Errorf("the install is not audited: %s", raw)
	}

	// Agent writes allowed for the context: an agent's install still waits for the user, whatever
	// the chart holds (A2, owner 2026-10-14 evening); declined, nothing is installed.
	h.s.SetAgentWrites("test", true)
	h.answer = false
	n, installs := len(h.confirmations()), len(fi.recordedInstalls())
	allowed := map[string]any{"repository": "test", "chart": "web", "version": "0.1.0", "values": "", "namespace": "apps", "name": "api2"}
	if _, err := h.dispatch(engine.OriginExternalAgent, "helm.chart.install", allowed); !errors.Is(err, engine.ErrDeclined) {
		t.Fatalf("an agent's install with agent writes allowed: %v, want ErrDeclined", err)
	}
	if asked := h.confirmations(); len(asked) != n+1 || !asked[n].AgentsAsk || asked[n].Command != "helm.chart.install" {
		t.Errorf("an agent's install with agent writes allowed: asked %+v", asked[n:])
	}
	if len(fi.recordedInstalls()) != installs {
		t.Error("a declined install ran")
	}
}

// An install the Helm SDK refused before it stored the release leaves no journal entry and says
// the release was not changed; one that failed after leaves the entry, as failed, which shows the
// release's state: its error points to the Helm Releases view, never to running the install again
// (nothing is offered after an install, A17). Neither error quotes a value.
func TestHelmInstallFailures(t *testing.T) {
	h, fi := installHarness(t)
	params := map[string]any{"repository": "test", "chart": "web", "version": "0.2.0", "values": "password: fail-s3cr3t\n", "namespace": "apps", "name": "web"}
	fi.err = fmt.Errorf("helm install apps/web: template: cannot use fail-s3cr3t; %w", helm.ErrUnchanged)
	_, err := h.dispatch(engine.OriginGUI, "helm.chart.install", params)
	if !errors.Is(err, helm.ErrUnchanged) || strings.Contains(err.Error(), "fail-s3cr3t") {
		t.Errorf("a refused install: %v", err)
	}
	if list, _ := h.journal.List(); len(list) != 0 {
		t.Errorf("journal after a refused install: %+v", list)
	}
	fi.err = errors.New("helm install apps/web: timed out waiting for the hook, fail-s3cr3t")
	_, err = h.dispatch(engine.OriginGUI, "helm.chart.install", params)
	if err == nil || strings.Contains(err.Error(), "fail-s3cr3t") || !strings.Contains(err.Error(), "may be left pending") ||
		!strings.Contains(err.Error(), "Helm Releases view") || strings.Contains(err.Error(), "run it again") {
		t.Errorf("a failed install: %v", err)
	}
	list, _ := h.journal.List()
	if len(list) != 1 || list[0].Action != engine.HelmInstall || list[0].State != engine.HelmOpFailed || list[0].Chart != "test/web" ||
		list[0].ChartVersion != "0.2.0" || strings.Contains(list[0].Error, "fail-s3cr3t") {
		t.Errorf("journal after a failed install: %+v", list)
	}
}

// An install a crash interrupted is listed on the next connect with its release's state when the
// release is left pending-install; one whose release is deployed, or does not exist, is dropped
// with a note. Nothing is offered to finish it (A17): the connect's notice names it, and the user
// cleans up in the Helm Releases view, whose uninstall replaces the entry and removes it when done.
func TestHelmInstallCrashJournal(t *testing.T) {
	journal := engine.NewMemoryHelmJournal()
	started := releaseT0.Add(48 * time.Hour)
	for _, name := range []string{"web", "api", "db"} {
		if err := journal.Put(engine.PendingHelmOp{Context: "test", Namespace: "apps", Release: name, Action: engine.HelmInstall,
			Chart: "test/" + name, ChartVersion: "0.2.0", Started: started, StartedBy: engine.OriginGUI, State: engine.HelmOpRunning}); err != nil {
			t.Fatal(err)
		}
	}
	src := testkit.NewMemorySource()
	fi := newFakeInstaller(t, src)
	fi.put("apps", "web", 1, common.StatusPendingInstall) // what the crash left
	fi.put("apps", "api", 1, common.StatusDeployed)       // finished before the crash was recorded
	h := newChartHarnessWith(t, journal, src, fi)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var pending []engine.PendingHelmOpInfo
	if !testkit.WaitFor(ctx, func() bool {
		res, err := h.dispatch(engine.OriginExternalAgent, "helm.pendingOperations", struct{}{})
		if err != nil {
			return false
		}
		b, _ := json.Marshal(res)
		pending = nil
		_ = json.Unmarshal(b, &pending)
		list, _ := journal.List()
		return len(list) == 1 && len(pending) == 1 && pending[0].ReleaseStatus == "pending-install"
	}) {
		list, _ := journal.List()
		t.Fatalf("pending %+v, journal %+v", pending, list)
	}
	if p := pending[0]; p.Release != "web" || p.Action != engine.HelmInstall || p.Status != engine.HelmOpStatusInterrupted {
		t.Errorf("offer %+v", p)
	}
	var notes []string
	notice := false
	h.mu.Lock()
	for _, ev := range h.events {
		if ev.Type == engine.EventToast {
			notes = append(notes, ev.Message)
		}
		notice = notice || (ev.Type == engine.EventHelm && ev.Message == "The install of release web in context test did not finish")
	}
	h.mu.Unlock()
	if !notice {
		t.Error("the connect's notice does not name the interrupted install")
	}
	if cmd, ok := h.s.Registry().Get("helm.pendingOperations"); !ok || !strings.Contains(cmd.Description, "Cogpit offers nothing to finish it") ||
		strings.Contains(cmd.Description, "an install by helm_release_uninstall") {
		t.Errorf("helm_pendingOperations tells agents to finish an install: %q", cmd.Description)
	}
	if n := strings.Join(notes, "\n"); !strings.Contains(n, "install of release api (context test): the cluster shows it finished") ||
		!strings.Contains(n, "install of release db (context test): the release does not exist") {
		t.Errorf("notes %q", n)
	}
	if _, err := h.dispatch(engine.OriginGUI, "helm.release.uninstall", map[string]any{"ref": engine.HelmReleaseRef("test", "apps", "web")}); err != nil {
		t.Fatal(err)
	}
	if list, _ := journal.List(); len(list) != 0 {
		t.Errorf("journal after finishing: %+v", list)
	}
}

// An agent's install that fails in the SDK gets the failure without its text: a chart's templates
// can quote what lookup read (values reach lookup through tpl), a Secret's data included. The user
// reads the error in the window's toast (review 2026-10-13).
func TestHelmInstallErrorStaysInTheWindow(t *testing.T) {
	h, fi := installHarness(t)
	h.answer = true
	params := map[string]any{"repository": "test", "chart": "web", "version": "0.2.0", "values": "a: b\n", "namespace": "apps", "name": "web"}
	fi.err = errors.New("helm install apps/web: template: web/templates/cm.yaml: name LOOKED-UP-SECRET-DATA is not valid")
	_, err := h.dispatch(engine.OriginExternalAgent, "helm.chart.install", params)
	if err == nil || strings.Contains(err.Error(), "LOOKED-UP-SECRET-DATA") || !strings.Contains(err.Error(), "Cogpit's window") {
		t.Errorf("the agent's error: %v", err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	shown := false
	for _, ev := range h.events {
		shown = shown || (ev.Type == engine.EventToast && strings.Contains(ev.Message, "LOOKED-UP-SECRET-DATA"))
	}
	if !shown {
		t.Error("the window was not shown the error")
	}
	h.mu.Unlock()
	res, err := h.dispatch(engine.OriginExternalAgent, "helm.pendingOperations", struct{}{})
	h.mu.Lock()
	list, _ := res.([]engine.PendingHelmOpInfo)
	if err != nil || len(list) != 1 || list[0].Error == "" || strings.Contains(list[0].Error, "LOOKED-UP-SECRET-DATA") {
		t.Errorf("the agent's pending operations: %+v %v", list, err)
	}
}
