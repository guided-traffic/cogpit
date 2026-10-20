package engine

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The cluster Overview's Issues and totals from the cache (no API request of their own). Lens's
// documentation says only that the section "displays system messages and warnings"; which objects
// count is Cogpit's rule (inferred), each the rule of the view that already shows it:
//
//   - a pod with the Pods view's warning icon (an error status such as CrashLoopBackOff, containers
//     not ready while running, not scheduled), its reason as the message;
//   - a node with the Nodes view's warning icon (not Ready, Ready Unknown, a pressure condition);
//   - a Deployment, Stateful Set or Daemon Set the views colour as a warning or an error: short of
//     ready pods with no rollout under way to fix it;
//   - a warning event of the last hour, about its involved object.
//
// The totals behind the charts' lines are the nodes' allocatable and capacity, and the requests and
// limits of the pods that have not finished on those nodes; the Pods chart counts those pods.

// IssueWindow is how far back a warning event counts as an issue: the last hour.
const IssueWindow = time.Hour

// The condition type and status the rules read.
const (
	condReady   = "Ready"
	condTrue    = "True"
	condFalse   = "False"
	condUnknown = "Unknown"
)

// overviewEntry is what one object adds to the page: its issue, and for a node or a pod its share
// of the totals. Entries are rebuilt only when the object's resourceVersion changed.
type overviewEntry struct {
	rv    string
	gen   uint32
	issue *ClusterIssue

	// A node's; addrs are its addresses besides its name (nodeAddresses).
	master              bool
	alloc, capa         model.Usage
	allocPods, capaPods int64
	addrs               []string

	// A pod's: the node it runs on, whether it still takes room there (not finished), and the sum
	// of its containers' requests and of the limits they set. A node's: its own name.
	node     string
	active   bool
	req, lim model.Usage
	// A node's or a pod's own state, as its view's row shows it: the Overview's health header counts
	// them (ClusterOverviewModel.NodeStates, PodStates).
	state model.Status

	// An event's last time: its issue leaves the Issues an hour later.
	last time.Time
}

// overviewBuild is one state of the page: its issues (all namespaces), the node groups' totals and
// the control-plane nodes. Immutable once built.
type overviewBuild struct {
	at      time.Time
	issues  []ClusterIssue
	groups  map[string]groupTotals
	masters []string // the control-plane nodes, sorted
	// nodeStates and podStates count the cluster's nodes and pods by state, in all namespaces.
	nodeStates, podStates map[model.Status]int
	// masterAddrs are the control-plane nodes' addresses, by node: the cluster's Prometheus history
	// finds the masters' series by them too (Hub.SetNodeAddresses, A43).
	masterAddrs map[string][]string
	synced      bool
	// nodesSynced: the nodes are listed (or cannot be read), so masters names the whole control plane
	// as far as Cogpit can know it.
	nodesSynced bool
	errText     string
	// expires is when the next event's issue leaves the last hour; zero when none will.
	expires time.Time
}

// groupTotals are one node group's numbers from the cache.
type groupTotals struct {
	nodes                                   int
	requests, limits, allocatable, capacity model.Usage
	allocPods, capaPods                     int64
	pods                                    int64 // the pods on the group's nodes that have not finished
}

// each visits the kind's objects, building the entry of an object that is new or changed with
// newEntry, and drops the entries of objects that are gone.
func (k *overviewKind) each(context string, newEntry func(p *model.KindProfile, context string, obj *unstructured.Unstructured) *overviewEntry, visit func(*overviewEntry)) {
	if k == nil {
		return
	}
	k.gen++
	objs := KindList(k.handle, k.profile)
	for _, obj := range objs {
		key := string(obj.GetUID())
		if key == "" {
			key = obj.GetNamespace() + "/" + obj.GetName()
		}
		rv := obj.GetResourceVersion()
		e := k.entries[key]
		if e == nil || e.rv != rv || rv == "" {
			e = safeEntry(k.profile, context, obj, newEntry)
			e.rv = rv
			k.entries[key] = e
		}
		e.gen = k.gen
		visit(e)
	}
	if len(k.entries) > len(objs) {
		for key, e := range k.entries {
			if e.gen != k.gen {
				delete(k.entries, key)
			}
		}
	}
}

// safeEntry builds an entry, an empty one when the object's content makes a rule panic.
func safeEntry(p *model.KindProfile, context string, obj *unstructured.Unstructured, newEntry func(*model.KindProfile, string, *unstructured.Unstructured) *overviewEntry) (e *overviewEntry) {
	defer func() {
		if recover() != nil {
			e = &overviewEntry{}
		}
	}()
	return newEntry(p, context, obj)
}

// build reads the page's state from the cache as of now.
func (src *overviewSource) build(now time.Time) *overviewBuild {
	src.mu.Lock()
	defer src.mu.Unlock()
	ob := &overviewBuild{at: now, synced: src.synced(), errText: src.errText(), // before the lists: never claims more
		nodesSynced: src.nodes == nil || src.nodes.handle.HasSynced() || src.nodes.handle.Err() != nil,
		groups:      map[string]groupTotals{NodesMaster: {}, NodesWorker: {}},
		nodeStates:  map[model.Status]int{}, podStates: map[model.Status]int{}}
	context := src.b.Context

	nodeGroup := map[string]string{}
	src.nodes.each(context, nodeEntry, func(e *overviewEntry) {
		if e.issue != nil {
			ob.issues = append(ob.issues, *e.issue)
		}
		name := e.node
		g := NodesWorker
		if e.master {
			g = NodesMaster
			ob.masters = append(ob.masters, name)
			if ob.masterAddrs == nil {
				ob.masterAddrs = map[string][]string{}
			}
			ob.masterAddrs[name] = e.addrs
		}
		nodeGroup[name] = g
		ob.nodeStates[e.state]++
		t := ob.groups[g]
		t.nodes++
		t.allocatable, t.capacity = t.allocatable.Add(e.alloc), t.capacity.Add(e.capa)
		t.allocPods += e.allocPods
		t.capaPods += e.capaPods
		ob.groups[g] = t
	})
	slices.Sort(ob.masters)

	src.pods.each(context, podEntry, func(e *overviewEntry) {
		if e.issue != nil {
			ob.issues = append(ob.issues, *e.issue)
		}
		ob.podStates[e.state]++
		g, ok := nodeGroup[e.node]
		if !e.active || !ok {
			return
		}
		t := ob.groups[g]
		t.pods++
		t.requests, t.limits = t.requests.Add(e.req), t.limits.Add(e.lim)
		ob.groups[g] = t
	})
	for _, k := range src.workloads {
		k.each(context, workloadEntry, func(e *overviewEntry) {
			if e.issue != nil {
				ob.issues = append(ob.issues, *e.issue)
			}
		})
	}
	src.events.each(context, eventEntry, func(e *overviewEntry) {
		if e.issue == nil {
			return
		}
		leaves := e.last.Add(IssueWindow)
		if !now.Before(leaves) {
			return
		}
		ob.issues = append(ob.issues, *e.issue)
		if ob.expires.IsZero() || leaves.Before(ob.expires) {
			ob.expires = leaves
		}
	})
	return ob
}

// nodeEntry is a node's issue (the Nodes view's warning icon), its group (a control-plane node is a
// master: the roles the Nodes view shows include control-plane or master) and its allocatable and
// capacity.
func nodeEntry(p *model.KindProfile, context string, obj *unstructured.Unstructured) *overviewEntry {
	row := model.BuildRow(p, context, obj)
	e := &overviewEntry{node: obj.GetName(), state: row.Status}
	if i := p.ColumnIndex("roles"); i >= 0 && i < len(row.Cells) {
		for _, r := range strings.Split(row.Cells[i].Text, ",") {
			if r = strings.TrimSpace(r); r == "control-plane" || r == "master" {
				e.master = true
			}
		}
	}
	e.alloc, e.capa = model.NodeCapacity(obj)
	e.addrs = nodeAddresses(e.node, obj)
	e.allocPods = quantityOf(obj, "status", "allocatable", "pods")
	e.capaPods = quantityOf(obj, "status", "capacity", "pods")
	if c, ok := warningOf(p, row); ok {
		sev := model.StatusWarning
		if !nodeReady(obj) {
			sev = model.StatusError
		}
		e.issue = &ClusterIssue{
			Ref: model.ObjectRef{Kind: p.Key, Name: obj.GetName()}, Name: obj.GetName(), Kind: p.Kind,
			Source: IssueStatus, Severity: sev, Message: c.Text, Since: nodeProblemSince(obj),
		}
	}
	return e
}

// podEntry is a pod's issue (the Pods view's warning icon) and what it takes of its node.
func podEntry(p *model.KindProfile, context string, obj *unstructured.Unstructured) *overviewEntry {
	row := model.BuildRow(p, context, obj)
	e := &overviewEntry{node: model.NestedString(obj.Object, "spec", "nodeName"), state: row.Status}
	switch model.NestedString(obj.Object, "status", "phase") {
	case "Succeeded", "Failed":
	default:
		e.active = e.node != ""
	}
	for _, c := range model.PodResources(obj) {
		e.req, e.lim = e.req.Add(c.Requests), e.lim.Add(c.Limits)
	}
	if c, ok := warningOf(p, row); ok {
		sev := model.StatusWarning
		if row.Status == model.StatusError {
			sev = model.StatusError
		}
		e.issue = &ClusterIssue{
			Ref: model.ObjectRef{Kind: p.Key, Namespace: obj.GetNamespace(), Name: obj.GetName()}, Name: obj.GetName(), Kind: p.Kind,
			Source: IssueStatus, Severity: sev, Message: c.Text, Since: podProblemSince(obj),
		}
	}
	return e
}

// workloadEntry is a workload's issue: its view colours it as a warning or an error, which for
// these kinds means short of ready pods with no rollout under way.
func workloadEntry(p *model.KindProfile, context string, obj *unstructured.Unstructured) *overviewEntry {
	row := model.BuildRow(p, context, obj)
	e := &overviewEntry{}
	if row.Status != model.StatusWarning && row.Status != model.StatusError {
		return e
	}
	desired, ready := workloadReadiness(p.Key, obj)
	msg := strconv.FormatInt(ready, 10) + " of " + strconv.FormatInt(desired, 10) + " pods ready"
	e.issue = &ClusterIssue{
		Ref: model.ObjectRef{Kind: p.Key, Namespace: obj.GetNamespace(), Name: obj.GetName()}, Name: obj.GetName(), Kind: p.Kind,
		Source: IssueStatus, Severity: row.Status, Message: msg, Since: conditionSince(obj, "Available", "False"),
	}
	return e
}

// workloadReadiness is a workload's desired and ready pods.
func workloadReadiness(kind string, obj *unstructured.Unstructured) (desired, ready int64) {
	if kind == logKindDaemonSets {
		desired, _ = model.NestedInt64(obj.Object, "status", "desiredNumberScheduled")
		ready, _ = model.NestedInt64(obj.Object, "status", "numberReady")
		return desired, ready
	}
	desired = 1 // the API's default for spec.replicas
	if n, ok := model.NestedInt64(obj.Object, "spec", "replicas"); ok {
		desired = n
	}
	ready, _ = model.NestedInt64(obj.Object, "status", "readyReplicas")
	return desired, ready
}

// eventEntry is a warning event's issue, about its involved object: the object's own row when
// Cogpit has a view for its kind, else the event's.
func eventEntry(p *model.KindProfile, context string, obj *unstructured.Unstructured) *overviewEntry {
	e := &overviewEntry{}
	if model.NestedString(obj.Object, "type") != "Warning" {
		return e
	}
	row := model.BuildRow(p, context, obj)
	cell := func(id string) model.Cell {
		if i := p.ColumnIndex(id); i >= 0 && i < len(row.Cells) {
			return row.Cells[i]
		}
		return model.Cell{}
	}
	if n := cell(lastSeenColumn).Num; n > 0 {
		e.last = time.Unix(n, 0)
	}
	involved := model.NestedMap(obj.Object, "involvedObject")
	if involved == nil {
		involved = model.NestedMap(obj.Object, "regarding")
	}
	kind, name := model.NestedString(involved, "kind"), model.NestedString(involved, "name")
	ref := model.ObjectRef{Kind: p.Key, Namespace: obj.GetNamespace(), Name: obj.GetName()}
	if link := cell("involvedObject").Link; link != nil {
		ref = *link
	}
	if kind == "" {
		kind = p.Kind
	}
	if name == "" {
		name = obj.GetName()
	}
	msg := cell("message").Text
	if reason := model.NestedString(obj.Object, "reason"); reason != "" {
		msg = reason + ": " + msg
	}
	e.issue = &ClusterIssue{
		Ref: ref, Name: name, Kind: kind, Source: IssueEvent, Severity: model.StatusWarning,
		Message: strings.TrimSuffix(msg, ": "), Since: e.last,
	}
	return e
}

// warningOf returns a row's warning cell when it shows a warning.
func warningOf(p *model.KindProfile, row model.Row) (model.Cell, bool) {
	i := p.ColumnIndex("warning")
	if i < 0 || i >= len(row.Cells) {
		return model.Cell{}, false
	}
	c := row.Cells[i]
	return c, c.Status != model.StatusNone && c.Status != ""
}

// quantityOf reads a count quantity ("110") at a path; 0 when unset or unreadable.
func quantityOf(obj *unstructured.Unstructured, fields ...string) int64 {
	if q, ok := model.ParseQuantity(model.NestedIntOrString(obj.Object, fields...)); ok {
		return q.Value()
	}
	return 0
}

// nodeReady reports whether a node's Ready condition is True.
func nodeReady(obj *unstructured.Unstructured) bool {
	for _, c := range model.NestedSlice(obj.Object, "status", "conditions") {
		if model.NestedString(c, "type") == condReady {
			return model.NestedString(c, "status") == condTrue
		}
	}
	return false
}

// nodeConditionProblem reports whether a node condition is in a problem state, as the Nodes
// profile reads it (kinds/nodes.go, conditionProblem): Ready not True; another condition whose
// type ends in Ready (SystemdUnitsReady) False or Unknown; any other condition True (the kubelet's
// pressures, NetworkUnavailable, what node-problem-detector adds).
func nodeConditionProblem(typ, status string) bool {
	switch {
	case typ == condReady:
		return status != condTrue
	case strings.HasSuffix(typ, condReady):
		return status == condFalse || status == condUnknown
	}
	return status == condTrue
}

// nodeProblemSince is when a node's problem began: the latest transition of a condition in a
// problem state (nodeConditionProblem); zero when the node does not say.
func nodeProblemSince(obj *unstructured.Unstructured) time.Time {
	var since time.Time
	for _, c := range model.NestedSlice(obj.Object, "status", "conditions") {
		typ, st := model.NestedString(c, "type"), model.NestedString(c, "status")
		if typ != "" && nodeConditionProblem(typ, st) {
			if t := model.NestedTime(c, "lastTransitionTime"); t.After(since) {
				since = t
			}
		}
	}
	return since
}

// podProblemSince is when a pod's problem began: when it stopped being Ready, else when it could
// not be scheduled, else when it was created.
func podProblemSince(obj *unstructured.Unstructured) time.Time {
	if t := conditionSince(obj, condReady, "False"); !t.IsZero() {
		return t
	}
	if t := conditionSince(obj, "PodScheduled", "False"); !t.IsZero() {
		return t
	}
	return obj.GetCreationTimestamp().Time
}

// conditionSince is the last transition of the condition typ when its status is status; zero
// otherwise.
func conditionSince(obj *unstructured.Unstructured, typ, status string) time.Time {
	for _, c := range model.NestedSlice(obj.Object, "status", "conditions") {
		if model.NestedString(c, "type") == typ && model.NestedString(c, "status") == status {
			return model.NestedTime(c, "lastTransitionTime")
		}
	}
	return time.Time{}
}
