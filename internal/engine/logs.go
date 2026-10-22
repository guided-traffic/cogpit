package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Logs tabs (ui-parity › Logs, gap G8). A logs tab shows a pod's container (pod.logs) or every
// container of the pods a controlling resource owns (workload.logs). Its options live in the
// engine, on the tab (DockTab.Logs), so that agents read them with ui_state and set them with
// logs_setOptions like the user does; the GUI renders them. Previous changes the request to the
// API server, so the GUI opens the stream again when it changes; the others are how the tab shows
// the lines it holds. Every stream carries the API server's timestamps since 2026-10-13, whether
// the tab shows them or not: the tab jumps to a time and loads older lines by them
// (logsolder.go), and the GUI hides them unless Timestamps is on. What a tab shows (its pod and
// container) is its source (logsources.go).

// LogOptions are a logs tab's options.
type LogOptions struct {
	// Previous shows the log of the container's previous, terminated instance (kubectl logs -p).
	Previous bool `json:"previous,omitempty"`
	// Timestamps shows each line's timestamp from the API server (kubectl logs --timestamps),
	// which the window writes in the user's zone and format with milliseconds (D-31). The stream
	// carries them either way, as RFC 3339.
	Timestamps bool `json:"timestamps,omitempty"`
	// ResourceNames shows each line's pod and container in a controlling resource's tab, whose
	// stream starts every line with "<pod>/<container> ".
	ResourceNames bool `json:"resourceNames,omitempty"`
	// Wrap wraps long lines instead of cutting them at the tab's edge.
	Wrap bool `json:"wrap,omitempty"`
	// Search is the text the tab looks for in its lines, case-insensitively.
	Search string `json:"search,omitempty"`
	// Filter shows only the lines that contain Search; off, the tab shows every line and
	// highlights the matches.
	Filter bool `json:"filter,omitempty"`
	// Pod narrows a controlling resource's tab to one of its pods; empty: every pod. (The
	// container a tab shows, or narrows to, is DockTab.Container.)
	Pod string `json:"pod,omitempty"`
	// Workload narrows a group's tab (an application's, grouplogs.go) to the workload of that
	// name; empty: every workload.
	Workload string `json:"workload,omitempty"`
	// LoadOlder counts the requests for older lines (logs.loadOlder): the GUI loads the lines
	// before the oldest it holds once per request (logsolder.go).
	LoadOlder int `json:"loadOlder,omitempty"`
	// Jump is the latest jump to a time (logs.jumpTo, logsjump.go): the GUI scrolls to the first
	// line written at or after it once per jump.
	Jump LogJump `json:"jump,omitzero"`
}

// RequestOlderLogs asks an open logs tab for the lines before the oldest it holds: the GUI loads
// them (OlderLogs) as it does when the user selects the tab's "Load older lines" row. It returns
// the tab.
func (s *Session) RequestOlderLogs(id string) (DockTab, error) {
	return s.updateLogsTab(id, func(t *DockTab) { t.Logs.LoadOlder++ })
}

// Kind keys the logs code refers to.
const (
	logKindPods         = "pods"
	logKindDeployments  = "deployments"
	logKindReplicaSets  = "replicasets"
	logKindDaemonSets   = "daemonsets"
	logKindStatefulSets = "statefulsets"
	logKindJobs         = "jobs"
)

// MaxLogSearch bounds a logs tab's search text, in bytes.
const MaxLogSearch = 1024

// WorkloadLogKinds are the kinds whose logs a tab shows for all their pods (workload.logs), as
// Lens offers Logs for them.
var WorkloadLogKinds = []string{logKindDeployments, logKindDaemonSets, logKindStatefulSets, logKindReplicaSets, logKindJobs}

// controllerKinds maps the controller kinds of pods to their kind keys.
var controllerKinds = map[string]string{
	apiKindReplicaSet: logKindReplicaSets, apiKindDaemonSet: logKindDaemonSets, apiKindStatefulSet: logKindStatefulSets, "Job": logKindJobs,
	"Deployment": logKindDeployments,
}

// logStreamer opens one container's log stream: *cluster.Conn, or a fake in tests.
type logStreamer interface {
	StreamLogs(ctx context.Context, opts cluster.LogOptions) (io.ReadCloser, error)
}

// openLogs opens a logs tab's stream with streamer: one container's log for a pod's tab, the
// merged logs of its pods for a controlling resource's tab.
func (s *Session) openLogs(ctx context.Context, tab DockTab, b *Backend, streamer logStreamer) (io.ReadCloser, error) {
	if IsLogGroup(tab.Ref.Kind) { // an application's workloads (grouplogs.go)
		return s.openGroupLogs(ctx, tab, b, streamer)
	}
	if tab.Ref.Kind != logKindPods {
		return s.openWorkloadLogs(ctx, tab, b, streamer)
	}
	return s.openResumingLog(ctx, b, streamer, podLogOptions(tab)) // goes on after an interruption (logsresume.go)
}

// podLogOptions is the log request of a pod's tab: the last 1,000 lines, then follow; the
// previous container's whole log (up to 1,000 lines) without following when Previous is set.
// Always with timestamps, which the GUI hides unless the tab shows them.
func podLogOptions(tab DockTab) cluster.LogOptions {
	return cluster.LogOptions{
		Namespace: tab.Ref.Namespace, Pod: tab.Ref.Name, Container: tab.Container,
		Follow: !tab.Logs.Previous, Previous: tab.Logs.Previous, Timestamps: true,
		TailLines: logTailLines,
	}
}

// LogsTab returns an open logs tab.
func (s *Session) LogsTab(id string) (DockTab, error) {
	tab, ok := s.UIState().DockTab(id)
	if !ok {
		return DockTab{}, fmt.Errorf("no dock tab %q", id)
	}
	if tab.Type != DockLogs || tab.Ref == nil {
		return DockTab{}, fmt.Errorf("dock tab %q is a %s tab, not a logs tab", id, tab.Type)
	}
	return tab, nil
}

// SetLogOptions changes an open logs tab's options with fn and returns the tab.
func (s *Session) SetLogOptions(id string, fn func(*LogOptions)) (DockTab, error) {
	var (
		out   DockTab
		found bool
	)
	s.UpdateUI(func(u *UIState) {
		for i := range u.Dock {
			t := &u.Dock[i]
			if t.ID == id && t.Type == DockLogs && t.Ref != nil {
				fn(&t.Logs)
				out, found = *t, true
				return
			}
		}
	})
	if !found {
		return DockTab{}, fmt.Errorf("no logs tab %q", id)
	}
	return out, nil
}

// OpenWorkloadLogsTab opens a logs tab on every pod of a controlling resource, with the pods'
// names shown, or activates the one already open on it. It returns the tab and whether it is new.
func (s *Session) OpenWorkloadLogsTab(ctx context.Context, ref model.ObjectRef) (DockTab, bool, error) {
	ref, p, err := s.ResolveRef(ref)
	if err != nil {
		return DockTab{}, false, err
	}
	if !slices.Contains(WorkloadLogKinds, ref.Kind) {
		return DockTab{}, false, fmt.Errorf("%w: logs of all pods are offered for %v", ErrNotApplicable, WorkloadLogKinds)
	}
	if _, err := s.Object(ctx, ref); err != nil {
		return DockTab{}, false, err
	}
	tab := DockTab{
		ID: s.NewDockID(DockLogs), Type: DockLogs, Title: "Logs: " + p.Kind + " " + ref.Name,
		Context: ref.Context, Ref: &ref, Logs: LogOptions{ResourceNames: true},
	}
	created := true
	s.UpdateUI(func(u *UIState) {
		for _, t := range u.Dock {
			if t.Type == DockLogs && t.Ref != nil && t.Ref.Same(ref) && t.Context == ref.Context {
				tab, created = t, false
				break
			}
		}
		if created {
			u.Dock = append(u.Dock, tab)
		}
		u.ShowDockTab(tab.ID)
	})
	return tab, created, nil
}

// OpenGroupLogsTab opens a logs tab on every workload a group deploys (A81: an application's,
// grouplogs.go), each line starting with "<workload>/<pod>/<container>", the names shown, or
// activates the one already open on it. It returns the tab and whether it is new.
func (s *Session) OpenGroupLogsTab(ctx context.Context, ref model.ObjectRef) (DockTab, bool, error) {
	ref, p, err := s.ResolveRef(ref)
	if err != nil {
		return DockTab{}, false, err
	}
	if !IsLogGroup(ref.Kind) {
		return DockTab{}, false, fmt.Errorf("%w: %s groups no workloads whose logs a tab shows", ErrNotApplicable, ref.Kind)
	}
	workloads, err := s.groupWorkloads(ctx, ref)
	if err != nil {
		return DockTab{}, false, err
	}
	if len(workloads) == 0 {
		return DockTab{}, false, fmt.Errorf("%w: %s %s has no workloads whose logs a tab shows (%v)", ErrNotApplicable, p.Kind, ref.Name, WorkloadLogKinds)
	}
	tab := DockTab{
		ID: s.NewDockID(DockLogs), Type: DockLogs, Title: "Logs: " + p.Kind + " " + ref.Name,
		Context: ref.Context, Ref: &ref, Logs: LogOptions{ResourceNames: true},
	}
	created := true
	s.UpdateUI(func(u *UIState) {
		for _, t := range u.Dock {
			if t.Type == DockLogs && t.Ref != nil && t.Ref.Same(ref) && t.Context == ref.Context {
				tab, created = t, false
				break
			}
		}
		if created {
			u.Dock = append(u.Dock, tab)
		}
		u.ShowDockTab(tab.ID)
	})
	return tab, created, nil
}

// ErrNoController is returned for a pod no supported resource controls.
var ErrNoController = errors.New("no controlling resource whose logs Cogpit shows")

// PodController returns the kind and name of the resource that controls a cached pod, by its
// controller reference (a pod's ReplicaSet, DaemonSet, StatefulSet or Job), without following it
// further. It does not wait for the cache.
func (s *Session) PodController(pod model.ObjectRef) (model.ObjectRef, error) {
	if pod.Kind != logKindPods {
		return model.ObjectRef{}, errors.New("not a pod")
	}
	pod, _, err := s.ResolveRef(pod)
	if err != nil {
		return model.ObjectRef{}, err
	}
	obj, err := s.Lookup(pod)
	if err != nil {
		return model.ObjectRef{}, err
	}
	return directController(obj, pod)
}

// ControllingWorkload returns the resource whose logs a pod's tab moves to: the pod's controller,
// or the Deployment that controls the pod's ReplicaSet. It waits for the cache like Object.
func (s *Session) ControllingWorkload(ctx context.Context, pod model.ObjectRef) (model.ObjectRef, error) {
	if pod.Kind != logKindPods {
		return model.ObjectRef{}, errors.New("not a pod")
	}
	pod, _, err := s.ResolveRef(pod)
	if err != nil {
		return model.ObjectRef{}, err
	}
	obj, err := s.Object(ctx, pod)
	if err != nil {
		return model.ObjectRef{}, err
	}
	ref, err := directController(obj, pod)
	if err != nil || ref.Kind != logKindReplicaSets {
		return ref, err
	}
	rs, err := s.Object(ctx, ref)
	if err != nil {
		return ref, nil // the replica set's own pods, then
	}
	if dep, err := directController(rs, ref); err == nil && dep.Kind == logKindDeployments {
		return dep, nil
	}
	return ref, nil
}

// directController returns the object's controller as a reference in obj's namespace and
// context, if it is of a kind whose logs Cogpit shows.
func directController(obj *unstructured.Unstructured, of model.ObjectRef) (model.ObjectRef, error) {
	for _, o := range obj.GetOwnerReferences() {
		if o.Controller == nil || !*o.Controller {
			continue
		}
		if key, ok := controllerKinds[o.Kind]; ok && o.Name != "" {
			return model.ObjectRef{Context: of.Context, Kind: key, Namespace: of.Namespace, Name: o.Name}, nil
		}
		return model.ObjectRef{}, fmt.Errorf("%s is controlled by %s %s: %w", of.String(), o.Kind, o.Name, ErrNoController)
	}
	return model.ObjectRef{}, fmt.Errorf("%s: %w", of.String(), ErrNoController)
}

// MoveLogsTab turns a pod's logs tab into the logs of the resource that controls the pod
// (ControllingWorkload): the same tab, now on every pod of that resource with their names shown;
// the other options stay. It returns the tab.
func (s *Session) MoveLogsTab(ctx context.Context, id string) (DockTab, error) {
	tab, err := s.LogsTab(id)
	if err != nil {
		return DockTab{}, err
	}
	if tab.Ref.Kind != logKindPods {
		return DockTab{}, fmt.Errorf("logs tab %q shows %s already", id, tab.Ref.String())
	}
	target, err := s.ControllingWorkload(ctx, *tab.Ref)
	if err != nil {
		return DockTab{}, err
	}
	target, p, err := s.ResolveRef(target)
	if err != nil {
		return DockTab{}, err
	}
	var (
		out   DockTab
		found bool
	)
	s.UpdateUI(func(u *UIState) {
		for i := range u.Dock {
			t := &u.Dock[i]
			if t.ID != id || t.Type != DockLogs {
				continue
			}
			r := target
			t.Ref, t.Container, t.Title = &r, "", "Logs: "+p.Kind+" "+target.Name
			t.Logs.ResourceNames, t.Logs.Pod = true, ""
			out, found = *t, true
			return
		}
	})
	if !found {
		return DockTab{}, fmt.Errorf("no logs tab %q", id)
	}
	return out, nil
}

// The API kinds of the apps group's workloads, as owner references and alert labels name them.
const (
	apiKindReplicaSet  = "ReplicaSet"
	apiKindStatefulSet = "StatefulSet"
	apiKindDaemonSet   = "DaemonSet"
)
