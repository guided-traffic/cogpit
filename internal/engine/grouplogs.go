package engine

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// A group's logs (A81, the owner's decision of 2026-10-14 evening): an application's logs in one
// merged tab, each line starting with "<workload>/<pod>/<container> ", with a filter per source.
// The groups are the objects that name the workloads they deploy, read from the cache: an
// application of the Applications view (its workloads by the app.kubernetes.io/instance label),
// an Argo CD Application (its status.resources) and a Flux Kustomization (its status.inventory).
// A Flux Helm release names no workloads in its status, so it is no group (helm-controller labels
// what it deploys, which a query over every workload kind would have to find).
//
// The stream is one workload stream per workload (workloadlogs.go), all writing into one sink,
// each with its share of the tab's bounds; the group's object is watched through the cache, so a
// workload that joins the group joins the stream and one that leaves it ends. A tab narrows to
// one workload (LogOptions.Workload), and within it to a pod and a container as a workload's tab
// does; the narrowing follows in the running streams.

// Bounds of a group's logs.
const (
	// maxGroupWorkloads bounds the workloads a group's tab streams; the tab says when it leaves
	// some out.
	maxGroupWorkloads = 20
	// minGroupStreams is the fewest containers each workload streams at once, whatever its share.
	minGroupStreams = 2
)

// groupMembers resolve a group's workloads from its cached object, by the group's kind key.
var groupMembers = map[string]func(*unstructured.Unstructured) []model.ObjectRef{
	kinds.ApplicationsKey:                        kinds.ApplicationWorkloads,
	kinds.ArgoApplicationsKey:                    kinds.ArgoAppWorkloads,
	"kustomizations.kustomize.toolkit.fluxcd.io": kinds.FluxInventoryWorkloads,
}

// IsLogGroup reports whether a logs tab of that kind shows the logs of the workloads its object
// groups (an application's) rather than one workload's or one pod's.
func IsLogGroup(kind string) bool {
	_, ok := groupMembers[kind]
	return ok
}

// LogGroupWorkloads returns the workloads of a group's object whose logs its tab shows: those of
// the kinds a workload's tab serves (WorkloadLogKinds), in the group's order, the context the
// group's.
func LogGroupWorkloads(group model.ObjectRef, obj *unstructured.Unstructured) []model.ObjectRef {
	members := groupMembers[group.Kind]
	if members == nil {
		return nil
	}
	var out []model.ObjectRef
	for _, w := range members(obj) {
		if slices.Contains(WorkloadLogKinds, w.Kind) {
			w.Context = group.Context
			out = append(out, w)
		}
	}
	return out
}

// groupBounds is each workload's share of a tab's bounds when n workloads stream.
func groupBounds(n int) workloadBounds {
	n = max(n, 1)
	return workloadBounds{streams: max(minGroupStreams, workloadLogStreams/n), tail: max(workloadLogMinTail, workloadLogTail/n)}
}

// groupLogs is the merged stream of a group's workloads. Read returns its lines; Close ends every
// workload's stream and releases the cache handle on the group.
type groupLogs struct {
	s        *Session
	tab      DockTab
	ref      model.ObjectRef
	p        *model.KindProfile
	b        *Backend
	streamer logStreamer
	h        *cache.Handle // the group's kind in its namespace

	ctx    context.Context
	cancel context.CancelFunc
	sink   *logSink
	pr     *io.PipeReader
	wake   chan struct{}
	stop   chan struct{}
	unsub  func()

	mu      sync.Mutex
	members map[model.ObjectRef]*workloadLogs // by the workload's reference
	failed  map[model.ObjectRef]bool          // workloads whose stream did not open: said once
	capped  bool

	closeOnce sync.Once
}

// openGroupLogs opens the merged logs of the workloads a group (the tab's object) deploys.
func (s *Session) openGroupLogs(ctx context.Context, tab DockTab, b *Backend, streamer logStreamer) (io.ReadCloser, error) {
	ref := *tab.Ref
	p, ok := b.Profile(ref.Kind)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownKind, ref.Kind)
	}
	h, err := b.readFollowing(ctx, p, p.Key, b.inNamespace(p, ref.Namespace))
	if err != nil {
		return nil, err
	}
	obj, ok := KindGet(h, p, ref.Namespace, ref.Name)
	if !ok {
		h.Release()
		return nil, fmt.Errorf("%s: %w", ref.String(), ErrNotFound)
	}
	pr, pw := io.Pipe()
	g := &groupLogs{
		s: s, tab: tab, ref: ref, p: p, b: b, streamer: streamer, h: h, sink: &logSink{pw: pw}, pr: pr,
		wake: make(chan struct{}, 1), stop: make(chan struct{}),
		members: map[model.ObjectRef]*workloadLogs{}, failed: map[model.ObjectRef]bool{},
	}
	g.ctx, g.cancel = context.WithCancel(ctx)
	workloads := g.capWorkloads(LogGroupWorkloads(ref, obj))
	if len(workloads) == 0 {
		g.notice(fmt.Sprintf("%s %s %s has no workloads whose logs Cogpit shows yet; theirs show here when it has", NoticePrefix, p.Kind, ref.Name))
	}
	if tab.Logs.Previous {
		// The previous instances' logs are complete: the stream ends after every workload's.
		var done sync.WaitGroup
		done.Add(len(workloads))
		for _, w := range workloads {
			g.startMember(w, groupBounds(len(workloads)), done.Done)
		}
		go func() {
			done.Wait()
			_ = pw.Close()
		}()
		return g, nil
	}
	g.unsub = h.Subscribe(func(cache.Event) { signal(g.wake) })
	for _, w := range workloads {
		g.startMember(w, groupBounds(len(workloads)), nil)
	}
	go func() {
		defer s.recoverTo("group logs")
		runCoalesced(g.stop, g.wake, s.opts.LogRescan, nil, g.rescan)
	}()
	return g, nil
}

// capWorkloads bounds the workloads streamed at once, saying so once.
func (g *groupLogs) capWorkloads(workloads []model.ObjectRef) []model.ObjectRef {
	if len(workloads) <= maxGroupWorkloads {
		return workloads
	}
	if !g.capped {
		g.capped = true
		g.notice(fmt.Sprintf("%s showing the logs of %d of %d workloads; narrow the tab to one to see another's", NoticePrefix, maxGroupWorkloads, len(workloads)))
	}
	return workloads[:maxGroupWorkloads]
}

// startMember starts one workload's stream into the group's sink; a workload whose stream does not
// open is said once. done, for a Previous stream, is called when its instances are written (or
// at once when it did not open).
func (g *groupLogs) startMember(w model.ObjectRef, bounds workloadBounds, done func()) {
	wl, err := g.s.newWorkloadLogs(g.ctx, g.tab, w, w.Name, g.b, g.streamer, g.sink, bounds, done)
	g.mu.Lock()
	defer g.mu.Unlock()
	if err != nil {
		if !g.failed[w] && g.ctx.Err() == nil {
			g.failed[w] = true
			g.notice(fmt.Sprintf("%s %s: %s", NoticePrefix, w.String(), oneLine(err.Error())))
		}
		if done != nil {
			done()
		}
		return
	}
	if g.ctx.Err() != nil { // closed meanwhile
		_ = wl.Close()
		return
	}
	delete(g.failed, w)
	g.members[w] = wl
}

// rescan follows the group's object: the workloads that joined it start streaming, those that left
// it end, and the stream says so.
func (g *groupLogs) rescan() {
	obj, ok := KindGet(g.h, g.p, g.ref.Namespace, g.ref.Name)
	if !ok {
		return // deleted or not listed: the streams go on until the tab closes
	}
	workloads := g.capWorkloads(LogGroupWorkloads(g.ref, obj))
	g.mu.Lock()
	var gone []*workloadLogs
	for w, wl := range g.members {
		if !slices.ContainsFunc(workloads, w.Same) {
			gone = append(gone, wl)
			delete(g.members, w)
			g.notice(fmt.Sprintf("%s %s left %s %s; its logs end here", NoticePrefix, wl.noun, g.p.Kind, g.ref.Name))
		}
	}
	var joined []model.ObjectRef
	for _, w := range workloads {
		if _, running := g.members[w]; !running {
			joined = append(joined, w)
		}
	}
	g.mu.Unlock()
	for _, wl := range gone {
		_ = wl.Close()
	}
	for _, w := range joined {
		if g.ctx.Err() != nil {
			return
		}
		g.startMember(w, groupBounds(len(workloads)), nil)
	}
}

// notice writes one of Cogpit's own lines without waiting for the reader.
func (g *groupLogs) notice(text string) {
	go func() {
		rest := strings.TrimPrefix(strings.TrimPrefix(text, NoticePrefix), " ")
		g.sink.write(NoticePrefix, []byte(rest))
	}()
}

// Read returns the merged lines.
func (g *groupLogs) Read(p []byte) (int, error) { return g.pr.Read(p) }

// Close ends every workload's stream and releases the handle on the group. It is idempotent.
func (g *groupLogs) Close() error {
	g.closeOnce.Do(func() {
		g.cancel()
		close(g.stop)
		if g.unsub != nil {
			g.unsub()
		}
		_ = g.pr.Close() // unblocks writers
		g.mu.Lock()
		members := g.members
		g.members = map[model.ObjectRef]*workloadLogs{}
		g.mu.Unlock()
		for _, wl := range members {
			_ = wl.Close()
		}
		g.h.Release()
	})
	return nil
}
