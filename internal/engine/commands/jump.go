package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// ui.jump (engine/jump.go): the palette's jump, k9s's `:pod kube-system /web @prod`, as one command,
// so that a jump entry dispatches one command and an agent jumps alike. It goes the ways the
// commands it stands for go: the cluster as cluster.connect shows or connects it, the namespace
// filter as ui.setNamespace sets it, the view as ui.openView opens it (its preview tab, the drawer,
// search and status filter cleared), the search as ui.setFilter sets it.

func registerJump(g *registrar) {
	add(g, engine.Spec[engine.JumpParams]{
		ID: engine.JumpCommand, Label: "Jump to View", Danger: engine.DangerUI, Verbs: cacheReads,
		Description: "Opens a view the way k9s's command line does (:pod kube-system /web @prod), as the command palette's " +
			"jump does when the user types a text that starts with \":\": view names it by its kind key or page, or by a " +
			"name k9s users type (a kind's plural, singular, Kind or short name such as po, deploy, svc; pulses, " +
			"workloads, helm, pf), case-insensitive. context first shows that cluster, connecting it as cluster_connect " +
			"does when it is not connected (a context whose credential plugin the user has not allowed fails, as there); " +
			"namespace then sets the namespace filter to that one namespace (all: every namespace; omitted: unchanged); " +
			"the view opens as ui_openView opens it, in the cluster's preview tab, with filter as its search text. " +
			"Reads only the cache. Returns the new UI state.",
		Enabled: jumpEnabled,
		Run:     jump,
	})
}

// jumpEnabled checks the view and the namespace on the cluster the jump goes to, when it is
// connected; a cluster that is not connected yet is checked once the jump connected it.
func jumpEnabled(s *engine.Session, p engine.JumpParams) error {
	if strings.TrimSpace(p.View) == "" {
		return fmt.Errorf("%w: no view named", engine.ErrUnknownKind)
	}
	if p.Context != "" && s.BackendOf(p.Context) == nil {
		return nil
	}
	b, err := connectedOn(s, p.Context)
	if err != nil {
		return err
	}
	_, err = b.ResolveJump(p)
	return err
}

func jump(ctx context.Context, s *engine.Session, p engine.JumpParams) (any, error) {
	if p.Context != "" {
		if err := jumpTo(ctx, s, p.Context); err != nil {
			return nil, err
		}
	}
	b, err := connected(s)
	if err != nil {
		return nil, err
	}
	v, err := b.ResolveJump(p)
	if err != nil {
		return nil, err
	}
	view, err := viewKey(s, v.View)
	if err != nil {
		return nil, err
	}
	switch ns := strings.TrimSpace(p.Namespace); ns {
	case "":
	case engine.JumpAllNamespaces:
		s.UpdateUI(func(u *engine.UIState) { u.Namespaces = nil })
	default:
		s.UpdateUI(func(u *engine.UIState) { u.Namespaces = []string{ns} })
	}
	st := s.OpenTab(view, false)
	if p.Filter != "" {
		st = s.UpdateUI(func(u *engine.UIState) {
			if u.View == view {
				u.Filter = p.Filter
			}
		})
	}
	return st, nil
}

// jumpTo shows a context's cluster: one connected already only shown, another connected first, as
// cluster.connect does. A connect that goes on in the background, was cancelled or superseded is
// its error (engine.ConnectNotFailed); one that failed says why.
func jumpTo(ctx context.Context, s *engine.Session, contextID string) error {
	if u := s.UIState(); u.Context == contextID && u.Connection.State.Live() {
		return nil
	}
	err := s.Connect(ctx, contextID)
	if engine.ConnectNotFailed(err) {
		return err
	}
	if u := s.UIState(); !u.Connection.State.Live() {
		cause := u.Connection.Error
		if cause == "" && err != nil {
			cause = err.Error()
		}
		return fmt.Errorf("could not connect to %s: %s", engine.ContextLabel(contextID), cause)
	}
	return err
}
