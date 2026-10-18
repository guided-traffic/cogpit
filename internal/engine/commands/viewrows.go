package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// view.rows (G17, docs/parity/beyond-lens.md): what the user sees in the active tab's view, for
// agents. It reads the view's snapshot, never the API server, and waits briefly for the snapshot to
// follow a filter or sort the caller just set.

type viewRowsParams struct {
	Offset int `json:"offset,omitempty" jsonschema:"index of the first row to return, from 0 (the table's first row); default 0"`
	Limit  int `json:"limit,omitempty" jsonschema:"rows to return; default 100, at most 500"`
}

// viewRowsWait bounds how long view.rows waits for the view's snapshot to follow its filters and columns.
const viewRowsWait = 2 * time.Second

func registerViewRows(g *registrar) {
	add(g, engine.Spec[viewRowsParams]{
		ID: "view.rows", Label: "Read View Rows", Danger: engine.DangerRead,
		Description: "Returns the rows the user sees in the active tab's view, as the table shows them: the namespace filter, " +
			"the search (with its match case and regular expression toggles), the status filter (states) and the sort applied, the visible columns in the " +
			"order the user arranged them, each cell's text as drawn (ages relative to now), each row's ref and status, its tips " +
			"(the tooltips of the cells that have one, by column id: a node's kubelet, its upstream support and version skew), and " +
			"selected for the rows of the selection; a page of them (offset, limit: default 100, at most 500). shown counts " +
			"every row the view shows, total the objects before filtering. Secrets show what their view shows (key names, " +
			"types), never a value. Reads the view's snapshot, never the API server; resources_list lists a kind regardless " +
			"of the view. stateChoices are the states the rows carry that the namespace filter and the search keep, with " +
			"their counts and the column they come from: what ui_setStatusFilter takes.",
		Enabled: func(s *engine.Session, _ viewRowsParams) error { _, err := openView(s); return err },
		Run: func(ctx context.Context, s *engine.Session, p viewRowsParams) (any, error) {
			limit, err := engine.CheckViewPage(p.Offset, p.Limit)
			if err != nil {
				return nil, err
			}
			view, _, err := openViewKey(s)
			if err != nil {
				return nil, err
			}
			t, u, err := caughtUpTable(ctx, s, view)
			if err != nil {
				return nil, err
			}
			return engine.RowsIn(t, u.Selected, p.Offset, limit, s.Now(), s.TimeFormat()), nil
		},
	})
}

// caughtUpTable returns the open view's snapshot once it shows what the UI state asks (its
// namespace filter, search, status filter, sort and columns) and its kind's first list is in, waiting at most
// viewRowsWait; then the snapshot it has, which says what it was filtered with and whether it is
// synced.
func caughtUpTable(ctx context.Context, s *engine.Session, view string) (*engine.TableSnapshot, engine.UIState, error) {
	wctx, cancel := context.WithTimeout(ctx, viewRowsWait)
	defer cancel()
	for {
		t, u := s.Table(view), s.UIState()
		if u.View != view {
			return nil, u, fmt.Errorf("the open view changed to %s; read again", u.View)
		}
		if t.CaughtUp(u) && t.Synced { // a view just opened has no rows before its first list
			return t, u, nil
		}
		select {
		case <-wctx.Done():
			if t == nil {
				return nil, u, fmt.Errorf("the %s view has no rows yet", view)
			}
			return t, u, nil // the rows say which filter they reflect
		case <-time.After(20 * time.Millisecond):
		}
	}
}
