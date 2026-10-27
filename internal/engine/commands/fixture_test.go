package commands_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

func str(obj *unstructured.Unstructured, fields ...string) string {
	v, _, _ := unstructured.NestedString(obj.Object, fields...)
	return v
}

func textCells(cols []model.Column, values ...func(*unstructured.Unstructured) string) func(*unstructured.Unstructured) ([]model.Cell, model.Status) {
	return func(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
		cells := make([]model.Cell, len(cols))
		for i, v := range values {
			cells[i] = model.Cell{Text: v(obj)}
		}
		return cells, model.StatusNone
	}
}

func name(o *unstructured.Unstructured) string      { return o.GetName() }
func namespace(o *unstructured.Unstructured) string { return o.GetNamespace() }

func profile(key, group, kind, title string, namespaced bool, extra ...model.Column) *model.KindProfile {
	cols := []model.Column{{ID: "name", Title: "Name", Type: model.ColumnText}, {ID: "namespace", Title: "Namespace", Type: model.ColumnText}}
	cols = append(cols, extra...)
	return &model.KindProfile{
		Key: key, Group: group, Version: "v1", Resource: key, Kind: kind, Title: title, Namespaced: namespaced,
		Columns: cols, Cells: textCells(cols, name, namespace),
	}
}

func testProfiles() []*model.KindProfile {
	pods := profile("pods", "", "Pod", "Pods", true, model.Column{ID: "node", Title: "Node", Type: model.ColumnText})
	pods.Cells = textCells(pods.Columns, name, namespace, func(o *unstructured.Unstructured) string { return str(o, "spec", "nodeName") })
	pods.Fields = map[string]func(*unstructured.Unstructured) []string{
		"podIP": func(o *unstructured.Unstructured) []string { return []string{str(o, "status", "podIP")} },
		"node":  func(o *unstructured.Unstructured) []string { return []string{str(o, "spec", "nodeName")} },
	}
	svc := profile("services", "", "Service", "Services", true, model.Column{ID: "clusterIP", Title: "Cluster IP", Type: model.ColumnText})
	svc.Cells = textCells(svc.Columns, name, namespace, func(o *unstructured.Unstructured) string { return str(o, "spec", "clusterIP") })
	svc.Fields = map[string]func(*unstructured.Unstructured) []string{
		"clusterIP": func(o *unstructured.Unstructured) []string { return []string{str(o, "spec", "clusterIP")} },
	}
	events := profile("events", "", "Event", "Events", true, model.Column{ID: "lastSeen", Title: "Last Seen", Type: model.ColumnAge})
	ns := &model.KindProfile{
		Key: "namespaces", Version: "v1", Resource: "namespaces", Kind: "Namespace", Title: "Namespaces",
		Columns: []model.Column{{ID: "name", Title: "Name", Type: model.ColumnText}},
	}
	return []*model.KindProfile{pods, profile("deployments", "apps", "Deployment", "Deployments", true), svc, ns, events}
}

// fakeConnector connects "test" and "other" to fresh caches over one MemorySource; "broken"
// fails. Backends get the fake writer and, unless demo is set, a (zero) connection, so that the
// commands that need one are enabled; nothing here dials a cluster.
type fakeConnector struct {
	src    *testkit.MemorySource
	writer *fakeWriter
	demo   bool
	health engine.HealthProber // the connection watch's probes (connection_test.go); nil: no watch
	access engine.Access       // the backends' access reviews; nil: none
}

func (f *fakeConnector) Contexts() ([]engine.ContextInfo, error) {
	return []engine.ContextInfo{{Name: "other"}, {Name: "test", Current: true}}, nil
}

func (f *fakeConnector) Connect(_ context.Context, name string) (*engine.Backend, error) {
	if name == "broken" {
		return nil, errors.New("dial tcp 127.0.0.1:1: connect: connection refused")
	}
	c := cache.New(f.src, cache.Options{WarmPeriod: time.Minute})
	b := &engine.Backend{Context: name, Namespace: "default", Cache: c, Profiles: testProfiles(), Close: c.Close, Demo: f.demo, Access: f.access}
	if !f.demo {
		b.Conn = &cluster.Conn{}
		b.Actions = f.writer
		b.Health = f.health
	}
	return b, nil
}

// fakeWriter records the writes the action commands make.
type fakeWriter struct {
	mu    sync.Mutex
	calls []string
	uids  []string // the UID each UID-bound write (restart, scale) carried, in call order
}

func (w *fakeWriter) RestartDeployment(_ context.Context, namespace, name, uid string) error {
	w.bound(uid)
	w.record("restart " + namespace + "/" + name)
	return nil
}

// bound records the UID a write carried.
func (w *fakeWriter) bound(uid string) {
	w.mu.Lock()
	w.uids = append(w.uids, uid)
	w.mu.Unlock()
}

// UIDs returns the UIDs the UID-bound writes carried.
func (w *fakeWriter) UIDs() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.uids...)
}

func (w *fakeWriter) DeletePod(_ context.Context, namespace, name, _ string) error {
	w.record("delete " + namespace + "/" + name)
	return nil
}

func (w *fakeWriter) record(c string) {
	w.mu.Lock()
	w.calls = append(w.calls, c)
	w.mu.Unlock()
}

func (w *fakeWriter) Calls() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.calls...)
}

// seed fills a source with a small cluster: pods, services (one with a known cluster IP), a
// deployment, namespaces and events.
func seed() *testkit.MemorySource {
	src := testkit.NewMemorySource()
	for _, ns := range []string{"shop", "infra"} {
		src.Set(testkit.NamespacesGVR, testkit.Object("v1", "Namespace", "", ns, nil))
		src.Set(testkit.PodsGVR, testkit.Pod(ns, "web-0"))
		src.Set(testkit.PodsGVR, testkit.Pod(ns, "web-1"))
		src.Set(testkit.DeploymentsGVR, testkit.Object("apps/v1", "Deployment", ns, "web", nil))
	}
	src.Set(testkit.ServicesGVR, testkit.Service("shop", "checkout", "10.96.100.100"))
	src.Set(testkit.ServicesGVR, testkit.Service("shop", "cart", "10.96.100.101"))
	src.Set(testkit.ServicesGVR, testkit.Service("infra", "dns", "10.96.0.10"))
	src.Set(testkit.EventsGVR, testkit.Object("v1", "Event", "shop", "checkout.1", map[string]any{
		"involvedObject": map[string]any{"kind": "Service", "namespace": "shop", "name": "checkout", "uid": "uid-shop-checkout"},
		"reason":         "Updated", "type": "Normal", "lastTimestamp": "2026-10-11T09:00:00Z",
	}))
	return src
}

type harness struct {
	s      *engine.Session
	src    *testkit.MemorySource
	writer *fakeWriter
	access *swapAccess // the connected backend's access reviews, where the harness installs them
}

func newHarness(t testing.TB, src *testkit.MemorySource, opts engine.Options) *harness {
	t.Helper()
	h := &harness{src: src, writer: &fakeWriter{}, access: &swapAccess{}}
	opts.Connector = &fakeConnector{src: src, writer: h.writer, access: h.access}
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
	return h
}

// run dispatches a command and decodes its result into out (if not nil) through JSON, as an MCP
// client would see it.
func (h *harness) run(t testing.TB, origin engine.Origin, id string, params any, out any) error {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	res, err := h.s.Dispatch(context.Background(), engine.Invocation{ID: id, Params: raw, Origin: origin})
	if err != nil {
		return err
	}
	if out != nil {
		b, mErr := json.Marshal(res)
		if mErr != nil {
			t.Fatalf("%s: result not JSON: %v", id, mErr)
		}
		if uErr := json.Unmarshal(b, out); uErr != nil {
			t.Fatalf("%s: %v", id, uErr)
		}
	}
	return nil
}

func (h *harness) must(t testing.TB, origin engine.Origin, id string, params any, out any) {
	t.Helper()
	if err := h.run(t, origin, id, params, out); err != nil {
		t.Fatalf("%s: %v", id, err)
	}
}

// waitSettled waits until every informer of the connected cluster has synced and opened its watch
// (testkit.Settled): the baseline of a request budget.
func (h *harness) waitSettled(t testing.TB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b := h.s.Backend()
	if b == nil {
		t.Fatal("not connected")
	}
	if !testkit.WaitFor(ctx, func() bool { return testkit.Settled(b.Cache, h.src) }) {
		t.Fatalf("the cache never settled: %+v", b.Cache.Stats())
	}
}

func waitTable(t testing.TB, s *engine.Session, view string, cond func(*engine.TableSnapshot) bool) *engine.TableSnapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var got *engine.TableSnapshot
	if !testkit.WaitFor(ctx, func() bool {
		got = s.Table(view)
		return got != nil && cond(got)
	}) {
		t.Fatalf("no matching snapshot of %s (last %+v)", view, got)
	}
	return got
}

// syncBuffer is an io.Writer safe for the audit log's concurrent writes.
type syncBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *syncBuffer) Lines() []engine.AuditEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []engine.AuditEntry
	dec := json.NewDecoder(bytes.NewReader(b.buf))
	for {
		var e engine.AuditEntry
		if err := dec.Decode(&e); err != nil {
			return out
		}
		out = append(out, e)
	}
}
