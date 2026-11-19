package commands

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// node.drain as a background job (D-63): after its confirmation (destructive, for every origin) the
// command records the drain in the journal, starts a job and returns at once with the job's dock
// tab. The job's steps are the cordon, then one per pod the drain evicts: the cache's pods on the
// node while the drain has not listed them (when the cache holds the cluster's pods: the Nodes view
// reads them), the drain's own list once it has. A pod's step runs while its eviction is asked for
// ("Evicting"), refused for now ("Waiting: the PodDisruptionBudget web allows no disruption now
// (…); retrying") and accepted ("Evicted; waiting for the pod to end"), and ends done ("Evicted",
// "Gone already"), failed with the API server's error, or stopped. The job's outcome counts the
// pods evicted, failed and not attempted.
//
// The job keeps the command's context values but not its cancellation (JobSpec.Parent): an MCP
// client that gives up on its call, or the call's end, no longer cancels the drain the user
// confirmed. job.cancel, closing the tab, another cluster, an interrupted connection (D-58) and
// Cogpit closing stop it: the drain's context ends, the library stops at its next request or wait,
// and the journal keeps the drain as stopped with the cause. Nothing is undone and the node is not
// uncordoned: the banner offers Finish drain, Uncordon and Dismiss, as after a crash (D-11, S8).
// The journal is as crash-safe as before: written before the cordon, removed when every pod is
// gone, kept failed or stopped otherwise.

// Texts of the drain job's steps and outcome.
const (
	drainCordoned  = "Cordoned"
	drainEvicting  = "Evicting"
	drainAccepted  = "Evicted; waiting for the pod to end"
	drainEvicted   = "Evicted"
	drainLingering = "Evicted; the pod was still shutting down when the drain ended"
	// drainNotListed is why a pod step is not attempted when the drain ended by itself first.
	drainNotListed = "the drain ended first"
)

// mirrorPodAnnotation marks a static pod's mirror, which the drain leaves alone.
const mirrorPodAnnotation = "kubernetes.io/config.mirror"

// drainPhase is how far a pod step's eviction got while it runs.
type drainPhase int

const (
	phaseEvicting drainPhase = iota + 1 // asked for, not answered yet
	phaseBlocked                        // refused for now; the drain retries
	phaseAccepted                       // accepted; the pod shuts down
)

// drainJob follows one drain in its job's steps: the cordon first (step 0), then a step per pod.
// The drain's progress calls come one at a time (cluster.DrainNode), and its end after the last of
// them; the lock keeps the steps consistent with the job's anyway.
type drainJob struct {
	b    *engine.Backend
	node model.ObjectRef

	mu      sync.Mutex
	j       *engine.Job // nil until the job runs
	steps   []engine.JobStep
	pods    map[string]int        // "namespace/name" → its step
	phase   map[string]drainPhase // a pod step under way: how far its eviction got
	waiting map[string]string     // a pod refused for now: why
	left    string                // the pods the drain leaves running (DrainListed's message)
	total   int                   // the pods the drain's list counts (DrainListed's Total)
}

// newDrainJob prepares the steps: the cordon, then the pods the cache holds on the node that the
// drain would evict with these settings, when the cache holds the cluster's pods now.
func newDrainJob(s *engine.Session, b *engine.Backend, node model.ObjectRef, settings engine.DrainSettings) *drainJob {
	dj := &drainJob{b: b, node: node, pods: map[string]int{}, phase: map[string]drainPhase{}, waiting: map[string]string{}}
	ref := node
	dj.steps = []engine.JobStep{{Ref: &ref, Text: stepText(b, node)}}
	if cached, ok := s.CachedNodePods(b.Context, node.Name); ok {
		dj.setPods(previewPods(cached, settings))
	}
	return dj
}

// previewPods are the cached pods a drain with these settings would evict, as "namespace/name",
// sorted: every pod on the node but mirror pods and, when the drain leaves them running, the
// running pods of a DaemonSet. The drain's own list replaces them once it has listed the node.
func previewPods(pods []*unstructured.Unstructured, settings engine.DrainSettings) []string {
	ignoreDS := settings.WithDefaults().IgnoreDaemonSets
	var out []string
	for _, p := range pods {
		if _, mirror := p.GetAnnotations()[mirrorPodAnnotation]; mirror {
			continue
		}
		phase := model.NestedString(p.Object, "status", "phase")
		finished := phase == "Succeeded" || phase == "Failed"
		if *ignoreDS && !finished && daemonSetPod(p) {
			continue
		}
		out = append(out, p.GetNamespace()+"/"+p.GetName())
	}
	slices.Sort(out)
	return out
}

// daemonSetPod reports whether a DaemonSet controls the pod.
func daemonSetPod(p *unstructured.Unstructured) bool {
	for _, o := range p.GetOwnerReferences() {
		if o.Controller != nil && *o.Controller && o.Kind == "DaemonSet" {
			return true
		}
	}
	return false
}

// setPods makes the pod steps those of pods, in their order; the cordon's step stays.
func (dj *drainJob) setPods(pods []string) {
	dj.steps = dj.steps[:1]
	clear(dj.pods)
	for _, name := range pods {
		dj.pods[name] = len(dj.steps)
		dj.steps = append(dj.steps, dj.podStep(name))
	}
}

// podStep is a pending step for a pod, "namespace/name".
func (dj *drainJob) podStep(name string) engine.JobStep {
	ns, pod, _ := strings.Cut(name, "/")
	ref := model.ObjectRef{Context: dj.node.Context, Kind: kindPods, Namespace: ns, Name: pod}
	return engine.JobStep{Ref: &ref, Text: stepText(dj.b, ref), State: engine.JobPending}
}

// initialSteps are the steps the job starts with.
func (dj *drainJob) initialSteps() []engine.JobStep {
	dj.mu.Lock()
	defer dj.mu.Unlock()
	return slices.Clone(dj.steps)
}

// start binds the running job and starts the cordon's step.
func (dj *drainJob) start(j *engine.Job) {
	dj.mu.Lock()
	defer dj.mu.Unlock()
	dj.j = j
	dj.set(0, engine.JobRunning, "")
}

// set moves step i in the steps and the job's tab.
func (dj *drainJob) set(i int, state engine.JobState, detail string) {
	dj.steps[i].State, dj.steps[i].Detail = state, detail
	if dj.j != nil {
		dj.j.SetStep(i, state, detail)
	}
}

// publish shows the steps in the job's tab after they changed in number.
func (dj *drainJob) publish() {
	if dj.j != nil {
		dj.j.SetSteps(dj.steps)
	}
}

// progress moves the steps as the drain reports.
func (dj *drainJob) progress(pr cluster.DrainProgress) {
	dj.mu.Lock()
	defer dj.mu.Unlock()
	switch pr.Step {
	case cluster.DrainCordoned:
		dj.set(0, engine.JobDone, drainCordoned)
	case cluster.DrainListed:
		dj.listed(pr)
	case cluster.DrainEvicting:
		dj.pod(pr.Pod, phaseEvicting, engine.JobRunning, drainEvicting)
	case cluster.DrainBlocked:
		dj.waiting[pr.Pod] = pr.Message
		dj.pod(pr.Pod, phaseBlocked, engine.JobRunning, "Waiting: "+pr.Message+"; retrying")
	case cluster.DrainAccepted:
		dj.pod(pr.Pod, phaseAccepted, engine.JobRunning, drainAccepted)
	case cluster.DrainEvicted:
		detail := drainEvicted
		if pr.Message != "" {
			detail = upperFirst(pr.Message)
		}
		dj.pod(pr.Pod, 0, engine.JobDone, detail)
	case cluster.DrainPodFailed:
		dj.pod(pr.Pod, 0, engine.JobFailed, upperFirst(pr.Message))
	}
}

// listed makes the pod steps the drain's own list and notes what it leaves running in the
// cordon's step. A list that does not name every pod it counts keeps the steps as they are.
func (dj *drainJob) listed(pr cluster.DrainProgress) {
	dj.total = pr.Total
	if pr.Message != "" {
		dj.left = pr.Message
		dj.steps[0].Detail = drainCordoned + "; " + pr.Message
	}
	if len(pr.Pods) == pr.Total {
		dj.setPods(slices.Sorted(slices.Values(pr.Pods)))
	}
	dj.publish()
}

// pod moves a pod's step; a pod the steps do not name gets one, at the end. A step that ended
// stays as it ended.
func (dj *drainJob) pod(name string, phase drainPhase, state engine.JobState, detail string) {
	i, ok := dj.pods[name]
	if !ok {
		i = len(dj.steps)
		dj.pods[name] = i
		dj.steps = append(dj.steps, dj.podStep(name))
		dj.publish()
	}
	if dj.steps[i].State.Ended() {
		return
	}
	if phase != 0 {
		dj.phase[name] = phase
	}
	dj.set(i, state, detail)
}

// finish ends the steps the drain left open and returns the job's outcome: the pods evicted and,
// when the drain did not finish, an error that counts the pods failed and not attempted, says
// why (stop, when the job was stopped; else the drain's error) and what the node is left as.
func (dj *drainJob) finish(drainErr, stop error) (string, error) {
	dj.mu.Lock()
	defer dj.mu.Unlock()
	if !dj.steps[0].State.Ended() {
		switch {
		case stop != nil:
			dj.set(0, engine.JobCancelled, "Stopped: "+stop.Error())
		case drainErr != nil:
			dj.set(0, engine.JobFailed, upperFirst(dj.trim(drainErr)))
		default:
			dj.set(0, engine.JobDone, drainCordoned)
		}
	}
	names := make([]string, len(dj.steps))
	for name, i := range dj.pods {
		names[i] = name
	}
	for i := 1; i < len(dj.steps); i++ {
		if !dj.steps[i].State.Ended() {
			state, detail := dj.openEnd(names[i], stop)
			dj.set(i, state, detail)
		}
	}
	var evicted, failed, skipped int
	for _, st := range dj.steps[1:] {
		switch st.State {
		case engine.JobDone:
			evicted++
		case engine.JobFailed:
			failed++
		default:
			skipped++
		}
	}
	total := len(dj.steps) - 1
	if drainErr == nil {
		text := fmt.Sprintf("Drained node %s: %s evicted.", dj.node.Name, podCount(evicted))
		if total == 0 {
			text = fmt.Sprintf("Drained node %s: no pods to evict.", dj.node.Name)
		}
		if dj.left != "" {
			text += " " + upperFirst(dj.left) + "."
		}
		return text, nil
	}
	var sentences []string
	if total > 0 {
		counts := fmt.Sprintf("Evicted %d of %s", evicted, podCount(total))
		var parts []string
		if failed > 0 {
			parts = append(parts, fmt.Sprintf("%d failed", failed))
		}
		if skipped > 0 {
			parts = append(parts, fmt.Sprintf("%d not attempted", skipped))
		}
		if len(parts) > 0 {
			counts += "; " + strings.Join(parts, ", ")
		}
		sentences = append(sentences, counts)
	}
	if stop == nil { // a stop's cause comes first in the outcome (engine.StartJob)
		sentences = append(sentences, upperFirst(strings.TrimSuffix(dj.trim(drainErr), ".")))
	}
	return "", errors.New(strings.Join(append(sentences, dj.leftAs()), ". "))
}

// openEnd is how a pod step still open at the drain's end ends: an accepted eviction counts as
// done (the pod is going), one under way or refused for now as stopped or failed, a pending one
// as not attempted.
func (dj *drainJob) openEnd(name string, stop error) (engine.JobState, string) {
	switch dj.phase[name] {
	case phaseAccepted:
		return engine.JobDone, drainLingering
	case phaseBlocked:
		if stop != nil {
			return engine.JobCancelled, "Stopped while waiting: " + dj.waiting[name]
		}
		return engine.JobFailed, "Not evicted: " + dj.waiting[name]
	case phaseEvicting:
		if stop != nil {
			return engine.JobCancelled, "Stopped before the API server answered its eviction"
		}
		return engine.JobFailed, "Not evicted: the drain ended before the API server answered its eviction"
	}
	why := drainNotListed
	if stop != nil {
		why = stop.Error()
	}
	return engine.JobCancelled, "Not attempted: " + why
}

// leftAs says what a drain that did not finish leaves, and what the user can do.
func (dj *drainJob) leftAs() string {
	if dj.steps[0].State != engine.JobDone {
		return fmt.Sprintf("Node %s was not cordoned; the drain stays unfinished (node.pendingDrains): "+
			"Finish drain runs it again, Dismiss forgets it", dj.node.Name)
	}
	return fmt.Sprintf("Node %s %s; the drain stays unfinished (node.pendingDrains): "+
		"Finish drain runs it again, Uncordon makes the node schedulable, Dismiss forgets it", dj.node.Name, dj.leftState())
}

// leftState says what the drain left of a cordoned node (drainLeft, nodes.go): no pod evicted, or how
// many of the node's pods were (a real cluster's drain was refused at once for its emptyDir pods
// and still said "partly drained"). Call it with dj.mu held or after the run.
func (dj *drainJob) leftState() string {
	evicted := 0
	for _, st := range dj.steps[1:] {
		if st.State == engine.JobDone {
			evicted++
		}
	}
	return drainLeft(dj.steps[0].State == engine.JobDone, cluster.DrainProgress{Done: evicted, Total: max(dj.total, len(dj.steps)-1)})
}

// trim is a drain error without the "drain node <name>: " its every message starts with.
func (dj *drainJob) trim(err error) string {
	return strings.TrimPrefix(err.Error(), "drain node "+dj.node.Name+": ")
}

// podCount is "1 pod", "3 pods".
func podCount(n int) string {
	if n == 1 {
		return "1 pod"
	}
	return fmt.Sprintf("%d pods", n)
}

// run is the job's work: drain, record the outcome in the journal, end the steps, and say how it
// ended in a toast when it ended by itself.
func (dj *drainJob) run(ctx context.Context, s *engine.Session, j *engine.Job, w engine.NodeActions, entry engine.PendingDrain,
	opts cluster.DrainOptions) (string, error) {
	d := s.Drains()
	dj.start(j)
	d.SetJob(entry.Context, entry.Node, j.ID())
	drainErr := drainSafely(ctx, s, w, entry.Node, opts, func(pr cluster.DrainProgress) {
		d.Progress(entry.Context, pr)
		dj.progress(pr)
	})
	var stop error
	journalErr := drainErr
	if drainErr != nil && ctx.Err() != nil {
		stop = cmp.Or(context.Cause(ctx), ctx.Err())
		journalErr = &engine.DrainStoppedError{Cause: stop}
	}
	if err := d.End(entry, journalErr); err != nil {
		s.Logger().Error("node.drain: recording the drain's end", "node", entry.Node, "err", err)
		userToast(s, engine.LevelError, "Cogpit could not record the end of the drain of node "+entry.Node+": "+err.Error())
	}
	outcome, err := dj.finish(drainErr, stop)
	switch {
	case drainErr == nil:
		dj.mu.Lock()
		evicted := 0
		for _, st := range dj.steps[1:] {
			if st.State == engine.JobDone {
				evicted++
			}
		}
		dj.mu.Unlock()
		userToast(s, engine.LevelInfo, fmt.Sprintf("Node %s drained: %s evicted", entry.Node, podCount(evicted)))
	case stop == nil: // stopped: the tab says why, and the banner offers the drain
		dj.mu.Lock()
		left := dj.leftState()
		dj.mu.Unlock()
		userToast(s, engine.LevelWarning, fmt.Sprintf("The drain of node %s did not finish; the node %s. "+
			"Finish the drain or uncordon the node.", entry.Node, left))
	}
	return outcome, err
}
