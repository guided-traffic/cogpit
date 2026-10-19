package commands_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/release/common"
	rspb "helm.sh/helm/v4/pkg/release/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

var releaseT0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// releaseRecord encodes one revision the way Helm's driver stores it: a Secret, or for
// helm.DriverConfigMap a ConfigMap (releaseRecordWith).
func releaseRecord(t testing.TB, driver helm.Driver, ns, name string, rev int, status common.Status) *unstructured.Unstructured {
	return releaseRecordWith(t, driver, ns, name, rev, status, fmt.Sprintf("0.%d.0", rev), map[string]any{"adminPassword": "hunter2", "replicas": rev})
}

// releaseSecretWith encodes one revision the way Helm's Secrets driver stores it, with a chart
// version and user-supplied values.
func releaseSecretWith(t testing.TB, ns, name string, rev int, status common.Status, version string, config map[string]any) *unstructured.Unstructured {
	t.Helper()
	rls := &rspb.Release{
		Name: name, Namespace: ns, Version: rev,
		Info: &rspb.Info{Status: status, FirstDeployed: releaseT0, LastDeployed: releaseT0.Add(time.Duration(rev) * time.Hour),
			Description: fmt.Sprintf("Revision %d", rev), Notes: "Thank you for installing " + name + ".\nPassword: hunter2"},
		Chart: &chart.Chart{
			Metadata: &chart.Metadata{Name: name, Version: version, AppVersion: "1.0"},
			Values:   map[string]any{"replicas": 1, "image": "web:1"},
		},
		Config: config,
		Manifest: "---\n# Source: " + name + "/templates/secret.yaml\napiVersion: v1\nkind: Secret\nmetadata:\n  name: " + name +
			"\nstringData:\n  password: hunter2\n---\n# Source: " + name + "/templates/cm.yaml\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: " +
			name + "\ndata:\n  mode: fast\n",
	}
	raw, err := json.Marshal(rls)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	payload := base64.StdEncoding.EncodeToString(buf.Bytes())
	secret := testkit.Object("v1", "Secret", ns, fmt.Sprintf("sh.helm.release.v1.%s.v%d", name, rev), map[string]any{
		"type": "helm.sh/release.v1",
		"data": map[string]any{"release": base64.StdEncoding.EncodeToString([]byte(payload))},
	})
	secret.SetLabels(map[string]string{"owner": "helm", "name": name, "version": fmt.Sprint(rev), "status": string(status)})
	return secret
}

// releaseRecordWith is releaseSecretWith for a storage driver: for helm.DriverConfigMap the
// ConfigMap Helm's ConfigMaps driver writes, data.release Helm's own encoding as text.
func releaseRecordWith(t testing.TB, driver helm.Driver, ns, name string, rev int, status common.Status, version string, config map[string]any) *unstructured.Unstructured {
	t.Helper()
	secret := releaseSecretWith(t, ns, name, rev, status, version, config)
	if driver != helm.DriverConfigMap {
		return secret
	}
	data, _, _ := unstructured.NestedString(secret.Object, "data", "release")
	payload, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		t.Fatal(err)
	}
	cm := secret.DeepCopy()
	cm.SetKind("ConfigMap")
	delete(cm.Object, "type")
	cm.SetUID(types.UID("uid-cm-" + ns + "-" + cm.GetName()))
	cm.Object["data"] = map[string]any{"release": string(payload)}
	return cm
}

// gvrOf is where a driver keeps release records.
func gvrOf(driver helm.Driver) schema.GroupVersionResource {
	if driver == helm.DriverConfigMap {
		return configMapsGVR
	}
	return testkit.SecretsGVR
}

// helmServer is the cluster side of the release actions: it acts on the source's release records
// as the Helm SDK would (a rollback adds a deployed revision and supersedes the others, an
// uninstall deletes them), in the storage driver each release is kept with (Secrets unless put
// with putIn), records the calls, and can fail or block one. An action that names another driver
// than the release's finds no release, as the SDK would not.
type helmServer struct {
	*fakeWriter
	src *testkit.MemorySource
	t   testing.TB

	mu       sync.Mutex
	calls    []string
	revs     map[string]int         // namespace/name → newest revision
	drivers  map[string]helm.Driver // namespace/name → the driver its records are in (absent: Secrets)
	failErr  error
	block    chan struct{} // closed to let a blocked action continue; nil: no block
	started  chan struct{}
	upgrades []engine.HelmUpgradeRequest
}

var _ engine.HelmWriter = (*helmServer)(nil)

func (h *helmServer) put(ns, name string, rev int, status common.Status) {
	h.putIn(helm.DriverSecret, ns, name, rev, status)
}

// putIn stores a revision of a release kept with driver.
func (h *helmServer) putIn(driver helm.Driver, ns, name string, rev int, status common.Status) {
	h.mu.Lock()
	if h.revs == nil {
		h.revs = map[string]int{}
	}
	if h.drivers == nil {
		h.drivers = map[string]helm.Driver{}
	}
	if rev > h.revs[ns+"/"+name] {
		h.revs[ns+"/"+name] = rev
	}
	h.drivers[ns+"/"+name] = driver
	h.mu.Unlock()
	h.src.Set(gvrOf(driver), releaseRecord(h.t, driver, ns, name, rev, status))
}

// driverOf is the driver a release's records are in.
func (h *helmServer) driverOf(ns, name string) helm.Driver {
	h.mu.Lock()
	defer h.mu.Unlock()
	if d := h.drivers[ns+"/"+name]; d != "" {
		return d
	}
	return helm.DriverSecret
}

// wrongDriver is the SDK's answer to an action with another driver than the release's (the zero
// driver is Secrets): the driver holds no record of it.
func (h *helmServer) wrongDriver(ns, name string, driver helm.Driver) error {
	if driver == "" {
		driver = helm.DriverSecret
	}
	if h.driverOf(ns, name) != driver {
		return fmt.Errorf("helm %s/%s: release: not found (the %s driver holds no record of it)", ns, name, driver)
	}
	return nil
}

func (h *helmServer) begin(call string) error {
	h.mu.Lock()
	h.calls = append(h.calls, call)
	err, block, started := h.failErr, h.block, h.started
	h.mu.Unlock()
	if started != nil {
		close(started)
	}
	if block != nil {
		<-block
	}
	return err
}

func (h *helmServer) RollbackRelease(_ context.Context, namespace, name string, driver helm.Driver, revision, from int) error {
	if err := h.wrongDriver(namespace, name, driver); err != nil {
		return err
	}
	h.mu.Lock()
	cur := h.revs[namespace+"/"+name]
	h.mu.Unlock()
	if from > 0 && from != cur { // as helm.Actions.Rollback: the server's newest revision first
		return fmt.Errorf("helm rollback %s/%s: %w: it is at revision %d on the server, the action is based on revision %d; %w",
			namespace, name, helm.ErrReleaseMoved, cur, from, helm.ErrUnchanged)
	}
	if err := h.begin(fmt.Sprintf("rollback %s/%s to %d", namespace, name, revision)); err != nil {
		return err
	}
	d := h.driverOf(namespace, name)
	for r := 1; r <= cur; r++ {
		h.src.Set(gvrOf(d), releaseRecord(h.t, d, namespace, name, r, common.StatusSuperseded))
	}
	h.putIn(d, namespace, name, cur+1, common.StatusDeployed)
	return nil
}

func (h *helmServer) UninstallRelease(_ context.Context, namespace, name string, driver helm.Driver) error {
	if err := h.wrongDriver(namespace, name, driver); err != nil {
		return err
	}
	if err := h.begin("uninstall " + namespace + "/" + name); err != nil {
		return err
	}
	d := h.driverOf(namespace, name)
	h.mu.Lock()
	cur := h.revs[namespace+"/"+name]
	delete(h.revs, namespace+"/"+name)
	h.mu.Unlock()
	for r := 1; r <= cur; r++ {
		h.src.Delete(gvrOf(d), namespace, fmt.Sprintf("sh.helm.release.v1.%s.v%d", name, r))
	}
	return nil
}

// UpgradeRelease supersedes every revision and adds a deployed one with the release's chart
// version and the given values, as the Helm SDK's upgrade would; failErr fails it before that.
func (h *helmServer) UpgradeRelease(_ context.Context, namespace, name string, up engine.HelmUpgradeRequest) (int, error) {
	if err := h.wrongDriver(namespace, name, up.Driver); err != nil {
		return 0, err
	}
	h.mu.Lock()
	cur := h.revs[namespace+"/"+name]
	h.mu.Unlock()
	if up.FromRevision > 0 && up.FromRevision != cur { // as helm.Actions.Upgrade
		return 0, fmt.Errorf("helm upgrade %s/%s: %w: it is at revision %d on the server, the action is based on revision %d; %w",
			namespace, name, helm.ErrReleaseMoved, cur, up.FromRevision, helm.ErrUnchanged)
	}
	h.mu.Lock()
	h.upgrades = append(h.upgrades, up)
	h.mu.Unlock()
	if err := h.begin(fmt.Sprintf("upgrade %s/%s from %d", namespace, name, cur)); err != nil {
		return 0, err
	}
	d := h.driverOf(namespace, name)
	for r := 1; r <= cur; r++ {
		h.src.Set(gvrOf(d), releaseRecord(h.t, d, namespace, name, r, common.StatusSuperseded))
	}
	h.mu.Lock()
	h.revs[namespace+"/"+name] = cur + 1
	h.mu.Unlock()
	version := fmt.Sprintf("0.%d.0", cur)
	if up.Chart != nil { // a chart downloaded from a repository (helmupgrade_repo_test.go)
		version = up.Chart.Version
	}
	h.src.Set(gvrOf(d), releaseRecordWith(h.t, d, namespace, name, cur+1, common.StatusDeployed, version, up.Values))
	return cur + 1, nil
}

// moveUnseen makes the server hold a release at a newer revision the cache has not seen: a watch
// that lags.
func (h *helmServer) moveUnseen(namespace, name string, revision int) {
	h.mu.Lock()
	h.revs[namespace+"/"+name] = revision
	h.mu.Unlock()
}

func (h *helmServer) recorded() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.calls...)
}

// helmConnector connects "test" with the Secrets and Helm releases profiles and the helmServer.
type helmConnector struct {
	src    *testkit.MemorySource
	server *helmServer
}

func (c *helmConnector) Contexts() ([]engine.ContextInfo, error) {
	return []engine.ContextInfo{{Name: "test", Current: true}}, nil
}

func (c *helmConnector) Connect(_ context.Context, name string) (*engine.Backend, error) {
	ch := cache.New(c.src, cache.Options{WarmPeriod: time.Minute})
	return &engine.Backend{
		Context: name, Namespace: "default", Cache: ch, Close: ch.Close,
		// Config Maps served: the releases read the release ConfigMaps too (helmdriver.go).
		Profiles: append(testProfiles(), configMapsProfile(), helm.NewReleaseProfile()),
		Conn:     &cluster.Conn{}, Actions: c.server,
	}, nil
}

// helmHarness is a session on the Helm connector: web at revision 3 and api at revision 1 in
// apps; a recording confirmer; an audit log; the session's events.
type helmHarness struct {
	s       *engine.Session
	src     *testkit.MemorySource
	server  *helmServer
	journal *engine.MemoryHelmJournal
	audit   *syncBuffer

	mu     sync.Mutex
	asked  []engine.ConfirmRequest
	answer bool
	onAsk  func()
	events []engine.Event
}

func newHelmHarness(t *testing.T, journal *engine.MemoryHelmJournal, seed func(*helmServer)) *helmHarness {
	t.Helper()
	src := testkit.NewMemorySource()
	h := &helmHarness{src: src, server: &helmServer{fakeWriter: &fakeWriter{}, src: src, t: t}, journal: journal, audit: &syncBuffer{}, answer: true}
	if seed == nil {
		seed = func(s *helmServer) {
			s.put("apps", "web", 1, common.StatusSuperseded)
			s.put("apps", "web", 2, common.StatusSuperseded)
			s.put("apps", "web", 3, common.StatusDeployed)
			s.put("apps", "api", 1, common.StatusDeployed)
		}
	}
	seed(h.server)
	h.s = engine.New(engine.Options{
		Connector: &helmConnector{src: src, server: h.server}, SnapshotInterval: 10 * time.Millisecond,
		Audit: h.audit, Helm: engine.NewHelmOps(journal),
		Confirmer: engine.ConfirmFunc(func(ctx context.Context, req engine.ConfirmRequest) (bool, error) {
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
	if _, err := h.s.Objects(context.Background(), helm.ReleaseKind, nil); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *helmHarness) dispatch(origin engine.Origin, id string, params any) (any, error) {
	raw, _ := json.Marshal(params)
	return h.s.Dispatch(context.Background(), engine.Invocation{ID: id, Params: raw, Origin: origin})
}

func (h *helmHarness) confirmations() []engine.ConfirmRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]engine.ConfirmRequest(nil), h.asked...)
}

func (h *helmHarness) eventsOf(typ engine.EventType) []engine.Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []engine.Event
	for _, ev := range h.events {
		if ev.Type == typ {
			out = append(out, ev)
		}
	}
	return out
}

// waitRelease waits until the cache shows a release at a revision (0: gone).
func (h *helmHarness) waitRelease(t *testing.T, ns, name string, rev int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ref := model.ObjectRef{Kind: helm.ReleaseKind, Namespace: ns, Name: name}
	if !testkit.WaitFor(ctx, func() bool {
		obj, err := h.s.Lookup(ref)
		if rev == 0 {
			return errors.Is(err, engine.ErrNotFound)
		}
		return err == nil && helm.ReleaseRevision(obj) == rev
	}) {
		t.Fatalf("release %s/%s never reached revision %d", ns, name, rev)
	}
}

var webRef = model.ObjectRef{Context: "test", Kind: helm.ReleaseKind, Namespace: "apps", Name: "web"}

// The Releases view and drawer come from the cache's release Secrets: one row per release, the
// drawer as Lens's (Properties, values, notes, resources); for agents the values and the notes
// are withheld; the drawer opens no events watch.
func TestHelmReleasesViewAndDrawer(t *testing.T) {
	h := newHelmHarness(t, engine.NewMemoryHelmJournal(), nil)
	var list commands.ListResult
	res, err := h.dispatch(engine.OriginExternalAgent, "resources.list", map[string]any{"kind": helm.ReleaseKind})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res)
	_ = json.Unmarshal(b, &list)
	if list.Total != 2 || list.Rows[1].Cells["Revision"] != "3" || list.Rows[1].Cells["Status"] != "Deployed" || list.Rows[1].Cells["Chart"] != "web" {
		t.Fatalf("releases list %s", b)
	}
	if strings.Contains(string(b), "hunter2") {
		t.Fatal("a value reached the list")
	}

	get := func(origin engine.Origin) (string, *engine.DetailModel) {
		res, err := h.dispatch(origin, "object.get", map[string]any{"ref": webRef})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(res)
		var m engine.DetailModel
		_ = json.Unmarshal(b, &m)
		return string(b), &m
	}
	agentJSON, m := get(engine.OriginExternalAgent)
	if strings.Contains(agentJSON, "hunter2") {
		t.Errorf("object.get for an agent carries a value: %s", agentJSON)
	}
	if sectionText(m, helm.TitleValues) != model.Withheld || sectionText(m, helm.TitleNotes) != model.Withheld {
		t.Errorf("values and notes for an agent: %q, %q", sectionText(m, helm.TitleValues), sectionText(m, helm.TitleNotes))
	}
	if m.Title != "Release: web" || !m.NoEvents || len(m.Events) != 0 {
		t.Errorf("title %q, no events %v", m.Title, m.NoEvents)
	}
	var titles []string
	for _, s := range m.Sections {
		titles = append(titles, s.Group+"/"+s.Title)
	}
	if strings.Join(titles, ",") != "/Properties,/Values,/Notes,Resources/ConfigMap,Resources/Secret" {
		t.Errorf("sections %v", titles)
	}
	var names []string
	for _, it := range m.Sections[0].Items {
		names = append(names, it.Name)
	}
	if strings.Join(names, ",") != "Chart,Updated,Namespace,Version,Status,Storage" || !strings.Contains(sectionText(m, model.TitleProperties), "Deployed") {
		t.Errorf("properties %v: %q", names, sectionText(m, model.TitleProperties))
	}
	if !strings.Contains(m.YAML, "kind: HelmRelease") || strings.Contains(m.YAML, "adminPassword") {
		t.Errorf("YAML %q", m.YAML)
	}
	// The drawer open in the GUI watches the release Secrets, not the events.
	if _, err := h.dispatch(engine.OriginGUI, "ui.navigate", map[string]any{"ref": webRef}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Detail(webRef); err != nil {
		t.Fatal(err)
	}
	for _, inf := range h.s.Diagnostics().Informers {
		if inf.Scope.GVR.Resource == "events" {
			t.Errorf("a release's drawer watches events: %+v", inf)
		}
	}
	if _, cli := get(engine.OriginCLI); !strings.Contains(sectionText(cli, helm.TitleValues), "adminPassword: hunter2") {
		t.Errorf("values for the CLI: %q", sectionText(cli, helm.TitleValues))
	}
	gui, err := h.s.Detail(webRef)
	if err != nil || !strings.Contains(sectionText(gui, helm.TitleValues), "adminPassword: hunter2") {
		t.Errorf("values in the GUI's drawer: %q, %v", sectionText(gui, helm.TitleValues), err)
	}
	for _, e := range h.audit.Lines() {
		if b, _ := json.Marshal(e); strings.Contains(string(b), "hunter2") {
			t.Errorf("audit line with a value: %s", b)
		}
	}
}

// sectionText joins the values of a drawer section.
func sectionText(m *engine.DetailModel, title string) string {
	var out []string
	for _, s := range m.Sections {
		if s.Title == title {
			for _, it := range s.Items {
				out = append(out, it.Value)
			}
		}
	}
	return strings.Join(out, "\n")
}

// Rollback and uninstall are destructive for every origin: the GUI and agents are asked, with the
// release's newest Secret's UID recorded; without a GUI they are refused; declined, nothing runs.
func TestHelmActionsConfirmPerOrigin(t *testing.T) {
	h := newHelmHarness(t, engine.NewMemoryHelmJournal(), nil)
	for _, origin := range []engine.Origin{engine.OriginGUI, engine.OriginExternalAgent, engine.OriginBuiltInAgent} {
		h.mu.Lock()
		h.answer = false
		h.mu.Unlock()
		_, err := h.dispatch(origin, "helm.release.rollback", map[string]any{"ref": webRef, "revision": 2})
		if !errors.Is(err, engine.ErrDeclined) {
			t.Errorf("%s: declined rollback: %v", origin, err)
		}
		_, err = h.dispatch(origin, "helm.release.uninstall", map[string]any{"ref": webRef})
		if !errors.Is(err, engine.ErrDeclined) {
			t.Errorf("%s: declined uninstall: %v", origin, err)
		}
	}
	asked := h.confirmations()
	if len(asked) != 6 {
		t.Fatalf("%d confirmations, want 6", len(asked))
	}
	for _, req := range asked {
		if req.Danger != engine.DangerDestructive || len(req.UIDs) != 1 || req.UIDs[0] != "uid-apps-sh.helm.release.v1.web.v3" ||
			req.Context != "test" || len(req.Namespaces) != 1 || req.Namespaces[0] != "apps" {
			t.Errorf("confirmation %+v", req)
		}
	}
	if !strings.HasSuffix(asked[0].Message(), "? To revision: 2.") || !strings.Contains(asked[1].Message(), "Deletes: every object of the release, its history.") ||
		!slices.Equal(asked[1].Effects, []engine.Effect{{Name: "Deletes", Value: "every object of the release, its history", Tone: engine.EffectLost}}) {
		t.Errorf("messages %q, %q", asked[0].Message(), asked[1].Message())
	}
	if calls := h.server.recorded(); len(calls) != 0 {
		t.Errorf("declined actions ran: %v", calls)
	}

	h.s.SetConfirmer(nil) // headless
	if _, err := h.dispatch(engine.OriginExternalAgent, "helm.release.uninstall", map[string]any{"ref": webRef}); !errors.Is(err, engine.ErrConfirmationUnavailable) {
		t.Errorf("headless uninstall: %v", err)
	}
}

// A rollback confirmed for one revision is refused when the release moved on while the dialog was
// open (another upgrade): the confirmation bound the newest release Secret's UID.
func TestHelmRollbackRefusedWhenTheReleaseMoved(t *testing.T) {
	h := newHelmHarness(t, engine.NewMemoryHelmJournal(), nil)
	h.mu.Lock()
	h.onAsk = func() {
		h.server.put("apps", "web", 4, common.StatusDeployed)
		h.waitRelease(t, "apps", "web", 4)
	}
	h.mu.Unlock()
	_, err := h.dispatch(engine.OriginGUI, "helm.release.rollback", map[string]any{"ref": webRef, "revision": 2})
	if !errors.Is(err, engine.ErrTargetsChanged) || !strings.Contains(err.Error(), "moved to revision 4") {
		t.Fatalf("rollback after the release moved: %v", err)
	}
	if calls := h.server.recorded(); len(calls) != 0 {
		t.Errorf("the rollback ran: %v", calls)
	}
}

// Rollback applies after an upgrade only, starts from the previous revision, refuses a revision
// the history lacks; a successful rollback and uninstall leave the journal empty; a failed one
// stays in it as failed.
func TestHelmRollbackAndUninstall(t *testing.T) {
	journal := engine.NewMemoryHelmJournal()
	h := newHelmHarness(t, journal, nil)
	reg := h.s.Registry()
	rb, _ := reg.Get("helm.release.rollback")
	apiRef := model.ObjectRef{Context: "test", Kind: helm.ReleaseKind, Namespace: "apps", Name: "api"}
	if rb.Applicable(h.s, mustJSON(t, map[string]any{"ref": apiRef, "revision": 0})) {
		t.Error("rollback offered for a release at revision 1")
	}
	if !rb.Applicable(h.s, mustJSON(t, map[string]any{"ref": webRef, "revision": 0})) {
		t.Error("rollback not offered after an upgrade")
	}
	if fields := rb.DialogFields(); len(fields) != 1 || fields[0] != "revision" {
		t.Errorf("dialog fields %+v", fields)
	}
	pre, err := rb.Prefill(h.s, mustJSON(t, map[string]any{"ref": webRef, "revision": 0}))
	if err != nil || !strings.Contains(string(pre), `"revision":2`) {
		t.Errorf("prefill %s, %v", pre, err)
	}
	actions := map[string]int{}
	for _, c := range reg.Actions(helm.ReleaseKind) {
		actions[c.ID] = c.Order
	}
	if actions["helm.release.rollback"] != 200 || actions["helm.release.uninstall"] != engine.OrderDelete {
		t.Errorf("row menu %v", actions)
	}
	del, _ := reg.Get("object.delete")
	if del.Applicable(h.s, mustJSON(t, map[string]any{"ref": webRef})) {
		t.Error("object.delete applies to a release")
	}

	if _, err := h.dispatch(engine.OriginGUI, "helm.release.rollback", map[string]any{"ref": webRef, "revision": 4}); err == nil ||
		!strings.Contains(err.Error(), "no revision 4") {
		t.Errorf("rollback to a revision the release never had: %v", err)
	}
	// The newest revision is a target too, as Lens's dialog allows (captured 2026-10-12): a new
	// revision equal to it.
	res, err := h.dispatch(engine.OriginGUI, "helm.release.rollback", map[string]any{"ref": webRef, "revision": 3})
	if err != nil {
		t.Fatalf("rollback to the newest revision: %v", err)
	}
	if r := res.(commands.HelmResult); r.Action != "rolledBack" || r.Revision != 3 {
		t.Errorf("result %+v", r)
	}
	h.waitRelease(t, "apps", "web", 4)
	res, err = h.dispatch(engine.OriginGUI, "helm.release.rollback", map[string]any{"ref": webRef, "revision": 1})
	if err != nil {
		t.Fatal(err)
	}
	if r := res.(commands.HelmResult); r.Action != "rolledBack" || r.Revision != 1 {
		t.Errorf("result %+v", r)
	}
	h.waitRelease(t, "apps", "web", 5)
	if list, _ := journal.List(); len(list) != 0 {
		t.Errorf("journal after a rollback: %+v", list)
	}

	h.server.mu.Lock()
	h.server.failErr = errors.New("cannot patch \"web\" with kind Deployment: the server rejected it")
	h.server.mu.Unlock()
	if _, err := h.dispatch(engine.OriginGUI, "helm.release.uninstall", map[string]any{"ref": webRef}); err == nil ||
		!strings.Contains(err.Error(), "may be left pending") {
		t.Errorf("failed uninstall: %v", err)
	}
	list, _ := journal.List()
	if len(list) != 1 || list[0].State != engine.HelmOpFailed || list[0].Action != engine.HelmUninstall || !strings.Contains(list[0].Error, "rejected") {
		t.Fatalf("journal after a failure: %+v", list)
	}
	h.server.mu.Lock()
	h.server.failErr = nil
	h.server.mu.Unlock()
	if _, err := h.dispatch(engine.OriginGUI, "helm.release.uninstall", map[string]any{"ref": webRef}); err != nil {
		t.Fatal(err)
	}
	h.waitRelease(t, "apps", "web", 0)
	if list, _ := journal.List(); len(list) != 0 {
		t.Errorf("journal after the uninstall: %+v", list)
	}
	want := []string{"rollback apps/web to 3", "rollback apps/web to 1", "uninstall apps/web", "uninstall apps/web"}
	if got := h.server.recorded(); strings.Join(got, ";") != strings.Join(want, ";") {
		t.Errorf("calls %v, want %v", got, want)
	}
	toasts := h.eventsOf(engine.EventToast)
	if len(toasts) == 0 || !strings.Contains(toasts[len(toasts)-1].Message, "Release web uninstalled") {
		t.Errorf("toasts %+v", toasts)
	}
}

// While an action on a release runs, a second one on it is disabled.
func TestHelmActionsOnePerRelease(t *testing.T) {
	h := newHelmHarness(t, engine.NewMemoryHelmJournal(), nil)
	h.server.mu.Lock()
	h.server.block, h.server.started = make(chan struct{}), make(chan struct{})
	h.server.mu.Unlock()
	done := make(chan error, 1)
	go func() {
		_, err := h.dispatch(engine.OriginGUI, "helm.release.rollback", map[string]any{"ref": webRef, "revision": 2})
		done <- err
	}()
	<-h.server.started
	_, err := h.dispatch(engine.OriginGUI, "helm.release.uninstall", map[string]any{"ref": webRef})
	var dis *engine.DisabledError
	if !errors.As(err, &dis) || !strings.Contains(dis.Reason, "is running") {
		t.Errorf("a second action while one runs: %v", err)
	}
	var running []engine.PendingHelmOpInfo
	res, _ := h.dispatch(engine.OriginExternalAgent, "helm.pendingOperations", struct{}{})
	b, _ := json.Marshal(res)
	_ = json.Unmarshal(b, &running)
	if len(running) != 1 || running[0].Status != engine.HelmOpStatusRunning || running[0].Revision != 2 || running[0].FromRevision != 3 {
		t.Errorf("pending while running: %s", b)
	}
	close(h.server.block)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// D-11: a rollback a crash interrupted (journal written, the release left pending-rollback) is
// offered on the next connect, with the release's state from the cache; running it again
// finishes it; an uninstall whose release is gone is dropped with a note; Dismiss forgets one.
func TestHelmCrashJournalOffer(t *testing.T) {
	journal := engine.NewMemoryHelmJournal()
	started := releaseT0.Add(48 * time.Hour)
	for _, op := range []engine.PendingHelmOp{
		{Context: "test", Namespace: "apps", Release: "web", Action: engine.HelmRollback, Revision: 2, FromRevision: 3,
			Started: started, StartedBy: engine.OriginGUI, State: engine.HelmOpRunning},
		{Context: "test", Namespace: "apps", Release: "gone", Action: engine.HelmUninstall, FromRevision: 1,
			Started: started, StartedBy: engine.OriginExternalAgent, State: engine.HelmOpRunning},
		{Context: "test", Namespace: "apps", Release: "api", Action: engine.HelmUninstall, FromRevision: 1,
			Started: started.Add(time.Minute), StartedBy: engine.OriginGUI, State: engine.HelmOpRunning},
		{Context: "elsewhere", Namespace: "apps", Release: "web", Action: engine.HelmUninstall, Started: started, State: engine.HelmOpRunning},
	} {
		if err := journal.Put(op); err != nil {
			t.Fatal(err)
		}
	}
	h := newHelmHarness(t, journal, func(s *helmServer) {
		s.put("apps", "web", 1, common.StatusSuperseded)
		s.put("apps", "web", 2, common.StatusSuperseded)
		s.put("apps", "web", 3, common.StatusSuperseded)
		s.put("apps", "web", 4, common.StatusPendingRollback) // what the crash left
		s.put("apps", "api", 1, common.StatusUninstalling)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		for _, ev := range h.eventsOf(engine.EventHelm) {
			if strings.Contains(ev.Message, "2 Helm actions in context test did not finish") {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("no offer: %+v", h.eventsOf(engine.EventHelm))
	}
	toasts := h.eventsOf(engine.EventToast)
	if len(toasts) == 0 || !strings.Contains(toasts[0].Message, "dropped the unfinished uninstall of release gone") {
		t.Errorf("toasts %+v", toasts)
	}

	var pending []engine.PendingHelmOpInfo
	res, err := h.dispatch(engine.OriginExternalAgent, "helm.pendingOperations", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res)
	_ = json.Unmarshal(b, &pending)
	if len(pending) != 2 || pending[0].Release != "web" || pending[0].Status != engine.HelmOpStatusInterrupted ||
		pending[0].ReleaseStatus != "pending-rollback" || pending[0].ReleaseRevision != 4 || pending[1].ReleaseStatus != "uninstalling" {
		t.Fatalf("pending %s", b)
	}
	res, _ = h.dispatch(engine.OriginExternalAgent, "helm.pendingOperations", map[string]any{"all": true})
	b, _ = json.Marshal(res)
	if !strings.Contains(string(b), `"context":"elsewhere"`) {
		t.Errorf("all contexts: %s", b)
	}

	// Finish: run the rollback again, as recorded.
	if _, err := h.dispatch(engine.OriginGUI, "helm.release.rollback", map[string]any{"ref": pending[0].Ref(), "revision": pending[0].Revision}); err != nil {
		t.Fatal(err)
	}
	h.waitRelease(t, "apps", "web", 5)
	// Dismiss the other.
	if _, err := h.dispatch(engine.OriginGUI, "helm.dismissOperation", map[string]any{"ref": map[string]any{"kind": helm.ReleaseKind, "namespace": "apps", "name": "api"}}); err != nil {
		t.Fatal(err)
	}
	list, _ := journal.List()
	if len(list) != 1 || list[0].Context != "elsewhere" {
		t.Errorf("journal after finishing and dismissing: %+v", list)
	}
	if _, err := h.dispatch(engine.OriginGUI, "helm.dismissOperation", map[string]any{"ref": map[string]any{"kind": helm.ReleaseKind, "namespace": "apps", "name": "api"}}); err == nil {
		t.Error("dismissed twice")
	}
	if calls := h.server.recorded(); len(calls) != 1 || calls[0] != "rollback apps/web to 2" {
		t.Errorf("calls %v", calls)
	}
}

// The file journal survives a restart, refuses to overwrite a file it cannot read, and keeps its
// file owner-only.
func TestFileHelmJournal(t *testing.T) {
	dir := t.TempDir()
	j := engine.NewFileHelmJournal(dir)
	op := engine.PendingHelmOp{Context: "c", Namespace: "apps", Release: "web", Action: engine.HelmRollback, Revision: 1, State: engine.HelmOpRunning}
	if err := j.Put(op); err != nil {
		t.Fatal(err)
	}
	again, err := engine.NewFileHelmJournal(dir).List()
	if err != nil || len(again) != 1 || again[0].Revision != 1 {
		t.Fatalf("after a restart: %+v, %v", again, err)
	}
	if err := j.Remove("c", "apps", "web"); err != nil {
		t.Fatal(err)
	}
	if list, _ := j.List(); len(list) != 0 {
		t.Errorf("after Remove: %+v", list)
	}
	assertOwnerOnly(t, j.Path())
	writeFile(t, j.Path(), "{not json")
	if _, err := j.List(); err == nil {
		t.Error("an unreadable journal was read")
	}
	if err := j.Put(op); err == nil {
		t.Error("an unreadable journal was overwritten")
	}
}

func assertOwnerOnly(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("%s has mode %v, want 0600", path, info.Mode().Perm())
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// An open release drawer follows a new revision: its cache holds the release's Secrets, whose
// names are not the release's, so a wake filtered by the release's name never came (review
// 2026-10-13).
func TestHelmReleaseDrawerFollowsANewRevision(t *testing.T) {
	h := newHelmHarness(t, engine.NewMemoryHelmJournal(), nil)
	if _, err := h.dispatch(engine.OriginGUI, "ui.navigate", map[string]any{"ref": webRef}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Detail(webRef); err != nil {
		t.Fatal(err)
	}
	before := len(h.eventsOf(engine.EventDetail))
	h.server.put("apps", "web", 4, common.StatusDeployed)
	h.waitRelease(t, "apps", "web", 4)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool { return len(h.eventsOf(engine.EventDetail)) > before }) {
		t.Fatal("the open release drawer was not told about revision 4")
	}
}
