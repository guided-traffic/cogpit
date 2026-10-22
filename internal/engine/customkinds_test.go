package engine

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

const (
	orderKey   = "orders.acme.cert-manager.io"
	orderGroup = "acme.cert-manager.io"
)

var ordersV1 = schema.GroupVersionResource{Group: orderGroup, Version: "v1", Resource: "orders"}

func orderColumns() []map[string]any {
	return []map[string]any{
		{"name": "State", "type": "string", "jsonPath": ".status.state"},
		{"name": "Issuer", "type": "string", "jsonPath": ".spec.issuerRef.name", "priority": int64(1)},
		{"name": "Age", "type": "date", "jsonPath": ".metadata.creationTimestamp"},
	}
}

func orderCRD() *unstructured.Unstructured {
	return testkit.CRD(orderGroup, "orders", "Order", true,
		testkit.CRDVersion{Name: "v1", Served: true, Storage: true, Columns: orderColumns()})
}

func order(namespace, name, state string) *unstructured.Unstructured {
	return testkit.Object("acme.cert-manager.io/v1", "Order", namespace, name, map[string]any{
		"spec":   map[string]any{"issuerRef": map[string]any{"name": "letsencrypt"}},
		"status": map[string]any{"state": state},
	})
}

// crdConnector connects "test" to a fresh cache over src with the custom kinds watch on; its
// built-in kinds are the engine tests' profiles plus extra.
type crdConnector struct {
	src   cache.Source
	extra []*model.KindProfile
}

func (c *crdConnector) Contexts() ([]ContextInfo, error) { return []ContextInfo{{Name: "test"}}, nil }

func (c *crdConnector) Connect(context.Context, string) (*Backend, error) {
	ch := cache.New(c.src, cache.Options{WarmPeriod: time.Minute})
	return &Backend{
		Context: "test", Namespace: "default", Cache: ch, Profiles: withProfiles(testProfiles(), c.extra...),
		CustomResources: true, Close: ch.Close,
	}, nil
}

// withProfiles returns the profiles with extra's: one of a key the profiles have replaces it, the
// others follow.
func withProfiles(base []*model.KindProfile, extra ...*model.KindProfile) []*model.KindProfile {
	out := append([]*model.KindProfile(nil), base...)
	for _, e := range extra {
		if i := slices.IndexFunc(out, func(p *model.KindProfile) bool { return p.Key == e.Key }); i >= 0 {
			out[i] = e
		} else {
			out = append(out, e)
		}
	}
	return out
}

func newCRDSession(t testing.TB, src cache.Source, extra ...*model.KindProfile) *Session {
	t.Helper()
	s := New(Options{Connector: &crdConnector{src: src, extra: extra}, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	connectTest(t, s)
	return s
}

func waitFor(t testing.TB, what string, cond func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, cond) {
		t.Fatalf("timed out waiting for %s", what)
	}
}

func hasKind(s *Session, key string) bool {
	b := s.Backend()
	if b == nil {
		return false
	}
	_, ok := b.Profile(key)
	return ok
}

func openView(s *Session, view string) {
	s.UpdateUI(func(u *UIState) { u.View, u.Selected, u.Drawer, u.Filter = view, nil, nil, "" })
}

// TestCustomKindLifecycle walks a custom resource kind through the engine: it appears once its
// definition is established, its view shows the printer columns, a status-only update keeps the
// profile, a new served version moves the open view, and deleting the definition leaves a view
// that says the cluster no longer serves the kind.
func TestCustomKindLifecycle(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.PodsGVR, testkit.Pod("default", "web-0"))
	src.Set(crdsGVR, testkit.NotEstablished(orderCRD()))
	src.Set(ordersV1, order("shop", "tls-1", "valid"))
	src.Set(ordersV1, order("infra", "tls-2", "pending"))
	s := newCRDSession(t, src)
	rec := record(s)

	b := s.Backend()
	waitFor(t, "the definitions to be listed", func() bool { set := b.customSet(); return set != nil && set.synced })
	if hasKind(s, orderKey) {
		t.Fatal("a definition that is not established offers a kind")
	}
	if _, _, err := s.Kind(orderKey); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("Kind before established: %v", err)
	}

	src.Set(crdsGVR, orderCRD())
	waitFor(t, "the Order kind", func() bool { return hasKind(s, orderKey) })
	waitFor(t, "EventKinds", func() bool { return rec.count(EventKinds, "") > 0 })
	p, _ := b.Profile(orderKey)
	if p.Title != "Order" || p.Version != "v1" || !p.Namespaced || p.GVR() != ordersV1 {
		t.Fatalf("profile %+v", p)
	}
	all := b.AllProfiles()
	if all[len(all)-1] != p || len(all) != len(b.Profiles)+1 {
		t.Errorf("AllProfiles: built-in kinds first, then the custom kind; got %d profiles", len(all))
	}

	openView(s, orderKey)
	snap := waitTable(t, s, orderKey, func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 2 })
	state := p.ColumnIndex("state")
	if snap.Title != "Order" || state < 0 || snap.Rows[0].Cells[state].Text != "valid" || snap.Rows[1].Cells[state].Text != "pending" {
		t.Fatalf("snapshot %s: %+v", summary(snap), snap.Rows)
	}

	// A status update of the definition changes nothing the view shows: same profile, no event.
	kindsEvents := rec.count(EventKinds, "")
	status := orderCRD()
	status.Object["status"].(map[string]any)["storedVersions"] = []any{"v1"}
	src.Set(crdsGVR, status)
	time.Sleep(100 * time.Millisecond)
	if q, _ := b.Profile(orderKey); q != p || rec.count(EventKinds, "") != kindsEvents {
		t.Errorf("a status update replaced the profile or announced new kinds")
	}

	// A new served version with a higher priority: the view moves to v2 and its columns.
	ordersV2 := ordersV1
	ordersV2.Version = "v2"
	src.Set(ordersV2, order("shop", "tls-1", "ready"))
	v2cols := []map[string]any{{"name": "Phase", "type": "string", "jsonPath": ".status.state"}}
	src.Set(crdsGVR, testkit.CRD(orderGroup, "orders", "Order", true,
		testkit.CRDVersion{Name: "v1", Served: true, Storage: true, Columns: orderColumns()},
		testkit.CRDVersion{Name: "v2", Served: true, Columns: v2cols}))
	snap = waitTable(t, s, orderKey, func(t *TableSnapshot) bool {
		return t.Synced && len(t.Rows) == 1 && len(t.Columns) > 2 && t.Columns[2].ID == "phase"
	})
	if q, _ := b.Profile(orderKey); q.Version != "v2" || snap.Rows[0].Cells[2].Text != "ready" {
		t.Errorf("after the version change: profile %s, row %+v", q.Version, snap.Rows[0])
	}

	// Deleting the definition: the kind is gone and the open view says so.
	src.Delete(crdsGVR, "", orderKey)
	waitFor(t, "the Order kind to disappear", func() bool { return !hasKind(s, orderKey) })
	snap = waitTable(t, s, orderKey, func(t *TableSnapshot) bool { return t.Error != "" })
	if len(snap.Rows) != 0 || !strings.Contains(snap.Error, "no longer serves Order") || snap.Title != "Order" {
		t.Errorf("vanished view: %s, error %q", summary(snap), snap.Error)
	}
	if u := s.UIState(); u.View != orderKey {
		t.Errorf("the view changed to %q; the user stays on the vanished view", u.View)
	}
	_, _, err := s.Kind(orderKey)
	if !errors.Is(err, ErrUnknownKind) || !strings.Contains(err.Error(), "pods") {
		t.Errorf("Kind after deletion: %v", err)
	}

	// Recreated, the view fills again.
	src.Set(crdsGVR, orderCRD())
	waitTable(t, s, orderKey, func(t *TableSnapshot) bool { return t.Error == "" && t.Synced && len(t.Rows) == 2 })

	// Another view drops the vanished snapshot.
	src.Delete(crdsGVR, "", orderKey)
	waitTable(t, s, orderKey, func(t *TableSnapshot) bool { return t.Error != "" })
	openView(s, "pods")
	if s.Table(orderKey) != nil {
		t.Error("the vanished snapshot outlived its view")
	}
}

// TestCustomKindDrawerFollowsProfile: a drawer open on a custom resource follows its kind.
func TestCustomKindDrawerFollowsProfile(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(crdsGVR, orderCRD())
	src.Set(ordersV1, order("shop", "tls-1", "valid"))
	s := newCRDSession(t, src)
	waitFor(t, "the Order kind", func() bool { return hasKind(s, orderKey) })
	ref := model.ObjectRef{Kind: orderKey, Namespace: "shop", Name: "tls-1"}
	s.UpdateUI(func(u *UIState) { u.View, u.Drawer = orderKey, &ref })
	d, err := s.DetailWait(context.Background(), ref)
	if err != nil || d.Missing || d.Title != "Order: tls-1" {
		t.Fatalf("detail: %+v, %v", d, err)
	}
	if len(d.Sections) < 2 || d.Sections[1].Title != "Order" || d.Sections[1].Items[0].Value != "valid" {
		t.Errorf("printer columns in the drawer: %+v", d.Sections)
	}
	src.Delete(crdsGVR, "", orderKey)
	waitFor(t, "the drawer to stop", func() bool {
		s.state.mu.Lock()
		defer s.state.mu.Unlock()
		return s.state.drawer == nil
	})
	if _, err := s.Detail(ref); !errors.Is(err, ErrUnknownKind) {
		t.Errorf("detail of a vanished kind: %v", err)
	}
}

// TestCustomKindsChangeKeepsNewerUI: following a change of the custom kinds never applies a UI
// state older than one an UpdateUI wrote meanwhile. customKindsChanged used to read the UI state
// before taking state.mu; an UpdateUI in between that opened the drawer was undone, and the drawer
// stayed closed while the UI state named it (TestCustomKindDrawerFollowsProfile then found the
// drawer stopped before the definition was even deleted). The test holds state.mu, lets
// customKindsChanged block on it, and only then writes the UI state, as UpdateUI does before its
// own reconcile.
func TestCustomKindsChangeKeepsNewerUI(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.PodsGVR, testkit.Pod("shop", "web-0"))
	s := newCRDSession(t, src)
	b := s.Backend()
	ref := model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}

	s.state.mu.Lock()
	done := make(chan struct{})
	go func() { defer close(done); s.customKindsChanged(b) }()
	waitFor(t, "customKindsChanged to wait for state.mu", func() bool { return blockedOnMutex("(*Session).customKindsChanged") })
	s.mu.Lock() // UpdateUI's first half: the new state is written, its reconcile still to come
	s.ui.Drawer = &ref
	s.ui.Revision++
	rev := s.ui.Revision
	s.mu.Unlock()
	s.state.mu.Unlock()
	<-done

	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	if s.state.drawer == nil || s.state.appliedRev != rev {
		t.Fatalf("reconciled an older UI state: drawer watch %v, applied revision %d, want %d",
			s.state.drawer != nil, s.state.appliedRev, rev)
	}
}

// blockedOnMutex reports whether a goroutine waits for a sync.Mutex inside fn (a function name as
// a stack trace prints it).
func blockedOnMutex(fn string) bool {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	for g := range strings.SplitSeq(string(buf), "\n\n") {
		if strings.Contains(g, "sync.(*Mutex).Lock") && strings.Contains(g, fn) {
			return true
		}
	}
	return false
}

// TestCuratedProfileWins: a built-in profile for a CRD kind keeps its key; the generic one is not
// offered beside it.
func TestCuratedProfileWins(t *testing.T) {
	curated := &model.KindProfile{
		Key: orderKey, Group: orderGroup, Version: "v1", Resource: "orders", Kind: "Order", Title: "Orders", Namespaced: true,
		Columns: []model.Column{{ID: "name", Title: "Name", Type: model.ColumnText}},
	}
	src := testkit.NewMemorySource()
	src.Set(crdsGVR, orderCRD())
	src.Set(crdsGVR, testkit.CRD("example.com", "things", "Thing", false))
	s := newCRDSession(t, src, curated)
	b := s.Backend()
	waitFor(t, "the Thing kind", func() bool { return hasKind(s, "things.example.com") })
	if p, _ := b.Profile(orderKey); p != curated {
		t.Errorf("Profile(%s) = %+v, want the curated profile", orderKey, p)
	}
	n := 0
	for _, p := range b.AllProfiles() {
		if p.Key == orderKey {
			n++
		}
	}
	if n != 1 || len(b.CustomProfiles()) != 1 {
		t.Errorf("%s listed %d times; custom kinds %d", orderKey, n, len(b.CustomProfiles()))
	}
}

// TestCustomKindsForbidden: a user who may not list definitions gets the built-in kinds, no
// custom ones, no error toast, and an explanation when asking for a custom kind.
func TestCustomKindsForbidden(t *testing.T) {
	mem := testkit.NewMemorySource()
	mem.Set(testkit.PodsGVR, testkit.Pod("default", "web-0"))
	src := testkit.RefusingSource{Inner: mem, Refuse: map[schema.GroupVersionResource]error{
		crdsGVR: apierrors.NewForbidden(crdsGVR.GroupResource(), "", errors.New(`User "dev" cannot list resource "customresourcedefinitions"`)),
	}}
	s := newCRDSession(t, src)
	rec := record(s)
	b := s.Backend()
	waitFor(t, "the forbidden list", func() bool { set := b.customSet(); return set != nil && set.err != "" })
	if len(b.CustomProfiles()) != 0 || len(b.AllProfiles()) != len(b.Profiles) {
		t.Errorf("custom kinds without permission: %d", len(b.CustomProfiles()))
	}
	_, _, err := s.Kind(orderKey)
	if !errors.Is(err, ErrUnknownKind) || !strings.Contains(err.Error(), "custom resource kinds are unavailable") {
		t.Errorf("Kind: %v", err)
	}
	waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 1 })
	for _, ev := range rec.all() {
		if ev.Type == EventToast {
			t.Errorf("toast: %s %s", ev.Level, ev.Message)
		}
	}
}

// TestCustomKindsRace churns definitions and objects while views, lookups and kind lists read the
// custom kinds. Run with -race.
func TestCustomKindsRace(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(crdsGVR, orderCRD())
	src.Set(ordersV1, order("shop", "tls-1", "valid"))
	s := newCRDSession(t, src)
	waitFor(t, "the Order kind", func() bool { return hasKind(s, orderKey) })
	openView(s, orderKey)

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // definitions come and go, change versions and columns
		defer wg.Done()
		for i := 0; ctx.Err() == nil; i++ {
			group := fmt.Sprintf("g%d.example.com", i%7)
			switch i % 4 {
			case 0:
				src.Set(crdsGVR, testkit.CRD(group, "things", "Thing", i%2 == 0))
			case 1:
				src.Set(crdsGVR, testkit.CRD(group, "things", "Thing", true,
					testkit.CRDVersion{Name: "v1", Served: true, Storage: true},
					testkit.CRDVersion{Name: "v2", Served: true, Columns: orderColumns()}))
			case 2:
				src.Delete(crdsGVR, "", "things."+group)
			case 3:
				if i%8 == 3 {
					src.Delete(crdsGVR, "", orderKey)
				} else {
					src.Set(crdsGVR, orderCRD())
				}
			}
			src.Set(ordersV1, order("shop", fmt.Sprintf("o-%d", i%5), "valid"))
			time.Sleep(time.Millisecond)
		}
	}()
	for g := range 4 {
		wg.Add(1)
		go func() { // readers: views, lookups, kind lists, the view's snapshots
			defer wg.Done()
			for i := 0; ctx.Err() == nil; i++ {
				b := s.Backend()
				for _, p := range b.AllProfiles() {
					_ = p.Key
					if q, ok := b.Profile(p.Key); ok && len(q.Columns) == 0 {
						t.Error("a profile without columns")
					}
				}
				_, _, _ = s.Kind(fmt.Sprintf("things.g%d.example.com", i%7))
				if snap := s.Table(orderKey); snap != nil {
					_ = len(snap.Rows)
				}
				switch (g + i) % 3 {
				case 0:
					openView(s, orderKey)
				case 1:
					openView(s, fmt.Sprintf("things.g%d.example.com", i%7))
				case 2:
					_, _ = s.Objects(ctx, orderKey, nil)
				}
				time.Sleep(time.Millisecond)
			}
		}()
	}
	wg.Wait()
}

// TestBackendWithoutCustomResources: the demo cluster and other backends without the flag have
// the built-in kinds only and start no watch on definitions.
func TestBackendWithoutCustomResources(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(crdsGVR, orderCRD())
	s, _ := newTestSession(t, src, 5*time.Millisecond)
	connectTest(t, s)
	b := s.Backend()
	if b.custom != nil || len(b.CustomProfiles()) != 0 {
		t.Fatal("custom kinds without CustomResources")
	}
	time.Sleep(50 * time.Millisecond)
	for _, st := range b.Cache.Stats() {
		if st.Scope.GVR == crdsGVR {
			t.Error("a watch on definitions without CustomResources")
		}
	}
}
