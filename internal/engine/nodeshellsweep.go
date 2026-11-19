package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/guided-traffic/cogpit/internal/cluster"
)

// The node shell pods a crash left (H-71, owner): when Cogpit connects to a context, it deletes the
// node shell pods its journal (nodeshelljournal.go) still names for that context, each with its UID
// as the delete's precondition, and says so in a notification. It never deletes a pod it did not
// create: only pods its own journal names, by UID; an entry recorded before the create returned (no
// UID) is matched by name and then by the random id the pod carries (NodeShellIDAnnotation) before
// its UID is used. Entries of another live Cogpit process of the user, and pods this session is
// creating, holds or deleting, are left alone. A pod that is gone, or whose name another pod has
// now, only clears its entry; a refused delete (forbidden, a lost connection) keeps the entry, says
// so, and is tried again at the next connect. Every delete is in the audit log. It runs off the UI
// goroutine and recovers from a panic (goSafe).

// NodeShellIDAnnotation carries a node shell pod's random id, the journal entry's ID.
const NodeShellIDAnnotation = "cogpit.io/node-shell-id"

// NodeShellCleanupAudit is the audit log's name for the delete of a node shell pod a crash left;
// its origin is "gui" (it follows a connect in the window) and its parameters name the pod.
const NodeShellCleanupAudit = "cogpit.nodeShellCleanup"

// NodeShellCleanupVerbs are the requests of that cleanup, in kube-system: the delete with its UID
// precondition, and a get of the pod by name for an entry recorded without its UID
// (privilege-footprint.md › Node shell).
var NodeShellCleanupVerbs = []Verb{
	{Verb: "get", Resource: kindPods, Namespace: NodeShellNamespace},
	{Verb: "delete", Resource: kindPods, Namespace: NodeShellNamespace},
}

// NodeShellPodReader reads one pod by name, for an entry recorded without its UID. *cluster.Conn
// implements it; a connection without it leaves such entries for the pod's deadline.
type NodeShellPodReader interface {
	GetPod(ctx context.Context, namespace, name string) (*corev1.Pod, error)
}

var _ NodeShellPodReader = (*cluster.Conn)(nil)

// nodeShellJournal returns the session's journal (Options.NodeShells, an in-memory one by default).
func (s *Session) nodeShellJournal() NodeShellJournal { return s.opts.NodeShells }

// forgetNodeShell drops a pod's journal entry; a journal that cannot be written is logged, and the
// next connect's sweep finds the pod gone.
func (s *Session) forgetNodeShell(k nodeShellKey) {
	if err := s.nodeShellJournal().Remove(k.context, k.namespace, k.name); err != nil {
		s.log.Warn("node shell: dropping its journal entry failed", "pod", k.namespace+"/"+k.name, "err", err)
	}
}

// followNodeShellSweep runs the sweep for each context the session connects to (EventUIState). It
// must not block: the sweep runs on its own goroutine.
func (s *Session) followNodeShellSweep(ev Event) {
	if ev.Type != EventUIState || ev.UIState == nil {
		return
	}
	u := ev.UIState
	n := &s.shells
	n.mu.Lock()
	if u.Revision <= n.sweepRev { // an older state delivered late
		n.mu.Unlock()
		return
	}
	n.sweepRev = u.Revision
	prev := n.sweepConn
	n.sweepConn = u.Connection.State
	n.mu.Unlock()
	if u.Connection.State != Connected || prev.Live() || u.Connection.Demo { // a reconnect after an interruption is no connect (D-58)
		return
	}
	ctxName := u.Context
	s.goSafe("node shell cleanup", func() { s.sweepNodeShells(ctxName) })
}

// nodeShellOutcome is what the sweep did with one entry.
type nodeShellOutcome int

const (
	shellGone    nodeShellOutcome = iota // no pod of Cogpit's under the name: entry cleared
	shellDeleted                         // the pod was deleted: entry cleared
	shellKept                            // not deleted: the entry stays for the next connect
)

// sweepNodeShells deletes the pods the journal names for ctxName, as the connected cluster has
// them, and tells the user what it did.
func (s *Session) sweepNodeShells(ctxName string) {
	b := s.BackendOf(ctxName) // its cluster, shown or not by now (D-69)
	if b == nil || ctxName == "" {
		return
	}
	acts, ok := b.Writer().(NodeShellActions)
	if !ok {
		return
	}
	entries, err := s.nodeShellJournal().List()
	if err != nil {
		s.log.Error("engine: the node shell journal is unreadable", "err", err)
		s.emit(Event{Type: EventToast, Level: LevelError, Message: "Node shell pods an earlier run left cannot be deleted: " + err.Error()})
		return
	}
	var deleted, kept []string
	for _, e := range entries {
		if e.Context != ctxName || e.OwnedElsewhere() || s.shells.busy(e.key()) {
			continue
		}
		if !s.state.holds(b) { // disconnected meanwhile: its next connect sweeps it
			return
		}
		what := fmt.Sprintf("%s/%s (node %s)", e.Namespace, e.Name, e.Node)
		switch outcome, err := s.cleanNodeShell(acts, e); outcome {
		case shellDeleted:
			deleted = append(deleted, what)
		case shellKept:
			kept = append(kept, what+": "+keptReason(err))
		}
	}
	if len(deleted) > 0 {
		w := shellWords(len(deleted))
		s.emit(Event{Type: EventToast, Level: LevelInfo, Message: fmt.Sprintf(
			"Cogpit deleted %s it had left in context %s (an earlier run ended before deleting %s): %s.",
			w.pods, ctxName, w.them, strings.Join(deleted, ", "))})
	}
	if len(kept) > 0 {
		w := shellWords(len(kept))
		s.emit(Event{Type: EventToast, Level: LevelWarning, Message: fmt.Sprintf(
			"Cogpit could not delete %s it had left in context %s; %s by %s deadline, and Cogpit tries again at the next connect: %s.",
			w.pods, ctxName, w.theyEnd, w.their, strings.Join(kept, "; "))})
	}
}

// nodeShellWords are the sweep's notification's words for one pod or several.
type nodeShellWords struct{ pods, them, theyEnd, their string }

func shellWords(n int) nodeShellWords {
	if n == 1 {
		return nodeShellWords{"a node shell pod", "it", "it ends", "its"}
	}
	return nodeShellWords{fmt.Sprintf("%d node shell pods", n), "them", "they end", "their"}
}

// cleanNodeShell deletes one entry's pod, or finds it gone, and clears or keeps the entry.
func (s *Session) cleanNodeShell(acts NodeShellActions, e NodeShellEntry) (nodeShellOutcome, error) {
	ctx, cancel := context.WithTimeout(context.Background(), nodeShellDeleteTimeout)
	defer cancel()
	uid := e.UID
	if uid == "" {
		r, ok := acts.(NodeShellPodReader)
		if !ok {
			return shellKept, fmt.Errorf("its UID was not recorded and this connection cannot read the pod")
		}
		pod, err := r.GetPod(ctx, e.Namespace, e.Name)
		switch {
		case apierrors.IsNotFound(err):
			s.forgetNodeShell(e.key())
			return shellGone, nil
		case err != nil:
			return shellKept, err
		case pod.Annotations[NodeShellIDAnnotation] != e.ID:
			s.forgetNodeShell(e.key()) // a pod of the name that is not the one Cogpit created
			return shellGone, nil
		}
		uid = string(pod.UID)
	}
	err := acts.DeletePod(ctx, e.Namespace, e.Name, uid)
	s.auditNodeShellCleanup(e, uid, err)
	switch {
	case err == nil:
		s.forgetNodeShell(e.key())
		return shellDeleted, nil
	case apierrors.IsNotFound(err), apierrors.IsConflict(err): // gone, or the name is another pod's (UID precondition)
		s.forgetNodeShell(e.key())
		return shellGone, nil
	}
	s.log.Warn("node shell: deleting a pod an earlier run left failed", "pod", e.Namespace+"/"+e.Name, "err", err)
	return shellKept, err
}

// auditNodeShellCleanup writes the delete's line: origin gui, the pod, its UID and node.
func (s *Session) auditNodeShellCleanup(e NodeShellEntry, uid string, err error) {
	params, _ := json.Marshal(struct {
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
		UID       string `json:"uid"`
		Node      string `json:"node"`
	}{e.Namespace, e.Name, uid, e.Node})
	s.audit(Invocation{ID: NodeShellCleanupAudit, Params: params, Origin: OriginGUI}, e.Context, auditDone, err)
}

// keptReason says why a pod was kept: forbidden in so many words, else the error.
func keptReason(err error) string {
	if apierrors.IsForbidden(err) {
		return "you may not delete pods in " + NodeShellNamespace
	}
	if err == nil {
		return "not deleted"
	}
	return err.Error()
}
