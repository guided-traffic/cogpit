package engine

import (
	"context"
	"errors"
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Each view keeps its checked rows (N111, the owner's decision in the audit of 2026-10-16; until
// then a view that ui.navigate or ui.openView changed to started without checked rows). The
// selection the window shows, ui.state lists and every action on a selection takes stays the shown
// view's (UIState.Selected), so the selection bar's Delete never acts on rows the user does not
// see. What changed is where a view's selection goes when the view is left: the tab that showed it
// remembers it and gives it back when it shows that view again (ui.openView, ui.tab.open, a link,
// ui.navigate, back and forward in the tab's history).
//
// The lead's decisions on the owner's choice (2026-10-16):
//   - per tab, as the tab bar's model has it (tabs.go: a tab keeps its own namespace filter,
//     search, sort, selection and drawer): two tabs showing the same view keep a selection each, a
//     new tab starts with none; and per cluster within a tab, since the one preview tab showed the
//     views of every connected cluster (D-69) until each cluster got its own (D-117): the key
//     keeps the cluster, though a tab's cluster no longer changes;
//   - ui.state does not list the remembered selections, only each tab's shown view's (selected):
//     an agent reads one back by showing its view again, and ui.state stays compact;
//   - ui.openView of the view the active tab shows keeps its selection (it cleared it before);
//   - a remembered selection ends with its tab (ui.tab.close), with its cluster's disconnect or
//     removal from Cogpit, and with the session: the workspace file keeps none, as it keeps no
//     tab's selection;
//   - a selection shown again is checked against the cache, the objects deleted meanwhile leave it
//     (Session.dropGoneSelected): a remembered selection never names an object that is gone.

// viewOf names a view of a cluster: the key of a remembered selection.
type viewOf struct{ context, view string }

// switchSelection moves the tab's selection to the view of the cluster it is about to show: the
// selection of the view it shows is remembered, and the one it remembers for the new view (none
// for a view it never left with checked rows) becomes its selection. The caller then sets the
// tab's Context and View.
func (t *ViewTab) switchSelection(contextID, view string) {
	if contextID == t.Context && view == t.View {
		return
	}
	if left := (viewOf{t.Context, t.View}); t.View != "" && len(t.Selected) > 0 {
		if t.selections == nil {
			t.selections = make(map[viewOf][]model.ObjectRef)
		}
		t.selections[left] = t.Selected
	} else {
		delete(t.selections, left)
	}
	shown := viewOf{contextID, view}
	t.Selected = t.selections[shown]
	delete(t.selections, shown)
}

// forgetSelections drops every tab's remembered selections of the clusters gone reports: a
// cluster disconnected or removed from Cogpit (closeTabsOf, dropContexts).
func (u *UIState) forgetSelections(gone func(string) bool) {
	for i := range u.Tabs {
		for k := range u.Tabs[i].selections {
			if gone(k.context) {
				delete(u.Tabs[i].selections, k)
			}
		}
	}
}

// selectionCheckWait bounds how long the check of a selection shown again waits for the lists of
// its objects' kinds: the objects of a kind not listed by then stay selected.
const selectionCheckWait = 2 * time.Second

// DropGoneSelected checks the selection st shows against the cache after a change that may show a
// remembered one (a view shown again, a tab activated, a cluster shown) and drops the objects the
// cache no longer holds, so that a selection never names an object deleted while it was not
// shown. It waits for a kind's list at most selectionCheckWait; an object the cache cannot answer
// for (its kind not listed by then, or refused) stays. It returns the new state, or st when
// nothing left the selection or the window shows another view by now.
func (s *Session) DropGoneSelected(ctx context.Context, st UIState) UIState {
	if len(st.Selected) == 0 {
		return st
	}
	wctx, cancel := context.WithTimeout(ctx, selectionCheckWait)
	defer cancel()
	var gone []model.ObjectRef
	unanswered := map[[2]string]bool{} // a kind in a namespace the cache could not read: not asked again
	for _, r := range st.Selected {
		where := [2]string{r.Kind, r.Namespace}
		if unanswered[where] {
			continue
		}
		ref := r
		if ref.Context == "" {
			ref.Context = st.Context // a selection's object without a context is the shown cluster's
		}
		switch _, err := s.Object(wctx, ref); {
		case err == nil:
		case errors.Is(err, ErrNotFound):
			gone = append(gone, r)
		default:
			unanswered[where] = true
		}
	}
	if len(gone) == 0 {
		return st
	}
	return s.UpdateUI(func(u *UIState) {
		if u.Context == st.Context && u.ActiveTab == st.ActiveTab && u.View == st.View {
			u.Selected = ApplySelection(u.Selected, gone, SelectRemove)
		}
	})
}
