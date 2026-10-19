package engine

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/terminal"
)

// The node shell (G7, G8; D-37; docs/security/privilege-footprint.md › Node shell): a root shell on
// a node, as Lens opens it. node.shell (commands/nodeshell.go), confirmed by the user for every
// origin, creates a privileged pod in kube-system on the node that shares the host's PID
// namespace; the dock's node shell tab waits for it to run (a watch on that pod) and execs nsenter
// into PID 1's namespaces through the pod shell's terminal path (terminal.StartNodeShell). The pod
// carries activeDeadlineSeconds, so that it ends even if Cogpit dies, and Cogpit deletes it when
// the shell ends, the tab closes, or Cogpit quits. Its image, an image pull secret and its maximum
// duration (nodeshell_duration.go) are cluster settings (Cluster Settings › Node Shell), the image
// pinned by digest by default.

// The node shell's names and bounds.
const (
	// NodeShellCommand is the id of the node shell's command.
	NodeShellCommand = "node.shell"
	// NodeShellNamespace is where the pod runs: kube-system, as Lens runs it.
	NodeShellNamespace = "kube-system"
	// NodeShellContainer is the pod's one container.
	NodeShellContainer = "shell"

	// DefaultNodeShellImage is the newest supported Alpine release, pinned to the digest of its
	// multi-arch index (owner, 2026-10-14, D-37): its BusyBox has nsenter and sleep. Chosen on
	// 2026-10-14 as 3.24 (3.24.2, supported until 2028-06-01), the digest read with `docker buildx
	// imagetools inspect docker.io/library/alpine:3.24`. Renovate moves the tag to the next release
	// and the digest to the tag's newest build (.github/renovate.json reads the comment below), and
	// CI's kind job runs every such change on a node (TestKindNodeShell).
	// renovate: datasource=docker depName=docker.io/library/alpine
	DefaultNodeShellImage = "docker.io/library/alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6"

	// DockNodeShell is a node shell's dock tab.
	DockNodeShell DockType = "nodeShell"

	nodeShellNamePrefix    = "cogpit-node-shell-"
	nodeShellStartTimeout  = 3 * time.Minute  // the pod pulls its image and starts
	nodeShellCreateTimeout = 30 * time.Second // the create request
	nodeShellDeleteTimeout = 30 * time.Second // one delete request
	nodeShellQuitWait      = 10 * time.Second // what Close waits for the deletes
	maxNodeShellImage      = 512
)

// NodeShellLabels mark a node shell's pod as Cogpit's.
var NodeShellLabels = map[string]string{
	"app.kubernetes.io/name":       "cogpit-node-shell",
	"app.kubernetes.io/managed-by": "cogpit",
}

// ErrNodeShellGUIOnly refuses an agent or the CLI setting the node shell's image, pull secret or
// maximum duration: they decide what runs as root on the node at the user's next node shell, and
// for how long. Resetting them to the defaults is allowed.
var ErrNodeShellGUIOnly = errors.New("the node shell's image, image pull secret and maximum duration are set only by the user, " +
	"in Cogpit's window (Cluster Settings › Node Shell): an agent or the CLI may reset them to the defaults, not set them")

// DefaultNodeShellImageName is DefaultNodeShellImage without its registry, path and digest
// (alpine:3.24), as the texts name it.
var DefaultNodeShellImageName = imageName(DefaultNodeShellImage)

// imageName is an image reference's last path element without its digest.
func imageName(image string) string {
	if at := strings.LastIndex(image, "@"); at >= 0 {
		image = image[:at]
	}
	return image[strings.LastIndex(image, "/")+1:]
}

// NodeShellActions are the cluster side of a node shell. *cluster.Conn implements them; tests put
// a fake in Backend.Actions.
type NodeShellActions interface {
	// CreatePod creates the pod as given.
	CreatePod(ctx context.Context, pod *corev1.Pod) (*corev1.Pod, error)
	// WaitPodRunning waits, watching the one pod, until its container runs.
	WaitPodRunning(ctx context.Context, namespace, name, container string) error
	// DeletePod deletes the pod; a non-empty uid is a precondition.
	DeletePod(ctx context.Context, namespace, name, uid string) error
}

var _ NodeShellActions = (*cluster.Conn)(nil)

// NodeShellTab is what a node shell tab names (DockTab.NodeShell): the node and the pod.
type NodeShellTab struct {
	Node      string `json:"node"`
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Image     string `json:"image"`
	// Pinned says whether the image names its digest; false: a floating tag.
	Pinned bool `json:"pinned"`
	// DeadlineSeconds is the pod's activeDeadlineSeconds.
	DeadlineSeconds int64 `json:"deadlineSeconds"`
}

// imageDigest is the end of an image reference pinned by digest.
var imageDigest = regexp.MustCompile(`@sha256:[0-9a-f]{64}$`)

// ImagePinned reports whether an image reference names its digest (repository@sha256:...).
func ImagePinned(image string) bool { return imageDigest.MatchString(image) }

// CheckNodeShellImage refuses what cannot be an image reference: empty is the default, else one
// line of at most 512 bytes without spaces, a URL's scheme or control characters. Whether the
// registry has it is the kubelet's to say.
func CheckNodeShellImage(image string) error {
	return checkImageReference("the node shell's image", "docker.io/library/alpine:3.24", image)
}

// checkImageReference is CheckNodeShellImage's rule for any image a setting names (what), the
// examples naming example: empty passes, else one line of at most 512 bytes without spaces, a URL's
// scheme or control characters.
func checkImageReference(what, example, image string) error {
	switch {
	case image == "":
		return nil
	case len(image) > maxNodeShellImage:
		return fmt.Errorf("%s: at most %d characters", what, maxNodeShellImage)
	case strings.IndexFunc(image, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0:
		return fmt.Errorf("%s: a reference without spaces, such as %s@sha256:…", what, example)
	case strings.Contains(image, "://"):
		return fmt.Errorf("%s: a reference such as %s, not a URL", what, example)
	}
	return nil
}

// CheckNodeShellPullSecret refuses a pull secret's name a Secret cannot have; empty is none.
func CheckNodeShellPullSecret(name string) error {
	if name == "" {
		return nil
	}
	if errs := validation.IsDNS1123Subdomain(name); len(errs) > 0 {
		return fmt.Errorf("the node shell's image pull secret %q is not a Secret's name: %s", name, strings.Join(errs, "; "))
	}
	return nil
}

// NodeShellConfig is what a context's node shells run with: its Cluster Settings › Node Shell, or
// the defaults.
type NodeShellConfig struct {
	// Image is the pod's image; DefaultNodeShellImage unless the user set one.
	Image string
	// PullSecret is an image pull secret in kube-system; empty: none.
	PullSecret string
	// Deadline is the pod's activeDeadlineSeconds and how long its container sleeps;
	// DefaultNodeShellDeadline unless the user set the maximum duration.
	Deadline time.Duration
}

// NodeShellSettings returns a context's node shell configuration: its cluster settings', else the
// defaults (DefaultNodeShellImage, no pull secret, DefaultNodeShellDeadline).
func (s *Session) NodeShellSettings(context string) NodeShellConfig {
	cs, err := s.ClusterSettingsOf(context)
	if err != nil {
		cs = ClusterSettings{}
	}
	cfg := NodeShellConfig{Image: cs.NodeShellImage, PullSecret: cs.NodeShellPullSecret, Deadline: nodeShellDeadline(cs.NodeShellMaxMinutes)}
	if cfg.Image == "" {
		cfg.Image = DefaultNodeShellImage
	}
	return cfg
}

// NodeShellPod is the pod of a node shell on node: in kube-system, bound to the node (nodeName),
// tolerating every taint, sharing the host's PID namespace, its one container privileged and
// sleeping until the deadline ends the pod; no service account token, no service links, never
// restarted, a second to stop.
func NodeShellPod(name, node string, cfg NodeShellConfig) *corev1.Pod {
	deadline := int64(cfg.Deadline / time.Second)
	grace := int64(1)
	privileged, noToken, noLinks := true, false, false
	pod := &corev1.Pod{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: NodeShellNamespace, Labels: maps.Clone(NodeShellLabels)},
		Spec: corev1.PodSpec{
			NodeName:                      node,
			HostPID:                       true,
			RestartPolicy:                 corev1.RestartPolicyNever,
			ActiveDeadlineSeconds:         &deadline,
			TerminationGracePeriodSeconds: &grace,
			AutomountServiceAccountToken:  &noToken,
			EnableServiceLinks:            &noLinks,
			Tolerations:                   []corev1.Toleration{{Operator: corev1.TolerationOpExists}},
			Containers: []corev1.Container{{
				Name:            NodeShellContainer,
				Image:           cfg.Image,
				Command:         []string{"sleep", fmt.Sprint(deadline)},
				SecurityContext: &corev1.SecurityContext{Privileged: &privileged},
			}},
		},
	}
	if cfg.PullSecret != "" {
		pod.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: cfg.PullSecret}}
	}
	return pod
}

// NodeShellEffects are the node shell's rows in the confirmation: the privileged pod it creates,
// its namespace and node, the root access, the image and whether it is pinned, the pull secret,
// and how long the pod lives at most.
func NodeShellEffects(node string, cfg NodeShellConfig) []Effect {
	out := []Effect{
		{Name: "Creates", Value: fmt.Sprintf("a privileged pod in namespace %s on node %s", NodeShellNamespace, node), Tone: EffectWarning},
		{Name: "Access", Value: "root on the node: its processes and namespaces", Tone: EffectWarning},
	}
	out = append(out, imageEffects(cfg.Image)...) // debugcontainer.go
	if cfg.PullSecret != "" {
		out = append(out, Effect{Name: "Pull secret", Value: cfg.PullSecret, Tone: EffectNames})
	}
	return append(out, Effect{Name: "Ends", Value: "when the shell closes, by itself after " + NodeShellDeadlineText(cfg.Deadline)})
}

// NodeShellDetail is NodeShellEffects' text, the confirmation's Detail: StartNodeShell refuses
// when it no longer matches the settings.
func NodeShellDetail(node string, cfg NodeShellConfig) string {
	return EffectsText(NodeShellEffects(node, cfg))
}

// nodeShellName returns a new pod name: the prefix and eight random characters.
func nodeShellName() string {
	return nodeShellNamePrefix + strings.ToLower(rand.Text()[:8])
}

// StartNodeShell creates the node shell's pod on the node ref names and opens its dock tab. The
// caller (node.shell) was confirmed: the pod is created only for the confirmed context, node and
// detail (image, pull secret, maximum duration), else ErrWrongCluster or ErrTargetsChanged.
func (s *Session) StartNodeShell(ctx context.Context, ref model.ObjectRef) (DockTab, error) {
	b, err := s.TargetBackend(ref.Context) // the node's cluster, shown or not (D-69)
	if err != nil {
		return DockTab{}, err
	}
	w, ok := b.Writer().(NodeShellActions)
	if !ok {
		return DockTab{}, errors.New("this cluster connection cannot open node shells")
	}
	if err := CheckConfirmedContext(ctx, b.Context); err != nil {
		return DockTab{}, err
	}
	r, _, err := s.ResolveRef(ref)
	if err != nil {
		return DockTab{}, err
	}
	cfg := s.NodeShellSettings(b.Context)
	if req, ok := Confirmed(ctx); ok && req.Detail != NodeShellDetail(r.Name, cfg) {
		return DockTab{}, fmt.Errorf("%w: the node shell's image, pull secret or maximum duration changed since you confirmed; "+
			"nothing was created", ErrTargetsChanged)
	}
	if uid, bound := ConfirmedUID(ctx, r); bound {
		if obj, err := s.Lookup(r); err != nil || string(obj.GetUID()) != uid {
			return DockTab{}, fmt.Errorf("%w: node %s is not the one you confirmed; nothing was created", ErrTargetsChanged, r.Name)
		}
	}
	// The pod is recorded before its create is sent, so that a crash cannot leave one the journal
	// does not name (nodeshelljournal.go, H-71); the sweep leaves it alone until it is in s.shells.
	pod := NodeShellPod(nodeShellName(), r.Name, cfg)
	entry := NodeShellEntry{Context: b.Context, Namespace: pod.Namespace, Name: pod.Name, ID: rand.Text(), Node: r.Name, Created: s.Now()}
	pod.Annotations = map[string]string{NodeShellIDAnnotation: entry.ID}
	s.shells.pend(entry.key())
	defer s.shells.unpend(entry.key())
	journal := s.nodeShellJournal()
	if err := journal.Put(entry); err != nil {
		return DockTab{}, fmt.Errorf("node shell on %s: nothing was created: its pod could not be recorded first, "+
			"so a crash could leave it behind unnoticed: %w", r.Name, err)
	}
	cctx, cancel := context.WithTimeout(ctx, nodeShellCreateTimeout)
	defer cancel()
	created, err := w.CreatePod(cctx, pod)
	if err != nil {
		s.forgetNodeShell(entry.key()) // nothing to delete: the create failed (or the sweep finds no pod)
		return DockTab{}, fmt.Errorf("node shell on %s: %w", r.Name, err)
	}
	entry.UID = string(created.UID)
	if err := journal.Put(entry); err != nil { // without the UID the sweep finds the pod by name and id
		s.log.Warn("node shell: recording its pod's UID failed", "pod", created.Namespace+"/"+created.Name, "err", err)
	}
	tab := DockTab{
		ID: s.NewDockID(DockNodeShell), Type: DockNodeShell, Title: "Node shell: " + r.Name,
		Context: b.Context, Ref: &r, Container: NodeShellContainer,
		NodeShell: &NodeShellTab{Node: r.Name, Namespace: created.Namespace, Pod: created.Name, Image: cfg.Image,
			Pinned: ImagePinned(cfg.Image), DeadlineSeconds: int64(cfg.Deadline / time.Second)},
	}
	s.shells.add(tab.ID, &nodeShellPod{context: b.Context, namespace: created.Namespace, name: created.Name, uid: string(created.UID),
		deadline: cfg.Deadline, actions: w})
	s.UpdateUI(func(u *UIState) {
		u.Dock = append(u.Dock, tab)
		u.ActiveDock = tab.ID
	})
	return tab, nil
}

// openNodeShell starts a node shell tab's session: the notice, the wait for the pod (a watch,
// bounded by nodeShellStartTimeout), then nsenter through the pod shell's exec stream. When the
// session ends (the shell exited, the tab closed), the pod is deleted.
func (s *Session) openNodeShell(ctx context.Context, tab DockTab, b *Backend) (terminal.Session, error) {
	p, ok := s.shells.get(tab.ID)
	if !ok || tab.NodeShell == nil {
		return nil, errors.New("this node shell has ended and its pod is deleted: open a new one")
	}
	notice := fmt.Sprintf("[cogpit] Node shell on %s: privileged pod %s/%s, image %s; the pod ends by itself after %s.\r\n"+
		"[cogpit] Waiting for the pod to run…\r\n", tab.NodeShell.Node, p.namespace, p.name, tab.NodeShell.Image,
		NodeShellDeadlineText(p.deadline))
	before := func(ctx context.Context) error {
		wctx, cancel := context.WithTimeout(ctx, nodeShellStartTimeout)
		defer cancel()
		err := p.actions.WaitPodRunning(wctx, p.namespace, p.name, NodeShellContainer)
		if err != nil && ctx.Err() == nil && errors.Is(wctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("the pod did not run within %s", nodeShellStartTimeout)
		}
		return err
	}
	sess, err := terminal.StartNodeShell(ctx, b.Conn.RESTConfig(), b.Conn.Kube(), terminal.NodeShellOptions{
		ExecOptions: terminal.ExecOptions{Namespace: p.namespace, Pod: p.name, Container: NodeShellContainer},
		Notice:      notice, Before: before,
	})
	if err != nil {
		s.endNodeShell(tab.ID)
		return nil, err
	}
	go func() {
		defer s.recoverGoroutine("node shell")
		<-sess.Done()
		// The shell ended or the tab closed: the pod goes. One the connection's interruption ended
		// stays for the tab's Reconnect (D-58, S7); it ends by its deadline or with its tab.
		if b.watch.settle(nodeShellSettleWait) != Connected {
			return
		}
		if t, ok := s.UIState().DockTab(tab.ID); ok && !t.Interrupted.IsZero() {
			return
		}
		s.endNodeShell(tab.ID)
	}()
	return sess, nil
}

// nodeShellPod is a node shell's pod, kept until it is deleted.
type nodeShellPod struct {
	context, namespace, name, uid string
	deadline                      time.Duration // its activeDeadlineSeconds
	actions                       NodeShellActions
}

func (p *nodeShellPod) key() nodeShellKey { return nodeShellKey{p.context, p.namespace, p.name} }

// nodeShellSet holds the session's node shell pods by their dock tab, and the deletes in flight.
// pending and deleting are the pods being created and being deleted, which the journal's sweep
// leaves to their own goroutine (nodeshellsweep.go).
type nodeShellSet struct {
	mu       sync.Mutex
	pods     map[string]*nodeShellPod
	pending  map[nodeShellKey]bool
	deleting map[nodeShellKey]bool
	wg       sync.WaitGroup

	// The connections the sweep followed (nodeshellsweep.go).
	sweepRev  uint64
	sweepConn ConnectionState
}

// add records a tab's pod and ends its pending state under the same lock.
func (n *nodeShellSet) add(tab string, p *nodeShellPod) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.pods == nil {
		n.pods = map[string]*nodeShellPod{}
	}
	n.pods[tab] = p
	delete(n.pending, p.key())
}

// pend marks a pod as being created; unpend ends that (add ends it too).
func (n *nodeShellSet) pend(k nodeShellKey) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.pending == nil {
		n.pending = map[nodeShellKey]bool{}
	}
	n.pending[k] = true
}

func (n *nodeShellSet) unpend(k nodeShellKey) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.pending, k)
}

// busy reports whether this session creates, holds or deletes the pod k names.
func (n *nodeShellSet) busy(k nodeShellKey) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.pending[k] || n.deleting[k] {
		return true
	}
	for _, p := range n.pods {
		if p.key() == k {
			return true
		}
	}
	return false
}

// deleted ends a pod's delete (take, takeAll).
func (n *nodeShellSet) deleted(p *nodeShellPod) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.deleting, p.key())
}

// markDeleting marks a pod's delete as in flight; n.mu is held.
func (n *nodeShellSet) markDeleting(p *nodeShellPod) {
	if n.deleting == nil {
		n.deleting = map[nodeShellKey]bool{}
	}
	n.deleting[p.key()] = true
	n.wg.Add(1)
}

func (n *nodeShellSet) get(tab string) (*nodeShellPod, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	p, ok := n.pods[tab]
	return p, ok
}

// take removes and returns a tab's pod, counting its delete as in flight (wg) under the same lock,
// so that a quit's wait sees every delete a pod was taken for; nil when it has none (any more).
func (n *nodeShellSet) take(tab string) *nodeShellPod {
	n.mu.Lock()
	defer n.mu.Unlock()
	p := n.pods[tab]
	if p != nil {
		delete(n.pods, tab)
		n.markDeleting(p)
	}
	return p
}

// takeAll removes and returns every pod, each delete counted as in flight as take does.
func (n *nodeShellSet) takeAll() []*nodeShellPod {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]*nodeShellPod, 0, len(n.pods))
	for _, p := range n.pods {
		out = append(out, p)
		n.markDeleting(p)
	}
	n.pods = nil
	return out
}

// NodeShellPods returns the pods of the session's open node shells by their dock tab: namespace
// and name.
func (s *Session) NodeShellPods() map[string]string {
	s.shells.mu.Lock()
	defer s.shells.mu.Unlock()
	out := make(map[string]string, len(s.shells.pods))
	for tab, p := range s.shells.pods {
		out[tab] = p.namespace + "/" + p.name
	}
	return out
}

// endNodeShell deletes a node shell tab's pod in the background, once.
func (s *Session) endNodeShell(tab string) {
	p := s.shells.take(tab) // counted in s.shells.wg
	if p == nil {
		return
	}
	go func() {
		defer s.shells.wg.Done()
		defer s.recoverGoroutine("node shell delete")
		s.deleteNodeShellPod(p)
	}()
}

// deleteNodeShellPod deletes one pod with its UID as precondition and drops its journal entry; a
// pod that is gone already, or whose name another pod has now (the UID precondition fails), is no
// failure. A failure is a warning toast: the entry stays, the pod lives until its deadline, and the
// next connect to its context deletes it (nodeshellsweep.go).
func (s *Session) deleteNodeShellPod(p *nodeShellPod) {
	defer s.shells.deleted(p)
	ctx, cancel := context.WithTimeout(context.Background(), nodeShellDeleteTimeout)
	defer cancel()
	err := p.actions.DeletePod(ctx, p.namespace, p.name, p.uid)
	if err == nil || apierrors.IsNotFound(err) || apierrors.IsConflict(err) {
		s.forgetNodeShell(p.key())
		return
	}
	s.log.Warn("node shell: deleting its pod failed", "pod", p.namespace+"/"+p.name, "err", err)
	s.emit(Event{Type: EventToast, Level: LevelWarning, Message: fmt.Sprintf(
		"The node shell's pod %s/%s was not deleted (%v); it ends by itself within %s of its start, and Cogpit deletes it "+
			"when you next connect to %s.", p.namespace, p.name, err, NodeShellDeadlineText(p.deadline), p.context)})
}

// endClosedNodeShells deletes the pods of the node shell tabs that left the dock.
func (s *Session) endClosedNodeShells(old, now []DockTab) {
	for _, t := range old {
		if t.Type == DockNodeShell && !slices.ContainsFunc(now, func(n DockTab) bool { return n.ID == t.ID }) {
			s.endNodeShell(t.ID)
		}
	}
}

// endNodeShells deletes every node shell's pod and waits for the deletes, at most
// nodeShellQuitWait: Cogpit quits.
func (s *Session) endNodeShells() {
	for _, p := range s.shells.takeAll() { // each counted in s.shells.wg
		go func() {
			defer s.shells.wg.Done()
			defer s.recoverGoroutine("node shell delete")
			s.deleteNodeShellPod(p)
		}()
	}
	done := make(chan struct{})
	go func() {
		s.shells.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(nodeShellQuitWait):
		s.log.Warn("node shell: pods still being deleted at quit; they end by their deadline")
	}
}

// recoverGoroutine logs a panic of one of the session's own goroutines instead of ending the
// process.
func (s *Session) recoverGoroutine(what string) {
	if r := recover(); r != nil {
		s.log.Error("engine: goroutine panicked", "goroutine", what, "panic", fmt.Sprint(r))
	}
}
