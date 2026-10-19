package commands

import (
	"context"
	"fmt"
	"slices"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// logOptionsParams are logs.setOptions' parameters: the tab and the options to change; options
// left out keep their value.
type logOptionsParams struct {
	ID            string  `json:"id" jsonschema:"logs tab id as ui_state lists it, e.g. logs-1"`
	Previous      *bool   `json:"previous,omitempty" jsonschema:"show the log of the container's previous, terminated instance (the stream opens again)"`
	Timestamps    *bool   `json:"timestamps,omitempty" jsonschema:"show each line's timestamp from the API server, written in the user's zone and format with milliseconds (the stream carries it either way)"`
	ResourceNames *bool   `json:"resourceNames,omitempty" jsonschema:"show each line's pod and container, in tabs of a controlling resource"`
	Wrap          *bool   `json:"wrap,omitempty" jsonschema:"wrap long lines instead of cutting them at the tab's edge"`
	Search        *string `json:"search,omitempty" jsonschema:"text to find in the lines, case-insensitively, at most 1024 bytes; empty clears it"`
	Filter        *bool   `json:"filter,omitempty" jsonschema:"show only the lines that contain search; off shows every line with the matches highlighted"`
}

// logSourceParams are logs.setSource's parameters: the tab, and the parts of its source to change.
type logSourceParams struct {
	ID        string  `json:"id" jsonschema:"logs tab id as ui_state lists it, e.g. logs-1"`
	Workload  *string `json:"workload,omitempty" jsonschema:"an application's tab only: the workload to show, one logs_sources lists; empty: every workload"`
	Pod       *string `json:"pod,omitempty" jsonschema:"the pod to show, one logs_sources lists; empty: every pod of a controlling resource's tab (a pod's tab shows one pod)"`
	Container *string `json:"container,omitempty" jsonschema:"the container to show, one logs_sources lists (an init container too, and in a pod's tab an ephemeral one); empty: every container of a controlling resource's tab (a pod's tab shows one)"`
}

// workloadLogVerbs are what a controlling resource's logs cost: the pods' (and for a Deployment
// the replica sets') list and watch through the cache, and each container's log.
var workloadLogVerbs = []engine.Verb{
	{Verb: nwVerbGet, Resource: kindPods, Subresource: "log"},
	{Verb: nwVerbList, Resource: kindPods}, {Verb: nwVerbWatch, Resource: kindPods},
	{Verb: nwVerbList, Group: nwGroupApps, Resource: replicaSetKindKey}, {Verb: nwVerbWatch, Group: nwGroupApps, Resource: replicaSetKindKey},
}

func registerLogs(g *registrar) {
	// Lens's documented orders: Deployment Scale, Restart, Logs (captured: 300); Stateful Set
	// Restart, Scale, Logs; Daemon Set Restart, Logs; Replica Set Scale, Logs; Job Logs. Logs
	// after the kind's other actions everywhere.
	add(g, engine.Spec[refParams]{
		ID: "workload.logs", Label: "Logs", MenuPath: "Workload/Logs", Kinds: engine.WorkloadLogKinds, Order: 300,
		Danger: engine.DangerRead, Verbs: workloadLogVerbs,
		Description: "Opens a dock tab with the logs of every container of every pod a Deployment, Daemon Set, Stateful " +
			"Set, Replica Set or Job controls (a Deployment's through its replica sets), each line starting with " +
			"<pod>/<container>, and follows them: pods and restarted containers join as they start. It starts with up " +
			"to 1,000 lines of history shared by the containers and streams at most 40 containers at once. Activates " +
			"the tab already open on the resource. Options: logs_setOptions. Returns the tab.",
		Enabled: func(s *engine.Session, p refParams) error {
			if !slices.Contains(engine.WorkloadLogKinds, p.Ref.Kind) {
				return fmt.Errorf("%w: logs of all pods are offered for %v", engine.ErrNotApplicable, engine.WorkloadLogKinds)
			}
			if _, err := streamableOn(s, p.Ref.Context, "logs"); err != nil {
				return err
			}
			_, err := s.Lookup(p.Ref)
			return err
		},
		Run: func(ctx context.Context, s *engine.Session, p refParams) (any, error) {
			tab, created, err := s.OpenWorkloadLogsTab(ctx, p.Ref)
			if err != nil {
				return nil, err
			}
			return DockResult{Tab: tab, Created: created}, nil
		},
	})

	add(g, engine.Spec[logOptionsParams]{
		ID: "logs.setOptions", Label: "Log Options", Danger: engine.DangerUI,
		Description: "Changes a logs tab's options, as the tab's toolbar does: previous (the previous terminated " +
			"container's log), timestamps, resourceNames, wrap, search and filter. Options left out keep their value. " +
			"Changing previous opens the stream again; the others change how the tab shows its lines. Which pod and " +
			"container a tab shows: logs_setSource. ui_state lists every tab's options. Returns the tab.",
		Enabled: func(s *engine.Session, p logOptionsParams) error {
			_, err := s.LogsTab(p.ID)
			return err
		},
		Run: func(_ context.Context, s *engine.Session, p logOptionsParams) (any, error) {
			if p.Search != nil && len(*p.Search) > engine.MaxLogSearch {
				return nil, fmt.Errorf("search is %d bytes; at most %d", len(*p.Search), engine.MaxLogSearch)
			}
			return s.SetLogOptions(p.ID, func(o *engine.LogOptions) {
				set := func(dst *bool, v *bool) {
					if v != nil {
						*dst = *v
					}
				}
				set(&o.Previous, p.Previous)
				set(&o.Timestamps, p.Timestamps)
				set(&o.ResourceNames, p.ResourceNames)
				set(&o.Wrap, p.Wrap)
				set(&o.Filter, p.Filter)
				if p.Search != nil {
					o.Search = *p.Search
				}
			})
		},
	})

	add(g, engine.Spec[dockParams]{
		ID: "logs.controller", Label: "Logs of the Controlling Resource", Danger: engine.DangerRead, Verbs: workloadLogVerbs,
		Description: "Turns a pod's logs tab into the logs of the resource that controls the pod (its Deployment, " +
			"Daemon Set, Stateful Set, Replica Set or Job; a Deployment's pods through their replica set), as " +
			"workload_logs shows them: every pod's containers, each line with its pod and container. Returns the tab.",
		Enabled: func(s *engine.Session, p dockParams) error {
			tab, err := s.LogsTab(p.ID)
			if err != nil {
				return err
			}
			if tab.Ref.Kind != kindPods {
				return fmt.Errorf("%w: the tab shows %s %s already", engine.ErrNotApplicable, tab.Ref.Kind, tab.Ref.Name)
			}
			if _, err := streamableOn(s, tabContext(s, p.ID), "logs"); err != nil {
				return err
			}
			_, err = s.PodController(*tab.Ref)
			return err
		},
		Run: func(ctx context.Context, s *engine.Session, p dockParams) (any, error) {
			return s.MoveLogsTab(ctx, p.ID)
		},
	})

	add(g, engine.Spec[dockParams]{
		ID: "logs.sources", Label: "Log Sources", Danger: engine.DangerRead, Verbs: workloadLogVerbs[1:],
		Description: "Lists what a logs tab's pod and container selectors offer, from Cogpit's cache: for the logs of " +
			"a controlling resource (allPods true) its pods, and the containers of the pod the tab shows or of every " +
			"pod; for an application's tab (application_logs, argocd_logs, flux_logs) also its workloads by name, the " +
			"pods then of the workload it shows or of every one; for a pod's tab the pods of the resource that controls " +
			"it (the pod alone if none does) and the pod's containers. Init containers are listed apart, and a pod's " +
			"tab lists its ephemeral (debug) containers apart too (ephemeralContainers). Makes no API " +
			"request once the pods are cached.",
		Enabled: func(s *engine.Session, p dockParams) error { _, err := s.LogsTab(p.ID); return err },
		Run: func(ctx context.Context, s *engine.Session, p dockParams) (any, error) {
			return s.LogSources(ctx, p.ID)
		},
	})

	add(g, engine.Spec[logSourceParams]{
		ID: "logs.setSource", Label: "Show Pod or Container", Danger: engine.DangerRead, Verbs: workloadLogVerbs,
		Description: "Changes which pod and container a logs tab shows, as the tab's selectors do (choices: " +
			"logs_sources). For an application's tab, workload narrows it to one of the application's workloads (empty: " +
			"every one; a pod of another workload is then dropped). For the logs of a controlling resource, pod narrows the tab to one of its pods and " +
			"container to the containers of that name, an init container's too; an empty value selects every pod or " +
			"container again (init containers that ran to completion only when named). The merged stream follows " +
			"at once: the streams left out end, the others go on, and a container selected again resumes from its " +
			"newest line with at most 1,000 lines written meanwhile. For a pod's tab, pod switches to another pod " +
			"of the resource that controls it (keeping the container if that pod has it) and container to another " +
			"of the pod's containers; its stream opens again. Parts left out stay. ui_state shows the tab's pod " +
			"(logs.pod) and container. Returns the tab.",
		Enabled: func(s *engine.Session, p logSourceParams) error {
			if _, err := s.LogsTab(p.ID); err != nil {
				return err
			}
			_, err := streamableOn(s, tabContext(s, p.ID), "logs")
			return err
		},
		Run: func(ctx context.Context, s *engine.Session, p logSourceParams) (any, error) {
			return s.SetGroupLogSource(ctx, p.ID, p.Workload, p.Pod, p.Container)
		},
	})

	add(g, engine.Spec[dockParams]{
		ID: "logs.loadOlder", Label: "Load Older Lines", Danger: engine.DangerRead, Verbs: workloadLogVerbs,
		Description: "Loads the lines before the oldest a logs tab holds, as selecting the tab's \"Load older lines\" " +
			"row does. Cogpit's window asks the API server for each container's tail again with 1,000 more lines " +
			"(shared by a controlling resource's containers, at least 100 each) and puts the older ones on top, " +
			"without repeating a line; a tab holds at most 10,000 lines. The window carries it out; ui_state counts " +
			"the requests among the tab's log options (loadOlder). Returns the tab.",
		Enabled: func(s *engine.Session, p dockParams) error {
			if _, err := s.LogsTab(p.ID); err != nil {
				return err
			}
			_, err := streamableOn(s, tabContext(s, p.ID), "logs")
			return err
		},
		Run: func(_ context.Context, s *engine.Session, p dockParams) (any, error) {
			return s.RequestOlderLogs(p.ID)
		},
	})

	add(g, engine.Spec[logJumpParams]{
		ID: "logs.jumpTo", Label: "Jump to Time", Danger: engine.DangerRead, Verbs: workloadLogVerbs,
		Description: "Scrolls a logs tab to the first line written at or after a time, as the tab's time field does, " +
			"and stops it following. The time reads in the zone and format the tab's timestamps show (ui_state's " +
			"timeFormat, the Preferences' time zone) or in ISO 8601: a date with a time of day (\"2026-10-13 01:20\", seconds " +
			"optional), a date alone, \"01:20\" (today, or yesterday if that is still to come), \"10m ago\", \"1h30m ago\", " +
			"\"2d ago\", \"now\", or RFC 3339. The lines the tab holds are searched first; when it holds none that old, Cogpit's " +
			"window asks the API server for each container's lines since then (bounded by the 10,000 lines a tab " +
			"holds) and puts them on top. The window carries it out; ui_state shows the latest jump among the tab's " +
			"log options (jump). Returns the tab.",
		Enabled: func(s *engine.Session, p logJumpParams) error {
			if _, err := s.LogsTab(p.ID); err != nil {
				return err
			}
			_, err := streamableOn(s, tabContext(s, p.ID), "logs")
			return err
		},
		Run: func(_ context.Context, s *engine.Session, p logJumpParams) (any, error) {
			return s.JumpLogs(p.ID, p.Time)
		},
	})
}

// logJumpParams are logs.jumpTo's parameters.
type logJumpParams struct {
	ID   string `json:"id" jsonschema:"logs tab id as ui_state lists it, e.g. logs-1"`
	Time string `json:"time" jsonschema:"the time to jump to, e.g. 2026-10-13 01:20, 01:20, 10m ago or 2026-10-13T01:20:00Z"`
}
