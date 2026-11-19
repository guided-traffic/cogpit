package kinds

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The goldens cover the recorded objects; these tests pin the rules for states that are rare in
// recordings: signals, lost nodes, odd selectors, the events.k8s.io field names.

func pod(spec, status map[string]any, meta map[string]any) map[string]any {
	if meta == nil {
		meta = map[string]any{"name": "p"}
	}
	return map[string]any{"metadata": meta, "spec": spec, "status": status}
}

func running(ready bool) map[string]any {
	return map[string]any{"name": "app", "ready": ready, "state": map[string]any{"running": map[string]any{}}}
}

func TestSummarisePodEdgeCases(t *testing.T) {
	oneApp := map[string]any{"containers": []any{map[string]any{"name": "app"}}}
	oneInit := map[string]any{
		"initContainers": []any{map[string]any{"name": "init"}},
		"containers":     []any{map[string]any{"name": "app"}},
	}
	tests := []struct {
		name   string
		obj    map[string]any
		text   string
		status model.Status
	}{
		{
			name: "init killed by a signal",
			obj: pod(oneInit, map[string]any{"phase": "Pending", "initContainerStatuses": []any{
				map[string]any{"name": "init", "state": map[string]any{"terminated": map[string]any{"exitCode": int64(137), "signal": int64(9)}}},
			}}, nil),
			text: "Init:Signal:9", status: model.StatusError,
		},
		{
			name: "init failed without a reason",
			obj: pod(oneInit, map[string]any{"phase": "Pending", "initContainerStatuses": []any{
				map[string]any{"name": "init", "state": map[string]any{"terminated": map[string]any{"exitCode": int64(3)}}},
			}}, nil),
			text: "Init:ExitCode:3", status: model.StatusError,
		},
		{
			name: "init waiting before it started",
			obj: pod(oneInit, map[string]any{"phase": "Pending", "initContainerStatuses": []any{
				map[string]any{"name": "init", "state": map[string]any{"waiting": map[string]any{"reason": "PodInitializing"}}},
			}}, nil),
			text: "Init:0/1", status: model.StatusPending,
		},
		{
			name: "Initialized condition outranks stale init statuses",
			obj: pod(oneInit, map[string]any{
				"phase":      "Running",
				"conditions": []any{map[string]any{"type": "Initialized", "status": "True"}},
				"initContainerStatuses": []any{
					map[string]any{"name": "init", "state": map[string]any{"waiting": map[string]any{}}},
				},
				"containerStatuses": []any{running(true)},
			}, nil),
			text: "Running", status: model.StatusOK,
		},
		{
			name: "app container exited by signal",
			obj: pod(oneApp, map[string]any{"phase": "Running", "containerStatuses": []any{
				map[string]any{"name": "app", "state": map[string]any{"terminated": map[string]any{"exitCode": int64(143), "signal": int64(15)}}},
			}}, nil),
			text: "Signal:15", status: model.StatusError,
		},
		{
			name: "app container exited without a reason",
			obj: pod(oneApp, map[string]any{"phase": "Failed", "containerStatuses": []any{
				map[string]any{"name": "app", "state": map[string]any{"terminated": map[string]any{"exitCode": int64(2)}}},
			}}, nil),
			text: "ExitCode:2", status: model.StatusError,
		},
		{
			name: "completed next to running, pod ready",
			obj: pod(nil, map[string]any{
				"phase":      "Running",
				"conditions": []any{map[string]any{"type": "Ready", "status": "True"}},
				"containerStatuses": []any{
					map[string]any{"name": "job", "state": map[string]any{"terminated": map[string]any{"reason": "Completed"}}},
					running(true),
				},
			}, nil),
			text: "Running", status: model.StatusWarning, // 1 of 2 containers ready
		},
		{
			name: "completed next to running, pod not ready",
			obj: pod(nil, map[string]any{
				"phase": "Running",
				"containerStatuses": []any{
					map[string]any{"name": "job", "state": map[string]any{"terminated": map[string]any{"reason": "Completed"}}},
					running(true),
				},
			}, nil),
			text: "NotReady", status: model.StatusWarning,
		},
		{
			name: "node lost while deleting",
			obj: pod(oneApp, map[string]any{"phase": "Unknown", "reason": "NodeLost"},
				map[string]any{"name": "p", "deletionTimestamp": "2026-10-11T11:00:00Z"}),
			text: "Unknown", status: model.StatusUnknown,
		},
		{
			name: "unknown waiting reason in a pending pod",
			obj: pod(oneApp, map[string]any{"phase": "Pending", "containerStatuses": []any{
				map[string]any{"name": "app", "state": map[string]any{"waiting": map[string]any{"reason": "SomethingNew"}}},
			}}, nil),
			text: "SomethingNew", status: model.StatusPending,
		},
		{
			name: "sidecar not started holds initialisation",
			obj: pod(map[string]any{
				"initContainers": []any{map[string]any{"name": "proxy", "restartPolicy": "Always"}},
				"containers":     []any{map[string]any{"name": "app"}},
			}, map[string]any{"phase": "Pending", "initContainerStatuses": []any{
				map[string]any{"name": "proxy", "started": false, "state": map[string]any{"running": map[string]any{}}},
			}}, nil),
			text: "Init:0/1", status: model.StatusPending,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := summarisePod(tt.obj)
			if s.text != tt.text || s.status != tt.status {
				t.Errorf("got %q %q, want %q %q", s.text, s.status, tt.text, tt.status)
			}
		})
	}
}

func TestPodRestartsAndCounts(t *testing.T) {
	obj := pod(map[string]any{
		"initContainers": []any{
			map[string]any{"name": "migrate"},
			map[string]any{"name": "proxy", "restartPolicy": "Always"},
		},
		"containers": []any{map[string]any{"name": "app"}},
	}, map[string]any{
		"phase": "Running",
		"initContainerStatuses": []any{
			map[string]any{"name": "migrate", "restartCount": int64(5), "state": map[string]any{"terminated": map[string]any{"exitCode": int64(0)}}},
			map[string]any{"name": "proxy", "restartCount": int64(2), "started": true, "ready": true},
		},
		"containerStatuses": []any{
			map[string]any{"name": "app", "restartCount": int64(-4), "ready": true, "state": map[string]any{"running": map[string]any{}}},
		},
	}, nil)
	s := summarisePod(obj)
	// Initialised: the finished init container's restarts no longer count; the sidecar's do; a
	// negative count is ignored.
	if s.restarts != 2 || s.ready != 2 || s.total != 2 || s.text != "Running" {
		t.Errorf("got %+v", s)
	}
}

func TestPodToken(t *testing.T) {
	tests := []struct {
		text, phase string
		ready, all  int64
		want        model.Status
	}{
		{"Running", "Running", 0, 0, model.StatusOK},
		{"Running", "Running", 1, 2, model.StatusWarning},
		{"Init:2/3", "Pending", 0, 1, model.StatusPending},
		{"Init:2/x", "Pending", 0, 1, model.StatusError},
		{"Init:", "Pending", 0, 1, model.StatusError},
		{"Succeeded", "Succeeded", 0, 1, model.StatusSucceeded},
		{"Shutdown", "Failed", 0, 1, model.StatusError},
		{"Mystery", "Running", 0, 1, model.StatusWarning},
		{"Mystery", "Succeeded", 0, 1, model.StatusSucceeded},
		{"Mystery", "", 0, 1, model.StatusUnknown},
		{"NodeLost", "Unknown", 0, 1, model.StatusUnknown},
	}
	for _, tt := range tests {
		if got := podToken(tt.text, tt.phase, tt.ready, tt.all); got != tt.want {
			t.Errorf("podToken(%q, %q, %d, %d) = %q, want %q", tt.text, tt.phase, tt.ready, tt.all, got, tt.want)
		}
	}
}

func TestController(t *testing.T) {
	obj := map[string]any{"metadata": map[string]any{"ownerReferences": []any{
		"garbage",
		map[string]any{"kind": "ConfigMap", "name": "first"},
		map[string]any{"kind": "Node", "name": "worker-1", "controller": true},
	}}}
	if kind, name := controller(obj); kind != "Node" || name != "worker-1" {
		t.Errorf("controller = %s/%s", kind, name)
	}
	delete(obj["metadata"].(map[string]any)["ownerReferences"].([]any)[2].(map[string]any), "controller")
	if kind, name := controller(obj); kind != "ConfigMap" || name != "first" {
		t.Errorf("first owner = %s/%s", kind, name)
	}
	if kind, name := controller(map[string]any{}); kind != "" || name != "" {
		t.Errorf("no owners = %s/%s", kind, name)
	}
}

func TestShellJoin(t *testing.T) {
	got := shellJoin([]string{"sh", "-c", "echo 'hi there'", ""})
	want := `sh -c "echo 'hi there'" ""`
	if got != want {
		t.Errorf("shellJoin = %s, want %s", got, want)
	}
	if shellJoin(nil) != "" {
		t.Error("empty argv should render empty")
	}
}

func TestLabelSelector(t *testing.T) {
	obj := map[string]any{"spec": map[string]any{"selector": map[string]any{
		"matchLabels": map[string]any{"b": "2", "a": "1", "n": int64(3)},
		"matchExpressions": []any{
			map[string]any{"key": "env", "operator": "NotIn", "values": []any{"dev", int64(1)}},
			map[string]any{"key": "gpu", "operator": "Exists"},
			map[string]any{"operator": "In"},
			map[string]any{"key": "zone", "operator": "Gt", "values": []any{"2"}},
			"garbage",
		},
	}}}
	got := labelSelector(obj, "spec", "selector")
	want := []string{"a=1", "b=2", "env notin (dev)", "gpu", "zone Gt (2)"}
	if len(got) != len(want) {
		t.Fatalf("labelSelector = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("labelSelector[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if labelSelector(map[string]any{"spec": map[string]any{"selector": "app=x"}}, "spec", "selector") != nil {
		t.Error("a selector that is not an object should yield nothing")
	}
}

func TestDeploymentStatusEdges(t *testing.T) {
	obj := map[string]any{"spec": map[string]any{"replicas": int64(-2)}}
	if got := desiredReplicas(obj); got != 0 {
		t.Errorf("negative replicas = %d", got)
	}
	// More ready pods than desired: a scale-down or a surge in progress.
	if got := deploymentStatus(map[string]any{}, 2, 3, nil); got != model.StatusPending {
		t.Errorf("surplus = %q", got)
	}
	// Generation not observed yet: the controller has work to do.
	obj = map[string]any{
		"metadata": map[string]any{"generation": int64(3)},
		"status":   map[string]any{"observedGeneration": int64(2), "updatedReplicas": int64(2)},
	}
	if got := deploymentStatus(obj, 2, 1, nil); got != model.StatusPending {
		t.Errorf("unobserved generation = %q", got)
	}
}

func TestNodeRolesAndConditions(t *testing.T) {
	obj := map[string]any{
		"metadata": map[string]any{"labels": map[string]any{
			"kubernetes.io/role":       "master",
			"node-role.kubernetes.io/": "",
		}},
		"spec": map[string]any{"unschedulable": true},
		"status": map[string]any{"conditions": []any{
			map[string]any{"type": "Ready", "status": "False"},
			map[string]any{"type": "KernelDeadlock", "status": "True"},
		}},
	}
	if roles := nodeRoles(obj); len(roles) != 1 || roles[0] != "master" {
		t.Errorf("roles = %q", roles)
	}
	// SchedulingDisabled comes first, as Lens shows a cordoned node (capture).
	s := summariseNode(obj)
	if c := s.conditionsCell(); c.Text != "SchedulingDisabled NotReady KernelDeadlock" || c.Status != model.StatusError || s.token != model.StatusError {
		t.Errorf("conditions = %q %q, row %q", c.Text, c.Status, s.token)
	}
	if w := s.warning(); w.Status != model.StatusWarning || w.Text != "NotReady, KernelDeadlock" {
		t.Errorf("warning = %+v", w)
	}
}

func TestEventFallbacks(t *testing.T) {
	obj := map[string]any{
		"count":              int64(0),
		"series":             map[string]any{"count": int64(-1)},
		"reportingComponent": "kubelet",
		"reportingInstance":  "kubelet",
	}
	if n := eventCount(obj); n != 1 {
		t.Errorf("eventCount = %d, want 1", n)
	}
	if s := eventSourceText(obj); s != "kubelet" {
		t.Errorf("source = %q", s)
	}
	obj = map[string]any{"source": map[string]any{"host": "worker-1"}}
	if s := eventSourceText(obj); s != "worker-1" {
		t.Errorf("host-only source = %q", s)
	}
	obj = map[string]any{"involvedObject": map[string]any{"name": "orphan"}}
	if s := eventObjectText(obj); s != "orphan" {
		t.Errorf("involved object without kind = %q", s)
	}
}

func TestContainerDetailHelpers(t *testing.T) {
	from := map[string]any{"fileKeyRef": map[string]any{"volumeName": "cfg", "path": "app.env", "key": "TOKEN"}}
	if got := envSource(from); got != "from file cfg/app.env key TOKEN" {
		t.Errorf("fileKeyRef = %q", got)
	}
	if got := envSource(map[string]any{"futureRef": map[string]any{}}); got != "from an unknown source" {
		t.Errorf("unknown source = %q", got)
	}
	if text, token := containerState(nil, false); text != "" || token != model.StatusNone {
		t.Errorf("empty state = %q %q", text, token)
	}
	spec := map[string]any{"ports": []any{
		map[string]any{"containerPort": int64(53), "protocol": "UDP", "hostPort": int64(53)},
		map[string]any{"name": "no-port"},
	}}
	if got := containerPorts(spec); got != "53/UDP (host port 53)" {
		t.Errorf("ports = %q", got)
	}
	if got := singleLine("a\n  b\r\nc"); got != "a b c" {
		t.Errorf("singleLine = %q", got)
	}
}
