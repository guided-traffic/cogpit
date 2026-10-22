package engine

import (
	"context"
	"errors"
	"fmt"
)

// Restoring a draft (drafts.go): the tab it was kept for opens again with its text, on the
// connected context only. A Create resource tab opens with the text. An Edit tab opens on the
// object as the cache holds it, with the draft's text and based on the version the draft was based
// on, so that an apply after a change on the cluster meanwhile is refused with a conflict and shows
// that change first (editor.go). The tab adopts the draft: it is that tab's draft from now on.

// RestoreDraft opens the tab of an offered draft with its text and returns the tab's state.
func (s *Session) RestoreDraft(ctx context.Context, id string) (EditorState, error) {
	d := s.opts.Drafts
	if d == nil {
		return EditorState{}, ErrNoDrafts
	}
	tabID, err := s.restoreDraft(ctx, d, id)
	if err != nil {
		return EditorState{}, err
	}
	d.changed("")
	return s.EditorState(ctx, tabID)
}

func (s *Session) restoreDraft(ctx context.Context, d *Drafts, id string) (string, error) {
	d.wmu.Lock()
	defer d.wmu.Unlock()
	dr, err := d.offeredDraft(id)
	if err != nil {
		return "", err
	}
	b := s.BackendOf(dr.Context) // the draft's cluster, shown or not (D-69)
	if b == nil || dr.Context == "" {
		return "", fmt.Errorf("%w: the draft of %s belongs to context %q; connect to it to restore the draft", ErrNotConnected, dr.what(), dr.Context)
	}
	var tabID string
	switch dr.Type {
	case DockCreate:
		st, err := s.OpenCreateTab(dr.Context, dr.Text)
		if err != nil {
			return "", err
		}
		tabID = st.Tab.ID
		s.edits.mu.Lock()
		if e, ok := s.edits.m[tabID]; ok {
			e.startText = "" // the restored text is unsaved work, not the text the tab opened with
		}
		s.edits.mu.Unlock()
	case DockEdit:
		if tabID, err = s.restoreEditTab(ctx, b, dr); err != nil {
			return "", err
		}
	default: // a drawer's values: RestoreConfigMapDraft (drafts_drawer.go)
		return "", fmt.Errorf("the draft %s holds %s, which the drawer restores, not an editor tab", id, dr.kept())
	}
	d.adopt(s, tabID, dr)
	return tabID, nil
}

// restoreEditTab opens an Edit tab on a draft's object with the draft's text and base version, or
// gives the draft to the Edit tab already open on it when that tab holds no unsaved text.
func (s *Session) restoreEditTab(ctx context.Context, b *Backend, dr Draft) (string, error) {
	t, b2, err := s.ResolveTarget(ctx, *dr.Ref)
	if err != nil {
		return "", err
	}
	if b2 != b {
		return "", fmt.Errorf("%w: the draft's cluster was connected anew", ErrWrongCluster)
	}
	if RedactedKind(t.APIVersion, t.Kind) { // never kept (D-19); read refuses such a file anyway
		return "", ErrSecretGUIOnly
	}
	if _, err := b.ObjectWriter(); err != nil {
		return "", err
	}
	obj, err := cachedObject(ctx, b, t)
	if errors.Is(err, ErrNotFound) {
		return "", fmt.Errorf("%s no longer exists on the cluster; the draft is kept until you discard it: %w", t.Ref.String(), err)
	}
	if err != nil {
		return "", err
	}
	obj = withTypeMeta(obj, t)
	baseRV, baseUID, baseText := dr.BaseResourceVersion, dr.BaseUID, ""
	if baseRV == "" || baseRV == obj.GetResourceVersion() {
		baseRV, baseUID = obj.GetResourceVersion(), string(obj.GetUID())
		if baseText, err = EditableYAML(obj); err != nil {
			return "", err
		}
	}
	// A draft's object is based on a version the cache may no longer hold: the base text is then
	// unknown, and the restored text counts as unsaved until it is applied or reverted.

	for _, x := range s.UIState().Dock {
		if x.Type != DockEdit || x.Ref == nil || !x.Ref.Same(t.Ref) || x.Context != t.Ref.Context {
			continue
		}
		e, err := s.editorOf(x.ID)
		if err != nil {
			return "", err
		}
		if e.text != e.baseText {
			return "", fmt.Errorf("the Edit tab on %s has unsaved changes of its own; apply or close it, then restore the draft", t.Ref.String())
		}
		if _, err := s.updateEditor(x.ID, func(e *editor) {
			e.text, e.baseRV, e.baseUID, e.baseText = dr.Text, baseRV, baseUID, baseText
			e.undo = nil
			e.rev++
		}); err != nil {
			return "", err
		}
		s.UpdateUI(func(u *UIState) { u.ShowDockTab(x.ID) })
		return x.ID, nil
	}

	r := t.Ref
	e := &editor{
		tab: DockTab{
			ID: s.NewDockID(DockEdit), Type: DockEdit, Title: t.Kind + " " + t.Ref.Name,
			Context: b.Context, Ref: &r,
		},
		text: dr.Text, rev: 1, target: t,
		baseRV: baseRV, baseUID: baseUID, baseText: baseText,
	}
	s.putEditor(e)
	created := true
	ui := s.UpdateUI(func(u *UIState) {
		for _, x := range u.Dock {
			if x.Type == DockEdit && x.Ref != nil && x.Ref.Same(t.Ref) && x.Context == t.Ref.Context {
				created = false
				return
			}
		}
		u.Dock = append(u.Dock, e.tab)
		u.ActiveDock = e.tab.ID
	})
	s.settleEditor(e.tab.ID, created, ui.Revision)
	if !created {
		return "", fmt.Errorf("an Edit tab on %s opened meanwhile; restore the draft again", t.Ref.String())
	}
	return e.tab.ID, nil
}
