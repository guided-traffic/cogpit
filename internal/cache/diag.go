package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// What the cache tells Connection Diagnostics (D-133): how long each read took to load, how its
// initial list came (streamed by the server, listed, or listed after the server did not stream
// it), and the name its requests carry, so that one that waits at the connection's rate limit is
// noted under its scope. It sends no request of its own.

// DiagnosticKind is what a Diagnostic reports.
type DiagnosticKind string

// The kinds of Diagnostic.
const (
	// DiagStreamFallback: the server did not stream a read's initial list (client-go's WatchList),
	// and client-go listed it instead. Err is the stream's error; nil when the stream opened and
	// ended before its initial list was complete. A stream the cache ended because it stalled is
	// told through OnStall instead (stall.go).
	DiagStreamFallback DiagnosticKind = "streamFallback"
	// DiagLoaded: a read finished its initial list; Took is the time since its informer started.
	DiagLoaded DiagnosticKind = "loaded"
)

// Diagnostic is one thing a read did that Connection Diagnostics shows.
type Diagnostic struct {
	Kind  DiagnosticKind
	Scope Scope
	Took  time.Duration
	Err   error
}

// How a read's initial list came (InformerStats.Initial).
const (
	InitialStream   = "stream"   // the server streamed it (WatchList)
	InitialList     = "list"     // listed: the source does not stream (the demo cluster, tests)
	InitialFallback = "fallback" // listed after the server did not stream it
)

// loadPoll bounds how often a starting read is checked for having loaded: from loadPollFirst,
// doubling to loadPollMax, so a slow read costs few wake-ups and a fast one is timed closely.
const (
	loadPollFirst = 10 * time.Millisecond
	loadPollMax   = 500 * time.Millisecond
)

// OnDiagnostic registers fn to be told of each read's load and of every initial list the server
// did not stream; nil unregisters. fn runs on the cache's goroutines and must not block.
func (c *Cache) OnDiagnostic(fn func(Diagnostic)) {
	if fn == nil {
		c.diag.Store(nil)
		return
	}
	c.diag.Store(&fn)
}

// LabelRequests registers fn to put a read's name on the context of its lists and watches (the
// engine: cluster.WithRequestLabel with the scope); nil unregisters.
func (c *Cache) LabelRequests(fn func(context.Context, Scope) context.Context) {
	if fn == nil {
		c.label.Store(nil)
		return
	}
	c.label.Store(&fn)
}

// labelled is ctx with the runner's scope named on it, when the cache has a labeller.
func (r *runner) labelled(ctx context.Context) context.Context {
	if r.c == nil {
		return ctx
	}
	fn := r.c.label.Load()
	if fn == nil {
		return ctx
	}
	return (*fn)(ctx, r.scope)
}

// tellDiag hands a Diagnostic to the OnDiagnostic hook, recovering a panic in it.
func (r *runner) tellDiag(d Diagnostic) {
	if r.c == nil {
		return
	}
	fn := r.c.diag.Load()
	if fn == nil {
		return
	}
	defer func() {
		if p := recover(); p != nil {
			r.log.Error("cache: the diagnostics hook panicked", "scope", r.scope.String(), "panic", fmt.Sprint(p))
		}
	}()
	(*fn)(d)
}

// timeLoad waits for the informer's initial list and tells how long it took; it ends with ctx.
func (r *runner) timeLoad(ctx context.Context) {
	wait := loadPollFirst
	t := time.NewTimer(wait)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if r.synced() {
			// At least a nanosecond: 0 means still loading, and Windows' clock reads 0 for a load
			// within one of its ticks.
			took := max(time.Since(r.started), time.Nanosecond)
			r.loadedIn.Store(int64(took))
			r.tellDiag(Diagnostic{Kind: DiagLoaded, Scope: r.scope, Took: took})
			return
		}
		wait = min(2*wait, loadPollMax)
		t.Reset(wait)
	}
}

// watchAsked notes a watch the reflector opened: a streamed initial list (sendInitialEvents) the
// server answered, refused or failed, or a plain watch.
func (r *runner) watchAsked(ctx context.Context, opts metav1.ListOptions, err error) {
	if ctx.Err() != nil {
		return
	}
	streamed := opts.SendInitialEvents != nil && *opts.SendInitialEvents
	r.diagMu.Lock()
	defer r.diagMu.Unlock()
	switch {
	case !streamed:
		r.streaming = false
	case err != nil && (isRefused(err) || apierrors.IsUnauthorized(err)):
		r.streaming = false // the list that follows is refused too: a refusal, not a fallback
	default:
		r.streaming, r.streamErr = true, err
		if err == nil {
			r.initial = InitialStream
		}
	}
}

// listAsked notes a list the reflector makes: the first page of a list after a streamed initial
// list it asked for is a fallback. It returns how the initial list comes, for listed ("" for a
// later page), and the stall when the cache ended the stream because it stalled (stall.go): the
// list's answer is told with it (OnStall), not as a fallback of the server's.
func (r *runner) listAsked(ctx context.Context, opts metav1.ListOptions) (string, *StreamStallError) {
	if ctx.Err() != nil || opts.Continue != "" {
		return "", nil
	}
	r.diagMu.Lock()
	fell, cause := r.streaming, r.streamErr
	r.streaming, r.streamErr = false, nil
	r.diagMu.Unlock()
	if !fell {
		return InitialList, nil
	}
	var stall *StreamStallError
	switch {
	case errors.As(cause, &stall):
	case errors.Is(cause, errListing): // the cache lists: the stall that made it was told by its own read
	default:
		r.tellDiag(Diagnostic{Kind: DiagStreamFallback, Scope: r.scope, Err: cause})
	}
	return InitialFallback, stall
}

// listed notes how a list that worked came (listAsked's answer).
func (r *runner) listed(initial string) {
	if initial == "" {
		return
	}
	r.diagMu.Lock()
	r.initial = initial
	r.diagMu.Unlock()
}

// loadStats are a runner's load time (zero while it loads) and how its initial list came.
func (r *runner) loadStats() (time.Duration, string) {
	r.diagMu.Lock()
	defer r.diagMu.Unlock()
	return time.Duration(r.loadedIn.Load()), r.initial
}
