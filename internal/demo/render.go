//nolint:goconst // Object builders: Kubernetes field names and values read best as literals, as in a manifest.
package demo

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Condition and phase strings, as the API server writes them.
const (
	condTrue      = "True"
	condFalse     = "False"
	phaseNameRun  = "Running"
	phaseNamePend = "Pending"
	typeNormal    = "Normal"
	typeWarning   = "Warning"
	appLabel      = "app"
	mainContainer = 0
)

func object(apiVersion, kind string, meta map[string]any, fields map[string]any) *unstructured.Unstructured {
	obj := map[string]any{"apiVersion": apiVersion, "kind": kind, "metadata": meta}
	for k, v := range fields {
		obj[k] = v
	}
	return &unstructured.Unstructured{Object: obj}
}

// reference is an object reference: an owner reference or an event's involvedObject.
func reference(apiVersion, kind, namespace, name, uid string) map[string]any {
	ref := map[string]any{"apiVersion": apiVersion, "kind": kind, "name": name, "uid": uid}
	if namespace != "" {
		ref["namespace"] = namespace
	}
	return ref
}

func condition(typ, status string, since time.Time, reason, message string) map[string]any {
	c := map[string]any{"type": typ, "status": status, "lastTransitionTime": ts(since)}
	if reason != "" {
		c["reason"] = reason
	}
	if message != "" {
		c["message"] = message
	}
	return c
}

// podCondition is a condition with the lastProbeTime pods carry.
func podCondition(typ, status string, since time.Time, reason, message string) map[string]any {
	c := condition(typ, status, since, reason, message)
	c["lastProbeTime"] = nil
	return c
}

func boolStatus(b bool) string {
	if b {
		return condTrue
	}
	return condFalse
}

// Namespaces.

func (c *Cluster) renderNamespace(ns *namespace) *unstructured.Unstructured {
	meta := map[string]any{"name": ns.name, "uid": ns.uid, "creationTimestamp": ts(ns.created), "labels": ns.labels}
	phase := "Active"
	if ns.terminating {
		phase = "Terminating"
		meta["deletionTimestamp"] = ts(c.opts.Now.Add(-90 * time.Minute))
	}
	return object("v1", "Namespace", meta, map[string]any{
		"spec":   map[string]any{"finalizers": []any{"kubernetes"}},
		"status": map[string]any{"phase": phase},
	})
}

// Nodes.

// renderNodeStatic renders the parts of a node that never change.
func (c *Cluster) renderNodeStatic(n *node) {
	n.labels = map[string]any{
		"beta.kubernetes.io/arch":           n.arch,
		"beta.kubernetes.io/os":             "linux",
		"kubernetes.io/arch":                n.arch,
		"kubernetes.io/hostname":            n.name,
		"kubernetes.io/os":                  "linux",
		"node.kubernetes.io/instance-type":  n.size,
		"topology.kubernetes.io/region":     "demo-1",
		"topology.kubernetes.io/zone":       n.zone,
		"node-role.kubernetes.io/" + n.role: "",
	}
	n.annotations = map[string]any{
		"node.alpha.kubernetes.io/ttl":                           "0",
		"volumes.kubernetes.io/controller-managed-attach-detach": "true",
	}
	var taints []any
	if n.role == "control-plane" {
		taints = append(taints, map[string]any{"key": "node-role.kubernetes.io/control-plane", "effect": "NoSchedule"})
	}
	if n.role == "gpu" {
		taints = append(taints, map[string]any{"key": "nvidia.com/gpu", "value": "present", "effect": "NoSchedule"})
	}
	if n.cordoned {
		taints = append(taints, map[string]any{"key": "node.kubernetes.io/unschedulable", "effect": "NoSchedule", "timeAdded": ts(c.opts.Now.Add(-3 * time.Hour))})
	}
	if n.notReady {
		since := ts(n.readySince)
		taints = append(taints,
			map[string]any{"key": "node.kubernetes.io/memory-pressure", "effect": "NoSchedule", "timeAdded": since},
			map[string]any{"key": "node.kubernetes.io/not-ready", "effect": "NoSchedule", "timeAdded": since},
			map[string]any{"key": "node.kubernetes.io/not-ready", "effect": "NoExecute", "timeAdded": since},
		)
	}
	n.spec = map[string]any{
		"podCIDR":    n.podCIDR,
		"podCIDRs":   []any{n.podCIDR},
		"providerID": "demo://" + n.zone + "/" + n.name,
	}
	if taints != nil {
		n.spec["taints"] = taints
	}
	if n.cordoned {
		n.spec["unschedulable"] = true
	}

	memKi := n.memGi*1024*1024 - 220_000 // what the kernel keeps
	capacity := map[string]any{
		"cpu":               strconv.FormatInt(n.cpu, 10),
		"memory":            strconv.FormatInt(memKi, 10) + "Ki",
		resourcePods:        "250",
		"ephemeral-storage": "203056560Ki",
		"hugepages-1Gi":     "0",
		"hugepages-2Mi":     "0",
	}
	allocatable := map[string]any{
		"cpu":               strconv.FormatInt(n.cpu*1000-110, 10) + "m",
		"memory":            strconv.FormatInt(memKi-1_500_000, 10) + "Ki",
		resourcePods:        "250",
		"ephemeral-storage": "187136401028",
		"hugepages-1Gi":     "0",
		"hugepages-2Mi":     "0",
	}
	if n.gpu > 0 {
		capacity["nvidia.com/gpu"] = strconv.FormatInt(n.gpu, 10)
		allocatable["nvidia.com/gpu"] = strconv.FormatInt(n.gpu, 10)
	}
	kernel, osImage := "6.12.48-cloud-amd64", "Debian GNU/Linux 13 (trixie)"
	if n.arch == "arm64" {
		kernel = "6.12.48-cloud-arm64"
	}
	n.static = map[string]any{
		"capacity":    capacity,
		"allocatable": allocatable,
		"addresses": []any{
			map[string]any{"type": "InternalIP", "address": n.ip},
			map[string]any{"type": "Hostname", "address": n.name},
		},
		"daemonEndpoints": map[string]any{"kubeletEndpoint": map[string]any{"Port": int64(10250)}},
		"nodeInfo": map[string]any{
			"machineID":               digest("machine", n.uid)[:32],
			"systemUUID":              n.uid,
			"bootID":                  digest("boot", n.uid)[:32],
			"kernelVersion":           kernel,
			"osImage":                 osImage,
			"containerRuntimeVersion": "containerd://2.1.4",
			"kubeletVersion":          n.kubelet,
			"kubeProxyVersion":        "",
			"operatingSystem":         "linux",
			"architecture":            n.arch,
		},
	}
}

func (c *Cluster) renderNode(n *node) *unstructured.Unstructured {
	meta := map[string]any{
		"name": n.name, "uid": n.uid, "creationTimestamp": ts(n.created),
		"labels": n.labels, "annotations": n.annotations,
	}
	hb := ts(n.heartbeat)
	pressure := func(typ, reason, message string) map[string]any {
		cond := condition(typ, condFalse, n.created, reason, message)
		cond["lastHeartbeatTime"] = hb
		return cond
	}
	memory := pressure("MemoryPressure", "KubeletHasSufficientMemory", "kubelet has sufficient memory available")
	ready := condition("Ready", condTrue, n.readySince, "KubeletReady", "kubelet is posting ready status")
	if n.notReady {
		memory = condition("MemoryPressure", condTrue, n.readySince, "KubeletHasInsufficientMemory", "kubelet has insufficient memory available")
		ready = condition("Ready", condFalse, n.readySince, "KubeletNotReady", "PLEG is not healthy: pleg was last seen active 3m12s ago; threshold is 3m0s")
		memory["lastHeartbeatTime"] = hb
	}
	ready["lastHeartbeatTime"] = hb
	status := map[string]any{
		"conditions": []any{
			memory,
			pressure("DiskPressure", "KubeletHasNoDiskPressure", "kubelet has no disk pressure"),
			pressure("PIDPressure", "KubeletHasSufficientPID", "kubelet has sufficient PID available"),
			ready,
		},
	}
	for k, v := range n.static {
		status[k] = v
	}
	return object("v1", "Node", meta, map[string]any{"spec": n.spec, "status": status})
}

// Pod templates.

// renderTemplate renders a template's containers once and derives its QoS class.
func renderTemplate(t *podTemplate) {
	t.rendered = make([]any, 0, len(t.containers))
	for i := range t.containers {
		ct := &t.containers[i]
		ct.imageID = imageID(ct.image)
		m := map[string]any{
			"name":                     ct.name,
			"image":                    ct.image,
			"imagePullPolicy":          "IfNotPresent",
			"terminationMessagePath":   "/dev/termination-log",
			"terminationMessagePolicy": "File",
		}
		if len(ct.ports) > 0 {
			ports := make([]any, 0, len(ct.ports))
			for _, p := range ct.ports {
				ports = append(ports, map[string]any{"name": p.name, "containerPort": p.port, "protocol": p.protocol})
			}
			m["ports"] = ports
		}
		res := map[string]any{}
		if len(ct.requests) > 0 {
			res["requests"] = strMap(ct.requests)
		}
		if len(ct.limits) > 0 {
			res["limits"] = strMap(ct.limits)
		}
		m["resources"] = res
		t.rendered = append(t.rendered, m)
	}
	t.qos = qosClass(t.containers)
}

// qosClass applies the Kubernetes rules: Guaranteed when every container's CPU and memory limits
// equal its requests, BestEffort when no container sets any, Burstable otherwise.
func qosClass(cs []container) string {
	guaranteed, none := true, true
	for _, ct := range cs {
		if len(ct.requests) > 0 || len(ct.limits) > 0 {
			none = false
		}
		for _, r := range []string{"cpu", "memory"} {
			l, ok := ct.limits[r]
			if !ok {
				guaranteed = false
				continue
			}
			if q, set := ct.requests[r]; set && q != l {
				guaranteed = false
			}
		}
	}
	switch {
	case none:
		return "BestEffort"
	case guaranteed:
		return "Guaranteed"
	default:
		return "Burstable"
	}
}

// imageID is the digest reference the kubelet reports for an image.
func imageID(image string) string {
	name := image
	if i := strings.LastIndex(name, ":"); i > strings.LastIndex(name, "/") {
		name = name[:i]
	}
	if !strings.Contains(strings.SplitN(name, "/", 2)[0], ".") {
		if !strings.Contains(name, "/") {
			name = "library/" + name
		}
		name = "docker.io/" + name
	}
	return name + "@sha256:" + digest("image", image)
}

// Pods.

var defaultTolerations = []any{
	map[string]any{"key": "node.kubernetes.io/not-ready", "operator": "Exists", "effect": "NoExecute", "tolerationSeconds": int64(300)},
	map[string]any{"key": "node.kubernetes.io/unreachable", "operator": "Exists", "effect": "NoExecute", "tolerationSeconds": int64(300)},
}

// renderPodSpec renders a pod's spec once, when the pod is created; it never changes.
func renderPodSpec(p *pod) map[string]any {
	t := p.tmpl
	spec := map[string]any{
		"containers":                    t.rendered,
		"restartPolicy":                 t.restartPolicy,
		"terminationGracePeriodSeconds": int64(30),
		"dnsPolicy":                     "ClusterFirst",
		"serviceAccountName":            t.serviceAccount,
		"serviceAccount":                t.serviceAccount,
		"schedulerName":                 "default-scheduler",
		"securityContext":               map[string]any{},
		"enableServiceLinks":            true,
		"priority":                      t.priority,
		"preemptionPolicy":              "PreemptLowerPriority",
		"tolerations":                   t.tolerations,
	}
	if t.priorityClass != "" {
		spec["priorityClassName"] = t.priorityClass
	}
	if t.hostNetwork {
		spec["hostNetwork"] = true
		spec["dnsPolicy"] = "ClusterFirstWithHostNet"
	}
	if p.node != nil {
		spec["nodeName"] = p.node.name
	}
	return spec
}

func (c *Cluster) renderPod(p *pod) *unstructured.Unstructured {
	meta := map[string]any{
		"name": p.name, "namespace": p.namespace, "uid": p.uid,
		"creationTimestamp": ts(p.created), "labels": p.labels,
	}
	if p.owners != nil {
		meta["ownerReferences"] = p.owners
	}
	if p.deploy != nil {
		meta["generateName"] = p.deploy.name + "-" + p.deploy.rsHash[p.rs] + "-"
	}
	if !p.deleting.IsZero() {
		meta["deletionTimestamp"] = ts(p.deleting.Add(30 * time.Second))
		meta["deletionGracePeriodSeconds"] = int64(30)
	}
	return object("v1", "Pod", meta, map[string]any{"spec": p.spec, "status": c.podStatus(p)})
}

func (c *Cluster) podStatus(p *pod) map[string]any {
	st := map[string]any{"qosClass": p.tmpl.qos}
	if p.phase == phaseUnschedulable {
		st["phase"] = phaseNamePend
		st["conditions"] = []any{podCondition("PodScheduled", condFalse, p.created, "Unschedulable", c.unschedulableMessage())}
		return st
	}
	if p.node != nil {
		st["hostIP"] = p.node.ip
		st["hostIPs"] = []any{map[string]any{"ip": p.node.ip}}
	}
	if p.ip != "" && p.phase != phaseEvicted {
		st["podIP"] = p.ip
		st["podIPs"] = []any{map[string]any{"ip": p.ip}}
	}
	if !p.started.IsZero() {
		st["startTime"] = ts(p.started)
	}

	sandbox, scheduled := condTrue, p.created
	readyReason, readyMsg := "", ""
	switch p.phase {
	case phaseRunning, phaseCrashLoop:
		st["phase"] = phaseNameRun
	case phaseCreating, phaseImagePull:
		st["phase"] = phaseNamePend
		if p.phase == phaseCreating {
			sandbox = condFalse
		}
	case phaseSucceeded:
		st["phase"] = "Succeeded"
		sandbox, readyReason = condFalse, "PodCompleted"
	case phaseFailed:
		st["phase"] = "Failed"
		sandbox, readyReason = condFalse, "PodFailed"
	case phaseEvicted:
		st["phase"] = "Failed"
		st["reason"] = "Evicted"
		st["message"] = "The node was low on resource: memory. Threshold quantity: 100Mi, available: 87232Ki. Container " +
			p.tmpl.containers[mainContainer].name + " was using 1452300Ki, request is 256Mi, has larger consumption of memory. "
		sandbox, readyReason = condFalse, "PodFailed"
	}
	ready := p.isReady()
	if !ready && readyReason == "" {
		readyReason = "ContainersNotReady"
		readyMsg = "containers with unready status: [" + p.tmpl.containers[mainContainer].name + "]"
	}
	readySince := p.readySince
	if readySince.IsZero() {
		readySince = p.created
	}
	st["conditions"] = []any{
		podCondition("PodReadyToStartContainers", sandbox, p.created, "", ""),
		podCondition("Initialized", condTrue, p.created, "", ""),
		podCondition("Ready", boolStatus(ready), readySince, readyReason, readyMsg),
		podCondition("ContainersReady", boolStatus(ready), readySince, readyReason, readyMsg),
		podCondition("PodScheduled", condTrue, scheduled, "", ""),
	}
	statuses := make([]any, 0, len(p.tmpl.containers))
	for i := range p.tmpl.containers {
		statuses = append(statuses, c.containerStatus(p, i))
	}
	st["containerStatuses"] = statuses
	return st
}

func (c *Cluster) unschedulableMessage() string {
	n := len(c.w.nodes)
	cp := n - len(c.w.workers)
	return fmt.Sprintf("0/%d nodes are available: %d Insufficient cpu, %d node(s) had untolerated taint(s). "+
		"preemption: 0/%d nodes are available: %d No preemption victims found for incoming pod, %d Preemption is not helpful for scheduling.",
		n, n-cp, cp, n, n-cp, cp)
}

func (c *Cluster) containerStatus(p *pod, i int) map[string]any {
	ct := &p.tmpl.containers[i]
	cs := map[string]any{
		"name":         ct.name,
		"image":        ct.image,
		"imageID":      ct.imageID,
		"restartCount": int64(0),
		"ready":        false,
		"started":      false,
		"lastState":    map[string]any{},
	}
	running := func(since time.Time) {
		cs["state"] = map[string]any{"running": map[string]any{"startedAt": ts(since)}}
		cs["started"] = true
		cs["containerID"] = "containerd://" + digest("container", p.uid, ct.name, strconv.FormatInt(p.restarts, 10))
	}
	terminated := func(reason string, code int64, started, finished time.Time) map[string]any {
		return map[string]any{"terminated": map[string]any{
			"reason": reason, "exitCode": code, "startedAt": ts(started), "finishedAt": ts(finished),
			"containerID": "containerd://" + digest("container", p.uid, ct.name, "exited"),
		}}
	}
	waiting := func(reason, message string) {
		w := map[string]any{"reason": reason}
		if message != "" {
			w["message"] = message
		}
		cs["state"] = map[string]any{"waiting": w}
	}
	main := i == mainContainer
	switch {
	case p.phase == phaseCreating:
		waiting("ContainerCreating", "")
		cs["imageID"] = ""
	case p.phase == phaseImagePull && main:
		cs["imageID"] = ""
		if p.pullBackOff {
			waiting("ImagePullBackOff", "Back-off pulling image \""+ct.image+"\": ErrImagePull: "+pullError(ct.image))
		} else {
			waiting("ErrImagePull", pullError(ct.image))
		}
	case p.phase == phaseCrashLoop && main:
		cs["restartCount"] = p.restarts
		cs["lastState"] = terminated("Error", 1, p.lastStart, p.lastExit)
		if p.inBackOff {
			waiting("CrashLoopBackOff", fmt.Sprintf("back-off 5m0s restarting failed container=%s pod=%s_%s(%s)", ct.name, p.name, p.namespace, p.uid))
			cs["containerID"] = "containerd://" + digest("container", p.uid, ct.name, "exited")
		} else {
			running(p.lastStart)
		}
	case p.phase == phaseSucceeded:
		cs["state"] = terminated("Completed", 0, p.started, p.lastExit)
		cs["containerID"] = "containerd://" + digest("container", p.uid, ct.name, "exited")
	case p.phase == phaseFailed:
		reason, code := "Completed", int64(0)
		if main {
			reason, code = "Error", 1
		}
		cs["state"] = terminated(reason, code, p.started, p.lastExit)
		cs["containerID"] = "containerd://" + digest("container", p.uid, ct.name, "exited")
	case p.phase == phaseEvicted:
		cs["state"] = terminated("Error", 137, p.started, p.lastExit)
		cs["containerID"] = "containerd://" + digest("container", p.uid, ct.name, "exited")
	default: // running, and the healthy containers of crash-looping and image-pull pods
		running(p.started)
		cs["ready"] = !main || p.isReady()
		if main {
			cs["restartCount"] = p.restarts
			if p.restarts > 0 {
				cs["lastState"] = terminated("Error", 137, p.started.Add(-time.Hour), p.started)
			}
		}
	}
	return cs
}

func pullError(image string) string {
	return "rpc error: code = NotFound desc = failed to pull and unpack image \"" + image + "\": " + image + ": not found"
}

// Deployments.

// renderDeploymentSpec renders a deployment's metadata labels and spec once.
func (c *Cluster) renderDeploymentSpec(d *deployment) {
	t := d.templates[d.current]
	d.labels = deploymentLabels(d.namespace, d.name) // labels.go
	d.spec = map[string]any{
		"replicas": int64(d.replicas),
		"selector": map[string]any{"matchLabels": strMap(d.selector)},
		"template": map[string]any{
			"metadata": map[string]any{"labels": strMap(t.labels)},
			"spec": map[string]any{
				"containers":                    t.rendered,
				"restartPolicy":                 "Always",
				"terminationGracePeriodSeconds": int64(30),
				"dnsPolicy":                     "ClusterFirst",
				"schedulerName":                 "default-scheduler",
				"serviceAccountName":            t.serviceAccount,
				"securityContext":               map[string]any{},
			},
		},
		"strategy": map[string]any{
			"type":          "RollingUpdate",
			"rollingUpdate": map[string]any{"maxSurge": "25%", "maxUnavailable": "25%"},
		},
		"revisionHistoryLimit":    int64(10),
		"progressDeadlineSeconds": int64(600),
	}
}

// refresh recomputes the conditions from the pods; a changed status moves its
// transition time to now.
func (d *deployment) refresh(now time.Time) {
	_, ready, _ := d.counts()
	available := ready >= d.replicas-d.replicas/4
	progressing := d.profile != profileBadImage
	if s := boolStatus(available); s != d.available {
		d.available, d.availableSince = s, now
	}
	if s := boolStatus(progressing); s != d.progressing {
		d.progressing, d.progressingSince = s, now
	}
}

func (c *Cluster) renderDeployment(d *deployment) *unstructured.Unstructured {
	meta := map[string]any{
		"name": d.name, "namespace": d.namespace, "uid": d.uid,
		"creationTimestamp": ts(d.created), "generation": d.generation, "labels": d.labels,
		"annotations": map[string]any{"deployment.kubernetes.io/revision": strconv.Itoa(d.current + 1)},
	}
	active, ready, updated := d.counts()
	st := map[string]any{"observedGeneration": d.generation}
	if active > 0 {
		st["replicas"] = int64(active)
	}
	if updated > 0 {
		st["updatedReplicas"] = int64(updated)
	}
	if ready > 0 {
		st["readyReplicas"] = int64(ready)
		st["availableReplicas"] = int64(ready)
	}
	if u := max(d.replicas, active) - ready; u > 0 {
		st["unavailableReplicas"] = int64(u)
	}
	rs := d.name + "-" + d.rsHash[d.current]
	available := condition("Available", d.available, d.availableSince, "MinimumReplicasAvailable", "Deployment has minimum availability.")
	if d.available != condTrue {
		available = condition("Available", d.available, d.availableSince, "MinimumReplicasUnavailable", "Deployment does not have minimum availability.")
	}
	var progressing map[string]any
	switch {
	case d.progressing != condTrue:
		progressing = condition("Progressing", condFalse, d.progressingSince, "ProgressDeadlineExceeded", "ReplicaSet \""+rs+"\" has timed out progressing.")
	case updated < d.replicas || ready < d.replicas:
		progressing = condition("Progressing", condTrue, d.progressingSince, "ReplicaSetUpdated", "ReplicaSet \""+rs+"\" is progressing.")
	default:
		progressing = condition("Progressing", condTrue, d.progressingSince, "NewReplicaSetAvailable", "ReplicaSet \""+rs+"\" has successfully progressed.")
	}
	for _, cond := range []map[string]any{available, progressing} {
		cond["lastUpdateTime"] = cond["lastTransitionTime"]
	}
	st["conditions"] = []any{available, progressing}
	return object("apps/v1", "Deployment", meta, map[string]any{"spec": d.spec, "status": st})
}

// Events.

func (c *Cluster) renderEvent(e *event) *unstructured.Unstructured {
	meta := map[string]any{
		"name": e.name, "namespace": e.namespace, "uid": e.uid, "creationTimestamp": ts(e.first),
	}
	source := map[string]any{"component": e.component}
	if e.host != "" {
		source["host"] = e.host
	}
	return object("v1", "Event", meta, map[string]any{
		"involvedObject":     e.involved,
		"reason":             e.reason,
		"message":            e.message,
		"source":             source,
		"firstTimestamp":     ts(e.first),
		"lastTimestamp":      ts(e.last),
		"count":              e.count,
		"type":               e.typ,
		"eventTime":          nil,
		"reportingComponent": e.component,
		"reportingInstance":  e.host,
	})
}
