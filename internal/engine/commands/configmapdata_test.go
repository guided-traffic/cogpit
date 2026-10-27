package commands_test

import (
	"context"
	"encoding/base64"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// A Config Map's values edited in the drawer (F3): read whole from the cache, reviewed (a diff and
// a dry run), saved with the resourceVersion they are based on as precondition, never over a newer
// version unasked, and an agent's save waits for the user (D-2).

var (
	cmApp    = model.ObjectRef{Kind: "configmaps", Namespace: "shop", Name: "cilium-config"}
	cmFrozen = model.ObjectRef{Kind: "configmaps", Namespace: "shop", Name: "frozen"}
	cmHelm   = model.ObjectRef{Kind: "configmaps", Namespace: "shop", Name: "sh.helm.release.v1.web.v1"}
)

// bigValue is longer than the drawer model shows of one value (4 KiB): the editor gets it whole.
var bigValue = strings.Repeat("0123456789abcdef\n", 300)

// configMapConnector connects "test" with the Config Maps profile and a writer that acts on the
// source (serveObjects).
type configMapConnector struct {
	src    *testkit.MemorySource
	writer *fakeWriter
	demo   bool // the demo cluster: no writer
}

func (c configMapConnector) Contexts() ([]engine.ContextInfo, error) {
	return []engine.ContextInfo{{Name: "test", Current: true}}, nil
}

func (c configMapConnector) Connect(context.Context, string) (*engine.Backend, error) {
	ch := cache.New(c.src, cache.Options{WarmPeriod: time.Minute})
	cms, _ := kinds.ByKey("configmaps")
	b := &engine.Backend{Context: "test", Namespace: "default", Cache: ch, Close: ch.Close, Demo: c.demo,
		Profiles: append([]*model.KindProfile{cms}, testProfiles()...)}
	if !c.demo {
		b.Conn, b.Actions = &cluster.Conn{}, c.writer
	}
	return b, nil
}

// configMapHarness is a harness with three Config Maps: cilium-config with long keys, a value
// with a final newline and one beyond the drawer's cut, and binary data; an immutable one; a Helm
// release record.
func configMapHarness(t *testing.T, opts engine.Options) (*harness, *objectServer) {
	t.Helper()
	src := seed()
	src.Set(configMapsGVR, testkit.Object("v1", "ConfigMap", "shop", "cilium-config", map[string]any{
		"data": map[string]any{
			"agent-not-ready-taint-key": "node.cilium.io/agent-not-ready",
			"bpf-lb-acceleration":       "disabled",
			"enable-ipv4":               "true",
			"config.yaml":               "a: 1\nb: 2\n",
			"huge":                      bigValue,
		},
		"binaryData": map[string]any{"logo.png": base64.StdEncoding.EncodeToString([]byte{0x89, 'P', 'N', 'G'})},
	}))
	src.Set(configMapsGVR, testkit.Object("v1", "ConfigMap", "shop", "frozen", map[string]any{
		"immutable": true, "data": map[string]any{"a": "1"},
	}))
	src.Set(configMapsGVR, testkit.Object("v1", "ConfigMap", "shop", "sh.helm.release.v1.web.v1", map[string]any{
		"metadata": map[string]any{"labels": map[string]any{"owner": "helm", "name": "web"}},
		"data":     map[string]any{"release": "H4sIAAAA-not-a-release"},
	}))
	h := &harness{src: src, writer: &fakeWriter{}}
	opts.Connector = configMapConnector{src: src, writer: h.writer}
	if opts.SnapshotInterval == 0 {
		opts.SnapshotInterval = 10 * time.Millisecond
	}
	h.s = engine.New(opts)
	if err := commands.RegisterAll(h.s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.s.Close)
	if err := h.s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	srv := serveObjects(h)
	if _, err := h.s.Objects(context.Background(), "configmaps", nil); err != nil {
		t.Fatal(err)
	}
	return h, srv
}

func valueOf(d engine.ConfigMapData, key string) (engine.ConfigMapValue, bool) {
	i := slices.IndexFunc(d.Values, func(v engine.ConfigMapValue) bool { return v.Key == key })
	if i < 0 {
		return engine.ConfigMapValue{}, false
	}
	return d.Values[i], true
}

// TestConfigMapDataSave: the GUI reads the values whole (a final newline kept, a value beyond the
// drawer's cut whole), reviews a change of two keys (only their lines, the server's dry run), and
// saves it: a dry run and an update carrying the version the values are based on, nothing else of
// the object changed.
func TestConfigMapDataSave(t *testing.T) {
	h, srv := configMapHarness(t, engine.Options{})
	gui := engine.OriginGUI
	var toasts []string
	h.s.Subscribe(func(ev engine.Event) {
		if ev.Type == engine.EventToast {
			toasts = append(toasts, ev.Message)
		}
	})

	var data engine.ConfigMapData
	h.must(t, gui, "configMap.readData", map[string]any{"ref": cmApp}, &data)
	if data.ResourceVersion == "" || data.ReadOnly != "" || data.More != 0 || len(data.Values) != 5 {
		t.Fatalf("data %+v", data)
	}
	if v, _ := valueOf(data, "config.yaml"); v.Value != "a: 1\nb: 2\n" {
		t.Errorf("config.yaml read as %q: the final newline must stay", v.Value)
	}
	if v, _ := valueOf(data, "huge"); v.Value != bigValue {
		t.Errorf("a value beyond the drawer's cut read as %d bytes, want %d", len(v.Value), len(bigValue))
	}
	if _, ok := valueOf(data, "logo.png"); ok || data.Values[0].Key != "agent-not-ready-taint-key" {
		t.Errorf("binary data listed, or keys not sorted: %+v", data.Values)
	}

	values := map[string]any{"agent-not-ready-taint-key": "node.cilium.io/not-ready", "enable-ipv4": "false", "bpf-lb-acceleration": "disabled"}
	params := map[string]any{"ref": cmApp, "resourceVersion": data.ResourceVersion, "data": values}
	var review engine.ConfigMapDataReview
	h.must(t, gui, "configMap.reviewData", params, &review)
	if !review.Changed || review.Stale || !review.Accepted || review.Refused != "" ||
		!slices.Equal(review.Keys, []string{"agent-not-ready-taint-key", "enable-ipv4"}) || review.Added != 2 || review.Removed != 2 {
		t.Fatalf("review %+v", review)
	}
	for _, l := range review.Lines {
		if l.Op != engine.DiffSame && !strings.Contains(l.Text, "agent-not-ready-taint-key") && !strings.Contains(l.Text, "enable-ipv4") {
			t.Errorf("the diff changes another line: %+v", l)
		}
	}
	if got := h.writer.Calls(); !slices.Equal(got, []string{"update(dry) shop/cilium-config"}) {
		t.Fatalf("writes of the review: %v", got)
	}

	var saved engine.ConfigMapDataSaved
	h.must(t, gui, "configMap.saveData", params, &saved)
	if got := h.writer.Calls(); !slices.Equal(got, []string{"update(dry) shop/cilium-config", "update(dry) shop/cilium-config", "update shop/cilium-config"}) {
		t.Errorf("writes of the save: %v", got)
	}
	sent := srv.lastUpdate()
	if sent.GetResourceVersion() != data.ResourceVersion || string(sent.GetUID()) != "uid-shop-cilium-config" {
		t.Errorf("the update carried resourceVersion %q uid %q, want the base %q", sent.GetResourceVersion(), sent.GetUID(), data.ResourceVersion)
	}
	got, _, _ := unstructured.NestedStringMap(sent.Object, "data")
	if got["agent-not-ready-taint-key"] != "node.cilium.io/not-ready" || got["enable-ipv4"] != "false" ||
		got["config.yaml"] != "a: 1\nb: 2\n" || got["huge"] != bigValue || len(got) != 5 {
		t.Errorf("the update's data %v", got)
	}
	if bin, _, _ := unstructured.NestedStringMap(sent.Object, "binaryData"); len(bin) != 1 {
		t.Errorf("the binary data was not kept: %v", bin)
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(sent.Object, "metadata", "managedFields"); found {
		t.Error("the update carried managedFields")
	}
	if saved.ResourceVersion == "" || saved.ResourceVersion == data.ResourceVersion || !slices.Equal(saved.Keys, review.Keys) {
		t.Errorf("save result %+v", saved)
	}
	if len(toasts) != 1 || toasts[0] != "ConfigMap shop/cilium-config saved" {
		t.Errorf("toasts %q", toasts)
	}

	// Saving the same values again from the stored version changes nothing and writes nothing.
	cachedRV(t, h, cmApp, data.ResourceVersion)
	params["resourceVersion"] = saved.ResourceVersion
	if err := h.run(t, gui, "configMap.saveData", params, nil); err == nil || !strings.Contains(err.Error(), "no value differs") {
		t.Errorf("a save without a change: %v", err)
	}
	if n := len(h.writer.Calls()); n != 3 {
		t.Errorf("a save without a change wrote: %v", h.writer.Calls())
	}
}

// TestConfigMapDataNeverOverwritesUnasked: another client changes the Config Map after the version
// the values are based on. The review shows the values against the newer version as stale and
// runs no dry run; the save writes nothing and reports a typed conflict with that version; a save
// naming the newer version writes the values over it and keeps the other client's change.
func TestConfigMapDataNeverOverwritesUnasked(t *testing.T) {
	h, srv := configMapHarness(t, engine.Options{})
	gui := engine.OriginGUI
	var data engine.ConfigMapData
	h.must(t, gui, "configMap.readData", map[string]any{"ref": cmApp}, &data)

	other, err := srv.current(configMapsGVR, "shop", "cilium-config")
	if err != nil {
		t.Fatal(err)
	}
	if err := unstructured.SetNestedField(other.Object, "enabled", "data", "bpf-lb-acceleration"); err != nil {
		t.Fatal(err)
	}
	h.src.Set(configMapsGVR, other)
	newer := cachedRV(t, h, cmApp, data.ResourceVersion)

	params := map[string]any{"ref": cmApp, "resourceVersion": data.ResourceVersion, "data": map[string]any{"enable-ipv4": "false"}}
	var review engine.ConfigMapDataReview
	h.must(t, gui, "configMap.reviewData", params, &review)
	if !review.Stale || review.Accepted || review.CachedResourceVersion != newer || !review.Changed || review.Added != 1 {
		t.Fatalf("review over a newer version %+v", review)
	}
	err = h.run(t, gui, "configMap.saveData", params, nil)
	var ce *engine.DataConflictError
	if !errors.As(err, &ce) || ce.CurrentResourceVersion != newer || ce.BaseResourceVersion != data.ResourceVersion || len(ce.Diff) == 0 {
		t.Fatalf("save over a newer version: %v (%+v)", err, ce)
	}
	if !strings.Contains(err.Error(), "nothing was saved") {
		t.Errorf("the conflict does not say nothing was saved: %v", err)
	}
	if calls := h.writer.Calls(); len(calls) != 0 {
		t.Fatalf("a stale save wrote: %v", calls)
	}

	// The server's 409 for a change the cache had not seen when the save began: nothing written
	// either, and the conflict names the version the cache then gets.
	params["resourceVersion"] = newer
	srv.mu.Lock()
	srv.failNext = conflictErr()
	srv.afterFail = func() {
		obj, err := srv.current(configMapsGVR, "shop", "cilium-config")
		if err == nil {
			err = unstructured.SetNestedField(obj.Object, "true", "data", "enable-bpf-masquerade")
		}
		if err != nil {
			t.Error(err)
			return
		}
		h.src.Set(configMapsGVR, obj)
	}
	srv.mu.Unlock()
	if err := h.run(t, gui, "configMap.saveData", params, nil); !errors.As(err, &ce) || ce.CurrentResourceVersion == "" || ce.CurrentResourceVersion == newer {
		t.Fatalf("a save the server refuses with 409: %v (%+v)", err, ce)
	}
	if calls := h.writer.Calls(); !slices.Equal(calls, []string{"update(dry) shop/cilium-config"}) {
		t.Fatalf("a save the server refused wrote: %v", calls)
	}

	// The second, explicit save over the version the conflict names.
	params["resourceVersion"] = ce.CurrentResourceVersion
	var saved engine.ConfigMapDataSaved
	h.must(t, gui, "configMap.saveData", params, &saved)
	got, _, _ := unstructured.NestedStringMap(srv.lastUpdate().Object, "data")
	if got["enable-ipv4"] != "false" || got["bpf-lb-acceleration"] != "enabled" || got["enable-bpf-masquerade"] != "true" {
		t.Errorf("the save over the newer version: %v", got)
	}
}

// TestConfigMapDataRebases (the owner's answer of 2026-10-15): another client changes a key the
// user did not edit. A review given the edited keys' values at the base version rebases the values
// onto the newer version: the diff and the dry run against it, the newer version to save with,
// the other client's change kept. A change of an edited key is the conflict as before; so is a
// review without from. A change that reaches the server while the review runs ends rebased too.
func TestConfigMapDataRebases(t *testing.T) {
	h, srv := configMapHarness(t, engine.Options{})
	gui := engine.OriginGUI
	var data engine.ConfigMapData
	h.must(t, gui, "configMap.readData", map[string]any{"ref": cmApp}, &data)
	base := data.ResourceVersion
	change := func(key, value string) {
		t.Helper()
		obj, err := srv.current(configMapsGVR, "shop", "cilium-config")
		if err != nil {
			t.Fatal(err)
		}
		if err := unstructured.SetNestedField(obj.Object, value, "data", key); err != nil {
			t.Fatal(err)
		}
		h.src.Set(configMapsGVR, obj)
	}
	change("bpf-lb-acceleration", "enabled")
	newer := cachedRV(t, h, cmApp, base)

	edits := map[string]any{"enable-ipv4": "false"}
	params := map[string]any{"ref": cmApp, "resourceVersion": base, "data": edits, "from": map[string]any{"enable-ipv4": "true"}}
	var review engine.ConfigMapDataReview
	h.must(t, gui, "configMap.reviewData", params, &review)
	if review.RebasedFrom != base || review.BaseResourceVersion != newer || review.Stale || !review.Accepted || !review.Changed {
		t.Fatalf("a review over a change of another key: %+v", review)
	}
	for _, l := range review.Lines {
		if l.Op != engine.DiffSame && !strings.Contains(l.Text, "enable-ipv4") {
			t.Errorf("the rebased diff changes another line: %+v", l)
		}
	}
	if got := h.writer.Calls(); !slices.Equal(got, []string{"update(dry) shop/cilium-config"}) || srv.lastUpdate().GetResourceVersion() != newer {
		t.Fatalf("the rebased dry run: %v at version %s", got, srv.lastUpdate().GetResourceVersion())
	}
	var saved engine.ConfigMapDataSaved
	h.must(t, gui, "configMap.saveData", map[string]any{"ref": cmApp, "resourceVersion": review.BaseResourceVersion, "data": edits}, &saved)
	got, _, _ := unstructured.NestedStringMap(srv.lastUpdate().Object, "data")
	if got["enable-ipv4"] != "false" || got["bpf-lb-acceleration"] != "enabled" {
		t.Errorf("the rebased save: %v", got)
	}

	// An edited key changed on the cluster: no rebase, the values against the newer version.
	cachedRV(t, h, cmApp, newer)
	change("enable-ipv4", "maybe")
	newest := cachedRV(t, h, cmApp, saved.ResourceVersion)
	params = map[string]any{"ref": cmApp, "resourceVersion": saved.ResourceVersion, "data": map[string]any{"enable-ipv4": "true"},
		"from": map[string]any{"enable-ipv4": "false"}}
	review = engine.ConfigMapDataReview{}
	h.must(t, gui, "configMap.reviewData", params, &review)
	if review.RebasedFrom != "" || !review.Stale || review.CachedResourceVersion != newest || review.Accepted {
		t.Errorf("a review over a change of the edited key: %+v", review)
	}
	delete(params, "from")
	params["data"], params["resourceVersion"] = map[string]any{"bpf-lb-acceleration": "disabled"}, saved.ResourceVersion
	review = engine.ConfigMapDataReview{}
	h.must(t, gui, "configMap.reviewData", params, &review)
	if review.RebasedFrom != "" || !review.Stale {
		t.Errorf("a review without from over a newer version: %+v", review)
	}

	// A change of another key that the server holds before the cache does: the dry run meets it,
	// and the review ends rebased onto it.
	change("agent-not-ready-taint-key", "node.cilium.io/other")
	params = map[string]any{"ref": cmApp, "resourceVersion": newest, "data": map[string]any{"enable-ipv4": "true"},
		"from": map[string]any{"enable-ipv4": "maybe"}}
	review = engine.ConfigMapDataReview{}
	h.must(t, gui, "configMap.reviewData", params, &review)
	if review.RebasedFrom != newest || review.BaseResourceVersion == newest || review.Stale || !review.Accepted {
		t.Errorf("a review that meets a newer version on the server: %+v", review)
	}
}

// TestConfigMapDataRefusals: values only, of keys the data holds; no immutable Config Map, no
// Helm release record (whose payload is never read out), no binary data; a resourceVersion is
// required.
func TestConfigMapDataRefusals(t *testing.T) {
	h, _ := configMapHarness(t, engine.Options{})
	gui := engine.OriginGUI
	var data engine.ConfigMapData
	h.must(t, gui, "configMap.readData", map[string]any{"ref": cmApp}, &data)
	rv := data.ResourceVersion
	for _, c := range []struct {
		name string
		ref  model.ObjectRef
		rv   string
		data map[string]any
		want string
	}{
		{"a new key", cmApp, rv, map[string]any{"new-key": "x"}, "has no key"},
		{"binary data", cmApp, rv, map[string]any{"logo.png": "x"}, "binary data"},
		{"no version", cmApp, "", map[string]any{"enable-ipv4": "false"}, "resourceVersion"},
		{"no values", cmApp, rv, map[string]any{}, "give data"},
		{"immutable", cmFrozen, rv, map[string]any{"a": "2"}, "immutable"},
		{"a Helm release record", cmHelm, rv, map[string]any{"release": "x"}, "Helm release record"},
		{"not a Config Map", checkout, rv, map[string]any{"a": "2"}, "applies to configmaps"},
	} {
		err := h.run(t, gui, "configMap.saveData", map[string]any{"ref": c.ref, "resourceVersion": c.rv, "data": c.data}, nil)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
	if calls := h.writer.Calls(); len(calls) != 0 {
		t.Errorf("a refused save wrote: %v", calls)
	}

	var helmData engine.ConfigMapData
	h.must(t, gui, "configMap.readData", map[string]any{"ref": cmHelm}, &helmData)
	if v, _ := valueOf(helmData, "release"); v.Value != "" || !strings.Contains(v.Note, "not shown") || !strings.Contains(helmData.ReadOnly, "Helm") {
		t.Errorf("a Helm release record read as %+v", helmData)
	}
	var frozen engine.ConfigMapData
	h.must(t, gui, "configMap.readData", map[string]any{"ref": cmFrozen}, &frozen)
	if !strings.Contains(frozen.ReadOnly, "immutable") {
		t.Errorf("an immutable Config Map read as %+v", frozen)
	}
}

// TestConfigMapDataAgents (D-2): an agent's save waits for the user, who reads the diff it writes;
// declined, nothing is written; accepted, it is, and the audit log has it. An agent's value holding
// a placeholder Cogpit showed in place of a Secret's value is refused before anyone is asked.
// Allowed agent writes for the context, the save runs without asking.
func TestConfigMapDataAgents(t *testing.T) {
	audit := &syncBuffer{}
	h, srv := configMapHarness(t, engine.Options{Audit: audit, ClusterSettings: engine.NewClusterSettingsStore(t.TempDir())})
	c := &confirmations{answer: false}
	h.s.SetConfirmer(c)
	agent := engine.OriginExternalAgent
	var data engine.ConfigMapData
	h.must(t, agent, "configMap.readData", map[string]any{"ref": cmApp}, &data)
	params := map[string]any{"ref": cmApp, "resourceVersion": data.ResourceVersion, "data": map[string]any{"enable-ipv4": "false"}}

	if err := h.run(t, agent, "configMap.saveData", params, nil); !errors.Is(err, engine.ErrDeclined) {
		t.Fatalf("a declined save: %v", err)
	}
	req := c.last(t)
	if req.Command != "configMap.saveData" || req.Danger != engine.DangerMutating || len(req.Objects) != 1 || req.Objects[0].Name != "cilium-config" {
		t.Errorf("confirmation %+v", req)
	}
	if !strings.Contains(req.Review, "-   enable-ipv4: \"true\"") || !strings.Contains(req.Review, "+   enable-ipv4: \"false\"") {
		t.Errorf("the confirmation's review does not show the change:\n%s", req.Review)
	}
	if calls := h.writer.Calls(); len(calls) != 0 {
		t.Fatalf("a declined save wrote: %v", calls)
	}

	marked := map[string]any{"ref": cmApp, "resourceVersion": data.ResourceVersion, "data": map[string]any{"enable-ipv4": "«secret shop/db/password»"}}
	asked := len(c.all())
	if err := h.run(t, agent, "configMap.saveData", marked, nil); !errors.Is(err, engine.ErrAgentMarkerWrite) {
		t.Errorf("an agent's placeholder: %v", err)
	}
	if len(c.all()) != asked {
		t.Error("a refused placeholder asked the user first")
	}

	c.mu.Lock()
	c.answer = true
	c.mu.Unlock()
	h.must(t, agent, "configMap.saveData", params, nil)
	if got, _, _ := unstructured.NestedString(srv.lastUpdate().Object, "data", "enable-ipv4"); got != "false" {
		t.Errorf("the accepted save wrote %q", got)
	}
	done := false
	for _, l := range audit.Lines() {
		done = done || (l.Command == "configMap.saveData" && l.Phase == "done" && l.OK)
	}
	if !done {
		t.Errorf("the audit log has no completed save: %+v", audit.Lines())
	}

	// Agent writes allowed for the context: the next save does not ask.
	h.must(t, engine.OriginGUI, "cluster.settings.setAgentWrites", map[string]any{"allowed": true}, nil)
	cachedRV(t, h, cmApp, data.ResourceVersion)
	var now engine.ConfigMapData
	h.must(t, agent, "configMap.readData", map[string]any{"ref": cmApp}, &now)
	asked = len(c.all())
	h.must(t, agent, "configMap.saveData", map[string]any{"ref": cmApp, "resourceVersion": now.ResourceVersion, "data": map[string]any{"enable-ipv4": "true"}}, nil)
	if len(c.all()) != asked {
		t.Error("an allowed agent write asked the user")
	}
}

// TestConfigMapDataDisabledOnTheDemo: the demo cluster cannot be changed; its values read, the
// save is disabled with the reason the drawer shows.
func TestConfigMapDataDisabledOnTheDemo(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(configMapsGVR, testkit.Object("v1", "ConfigMap", "shop", "app", map[string]any{"data": map[string]any{"a": "1"}}))
	h := &harness{src: src, writer: &fakeWriter{}}
	h.s = engine.New(engine.Options{Connector: configMapConnector{src: src, demo: true}, SnapshotInterval: 10 * time.Millisecond})
	if err := commands.RegisterAll(h.s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.s.Close)
	if err := h.s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	ref := model.ObjectRef{Kind: "configmaps", Namespace: "shop", Name: "app"}
	if _, err := h.s.Objects(context.Background(), "configmaps", nil); err != nil {
		t.Fatal(err)
	}
	var data engine.ConfigMapData
	h.must(t, engine.OriginGUI, "configMap.readData", map[string]any{"ref": ref}, &data)
	err := h.run(t, engine.OriginGUI, "configMap.saveData", map[string]any{"ref": ref, "resourceVersion": data.ResourceVersion, "data": map[string]any{"a": "2"}}, nil)
	var dis *engine.DisabledError
	if !errors.As(err, &dis) || !strings.Contains(dis.Reason, "demo") {
		t.Errorf("a save on the demo cluster: %v", err)
	}
}

func conflictErr() error {
	return apierrors.NewConflict(configMapsGVR.GroupResource(), "cilium-config", errors.New("the object has been modified"))
}

// TestConfigMapDataAgentsReadNoSecret (H-82): a Config Map value that holds a cached Secret's value
// reads with the placeholder for agents, in the values and in a review's diff; the window reads it.
func TestConfigMapDataAgentsReadNoSecret(t *testing.T) {
	h := newAgentViewHarness(t)
	const placeholder = "«secret shop/db/password»"
	var gui, agent engine.ConfigMapData
	h.must(t, engine.OriginGUI, "configMap.readData", map[string]any{"ref": appCMRef()}, &gui)
	h.must(t, engine.OriginExternalAgent, "configMap.readData", map[string]any{"ref": appCMRef()}, &agent)
	if v, _ := valueOf(gui, "dsn"); !strings.Contains(v.Value, dbPass) {
		t.Errorf("the window's read: %+v", v)
	}
	if v, _ := valueOf(agent, "dsn"); strings.Contains(v.Value, dbPass) || !strings.Contains(v.Value, placeholder) {
		t.Errorf("an agent's read: %+v", v)
	}
	var review engine.ConfigMapDataReview
	h.must(t, engine.OriginExternalAgent, "configMap.reviewData", map[string]any{"ref": appCMRef(),
		"resourceVersion": agent.ResourceVersion, "data": map[string]any{"user": "app"}, "context": -1}, &review)
	if !review.Changed || len(review.Lines) < 3 {
		t.Fatalf("review %+v", review)
	}
	for _, l := range review.Lines {
		if strings.Contains(l.Text, dbPass) {
			t.Errorf("an agent's review shows the value: %+v", l)
		}
	}
}
