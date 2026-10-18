package commands

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// DockResult is the result of the commands that open a dock tab.
type DockResult struct {
	Tab     engine.DockTab `json:"tab"`
	Created bool           `json:"created"` // false: an existing tab was activated
}

type containerParams struct {
	Ref       model.ObjectRef `json:"ref" jsonschema:"the pod"`
	Container string          `json:"container,omitempty" jsonschema:"container name; empty means the pod's first container"`
}

type dockParams struct {
	ID string `json:"id" jsonschema:"dock tab id as ui_state lists it, e.g. logs-1"`
}

func registerDock(g *registrar) {
	add(g, engine.Spec[containerParams]{
		ID: "pod.logs", Label: "Logs", MenuPath: "Pod/Logs", Kinds: []string{kindPods}, Order: 500, Danger: engine.DangerRead,
		Verbs: []engine.Verb{{Verb: "get", Resource: kindPods, Subresource: "log"}},
		Description: "Opens a dock tab that follows a container's log (the last 1,000 lines, then new ones), or activates " +
			"the tab already open for that pod and container. Returns the tab.",
		Enabled: func(s *engine.Session, p containerParams) error {
			if _, err := streamableOn(s, p.Ref.Context, "logs"); err != nil {
				return err
			}
			return podRef(p.Ref)
		},
		Run: func(ctx context.Context, s *engine.Session, p containerParams) (any, error) {
			ref, container, err := podContainer(ctx, s, p)
			if err != nil {
				return nil, err
			}
			tab := engine.DockTab{
				ID: s.NewDockID(engine.DockLogs), Type: engine.DockLogs, Title: "Logs: " + ref.Name + "/" + container,
				Context: ref.Context, Ref: &ref, Container: container,
			}
			created := true
			s.UpdateUI(func(u *engine.UIState) {
				for _, t := range u.Dock {
					if t.Type == engine.DockLogs && t.Ref != nil && t.Ref.Same(ref) && t.Context == ref.Context && t.Container == container {
						tab, created = t, false
						break
					}
				}
				if created {
					u.Dock = append(u.Dock, tab)
				}
				u.ActiveDock = tab.ID
			})
			return DockResult{Tab: tab, Created: created}, nil
		},
	})

	add(g, engine.Spec[containerParams]{
		ID: "pod.shell", Label: "Shell", MenuPath: "Pod/Shell", Kinds: []string{kindPods}, Order: 200, Danger: engine.DangerMutating,
		Verbs: []engine.Verb{{Verb: "create", Resource: kindPods, Subresource: "exec"}},
		Description: "Opens a dock tab with an interactive shell in a container (bash, else ash, else sh). Anything typed " +
			"there runs with the container's privileges, so an agent's request waits for the user's acceptance. Returns the tab.",
		Enabled: func(s *engine.Session, p containerParams) error {
			if _, err := streamableOn(s, p.Ref.Context, "shells"); err != nil {
				return err
			}
			return podRef(p.Ref)
		},
		Targets: func(s *engine.Session, p containerParams) ([]model.ObjectRef, error) { return targetOf(s, p.Ref) },
		Run: func(ctx context.Context, s *engine.Session, p containerParams) (any, error) {
			ref, container, err := podContainer(ctx, s, p)
			if err != nil {
				return nil, err
			}
			if err := engine.CheckConfirmedContext(ctx, ref.Context); err != nil {
				return nil, err
			}
			tab := engine.DockTab{
				ID: s.NewDockID(engine.DockShell), Type: engine.DockShell, Title: "Shell: " + ref.Name + "/" + container,
				Context: ref.Context, Ref: &ref, Container: container,
			}
			s.UpdateUI(func(u *engine.UIState) {
				u.Dock = append(u.Dock, tab)
				u.ActiveDock = tab.ID
			})
			return DockResult{Tab: tab, Created: true}, nil
		},
	})

	add(g, engine.Spec[engine.OnCluster]{
		ID: "terminal.open", Label: "New Terminal", MenuPath: "View/New Terminal", Shortcut: "Cmd+Shift+T", Danger: engine.DangerUI,
		Description: "Opens a dock tab with a local shell whose kubectl is bound to a connected context (the shown cluster's " +
			"unless context names another) and, for the shown cluster, to the namespace filter if it names exactly one " +
			"namespace. Returns the tab.",
		Enabled: func(s *engine.Session, p engine.OnCluster) error {
			_, err := streamableOn(s, p.Context, "terminals")
			return err
		},
		Run: func(_ context.Context, s *engine.Session, p engine.OnCluster) (any, error) {
			tab, err := openTerminalTab(s, p.Context)
			if err != nil {
				return nil, err
			}
			return DockResult{Tab: tab, Created: true}, nil
		},
	})

	add(g, engine.Spec[dockParams]{
		ID: "dock.close", Label: "Close Tab", Danger: engine.DangerUI,
		Description: "Closes a dock tab and ends its stream, shell or job (a job stops; what it wrote stays). The tab before it becomes active if the closed one was. " +
			"Returns the new UI state.",
		Run: func(_ context.Context, s *engine.Session, p dockParams) (any, error) {
			found := false
			st := s.UpdateUI(func(u *engine.UIState) {
				i := slices.IndexFunc(u.Dock, func(t engine.DockTab) bool { return t.ID == p.ID })
				if i < 0 {
					return
				}
				found = true
				u.Dock = slices.Delete(u.Dock, i, i+1)
				if u.ActiveDock != p.ID {
					return
				}
				switch {
				case len(u.Dock) == 0:
					u.ActiveDock = ""
				case i > 0:
					u.ActiveDock = u.Dock[i-1].ID
				default:
					u.ActiveDock = u.Dock[0].ID
				}
			})
			if !found {
				return nil, fmt.Errorf("no dock tab %q", p.ID)
			}
			return st, nil
		},
	})

	add(g, engine.Spec[dockParams]{
		ID: "dock.activate", Label: "Show Tab", Danger: engine.DangerUI,
		Description: "Shows a dock tab. The dock shows the tabs of the cluster the window shows and those of no cluster " +
			"(ui_state's dock lists every tab with its context); a tab of another connected cluster shows that cluster " +
			"first, as cluster_connect shows a connected one, from Cogpit's cache and without a request. Returns the new UI state.",
		Run: func(_ context.Context, s *engine.Session, p dockParams) (any, error) {
			u := s.UIState()
			if t, ok := u.DockTab(p.ID); ok && !u.InDockScope(t) {
				if err := s.Show(t.Context); err != nil { // the dock per cluster (D-69): its cluster first
					return nil, err
				}
			}
			found := false
			st := s.UpdateUI(func(u *engine.UIState) {
				if _, ok := u.DockTab(p.ID); ok {
					found = true
					u.ActiveDock = p.ID
				}
			})
			if !found {
				return nil, fmt.Errorf("no dock tab %q", p.ID)
			}
			return st, nil
		},
	})
}

func podRef(ref model.ObjectRef) error {
	if ref.Kind != kindPods {
		return errors.New("this command applies to pods")
	}
	return nil
}

// podContainer resolves a pod reference from the cache and picks its container: the given one,
// which must be one of the pod's containers or init containers, or the pod's first container.
func podContainer(ctx context.Context, s *engine.Session, p containerParams) (model.ObjectRef, string, error) {
	if err := podRef(p.Ref); err != nil {
		return p.Ref, "", err
	}
	ref, _, err := s.ResolveRef(p.Ref)
	if err != nil {
		return ref, "", err
	}
	pod, err := s.Object(ctx, ref)
	if err != nil {
		return ref, "", err
	}
	names := engine.ContainerNames(pod, true)
	if p.Container == "" {
		if len(names) == 0 {
			return ref, "", fmt.Errorf("%s has no containers", ref.String())
		}
		return ref, names[0], nil
	}
	if !slices.Contains(names, p.Container) {
		return ref, "", fmt.Errorf("%s has no container %q; containers: %s", ref.String(), p.Container, strings.Join(names, ", "))
	}
	return ref, p.Container, nil
}

// openTerminalTab opens a terminal tab in the dock, as terminal.open does: a local shell whose
// kubectl is bound to contextID's cluster (the shown one for ""). The dock opens with it.
func openTerminalTab(s *engine.Session, contextID string) (engine.DockTab, error) {
	b, err := streamableOn(s, contextID, "terminals")
	if err != nil {
		return engine.DockTab{}, err
	}
	id := s.NewDockID(engine.DockTerminal)
	tab := engine.DockTab{
		ID: id, Type: engine.DockTerminal, Title: "Terminal " + strings.TrimPrefix(id, string(engine.DockTerminal)+"-"),
		Context: b.Context,
	}
	s.UpdateUI(func(u *engine.UIState) {
		u.Dock = append(u.Dock, tab)
		u.ActiveDock = tab.ID
	})
	return tab, nil
}
