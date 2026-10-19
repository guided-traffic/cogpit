package commands_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"helm.sh/helm/v4/pkg/release/common"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// Helm's ConfigMaps storage driver (HELM_DRIVER=configmap) in the commands: the releases kept in
// ConfigMaps beside those kept in Secrets, the actions run with each release's own driver, the
// verbs checked on the release's own records, the HELM_DRIVER preference for new installs, and
// the release ConfigMaps' payloads kept from agents.

// configMapsProfile is the built-in Config Maps kind: its drawer, cells and fields.
func configMapsProfile() *model.KindProfile {
	p, ok := kinds.ByKey("configmaps")
	if !ok {
		panic("no configmaps kind")
	}
	return p
}

// cmSeed is web at revision 3 in Secrets and cm at revision 2 in ConfigMaps, both in apps.
func cmSeed(s *helmServer) {
	s.put("apps", "web", 1, common.StatusSuperseded)
	s.put("apps", "web", 2, common.StatusSuperseded)
	s.put("apps", "web", 3, common.StatusDeployed)
	s.putIn(helm.DriverConfigMap, "apps", "cm", 1, common.StatusSuperseded)
	s.putIn(helm.DriverConfigMap, "apps", "cm", 2, common.StatusDeployed)
}

var cmRef = model.ObjectRef{Context: "test", Kind: helm.ReleaseKind, Namespace: "apps", Name: "cm"}

// A release kept in ConfigMaps is listed beside one kept in Secrets, its drawer names its
// storage, its values come from its ConfigMap (the window reads them, agents read them withheld),
// and its Upgrade tab opens on them.
func TestHelmConfigMapReleasesListedAndRead(t *testing.T) {
	h := newHelmHarness(t, engine.NewMemoryHelmJournal(), cmSeed)
	res, err := h.dispatch(engine.OriginExternalAgent, "resources.list", map[string]any{"kind": helm.ReleaseKind})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range res.(commands.ListResult).Rows {
		names = append(names, r.Ref.Name+"@"+r.Cells["Revision"])
	}
	slices.Sort(names)
	if strings.Join(names, ",") != "cm@2,web@3" {
		t.Fatalf("releases %v", names)
	}
	res, err = h.dispatch(engine.OriginExternalAgent, "object.get", map[string]any{"ref": cmRef})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(*engine.DetailModel)
	if s := sectionText(m, model.TitleProperties); !strings.Contains(s, "ConfigMaps") || !strings.Contains(s, "Deployed") {
		t.Errorf("properties %q", s)
	}
	if v := sectionText(m, helm.TitleValues); v != model.Withheld || strings.Contains(m.YAML, "hunter2") {
		t.Errorf("an agent reads the values: %q / %q", v, m.YAML)
	}
	res, err = h.dispatch(engine.OriginGUI, "helm.release.values", map[string]any{"ref": cmRef})
	if err != nil {
		t.Fatal(err)
	}
	if v := res.(helm.Values); !strings.Contains(v.YAML, "hunter2") || v.Revision != 2 {
		t.Errorf("the window's values %+v", v)
	}
	res, err = h.dispatch(engine.OriginExternalAgent, "helm.release.values", map[string]any{"ref": cmRef})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(res); strings.Contains(string(b), "hunter2") {
		t.Errorf("an agent reads the values: %s", b)
	}
	res, err = h.dispatch(engine.OriginGUI, helm.UpgradeTabCommand, map[string]any{"ref": cmRef})
	if err != nil {
		t.Fatal(err)
	}
	if tab := res.(commands.EditResult); !strings.Contains(tab.Text, "hunter2") {
		t.Errorf("the Upgrade tab's values %q", tab.Text)
	}
}

// Rollback, upgrade and uninstall of a release kept in ConfigMaps run with the ConfigMaps driver
// (the fake server, as the SDK, finds no release with another), and the cache follows its
// ConfigMaps; the release kept in Secrets keeps the Secrets driver.
func TestHelmConfigMapReleaseActions(t *testing.T) {
	h := newHelmHarness(t, engine.NewMemoryHelmJournal(), cmSeed)
	if _, err := h.dispatch(engine.OriginGUI, "helm.release.rollback", map[string]any{"ref": cmRef, "revision": 1}); err != nil {
		t.Fatal(err)
	}
	h.waitRelease(t, "apps", "cm", 3)
	vals := "replicas: 9\n"
	if _, err := h.dispatch(engine.OriginGUI, "helm.release.upgrade", map[string]any{"ref": cmRef, "values": vals, "revision": 3}); err != nil {
		t.Fatal(err)
	}
	h.waitRelease(t, "apps", "cm", 4)
	h.server.mu.Lock()
	up := h.server.upgrades[len(h.server.upgrades)-1]
	h.server.mu.Unlock()
	if up.Driver != helm.DriverConfigMap || up.FromRevision != 3 {
		t.Errorf("upgrade request %+v", up)
	}
	if obj, err := h.s.Lookup(cmRef); err != nil || helm.ReleaseStorage(obj) != helm.DriverConfigMap {
		t.Errorf("after the upgrade: %v %v", obj, err)
	}
	if _, err := h.dispatch(engine.OriginGUI, "helm.release.rollback", map[string]any{"ref": webRef, "revision": 2}); err != nil {
		t.Fatalf("the release kept in Secrets: %v", err)
	}
	if _, err := h.dispatch(engine.OriginGUI, "helm.release.uninstall", map[string]any{"ref": cmRef}); err != nil {
		t.Fatal(err)
	}
	h.waitRelease(t, "apps", "cm", 0)
	cms, err := h.s.Objects(context.Background(), "configmaps", []string{"apps"})
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range cms {
		if strings.HasPrefix(o.GetName(), "sh.helm.release.v1.cm.") {
			t.Errorf("release ConfigMap %s left", o.GetName())
		}
	}
	calls := h.server.recorded()
	if !slices.Contains(calls, "uninstall apps/cm") || !slices.Contains(calls, "rollback apps/cm to 1") || !slices.Contains(calls, "rollback apps/web to 2") {
		t.Errorf("calls %v", calls)
	}
}

// accessFake denies every verb on one resource and allows the rest.
type accessFake struct{ deny string }

func (a accessFake) Allowed(_ string, v engine.Verb) (bool, bool) { return v.Resource != a.deny, true }

// accessConnector is the Helm connector with the user's access reviews answered by access.
type accessConnector struct {
	*helmConnector
	access engine.Access
}

func (c *accessConnector) Connect(ctx context.Context, name string) (*engine.Backend, error) {
	b, err := c.helmConnector.Connect(ctx, name)
	if err == nil {
		b.Access = c.access
	}
	return b, err
}

// The actions on a release declare the verbs on both drivers' records; the access check asks
// only for its own driver's: a user who may not touch Secrets may still roll back a release kept
// in ConfigMaps, and not one kept in Secrets, and the other way round.
func TestHelmVerbsFollowTheReleasesDriver(t *testing.T) {
	for _, tc := range []struct {
		deny            string
		cmOK, secretsOK bool
	}{{"secrets", true, false}, {"configmaps", false, true}} {
		src := testkit.NewMemorySource()
		server := &helmServer{fakeWriter: &fakeWriter{}, src: src, t: t}
		cmSeed(server)
		s := engine.New(engine.Options{
			Connector:        &accessConnector{helmConnector: &helmConnector{src: src, server: server}, access: accessFake{deny: tc.deny}},
			SnapshotInterval: 10 * time.Millisecond, Helm: engine.NewHelmOps(engine.NewMemoryHelmJournal()),
		})
		if err := commands.RegisterAll(s.Registry()); err != nil {
			t.Fatal(err)
		}
		if err := s.Connect(context.Background(), "test"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Objects(context.Background(), helm.ReleaseKind, nil); err != nil {
			t.Fatalf("deny %s: %v", tc.deny, err)
		}
		cmd, _ := s.Registry().Get("helm.release.rollback")
		var declared []string
		for _, v := range cmd.Verbs {
			declared = append(declared, v.Verb+" "+v.Resource)
		}
		if !slices.Contains(declared, "update configmaps") || !slices.Contains(declared, "update secrets") {
			t.Errorf("declared %v", declared)
		}
		for ref, want := range map[model.ObjectRef]bool{cmRef: tc.cmOK, webRef: tc.secretsOK} {
			raw, _ := json.Marshal(map[string]any{"ref": ref, "revision": 1})
			err := cmd.Enabled(s, raw)
			if got := err == nil; got != want || (!want && !errors.Is(err, engine.ErrNotPermitted)) {
				t.Errorf("deny %s, %s: enabled %v (%v), want %v", tc.deny, ref.Name, got, err, want)
			}
		}
		s.Close()
	}
}

// The HELM_DRIVER preference: Secrets by default, configmap set in the window; agents and the CLI
// may set it back to secret, not to configmap; sql is refused for everyone, as is anything else.
func TestHelmDriverPreference(t *testing.T) {
	s := engine.New(engine.Options{Preferences: engine.NewPreferencesStore(t.TempDir())})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	set := func(origin engine.Origin, driver string) error {
		raw, _ := json.Marshal(map[string]any{"helmDriver": driver})
		_, err := s.Dispatch(context.Background(), engine.Invocation{ID: "preferences.set", Params: raw, Origin: origin})
		return err
	}
	if s.HelmDriver() != helm.DriverSecret {
		t.Errorf("default %s", s.HelmDriver())
	}
	if err := set(engine.OriginExternalAgent, "configmap"); !errors.Is(err, engine.ErrHelmDriverGUIOnly) {
		t.Errorf("an agent's configmap: %v", err)
	}
	if err := set(engine.OriginCLI, "ConfigMaps"); !errors.Is(err, engine.ErrHelmDriverGUIOnly) {
		t.Errorf("the CLI's configmaps: %v", err)
	}
	if err := set(engine.OriginGUI, "configmaps"); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Preferences(); p.HelmDriver != "configmap" || s.HelmDriver() != helm.DriverConfigMap || s.UIState().Preferences.HelmDriver != "configmap" {
		t.Errorf("kept %q, in effect %s", p.HelmDriver, s.HelmDriver())
	}
	for _, bad := range []string{"sql", "memory"} {
		if err := set(engine.OriginGUI, bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	if err := set(engine.OriginGUI, "sql"); !errors.Is(err, helm.ErrSQLDriver) {
		t.Errorf("sql: %v", err)
	}
	if err := set(engine.OriginExternalAgent, "secret"); err != nil {
		t.Fatalf("an agent sets it back: %v", err)
	}
	if p, _ := s.Preferences(); p.HelmDriver != "" || s.HelmDriver() != helm.DriverSecret {
		t.Errorf("after secret: %q", p.HelmDriver)
	}
}

// configMapsChartConnector is the chart connector with Config Maps served, so that a release
// installed into ConfigMaps shows.
type configMapsChartConnector struct{ *chartConnector }

func (c *configMapsChartConnector) Connect(ctx context.Context, name string) (*engine.Backend, error) {
	ch := cache.New(c.src, cache.Options{WarmPeriod: time.Minute})
	profiles := append(append(testProfiles(), configMapsProfile(), helm.NewReleaseProfile()), kinds.Local()...)
	return &engine.Backend{Context: name, Namespace: "default", Cache: ch, Close: ch.Close, Profiles: profiles,
		Conn: &cluster.Conn{}, Actions: c.actions}, nil
}

// A new install takes the HELM_DRIVER preference: its request names the ConfigMaps driver, an
// agent's confirmation says where its values go, and the release shows from its ConfigMap.
func TestHelmInstallUsesTheDriverPreference(t *testing.T) {
	src := testkit.NewMemorySource()
	fi := newFakeInstaller(t, src)
	h := &chartHarness{src: src, helmFile: testkit.NewHelmRepoFile(t), cacheDir: t.TempDir(), audit: &syncBuffer{}, journal: engine.NewMemoryHelmJournal(), answer: true}
	h.repo = testkit.NewHelmRepo(t, webChartV("0.1.0"), webChartV("0.2.0"))
	h.repos = engine.NewHelmRepos(h.helmFile.Path, h.cacheDir, engine.WithChartFetcher(&helm.Fetcher{Client: h.repo.Client()}))
	h.s = engine.New(engine.Options{
		Connector:        &configMapsChartConnector{&chartConnector{src: src, actions: fi}},
		SnapshotInterval: 10 * time.Millisecond, Audit: h.audit, Helm: engine.NewHelmOps(h.journal), HelmRepos: h.repos,
		Preferences: engine.NewPreferencesStore(t.TempDir()),
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
	t.Cleanup(h.s.Close)
	if err := h.s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	h.addRepo(t)
	h.charts(t, 1)
	if _, err := h.dispatch(engine.OriginGUI, "preferences.set", map[string]any{"helmDriver": "configmap"}); err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"repository": "test", "chart": "web", "version": "0.2.0", "values": "greeting: hi\n", "namespace": "apps", "name": "web"}
	if _, err := h.dispatch(engine.OriginExternalAgent, "helm.chart.install", params); err != nil {
		t.Fatal(err)
	}
	asked := h.confirmations()
	if len(asked) != 1 || !strings.Contains(asked[0].Detail, "Records: in ConfigMaps (HELM_DRIVER)") {
		t.Errorf("confirmation %+v", asked)
	}
	ins := fi.recordedInstalls()
	if len(ins) != 1 || ins[0].Driver != helm.DriverConfigMap {
		t.Fatalf("installs %+v", ins)
	}
	h.waitRelease(t, "apps", "web", 1)
	obj, err := h.s.Lookup(engine.HelmReleaseRef("test", "apps", "web"))
	if err != nil || helm.ReleaseStorage(obj) != helm.DriverConfigMap {
		t.Errorf("the installed release %v %v", obj, err)
	}
	// A second install of the name is refused from the cache, whichever driver holds it.
	if _, err := h.dispatch(engine.OriginGUI, "helm.chart.install", params); err == nil {
		t.Error("a second install of web")
	}
}

// The release ConfigMaps' payloads never reach agents through the Config Maps kind: object_get's
// YAML has the payload redacted and its drawer the payload's size; resources_list names the key
// only; an agent cannot open an Edit tab on one, nor read the text of the user's, nor apply or
// create one; the audit log has the payload redacted. The user sees the drawer and edits in the
// window as before.
func TestHelmConfigMapPayloadsKeptFromAgents(t *testing.T) {
	h := newHelmHarness(t, engine.NewMemoryHelmJournal(), cmSeed)
	ref := model.ObjectRef{Context: "test", Kind: "configmaps", Namespace: "apps", Name: "sh.helm.release.v1.cm.v2"}
	payload, _, _ := unstructured.NestedString(releaseRecordWith(t, helm.DriverConfigMap, "apps", "cm", 2, common.StatusDeployed, "0.2.0",
		map[string]any{"adminPassword": "hunter2", "replicas": 2}).Object, "data", "release") // as cmSeed put it
	if len(payload) < 40 {
		t.Fatalf("payload %q", payload)
	}
	marker := payload[:40]
	for _, origin := range []engine.Origin{engine.OriginExternalAgent, engine.OriginGUI} {
		res, err := h.dispatch(origin, "object.get", map[string]any{"ref": ref})
		if err != nil {
			t.Fatal(err)
		}
		m := res.(*engine.DetailModel)
		b, _ := json.Marshal(m)
		if strings.Contains(string(b), marker) || !strings.Contains(m.YAML, "release: <redacted>") ||
			!strings.Contains(sectionText(m, "Data"), "Helm release record") {
			t.Errorf("%s: object_get %s", origin, b)
		}
	}
	res, err := h.dispatch(engine.OriginExternalAgent, "resources.list", map[string]any{"kind": "configmaps"})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(res); strings.Contains(string(b), marker) || !strings.Contains(string(b), `"Keys":"release"`) {
		t.Errorf("resources.list %s", b)
	}
	if _, err := h.dispatch(engine.OriginExternalAgent, "object.edit", map[string]any{"ref": ref}); !errors.Is(err, engine.ErrSecretGUIOnly) {
		t.Errorf("an agent's Edit tab: %v", err)
	}
	res, err = h.dispatch(engine.OriginGUI, "object.edit", map[string]any{"ref": ref})
	if err != nil {
		t.Fatalf("the user's Edit tab: %v", err)
	}
	tab := res.(commands.EditResult)
	if !strings.Contains(tab.Text, marker) {
		t.Errorf("the user's tab lacks the record: %q", tab.Text)
	}
	got, err := h.dispatch(engine.OriginExternalAgent, "editor.get", map[string]any{"id": tab.Tab.ID})
	if err != nil {
		t.Fatal(err)
	}
	if st := got.(engine.EditorState); st.Text != model.Withheld {
		t.Errorf("an agent reads the user's tab: %q", st.Text)
	}
	text := tab.Text
	if _, err := h.dispatch(engine.OriginExternalAgent, "editor.setText", map[string]any{"id": tab.Tab.ID, "yaml": text}); !errors.Is(err, engine.ErrSecretGUIOnly) {
		t.Errorf("an agent's setText: %v", err)
	}
	if _, err := h.dispatch(engine.OriginExternalAgent, "object.create", map[string]any{"yaml": text}); err == nil {
		t.Error("an agent creates a release ConfigMap")
	}
	h.audit.mu.Lock()
	audit := string(h.audit.buf)
	h.audit.mu.Unlock()
	if strings.Contains(audit, marker) {
		t.Errorf("the audit log holds the payload: %s", audit)
	}
	if out, changed := engine.RedactManifests(text, true); !changed || strings.Contains(out, marker) || !strings.Contains(out, "release: <redacted>") {
		t.Errorf("RedactManifests %v %q", changed, out)
	}
	list := "apiVersion: v1\nkind: ConfigMapList\nitems:\n- metadata:\n    name: r\n    labels: {owner: helm}\n  data:\n    release: " + marker + "\n"
	if out, changed := engine.RedactManifests(list, true); !changed || strings.Contains(out, marker) {
		t.Errorf("a ConfigMapList: %v %q", changed, out)
	}
}
