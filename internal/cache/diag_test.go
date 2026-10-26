package cache_test

import (
	"context"
	"sync"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	toolscache "k8s.io/client-go/tools/cache"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// streamMode is how a streamSource answers a streamed initial list (sendInitialEvents).
type streamMode int

const (
	streamServed  streamMode = iota // the objects, then the bookmark that ends the initial list
	streamInvalid                   // a 400, as a server without WatchList answers
	streamRefused                   // a 403
)

// streamSource is a cluster's source that says it streams initial lists, answering them as mode
// says; it records the label on each request's context.
type streamSource struct {
	mode streamMode

	mu     sync.Mutex
	labels []string
	fakes  []*watch.FakeWatcher
}

type labelKey struct{}

func (s *streamSource) ListerWatcher(cache.Scope) toolscache.ListerWatcher { return &streamLW{s: s} }

func (s *streamSource) note(ctx context.Context) {
	l, _ := ctx.Value(labelKey{}).(string)
	s.mu.Lock()
	s.labels = append(s.labels, l)
	s.mu.Unlock()
}

func (s *streamSource) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.fakes {
		f.Stop()
	}
}

type streamLW struct{ s *streamSource }

func (lw *streamLW) List(opts metav1.ListOptions) (runtime.Object, error) {
	return lw.ListWithContext(context.Background(), opts)
}

func (lw *streamLW) Watch(opts metav1.ListOptions) (watch.Interface, error) {
	return lw.WatchWithContext(context.Background(), opts)
}

func (lw *streamLW) ListWithContext(ctx context.Context, _ metav1.ListOptions) (runtime.Object, error) {
	lw.s.note(ctx)
	if lw.s.mode == streamRefused {
		return nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", nil)
	}
	l := &unstructured.UnstructuredList{Object: map[string]any{"apiVersion": "v1", "kind": "List"}}
	l.SetResourceVersion("5")
	l.Items = []unstructured.Unstructured{*testkit.Pod("default", "web-0")}
	return l, nil
}

func (lw *streamLW) WatchWithContext(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	lw.s.note(ctx)
	streamed := opts.SendInitialEvents != nil && *opts.SendInitialEvents
	switch {
	case streamed && lw.s.mode == streamInvalid:
		return nil, apierrors.NewBadRequest("sendInitialEvents is forbidden for watch unless the WatchList feature gate is enabled")
	case streamed && lw.s.mode == streamRefused:
		return nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", nil)
	}
	f := watch.NewFakeWithChanSize(4, false)
	lw.s.mu.Lock()
	lw.s.fakes = append(lw.s.fakes, f)
	lw.s.mu.Unlock()
	if streamed {
		pod := testkit.Pod("default", "web-0")
		pod.SetResourceVersion("5")
		f.Add(pod)
		end := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Pod"}}
		end.SetResourceVersion("5")
		end.SetAnnotations(map[string]string{metav1.InitialEventsAnnotationKey: "true"})
		f.Action(watch.Bookmark, end)
	}
	return f, nil
}

func TestDiagnosticsOfARead(t *testing.T) {
	tests := []struct {
		name     string
		mode     streamMode
		initial  string
		fallback bool
		loads    bool
	}{
		{name: "streamed", mode: streamServed, initial: cache.InitialStream, loads: true},
		{name: "server without WatchList", mode: streamInvalid, initial: cache.InitialFallback, fallback: true, loads: true},
		{name: "refused", mode: streamRefused},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := &streamSource{mode: tc.mode}
			c := cache.New(src, cache.Options{})
			defer src.stop()
			defer c.Close()
			c.LabelRequests(func(ctx context.Context, s cache.Scope) context.Context {
				return context.WithValue(ctx, labelKey{}, s.String())
			})
			var mu sync.Mutex
			var got []cache.Diagnostic
			c.OnDiagnostic(func(d cache.Diagnostic) {
				mu.Lock()
				got = append(got, d)
				mu.Unlock()
			})
			scope := cache.Scope{GVR: testkit.PodsGVR, Namespace: "default"}
			h := c.Acquire(scope)
			defer h.Release()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if !tc.loads {
				waitFor(t, func() bool { return h.Err() != nil })
				time.Sleep(50 * time.Millisecond)
			} else if !h.WaitForSync(ctx) {
				t.Fatal("not synced")
			}
			// The load is told by the poll after the sync.
			if tc.loads {
				waitFor(t, func() bool {
					mu.Lock()
					defer mu.Unlock()
					for _, d := range got {
						if d.Kind == cache.DiagLoaded {
							return true
						}
					}
					return false
				})
			}
			mu.Lock()
			var fell, loaded int
			for _, d := range got {
				if d.Scope != scope {
					t.Errorf("diagnostic of %v, want %v", d.Scope, scope)
				}
				switch d.Kind {
				case cache.DiagStreamFallback:
					fell++
					if d.Err == nil || !apierrors.IsBadRequest(d.Err) {
						t.Errorf("fallback cause = %v, want the server's 400", d.Err)
					}
				case cache.DiagLoaded:
					loaded++
					if d.Took <= 0 {
						t.Errorf("loaded in %v", d.Took)
					}
				}
			}
			mu.Unlock()
			if (fell > 0) != tc.fallback {
				t.Errorf("fallbacks = %d, want one: %v", fell, tc.fallback)
			}
			if loaded > 1 {
				t.Errorf("loaded told %d times", loaded)
			}
			st := c.Stats()
			if len(st) != 1 {
				t.Fatalf("stats = %+v", st)
			}
			if st[0].Initial != tc.initial || (tc.loads && st[0].LoadedIn <= 0) || (!tc.loads && st[0].LoadedIn != 0) {
				t.Errorf("stats = initial %q, loaded in %v; want %q", st[0].Initial, st[0].LoadedIn, tc.initial)
			}
			src.mu.Lock()
			for _, l := range src.labels {
				if l != "pods in default" {
					t.Errorf("a request's label = %q, want the scope", l)
				}
			}
			src.mu.Unlock()
		})
	}
}
