package commands

import (
	"context"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

func registerAttach(g *registrar) {
	// Lens's pod menu (capture, 2026-10-11): Attach Pod › (a submenu of the containers) first.
	add(g, engine.Spec[containerParams]{
		ID: "pod.attach", Label: "Attach Pod", MenuPath: "Pod/Attach", Kinds: []string{kindPods}, Order: 100,
		Danger: engine.DangerMutating,
		Verbs:  []engine.Verb{{Verb: nwVerbCreate, Resource: kindPods, Subresource: subresourceAttach}},
		Description: "Opens a dock tab attached to a container's main process, as kubectl attach does: its output from " +
			"now on and, when the container was started with stdin (and a TTY), what is typed there as its input. " +
			"Anything typed reaches the process, so an agent's request waits for the user's acceptance. Closing the tab " +
			"detaches; the process goes on. Returns the tab.",
		Enabled: func(s *engine.Session, p containerParams) error {
			if _, err := streamableOn(s, p.Ref.Context, "attach sessions"); err != nil {
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
				ID: s.NewDockID(engine.DockAttach), Type: engine.DockAttach, Title: "Attach: " + ref.Name + "/" + container,
				Context: ref.Context, Ref: &ref, Container: container,
			}
			s.UpdateUI(func(u *engine.UIState) {
				u.Dock = append(u.Dock, tab)
				u.ActiveDock = tab.ID
			})
			return DockResult{Tab: tab, Created: true}, nil
		},
	})
}
