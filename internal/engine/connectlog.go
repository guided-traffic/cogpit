package engine

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// The connection log (G2, D-66): what happened each time Cogpit connected to a context, logged in
// again, reconnected or disconnected, step by step with times and durations, successful attempts
// too (the owner's decision of 2026-10-16). Lens shows INFO and ERROR lines behind a warning icon
// beside a context whose connect failed ("Connection Updates", captured 2026-10-12); Cogpit's
// dialog of that name opens from every status dot of a cluster and follows the log live.
//
// The log is kept in attempts: a connect (Session.Connect), a reconnect (the connection watch's
// interruption until it is back, connwatch.go), a login (the credential plugin renewing an expired
// login, or the user's Log in again) and a disconnect. An attempt has a kind, a start, its lines,
// and once it ends an outcome and a duration. The session keeps its own attempts in memory (at most
// maxMemAttempts per context and maxConnectLogContexts contexts), which mark a context whose last
// attempt failed (ContextInfo.ConnectProblem) and answer ConnectLog; with a directory
// (Options.ConnectLogDir) each line is also appended to the context's file as it happens
// (connectlog_files.go), so the log survives a restart and a force-quit, and is read back from there
// when the dialog or cluster.connectLog asks (ConnectAttempts), 7 days and 1 MiB per context.
//
// A line carries only what the cluster layer's steps and errors carry (cluster.ConnectStep,
// cluster.ErrorPicture, cluster.ConnectError, kubeconfig.SafeError), which never hold a
// credential; each is escaped for control characters and capped. A credential plugin's standard
// error is in the log only after a failed login, scrubbed and cut to 4 KiB by the cluster layer
// (cluster/pluginstderr.go); its standard output, the credential, never is.

// Levels of a connect log line: INFO and ERROR as Lens writes them; WARN is Cogpit's own, for a
// connection that works but is not safe (tlswarning.go) or was interrupted, and does not mark a
// connect problem.
const (
	LogInfo  = "INFO"
	LogWarn  = "WARN"
	LogError = "ERROR"
)

// Kinds of attempts (ConnectAttempt.Kind).
const (
	AttemptConnect    = "connect"
	AttemptReconnect  = "reconnect"
	AttemptLogin      = "login"
	AttemptDisconnect = "disconnect"
	// AttemptInterruption is a connection's interruption with its cause, an entry of its own that
	// ends at once (OutcomeInterrupted); the reconnect after it is the next attempt (the owner's
	// WF37 of 2026-10-19: a reconnect that succeeded read as ending in "Connection interrupted").
	AttemptInterruption = "interruption"
)

// Outcomes of an attempt (ConnectAttempt.Outcome); empty while it runs, or when Cogpit stopped
// before it ended.
const (
	OutcomeOK        = "ok"
	OutcomeFailed    = "failed"
	OutcomeAbandoned = "abandoned" // a newer attempt began before it ended
	OutcomeCancelled = "cancelled" // the user cancelled it (Disconnect of a connect under way, D-73)
	// OutcomeInterrupted ends an AttemptInterruption: the connection was lost, a reconnect follows.
	OutcomeInterrupted = "interrupted"
)

const (
	maxConnectLogContexts = 64  // contexts the session remembers
	maxMemAttempts        = 30  // attempts per context the session remembers
	maxAttemptLines       = 200 // lines per attempt, in memory and on disk
	maxConnectLogText     = 600 // bytes per line
	// connectLogEventDelay coalesces EventConnectLog: the open dialog reads the log at most this
	// often while a connect writes it.
	connectLogEventDelay = 100 * time.Millisecond
)

// EventConnectLog: a context's connection log changed (View is the context id); the dialog that
// shows it reads it again. Coalesced to one per connectLogEventDelay.
const EventConnectLog EventType = "connectLog"

// ConnectLogLine is one line of a context's connection log.
type ConnectLogLine struct {
	Time  time.Time `json:"time"`
	Level string    `json:"level"` // LogInfo, LogWarn or LogError
	Text  string    `json:"text"`
	// Step names what the line documents (cluster.StepKubeconfig, …, or one of the engine's:
	// "discovery", "access", "cache", "heartbeat"); empty for an attempt's own lines.
	Step string `json:"step,omitempty"`
	// TookMS is how long the step took, in milliseconds; 0 when it is not timed.
	TookMS int64 `json:"tookMs,omitempty"`
}

// ConnectAttempt is one attempt of a context's connection log.
type ConnectAttempt struct {
	Kind    string    `json:"kind"` // AttemptConnect, AttemptReconnect, AttemptLogin, AttemptDisconnect, AttemptInterruption
	Context string    `json:"context"`
	Start   time.Time `json:"start"`
	End     time.Time `json:"end,omitzero"`
	// TookMS is End − Start in milliseconds, once it ended.
	TookMS  int64  `json:"tookMs,omitempty"`
	Outcome string `json:"outcome,omitempty"` // OutcomeOK, OutcomeFailed, OutcomeAbandoned, OutcomeCancelled, OutcomeInterrupted; empty: see Running
	// Running: this session's attempt has not ended yet. An attempt without an outcome that is not
	// running ended when Cogpit stopped (a quit or a crash).
	Running bool             `json:"running,omitempty"`
	Lines   []ConnectLogLine `json:"lines"`
	// Omitted counts the lines left out beyond maxAttemptLines.
	Omitted int `json:"omitted,omitempty"`
	// Stages are a connect's stages, the failed one with its cause (connectfailure.go, D-114): the
	// kubeconfig, a credential plugin's login, DNS, TCP, TLS and the API server's answer. Empty for
	// another kind of attempt, one of a connector that measures none (the demo cluster), and one
	// written before Cogpit kept them.
	Stages []ConnectStage `json:"stages,omitempty"`

	id   int64 // its start in UnixNano, unique per context: the file's attempt key
	live bool  // begun by this session
}

// connectLogs holds the session's attempts by context id and writes them to files. The zero value
// keeps them in memory only.
type connectLogs struct {
	mu     sync.Mutex
	byCtx  map[string]*ctxAttempts
	lastID int64
	now    func() time.Time
	files  *connectLogFiles  // nil: memory only
	emit   func(ctx string)  // EventConnectLog; nil: none
	warn   func(err error)   // a file that cannot be written, said once; nil: none
	pend   map[string]bool   // contexts whose event is due
	timer  bool              // an event flush is scheduled
	failed bool              // a write failed and was said
	used   map[string]uint64 // the order contexts were last written, for eviction
	tick   uint64
}

// ctxAttempts are one context's attempts in memory, oldest first.
type ctxAttempts struct {
	list []*ConnectAttempt
}

// setup gives the log its clock, its files and its notifications (New).
func (l *connectLogs) setup(now func() time.Time, files *connectLogFiles, emit func(string), warn func(error)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.now, l.files, l.emit, l.warn = now, files, emit, warn
}

func (l *connectLogs) clock() time.Time {
	if l.now != nil {
		return l.now().UTC()
	}
	return time.Now().UTC()
}

// ctxLocked returns ctx's attempts, making room among the contexts.
func (l *connectLogs) ctxLocked(ctx string) *ctxAttempts {
	if l.byCtx == nil {
		l.byCtx = map[string]*ctxAttempts{}
		l.used = map[string]uint64{}
	}
	c, ok := l.byCtx[ctx]
	if !ok {
		if len(l.byCtx) >= maxConnectLogContexts {
			l.evictLocked()
		}
		c = &ctxAttempts{}
		l.byCtx[ctx] = c
	}
	l.tick++
	l.used[ctx] = l.tick
	return c
}

// evictLocked forgets the context written longest ago, one without an attempt running first (its
// file keeps its attempts).
func (l *connectLogs) evictLocked() {
	victim, oldest, idle := "", uint64(0), false
	for k, c := range l.byCtx {
		free := c.openLocked() == nil
		if victim == "" || (free && !idle) || (free == idle && l.used[k] < oldest) {
			victim, oldest, idle = k, l.used[k], free
		}
	}
	if victim != "" {
		delete(l.byCtx, victim)
		delete(l.used, victim)
	}
}

// openLocked is the attempt that has not ended, if the latest one has not.
func (c *ctxAttempts) openLocked() *ConnectAttempt {
	if c == nil || len(c.list) == 0 {
		return nil
	}
	if last := c.list[len(c.list)-1]; last.End.IsZero() {
		return last
	}
	return nil
}

// latestLocked is the latest attempt, ended or not.
func (c *ctxAttempts) latestLocked() *ConnectAttempt {
	if c == nil || len(c.list) == 0 {
		return nil
	}
	return c.list[len(c.list)-1]
}

// begin starts an attempt of kind for ctx with its first line and returns it, for stepTo and
// finish; an attempt still running is ended as abandoned first.
func (l *connectLogs) begin(ctx, kind, level, text string) *ConnectAttempt {
	if l == nil { // a quiet writer (connWatch.logs)
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.beginLocked(ctx, kind, level, text)
}

// stepTo appends a step's line to attempt a, unless a newer attempt abandoned it.
func (l *connectLogs) stepTo(a *ConnectAttempt, level, step, text string, took time.Duration) {
	if l == nil || a == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if a.Outcome == OutcomeAbandoned {
		return
	}
	l.addLocked(a, level, text, step, took, l.clock())
	l.changedLocked(a.Context)
}

// finish appends text (if any) to attempt a and ends it with outcome, unless it ended already.
func (l *connectLogs) finish(a *ConnectAttempt, outcome, level, text string) {
	if a == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !a.End.IsZero() {
		return
	}
	l.addLocked(a, level, text, "", 0, l.clock())
	l.endLocked(a, outcome, l.clock())
	l.changedLocked(a.Context)
}

func (l *connectLogs) beginLocked(ctx, kind, level, text string) *ConnectAttempt {
	c := l.ctxLocked(ctx)
	now := l.clock()
	if open := c.openLocked(); open != nil {
		l.endLocked(open, OutcomeAbandoned, now)
	}
	id := now.UnixNano()
	if id <= l.lastID {
		id = l.lastID + 1
	}
	l.lastID = id
	a := &ConnectAttempt{Kind: kind, Context: ctx, Start: now, Lines: []ConnectLogLine{}, id: id, live: true}
	c.list = append(c.list, a)
	if len(c.list) > maxMemAttempts {
		c.list = slices.Delete(c.list, 0, len(c.list)-maxMemAttempts)
	}
	l.writeLocked(ctx, logRecord{Time: now, Context: ctx, Attempt: id, Event: recordBegin, Kind: kind}, true)
	l.addLocked(a, level, text, "", 0, now)
	l.changedLocked(ctx)
	return a
}

// add appends the lines of text (one per line of text) to ctx's latest attempt; without one, it
// begins a connect attempt.
func (l *connectLogs) add(ctx, level, text string) { l.step(ctx, level, "", text, 0) }

// step appends a step's line to ctx's latest attempt.
func (l *connectLogs) step(ctx, level, step, text string, took time.Duration) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	c := l.ctxLocked(ctx)
	a := c.latestLocked()
	if a == nil {
		l.beginLocked(ctx, AttemptConnect, level, text)
		return
	}
	l.addLocked(a, level, text, step, took, l.clock())
	l.changedLocked(ctx)
}

// end appends text to ctx's attempt that runs and ends it with outcome; without one running, text
// is appended to the latest attempt (or begins one of kind).
func (l *connectLogs) end(ctx, kind, outcome, level, text string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	c := l.ctxLocked(ctx)
	a := c.openLocked()
	if a == nil {
		a = l.beginLocked(ctx, kind, level, text)
	} else {
		l.addLocked(a, level, text, "", 0, l.clock())
	}
	l.endLocked(a, outcome, l.clock())
	l.changedLocked(ctx)
}

// stepRunning appends a step's line to ctx's attempt that runs; without one, nothing.
func (l *connectLogs) stepRunning(ctx, level, step, text string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if a := l.byCtx[ctx].openLocked(); a != nil {
		l.addLocked(a, level, text, step, 0, l.clock())
		l.changedLocked(ctx)
	}
}

func (l *connectLogs) endLocked(a *ConnectAttempt, outcome string, now time.Time) {
	if !a.End.IsZero() {
		return
	}
	if a.Omitted > 0 {
		l.lineLocked(a, ConnectLogLine{Time: now, Level: LogInfo,
			Text: fmt.Sprintf("(%d lines left out: an attempt keeps at most %d)", a.Omitted, maxAttemptLines)})
	}
	a.End, a.Outcome = now, outcome
	a.TookMS = now.Sub(a.Start).Milliseconds()
	l.writeLocked(a.Context, logRecord{Time: now, Attempt: a.id, Event: recordEnd, Outcome: outcome, TookMS: a.TookMS}, false)
}

// addLocked appends each non-empty line of text, escaped and capped.
func (l *connectLogs) addLocked(a *ConnectAttempt, level, text, step string, took time.Duration, now time.Time) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, " \t\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(a.Lines) >= maxAttemptLines {
			a.Omitted++
			continue
		}
		l.lineLocked(a, ConnectLogLine{Time: now, Level: level, Text: kubeconfig.DisplayText(line, maxConnectLogText),
			Step: step, TookMS: took.Milliseconds()})
		took = 0 // the duration belongs to the step's first line
	}
}

func (l *connectLogs) lineLocked(a *ConnectAttempt, line ConnectLogLine) {
	a.Lines = append(a.Lines, line)
	l.writeLocked(a.Context, logRecord{Time: line.Time, Attempt: a.id, Level: line.Level, Text: line.Text,
		Step: line.Step, TookMS: line.TookMS}, false)
}

// writeLocked appends rec to ctx's file; a write that fails is said once (warn).
func (l *connectLogs) writeLocked(ctx string, rec logRecord, begins bool) {
	if l.files == nil {
		return
	}
	if err := l.files.append(ctx, rec, begins); err != nil && !l.failed {
		l.failed = true
		if l.warn != nil {
			l.warn(err)
		}
	}
}

// changedLocked schedules EventConnectLog for ctx.
func (l *connectLogs) changedLocked(ctx string) {
	if l.emit == nil {
		return
	}
	if l.pend == nil {
		l.pend = map[string]bool{}
	}
	l.pend[ctx] = true
	if l.timer {
		return
	}
	l.timer = true
	time.AfterFunc(connectLogEventDelay, l.flush)
}

// flush sends the events that are due.
func (l *connectLogs) flush() {
	defer func() { _ = recover() }() // a subscriber's panic is the emitter's to log
	l.mu.Lock()
	due := l.pend
	l.pend, l.timer = nil, false
	emit := l.emit
	l.mu.Unlock()
	for ctx := range due {
		emit(ctx)
	}
}

// lines returns ctx's lines of this session: its latest connect attempt and every attempt after
// it, as the log read before attempts (cluster.connectLog's lines).
func (l *connectLogs) lines(ctx string) []ConnectLogLine {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []ConnectLogLine{}
	c := l.byCtx[ctx]
	if c == nil {
		return out
	}
	from := 0
	for i, a := range c.list {
		if a.Kind == AttemptConnect {
			from = i
		}
	}
	for _, a := range c.list[from:] {
		out = append(out, a.Lines...)
	}
	return out
}

// problem reports whether ctx's latest attempt of this session ends in an error line.
func (l *connectLogs) problem(ctx string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.byCtx[ctx].latestLocked()
	return a != nil && len(a.Lines) > 0 && a.Lines[len(a.Lines)-1].Level == LogError
}

// attempts returns ctx's attempts newest first, at most limit (0: all): those in its file within
// the retention, and the session's own (which win for the same attempt: they know whether it runs,
// and hold what a failed write left out of the file). The file is read now, not before.
func (l *connectLogs) attempts(ctx string, limit int) ([]ConnectAttempt, error) {
	var fromFile []*ConnectAttempt
	var err error
	l.mu.Lock()
	files := l.files
	now := l.clock()
	l.mu.Unlock()
	if files != nil {
		fromFile, err = files.load(ctx, now)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	byID := map[int64]ConnectAttempt{}
	for _, a := range fromFile {
		byID[a.id] = *a
	}
	if c := l.byCtx[ctx]; c != nil {
		open := c.openLocked()
		for _, a := range c.list {
			cp := *a
			cp.Lines = slices.Clone(a.Lines)
			cp.Stages = slices.Clone(a.Stages)
			cp.Running = a == open
			byID[a.id] = cp
		}
	}
	out := make([]ConnectAttempt, 0, len(byID))
	for _, a := range byID {
		if now.Sub(a.last()) > connectLogRetention {
			continue
		}
		if a.Lines == nil {
			a.Lines = []ConnectLogLine{}
		}
		out = append(out, a)
	}
	slices.SortFunc(out, func(a, b ConnectAttempt) int { return cmp.Compare(b.id, a.id) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, err
}

// last is the time of the attempt's latest record.
func (a ConnectAttempt) last() time.Time {
	t := a.Start
	if a.End.After(t) {
		t = a.End
	}
	if n := len(a.Lines); n > 0 && a.Lines[n-1].Time.After(t) {
		t = a.Lines[n-1].Time
	}
	return t
}

// ConnectLog returns this session's log of the context id: the lines of its latest connect and of
// every attempt after it (reconnects, logins, a disconnect); empty when there was none.
func (s *Session) ConnectLog(ctx string) []ConnectLogLine { return s.state.logs.lines(ctx) }

// ConnectAttempts returns the context's attempts newest first, at most limit (0: every one kept):
// from its file (7 days, 1 MiB) and this session. An error says the file could not be read; the
// session's attempts are returned with it.
func (s *Session) ConnectAttempts(ctx string, limit int) ([]ConnectAttempt, error) {
	return s.state.logs.attempts(ctx, limit)
}

// markConnectProblems sets ConnectProblem on the contexts whose last attempt failed.
func (s *Session) markConnectProblems(list []ContextInfo) {
	for i := range list {
		list[i].ConnectProblem = s.state.logs.problem(list[i].Name)
	}
}
