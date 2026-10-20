package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/guided-traffic/cogpit/internal/cluster"
)

// A pod's followed log goes on after an interrupted connection (D-58, S7): when its stream ends
// with the connection, it waits until the connection is back, writes a marker line ("[cogpit] ──
// no connection 14:03:12 – 14:07:40 CEST (4 min 28 s) · …") and opens the log again from the last
// line's time (sinceTime), the lines it has already written left out. A stream that ends while the connection works ends the
// tab's log as before. A controlling resource's logs resume the same way (workloadlogs.go). The
// request names the pod, never its UID: a pod created again under its name meanwhile, or a
// restarted container, is what the log reads then, and a line says so (notContinuedNotice).

// interruptedMarker is the line a log shows where its stream broke with the connection and was
// opened again (the owner's WF35 of 2026-10-19: "— interrupted 11:10–11:10 —" after ten minutes of
// sleep could not be read). The gap runs from the last line that arrived before the break (seen,
// by Cogpit's clock; the interruption's start, from, when it came first or no line had) to the
// reconnect (to), with seconds and the zone, in the user's time format (D-31): the connection
// watch notices a sleep only at the wake, so its own start says when the computer woke, not when
// the lines stopped. The log is read again from its last line's time, so what the container wrote
// meanwhile follows the marker; a kubelet that rotated the container's log meanwhile may have
// dropped some, which Cogpit cannot tell, so the marker does not claim that none is missing.
func (s *Session) interruptedMarker(from, to, seen time.Time) string {
	if !seen.IsZero() && seen.Before(from) {
		from = seen
	}
	tf := s.TimeFormat()
	zone := to.UTC().Format("MST")
	if tf.Zone != nil {
		zone = to.In(tf.Zone).Format("MST")
	}
	return fmt.Sprintf("%s%s – %s %s (%s) · the log was read again from its last line: what was written meanwhile follows ──",
		interruptedMarkerStart, tf.FormatTime(from), tf.FormatTime(to), zone, FormatSpan(to.Sub(from)))
}

// interruptedMarkerStart begins interruptedMarker's line.
const interruptedMarkerStart = "── no connection "

// notContinuedNotice is the line a log shows where it was opened again and does not go on from the
// last line it had.
const notContinuedNotice = "the log read again does not hold its last line: the container restarted, the pod was created again under its name or its log was rotated meanwhile; lines written in between may be missing"

// resumingLog is a followed pod log that survives interruptions.
type resumingLog struct {
	s        *Session
	w        *connWatch
	streamer logStreamer
	opts     cluster.LogOptions
	ctx      context.Context
	cancel   context.CancelFunc
	pr       *io.PipeReader
	pw       *io.PipeWriter
}

// logLineBytes bounds a line the resumer copies; the rest of a longer line is dropped, as the
// window drops it.
const logLineBytes = 64 << 10

// openResumingLog opens a pod's log; a followed one resumes after an interruption. The first open's
// error is returned as it was.
func (s *Session) openResumingLog(ctx context.Context, b *Backend, streamer logStreamer, opts cluster.LogOptions) (io.ReadCloser, error) {
	if !opts.Follow || b.watch == nil {
		return streamer.StreamLogs(ctx, opts)
	}
	ctx, cancel := context.WithCancel(ctx)
	opened := s.Now()
	rc, err := streamer.StreamLogs(ctx, opts)
	if err != nil {
		cancel()
		return nil, err
	}
	pr, pw := io.Pipe()
	r := &resumingLog{s: s, w: b.watch, streamer: streamer, opts: opts, ctx: ctx, cancel: cancel, pr: pr, pw: pw}
	go r.run(rc, opened)
	return r, nil
}

func (r *resumingLog) Read(p []byte) (int, error) { return r.pr.Read(p) }

// Close ends the stream.
func (r *resumingLog) Close() error {
	r.cancel()
	return r.pr.Close()
}

// run copies the stream opened at opened, and after an interruption the streams it opens again.
func (r *resumingLog) run(rc io.ReadCloser, opened time.Time) {
	var endErr error
	defer func() { _ = r.pw.CloseWithError(endErr) }()
	defer r.s.recoverTo("a log stream")
	var cur logCursor
	for {
		err := r.copy(rc, &cur)
		_ = rc.Close()
		if r.ctx.Err() != nil || errors.Is(err, io.ErrClosedPipe) {
			return
		}
		// The stream may break only once the connection is back (CI, 2026-10-16): asked by when it
		// opened, the interruption it lived through still counts.
		from, to, werr := r.w.await(r.ctx, opened, r.lapsedLine)
		if werr != nil || from.IsZero() {
			endErr = err // the stream ended while the connection works: the log ends here, as before
			return
		}
		if !r.line(NoticePrefix + " " + r.s.interruptedMarker(from, to, cur.seen)) {
			return
		}
		opts := r.opts
		if !cur.last.IsZero() {
			opts.TailLines, opts.SinceTime = 0, cur.last.Truncate(time.Second)
		}
		for {
			opened = r.s.Now()
			rc, err = r.streamer.StreamLogs(r.ctx, opts)
			if err == nil {
				break
			}
			if r.ctx.Err() != nil {
				return
			}
			// Broken again at once: wait for the connection again; else the log ends with the error.
			if from, _, werr := r.w.await(r.ctx, opened, r.lapsedLine); werr != nil || from.IsZero() {
				r.line(NoticePrefix + " the log could not be opened again: " + oneLine(err.Error()))
				return
			}
		}
	}
}

// lapsedNotice is the line a log shows while it waits for a login that lapsed while its cluster
// was not shown (D-69 item 4).
const lapsedNotice = "the login lapsed; it is renewed when the cluster is shown"

// lapsedLine writes lapsedNotice: the stream waits for the cluster to be shown.
func (r *resumingLog) lapsedLine() { r.line(NoticePrefix + " " + lapsedNotice) }

// line writes one of Cogpit's own lines; false once the reader is gone.
func (r *resumingLog) line(text string) bool {
	_, err := r.pw.Write([]byte(text + "\n"))
	return err == nil
}

// copy copies a stream's lines, moving cur with their timestamps and leaving out what a
// resumption sends again (the lines before cur and the ones at it already written). A resumption
// whose first line is newer than cur's does not go on from the last line: the request names the
// pod and the container, and another pod or container instance holds the name now, or the log was
// rotated; notContinuedNotice says so before that line.
func (r *resumingLog) copy(rc io.Reader, cur *logCursor) error {
	had, skip := cur.last, cur.atIt
	check := !had.IsZero()
	br := bufio.NewReaderSize(rc, logLineBytes)
	for {
		line, err := br.ReadSlice('\n')
		full := errors.Is(err, bufio.ErrBufferFull)
		keep := len(line) > 0
		if keep {
			if ts, _, ok := splitLogTimestamp(line); ok {
				if check && ts.After(had) && !r.line(NoticePrefix+" "+notContinuedNotice) {
					return io.ErrClosedPipe
				}
				check = false
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
			if _, werr := r.pw.Write(line); werr != nil {
				return werr
			}
			if full {
				if _, werr := r.pw.Write([]byte("\n")); werr != nil {
					return werr
				}
			}
			cur.lines++
			cur.seen = r.s.Now()
		}
		for full { // the rest of a line longer than the bound is dropped
			_, err = br.ReadSlice('\n')
			full = errors.Is(err, bufio.ErrBufferFull)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}
