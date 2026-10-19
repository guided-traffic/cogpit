package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The source of a logs tab (ui-parity › Logs, "Pod and container selectors", gap G8): which pods
// and containers it shows. Lens's log tab has a pod and a container drop-down (its documentation's
// "View logs"); what they offer is inferred. A controlling resource's tab shows every running
// container of its pods, or narrows to one pod (LogOptions.Pod) and to the containers of one name
// (DockTab.Container), an init container among them; its merged stream follows the narrowing
// without opening the streams that stay again (workloadlogs.go). A pod's tab shows one container
// of one pod and switches to another of its containers, or to another pod of the resource that
// controls it, by opening its stream again. Every list comes from the cache. A pod's tab offers
// its ephemeral (debug) containers too (D-102); a controlling resource's tab does not: an
// ephemeral container belongs to one pod and is in no template, so the merged stream stays with
// the template's containers (the lead's choice, D-102).

// LogSources are what a logs tab's selectors offer.
type LogSources struct {
	// Workloads are a group's workloads by name (an application's tab, grouplogs.go), in the
	// group's order; none in another tab.
	Workloads []string `json:"workloads,omitempty"`
	// AllPods: the tab can show every pod at once (a controlling resource's tab).
	AllPods bool `json:"allPods"`
	// Pods are the resource's pods by name; for a pod's tab, the pods of the resource that
	// controls it (the pod alone when none does).
	Pods []string `json:"pods"`
	// Containers are the containers' names: of the tab's pod, or for a controlling resource's
	// tab of the pod it narrows to, else of every pod; InitContainers the init containers' apart,
	// EphemeralContainers a pod's tab's ephemeral (debug) containers apart.
	Containers          []string `json:"containers"`
	InitContainers      []string `json:"initContainers,omitempty"`
	EphemeralContainers []string `json:"ephemeralContainers,omitempty"`
}

// logSelection is what a controlling resource's tab narrows to, and a group's tab to one of its
// workloads (grouplogs.go); empty fields select all.
type logSelection struct {
	workload, pod, container string
}

func selectionOf(t DockTab) logSelection {
	return logSelection{workload: t.Logs.Workload, pod: t.Logs.Pod, container: t.Container}
}

func (sel logSelection) matches(pod, container string) bool {
	return (sel.pod == "" || sel.pod == pod) && (sel.container == "" || sel.container == container)
}

// LogSources returns what an open logs tab's selectors offer, from the cache.
func (s *Session) LogSources(ctx context.Context, id string) (LogSources, error) {
	tab, err := s.LogsTab(id)
	if err != nil {
		return LogSources{}, err
	}
	if tab.Ref.Kind == logKindPods {
		pod, err := s.Object(ctx, *tab.Ref)
		if err != nil {
			return LogSources{}, err
		}
		return LogSources{
			Pods:                objectNames(s.podSiblings(ctx, *tab.Ref, pod)),
			Containers:          containerNames(pod, "containers"),
			InitContainers:      containerNames(pod, "initContainers"),
			EphemeralContainers: containerNames(pod, specEphemeralContainers),
		}, nil
	}
	pods, workloads, err := s.tabPods(ctx, tab, tab.Logs.Workload)
	if err != nil {
		return LogSources{}, err
	}
	out := LogSources{Workloads: workloads, AllPods: true, Pods: objectNames(pods)}
	out.Containers, out.InitContainers = containersOf(narrowedPods(pods, tab.Logs.Pod))
	return out, nil
}

// SetLogSource changes what an open logs tab shows; nil leaves a part as it is. For a controlling
// resource's tab, pod narrows it to one of the resource's pods and container to the containers
// of that name (an init container's too); "" selects all. A pod given alone keeps the container
// if that pod has one of its name, and selects every container otherwise. For a pod's tab, pod
// switches to another pod of the resource that controls the pod, keeping the container if the
// new pod has it (else its first), and container to another of the pod's containers. It returns
// the tab.
func (s *Session) SetLogSource(ctx context.Context, id string, pod, container *string) (DockTab, error) {
	return s.SetGroupLogSource(ctx, id, nil, pod, container)
}

// SetGroupLogSource is SetLogSource with a group's workload too (an application's tab,
// grouplogs.go): workload narrows the tab to the group's workload of that name, "" to every one; a
// pod of another workload is then dropped from the selection. Other tabs take no workload.
func (s *Session) SetGroupLogSource(ctx context.Context, id string, workload, pod, container *string) (DockTab, error) {
	tab, err := s.LogsTab(id)
	if err != nil {
		return DockTab{}, err
	}
	group := IsLogGroup(tab.Ref.Kind)
	if workload != nil && *workload != "" && !group {
		return DockTab{}, fmt.Errorf("%w: only an application's logs tab narrows to a workload; this one shows %s", ErrNotApplicable, tab.Ref.String())
	}
	if tab.Ref.Kind == logKindPods {
		return s.setPodLogSource(ctx, tab, pod, container)
	}
	sel := selectionOf(tab)
	if workload != nil {
		_, names, err := s.tabPods(ctx, tab, "")
		if err != nil {
			return DockTab{}, err
		}
		if *workload != "" && !slices.Contains(names, *workload) {
			return DockTab{}, fmt.Errorf("%s has no workload %q; its workloads: %s", tab.Ref.String(), *workload, listOrNone(names))
		}
		sel.workload = *workload
	}
	pods, _, err := s.tabPods(ctx, tab, sel.workload)
	if err != nil {
		return DockTab{}, err
	}
	if pod == nil && sel.pod != "" && !slices.ContainsFunc(pods, func(p *unstructured.Unstructured) bool { return p.GetName() == sel.pod }) {
		sel.pod = "" // the pod is not the workload's now selected
	}
	if pod != nil {
		if *pod != "" && !slices.ContainsFunc(pods, func(p *unstructured.Unstructured) bool { return p.GetName() == *pod }) {
			return DockTab{}, fmt.Errorf("%s has no pod %q; its pods: %s", tab.Ref.String(), *pod, listOrNone(objectNames(pods)))
		}
		sel.pod = *pod
	}
	if container != nil {
		sel.container = *container
	}
	if sel.container != "" && (pod != nil || container != nil) {
		regular, init := containersOf(narrowedPods(pods, sel.pod))
		if all := slices.Concat(regular, init); !slices.Contains(all, sel.container) {
			if container != nil {
				return DockTab{}, fmt.Errorf("no container %q in %s; containers: %s", sel.container, sourceNoun(tab, sel.pod), listOrNone(all))
			}
			sel.container = "" // the new pod has none of that name: every container
		}
	}
	return s.updateLogsTab(id, func(t *DockTab) {
		t.Logs.Workload, t.Logs.Pod, t.Container = sel.workload, sel.pod, sel.container
	})
}

// setPodLogSource switches a pod's tab to another pod or container.
func (s *Session) setPodLogSource(ctx context.Context, tab DockTab, pod, container *string) (DockTab, error) {
	ref := *tab.Ref
	if pod != nil && *pod != ref.Name {
		if *pod == "" {
			return DockTab{}, fmt.Errorf("%w: a pod's logs tab shows one pod; logs_controller shows every pod of its controller", ErrNotApplicable)
		}
		cur, err := s.Object(ctx, ref)
		if err != nil {
			return DockTab{}, err
		}
		siblings := s.podSiblings(ctx, ref, cur)
		if !slices.ContainsFunc(siblings, func(p *unstructured.Unstructured) bool { return p.GetName() == *pod }) {
			return DockTab{}, fmt.Errorf("%q is not a pod of the resource that controls %s; its pods: %s", *pod, ref.Name, listOrNone(objectNames(siblings)))
		}
		ref.Name = *pod
	}
	obj, err := s.Object(ctx, ref)
	if err != nil {
		return DockTab{}, err
	}
	all := ContainerNames(obj, true)
	c := tab.Container
	switch {
	case container != nil && *container == "":
		return DockTab{}, fmt.Errorf("%w: a pod's logs tab shows one container; name one of %s", ErrNotApplicable, listOrNone(all))
	case container != nil:
		if !slices.Contains(all, *container) {
			return DockTab{}, fmt.Errorf("%s has no container %q; containers: %s", ref.String(), *container, listOrNone(all))
		}
		c = *container
	case !slices.Contains(all, c):
		if len(all) == 0 {
			return DockTab{}, fmt.Errorf("%s has no containers", ref.String())
		}
		c = all[0]
	}
	return s.updateLogsTab(tab.ID, func(t *DockTab) {
		r := ref
		t.Ref, t.Container, t.Title = &r, c, "Logs: "+ref.Name+"/"+c
	})
}

// updateLogsTab changes an open logs tab with fn and returns it.
func (s *Session) updateLogsTab(id string, fn func(*DockTab)) (DockTab, error) {
	var (
		out   DockTab
		found bool
	)
	s.UpdateUI(func(u *UIState) {
		for i := range u.Dock {
			t := &u.Dock[i]
			if t.ID == id && t.Type == DockLogs && t.Ref != nil {
				fn(t)
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

// tabPods returns the cached pods a controlling resource's or a group's tab shows: a resource's
// (workloadPods), or, for a group (an application's, grouplogs.go), those of its workload named
// workload ("": of every one) in the group's order, each workload's by name; and the group's
// workloads by name. A workload of the group that is gone has no pods.
func (s *Session) tabPods(ctx context.Context, tab DockTab, workload string) ([]*unstructured.Unstructured, []string, error) {
	if !IsLogGroup(tab.Ref.Kind) {
		pods, err := s.workloadPods(ctx, *tab.Ref)
		return pods, nil, err
	}
	members, err := s.groupWorkloads(ctx, *tab.Ref)
	if err != nil {
		return nil, nil, err
	}
	var pods []*unstructured.Unstructured
	names := make([]string, 0, len(members))
	for _, w := range members {
		if !slices.Contains(names, w.Name) {
			names = append(names, w.Name)
		}
		if workload != "" && w.Name != workload {
			continue
		}
		wp, err := s.workloadPods(ctx, w)
		if err != nil {
			continue // gone, or not listed: its stream says why
		}
		pods = append(pods, wp...)
	}
	return pods, names, nil
}

// groupWorkloads returns the workloads a group's object names whose logs its tab shows, waiting for
// the cache like Object.
func (s *Session) groupWorkloads(ctx context.Context, ref model.ObjectRef) ([]model.ObjectRef, error) {
	obj, err := s.Object(ctx, ref)
	if err != nil {
		return nil, err
	}
	return LogGroupWorkloads(ref, obj), nil
}

// workloadPods returns the cached pods a controlling resource controls (a Deployment's through its
// replica sets), by name, waiting for the cache like Object.
func (s *Session) workloadPods(ctx context.Context, ref model.ObjectRef) ([]*unstructured.Unstructured, error) {
	obj, err := s.Object(ctx, ref)
	if err != nil {
		return nil, err
	}
	owners := map[string]bool{string(obj.GetUID()): true}
	if ref.Kind == logKindDeployments {
		rss, err := s.Objects(ctx, logKindReplicaSets, []string{ref.Namespace})
		if err != nil {
			return nil, err
		}
		for _, rs := range rss {
			if controllerUID(rs) == string(obj.GetUID()) {
				owners[string(rs.GetUID())] = true
			}
		}
	}
	all, err := s.Objects(ctx, logKindPods, []string{ref.Namespace})
	if err != nil {
		return nil, err
	}
	var pods []*unstructured.Unstructured
	for _, p := range all {
		if owners[controllerUID(p)] {
			pods = append(pods, p)
		}
	}
	slices.SortFunc(pods, func(a, b *unstructured.Unstructured) int { return strings.Compare(a.GetName(), b.GetName()) })
	return pods, nil
}

// podSiblings returns the pods of the resource that controls pod (ControllingWorkload), the pod
// among them; the pod alone when no resource Cogpit shows logs of controls it.
func (s *Session) podSiblings(ctx context.Context, ref model.ObjectRef, pod *unstructured.Unstructured) []*unstructured.Unstructured {
	alone := []*unstructured.Unstructured{pod}
	owner, err := s.ControllingWorkload(ctx, ref)
	if err != nil {
		return alone
	}
	pods, err := s.workloadPods(ctx, owner)
	if err != nil || !slices.ContainsFunc(pods, func(p *unstructured.Unstructured) bool { return p.GetName() == ref.Name }) {
		return alone
	}
	return pods
}

// narrowedPods returns the pod named pod among pods, or every pod when pod is empty or gone.
func narrowedPods(pods []*unstructured.Unstructured, pod string) []*unstructured.Unstructured {
	if i := slices.IndexFunc(pods, func(p *unstructured.Unstructured) bool { return p.GetName() == pod }); i >= 0 {
		return pods[i : i+1]
	}
	return pods
}

// containersOf returns the distinct container and init container names of pods, in the order
// they first appear.
func containersOf(pods []*unstructured.Unstructured) (regular, init []string) {
	add := func(dst []string, names []string) []string {
		for _, n := range names {
			if !slices.Contains(dst, n) {
				dst = append(dst, n)
			}
		}
		return dst
	}
	for _, p := range pods {
		regular = add(regular, containerNames(p, "containers"))
		init = add(init, containerNames(p, "initContainers"))
	}
	return regular, init
}

func objectNames(objs []*unstructured.Unstructured) []string {
	out := make([]string, len(objs))
	for i, o := range objs {
		out[i] = o.GetName()
	}
	return out
}

func listOrNone(s []string) string {
	if len(s) == 0 {
		return "none"
	}
	return strings.Join(s, ", ")
}

// sourceNoun names what a controlling resource's tab narrows to in an error.
func sourceNoun(tab DockTab, pod string) string {
	if pod != "" {
		return "pod " + pod
	}
	return tab.Ref.String()
}
