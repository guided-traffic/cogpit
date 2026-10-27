package metrics_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// fakeProvider counts the requests it gets, the way the cluster's counting transport would.
type fakeProvider struct {
	mu       sync.Mutex
	requests map[string]int
	seq      int // a measurement's age: each answer is one second newer
	err      error
	panics   bool
	base     time.Time
	hold     atomic.Pointer[chan struct{}] // while set, a pod's measurement waits for it to close
}

func newFake() *fakeProvider {
	return &fakeProvider{requests: map[string]int{}, base: time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)}
}

func (f *fakeProvider) ID() string                         { return "fake" }
func (f *fakeProvider) Label() string                      { return "Fake" }
func (f *fakeProvider) Capabilities() metrics.Capabilities { return metrics.Capabilities{} }

func (f *fakeProvider) count(verb string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[verb]
}

func (f *fakeProvider) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.requests {
		n += c
	}
	return n
}

func (f *fakeProvider) setErr(err error) {
	f.mu.Lock()
	f.err = err
	f.mu.Unlock()
}

// hit records a request and returns the error to fail with and the measurement's time.
func (f *fakeProvider) hit(verb string) (time.Time, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests[verb]++
	if f.panics {
		panic("provider bug")
	}
	f.seq++
	return f.base.Add(time.Duration(f.seq) * time.Second), f.seq, f.err
}

func (f *fakeProvider) pod(ns, name string, at time.Time, n int) model.PodUsage {
	return model.PodUsage{Namespace: ns, Name: name, Timestamp: at, Containers: []model.ContainerUsage{
		{Name: "app", Usage: model.Usage{CPUMilli: int64(10 * n), MemoryBytes: int64(n) << 20}},
		{Name: "sidecar", Usage: model.Usage{CPUMilli: 1, MemoryBytes: 1 << 10}},
	}}
}

func (f *fakeProvider) ListPods(_ context.Context, ns string) ([]model.PodUsage, error) {
	at, _, err := f.hit("list pods")
	if err != nil {
		return nil, err
	}
	out := []model.PodUsage{f.pod("web", "a", at, 1), f.pod("web", "b", at, 2), f.pod("db", "c", at, 3)}
	if ns == "" {
		return out, nil
	}
	var sel []model.PodUsage
	for _, p := range out {
		if p.Namespace == ns {
			sel = append(sel, p)
		}
	}
	return sel, nil
}

func (f *fakeProvider) GetPod(ctx context.Context, ns, name string) (model.PodUsage, error) {
	if hold := f.hold.Load(); hold != nil {
		select {
		case <-*hold:
		case <-ctx.Done():
			return model.PodUsage{}, ctx.Err()
		}
	}
	at, seq, err := f.hit("get pods")
	if err != nil {
		return model.PodUsage{}, err
	}
	if name == "gone" {
		return model.PodUsage{}, metrics.ErrNoData
	}
	return f.pod(ns, name, at, seq), nil
}

func (f *fakeProvider) ListNodes(context.Context) ([]model.NodeUsage, error) {
	at, _, err := f.hit("list nodes")
	if err != nil {
		return nil, err
	}
	return []model.NodeUsage{{Name: "n1", Timestamp: at, Usage: model.Usage{CPUMilli: 500, MemoryBytes: 1 << 30}}}, nil
}

func (f *fakeProvider) GetNode(_ context.Context, name string) (model.NodeUsage, error) {
	at, _, err := f.hit("get nodes")
	if err != nil {
		return model.NodeUsage{}, err
	}
	return model.NodeUsage{Name: name, Timestamp: at, Usage: model.Usage{CPUMilli: 500, MemoryBytes: 1 << 30}}, nil
}

// countingDetector counts discovery requests; set changes its answer.
type countingDetector struct {
	n    atomic.Int64
	mu   sync.Mutex
	prov metrics.Provider
	st   metrics.State
}

func (d *countingDetector) Detect(context.Context) (metrics.Provider, metrics.State) {
	d.n.Add(1)
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.prov, d.st
}

func (d *countingDetector) set(p metrics.Provider, st metrics.State) {
	d.mu.Lock()
	d.prov, d.st = p, st
	d.mu.Unlock()
}

func detectorFor(p metrics.Provider) *countingDetector {
	return &countingDetector{prov: p, st: metrics.State{Status: metrics.StatusAvailable, Provider: p.ID(), Label: p.Label()}}
}

// fast are short intervals. The tests that wait for polls or for nothing to happen run in a
// testing/synctest bubble: the hub's timers and the waits run on the bubble's clock, which moves
// only while every goroutine of the test waits.
func fast() metrics.Options {
	return metrics.Options{ListInterval: 30 * time.Millisecond, ObjectInterval: 10 * time.Millisecond, TTL: 10 * time.Millisecond}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

var (
	allPods = metrics.Scope{Kind: metrics.KindPods}
	podA    = metrics.Scope{Kind: metrics.KindPods, Namespace: "web", Name: "a"}
	allNode = metrics.Scope{Kind: metrics.KindNodes}
)

func TestNothingOpenNothingRequested(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFake()
		det := detectorFor(f)
		h := metrics.NewHub(det, fast())
		defer h.Close()
		time.Sleep(100 * time.Millisecond)
		if f.total() != 0 || det.n.Load() != 0 || h.Subscribed() != 0 {
			t.Errorf("an idle hub asked the cluster: %d requests, %d detections, %d scopes polled", f.total(), det.n.Load(), h.Subscribed())
		}
		// Reads ask nothing either.
		if _, ok := h.Pod("web", "a"); ok {
			t.Error("an idle hub knows a pod")
		}
		if st := h.State(allPods); st.Status != metrics.StatusUnknown {
			t.Errorf("state of an idle hub = %q, want unknown", st.Status)
		}
		if f.total() != 0 || det.n.Load() != 0 {
			t.Errorf("reads asked the cluster: %d requests, %d detections", f.total(), det.n.Load())
		}
	})
}

func TestPollsOnlyWhileSubscribed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFake()
		h := metrics.NewHub(detectorFor(f), fast())
		defer h.Close()

		sub := h.Subscribe(allPods, nil)
		eventually(t, "the first polls", func() bool { return f.count("list pods") >= 3 })
		if h.Subscribed() != 1 {
			t.Errorf("polled scopes = %d, want 1", h.Subscribed())
		}
		sub.Release()
		sub.Release()   // harmless
		synctest.Wait() // a poll under way at the release has ended
		stopped := f.total()
		time.Sleep(150 * time.Millisecond) // five list intervals
		if f.total() != stopped {
			t.Errorf("polls continued after the release: %d, then %d", stopped, f.total())
		}
		if h.Subscribed() != 0 {
			t.Errorf("polled scopes after the release = %d", h.Subscribed())
		}
	})
}

func TestOnePollServesEveryConsumerOfAScope(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFake()
		h := metrics.NewHub(detectorFor(f), fast())
		defer h.Close()

		var n1, n2 atomic.Int64
		s1 := h.Subscribe(allPods, func() { n1.Add(1) })
		s2 := h.Subscribe(allPods, func() { n2.Add(1) })
		defer s1.Release()
		defer s2.Release()
		eventually(t, "five polls", func() bool { return f.count("list pods") >= 5 })

		polls := int64(f.count("list pods"))
		if a, b := n1.Load(), n2.Load(); a < polls-1 || a > polls || b < polls-1 || b > polls {
			t.Errorf("%d polls, %d and %d notifications: each consumer hears every poll, and there is one poll per interval, not one per consumer", polls, a, b)
		}
		if h.Subscribed() != 1 {
			t.Errorf("polled scopes = %d, want 1", h.Subscribed())
		}
		// Releasing one consumer keeps the poll for the other.
		s1.Release()
		before := f.count("list pods")
		eventually(t, "polls after the first release", func() bool { return f.count("list pods") > before })
	})
}

func TestDetectionIsKeptAndShared(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFake()
		det := detectorFor(f)
		h := metrics.NewHub(det, fast())
		defer h.Close()
		s1 := h.Subscribe(allPods, nil)
		s2 := h.Subscribe(allNode, nil)
		s3 := h.Subscribe(podA, nil)
		defer s1.Release()
		defer s2.Release()
		defer s3.Release()
		eventually(t, "many polls", func() bool { return f.total() >= 12 })
		if n := det.n.Load(); n != 1 {
			t.Errorf("%d detections for %d requests, want 1 (kept for the detection TTL)", n, f.total())
		}
	})
}

func TestNotServedIsAStateAndAsksNothingElse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		det := &countingDetector{st: metrics.State{Status: metrics.StatusNotServed, Message: "the cluster serves no metrics.k8s.io API"}}
		opts := fast()
		opts.RetryDetectTTL = time.Hour
		h := metrics.NewHub(det, opts)
		defer h.Close()
		var polls atomic.Int64
		sub := h.Subscribe(allPods, func() { polls.Add(1) })
		defer sub.Release()
		eventually(t, "three polls", func() bool { return polls.Load() >= 3 })
		st := h.State(allPods)
		if st.Status != metrics.StatusNotServed || st.Usable() || st.Message == "" {
			t.Errorf("state = %+v", st)
		}
		if n := det.n.Load(); n != 1 {
			t.Errorf("%d detections while the API is missing, want 1 within the retry TTL", n)
		}
		if _, ok := h.Pod("web", "a"); ok {
			t.Error("a pod has metrics without a provider")
		}
	})
}

func TestMissingAPIIsDetectedAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		det := &countingDetector{st: metrics.State{Status: metrics.StatusNotServed, Message: "none"}}
		opts := fast()
		opts.RetryDetectTTL = 20 * time.Millisecond
		h := metrics.NewHub(det, opts)
		defer h.Close()
		sub := h.Subscribe(allPods, nil)
		defer sub.Release()
		eventually(t, "a second detection", func() bool { return det.n.Load() >= 2 })

		// The metrics-server gets installed.
		f := newFake()
		det.set(f, metrics.State{Status: metrics.StatusAvailable, Provider: "fake", Label: "Fake"})
		eventually(t, "polls after the install", func() bool { return f.count("list pods") > 0 })
		// The state is stored after the list returns: wait for it, not for the call (testing.md).
		eventually(t, "the state available", func() bool { return h.State(allPods).Usable() })
	})
}

func TestErrorsAreStates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFake()
		h := metrics.NewHub(detectorFor(f), fast())
		defer h.Close()
		f.setErr(apierrors.NewForbidden(schema.GroupResource{Group: "metrics.k8s.io", Resource: "pods"}, "", errors.New("denied")))
		sub := h.Subscribe(allPods, nil)
		defer sub.Release()
		eventually(t, "forbidden", func() bool { return h.State(allPods).Status == metrics.StatusForbidden })

		f.setErr(errors.New("connection reset"))
		eventually(t, "unavailable", func() bool { return h.State(allPods).Status == metrics.StatusUnavailable })

		f.setErr(nil)
		eventually(t, "recovery", func() bool { return h.State(allPods).Usable() })
		eventually(t, "data", func() bool { _, ok := h.Pod("web", "a"); return ok })
	})
}

func TestProviderPanicIsContained(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFake()
		f.panics = true
		h := metrics.NewHub(detectorFor(f), fast())
		defer h.Close()
		sub := h.Subscribe(allPods, nil)
		defer sub.Release()
		eventually(t, "a state for the panic", func() bool { return h.State(allPods).Status == metrics.StatusUnavailable })
	})
}

// panicky is a provider with history and the nodes' disks whose range queries or disk list
// panic, as one with a bug would.
type panicky struct {
	*fakeProvider
	rangePanics, diskPanics bool
}

func (p panicky) Capabilities() metrics.Capabilities { return metrics.Capabilities{History: true} }

func (p panicky) ObjectRange(context.Context, metrics.RangeQuery) (*metrics.ObjectSeries, error) {
	if p.rangePanics {
		panic("provider bug")
	}
	return &metrics.ObjectSeries{Targets: map[string]*metrics.TargetSeries{"": {}}}, nil
}

func (p panicky) ListNodeDisks(context.Context) ([]model.NodeDisk, error) {
	if p.diskPanics {
		panic("provider bug")
	}
	return nil, nil
}

// A panic at each of the hub's other boundaries (a detection, a range query, the disks' list, the
// detection DetectUnknown starts) is a state: unavailable, with a class. The scope's polls go on,
// and Close returns (in the bubble a goroutine left behind fails the test).
func TestPanicsAreStatesAtEveryBoundary(t *testing.T) {
	panics := metrics.DetectorFunc(func(context.Context) (metrics.Provider, metrics.State) { panic("detector bug") })
	opts := fast()
	opts.MinStep, opts.RangeWindow = 5*time.Millisecond, 600*time.Millisecond // a drawer of a provider with history polls at its step
	for _, tc := range []struct {
		name  string
		det   metrics.Detector
		scope metrics.Scope
	}{
		{"detection", panics, allPods},
		{"range query", detectorFor(panicky{fakeProvider: newFake(), rangePanics: true}), podA},
		{"disks", detectorFor(panicky{fakeProvider: newFake(), diskPanics: true}), metrics.DiskScope},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				h := metrics.NewHub(tc.det, opts)
				var polls atomic.Int64
				sub := h.Subscribe(tc.scope, func() { polls.Add(1) })
				eventually(t, "two polls", func() bool { return polls.Load() >= 2 })
				if st := h.State(tc.scope); st.Status != metrics.StatusUnavailable || st.Reason == "" {
					t.Errorf("state = %+v, want unavailable with a class", st)
				}
				sub.Release()
				h.Close()
			})
		})
	}
	t.Run("DetectUnknown", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			h := metrics.NewHub(panics, opts)
			done := make(chan struct{})
			if !h.DetectUnknown(func() { close(done) }) {
				t.Fatal("no detection started")
			}
			<-done
			if ps := h.Providers(); len(ps) != 1 || ps[0].Status != metrics.StatusUnavailable || ps[0].LastError == "" {
				t.Errorf("providers = %+v, want the one unavailable with a class", ps)
			}
			h.Close()
		})
	})
}

func TestReadsServeTheFreshestAndExpire(t *testing.T) {
	f := newFake()
	var now atomic.Int64
	clock := func() time.Time { return f.base.Add(time.Duration(now.Load())) }
	opts := fast()
	opts.ListInterval = time.Hour // the poll happens once, at the start
	opts.TTL = 15 * time.Second
	opts.Now = clock
	h := metrics.NewHub(detectorFor(f), opts)
	defer h.Close()

	sub := h.Subscribe(allPods, nil)
	eventually(t, "the poll", func() bool { _, ok := h.Pod("web", "a"); return ok })
	p, _ := h.Pod("web", "a")
	if p.Total().CPUMilli != 11 {
		t.Errorf("pod a = %+v", p)
	}
	// While polled, a result is current for two intervals; the clock moves an hour without a poll.
	now.Store(int64(time.Hour))
	if _, ok := h.Pod("web", "a"); !ok {
		t.Error("a polled result expired within two intervals")
	}
	now.Store(int64(3 * time.Hour))
	if _, ok := h.Pod("web", "a"); ok {
		t.Error("a polled result outlived two intervals")
	}
	sub.Release()
	now.Store(int64(3*time.Hour + time.Second))
	// Not polled any more: the TTL applies (the result is three hours old).
	if _, ok := h.Pod("web", "a"); ok {
		t.Error("an unpolled stale result was served")
	}
}

func TestFetchCachesForTheTTL(t *testing.T) {
	f := newFake()
	var now atomic.Int64
	opts := fast()
	opts.TTL = 15 * time.Second
	opts.Now = func() time.Time { return f.base.Add(time.Duration(now.Load())) }
	h := metrics.NewHub(detectorFor(f), opts)
	defer h.Close()
	ctx := context.Background()

	r := h.Fetch(ctx, metrics.Scope{Kind: metrics.KindPods, Namespace: "web"})
	if !r.State.Usable() || len(r.Pods) != 2 || r.Pods[0].Name != "a" {
		t.Fatalf("namespace web = %+v", r)
	}
	h.Fetch(ctx, metrics.Scope{Kind: metrics.KindPods, Namespace: "web"})
	if f.count("list pods") != 1 {
		t.Errorf("a second read within the TTL sent a request: %d", f.count("list pods"))
	}
	now.Store(int64(16 * time.Second))
	h.Fetch(ctx, metrics.Scope{Kind: metrics.KindPods, Namespace: "web"})
	if f.count("list pods") != 2 {
		t.Errorf("a read after the TTL sent %d requests in all, want 2", f.count("list pods"))
	}
	// One pod is served from the namespace list that is fresh.
	r = h.Fetch(ctx, podA)
	if len(r.Pods) != 1 || r.Pods[0].Name != "a" || f.count("get pods") != 0 {
		t.Errorf("one pod: %+v after %d gets, want it from the fresh list", r.Pods, f.count("get pods"))
	}
	// A pod nobody measured is a result without data, not an error.
	r = h.Fetch(ctx, metrics.Scope{Kind: metrics.KindPods, Namespace: "web", Name: "gone"})
	if !r.State.Usable() || len(r.Pods) != 0 {
		t.Errorf("a pod without metrics = %+v", r)
	}
	if h.Subscribed() != 0 {
		t.Error("a one-off read left a poll running")
	}
}

func TestSubscribeOnAFreshResultWaitsForItToGoStale(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFake()
		opts := fast()
		opts.TTL = 300 * time.Millisecond
		opts.ListInterval = 20 * time.Millisecond
		h := metrics.NewHub(detectorFor(f), opts)
		defer h.Close()
		h.Fetch(context.Background(), allNode)
		if f.count("list nodes") != 1 {
			t.Fatal("the fetch sent no request")
		}
		sub := h.Subscribe(allNode, nil)
		defer sub.Release()
		time.Sleep(150 * time.Millisecond)
		if n := f.count("list nodes"); n != 1 {
			t.Errorf("%d requests 150 ms after a fetch with a 300 ms TTL, want the one", n)
		}
		eventually(t, "the poll after the TTL", func() bool { return f.count("list nodes") >= 2 })
	})
}

func TestSeriesWhileWatched(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFake()
		opts := fast()
		opts.SeriesPoints = 5
		h := metrics.NewHub(detectorFor(f), opts)
		defer h.Close()

		if got := h.Series(podA); got != nil {
			t.Errorf("series of an unwatched pod = %v", got)
		}
		sub := h.Subscribe(podA, nil)
		eventually(t, "twelve polls", func() bool { return f.count("get pods") >= 12 })
		series := h.Series(podA)
		if len(series) != 3 || series[0].Container != "" || series[1].Container != "app" || series[2].Container != "sidecar" {
			t.Fatalf("series = %+v, want the pod, then its containers by name", series)
		}
		for _, s := range series {
			if len(s.Samples) > 5 {
				t.Errorf("series %q holds %d samples, the bound is 5", s.Container, len(s.Samples))
			}
			for i := 1; i < len(s.Samples); i++ {
				if !s.Samples[i].At.After(s.Samples[i-1].At) {
					t.Errorf("series %q is not in time order: %v", s.Container, s.Samples)
				}
			}
		}
		// The pod's line is the sum of its containers'.
		last := len(series[0].Samples) - 1
		if got, want := series[0].Samples[last].CPUMilli, series[1].Samples[last].CPUMilli+series[2].Samples[last].CPUMilli; got != want {
			t.Errorf("pod cpu %d != containers' %d", got, want)
		}
		sub.Release()
		if got := h.Series(podA); got != nil {
			t.Errorf("series after the last release = %v, want none", got)
		}
	})
}

func TestSeriesIsSeededFromAFreshList(t *testing.T) {
	f := newFake()
	opts := fast()
	opts.ObjectInterval = time.Hour
	opts.ListInterval = time.Hour
	opts.TTL = time.Hour
	h := metrics.NewHub(detectorFor(f), opts)
	defer h.Close()
	list := h.Subscribe(allPods, nil)
	defer list.Release()
	eventually(t, "the list", func() bool { _, ok := h.Pod("web", "a"); return ok })

	// The drawer's own first poll is held until the checks: on a slow runner it can otherwise
	// answer before them (CI 2026-10-14) and add a second point, which is not the seed's.
	held := make(chan struct{})
	f.hold.Store(&held)
	defer close(held)
	sub := h.Subscribe(podA, nil)
	defer sub.Release()
	series := h.Series(podA)
	if len(series) == 0 || len(series[0].Samples) != 1 {
		t.Fatalf("a new drawer's chart starts with the list's measurement, got %+v", series)
	}
	if f.count("get pods") != 0 {
		t.Error("the seed cost a request")
	}
}

func TestSeriesBounds(t *testing.T) {
	at := func(m int) time.Time {
		return time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC).Add(time.Duration(m) * time.Minute)
	}
	var pts []model.UsageSample
	for m := 0; m < 100; m++ {
		pts = metrics.AddSample(pts, model.UsageSample{At: at(m)}, 1000, time.Hour)
	}
	if len(pts) != 61 || !pts[0].At.Equal(at(39)) {
		t.Errorf("age bound: %d points from %v, want 61 from minute 39", len(pts), pts[0].At)
	}
	pts = nil
	for m := 0; m < 100; m++ {
		pts = metrics.AddSample(pts, model.UsageSample{At: at(m)}, 7, 24*time.Hour)
	}
	if len(pts) != 7 || !pts[6].At.Equal(at(99)) || !pts[0].At.Equal(at(93)) {
		t.Errorf("count bound: %d points", len(pts))
	}
	before := len(pts)
	pts = metrics.AddSample(pts, model.UsageSample{At: at(99)}, 7, 24*time.Hour)
	pts = metrics.AddSample(pts, model.UsageSample{At: at(50)}, 7, 24*time.Hour)
	if len(pts) != before {
		t.Error("a sample that is not newer was added")
	}
}

func TestCloseStopsEverything(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFake()
		h := metrics.NewHub(detectorFor(f), fast())
		h.Subscribe(allPods, nil)
		h.Subscribe(podA, nil)
		eventually(t, "polls", func() bool { return f.total() >= 4 })
		h.Close()
		n := f.total()
		time.Sleep(80 * time.Millisecond)
		if f.total() != n {
			t.Error("polls continued after Close")
		}
		h.Subscribe(allPods, nil) // inert
		time.Sleep(50 * time.Millisecond)
		if f.total() != n {
			t.Error("a subscription after Close polled")
		}
	})
}
