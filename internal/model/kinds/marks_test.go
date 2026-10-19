package kinds

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/model"
)

// A pod's Containers cell keeps "ready/total" to sort and search by and draws one square per
// container, init containers first: running in the Running colour, stopped grey (a finished init
// container too), waiting pending, or red on an error; each square names its container (owner,
// 2026-10-12, from Lens). A running container that is not ready is orange (the owner's F23 of
// 2026-10-17); a plain init container, which Kubernetes never marks ready while it runs, stays in the
// Running colour, a sidecar (restartPolicy Always) does not.
func TestContainerMarks(t *testing.T) {
	obj := pod(map[string]any{
		"initContainers": []any{
			map[string]any{"name": "migrate"},
			map[string]any{"name": "proxy", "restartPolicy": "Always"},
			map[string]any{"name": "seed"},
			map[string]any{"name": "mesh", "restartPolicy": "Always"},
		},
		"containers": []any{
			map[string]any{"name": "app"},
			map[string]any{"name": "worker"},
			map[string]any{"name": "late"},
			map[string]any{"name": "warming"},
		},
	}, map[string]any{
		"phase": "Running",
		"initContainerStatuses": []any{
			map[string]any{"name": "migrate", "state": map[string]any{"terminated": map[string]any{"reason": "Completed", "exitCode": int64(0)}}},
			map[string]any{"name": "proxy", "ready": true, "started": true, "state": map[string]any{"running": map[string]any{}}},
			map[string]any{"name": "seed", "ready": false, "state": map[string]any{"running": map[string]any{}}},
			map[string]any{"name": "mesh", "ready": false, "started": true, "state": map[string]any{"running": map[string]any{}}},
		},
		"containerStatuses": []any{
			map[string]any{"name": "app", "ready": true, "state": map[string]any{"running": map[string]any{}}},
			map[string]any{"name": "worker", "state": map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff"}}},
			map[string]any{"name": "warming", "ready": false, "started": true, "state": map[string]any{"running": map[string]any{}}},
		},
	}, nil)
	marks := containerMarks(obj)
	if marks == nil {
		t.Fatal("no marks")
	}
	want := []model.CellMark{
		{Text: "migrate: terminated (Completed, exit code 0)", Status: model.StatusStopped},
		{Text: "proxy: running", Status: model.StatusOK},
		{Text: "seed: running", Status: model.StatusOK},
		{Text: "mesh: running, not ready", Status: model.StatusWarning},
		{Text: "app: running", Status: model.StatusOK},
		{Text: "worker: waiting (CrashLoopBackOff)", Status: model.StatusError},
		{Text: "late: waiting to start", Status: model.StatusPending},
		{Text: "warming: running, not ready", Status: model.StatusWarning},
	}
	if len(*marks) != len(want) {
		t.Fatalf("marks %+v", *marks)
	}
	for i, m := range *marks {
		if m != want[i] {
			t.Errorf("mark %d = %+v, want %+v", i, m, want[i])
		}
	}
	cell := containersCell(obj, summarisePod(obj))
	if cell.Text != "2/6" || cell.Marks == nil { // the sidecars count, plain init containers do not
		t.Errorf("cell %+v: the readable form stays for sorting and search", cell)
	}
}

// debuggedPod is a pod with one container and five ephemeral (debug) containers: one finished
// well, one running with a target, one finished with an error, one waiting for its image, one that
// cannot pull it, and one the kubelet has not reported yet.
func debuggedPod() map[string]any {
	eph := func(name, target string) map[string]any {
		c := map[string]any{"name": name, "image": "nicolaka/netshoot:v0.16", "stdin": true, "tty": true}
		if target != "" {
			c["targetContainerName"] = target
		}
		return c
	}
	return pod(map[string]any{
		"containers": []any{map[string]any{"name": "app"}},
		"ephemeralContainers": []any{
			eph("dnscheck", ""), eph("debugger", "app"), eph("crashed", "app"), eph("pulling", "app"),
			eph("badimage", ""), eph("fresh", "app"),
		},
	}, map[string]any{
		"phase":             "Running",
		"containerStatuses": []any{running(true)},
		"ephemeralContainerStatuses": []any{
			map[string]any{"name": "dnscheck", "state": map[string]any{"terminated": map[string]any{"reason": "Completed", "exitCode": int64(0)}}},
			map[string]any{"name": "debugger", "state": map[string]any{"running": map[string]any{}}},
			map[string]any{"name": "crashed", "state": map[string]any{"terminated": map[string]any{"reason": "Error", "exitCode": int64(130)}}},
			map[string]any{"name": "pulling", "state": map[string]any{"waiting": map[string]any{"reason": "ContainerCreating"}}},
			map[string]any{"name": "badimage", "state": map[string]any{"waiting": map[string]any{"reason": "ImagePullBackOff"}}},
		},
	}, nil)
}

// Ephemeral containers get a square each after the regular ones (the owner, 2026-10-19; D-102):
// blue while running or starting, a blue outline once finished whatever the exit code, red while
// waiting on an error; the live ones first, so that the finished ones are what a narrow cell's
// count hides. The tooltip names each ephemeral with its state and target. The ratio stays
// kubectl's READY: ephemeral containers count in neither part.
func TestEphemeralContainerMarks(t *testing.T) {
	obj := debuggedPod()
	marks := containerMarks(obj)
	if marks == nil {
		t.Fatal("no marks")
	}
	want := []model.CellMark{
		{Text: "app: running", Status: model.StatusOK},
		{Text: "debugger: ephemeral, running, target app", Status: model.StatusEphemeral},
		{Text: "pulling: ephemeral, waiting (ContainerCreating), target app", Status: model.StatusEphemeral},
		{Text: "badimage: ephemeral, waiting (ImagePullBackOff)", Status: model.StatusError},
		{Text: "fresh: ephemeral, waiting to start, target app", Status: model.StatusEphemeral},
		{Text: "dnscheck: ephemeral, terminated (Completed, exit code 0)", Status: model.StatusEphemeral, Hollow: true},
		{Text: "crashed: ephemeral, terminated (Error, exit code 130), target app", Status: model.StatusEphemeral, Hollow: true},
	}
	if len(*marks) != len(want) {
		t.Fatalf("marks %+v", *marks)
	}
	for i, m := range *marks {
		if m != want[i] {
			t.Errorf("mark %d = %+v, want %+v", i, m, want[i])
		}
	}
	s := summarisePod(obj)
	if cell := containersCell(obj, s); cell.Text != "1/1" {
		t.Errorf("the ratio %q: ephemeral containers do not count", cell.Text)
	}
	if s.text != "Running" || s.status != model.StatusOK || s.restarts != 0 {
		t.Errorf("summary %+v: an ephemeral container's state is not the pod's", s)
	}
}

// The drawer's Ephemeral Containers group names each one's Target (the owner's "Fix bauen",
// 2026-10-19), after its Status; one without a target has no such row, and regular containers
// never have one.
func TestEphemeralContainerSectionTarget(t *testing.T) {
	sections := podDetail(asUnstructured(debuggedPod()))
	got := map[string]model.DetailSection{}
	for _, sec := range sections {
		if sec.Group != "" {
			got[sec.Group+"/"+sec.Title] = sec
		}
	}
	debugger, ok := got["Ephemeral Containers/debugger"]
	if !ok {
		t.Fatalf("no section for debugger: %v", got)
	}
	if v := sectionValue(debugger, "Target"); v != "app" {
		t.Errorf("debugger's Target %q", v)
	}
	if debugger.Items[0].Name != titleStatus || debugger.Items[1].Name != "Target" {
		t.Errorf("Target is not right after Status: %+v", debugger.Items[:2])
	}
	if v := sectionValue(got["Ephemeral Containers/dnscheck"], "Target"); v != "" {
		t.Errorf("dnscheck has no target, shows %q", v)
	}
	if v := sectionValue(got["Containers/app"], "Target"); v != "" {
		t.Errorf("a regular container shows a Target %q", v)
	}
}

// sectionValue is the value of a section's item by name, "" without one.
func sectionValue(sec model.DetailSection, name string) string {
	for _, it := range sec.Items {
		if it.Name == name {
			return it.Value
		}
	}
	return ""
}
