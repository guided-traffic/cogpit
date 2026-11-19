package demo

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// world is the typed state the published objects are rendered from. The churn changes it and
// renders a new object version per change; parts that never change (specs, labels, owner
// references, a node's capacity) are rendered once and shared by every version, which is safe
// because published objects are never modified and consumers get deep copies.
type world struct {
	namespaces []*namespace
	nodes      []*node
	workers    []*node // schedulable workers, for new pods
	deploys    []*deployment
	daemonSets []*daemonSet
	cronJobs   []*cronJob
	pods       []*pod // every pod; pod.idx is its index
	podKeys    map[string]struct{}
	events     []*event // oldest first, from evHead
	evHead     int

	notReady       []*pod // running pods of healthy deployments whose readiness fails
	notReadyTarget int    // the churn keeps notReady near this size
	unstable       []*pod // crash-looping and image-pull pods; they stay that way
	creating       []*pod // replacement pods in ContainerCreating, oldest first
	terminating    []*pod // pods with a deletionTimestamp, oldest first
	dirty          []*deployment

	// settle is how many changes a new pod spends in ContainerCreating and a deleted one in
	// Terminating: about two seconds at the configured rate. Counting changes instead of time
	// keeps the churn's sequence independent of the generator's timing.
	settle int64
}

type namespace struct {
	name        string
	uid         string
	created     time.Time
	labels      map[string]any
	terminating bool
	weight      int // share of the user workloads; 0 for none
}

type node struct {
	idx         int
	name        string
	uid         string
	created     time.Time
	role        string
	arch        string
	size        string
	zone        string
	cpu         int64 // cores
	memGi       int64
	gpu         int64
	kubelet     string
	ip          string
	podCIDR     string
	notReady    bool
	cordoned    bool
	readySince  time.Time
	heartbeat   time.Time
	nextIP      int
	labels      map[string]any
	annotations map[string]any
	spec        map[string]any
	static      map[string]any // status fields that never change
}

// podIP returns the next address in the node's /23 pod CIDR (10.244.2i.0/23).
func (n *node) podIP() string {
	if n.nextIP < 2 || n.nextIP > 509 {
		n.nextIP = 2
	}
	ip := n.nextIP
	n.nextIP++
	return fmt.Sprintf("10.%d.%d.%d", 244+n.idx/128, (n.idx%128)*2+ip/256, ip%256)
}

// profile is how a deployment's pods behave.
type profile int

const (
	profileHealthy  profile = iota
	profileCrash            // every pod crash-loops
	profileBadImage         // a rollout stuck on an image that does not exist
	profilePending          // pods too big for any node
)

type deployment struct {
	name       string
	namespace  string
	uid        string
	created    time.Time
	generation int64
	replicas   int
	profile    profile
	templates  [2]*podTemplate // [0] the running replica set; [1] a stuck rollout's new one
	rsHash     [2]string
	rsUID      [2]string
	current    int // the newest replica set: 1 for a stuck rollout
	pods       map[*pod]struct{}
	podLabels  [2]map[string]any // shared by the pods of each replica set
	podOwners  [2][]any
	rsCreated  [2]time.Time
	rsStatus   [2][3]int // each replica set's active, ready and available pods as last published
	selector   map[string]string
	labels     map[string]any
	spec       map[string]any
	dirty      bool

	available, progressing           string // condition status, "True" or "False"
	availableSince, progressingSince time.Time
}

// daemonSet is a DaemonSet and the pods the build placed for it, one a node (kube-proxy). The churn
// leaves its pods alone.
type daemonSet struct {
	name, namespace, uid string
	labels               map[string]string // its selector and its pods' own labels
	tmpl                 *podTemplate
	pods                 []*pod
}

// cronJob is a CronJob and the Jobs of its runs the build kept, newest first. The churn leaves
// them alone.
type cronJob struct {
	name, namespace, uid string
	tmpl                 *podTemplate
	jobs                 []*jobRun
}

// jobRun is one run of a CronJob: a Job and its one pod.
type jobRun struct {
	name, uid string
	scheduled time.Time
	pod       *pod
}

// counts returns the deployment's active (scheduled for the replica count), ready and updated
// pods. Terminating, failed and completed pods are not active, as for a ReplicaSet.
func (d *deployment) counts() (active, ready, updated int) {
	for p := range d.pods {
		if !p.deleting.IsZero() || p.phase == phaseSucceeded || p.phase == phaseFailed || p.phase == phaseEvicted {
			continue
		}
		active++
		if p.isReady() {
			ready++
		}
		if p.rs == d.current {
			updated++
		}
	}
	return active, ready, updated
}

// rsCounts returns the active and ready pods of one of the deployment's replica sets, counted
// as counts counts them.
func (d *deployment) rsCounts(rs int) (active, ready int) {
	for p := range d.pods {
		if p.rs != rs || !p.deleting.IsZero() || p.phase == phaseSucceeded || p.phase == phaseFailed || p.phase == phaseEvicted {
			continue
		}
		active++
		if p.isReady() {
			ready++
		}
	}
	return active, ready
}

// podPhase is a pod's lifecycle state; the Kubernetes phase and container states follow from it.
type podPhase int

const (
	phaseRunning       podPhase = iota // Running; ready says whether its readiness passes
	phaseCreating                      // Pending: ContainerCreating
	phaseUnschedulable                 // Pending: no node fits
	phaseCrashLoop                     // Running: the first container crash-loops
	phaseImagePull                     // Pending: the first container's image cannot be pulled
	phaseSucceeded                     // Completed
	phaseFailed                        // Error
	phaseEvicted                       // Failed: evicted by the kubelet
)

type pod struct {
	idx       int
	name      string
	namespace string
	uid       string
	created   time.Time
	labels    map[string]any
	owners    []any
	spec      map[string]any
	tmpl      *podTemplate
	deploy    *deployment // nil when no deployment owns the pod
	rs        int
	node      *node // nil when unscheduled
	ip        string

	phase       podPhase
	ready       bool
	readySince  time.Time
	started     time.Time
	restarts    int64
	inBackOff   bool      // phaseCrashLoop: waiting in CrashLoopBackOff (or briefly running)
	lastStart   time.Time // phaseCrashLoop: the first container's latest start
	lastExit    time.Time // the first container's latest termination
	pullBackOff bool      // phaseImagePull: ImagePullBackOff (or ErrImagePull)
	deleting    time.Time // deletionTimestamp; zero when not terminating
	since       int64     // resourceVersion when it entered ContainerCreating or Terminating
}

func (p *pod) isReady() bool { return p.phase == phaseRunning && p.ready }

func (p *pod) key() string { return objKey(p.namespace, p.name) }

// podTemplate is what the pods of one workload share.
type podTemplate struct {
	labels         map[string]string
	containers     []container
	rendered       []any // spec.containers
	restartPolicy  string
	serviceAccount string
	priorityClass  string
	priority       int64
	hostNetwork    bool
	tolerations    []any
	qos            string
}

type container struct {
	name     string
	image    string
	imageID  string
	ports    []containerPort
	requests map[string]string
	limits   map[string]string
}

type containerPort struct {
	name     string
	port     int64
	protocol string
}

type event struct {
	name      string
	namespace string
	uid       string
	involved  map[string]any
	reason    string
	message   string
	typ       string
	component string
	host      string
	first     time.Time
	last      time.Time
	count     int64
}

// Random helpers. All randomness comes from c.rng, called with c.mu held, so the cluster is
// deterministic for a seed.

// safeAlphabet is the alphabet Kubernetes uses for generated name suffixes.
const safeAlphabet = "bcdfghjklmnpqrstvwxz2456789"

func (c *Cluster) suffix(n int) string {
	var b strings.Builder
	b.Grow(n)
	for range n {
		b.WriteByte(safeAlphabet[c.rng.IntN(len(safeAlphabet))])
	}
	return b.String()
}

func (c *Cluster) newUID() string {
	var b [16]byte
	for i := range b {
		b[i] = byte(c.rng.Uint32())
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (c *Cluster) hexString(n int) string {
	var b strings.Builder
	b.Grow(n)
	for range n {
		b.WriteByte("0123456789abcdef"[c.rng.IntN(16)])
	}
	return b.String()
}

func (c *Cluster) chance(p float64) bool { return c.rng.Float64() < p }

func pick[T any](c *Cluster, list []T) T { return list[c.rng.IntN(len(list))] }

// between returns a random time in [from, to).
func (c *Cluster) between(from, to time.Time) time.Time {
	d := to.Sub(from)
	if d <= 0 {
		return from
	}
	return from.Add(time.Duration(c.rng.Int64N(int64(d))))
}

// digest returns a stable sha256 hex digest of the parts, for image and container ids.
func digest(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

// ts formats a timestamp as the API server does.
func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// strMap converts labels to the JSON-compatible form unstructured objects need.
func strMap(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
