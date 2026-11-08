package commands

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The selection (ui-parity › Tables, G5, G7, G9): ui.select's modes (ui.go, selectObjects) and the
// actions on a selection, which take its objects in "refs" (engine/selection.go): Delete here,
// Restart (restartmany.go), the Flux actions (flux.go) and Argo CD's Sync and Refresh (argocd.go);
// more than engine.DirectSelection objects run as a background job (manyjob.go, G4). The other
// actions stay on one object (Cogpit's default, inferred, 2026-10-13: each has its own dialog or
// confirmation per object, and none is as common on many objects at once).

type selectionParams struct {
	Refs []model.ObjectRef `json:"refs" jsonschema:"the objects, as ui_state's selected lists them: objects of any kind of one connected cluster (their refs' context; the shown cluster without one), at most 500"`
}

// DeleteManyResult is object.deleteMany's result when every object was deleted.
type DeleteManyResult struct {
	Deleted []model.ObjectRef `json:"deleted"`
}

// DeleteFailure is one object a delete of a selection did not delete, and why.
type DeleteFailure struct {
	Ref   model.ObjectRef `json:"ref"`
	Error string          `json:"error"`
}

// DeleteManyError is object.deleteMany's error when it did not delete every object: which it
// deleted and which it did not, each with its reason. Nothing is retried.
type DeleteManyError struct {
	Deleted []model.ObjectRef
	Failed  []DeleteFailure
}

func (e *DeleteManyError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "deleted %d of %d objects", len(e.Deleted), len(e.Deleted)+len(e.Failed))
	if len(e.Deleted) > 0 {
		b.WriteString(" (")
		b.WriteString(refList(e.Deleted))
		b.WriteString(")")
	}
	b.WriteString("; not deleted: ")
	for i, f := range e.Failed {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(f.Ref.String())
		b.WriteString(": ")
		b.WriteString(f.Error)
	}
	b.WriteString(". Nothing was retried.")
	return b.String()
}

// refList names objects: "configmaps/shop/a, configmaps/shop/b".
func refList(refs []model.ObjectRef) string {
	names := make([]string, len(refs))
	for i, r := range refs {
		names[i] = r.String()
	}
	return strings.Join(names, ", ")
}

// errNotAttempted is a selection's object a delete stopped before: the cluster changed or the
// call was cancelled.
var errNotAttempted = errors.New("not attempted")

func registerSelection(g *registrar) {
	add(g, engine.Spec[selectionParams]{
		ID: "object.deleteMany", Label: labelDelete, MenuPath: "Object/Delete Selected", Order: engine.OrderDelete, RowKeys: deleteKeys,
		Danger: engine.DangerDestructive, Verbs: []engine.Verb{{Verb: "delete", Resource: "*"}},
		Description: "Deletes several objects of any kinds at once, as Delete on the table's selection does: one " +
			"confirmation names every object (the message the first ten and the count), their namespaces and the " +
			"context, for every origin; then each object is deleted with its default grace period and the UID Cogpit's " +
			"cache held for it when the user confirmed as a precondition, one after the other. At most 500 objects. " +
			"When an object is not deleted the others still are; nothing is retried. Deleted objects leave the selection. " +
			jobManyDescription + " Always asks the user to confirm.",
		Enabled: func(s *engine.Session, p selectionParams) error {
			if len(p.Refs) == 0 {
				return errors.New("no objects are selected")
			}
			if len(p.Refs) > engine.MaxSelection {
				return fmt.Errorf("at most %d objects at once, %d are selected", engine.MaxSelection, len(p.Refs))
			}
			for _, r := range p.Refs {
				if err := cachedAny(s, r); err != nil {
					return fmt.Errorf("%s: %w", r.String(), err)
				}
			}
			return nil
		},
		Targets: func(s *engine.Session, p selectionParams) ([]model.ObjectRef, error) {
			return selectionTargets(s, p.Refs)
		},
		Run: deleteMany,
	})
}

// selectionTargets resolves a selection's objects for the confirmation, each once, in order.
func selectionTargets(s *engine.Session, refs []model.ObjectRef) ([]model.ObjectRef, error) {
	out := make([]model.ObjectRef, 0, len(refs))
	for _, r := range refs {
		t, err := objectTarget(s, r)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", r.String(), err)
		}
		if !slices.ContainsFunc(out, t[0].Same) {
			out = append(out, t[0])
		}
	}
	return out, nil
}

// deleteMany implements object.deleteMany: the objects the user confirmed, one after the other,
// each bound to its confirmed UID (Session.DeleteObject), every failure kept for the report; more
// than engine.DirectSelection as a job (manyjob.go). A crash in the middle leaves some of the
// objects deleted and the others not, each wholly one or the other (one request each): the list
// shows which are left, and Delete of those finishes what the user asked for; nothing needs
// repairing (D-11).
func deleteMany(ctx context.Context, s *engine.Session, p selectionParams) (any, error) {
	b, err := writableOn(s, refsContext(p.Refs)) // the selection's cluster, shown or not (D-69)
	if err != nil {
		return nil, err
	}
	if err := engine.CheckConfirmedContext(ctx, b.Context); err != nil {
		return nil, err
	}
	targets, err := selectionTargets(s, p.Refs)
	if err != nil {
		return nil, err
	}
	if err := engine.CheckConfirmedTargets(ctx, targets); err != nil {
		return nil, err
	}
	a := manyAction{
		id: "object.deleteMany", label: labelDelete, done: "deleted", noun: "objects", b: b, targets: targets,
		do: func(ctx context.Context, ref model.ObjectRef) (model.ObjectRef, error) {
			res, err := s.DeleteObject(ctx, ref)
			return res.Ref, err
		},
		after: func(deleted []model.ObjectRef) {
			if len(deleted) > 0 {
				s.UpdateUI(func(u *engine.UIState) {
					u.Selected = engine.ApplySelection(u.Selected, deleted, engine.SelectRemove)
				})
			}
		},
	}
	return runMany(ctx, s, a, func(out manyOutcome) (any, error) {
		if len(out.failed) > 0 { // the caller shows it, as object.delete's errors: the GUI in a toast
			return nil, &DeleteManyError{Deleted: out.done, Failed: out.failed}
		}
		s.Emit(engine.Event{Type: engine.EventToast, Level: engine.LevelInfo, Message: fmt.Sprintf("%d objects deleted", len(out.done))})
		return DeleteManyResult{Deleted: out.done}, nil
	})
}

// selectObjects implements ui.select.
func selectObjects(ctx context.Context, s *engine.Session, p selectParams) (any, error) {
	view, prof, err := openViewKey(s)
	if err != nil {
		return nil, err
	}
	mode, err := engine.ParseSelectMode(p.Mode)
	if err != nil {
		return nil, err
	}
	if mode == engine.SelectOpen && (p.All || len(p.Refs) != 1 || p.NoDrawer) {
		return nil, errors.New("mode open opens the drawer on one object: give exactly one in refs, without all and noDrawer")
	}
	var refs []model.ObjectRef
	if p.All {
		if len(p.Refs) > 0 {
			return nil, errors.New("all takes every row the view shows: leave refs out")
		}
		if refs, err = shownRows(ctx, s, view); err != nil {
			return nil, err
		}
	} else {
		refs = make([]model.ObjectRef, 0, len(p.Refs))
		for _, r := range p.Refs {
			if r.Kind != prof.Key && !engine.MixedKinds(view) {
				return nil, fmt.Errorf("%s is not of the open view's kind %s; use ui_navigate", r.String(), prof.Key)
			}
			rr, _, err := s.ResolveRef(r)
			if err != nil {
				return nil, err
			}
			refs = append(refs, rr)
		}
	}
	applied := false
	st := s.UpdateUI(func(u *engine.UIState) {
		if u.View != view {
			return
		}
		applied = true
		if mode == engine.SelectOpen { // a plain click on a row (A73): the checked rows stay checked
			d := refs[0]
			u.Drawer = &d
			return
		}
		u.Selected = engine.ApplySelection(u.Selected, refs, mode)
		if mode != engine.SelectReplace || p.NoDrawer {
			return
		}
		if len(u.Selected) == 0 {
			u.Drawer = nil
			return
		}
		d := u.Selected[0]
		u.Drawer = &d
	})
	if !applied {
		return nil, errors.New("the open view changed; select again")
	}
	return st, nil
}

// shownWait bounds how long ui.select with all waits for the view's rows to follow its filters.
const shownWait = 2 * time.Second

// shownRows returns the objects of the rows the open view shows, from its snapshot once that has
// caught up with the namespace filter, the search and the status filter (snapshots follow the UI state within a
// tenth of a second; an agent may select all right after setting the search).
func shownRows(ctx context.Context, s *engine.Session, view string) ([]model.ObjectRef, error) {
	wctx, cancel := context.WithTimeout(ctx, shownWait)
	defer cancel()
	for {
		t, u := s.Table(view), s.UIState()
		if t != nil && t.Filter == u.Filter && t.FilterOptions == u.FilterOptions && slices.Equal(t.Namespaces, u.Namespaces) &&
			slices.Equal(t.States, u.States) {
			refs := make([]model.ObjectRef, len(t.Rows))
			for i := range t.Rows {
				refs[i] = t.Rows[i].Ref
			}
			return refs, nil
		}
		select {
		case <-wctx.Done():
			if t == nil {
				return nil, fmt.Errorf("the %s view has no rows yet", view)
			}
			return nil, fmt.Errorf("the %s view's rows have not caught up with its filters yet; try again", view)
		case <-time.After(20 * time.Millisecond):
		}
	}
}
