package cache

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	toolscache "k8s.io/client-go/tools/cache"
)

// Stalled streams (D-135). client-go streams a read's initial list (WatchList: a watch with
// sendInitialEvents) and waits, without a bound, for the bookmark that ends it. A healthy API
// server sends the objects from its watch cache and that bookmark at once (on a 1.37 cluster, 22 to
// 758 ms for up to 261 objects, the bookmark right after the last object); on some clusters the
// stream goes silent before its end, for a minute and longer, while a list of the same objects
// answers at once (an OpenShift cluster, 2026-10-26: 17 s to over 2 min per read).
//
// The cache bounds that silence: a streamed initial list that receives nothing for the stall bound
// (Options.StreamStall) before its end is ended, client-go lists in its place, and from then on
// every read of the cache lists and then watches, as a server without WatchList is read: the
// reads still waiting for their streams end at once, and later reads ask for no stream. OnStall
// tells of the stall once the list in its place answered, so that the engine can tell a stream
// that stalls from a connection that does (a quick list) and keep the cluster listing
// (Options.Lists, ListFromNowOn: its Cluster Settings).

// DefaultStreamStall is how long a streamed initial list may receive nothing (no answer to its
// request, no object, no bookmark) before its end.
const DefaultStreamStall = 10 * time.Second

// StreamStallError is why a read's streamed initial list was ended: it received nothing for
// Silent before its end.
type StreamStallError struct {
	Silent time.Duration
	// Answered says the server had answered the request (the stream had begun); Objects counts the
	// objects it sent before it went silent.
	Answered bool
	Objects  int
}

func (e *StreamStallError) Error() string {
	if !e.Answered {
		return fmt.Sprintf("the server did not answer the stream's request within %s", e.Silent)
	}
	objects := "objects"
	if e.Objects == 1 {
		objects = "object"
	}
	return fmt.Sprintf("%d %s received, then nothing for %s before the end of the initial list", e.Objects, objects, e.Silent)
}

// errListing ends a read's stream, or refuses it before it is asked for, once the cache lists:
// another read's stream stalled, or the cluster's streams are off. It is the cache's own, never
// the server's.
var errListing = errors.New("this cache lists from now on")

// Stall is a read whose stream the cache ended because it stalled, and how the list client-go
// made in its place answered: Listed is how long its first page took, ListErr why it failed.
type Stall struct {
	Scope   Scope
	Err     *StreamStallError
	Listed  time.Duration
	ListErr error
}

// OnStall registers fn to be told of every read whose stream stalled, once the list in its place
// answered; nil unregisters. fn runs on the informer's goroutine and must not block.
func (c *Cache) OnStall(fn func(Stall)) {
	if fn == nil {
		c.stall.Store(nil)
		return
	}
	c.stall.Store(&fn)
}

// tellStall hands a stall to the OnStall hook, recovering a panic in it.
func (r *runner) tellStall(st Stall) {
	if r.c == nil {
		return
	}
	fn := r.c.stall.Load()
	if fn == nil {
		return
	}
	defer func() {
		if p := recover(); p != nil {
			r.log.Error("cache: the stall hook panicked", "scope", r.scope.String(), "panic", fmt.Sprint(p))
		}
	}()
	(*fn)(st)
}

// streamStall is the cache's stall bound.
func (c *Cache) streamStall() time.Duration {
	if c.opts.StreamStall > 0 {
		return c.opts.StreamStall
	}
	return DefaultStreamStall
}

// Listing reports whether the cache lists every read's initial list: since a stream stalled, since
// ListFromNowOn, or from the start (Options.Lists).
func (c *Cache) Listing() bool { return c.lists.Load() }

// ListFromNowOn makes every read of the cache list from now on, never stream: the streams still
// running end at once (the user switched the cluster's streams off).
func (c *Cache) ListFromNowOn() {
	if c.lists.CompareAndSwap(false, true) {
		c.listNow()
	}
}

// listing reports whether r's cache lists from now on.
func (r *runner) listing() bool { return r.c != nil && r.c.lists.Load() }

// isStreamed reports whether a watch asks for a streamed initial list.
func isStreamed(opts metav1.ListOptions) bool {
	return opts.SendInitialEvents != nil && *opts.SendInitialEvents
}

// watchStream opens a streamed initial list with the stall bound on it: the request's answer and
// then each event must come within it until the bookmark that ends the initial list. Once the cache
// lists, it asks for no stream: client-go lists instead.
func (t *trackedLW) watchStream(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	r := t.r
	if r.c == nil {
		w, err := toolscache.ToWatcherWithContext(t.inner).WatchWithContext(ctx, opts)
		r.watchAsked(ctx, opts, err)
		t.observe(ctx, err)
		return w, err
	}
	if r.listing() {
		r.notStreamed()
		return nil, errListing
	}
	bound := r.c.streamStall()
	reqCtx, cancel := context.WithCancelCause(ctx)
	timer := time.AfterFunc(bound, func() { cancel(&StreamStallError{Silent: bound}) })
	stopListing := context.AfterFunc(r.c.listCtx, func() { cancel(errListing) })
	w, err := toolscache.ToWatcherWithContext(t.inner).WatchWithContext(reqCtx, opts)
	timer.Stop()
	stopListing()
	if cause := context.Cause(reqCtx); cause != nil && ctx.Err() == nil {
		// Ended here, not by the informer: before the answer, or in the instant after it.
		if w != nil {
			w.Stop()
		}
		cancel(nil)
		r.streamEnded(cause)
		return nil, cause
	}
	if err == nil && w == nil {
		err = errors.New("the source returned no watch")
	}
	r.watchAsked(ctx, opts, err)
	t.observe(ctx, err)
	if err != nil {
		cancel(nil)
		return nil, err
	}
	s := &stallWatch{inner: w, result: make(chan watch.Event), done: make(chan struct{}), cancel: func() { cancel(nil) }}
	go s.run(r, bound)
	return s, nil
}

// notStreamed notes that a read lists because its cache lists: not a fallback of its own.
func (r *runner) notStreamed() {
	r.diagMu.Lock()
	r.streaming, r.streamErr = false, nil
	r.diagMu.Unlock()
}

// streamEnded notes why the cache ended a read's stream, for the list that follows (listAsked); a
// stall of its own makes the cache list from now on.
func (r *runner) streamEnded(cause error) {
	r.diagMu.Lock()
	r.streaming, r.streamErr = true, cause
	r.diagMu.Unlock()
	var stall *StreamStallError
	if errors.As(cause, &stall) {
		r.log.Warn("cache: the streamed initial list stalled; it and every read after it list", "err", cause)
		r.c.ListFromNowOn()
	}
}

// stallWatch passes a streamed initial list's events through and ends it, with an error event that
// makes client-go list instead, when nothing arrives for the stall bound before the bookmark that
// ends the initial list, or when the cache lists from now on. After that bookmark it only passes
// events through: client-go keeps the stream as the read's watch.
type stallWatch struct {
	inner  watch.Interface
	result chan watch.Event
	done   chan struct{}
	once   sync.Once
	cancel func() // ends the request's context
}

func (s *stallWatch) ResultChan() <-chan watch.Event { return s.result }

func (s *stallWatch) Stop() {
	s.once.Do(func() {
		close(s.done)
		s.inner.Stop()
		s.cancel()
	})
}

func (s *stallWatch) run(r *runner, bound time.Duration) {
	defer close(s.result)
	defer s.Stop()
	in := s.inner.ResultChan()
	timer := time.NewTimer(bound)
	defer timer.Stop()
	objects, ended := 0, false
	for {
		var stall <-chan time.Time
		var listing <-chan struct{}
		if !ended {
			stall, listing = timer.C, r.c.listCtx.Done()
		}
		select {
		case <-s.done:
			return
		case ev, ok := <-in:
			if !ok {
				return
			}
			if !ended {
				switch {
				case isInitialEventsEnd(ev):
					ended = true
				case ev.Type == watch.Added || ev.Type == watch.Modified || ev.Type == watch.Deleted:
					objects++
				}
			}
			select {
			case s.result <- ev:
			case <-s.done:
				return
			}
			if !ended {
				timer.Reset(bound) // silence counts from when the event was taken
			}
		case <-stall:
			s.end(r, &StreamStallError{Silent: bound, Answered: true, Objects: objects})
			return
		case <-listing:
			s.end(r, errListing)
			return
		}
	}
}

// end stops the stream and hands client-go an error event, which it does not retry: it lists.
func (s *stallWatch) end(r *runner, cause error) {
	r.streamEnded(cause)
	s.inner.Stop()
	ev := watch.Event{Type: watch.Error, Object: &metav1.Status{
		Status: metav1.StatusFailure, Code: http.StatusGatewayTimeout, Reason: metav1.StatusReasonTimeout,
		Message: "the cache ended the streamed initial list: " + cause.Error(),
	}}
	select {
	case s.result <- ev:
	case <-s.done:
	}
}

// isInitialEventsEnd reports whether an event is the bookmark that ends a streamed initial list.
func isInitialEventsEnd(ev watch.Event) bool {
	if ev.Type != watch.Bookmark || ev.Object == nil {
		return false
	}
	m, ok := ev.Object.(metav1.Object)
	return ok && m.GetAnnotations()[metav1.InitialEventsAnnotationKey] == "true"
}
