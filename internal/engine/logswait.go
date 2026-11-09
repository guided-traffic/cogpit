package engine

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
)

// A pod's tab opened on a container that has no log yet (D-162): the stream says what the
// container waits for and shows the log once the container starts. The pods' cache says when; no
// log request goes out before that. A controlling resource's tab does the same by itself
// (workloadlogs.go scan). The API server's own "is waiting to start" counts too, for a cache that
// lags behind it.

// waitingToStart is what the kubelet answers for a container that has no log yet.
const waitingToStart = "is waiting to start"

// couldNotOpenLogs starts the line a log shows when it could not be opened.
const couldNotOpenLogs = "could not open the logs: "

// openPodLog opens a pod's tab's log; a followed one whose container has not started shows it when
// it starts. What a cached pod cannot say (not cached yet, another error) goes to the API server,
// whose answer is returned as it was.
func (s *Session) openPodLog(ctx context.Context, tab DockTab, b *Backend, streamer logStreamer) (io.ReadCloser, error) {
	opts := podLogOptions(tab)
	if opts.Follow {
		if pod, err := s.Lookup(*tab.Ref); err == nil {
			if _, waiting := podContainerWaiting(pod, opts.Container); waiting {
				return s.waitForPodLog(ctx, b, streamer, opts), nil
			}
		}
	}
	rc, err := s.openResumingLog(ctx, b, streamer, opts)
	if opts.Follow && isWaitingToStart(err) {
		return s.waitForPodLog(ctx, b, streamer, opts), nil
	}
	return rc, err
}

// isWaitingToStart reports whether a log request failed because its container has no log yet.
func isWaitingToStart(err error) bool {
	return err != nil && strings.Contains(err.Error(), waitingToStart)
}

// podEnded reports whether a pod has reached its last phase: a container that has not started by
// then never will.
func podEnded(pod *unstructured.Unstructured) bool {
	phase := corev1.PodPhase(model.NestedString(pod, "status", "phase"))
	return phase == corev1.PodSucceeded || phase == corev1.PodFailed
}

// podContainerWaiting reports whether the named container of a cached pod has no log yet and may
// still get one, and why as the kubelet says it ("" when it gave no reason, or the pod has no node
// or status yet). A container that ran before has a log: its last instance's. A name the pod does
// not declare is the API server's to refuse.
func podContainerWaiting(pod *unstructured.Unstructured, container string) (reason string, waiting bool) {
	if container == "" || podEnded(pod) || !podDeclares(pod, container) {
		return "", false
	}
	for _, list := range []string{"containerStatuses", "initContainerStatuses", "ephemeralContainerStatuses"} {
		for _, st := range model.NestedSlice(pod, "status", list) {
			if model.NestedString(st, "name") != container {
				continue
			}
			if model.NestedMap(st, "state", "running") != nil || model.NestedMap(st, "state", "terminated") != nil ||
				model.NestedMap(st, "lastState", "terminated") != nil {
				return "", false
			}
			return model.NestedString(st, "state", "waiting", "reason"), true
		}
	}
	return "", true
}

// podDeclares reports whether a pod's spec has a container of that name, of any of the three sorts.
func podDeclares(pod *unstructured.Unstructured, container string) bool {
	for _, list := range []string{specContainers, specInitContainers, specEphemeralContainers} {
		for _, c := range model.NestedSlice(pod, "spec", list) {
			if model.NestedString(c, "name") == container {
				return true
			}
		}
	}
	return false
}

// waitingLog is a pod's log that waits for its container, then is the log.
type waitingLog struct {
	s        *Session
	b        *Backend
	streamer logStreamer
	opts     cluster.LogOptions
	ctx      context.Context
	cancel   context.CancelFunc
	pr       *io.PipeReader
	pw       *io.PipeWriter
	said     map[string]bool // the reasons already written
}

// waitForPodLog returns the stream at once; it waits in the background.
func (s *Session) waitForPodLog(ctx context.Context, b *Backend, streamer logStreamer, opts cluster.LogOptions) io.ReadCloser {
	ctx, cancel := context.WithCancel(ctx)
	pr, pw := io.Pipe()
	w := &waitingLog{s: s, b: b, streamer: streamer, opts: opts, ctx: ctx, cancel: cancel, pr: pr, pw: pw, said: map[string]bool{}}
	go w.run()
	return w
}

func (w *waitingLog) Read(p []byte) (int, error) { return w.pr.Read(p) }

// Close ends the stream, waiting or not.
func (w *waitingLog) Close() error {
	w.cancel()
	return w.pr.Close()
}

// run waits for the container, then copies its log.
func (w *waitingLog) run() {
	var endErr error
	defer func() { _ = w.pw.CloseWithError(endErr) }()
	defer w.s.recoverTo("a log stream")
	rc := w.await()
	if rc == nil {
		return
	}
	defer func() { _ = rc.Close() }()
	_, endErr = io.Copy(w.pw, rc)
}

// await holds a cache handle on the pods until the log is open and returns it; nil when the stream
// ends here, with a line that says why.
func (w *waitingLog) await() io.ReadCloser {
	h, err := w.pods()
	if err != nil {
		if w.ctx.Err() == nil {
			w.line("cannot tell when the container starts: " + oneLine(err.Error()))
		}
		return nil
	}
	defer h.Release()
	wake := make(chan struct{}, 1)
	defer h.Subscribe(func(cache.Event) { signal(wake) })() // before the first look: no change is missed
	retries := 0
	for w.ctx.Err() == nil {
		pod, ok := h.Get(w.opts.Namespace, w.opts.Pod)
		if !ok {
			w.line("the pod no longer exists; its log ends here")
			return nil
		}
		if reason, waiting := podContainerWaiting(pod, w.opts.Container); waiting {
			if !w.say(reason) {
				return nil
			}
			select {
			case <-w.ctx.Done():
			case <-wake:
			}
			continue
		}
		rc, again := w.open(pod, &retries)
		if rc != nil || !again {
			return rc
		}
	}
	return nil
}

// pods takes a handle on the pods of the log's namespace, as a controlling resource's log does.
func (w *waitingLog) pods() (*cache.Handle, error) {
	p, ok := w.b.Profile(logKindPods)
	if !ok {
		return nil, fmt.Errorf("%w: pods", ErrUnknownKind)
	}
	h, err := w.b.readFollowing(w.ctx, p, w.b.Scope(p).String(), w.b.inNamespace(p, w.opts.Namespace))
	if err != nil {
		return nil, err
	}
	if err := refusedIn(h, h.Scope().String()); err != nil {
		h.Release()
		return nil, err
	}
	return h, nil
}

// open opens the log of a container the cache shows started. again: not open, and await goes on.
// An API server that still calls it waiting is asked again after a backoff, a few times: the
// status it serves can be ahead of the kubelet that answers for the log. A connection that broke
// is waited for (D-58).
func (w *waitingLog) open(pod *unstructured.Unstructured, retries *int) (rc io.ReadCloser, again bool) {
	opened := w.s.Now()
	rc, err := w.s.openResumingLog(w.ctx, w.b, w.streamer, w.opts)
	if err == nil {
		return rc, false
	}
	if w.ctx.Err() != nil {
		return nil, false
	}
	if isWaitingToStart(err) && !podEnded(pod) && *retries < workloadLogResumes {
		if !w.say("") {
			return nil, false
		}
		*retries++
		return nil, w.sleep(*retries)
	}
	from, _, werr := w.b.watch.await(w.ctx, opened, func() { w.line(lapsedNotice) })
	if werr == nil && !from.IsZero() {
		return nil, true
	}
	w.line(couldNotOpenLogs + oneLine(err.Error()))
	return nil, false
}

// sleep waits out the backoff of the nth retry; false once the stream closed.
func (w *waitingLog) sleep(n int) bool {
	t := time.NewTimer(min(workloadLogBackoff<<(n-1), workloadLogMaxBackoff))
	defer t.Stop()
	select {
	case <-w.ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// say writes what the container waits for, each reason once: an image pull alternates between two
// for as long as it fails. False once the reader is gone.
func (w *waitingLog) say(reason string) bool {
	if w.said[reason] || (reason == "" && len(w.said) > 0) {
		return true
	}
	w.said[reason] = true
	why := ""
	if reason != "" {
		why = ": " + oneLine(reason)
	}
	return w.line(fmt.Sprintf("container %q is waiting to start%s; its log shows here when it starts", w.opts.Container, why))
}

// line writes one of Cogpit's own lines; false once the reader is gone.
func (w *waitingLog) line(text string) bool {
	_, err := w.pw.Write([]byte(NoticePrefix + " " + text + "\n"))
	return err == nil
}
