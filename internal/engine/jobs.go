package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Background jobs (A91, the owner's decision of 2026-10-14 evening; G4). A command whose work
// outlives its call, as Reconcile with Source does while it waits for Flux's controllers, or an
// action on a selection of more than DirectSelection objects, starts a job: the command returns at
// once with the job's dock tab, and the job runs on a goroutine of the session's. The tab is engine
// state (DockTab.Job): the job's steps, each pending, running, done, failed or stopped with a line
// of detail, their counts, its state and its outcome, so that agents read its progress with
// ui_state as the window shows it in the dock.
//
// A job ends when its work ends, when it is cancelled (job.cancel, the tab's Cancel button: the tab
// stays with the outcome), when the user closes its tab (closing stops the job, it undoes nothing
// written so far), when its cluster is interrupted or disconnected (not when the window shows another
// cluster, D-69), or when the session closes. Jobs are
// not journaled: a crash ends one, and what it had written stays; each job's command says why that
// is harmless (D-11). A job's goroutine recovers from a panic at its boundary, which fails the job.
//
// A job keeps its command's context values (JobSpec.Parent): the origin, the confirmation the user
// gave with the UIDs it bound, so that each step acts on exactly the objects confirmed. A job an
// agent started gets a third audit line when it ends (phase "ended", AuditEntry), after the
// "requested" and "done" lines of the command that started it.

// DockJob: a background job's progress (jobs.go).
const DockJob DockType = "job"

// JobState is the state of a job and of each of its steps.
type JobState string

// Job and step states.
const (
	JobPending   JobState = "pending"   // a step not started yet
	JobRunning   JobState = "running"   // a job, or a step, under way
	JobDone      JobState = "done"      // finished as asked
	JobFailed    JobState = "failed"    // ended with an error (a step: Detail says why)
	JobCancelled JobState = "cancelled" // stopped: cancelled, its tab closed, the cluster changed, the session closed (a step: not attempted)
)

// Ended reports whether a state is final.
func (st JobState) Ended() bool { return st == JobDone || st == JobFailed || st == JobCancelled }

// JobStep is one step of a job: what it does and how far it got.
type JobStep struct {
	// Ref is the object the step acts on, if any.
	Ref    *model.ObjectRef `json:"ref,omitempty"`
	Text   string           `json:"text"`
	State  JobState         `json:"state"`
	Detail string           `json:"detail,omitempty"`
}

// JobCounts counts a job's steps by state, so that the window's header and an agent read its
// progress without counting the steps.
type JobCounts struct {
	Total     int `json:"total"`
	Pending   int `json:"pending,omitempty"`
	Running   int `json:"running,omitempty"`
	Done      int `json:"done,omitempty"`
	Failed    int `json:"failed,omitempty"`
	Cancelled int `json:"cancelled,omitempty"`
}

// CountJobSteps counts steps by state.
func CountJobSteps(steps []JobStep) JobCounts {
	c := JobCounts{Total: len(steps)}
	for _, st := range steps {
		switch st.State {
		case JobPending:
			c.Pending++
		case JobRunning:
			c.Running++
		case JobDone:
			c.Done++
		case JobFailed:
			c.Failed++
		case JobCancelled:
			c.Cancelled++
		}
	}
	return c
}

// JobTab is a job tab's progress (DockTab.Job).
type JobTab struct {
	// Command is the command that started the job ("flux.reconcileWithSource").
	Command string    `json:"command"`
	State   JobState  `json:"state"`
	Steps   []JobStep `json:"steps"`
	// Counts are the steps by state, kept with them.
	Counts  JobCounts `json:"counts"`
	Started time.Time `json:"started"`
	Ended   time.Time `json:"ended,omitzero"`
	// Outcome says in one sentence or two how the job ended, what was done and what was not;
	// empty while it runs.
	Outcome string `json:"outcome,omitempty"`
}

func (j *JobTab) clone() *JobTab {
	if j == nil {
		return nil
	}
	c := *j
	c.Steps = slices.Clone(j.Steps)
	for i, st := range c.Steps {
		if st.Ref != nil {
			r := *st.Ref
			c.Steps[i].Ref = &r
		}
	}
	return &c
}

// JobSpec describes a job StartJob starts.
type JobSpec struct {
	// Command is the command that starts it; Title the tab's title; Context the cluster it acts
	// on, a connected one (D-69: shown or not).
	Command, Title, Context string
	// Parent is the starting command's context: the job's context keeps its values (the origin, the
	// confirmation the user gave and the UIDs it bound; engine.Confirmed, ConfirmedUID) but not its
	// end, since the job outlives the call. Nil: none.
	Parent context.Context
	// Steps are its steps, shown pending until Run moves them.
	Steps []JobStep
	// Run does the job's work on a goroutine of the session's and returns its outcome, or the
	// error that failed it; stopped, it returns what it did and did not do as an error (or
	// ctx.Err() when it has nothing to add), which the outcome adds after why it stopped. ctx ends
	// when the job is cancelled or stopped. Run reports its steps' progress with Job.SetStep.
	Run func(ctx context.Context, j *Job) (outcome string, err error)
}

// Job is a running job, as its Run sees it.
type Job struct {
	s  *Session
	id string
}

// ID is the job's dock tab id.
func (j *Job) ID() string { return j.id }

// SetStep moves step i to state with a line of detail; a step of a job that ended, or a tab that
// is gone, is left alone.
func (j *Job) SetStep(i int, state JobState, detail string) {
	j.s.updateJob(j.id, func(t *JobTab) {
		if i < 0 || i >= len(t.Steps) || t.State.Ended() {
			return
		}
		t.Steps[i].State, t.Steps[i].Detail = state, detail
	})
}

// SetSteps replaces the job's steps, for a job that learns them as it runs: a drain knows the pods
// it evicts once it has listed them (commands/drainjob.go). A step without a state is pending. A
// job that ended, or a tab that is gone, is left alone.
func (j *Job) SetSteps(steps []JobStep) {
	steps = slices.Clone(steps)
	for i := range steps {
		if steps[i].State == "" {
			steps[i].State = JobPending
		}
		if r := steps[i].Ref; r != nil {
			cp := *r
			steps[i].Ref = &cp
		}
	}
	j.s.updateJob(j.id, func(t *JobTab) {
		if !t.State.Ended() {
			t.Steps = steps
		}
	})
}

// Step returns step i as the tab holds it now, and whether the tab still holds the job.
func (j *Job) Step(i int) (JobStep, bool) {
	t, ok := j.s.UIState().DockTab(j.id)
	if !ok || t.Job == nil || i < 0 || i >= len(t.Job.Steps) {
		return JobStep{}, false
	}
	return t.Job.Steps[i], true
}

// Errors that cancel a job.
var (
	errJobTabClosed      = errors.New("its tab was closed")
	errJobClusterChanged = errors.New("its cluster was disconnected")
	// errJobInterrupted stops a job when its cluster stops answering (D-58): as after a crash
	// (S8), nothing a job does continues by itself after the connection is back; what it wrote
	// stays, and the step under way may or may not have reached the cluster.
	errJobInterrupted = errors.New("the connection was interrupted; Cogpit does not continue the job after the reconnect")
	// errJobLoginLapsed stops a job on a cluster that is not shown whose login lapsed (D-69 item 4):
	// its requests would fail, so it stops as at an interruption.
	errJobLoginLapsed    = errors.New("the login lapsed while the cluster was not shown; Cogpit does not continue the job after it is renewed")
	errJobSessionClosed  = errors.New("the app is closing")
	errJobCancelled      = errors.New("cancelled")
	errJobAgentCancelled = errors.New("cancelled by an agent")
)

// ErrJobEnded is CancelJob's error for a job that is no longer running.
var ErrJobEnded = errors.New("the job has ended")

// jobSet is a session's running jobs, by tab id: how to cancel each and the cluster it acts on.
type jobSet struct {
	mu      sync.Mutex
	running map[string]runningJob
	closed  bool
}

type runningJob struct {
	context string
	cancel  context.CancelCauseFunc
	ended   chan struct{} // closed once the tab shows how the job ended
}

// StartJob opens a job's dock tab, makes it the active one, and runs the job. It returns the tab.
func (s *Session) StartJob(spec JobSpec) (DockTab, error) {
	if spec.Run == nil {
		return DockTab{}, errors.New("a job needs work to do")
	}
	u := s.UIState()
	switch st := u.connectionState(spec.Context); { // its cluster, shown or not (D-69)
	case st == "":
		return DockTab{}, &NotConnectedError{Context: spec.Context}
	case st != Connected:
		return DockTab{}, fmt.Errorf("the job acts on %q, whose connection is %s", spec.Context, st)
	}
	steps := slices.Clone(spec.Steps)
	for i := range steps {
		if steps[i].State == "" {
			steps[i].State = JobPending
		}
	}
	tab := DockTab{
		ID: s.NewDockID(DockJob), Type: DockJob, Title: spec.Title, Context: spec.Context,
		Job: &JobTab{Command: spec.Command, State: JobRunning, Steps: steps, Counts: CountJobSteps(steps), Started: s.Now()},
	}
	parent := context.Background()
	if spec.Parent != nil {
		parent = context.WithoutCancel(spec.Parent)
	}
	ctx, cancel := context.WithCancelCause(parent)
	s.jobs.mu.Lock()
	if s.jobs.closed {
		s.jobs.mu.Unlock()
		cancel(errJobSessionClosed)
		return DockTab{}, errJobSessionClosed
	}
	if s.jobs.running == nil {
		s.jobs.running = map[string]runningJob{}
	}
	ended := make(chan struct{})
	s.jobs.running[tab.ID] = runningJob{context: spec.Context, cancel: cancel, ended: ended}
	s.jobs.mu.Unlock()
	s.UpdateUI(func(u *UIState) {
		u.Dock = append(u.Dock, tab)
		u.ActiveDock = tab.ID
	})
	j := &Job{s: s, id: tab.ID}
	s.goSafe("job "+tab.Title, func() {
		var (
			outcome string
			err     = errJobPanicked
		)
		defer func() {
			cause := context.Cause(ctx)
			cancel(nil)
			state, text := jobEnd(outcome, err, cause)
			// Off the running jobs before the tab says it ended: whoever sees the end may reconnect
			// (busyOn) at once.
			s.jobs.forget(tab.ID)
			s.endJob(tab.ID, state, text)
			close(ended)
			s.auditJobEnd(ctx, spec, state, text)
		}()
		outcome, err = spec.Run(ctx, j)
	})
	return tab, nil
}

// errJobPanicked fails a job whose Run panicked (goSafe logs the panic and tells the user).
var errJobPanicked = errors.New("an internal error ended it")

// jobEnd says how a job ended: done with its outcome when Run returned no error, even if a stop
// came after its last step; stopped with why and, when Run said so, what it did and did not do;
// else failed with Run's error as a sentence.
func jobEnd(outcome string, err, cause error) (JobState, string) {
	switch {
	case err == nil:
		return JobDone, outcome
	case cause != nil && !errors.Is(cause, context.Canceled):
		text := "Stopped: " + cause.Error() + "."
		if !errors.Is(err, context.Canceled) {
			text += " " + upperFirst(err.Error())
		}
		return JobCancelled, text
	}
	return JobFailed, upperFirst(err.Error())
}

// endJob records how a job ended. Steps still pending or running are left as they are, so that
// the tab shows where it stopped (a job that marks the steps it did not attempt does so itself).
func (s *Session) endJob(id string, state JobState, outcome string) {
	now := s.Now()
	s.updateJob(id, func(t *JobTab) {
		t.Ended, t.State, t.Outcome = now, state, outcome
	})
}

// auditJobEnd writes the "ended" line of a job an agent started: the command, its parameters and
// the context it acted on, as its "requested" line has them, ok when the job is done, else its
// outcome as the error.
func (s *Session) auditJobEnd(ctx context.Context, spec JobSpec, state JobState, outcome string) {
	inv, ok := ctx.Value(invocationKey{}).(Invocation)
	if !ok || !inv.Origin.IsAgent() {
		return
	}
	actOn := spec.Context
	if req, ok := Confirmed(ctx); ok && req.Context != "" {
		actOn = req.Context
	}
	var err error
	if state != JobDone {
		err = errors.New(outcome)
	}
	s.audit(inv, actOn, auditEnded, err)
}

// updateJob changes an open job tab's progress with fn; the counts follow the steps.
func (s *Session) updateJob(id string, fn func(*JobTab)) {
	if t, ok := s.UIState().DockTab(id); !ok || t.Job == nil {
		return // closed: nothing to show it on, and no UI state change to announce
	}
	s.UpdateUI(func(u *UIState) {
		for i := range u.Dock {
			if u.Dock[i].ID == id && u.Dock[i].Job != nil {
				job := u.Dock[i].Job.clone()
				fn(job)
				job.Counts = CountJobSteps(job.Steps)
				u.Dock[i].Job = job
				return
			}
		}
	})
}

// jobCancelWait bounds how long CancelJob waits for a cancelled job to end: its step under way
// finishes first (a request), so that the tab says what it did.
const jobCancelWait = 10 * time.Second

// CancelJob stops a running job (job.cancel, the job tab's Cancel button): its work ends as when
// its tab closes, but the tab stays with the outcome, which says it was cancelled, by an agent when
// by an origin that is one. It undoes nothing. It waits until the tab shows how the job ended, at
// most jobCancelWait or until ctx ends, and returns the tab as it is then.
func (s *Session) CancelJob(ctx context.Context, id string, by Origin) (DockTab, error) {
	if err := s.JobCancellable(id); err != nil {
		return DockTab{}, err
	}
	cause := errJobCancelled
	if by.IsAgent() {
		cause = errJobAgentCancelled
	}
	s.jobs.mu.Lock()
	r, ok := s.jobs.running[id]
	s.jobs.mu.Unlock()
	if !ok {
		return DockTab{}, ErrJobEnded
	}
	r.cancel(cause)
	wait := time.NewTimer(jobCancelWait)
	defer wait.Stop()
	select {
	case <-r.ended:
	case <-wait.C:
	case <-ctx.Done():
	}
	t, _ := s.UIState().DockTab(id)
	return t, nil
}

// JobCancellable says why a dock tab's job cannot be cancelled: no such tab, no job tab, or a job
// that ended; nil when it runs.
func (s *Session) JobCancellable(id string) error {
	t, ok := s.UIState().DockTab(id)
	switch {
	case !ok:
		return fmt.Errorf("no dock tab %q is open; ui_state lists the open ones (dock)", id)
	case t.Job == nil:
		return fmt.Errorf("dock tab %q is a %s tab, not a job", id, t.Type)
	case t.Job.State.Ended():
		return ErrJobEnded
	}
	return nil
}

// followJobs cancels the jobs whose tab a UI state change closed, those whose cluster's connection
// was interrupted, and those whose cluster was disconnected. Showing another cluster ends no job
// (D-69): a drain goes on while the window shows another cluster. UpdateUI calls it after the
// change.
func (s *Session) followJobs(old, now UIState) {
	s.jobs.mu.Lock()
	var cancels []func()
	for id, r := range s.jobs.running {
		has := func(dock []DockTab) bool {
			return slices.ContainsFunc(dock, func(t DockTab) bool { return t.ID == id })
		}
		was, is := old.connectionState(r.context), now.connectionState(r.context)
		switch {
		case has(old.Dock) && !has(now.Dock): // this change closed it (StartJob registers a job before its tab opens)
			cancels = append(cancels, func() { r.cancel(errJobTabClosed) })
		case was == Connected && (is == Interrupted || is == LoginExpired):
			cancels = append(cancels, func() { r.cancel(errJobInterrupted) })
		case is == Connected && !old.loginLapsed(r.context) && now.loginLapsed(r.context):
			cancels = append(cancels, func() { r.cancel(errJobLoginLapsed) })
		case was.Live() && !is.Live():
			cancels = append(cancels, func() { r.cancel(errJobClusterChanged) })
		}
	}
	s.jobs.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

// forget drops a job that ended.
func (js *jobSet) forget(id string) {
	js.mu.Lock()
	defer js.mu.Unlock()
	delete(js.running, id)
}

// stopAll cancels every job and takes no new ones: the session closes.
func (js *jobSet) stopAll() {
	js.mu.Lock()
	js.closed = true
	running := js.running
	js.running = nil
	js.mu.Unlock()
	for _, r := range running {
		r.cancel(errJobSessionClosed)
	}
}

// upperFirst writes an error as a sentence: its first letter upper case, a full stop at the end.
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	if c := s[0]; c >= 'a' && c <= 'z' {
		s = string(c-'a'+'A') + s[1:]
	}
	if s[len(s)-1] != '.' {
		s += "."
	}
	return s
}
