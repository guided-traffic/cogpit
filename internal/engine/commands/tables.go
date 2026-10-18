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

// The table commands (ui-parity › Tables): which columns a view shows and the Definitions view's
// group filter. Both are UI state, so agents see them in ui_state and set them as the user does.

type columnsParams struct {
	View   string   `json:"view,omitempty" jsonschema:"kind key of the view; empty or omitted: the open view"`
	Hidden []string `json:"hidden,omitempty" jsonschema:"ids of the columns to hide (kinds_list lists a kind's columns, hidden: true for those hidden by default); every other column is shown, and at least one must stay visible"`
	Reset  bool     `json:"reset,omitempty" jsonschema:"true: go back to the view's default columns; hidden is then ignored"`
}

type columnOrderParams struct {
	View  string   `json:"view,omitempty" jsonschema:"kind key of the view; empty or omitted: the open view"`
	Order []string `json:"order,omitempty" jsonschema:"column ids in the order to show them, left to right (kinds_list lists a kind's columns); the columns left out follow in the kind's order"`
	Reset bool     `json:"reset,omitempty" jsonschema:"true: go back to the kind's order; order is then ignored"`
}

type groupsParams struct {
	Groups []string `json:"groups,omitempty" jsonschema:"API groups the Definitions view shows (cert-manager.io); empty or omitted shows all groups"`
}

type exportParams struct {
	View string `json:"view,omitempty" jsonschema:"kind key of the view to export; it must be the open view; empty or omitted: the open view"`
}

// CSVResult is table.exportCsv's result.
type CSVResult struct {
	View string `json:"view"`
	// Filename is a file name to suggest when saving: "<view>-<context>.csv".
	Filename string `json:"filename"`
	// Rows is the number of records after the header row.
	Rows int `json:"rows"`
	// CSV is the table: a header row with the visible columns' titles, then the rows as shown.
	CSV string `json:"csv"`
}

func registerTables(g *registrar) {
	add(g, engine.Spec[columnsParams]{
		ID: "ui.setColumns", Label: "Columns", MenuPath: "View/Columns", Danger: engine.DangerUI,
		Description: "Shows and hides the columns of a view, as the ⋮ at the end of the table's header does: hidden " +
			"lists the column ids to hide, every other column is shown. Each view keeps its own columns; reset returns " +
			"a view to its default columns (some kinds hide columns by default, such as a custom resource's printer " +
			"columns of a higher priority). ui_state shows the views whose columns differ from their defaults in " +
			"hiddenColumns. Returns the new UI state.",
		Enabled: func(s *engine.Session, _ columnsParams) error { _, err := connected(s); return err },
		Run: func(_ context.Context, s *engine.Session, p columnsParams) (any, error) {
			view := p.View
			if view == "" {
				view = s.UIState().View
			}
			if view == "" {
				return nil, errors.New("no view is open; name one in view")
			}
			prof, ok := s.ViewProfile(view) // a kind's columns, or a page table's
			if !ok {
				_, _, err := s.Kind(view) // says which kinds the cluster serves
				return nil, err
			}
			prof = s.WithViewColumns(prof) // an integration's column too (D-79)
			hidden, err := hiddenColumns(prof, p.Hidden)
			if err != nil {
				return nil, err
			}
			defaults := engine.HiddenByDefault(prof.Columns)
			return s.UpdateUI(func(u *engine.UIState) {
				if p.Reset || slices.Equal(hidden, defaults) {
					delete(u.HiddenColumns, view)
					return
				}
				if u.HiddenColumns == nil {
					u.HiddenColumns = map[string][]string{}
				}
				u.HiddenColumns[view] = hidden
			}), nil
		},
	})

	add(g, engine.Spec[columnOrderParams]{
		ID: "ui.setColumnOrder", Label: "Column Order", Danger: engine.DangerUI,
		Description: "Arranges the columns of a view, as dragging a column's header does: order lists column ids " +
			"left to right, and the columns it leaves out follow in the kind's order. Each view keeps its own order " +
			"for the session, hidden columns included (ui_setColumns hides them); reset returns a view to its " +
			"kind's order, as Reset Column Order in the header's ⋮ does. ui_state shows the arranged views in " +
			"columnOrder; a view's table and its CSV export follow it. Returns the new UI state.",
		Enabled: func(s *engine.Session, _ columnOrderParams) error { _, err := connected(s); return err },
		Run: func(_ context.Context, s *engine.Session, p columnOrderParams) (any, error) {
			view := p.View
			if view == "" {
				view = s.UIState().View
			}
			if view == "" {
				return nil, errors.New("no view is open; name one in view")
			}
			prof, ok := s.ViewProfile(view)
			if !ok {
				_, _, err := s.Kind(view)
				return nil, err
			}
			prof = s.WithViewColumns(prof) // an integration's column too (D-79)
			order, err := columnOrder(prof, p.Order)
			if err != nil {
				return nil, err
			}
			return s.UpdateUI(func(u *engine.UIState) {
				if p.Reset || engine.ColumnOrder(prof.Columns, order) == nil {
					delete(u.ColumnOrder, view)
					return
				}
				if u.ColumnOrder == nil {
					u.ColumnOrder = map[string][]string{}
				}
				u.ColumnOrder[view] = order
			}), nil
		},
	})

	add(g, engine.Spec[groupsParams]{
		ID: "ui.setGroups", Label: "Select API Groups", MenuPath: "View/API Groups", Danger: engine.DangerUI,
		Description: "Sets the API group filter of the Definitions view (customresourcedefinitions), which Lens shows " +
			"in place of the namespace filter: only the definitions of these groups are listed. An empty list shows " +
			"all groups. Returns the new UI state.",
		Run: func(_ context.Context, s *engine.Session, p groupsParams) (any, error) {
			var gs []string
			for _, g := range p.Groups {
				g = strings.TrimSpace(g)
				if g != "" && !slices.Contains(gs, g) {
					gs = append(gs, g)
				}
			}
			return s.UpdateUI(func(u *engine.UIState) { u.Groups = gs }), nil
		},
	})

	add(g, engine.Spec[exportParams]{
		ID: "table.exportCsv", Label: "Download as CSV", MenuPath: "View/Download as CSV", Danger: engine.DangerRead, Verbs: cacheReads,
		Description: "Exports the open view's table as CSV text, as the download button beside the item count does: a header " +
			"row with the titles of the visible columns, then the rows as the table shows them (namespace filter, search, " +
			"status filter and sort applied; ages relative to now). Reads the view's snapshot, never the API server. Cells a spreadsheet " +
			"would evaluate as a formula start with an apostrophe. Returns {view, filename, rows, csv}.",
		Enabled: func(s *engine.Session, _ exportParams) error { _, err := openView(s); return err },
		Run: func(_ context.Context, s *engine.Session, p exportParams) (any, error) {
			u := s.UIState()
			if p.View != "" && p.View != u.View {
				return nil, fmt.Errorf("only the open view can be exported, and %s is not open; open it with ui_openView", p.View)
			}
			t := s.Table(u.View)
			if t == nil {
				return nil, fmt.Errorf("the %s view has no rows yet", u.View)
			}
			var b strings.Builder
			if err := engine.WriteCSVIn(&b, t, s.Now(), s.TimeFormat()); err != nil {
				return nil, err
			}
			cs, _ := s.ClusterSettingsOf(t.Context) // its display name, if the user gave it one
			return CSVResult{View: t.View, Filename: csvFilename(t.View, engine.ClusterName(t.Context, cs)), Rows: len(t.Rows), CSV: b.String()}, nil
		},
	})
}

// csvFilename is "<view>-<cluster>.csv", the cluster by the name the window shows (D-68: its
// display name, else the short name, never the kubeconfig file it comes from; the owner's WF19),
// with every run of characters but letters, digits, dots, dashes and underscores one underscore:
// names may hold blanks, colons and slashes (an EKS context is an ARN).
func csvFilename(view, cluster string) string {
	clean := func(s string) string {
		var b strings.Builder
		under := false
		for _, r := range s {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
				b.WriteRune(r)
				under = false
			case !under:
				b.WriteByte('_')
				under = true
			}
		}
		return strings.Trim(b.String(), "_")
	}
	name := clean(view)
	if c := clean(cluster); c != "" {
		name += "-" + c
	}
	return name + ".csv"
}

// columnOrder checks the ids against a kind's columns and returns every column id in the order
// to show them: the ids given, then the others in the kind's order. An unknown or repeated id is
// refused.
func columnOrder(prof *model.KindProfile, ids []string) ([]string, error) {
	out := make([]string, 0, len(prof.Columns))
	for _, id := range ids {
		if prof.ColumnIndex(id) < 0 {
			return nil, fmt.Errorf("the %s view has no column %q; columns: %s", prof.Key, id, columnIDs(prof))
		}
		if slices.Contains(out, id) {
			return nil, fmt.Errorf("column %q is listed twice", id)
		}
		out = append(out, id)
	}
	for _, c := range prof.Columns {
		if !slices.Contains(out, c.ID) {
			out = append(out, c.ID)
		}
	}
	return out, nil
}

// hiddenColumns checks the ids against a kind's columns and returns them in the columns' order.
// Hiding every column is refused: a table without columns shows nothing to click.
func hiddenColumns(prof *model.KindProfile, ids []string) ([]string, error) {
	var out []string
	for _, c := range prof.Columns {
		if slices.Contains(ids, c.ID) {
			out = append(out, c.ID)
		}
	}
	for _, id := range ids {
		if prof.ColumnIndex(id) < 0 {
			return nil, fmt.Errorf("the %s view has no column %q; columns: %s", prof.Key, id, columnIDs(prof))
		}
	}
	if len(out) == len(prof.Columns) {
		return nil, fmt.Errorf("at least one column of the %s view must stay visible", prof.Key)
	}
	return out, nil
}
