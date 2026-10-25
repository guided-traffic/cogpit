package demo

import (
	"context"
	"encoding/json"
	"net"
	"reflect"
	"strconv"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	toolscache "k8s.io/client-go/tools/cache"

	"github.com/guided-traffic/cogpit/internal/cache"
)

var testNow = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

// newTestCluster builds a static cluster with a fixed reference time and a clock that advances
// one millisecond per call, so churn timestamps are reproducible.
func newTestCluster(t testing.TB, opts Options) *Cluster {
	t.Helper()
	opts.NoChurn = true
	if opts.Now.IsZero() {
		opts.Now = testNow
	}
	c := New(opts)
	clock := testNow
	c.clock = func() time.Time {
		clock = clock.Add(time.Millisecond)
		return clock
	}
	return c
}

func (c *Cluster) stepLocked(n int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.step(n)
}

func list(t testing.TB, c *Cluster, gvr schema.GroupVersionResource, namespace string) *unstructured.UnstructuredList {
	t.Helper()
	obj, err := lwc(c, gvr, namespace).ListWithContext(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list %s: %v", gvr.Resource, err)
	}
	return obj.(*unstructured.UnstructuredList)
}

// lwc returns the context-aware list-watcher the reflector uses.
func lwc(c *Cluster, gvr schema.GroupVersionResource, namespace string) toolscache.ListerWatcherWithContext {
	return toolscache.ToListerWatcherWithContext(c.Source().ListerWatcher(cache.Scope{GVR: gvr, Namespace: namespace}))
}

func startWatch(t testing.TB, c *Cluster, gvr schema.GroupVersionResource, namespace, rv string, bookmarks bool) watch.Interface {
	t.Helper()
	w, err := lwc(c, gvr, namespace).WatchWithContext(context.Background(), metav1.ListOptions{ResourceVersion: rv, AllowWatchBookmarks: bookmarks})
	if err != nil {
		t.Fatalf("watch %s: %v", gvr.Resource, err)
	}
	t.Cleanup(w.Stop)
	return w
}

// receive reads n events or fails after a timeout.
func receive(t testing.TB, w watch.Interface, n int) []watch.Event {
	t.Helper()
	out := make([]watch.Event, 0, n)
	timeout := time.After(10 * time.Second)
	for len(out) < n {
		select {
		case ev, ok := <-w.ResultChan():
			if !ok {
				t.Fatalf("watch closed after %d of %d events", len(out), n)
			}
			out = append(out, ev)
		case <-timeout:
			t.Fatalf("received %d of %d events", len(out), n)
		}
	}
	return out
}

func gvrOf(k kind) schema.GroupVersionResource { return kindInfo[k].gvr }

// The Helm release records of a cluster with every namespace (helm.go): six revisions of shop-web
// and two of cache in Secrets, three of search-api in ConfigMaps.
const (
	helmSecrets    = 8
	helmConfigMaps = 3
)

func TestBuildSizesAndVariety(t *testing.T) {
	c := newTestCluster(t, Options{Seed: 1})
	want := map[kind]int{
		kindPods: DefaultPods, kindNodes: DefaultNodes, kindNamespaces: DefaultNamespaces,
		kindDeployments: DefaultDeployments, kindServices: DefaultServices, kindEvents: DefaultEvents,
		kindSecrets: helmSecrets, kindConfigMaps: helmConfigMaps,
	}
	for k, n := range want {
		if got := len(c.store[k]); got != n {
			t.Errorf("%s: %d objects, want %d", kindInfo[k].kind, got, n)
		}
	}

	// The statuses the Pods view must show.
	phases := map[podPhase]int{}
	notReady, terminating := 0, 0
	for _, p := range c.w.pods {
		phases[p.phase]++
		if p.phase == phaseRunning && !p.ready {
			notReady++
		}
		if !p.deleting.IsZero() {
			terminating++
		}
	}
	t.Logf("phases %v, not ready %d, terminating %d", phases, notReady, terminating)
	if phases[phaseRunning] < DefaultPods*85/100 {
		t.Errorf("only %d running pods", phases[phaseRunning])
	}
	for _, ph := range []podPhase{phaseCreating, phaseUnschedulable, phaseCrashLoop, phaseImagePull, phaseSucceeded, phaseFailed, phaseEvicted} {
		if phases[ph] == 0 {
			t.Errorf("no pod in phase %d", ph)
		}
	}
	if notReady == 0 || terminating == 0 {
		t.Errorf("not ready %d, terminating %d", notReady, terminating)
	}

	notReadyNodes, cordoned := 0, 0
	for _, n := range c.w.nodes {
		if n.notReady {
			notReadyNodes++
		}
		if n.cordoned {
			cordoned++
		}
	}
	if notReadyNodes != 1 || cordoned != 1 {
		t.Errorf("NotReady nodes %d, cordoned %d; want 1 and 1", notReadyNodes, cordoned)
	}
}

func TestServicesHaveUniqueClusterIPs(t *testing.T) {
	c := newTestCluster(t, Options{Seed: 1})
	_, cidr, _ := net.ParseCIDR("10.96.0.0/16")
	seen := map[string]string{}
	found := false
	for key, svc := range c.store[kindServices] {
		ip, _, _ := unstructured.NestedString(svc.Object, "spec", "clusterIP")
		if ip == "" || ip == "None" {
			continue
		}
		if !cidr.Contains(net.ParseIP(ip)) {
			t.Errorf("%s: clusterIP %s outside 10.96.0.0/16", key, ip)
		}
		if other, dup := seen[ip]; dup {
			t.Errorf("%s and %s share clusterIP %s", key, other, ip)
		}
		seen[ip] = key
		if ip == MCPClusterIP {
			found = key == MCPNamespace+"/"+MCPService
		}
	}
	if !found {
		t.Errorf("no service %s/%s with clusterIP %s", MCPNamespace, MCPService, MCPClusterIP)
	}
}

func TestDeterministicForASeed(t *testing.T) {
	a := newTestCluster(t, Options{Seed: 7, Pods: 2_000, Deployments: 100, Services: 60, Events: 300})
	b := newTestCluster(t, Options{Seed: 7, Pods: 2_000, Deployments: 100, Services: 60, Events: 300})
	other := newTestCluster(t, Options{Seed: 8, Pods: 2_000, Deployments: 100, Services: 60, Events: 300})
	if !sameStores(t, a, b) {
		t.Fatal("same seed, different clusters")
	}
	if sameStores(t, a, other) {
		t.Fatal("different seeds, same cluster")
	}
	// The churn's sequence depends on the seed only, not on how it is batched.
	a.stepLocked(5_000)
	for b.rv < a.rv { // batches end on operation boundaries, so b reaches exactly a.rv
		b.stepLocked(int(min(100, a.rv-b.rv)))
	}
	if a.rv != b.rv {
		t.Fatalf("resourceVersions %d and %d after the same changes", a.rv, b.rv)
	}
	if !sameStores(t, a, b) {
		t.Fatal("same seed, different churn")
	}
}

func sameStores(t *testing.T, a, b *Cluster) bool {
	t.Helper()
	for k := range numKinds {
		if len(a.store[k]) != len(b.store[k]) {
			return false
		}
		for key, oa := range a.store[k] {
			ob, ok := b.store[k][key]
			if !ok {
				return false
			}
			ja, err := json.Marshal(oa.Object)
			if err != nil {
				t.Fatal(err)
			}
			jb, _ := json.Marshal(ob.Object)
			if string(ja) != string(jb) {
				return false
			}
		}
	}
	return true
}

func TestChurnKeepsTheWorldConsistent(t *testing.T) {
	c := newTestCluster(t, Options{Seed: 3})
	initialTerminating := len(c.w.terminating)
	c.stepLocked(30_000)

	c.mu.Lock()
	defer c.mu.Unlock()
	if got, want := len(c.w.pods)-len(c.w.terminating), DefaultPods-initialTerminating; got != want {
		t.Errorf("pods without the terminating ones: %d, want %d", got, want)
	}
	if len(c.store[kindPods]) != len(c.w.pods) {
		t.Errorf("store has %d pods, world %d", len(c.store[kindPods]), len(c.w.pods))
	}
	if n := len(c.store[kindEvents]); n != DefaultEvents {
		t.Errorf("%d events, want %d", n, DefaultEvents)
	}
	if n := len(c.w.notReady); n < c.w.notReadyTarget-5 || n > c.w.notReadyTarget+5 {
		t.Errorf("%d pods not ready, target %d", n, c.w.notReadyTarget)
	}
	if len(c.w.terminating) == 0 || len(c.w.creating) == 0 {
		t.Errorf("terminating %d, creating %d: the replacement cycle does not run", len(c.w.terminating), len(c.w.creating))
	}

	// Once the deployment controller has caught up, every status matches the pods in the store.
	for len(c.w.dirty) > 0 {
		c.opDeploymentStatus(c.clock())
	}
	ready := map[string]int64{} // replica-set owner uid → ready, active pods
	for _, p := range c.store[kindPods] {
		if p.GetDeletionTimestamp() != nil {
			continue
		}
		conds, _, _ := unstructured.NestedSlice(p.Object, "status", "conditions")
		for _, cond := range conds {
			m := cond.(map[string]any)
			if m["type"] == "Ready" && m["status"] == condTrue {
				for _, ref := range p.GetOwnerReferences() {
					ready[string(ref.UID)]++
				}
			}
		}
	}
	for _, d := range c.w.deploys {
		obj := c.store[kindDeployments][objKey(d.namespace, d.name)]
		got, _, _ := unstructured.NestedInt64(obj.Object, "status", "readyReplicas")
		want := ready[d.rsUID[0]]
		if d.rsUID[1] != "" {
			want += ready[d.rsUID[1]]
		}
		if got != want {
			t.Errorf("deployment %s/%s: readyReplicas %d, ready pods %d", d.namespace, d.name, got, want)
		}
	}
}

func TestListIsASortedCopy(t *testing.T) {
	c := newTestCluster(t, Options{Seed: 1, Pods: 500, Deployments: 40, Services: 20, Events: 50})
	l := list(t, c, gvrOf(kindPods), "")
	if l.GetResourceVersion() != strconv.FormatInt(c.rv, 10) {
		t.Errorf("list resourceVersion %s, cluster %d", l.GetResourceVersion(), c.rv)
	}
	for i := 1; i < len(l.Items); i++ {
		a, b := l.Items[i-1], l.Items[i]
		if a.GetNamespace() > b.GetNamespace() || (a.GetNamespace() == b.GetNamespace() && a.GetName() >= b.GetName()) {
			t.Fatalf("not sorted: %s/%s before %s/%s", a.GetNamespace(), a.GetName(), b.GetNamespace(), b.GetName())
		}
	}
	first := &l.Items[0]
	first.SetLabels(map[string]string{"changed": "yes"})
	unstructured.RemoveNestedField(first.Object, "spec")
	again := list(t, c, gvrOf(kindPods), "")
	if again.Items[0].GetLabels()["changed"] != "" {
		t.Error("a listed object shares its labels with the cluster")
	}
	if _, ok := again.Items[0].Object["spec"]; !ok {
		t.Error("a listed object shares its map with the cluster")
	}

	scoped := list(t, c, gvrOf(kindPods), MCPNamespace)
	if len(scoped.Items) == 0 {
		t.Fatal("no pods in", MCPNamespace)
	}
	for _, it := range scoped.Items {
		if it.GetNamespace() != MCPNamespace {
			t.Fatalf("namespace-scoped list returned %s/%s", it.GetNamespace(), it.GetName())
		}
	}
}

func TestUnknownResourceIsNotFound(t *testing.T) {
	c := newTestCluster(t, Options{Seed: 1, Pods: 100, Deployments: 10, Services: 5, Events: 10})
	lw := lwc(c, schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, "")
	if _, err := lw.ListWithContext(context.Background(), metav1.ListOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("list: %v, want NotFound", err)
	}
	if _, err := lw.WatchWithContext(context.Background(), metav1.ListOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("watch: %v, want NotFound", err)
	}
}

func TestWatchResumesFromARecentResourceVersion(t *testing.T) {
	c := newTestCluster(t, Options{Seed: 4, Pods: 2_000, Deployments: 100, Services: 50, Events: 200})
	l := list(t, c, gvrOf(kindPods), "")
	state := map[string]string{} // key → resourceVersion
	for _, it := range l.Items {
		state[objKey(it.GetNamespace(), it.GetName())] = it.GetResourceVersion()
	}
	c.stepLocked(3_000)

	c.mu.Lock()
	changed := map[string]bool{}
	from, _ := strconv.ParseInt(l.GetResourceVersion(), 10, 64)
	c.history.since(from, func(ch *change) {
		if ch.kind == kindPods {
			changed[ch.key] = true
		}
	})
	c.mu.Unlock()
	if len(changed) == 0 {
		t.Fatal("the churn changed no pod")
	}

	w := startWatch(t, c, gvrOf(kindPods), "", l.GetResourceVersion(), false)
	last := from
	for _, ev := range receive(t, w, len(changed)) {
		u := ev.Object.(*unstructured.Unstructured)
		rv, _ := strconv.ParseInt(u.GetResourceVersion(), 10, 64)
		if rv <= last {
			t.Fatalf("resourceVersion %d after %d", rv, last)
		}
		last = rv
		key := objKey(u.GetNamespace(), u.GetName())
		switch ev.Type {
		case watch.Deleted:
			delete(state, key)
		case watch.Added, watch.Modified:
			state[key] = u.GetResourceVersion()
		default:
			t.Fatalf("unexpected %s event", ev.Type)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(state) != len(c.store[kindPods]) {
		t.Fatalf("replayed state has %d pods, cluster %d", len(state), len(c.store[kindPods]))
	}
	for key, o := range c.store[kindPods] {
		if state[key] != o.GetResourceVersion() {
			t.Fatalf("%s: replayed resourceVersion %s, cluster %s", key, state[key], o.GetResourceVersion())
		}
	}
}

func TestWatchStreamsLiveChangesInOrder(t *testing.T) {
	c := newTestCluster(t, Options{Seed: 5, Pods: 1_000, Deployments: 60, Services: 30, Events: 100})
	l := list(t, c, gvrOf(kindDeployments), "")
	w := startWatch(t, c, gvrOf(kindDeployments), "", l.GetResourceVersion(), false)
	before := c.rv
	c.stepLocked(2_000)
	c.mu.Lock()
	n := 0
	c.history.since(before, func(ch *change) {
		if ch.kind == kindDeployments {
			n++
		}
	})
	c.mu.Unlock()
	for _, ev := range receive(t, w, n) {
		if ev.Type != watch.Modified {
			t.Fatalf("deployment event %s, want MODIFIED: %#v", ev.Type, ev)
		}
	}
}

func TestWatchFromAnExpiredResourceVersionIsGone(t *testing.T) {
	c := newTestCluster(t, Options{Seed: 6, Pods: 500, Deployments: 40, Services: 20, Events: 50, History: 100})
	l := list(t, c, gvrOf(kindPods), "")
	c.stepLocked(500)
	w := startWatch(t, c, gvrOf(kindPods), "", l.GetResourceVersion(), false)
	ev, ok := <-w.ResultChan()
	if !ok || ev.Type != watch.Error {
		t.Fatalf("first event %v (open %v), want ERROR", ev.Type, ok)
	}
	err := apierrors.FromObject(ev.Object)
	if !apierrors.IsResourceExpired(err) || apierrors.ReasonForError(err) != metav1.StatusReasonExpired {
		t.Fatalf("error %v, want 410 Expired", err)
	}
	if _, open := <-w.ResultChan(); open {
		t.Error("the expired watch stays open")
	}
	if got := c.Stats().Expired; got != 1 {
		t.Errorf("Stats().Expired = %d, want 1", got)
	}
}

func TestWatchFromZeroSendsTheCurrentObjects(t *testing.T) {
	c := newTestCluster(t, Options{Seed: 1, Pods: 300, Deployments: 20, Services: 10, Events: 20})
	w := startWatch(t, c, gvrOf(kindNodes), "", "0", false)
	for _, ev := range receive(t, w, DefaultNodes) {
		if ev.Type != watch.Added {
			t.Fatalf("event %s, want ADDED", ev.Type)
		}
	}
}

func TestNamespaceScopedWatchFilters(t *testing.T) {
	c := newTestCluster(t, Options{Seed: 2, Pods: 2_000, Deployments: 100, Services: 40, Events: 200})
	l := list(t, c, gvrOf(kindPods), MCPNamespace)
	w := startWatch(t, c, gvrOf(kindPods), MCPNamespace, l.GetResourceVersion(), true)
	c.stepLocked(5_000)
	c.mu.Lock()
	c.sendBookmarks()
	c.mu.Unlock()
	for {
		ev := receive(t, w, 1)[0]
		u := ev.Object.(*unstructured.Unstructured)
		if ev.Type == watch.Bookmark {
			if u.GetResourceVersion() != strconv.FormatInt(c.rv, 10) {
				t.Errorf("bookmark at %s, cluster at %d", u.GetResourceVersion(), c.rv)
			}
			return
		}
		if u.GetNamespace() != MCPNamespace {
			t.Fatalf("event for %s/%s on a %s watch", u.GetNamespace(), u.GetName(), MCPNamespace)
		}
	}
}

func TestSlowWatcherIsClosed(t *testing.T) {
	c := newTestCluster(t, Options{Seed: 1, Pods: 1_000, Deployments: 60, Services: 30, Events: 100})
	c.queueLimit = 20
	l := list(t, c, gvrOf(kindPods), "")
	w := startWatch(t, c, gvrOf(kindPods), "", l.GetResourceVersion(), false)
	c.stepLocked(5_000) // far more pod events than the result buffer and the queue hold
	timeout := time.After(10 * time.Second)
	n := 0
	for open := true; open; {
		select {
		case _, open = <-w.ResultChan():
			n++
		case <-timeout:
			t.Fatal("the slow watcher was not closed")
		}
	}
	if n > resultBuffer+c.queueLimit+2 {
		t.Errorf("received %d events before the close", n)
	}
	if got := c.Stats().Dropped; got != 1 {
		t.Errorf("Stats().Dropped = %d, want 1", got)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.watchers[kindPods]) != 0 {
		t.Error("the closed watcher is still registered")
	}
}

func TestWatchStopsWithItsContext(t *testing.T) {
	c := newTestCluster(t, Options{Seed: 1, Pods: 300, Deployments: 20, Services: 10, Events: 20})
	ctx, cancel := context.WithCancel(context.Background())
	lw := c.Source().ListerWatcher(cache.Scope{GVR: gvrOf(kindPods)}).(*listerWatcher)
	w, err := lw.WatchWithContext(ctx, metav1.ListOptions{ResourceVersion: strconv.FormatInt(c.rv, 10)})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case _, open := <-w.ResultChan():
		if open {
			t.Error("event after cancel")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watch still open after its context ended")
	}
}

func TestObjectsCarryTheColumnFields(t *testing.T) {
	c := newTestCluster(t, Options{Seed: 1, Pods: 2_000, Deployments: 100, Services: 60, Events: 200})
	paths := map[kind][][]string{
		kindPods: {
			{"metadata", "ownerReferences"}, {"spec", "containers"}, {"spec", "nodeName"}, {"status", "phase"},
			{"status", "containerStatuses"}, {"status", "qosClass"}, {"status", "podIP"}, {"status", "conditions"},
		},
		kindDeployments: {{"spec", "replicas"}, {"status", "replicas"}, {"status", "readyReplicas"}, {"status", "conditions"}},
		kindServices:    {{"spec", "type"}, {"spec", "clusterIP"}, {"spec", "ports"}, {"spec", "selector"}},
		kindNodes: {
			{"status", "nodeInfo", "kubeletVersion"}, {"status", "conditions"}, {"status", "capacity", "cpu"},
			{"status", "allocatable", "memory"}, {"status", "addresses"}, {"metadata", "labels"},
		},
		kindNamespaces: {{"status", "phase"}, {"metadata", "labels"}},
		kindSecrets:    {{"type"}, {"data", "release"}, {"metadata", "labels"}, {"metadata", "uid"}},
		kindConfigMaps: {{"data", "release"}, {"metadata", "labels"}, {"metadata", "uid"}},
		kindEvents: {
			{"involvedObject", "kind"}, {"involvedObject", "name"}, {"reason"}, {"message"}, {"count"},
			{"type"}, {"source", "component"}, {"lastTimestamp"}, {"firstTimestamp"},
		},
	}
	for k, want := range paths {
		for _, path := range want {
			n := 0
			for _, o := range c.store[k] {
				if _, ok, _ := unstructured.NestedFieldNoCopy(o.Object, path...); ok {
					n++
				}
			}
			if n == 0 {
				t.Errorf("%s: no object has %v", kindInfo[k].kind, path)
			}
		}
		// Every object survives the deep copy informers and List make (JSON-compatible types only).
		for key, o := range c.store[k] {
			if cp := o.DeepCopy(); !reflect.DeepEqual(cp.Object, o.Object) {
				t.Fatalf("%s %s: deep copy differs", kindInfo[k].kind, key)
			}
		}
	}
}

func TestStatsCountsObjectsAndEvents(t *testing.T) {
	c := newTestCluster(t, Options{Seed: 1, Pods: 500, Deployments: 40, Services: 20, Events: 50})
	st := c.Stats()
	if st.Objects != 500+DefaultNodes+DefaultNamespaces+40+20+50+helmSecrets+helmConfigMaps {
		t.Errorf("Objects = %d", st.Objects)
	}
	n := c.stepLocked(100)
	if got := c.Stats().EventsSent; got != int64(n) {
		t.Errorf("EventsSent = %d, want %d", got, n)
	}
}

// The churn never changes Helm's release records: after it, every record has the resourceVersion
// the build gave it.
func TestChurnLeavesHelmRecordsAlone(t *testing.T) {
	c := newTestCluster(t, Options{Seed: 5, Pods: 1_000, Deployments: 50, Services: 20, Events: 100})
	snapshot := func() map[string]string {
		out := map[string]string{}
		for _, k := range []kind{kindSecrets, kindConfigMaps} {
			for key, o := range c.store[k] {
				out[kindInfo[k].kind+" "+key] = o.GetResourceVersion()
			}
		}
		return out
	}
	before := snapshot()
	if len(before) != helmSecrets+helmConfigMaps {
		t.Fatalf("%d release records, want %d", len(before), helmSecrets+helmConfigMaps)
	}
	c.stepLocked(20_000)
	c.mu.Lock()
	defer c.mu.Unlock()
	if after := snapshot(); !reflect.DeepEqual(after, before) {
		t.Errorf("release records after the churn %v, before %v", after, before)
	}
}

// The release records take nothing from the random generator: building them again leaves its
// state as it was, so the rest of the cluster and its churn are what they were before the records
// came. Their uids, made from their kind, namespace and name, are unique.
func TestHelmRecordsDrawNoRandomNumbers(t *testing.T) {
	c := newTestCluster(t, Options{Seed: 6, Pods: 300, Deployments: 20, Services: 10, Events: 30})
	c.mu.Lock()
	want := c.rng.Uint64()
	c.mu.Unlock()

	d := newTestCluster(t, Options{Seed: 6, Pods: 300, Deployments: 20, Services: 10, Events: 30})
	d.mu.Lock()
	defer d.mu.Unlock()
	d.buildHelmReleases(d.opts.Now) // a second set of records replaces the first
	if got := d.rng.Uint64(); got != want {
		t.Errorf("the generator moved: %d, want %d", got, want)
	}
	uids := map[string]string{}
	for _, k := range []kind{kindSecrets, kindConfigMaps} {
		for key, o := range d.store[k] {
			if other, dup := uids[string(o.GetUID())]; dup {
				t.Errorf("%s and %s share a uid", key, other)
			}
			uids[string(o.GetUID())] = key
		}
	}
}
