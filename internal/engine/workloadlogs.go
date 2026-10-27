package engine

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The logs of a controlling resource (workload.logs): one stream that merges the logs of every
// container of every pod the resource controls (for a Deployment, through its ReplicaSets), each
// line starting with "<pod>/<container> " and the API server's timestamp. The pods come from the
// cache, never from a list request: the stream holds cache handles on pods (and replica sets)
// while it is open and starts the log of a container when the cache shows it running, so pods that
// join while the tab follows (a rollout, a scale-up, a restarted container) join the stream.
//
// The tab's selection (logsources.go) narrows the stream while it follows: the streams of the
// containers it leaves out end, the others go on untouched, and a container selected again
// resumes from the newest line it had written (sinceTime), with the lines written meanwhile, at
// most workloadLogTail of them.
//
// A group's logs (grouplogs.go, A81: an application's) are several such streams, one per workload,
// writing into one sink: each line then starts with "<workload>/<pod>/<container> ", and each
// stream's bounds are its share of the group's.

// Bounds of a controlling resource's logs.
const (
	// workloadLogStreams bounds the containers streamed at once; the stream says when it leaves
	// some out, and starts them when others end.
	workloadLogStreams = 40
	// workloadLogTail is the history the stream starts with, shared by the containers it opens
	// with, but at least workloadLogMinTail lines each. A container that starts later gets
	// workloadLogTail of its own.
	workloadLogTail    = 1000
	workloadLogMinTail = 100
	// workloadLogLineBytes cuts longer lines (the log view keeps 64 KiB of a line too).
	workloadLogLineBytes = 64 << 10
	// workloadLogRescan is the most often the pods are looked at again after a change, unless the
	// session's options say otherwise (Options.LogRescan).
	workloadLogRescan = 250 * time.Millisecond
)

// NoticePrefix starts the lines Cogpit writes into a log stream itself.
const NoticePrefix = "[cogpit]"

// logSink is where a merged stream's lines go: one pipe, one line at a time, for one workload's
// stream or for every workload of a group.
type logSink struct {
	mu sync.Mutex
	pw *io.PipeWriter
}

// write adds one line: the prefix, a space, the line, a line feed. It reports false once the pipe
// is closed.
func (k *logSink) write(prefix string, line []byte) bool {
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	buf := make([]byte, 0, len(prefix)+len(line)+2)
	buf = append(buf, prefix...)
	buf = append(buf, ' ')
	buf = append(buf, line...)
	buf = append(buf, '\n')
	k.mu.Lock()
	defer k.mu.Unlock()
	_, err := k.pw.Write(buf)
	return err == nil
}

// workloadBounds are one workload stream's share of what a tab streams: the containers at once,
// and the history it starts with, shared by the containers it opens with. A workload's own tab
// has all of it; a group's workloads share it (grouplogs.go).
type workloadBounds struct {
	streams int
	tail    int
}

// workloadLogs is the merged stream of a controlling resource's pods. Read returns its lines; Close
// ends every container's stream and releases the cache handles.
type workloadLogs struct {
	s        *Session
	watch    *connWatch // the connection's: a stream that broke with it resumes once it is back (D-58)
	tabID    string
	ref      model.ObjectRef
	noun     string // "Deployment web"
	opts     LogOptions
	streamer logStreamer
	uid      string // the resource's UID: its pods' (or replica sets') controller
	pods, rs *cache.Handle
	// name is the workload's name before each line's pod in a group's stream (grouplogs.go); ""
	// in the workload's own tab.
	name   string
	bounds workloadBounds

	ctx    context.Context
	cancel context.CancelFunc
	pr     *io.PipeReader // nil for a group's workload: the group reads the sink
	sink   *logSink
	// previousDone ends a Previous stream once every previous instance was written: it closes the
	// pipe of a workload's own tab, and tells a group that this workload is done.
	previousDone func()
	wake         chan struct{}
	stop         chan struct{}
	unsub        []func()
	wg           sync.WaitGroup

	mu sync.Mutex
	// started holds the "<pod uid>/<container>/<instance>" streams started; scan forgets the ones
	// whose pod or container instance the cache no longer shows, which never come back.
	started   map[string]bool
	streaming map[string]*streamEntry // the started streams still running, by the instance they follow
	// cursors are where the streams the selection ended stood, for when it selects them again.
	cursors map[string]logCursor
	sel     logSelection // what the tab narrows to
	active  int
	capped  bool
	tail    int64 // the history each of the first containers starts with

	closeOnce sync.Once
}

// logTarget is one container log to stream: one instance of a container of one pod.
type logTarget struct {
	key, pod, container string
	uid                 string // the pod's
	restarts            int64  // the instance: the container's restart count
	// named: an init container that is no sidecar, streamed only when the selection names it.
	named bool
	// workload is the name of the group's workload the pod belongs to; "" in a workload's own tab.
	workload string
}

// selects reports whether the selection takes the target in.
func (sel logSelection) selects(t logTarget) bool {
	return (!t.named || sel.container == t.container) && sel.matches(t.pod, t.container) &&
		(sel.workload == "" || t.workload == "" || sel.workload == t.workload)
}

// streamEntry is a running stream: its target, and how the selection ends it. Its fields are
// guarded by workloadLogs.mu.
//
// A log request names the pod and the container, never the pod's UID or the container's instance:
// the API server answers with the instance that holds the name when the request arrives. A stream's
// first request is opened when the cache shows its instance running. A stream opened again (after
// an interruption, or after the server ended it) is opened by the name while the cache may lag, right
// after a reconnect always: the pod may have been created again under its name meanwhile (a Stateful
// Set's), or the container restarted, and the stream then follows that instance. Such a stream is
// byName: when the cache shows that container's new instance, the stream is handed it (adopt)
// instead of a second stream opening for it, which would show its lines again.
type streamEntry struct {
	target logTarget // the instance it follows; adopt changes it, never its pod or container
	cancel context.CancelFunc
	byName bool // opened again by the name since the cache showed its instance running
}

// openWorkloadLogs opens the merged logs of the resource a logs tab names.
func (s *Session) openWorkloadLogs(ctx context.Context, tab DockTab, b *Backend, streamer logStreamer) (io.ReadCloser, error) {
	pr, pw := io.Pipe()
	wl, err := s.newWorkloadLogs(ctx, tab, *tab.Ref, "", b, streamer, &logSink{pw: pw},
		workloadBounds{streams: workloadLogStreams, tail: workloadLogTail}, func() { _ = pw.Close() })
	if err != nil {
		_ = pw.Close()
		return nil, err
	}
	wl.pr = pr
	return wl, nil
}

// newWorkloadLogs starts the merged stream of the resource ref, a workload of the tab (the tab's
// own, or one of its group's, named name), writing its lines into sink within bounds;
// previousDone ends a Previous stream (see workloadLogs).
func (s *Session) newWorkloadLogs(ctx context.Context, tab DockTab, ref model.ObjectRef, name string, b *Backend, streamer logStreamer,
	sink *logSink, bounds workloadBounds, previousDone func()) (*workloadLogs, error) {
	obj, err := s.Object(ctx, ref)
	if err != nil {
		return nil, err
	}
	podsP, ok := b.Profile(logKindPods)
	if !ok {
		return nil, fmt.Errorf("%w: pods", ErrUnknownKind)
	}
	noun := ref.Kind + " " + ref.Name
	if p, ok := b.Profile(ref.Kind); ok {
		noun = p.Kind + " " + ref.Name
	}
	wl := &workloadLogs{
		s: s, watch: b.watch, tabID: tab.ID, ref: ref, noun: noun, opts: tab.Logs, streamer: streamer, uid: string(obj.GetUID()),
		name: name, bounds: bounds, sink: sink, previousDone: previousDone,
		wake: make(chan struct{}, 1), stop: make(chan struct{}), started: map[string]bool{},
		streaming: map[string]*streamEntry{}, cursors: map[string]logCursor{}, sel: selectionOf(tab),
	}
	// The workload's namespace, once the access reviews said how (accessiblens_plan.go acquireIn):
	// a handle taken before they answered would read in all namespaces, which a user who may not
	// list pods there is refused, and it would never sync. A read in all namespaces that the server
	// refuses all the same (a RoleBinding in the context's namespace alone) is taken again in the
	// workload's namespace (readFollowing).
	var rsP *model.KindProfile
	if ref.Kind == logKindDeployments {
		if rsP, ok = b.Profile(logKindReplicaSets); !ok {
			return nil, fmt.Errorf("%w: replicasets", ErrUnknownKind)
		}
	}
	for _, read := range []struct {
		p  *model.KindProfile
		to **cache.Handle
	}{{podsP, &wl.pods}, {rsP, &wl.rs}} {
		if read.p == nil {
			continue
		}
		h, err := b.readFollowing(ctx, read.p, b.Scope(read.p).String(), b.inNamespace(read.p, ref.Namespace))
		if err == nil {
			if err = refusedIn(h, h.Scope().String()); err != nil {
				h.Release()
			}
		}
		if err != nil {
			if wl.pods != nil {
				wl.pods.Release()
			}
			return nil, err
		}
		*read.to = h
	}
	if !wl.opts.Previous {
		// Subscribed before the first scan, so that no pod is missed between the two.
		for _, h := range []*cache.Handle{wl.pods, wl.rs} {
			if h != nil {
				wl.unsub = append(wl.unsub, h.Subscribe(func(cache.Event) { signal(wl.wake) }))
			}
		}
		// The tab's selection, whoever changes it; read again after subscribing, so that no
		// change is missed between the two. A tab with Previous opens again instead (the GUI).
		wl.unsub = append(wl.unsub, s.Subscribe(func(ev Event) {
			if ev.Type == EventUIState && ev.UIState != nil {
				if t, ok := ev.UIState.DockTab(wl.tabID); ok {
					wl.setSelection(selectionOf(t))
				}
			}
		}))
		if t, ok := s.UIState().DockTab(wl.tabID); ok {
			wl.sel = selectionOf(t)
		}
	}
	wl.ctx, wl.cancel = context.WithCancel(ctx)

	first := wl.scan()
	tail := int64(bounds.tail)
	if n := len(first); n > 0 {
		tail = max(int64(workloadLogMinTail), int64(bounds.tail/min(n, max(bounds.streams, 1))))
	}
	if len(first) == 0 {
		wl.noticeAsync(wl.emptyNotice())
	}
	wl.tail = tail
	wl.start(first, tail)
	if wl.opts.Previous {
		// The previous instances' logs are complete: the stream ends after them.
		go func() {
			wl.wg.Wait()
			if wl.previousDone != nil {
				wl.previousDone()
			}
		}()
		return wl, nil
	}
	go func() {
		defer s.recoverTo("workload logs")
		runCoalesced(wl.stop, wl.wake, s.opts.LogRescan, nil, func() { wl.start(wl.scan(), int64(bounds.tail)) })
	}()
	return wl, nil
}

func (wl *workloadLogs) emptyNotice() string {
	if wl.opts.Previous {
		return fmt.Sprintf("%s no container of %s has a previous instance", NoticePrefix, wl.noun)
	}
	return fmt.Sprintf("%s %s has no running containers yet; their logs show here when they start", NoticePrefix, wl.noun)
}

// setSelection narrows (or widens) the stream to sel: the streams of the containers sel leaves out
// end, keeping where they stood; a rescan starts those it selects that do not stream.
func (wl *workloadLogs) setSelection(sel logSelection) {
	wl.mu.Lock()
	if sel == wl.sel {
		wl.mu.Unlock()
		return
	}
	wl.sel = sel
	for _, e := range wl.streaming {
		if !sel.selects(e.target) {
			e.cancel()
		}
	}
	wl.mu.Unlock()
	signal(wl.wake)
}

// scan returns the containers whose logs are not streamed yet: those of the resource's pods that
// run or have run (for Previous: that have a previous instance) and that the selection selects,
// in pod and container order. Init containers that are no sidecars (restartPolicy Always) count
// only when the selection names one. It forgets the started streams whose pod or container
// instance the cache no longer shows (a pod replaced in a rollout, a container restarted): their
// keys never come back, since a pod's UID is never reused and a restart count only grows, so the
// set holds no more than the pods' containers.
func (wl *workloadLogs) scan() []logTarget {
	owners := map[string]bool{wl.uid: true}
	if wl.rs != nil {
		for _, rs := range wl.rs.ListNamespace(wl.ref.Namespace) {
			if controllerUID(rs) == wl.uid {
				owners[string(rs.GetUID())] = true
			}
		}
	}
	wl.mu.Lock()
	defer wl.mu.Unlock()
	var out []logTarget
	shown := map[string]bool{} // the keys of every container instance the cache shows
	for _, pod := range wl.pods.ListNamespace(wl.ref.Namespace) {
		if !owners[controllerUID(pod)] {
			continue
		}
		for _, cs := range podContainerStatuses(pod) {
			name, st := cs.name, cs.status
			restarts, _ := model.NestedInt64(st, "restartCount")
			instance := fmt.Sprint(restarts)
			if wl.opts.Previous {
				instance = "previous"
			}
			key := string(pod.GetUID()) + "/" + name + "/" + instance
			shown[key] = true
			target := logTarget{key: key, pod: pod.GetName(), container: name, uid: string(pod.GetUID()), restarts: restarts,
				named: cs.init && !cs.sidecar, workload: wl.name}
			switch {
			case !wl.sel.selects(target):
				continue
			case wl.opts.Previous:
				if model.NestedMap(st, "lastState", "terminated") == nil {
					continue
				}
			case model.NestedMap(st, "state", "running") == nil && model.NestedMap(st, "state", "terminated") == nil:
				continue // waiting: no log yet
			}
			if !wl.started[key] {
				out = append(out, target)
			}
		}
	}
	for key := range wl.started {
		if _, running := wl.streaming[key]; !shown[key] && !running {
			delete(wl.started, key)
		}
	}
	for key := range wl.cursors {
		if !shown[key] {
			delete(wl.cursors, key) // the pod or the instance is gone: nothing to resume
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].pod != out[j].pod {
			return out[i].pod < out[j].pod
		}
		return out[i].container < out[j].container
	})
	return out
}

// containerStatus is a container of a pod's spec with its status.
type containerStatus struct {
	name   string
	status any
	// init: an init container; sidecar: one that runs beside the others (restartPolicy Always).
	init, sidecar bool
}

// podContainerStatuses returns a pod's containers that have a status, in spec order, its init
// containers after them.
func podContainerStatuses(pod *unstructured.Unstructured) []containerStatus {
	var out []containerStatus
	add := func(specs, statuses string, init bool) {
		byName := map[string]any{}
		for _, st := range model.NestedSlice(pod, "status", statuses) {
			byName[model.NestedString(st, "name")] = st
		}
		for _, c := range model.NestedSlice(pod, "spec", specs) {
			name := model.NestedString(c, "name")
			if st, ok := byName[name]; ok && name != "" {
				out = append(out, containerStatus{name: name, status: st, init: init, sidecar: init && model.NestedString(c, "restartPolicy") == "Always"})
			}
		}
	}
	add("containers", "containerStatuses", false)
	add("initContainers", "initContainerStatuses", true)
	return out
}

// controllerUID is the UID of an object's controller, or "".
func controllerUID(obj *unstructured.Unstructured) string {
	for _, o := range obj.GetOwnerReferences() {
		if o.Controller != nil && *o.Controller {
			return string(o.UID)
		}
	}
	return ""
}

// start streams the targets, as many as the bound allows.
func (wl *workloadLogs) start(targets []logTarget, tail int64) {
	wl.mu.Lock()
	defer wl.mu.Unlock()
	for _, t := range targets {
		if wl.ctx.Err() != nil {
			return
		}
		if wl.started[t.key] { // another start took it since the scan (previous instances)
			continue
		}
		if wl.adopt(t) {
			continue
		}
		if wl.active >= wl.bounds.streams {
			if !wl.capped {
				wl.capped = true
				wl.noticeAsync(fmt.Sprintf("%s %sshowing %d containers' logs at once; the others join as these end", NoticePrefix, wl.namePrefix(), wl.bounds.streams))
			}
			return
		}
		ctx, cancel := context.WithCancel(wl.ctx)
		e := &streamEntry{target: t, cancel: cancel}
		wl.started[t.key], wl.streaming[t.key] = true, e
		cur, resumed := wl.cursors[t.key]
		delete(wl.cursors, t.key)
		wl.active++
		wl.wg.Add(1)
		go wl.stream(ctx, e, tail, cur, resumed)
	}
}

// adopt hands t, a container instance the cache shows that no stream follows, to the stream of the
// same pod and container that was opened again by the name (streamEntry.byName): that stream
// follows whatever instance holds the name, t by now, and a second stream would show t's lines
// again. It reports whether it did. Called with wl.mu held.
func (wl *workloadLogs) adopt(t logTarget) bool {
	for key, e := range wl.streaming {
		if !e.byName || key == t.key || e.target.pod != t.pod || e.target.container != t.container {
			continue
		}
		delete(wl.streaming, key) // scan forgets the old key: the cache no longer shows it
		e.target = t
		wl.started[t.key], wl.streaming[t.key] = true, e
		return true
	}
	return false
}

// targetOf is the instance a stream follows now.
func (wl *workloadLogs) targetOf(e *streamEntry) logTarget {
	wl.mu.Lock()
	defer wl.mu.Unlock()
	return e.target
}

// resume reports whether a stream may open its container's log again: the cache shows the instance
// it follows running. From then on the stream is byName (streamEntry), in the same lock as the
// rescan's adopt, so that no rescan starts a second stream between the two.
func (wl *workloadLogs) resume(e *streamEntry) bool {
	wl.mu.Lock()
	defer wl.mu.Unlock()
	if !wl.stillRunning(e.target) {
		return false
	}
	e.byName = true
	return true
}

// Resuming a follow stream the server ended while its container still runs (the API server or
// kubelet closing a long stream, a broken connection).
var (
	// workloadLogResumes bounds the resumptions in a row that bring no new line; then the stream
	// says so and stops following that container.
	workloadLogResumes = 5
	// workloadLogBackoff is the wait before a resumption, doubled for each further one in a row,
	// at most workloadLogMaxBackoff.
	workloadLogBackoff    = 500 * time.Millisecond
	workloadLogMaxBackoff = 8 * time.Second
)

// workloadLogSteady is how long a stream must have stayed open for its end to count as a new
// drop rather than another of the same: the count of resumptions in a row starts again.
const workloadLogSteady = 30 * time.Second

// logCursor is where a container's log stands: the newest timestamp written and how many lines
// carried it, so that a resumption drops what it has already written.
type logCursor struct {
	last  time.Time
	atIt  int
	lines int       // lines written, with or without a timestamp
	seen  time.Time // when the last line written arrived, by Cogpit's clock (interruptedMarker)
}

// stream copies one container's log into the merged stream, line by line, each with its prefix.
// A follow stream the server ends while the cache still shows the container instance running is
// opened again from the newest line's timestamp (sinceTime), after a backoff, at most
// workloadLogResumes times in a row without a new line. ctx ends with the merged stream, or when
// the selection leaves the container out: then the stream keeps its cursor, and when the selection
// takes the container in again it starts from it (resumed), with at most workloadLogTail lines. A
// stream opened again follows the container by its pod's name (streamEntry): the instance e
// follows may change (adopt), its pod and container never do.
func (wl *workloadLogs) stream(ctx context.Context, e *streamEntry, tail int64, cur logCursor, resumed bool) {
	defer wl.wg.Done()
	defer func() {
		deselected := ctx.Err() != nil && wl.ctx.Err() == nil // asked before the cancel below
		wl.mu.Lock()
		key := e.target.key // the instance it followed last
		wl.active--
		e.cancel()
		delete(wl.streaming, key)
		if deselected { // left out by the selection: started again when selected
			delete(wl.started, key)
			if cur.lines > 0 {
				wl.cursors[key] = cur
			}
		}
		wl.mu.Unlock()
		if wl.opts.Previous {
			// No rescan runs for previous instances: start the ones left out here, before this
			// stream's Done, so that the merged stream does not end in between.
			wl.start(wl.scan(), wl.tail)
			return
		}
		signal(wl.wake) // a container left out may start now
	}()
	defer wl.s.recoverTo("workload logs")
	t := wl.targetOf(e)
	prefix := t.pod + "/" + t.container
	if wl.name != "" { // a group's workload (grouplogs.go)
		prefix = wl.name + "/" + prefix
	}
	follow := !wl.opts.Previous
	drops := 0 // resumptions in a row without a new line
	for attempt := 0; ; attempt++ {
		prev := t
		t = wl.targetOf(e)
		opts := cluster.LogOptions{
			Namespace: wl.ref.Namespace, Pod: t.pod, Container: t.container,
			// Every stream asks for timestamps: a resumption starts from them, and the tab jumps
			// to a time and loads older lines by them. The GUI hides them unless the tab shows
			// them.
			Follow: follow, Previous: wl.opts.Previous, Timestamps: true, TailLines: tail,
		}
		check := checkNone
		switch {
		case attempt == 0 && resumed && !cur.last.IsZero():
			// Selected again: the lines since it was left out, but no more than a fresh start.
			opts.TailLines, opts.SinceTime, check = workloadLogTail, cur.last.Truncate(time.Second), checkBound
		case !cur.last.IsZero():
			opts.TailLines, opts.SinceTime = 0, cur.last.Truncate(time.Second)
			if attempt > 0 && t.key == prev.key { // the same instance, as far as the cache knows
				check = checkContinued
			}
		}
		opened := time.Now()
		openedAt := wl.s.Now() // the watch's clock, for await
		rc, err := wl.streamer.StreamLogs(ctx, opts)
		fresh := 0
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if attempt == 0 {
				wl.write(NoticePrefix, []byte(prefix+": "+oneLine(err.Error())))
				return
			}
		} else {
			var open bool
			fresh, open = wl.copyLines(rc, prefix, &cur, check)
			_ = rc.Close()
			if !open {
				return // the tab closed
			}
		}
		// Ended: by the container (it exited or restarted; the scan starts its next instance), by
		// the tab or the selection, or by the server while the container runs on.
		if !follow || ctx.Err() != nil || !wl.stillRunning(wl.targetOf(e)) {
			return
		}
		if cur.last.IsZero() && cur.lines > 0 {
			return // lines without timestamps: a resumption could not tell which it has
		}
		// Broken with the connection: wait until it is back, mark the gap and go on at once (D-58),
		// unless the cache shows the instance gone by then: the scan streams the one that holds the
		// name now.
		from, to, werr := wl.watch.await(ctx, openedAt, func() { wl.write(NoticePrefix, []byte(prefix+": "+lapsedNotice)) })
		if werr != nil {
			return
		}
		if !from.IsZero() {
			if !wl.write(NoticePrefix, []byte(prefix+": "+wl.s.interruptedMarker(from, to, cur.seen))) || !wl.resume(e) {
				return
			}
			drops = 0
			continue
		}
		if wl.targetOf(e).key != t.key {
			// Handed the container's next instance while this request ran (adopt): the end was
			// the old instance's, and the new one runs. On at once.
			if !wl.resume(e) {
				return
			}
			drops = 0
			continue
		}
		if fresh > 0 || time.Since(opened) >= workloadLogSteady {
			drops = 0
		}
		drops++
		if drops > workloadLogResumes {
			wl.write(NoticePrefix, []byte(fmt.Sprintf("%s: the log stream ended %d times in a row without new lines; Cogpit stopped following it", prefix, drops-1)))
			return
		}
		wait := time.NewTimer(min(workloadLogBackoff<<(drops-1), workloadLogMaxBackoff))
		select {
		case <-ctx.Done():
			wait.Stop()
			return
		case <-wait.C:
		}
		// Asked again after the wait: a stream also ends when its container restarts, and the
		// cache may show the restart late. A resumption names the container, not the instance:
		// if the cache still shows the old one, it follows the new one by the name, which the
		// rescan then hands it (adopt).
		if !wl.resume(e) {
			return
		}
	}
}

// lineCheck is what a stream's first timestamped line is checked for against the cursor.
type lineCheck int

const (
	checkNone lineCheck = iota
	// checkBound: a stream selected again, whose request is bounded: a first line newer than the
	// cursor's means the bound cut lines off.
	checkBound
	// checkContinued: a stream opened again: the same container instance sends its last line again
	// (the request starts at its second), so a first line newer than the cursor's means that the
	// log is another instance's (a restart, a pod created again under its name) or was rotated.
	checkContinued
)

// copyLines copies one opened stream's lines into the merged stream and returns how many it
// wrote; open is false once the merged stream is closed. Each line's leading timestamp (every
// stream asks for them) moves cur, and lines at or before cur's are dropped (a resumption sends
// the second it starts in again). A first line newer than cur's gets a notice before it, by check:
// the bound of a stream selected again cut lines off (checkBound), or a stream opened again does
// not go on from its last line (checkContinued, notContinuedNotice).
func (wl *workloadLogs) copyLines(rc io.Reader, prefix string, cur *logCursor, check lineCheck) (fresh int, open bool) {
	// What the stream had when this one opened: lines before it, and the first atIt lines at it,
	// are written already.
	had, skip := cur.last, cur.atIt
	br := bufio.NewReaderSize(rc, workloadLogLineBytes)
	for {
		line, err := br.ReadSlice('\n')
		full := errors.Is(err, bufio.ErrBufferFull)
		keep := len(line) > 0
		if keep {
			if ts, _, ok := splitLogTimestamp(line); ok {
				if check != checkNone && ts.After(had) && !wl.write(NoticePrefix, []byte(prefix+": "+checkNotice(check))) {
					return fresh, false
				}
				check = checkNone
				switch {
				case ts.Before(had):
					keep = false
				case ts.Equal(had) && skip > 0:
					skip--
					keep = false
				case ts.After(cur.last):
					cur.last, cur.atIt = ts, 1
				default:
					cur.atIt++
				}
			}
		}
		if keep {
			if !wl.write(prefix, line) {
				return fresh, false
			}
			fresh++
			cur.lines++
			cur.seen = wl.s.Now()
		}
		for full { // the rest of a line longer than the bound is dropped
			_, err = br.ReadSlice('\n')
			full = errors.Is(err, bufio.ErrBufferFull)
		}
		if err != nil {
			return fresh, true
		}
	}
}

// checkNotice is the notice before a first line newer than the cursor's (copyLines).
func checkNotice(check lineCheck) string {
	if check == checkBound {
		return fmt.Sprintf("more lines were written while it was not selected than Cogpit loads again (%d); the older ones are not shown", workloadLogTail)
	}
	return notContinuedNotice
}

// splitLogTimestamp splits the RFC 3339 timestamp the API server puts before a line when asked
// for timestamps from the line.
func splitLogTimestamp(line []byte) (time.Time, []byte, bool) {
	head, rest, found := bytes.Cut(line, []byte{' '})
	if !found {
		head, rest = bytes.TrimRight(line, "\r\n"), nil
	}
	ts, err := time.Parse(time.RFC3339Nano, string(head))
	if err != nil {
		return time.Time{}, line, false
	}
	return ts, rest, true
}

// stillRunning reports whether the cache shows the target's container instance running: the same
// pod (by UID), the same restart count.
func (wl *workloadLogs) stillRunning(t logTarget) bool {
	pod, ok := wl.pods.Get(wl.ref.Namespace, t.pod)
	if !ok || string(pod.GetUID()) != t.uid {
		return false
	}
	for _, cs := range podContainerStatuses(pod) {
		if cs.name == t.container {
			restarts, _ := model.NestedInt64(cs.status, "restartCount")
			return restarts == t.restarts && model.NestedMap(cs.status, "state", "running") != nil
		}
	}
	return false
}

// write adds one line to the merged stream: the prefix, a space, the line, a line feed. It
// reports false once the stream is closed.
func (wl *workloadLogs) write(prefix string, line []byte) bool { return wl.sink.write(prefix, line) }

// namePrefix is "<workload>: " before a notice of a group's workload, "" in a workload's own tab.
func (wl *workloadLogs) namePrefix() string {
	if wl.name == "" {
		return ""
	}
	return wl.name + ": "
}

// noticeAsync writes one of Cogpit's own lines without waiting for the reader.
func (wl *workloadLogs) noticeAsync(text string) {
	wl.wg.Add(1)
	go func() {
		defer wl.wg.Done()
		prefix, rest, _ := strings.Cut(text, " ")
		wl.write(prefix, []byte(rest))
	}()
}

// oneLine makes an error from the cluster one line without control characters.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}

// Read returns the merged lines.
func (wl *workloadLogs) Read(p []byte) (int, error) { return wl.pr.Read(p) }

// Close ends every container's stream and releases the cache handles. It is idempotent and does
// not wait for the streams' goroutines. A group's workload leaves the pipe to the group.
func (wl *workloadLogs) Close() error {
	wl.closeOnce.Do(func() {
		wl.cancel()
		close(wl.stop)
		if wl.pr != nil {
			_ = wl.pr.Close() // unblocks writers
		}
		wl.release()
	})
	return nil
}

func (wl *workloadLogs) release() {
	for _, u := range wl.unsub {
		u()
	}
	wl.unsub = nil
	wl.pods.Release()
	if wl.rs != nil {
		wl.rs.Release()
	}
}
