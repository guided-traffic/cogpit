package commands_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// argoWrite is one write the fake Argo CD writer received.
type argoWrite struct {
	op, version, namespace, name, uid, rv string
	hard                                  bool
	operation                             map[string]any
}

// argoWriter records Argo CD's writes; conflicts makes that many Syncs fail as changed first.
type argoWriter struct {
	mu        sync.Mutex
	writes    []argoWrite
	conflicts int
	onSync    func() // runs before a conflict is returned (the change that caused it)
}

func (w *argoWriter) RestartDeployment(context.Context, string, string, string) error { return nil }
func (w *argoWriter) DeletePod(context.Context, string, string, string) error         { return nil }

func (w *argoWriter) RefreshArgoApplication(_ context.Context, version, namespace, name, uid string, hard bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes = append(w.writes, argoWrite{op: "refresh", version: version, namespace: namespace, name: name, uid: uid, hard: hard})
	return nil
}

func (w *argoWriter) SyncArgoApplication(_ context.Context, version, namespace, name, uid, rv string, operation map[string]any) error {
	w.mu.Lock()
	w.writes = append(w.writes, argoWrite{op: "sync", version: version, namespace: namespace, name: name, uid: uid, rv: rv, operation: operation})
	conflict := w.conflicts > 0
	if conflict {
		w.conflicts--
	}
	hook := w.onSync
	w.mu.Unlock()
	if conflict {
		if hook != nil {
			hook()
		}
		return fmt.Errorf("sync: %w", cluster.ErrArgoChanged)
	}
	return nil
}

func (w *argoWriter) Writes() []argoWrite {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]argoWrite(nil), w.writes...)
}

// argoConnector connects "test" with the real Secrets profile and the custom kinds watch, so that
// Argo CD's kinds come from their definitions as on a real cluster.
type argoConnector struct {
	src    *testkit.MemorySource
	writer *argoWriter
}

func (c *argoConnector) Contexts() ([]engine.ContextInfo, error) {
	return []engine.ContextInfo{{Name: "test", Current: true}}, nil
}

func (c *argoConnector) Connect(_ context.Context, name string) (*engine.Backend, error) {
	ch := cache.New(c.src, cache.Options{WarmPeriod: time.Minute})
	p, _ := kinds.ByKey("secrets")
	return &engine.Backend{
		Context: name, Namespace: "default", Cache: ch, Close: ch.Close, CustomResources: true,
		Profiles: append(append([]*model.KindProfile{p}, testProfiles()...), kinds.Local()...),
		Conn:     &cluster.Conn{}, Actions: c.writer,
	}, nil
}

// DialPortForward makes the writer a port forwarder that never reaches a pod.
func (w *argoWriter) DialPortForward(context.Context, string, string) (cluster.PortTunnel, error) {
	return nil, errors.New("no pods in this test")
}

// TestArgoOpenUIFindsTheServer: Open Argo CD UI forwards the https port of the Service labelled
// app.kubernetes.io/name=argocd-server, found in the cache; an agent's request names it in the
// confirmation; without such a Service it says so.
func TestArgoOpenUIFindsTheServer(t *testing.T) {
	h := newArgoHarness(t)
	h.answer = false
	if _, err := h.dispatch(engine.OriginExternalAgent, "argocd.openUI", map[string]any{}); err == nil ||
		!strings.Contains(err.Error(), "app.kubernetes.io/name=argocd-server") {
		t.Fatalf("without the Service: %v", err)
	}
	h.src.Set(testkit.ServicesGVR, testkit.Object("v1", "Service", "argocd", "argocd-server", map[string]any{
		"metadata": map[string]any{"labels": map[string]any{"app.kubernetes.io/name": "argocd-server"}},
		"spec": map[string]any{"selector": map[string]any{"app": "argocd-server"}, "ports": []any{
			map[string]any{"name": "http", "port": int64(80)}, map[string]any{"name": "https", "port": int64(443)},
		}},
	}))
	waitUntil(t, "the Service in the cache", func() bool {
		_, err := h.s.Lookup(model.ObjectRef{Context: "test", Kind: "services", Namespace: "argocd", Name: "argocd-server"})
		return err == nil
	})
	if _, err := h.dispatch(engine.OriginExternalAgent, "argocd.openUI", map[string]any{}); !errors.Is(err, engine.ErrDeclined) {
		t.Fatalf("an agent's request: %v", err)
	}
	asked := h.confirmations()
	if len(asked) == 0 || asked[len(asked)-1].Objects[0].Name != "argocd-server" || asked[len(asked)-1].Danger != engine.DangerMutating {
		t.Fatalf("confirmations %+v", asked)
	}
}

// argoHarness is a session on the Argo CD connector with an audit log, a log, every event and
// every confirmation it was asked for (answered by answer).
type argoHarness struct {
	s      *engine.Session
	src    *testkit.MemorySource
	writer *argoWriter
	audit  *syncBuffer
	logs   *bytes.Buffer
	mu     sync.Mutex
	events []engine.Event
	asked  []engine.ConfirmRequest
	answer bool
}

func newArgoHarness(t *testing.T) *argoHarness {
	t.Helper()
	src := testkit.NewMemorySource()
	for _, crd := range testkit.ArgoCRDs() {
		src.Set(testkit.CRDsGVR, crd)
	}
	h := &argoHarness{src: src, writer: &argoWriter{}, audit: &syncBuffer{}, logs: &bytes.Buffer{}, answer: true}
	var logMu sync.Mutex
	h.s = engine.New(engine.Options{
		Connector: &argoConnector{src: src, writer: h.writer}, SnapshotInterval: 10 * time.Millisecond, Audit: h.audit,
		Logger: slog.New(slog.NewTextHandler(lockedWriter{&logMu, h.logs}, &slog.HandlerOptions{Level: slog.LevelDebug})),
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
	waitUntil(t, "Argo CD's kinds", func() bool {
		b := h.s.Backend()
		_, ok := b.Profile(kinds.ArgoClustersKey)
		return ok
	})
	return h
}

func (h *argoHarness) dispatch(origin engine.Origin, id string, params any) (any, error) {
	raw, _ := json.Marshal(params)
	return h.s.Dispatch(context.Background(), engine.Invocation{ID: id, Params: raw, Origin: origin})
}

func (h *argoHarness) confirmations() []engine.ConfirmRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]engine.ConfirmRequest(nil), h.asked...)
}

// trail is everything the session wrote outside the window: the audit log, the events, the log.
func (h *argoHarness) trail() string {
	var out bytes.Buffer
	for _, e := range h.audit.Lines() {
		b, _ := json.Marshal(e)
		out.Write(b)
	}
	h.mu.Lock()
	for _, ev := range h.events {
		b, _ := json.Marshal(ev)
		out.Write(b)
	}
	h.mu.Unlock()
	out.WriteString(h.logs.String())
	return out.String()
}

// app adds an Application and waits for the cache to hold it.
func (h *argoHarness) app(t *testing.T, obj map[string]any, name string) model.ObjectRef {
	t.Helper()
	o := testkit.ArgoApplication("argocd", name, "OutOfSync", "Healthy")
	for k, v := range obj {
		o.Object[k] = v
	}
	h.src.Set(testkit.ArgoApplicationsGVR, o)
	ref := model.ObjectRef{Context: "test", Kind: kinds.ArgoApplicationsKey, Namespace: "argocd", Name: name}
	waitUntil(t, name+" in the cache", func() bool {
		_, err := h.s.Lookup(ref)
		return err == nil
	})
	return ref
}

// TestArgoSyncWritesTheOperation: Sync writes the operation argocd app sync would, bound to the
// UID and resourceVersion; with prune it is destructive and confirmed for the GUI too, naming the
// Application, its namespace and the context and what prune deletes; without, the GUI runs it at
// once and an agent's waits for the user's acceptance (D-2).
func TestArgoSyncWritesTheOperation(t *testing.T) {
	h := newArgoHarness(t)
	ref := h.app(t, map[string]any{"spec": map[string]any{
		"project": "default",
		"source":  map[string]any{"repoURL": "https://git.example.com/apps.git", "targetRevision": "main", "path": "shop"},
		"syncPolicy": map[string]any{
			"syncOptions": []any{"CreateNamespace=true"},
			"retry":       map[string]any{"limit": int64(3)},
		},
	}, "status": map[string]any{
		"sync": map[string]any{"status": "OutOfSync"},
		"resources": []any{
			map[string]any{"kind": "ConfigMap", "namespace": "shop", "name": "old-config", "requiresPruning": true},
			map[string]any{"kind": "Deployment", "namespace": "shop", "name": "web"},
		},
	}}, "shop")
	cached, _ := h.s.Lookup(ref)

	if _, err := h.dispatch(engine.OriginGUI, "argocd.application.sync", map[string]any{"ref": ref}); err != nil {
		t.Fatal(err)
	}
	if n := len(h.confirmations()); n != 0 {
		t.Fatalf("a sync without prune from the GUI asked %d times", n)
	}
	w := h.writer.Writes()
	if len(w) != 1 || w[0].op != "sync" || w[0].version != "v1alpha1" || w[0].uid != string(cached.GetUID()) || w[0].rv != cached.GetResourceVersion() {
		t.Fatalf("writes %+v", w)
	}
	op, _ := json.Marshal(w[0].operation)
	want := `{"initiatedBy":{"username":"cogpit"},"retry":{"limit":3},"sync":{"prune":false,"revision":"main",` +
		`"source":{"path":"shop","repoURL":"https://git.example.com/apps.git","targetRevision":"main"},"syncOptions":["CreateNamespace=true"]}}`
	if string(op) != want {
		t.Errorf("operation %s, want %s", op, want)
	}

	// With prune: destructive, confirmed for the GUI, its text saying what prune deletes.
	if _, err := h.dispatch(engine.OriginGUI, "argocd.application.sync", map[string]any{"ref": ref, "prune": true}); err != nil {
		t.Fatal(err)
	}
	asked := h.confirmations()
	if len(asked) != 1 || asked[0].Danger != engine.DangerDestructive || asked[0].Context != "test" ||
		len(asked[0].Objects) != 1 || asked[0].Objects[0].Name != "shop" || asked[0].Objects[0].Namespace != "argocd" ||
		!strings.Contains(asked[0].Detail, "no longer in Git are deleted") || !strings.Contains(asked[0].Detail, "Syncs to: main.") ||
		!strings.Contains(asked[0].Detail, "To prune: ConfigMap → shop/old-config.") ||
		!slices.Contains(asked[0].Effects, engine.Effect{Name: "To prune", Value: "ConfigMap → shop/old-config", Tone: engine.EffectLost}) {
		t.Fatalf("confirmation %+v", asked)
	}
	if w := h.writer.Writes(); len(w) != 2 || w[1].operation["sync"].(map[string]any)["prune"] != true {
		t.Fatalf("writes %+v", w)
	}

	// An agent's sync without prune is mutating: it waits for the user's acceptance.
	h.answer = false
	if _, err := h.dispatch(engine.OriginExternalAgent, "argocd.application.sync", map[string]any{"ref": ref}); !errors.Is(err, engine.ErrDeclined) {
		t.Fatalf("an agent's declined sync: %v", err)
	}
	if asked := h.confirmations(); len(asked) != 2 || asked[1].Danger != engine.DangerMutating {
		t.Fatalf("confirmations %+v", asked)
	}
	if n := len(h.writer.Writes()); n != 2 {
		t.Errorf("a declined sync wrote: %d writes", n)
	}
}

// TestArgoSyncRefusedWhileAnOperationRuns: a requested or running operation greys Sync with the
// reason, and Dispatch refuses it for every origin; Refresh still runs.
func TestArgoSyncRefusedWhileAnOperationRuns(t *testing.T) {
	h := newArgoHarness(t)
	running := h.app(t, map[string]any{"status": map[string]any{
		"operationState": map[string]any{"phase": "Running", "startedAt": "2026-10-11T11:59:00Z"},
	}}, "running")
	requested := h.app(t, map[string]any{"operation": map[string]any{"sync": map[string]any{"revision": "main"}}}, "requested")
	for ref, reason := range map[model.ObjectRef]string{running: "phase Running", requested: "requested"} {
		cmd, _ := h.s.Registry().Get("argocd.application.sync")
		raw, _ := json.Marshal(map[string]any{"ref": ref})
		if err := cmd.Enabled(h.s, raw); err == nil || !strings.Contains(err.Error(), reason) {
			t.Errorf("%s: enabled %v", ref.Name, err)
		}
		if _, err := h.dispatch(engine.OriginGUI, "argocd.application.sync", map[string]any{"ref": ref}); err == nil {
			t.Errorf("%s: synced while an operation runs", ref.Name)
		}
		if _, err := h.dispatch(engine.OriginGUI, "argocd.application.refresh", map[string]any{"ref": ref}); err != nil {
			t.Errorf("%s: refresh %v", ref.Name, err)
		}
	}
	for _, w := range h.writer.Writes() {
		if w.op == "sync" {
			t.Errorf("a sync was written: %+v", w)
		}
	}
}

// TestArgoSyncChecksAgainAfterAChange: a sync refused because the Application changed checks the
// newer version and writes once more; when the newer version runs an operation, it stops.
func TestArgoSyncChecksAgainAfterAChange(t *testing.T) {
	h := newArgoHarness(t)
	ref := h.app(t, nil, "shop")
	h.writer.conflicts = 1
	h.writer.onSync = func() { // the controller updated the status meanwhile
		h.src.Set(testkit.ArgoApplicationsGVR, testkit.ArgoApplication("argocd", "shop", "Synced", "Healthy"))
	}
	if _, err := h.dispatch(engine.OriginGUI, "argocd.application.sync", map[string]any{"ref": ref}); err != nil {
		t.Fatal(err)
	}
	w := h.writer.Writes()
	now, _ := h.s.Lookup(ref)
	if len(w) != 2 || w[0].rv == w[1].rv || w[1].rv != now.GetResourceVersion() {
		t.Fatalf("writes %+v", w)
	}

	h.writer.conflicts = 1
	h.writer.onSync = func() { // someone started a sync meanwhile
		o := testkit.ArgoApplication("argocd", "shop", "Synced", "Healthy")
		o.Object["status"].(map[string]any)["operationState"] = map[string]any{"phase": "Running"}
		h.src.Set(testkit.ArgoApplicationsGVR, o)
	}
	_, err := h.dispatch(engine.OriginGUI, "argocd.application.sync", map[string]any{"ref": ref})
	if err == nil || !strings.Contains(err.Error(), "operation is in progress") {
		t.Fatalf("a sync over someone's running one: %v", err)
	}
	if w := h.writer.Writes(); len(w) != 3 {
		t.Errorf("writes %+v", w)
	}
}

// TestArgoRefreshNormalAndHard: Refresh is mutating (an agent's waits for the acceptance, the GUI
// runs it), normal or hard, bound to the UID.
func TestArgoRefreshNormalAndHard(t *testing.T) {
	h := newArgoHarness(t)
	ref := h.app(t, nil, "shop")
	cached, _ := h.s.Lookup(ref)
	if _, err := h.dispatch(engine.OriginGUI, "argocd.application.refresh", map[string]any{"ref": ref}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.dispatch(engine.OriginExternalAgent, "argocd.application.refresh", map[string]any{"ref": ref, "hard": true}); err != nil {
		t.Fatal(err)
	}
	w := h.writer.Writes()
	if len(w) != 2 || w[0].hard || !w[1].hard || w[0].uid != string(cached.GetUID()) || w[1].uid != string(cached.GetUID()) {
		t.Fatalf("writes %+v", w)
	}
	asked := h.confirmations()
	if len(asked) != 1 || asked[0].Danger != engine.DangerMutating || !strings.Contains(asked[0].Detail, "Refresh: hard:") {
		t.Fatalf("an agent's refresh asked %+v", asked)
	}
	// Other kinds: not applicable, so no menu offers them.
	cmd, _ := h.s.Registry().Get("argocd.application.refresh")
	raw, _ := json.Marshal(map[string]any{"ref": model.ObjectRef{Kind: "pods", Namespace: "x", Name: "y"}})
	if err := cmd.Enabled(h.s, raw); !errors.Is(err, engine.ErrNotApplicable) {
		t.Errorf("refresh on a pod: %v", err)
	}
}

// TestArgoBulkSyncAndRefresh: several Applications in one acceptance, each written bound to its
// UID; one with an operation running is reported and the others are done; one notification.
func TestArgoBulkSyncAndRefresh(t *testing.T) {
	h := newArgoHarness(t)
	a := h.app(t, nil, "a")
	b := h.app(t, nil, "b")
	busy := h.app(t, map[string]any{"status": map[string]any{"operationState": map[string]any{"phase": "Running"}}}, "busy")

	res, err := h.dispatch(engine.OriginGUI, "argocd.application.refreshMany", map[string]any{"refs": []model.ObjectRef{a, b}, "hard": true})
	if err != nil {
		t.Fatal(err)
	}
	if r := res.(commands.ArgoManyResult); len(r.Done) != 2 || r.Action != "hard refresh requested" {
		t.Errorf("refreshMany %+v", r)
	}

	_, err = h.dispatch(engine.OriginGUI, "argocd.application.syncMany", map[string]any{"refs": []model.ObjectRef{a, busy, b}, "prune": true})
	var many *commands.ArgoManyError
	if !errors.As(err, &many) || len(many.Done) != 2 || len(many.Failed) != 1 || many.Failed[0].Ref.Name != "busy" ||
		!strings.Contains(many.Failed[0].Error, "in progress") {
		t.Fatalf("syncMany: %v", err)
	}
	asked := h.confirmations()
	if len(asked) != 1 || asked[0].Danger != engine.DangerDestructive || len(asked[0].Objects) != 3 || len(asked[0].UIDs) != 3 {
		t.Fatalf("syncMany asked %+v", asked)
	}
	var syncs int
	for _, w := range h.writer.Writes() {
		if w.op == "sync" {
			syncs++
			if w.uid == "" || w.operation["sync"].(map[string]any)["prune"] != true {
				t.Errorf("sync %+v", w)
			}
		}
	}
	if syncs != 2 {
		t.Errorf("%d syncs written", syncs)
	}
	// The selection's menu offers both for Applications, Delete after them.
	var ids []string
	for _, c := range h.s.Registry().SelectionActions([]string{kinds.ArgoApplicationsKey}) {
		ids = append(ids, c.ID)
	}
	if got := strings.Join(ids, ","); got != "argocd.application.syncMany,argocd.application.refreshMany,object.deleteMany" {
		t.Errorf("selection actions %s", got)
	}
}

// TestArgoActionsBoundToTheConfirmedUID: an Application deleted and created again under its name
// while the user confirmed a sync with prune is not synced.
func TestArgoActionsBoundToTheConfirmedUID(t *testing.T) {
	h := newArgoHarness(t)
	ref := h.app(t, nil, "shop")
	h.s.SetConfirmer(engine.ConfirmFunc(func(context.Context, engine.ConfirmRequest) (bool, error) {
		o := testkit.ArgoApplication("argocd", "shop", "OutOfSync", "Healthy")
		o.SetUID("another-uid")
		h.src.Set(testkit.ArgoApplicationsGVR, o)
		waitUntil(t, "the new object in the cache", func() bool {
			got, err := h.s.Lookup(ref)
			return err == nil && string(got.GetUID()) == "another-uid"
		})
		return true, nil
	}))
	_, err := h.dispatch(engine.OriginGUI, "argocd.application.sync", map[string]any{"ref": ref, "prune": true})
	if !errors.Is(err, engine.ErrTargetsChanged) {
		t.Fatalf("sync of a replaced application: %v", err)
	}
	if w := h.writer.Writes(); len(w) != 0 {
		t.Errorf("writes %+v", w)
	}
}

// TestArgoSettingsNeverReachAgents: no credential of Argo CD's repository and cluster Secrets
// reaches an agent through any read path, the audit log, an event or the log (D-35).
func TestArgoSettingsNeverReachAgents(t *testing.T) {
	h := newArgoHarness(t)
	h.src.Set(testkit.SecretsGVR, testkit.ArgoRepoSecret("argocd", "repo-shop", "https://deploy:fake-url-token@git.example.com/shop.git", "fake-repo-password"))
	h.src.Set(testkit.SecretsGVR, testkit.ArgoClusterSecret("argocd", "prod", "https://prod.example.com:6443", "fake-cluster-token"))
	forbidden := []string{"fake-repo-password", "fake-url-token", "deploy-bot", "fake-cluster-token", `"bearerToken`}
	for _, f := range append([]string(nil), forbidden...) {
		forbidden = append(forbidden, base64.StdEncoding.EncodeToString([]byte(f)))
	}
	carries := func(b []byte) string {
		for _, f := range forbidden {
			if strings.Contains(string(b), f) {
				return f
			}
		}
		return ""
	}
	repo := model.ObjectRef{Context: "test", Kind: kinds.ArgoRepositoriesKey, Namespace: "argocd", Name: "repo-shop"}
	cl := model.ObjectRef{Context: "test", Kind: kinds.ArgoClustersKey, Namespace: "argocd", Name: "prod"}
	calls := []struct {
		id     string
		params map[string]any
	}{
		{"object.get", map[string]any{"ref": repo}},
		{"object.get", map[string]any{"ref": cl, "related": true}},
		{"resources.list", map[string]any{"kind": kinds.ArgoRepositoriesKey}},
		{"resources.list", map[string]any{"kind": kinds.ArgoClustersKey, "filter": "prod"}},
		{"resources.find", map[string]any{"kind": kinds.ArgoRepositoriesKey, "field": "url", "value": "git.example.com", "match": "contains"}},
		{"argocd.dashboard", map[string]any{"attention": true}},
		{"ui.openView", map[string]any{"view": kinds.ArgoRepositoriesKey}},
		{"view.rows", map[string]any{}},
		{"table.exportCsv", map[string]any{}},
	}
	var sawURL bool
	for _, c := range calls {
		res, err := h.dispatch(engine.OriginExternalAgent, c.id, c.params)
		if err != nil {
			t.Fatalf("%s %v: %v", c.id, c.params, err)
		}
		b, _ := json.Marshal(res)
		if l := carries(b); l != "" {
			t.Errorf("%s %v carries %q", c.id, c.params, l)
		}
		// encoding/json escapes the angle brackets of <redacted>.
		sawURL = sawURL || strings.Contains(string(b), "redacted\\u003e@git.example.com/shop.git")
		if c.id == "ui.openView" {
			waitUntil(t, "the repositories' rows", func() bool {
				snap := h.s.Table(kinds.ArgoRepositoriesKey)
				return snap != nil && snap.Synced && len(snap.Rows) == 1
			})
		}
	}
	if !sawURL {
		t.Error("no read showed the repository's URL, its user information redacted")
	}
	if l := carries([]byte(h.trail())); l != "" {
		t.Errorf("%q in the audit log, the events or the log", l)
	}
}

// TestArgoSyncRespectsSyncWindows (A33, owner 2026-10-14 evening; H-67): a sync of an Application
// its AppProject's open deny window covers is refused with the window named and nothing written,
// also in a selection, where the others are synced; an Application the window does not cover is
// synced.
func TestArgoSyncRespectsSyncWindows(t *testing.T) {
	h := newArgoHarness(t)
	h.src.Set(testkit.ArgoProjectsGVR, testkit.Object("argoproj.io/v1alpha1", "AppProject", "argocd", "default", map[string]any{
		"spec": map[string]any{"syncWindows": []any{
			map[string]any{"kind": "deny", "schedule": "* * * * *", "duration": "1h", "applications": []any{"web*"}},
		}},
	}))
	web, api := h.app(t, nil, "web"), h.app(t, nil, "api")
	_, err := h.dispatch(engine.OriginGUI, "argocd.application.sync", map[string]any{"ref": web})
	if err == nil || !strings.Contains(err.Error(), "deny window \"* * * * *\"") {
		t.Fatalf("a sync in a deny window: %v", err)
	}
	if _, err := h.dispatch(engine.OriginGUI, "argocd.application.sync", map[string]any{"ref": api}); err != nil {
		t.Fatalf("a sync the window does not cover: %v", err)
	}
	_, err = h.dispatch(engine.OriginGUI, "argocd.application.syncMany", map[string]any{"refs": []model.ObjectRef{web, api}})
	if err == nil || !strings.Contains(err.Error(), "deny window") {
		t.Errorf("a selection with one in the window: %v", err)
	}
	synced := map[string]int{}
	for _, w := range h.writer.Writes() {
		if w.op == "sync" {
			synced[w.name]++
		}
	}
	if synced["web"] != 0 || synced["api"] != 2 {
		t.Errorf("syncs written %v, want api twice and web never", synced)
	}
}

// TestArgoSyncWindowsOfAnAppInAnotherNamespace: an Application outside Argo CD's namespace
// ("Applications in any namespace") has its AppProject in the namespace Argo CD's controller runs
// in, which it writes into status.controllerNamespace; the project's deny window greys its Sync
// and refuses the write, as on a live Argo CD controller (checked on a real cluster, 2026-10-16:
// before, Cogpit looked for the project in the Application's namespace, found none, and let the
// sync through to wait in the controller).
func TestArgoSyncWindowsOfAnAppInAnotherNamespace(t *testing.T) {
	h := newArgoHarness(t)
	h.src.Set(testkit.ArgoProjectsGVR, testkit.Object("argoproj.io/v1alpha1", "AppProject", "argocd", "frozen", map[string]any{
		"spec": map[string]any{
			"sourceNamespaces": []any{"team-billing"},
			"syncWindows":      []any{map[string]any{"kind": "deny", "schedule": "* * * * *", "duration": "1h", "applications": []any{"*"}}},
		},
	}))
	o := testkit.ArgoApplication("team-billing", "billing", "OutOfSync", "Missing")
	o.Object["spec"].(map[string]any)["project"] = "frozen"
	o.Object["status"].(map[string]any)["controllerNamespace"] = "argocd"
	h.src.Set(testkit.ArgoApplicationsGVR, o)
	ref := model.ObjectRef{Context: "test", Kind: kinds.ArgoApplicationsKey, Namespace: "team-billing", Name: "billing"}
	waitUntil(t, "the Application in the cache", func() bool { _, err := h.s.Lookup(ref); return err == nil })

	_, err := h.dispatch(engine.OriginGUI, "argocd.application.sync", map[string]any{"ref": ref})
	if err == nil || !strings.Contains(err.Error(), "deny window") {
		t.Fatalf("a sync in its project's deny window: %v", err)
	}
	for _, w := range h.writer.Writes() {
		if w.op == "sync" {
			t.Errorf("a sync was written: %+v", w)
		}
	}
	// The drawer links the project where it is.
	d, err := h.s.Detail(ref)
	if err != nil {
		t.Fatal(err)
	}
	for _, sec := range d.Sections {
		for _, it := range sec.Items {
			if it.Name == "Project" && (it.Link == nil || it.Link.Namespace != "argocd" || it.Link.Name != "frozen") {
				t.Errorf("project link %+v", it.Link)
			}
		}
	}
}
