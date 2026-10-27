package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// Test profiles: richer than the Phase 0 placeholders, with the columns and fields the tests need.

func ageCell(obj *unstructured.Unstructured) model.Cell {
	ts := obj.GetCreationTimestamp()
	if ts.IsZero() {
		return model.Cell{}
	}
	return model.Cell{Num: ts.Unix()}
}

var testPods = &model.KindProfile{
	Key: "pods", Version: "v1", Resource: "pods", Kind: "Pod", Title: "Pods", Namespaced: true,
	Columns: []model.Column{
		{ID: "name", Title: "Name", Type: model.ColumnText},
		{ID: "namespace", Title: "Namespace", Type: model.ColumnText},
		{ID: "restarts", Title: "Restarts", Type: model.ColumnNumber},
		{ID: "node", Title: "Node", Type: model.ColumnText},
		{ID: "age", Title: "Age", Type: model.ColumnAge},
		{ID: "status", Title: "Status", Type: model.ColumnText},
	},
	Cells: func(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
		var restarts int64
		v, _, _ := unstructured.NestedFieldNoCopy(obj.Object, "status", "containerStatuses")
		list, _ := v.([]any)
		for _, c := range list {
			if m, ok := c.(map[string]any); ok {
				n, _ := m["restartCount"].(int64)
				restarts += n
			}
		}
		phase := nestedString(obj.Object, "status", "phase")
		st := model.StatusPending
		if phase == "Running" {
			st = model.StatusOK
		}
		return []model.Cell{
			{Text: obj.GetName()}, {Text: obj.GetNamespace()},
			{Text: strconv.FormatInt(restarts, 10), Num: restarts},
			{Text: nestedString(obj.Object, "spec", "nodeName")},
			ageCell(obj), {Text: phase, Status: st},
		}, st
	},
	Fields: map[string]func(*unstructured.Unstructured) []string{
		"podIP": func(obj *unstructured.Unstructured) []string {
			return []string{nestedString(obj.Object, "status", "podIP")}
		},
		"node": func(obj *unstructured.Unstructured) []string {
			return []string{nestedString(obj.Object, "spec", "nodeName")}
		},
	},
	Detail: func(obj *unstructured.Unstructured) []model.DetailSection {
		sec := model.DetailSection{Title: "Containers"}
		for _, n := range ContainerNames(obj, false) {
			sec.Items = append(sec.Items, model.DetailItem{Value: n})
		}
		return []model.DetailSection{sec}
	},
}

var testServices = &model.KindProfile{
	Key: "services", Version: "v1", Resource: "services", Kind: "Service", Title: "Services", Namespaced: true,
	Columns: []model.Column{
		{ID: "name", Title: "Name", Type: model.ColumnText},
		{ID: "namespace", Title: "Namespace", Type: model.ColumnText},
		{ID: "type", Title: "Type", Type: model.ColumnText},
		{ID: "clusterIP", Title: "Cluster IP", Type: model.ColumnText},
		{ID: "age", Title: "Age", Type: model.ColumnAge},
	},
	Cells: func(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
		return []model.Cell{
			{Text: obj.GetName()}, {Text: obj.GetNamespace()},
			{Text: nestedString(obj.Object, "spec", "type")},
			{Text: nestedString(obj.Object, "spec", "clusterIP")},
			ageCell(obj),
		}, model.StatusNone
	},
	Fields: map[string]func(*unstructured.Unstructured) []string{
		"clusterIP": func(obj *unstructured.Unstructured) []string {
			return []string{nestedString(obj.Object, "spec", "clusterIP")}
		},
	},
}

var testDeployments = &model.KindProfile{
	Key: "deployments", Group: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment",
	Title: "Deployments", Namespaced: true,
	Columns: []model.Column{
		{ID: "name", Title: "Name", Type: model.ColumnText},
		{ID: "namespace", Title: "Namespace", Type: model.ColumnText},
	},
}

var testEvents = &model.KindProfile{
	Key: "events", Version: "v1", Resource: "events", Kind: "Event", Title: "Events", Namespaced: true,
	Columns: []model.Column{
		{ID: "name", Title: "Name", Type: model.ColumnText},
		{ID: "namespace", Title: "Namespace", Type: model.ColumnText},
		{ID: "reason", Title: "Reason", Type: model.ColumnText},
		{ID: "lastSeen", Title: "Last Seen", Type: model.ColumnAge},
	},
}

var testNamespaces = &model.KindProfile{
	Key: "namespaces", Version: "v1", Resource: "namespaces", Kind: "Namespace", Title: "Namespaces",
	Columns: []model.Column{{ID: "name", Title: "Name", Type: model.ColumnText}},
}

var testSecrets = &model.KindProfile{
	Key: "secrets", Version: "v1", Resource: "secrets", Kind: "Secret", Title: "Secrets", Namespaced: true,
	Columns: []model.Column{{ID: "name", Title: "Name", Type: model.ColumnText}},
}

func testProfiles() []*model.KindProfile {
	return []*model.KindProfile{testPods, testDeployments, testServices, testNamespaces, testEvents, testSecrets}
}

// fakeConnector connects every context but "broken" to a fresh cache over one MemorySource, so
// the source's List and Watch counters are the API requests a scenario costs. The context "slow"
// blocks until its ctx ends.
type fakeConnector struct {
	src      *testkit.MemorySource
	profiles []*model.KindProfile
	actions  Actions
	conn     *cluster.Conn
	health   HealthProber // the connection watch's probes (connwatch_test.go); nil: none
	version  string       // the API server's version of a backend without conn (connwatch_version_test.go)
	access   Access       // the backends' access reviews, set before the session publishes them; nil: none

	mu       sync.Mutex
	connects int
}

func (f *fakeConnector) Contexts() ([]ContextInfo, error) {
	return []ContextInfo{{Name: "broken"}, {Name: "test", Current: true}}, nil
}

func (f *fakeConnector) Connect(ctx context.Context, name string) (*Backend, error) {
	f.mu.Lock()
	f.connects++
	f.mu.Unlock()
	switch name {
	case "broken":
		return nil, errors.New("dial tcp 127.0.0.1:1: connect: connection refused")
	case "slow":
		<-ctx.Done()
		return nil, ctx.Err()
	}
	c := cache.New(f.src, cache.Options{WarmPeriod: time.Minute})
	return &Backend{
		Context: name, Namespace: "default", Cache: c, Profiles: f.profiles,
		Actions: f.actions, Conn: f.conn, Close: c.Close, Health: f.health, Version: f.version, Access: f.access,
	}, nil
}

// newTestSession returns a session over src with a fast snapshot interval, closed at cleanup.
func newTestSession(t testing.TB, src *testkit.MemorySource, interval time.Duration) (*Session, *fakeConnector) {
	t.Helper()
	fc := &fakeConnector{src: src, profiles: testProfiles()}
	s := New(Options{Connector: fc, SnapshotInterval: interval})
	t.Cleanup(s.Close)
	return s, fc
}

func connectTest(t testing.TB, s *Session) {
	t.Helper()
	if err := s.Connect(context.Background(), "test"); err != nil {
		t.Fatalf("connect: %v", err)
	}
}

// waitTable waits for a snapshot of view that satisfies cond.
func waitTable(t testing.TB, s *Session, view string, cond func(*TableSnapshot) bool) *TableSnapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var got *TableSnapshot
	if !testkit.WaitFor(ctx, func() bool {
		got = s.Table(view)
		return got != nil && cond(got)
	}) {
		t.Fatalf("no matching snapshot of %s; last: %s", view, summary(got))
	}
	return got
}

func summary(t *TableSnapshot) string {
	if t == nil {
		return "nil"
	}
	keys := rowKeys(t)
	if len(keys) > 10 {
		keys = append(keys[:10:10], "...")
	}
	return fmt.Sprintf("rev %d synced %v total %d rows %v", t.Revision, t.Synced, t.Total, keys)
}

func rowKeys(t *TableSnapshot) []string {
	out := make([]string, 0, len(t.Rows))
	for _, r := range t.Rows {
		out = append(out, r.Ref.Key())
	}
	return out
}

// recorder collects engine events.
type recorder struct {
	mu     sync.Mutex
	events []Event
}

func record(s *Session) *recorder {
	r := &recorder{}
	s.Subscribe(func(ev Event) {
		r.mu.Lock()
		r.events = append(r.events, ev)
		r.mu.Unlock()
	})
	return r
}

func (r *recorder) all() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.events...)
}

func (r *recorder) count(typ EventType, view string) int {
	n := 0
	for _, ev := range r.all() {
		if ev.Type == typ && (view == "" || ev.View == view) {
			n++
		}
	}
	return n
}

// announced reports whether the recorder holds the table event of a view's snapshot: the view's
// worker stores a snapshot before it emits its event, so a snapshot read may still be unannounced.
func (r *recorder) announced(view string, revision uint64) bool {
	return slices.ContainsFunc(r.all(), func(ev Event) bool {
		return ev.Type == EventTable && ev.View == view && ev.Revision == revision
	})
}

// event builds a core v1 event about an object.
func event(namespace, name, kind, objName, uid, reason, typ string, last time.Time) *unstructured.Unstructured {
	inv := map[string]any{"kind": kind, "namespace": namespace, "name": objName}
	if uid != "" {
		inv["uid"] = uid
	}
	return testkit.Object("v1", "Event", namespace, name, map[string]any{
		"involvedObject": inv, "reason": reason, "type": typ, "message": reason + " happened",
		"source":        map[string]any{"component": "kubelet", "host": "node-1"},
		"count":         int64(2),
		"lastTimestamp": last.UTC().Format(time.RFC3339),
	})
}
