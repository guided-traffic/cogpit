package commands

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// ui.sortDrawerTable: the order of a sortable table in the detail drawer (owner, 2026-10-13: a
// node's Pods sort by Name, Namespace, CPU, Memory and Status; a workload's Pods since D-40, under
// the same title and so the same order; engine/drawersort.go). The order is
// UI state the engine holds (UIState.DrawerSort), so a click on a column's title in the window and
// an agent set the same thing, and ui_state shows it.

type drawerSortParams struct {
	Section string `json:"section,omitempty" jsonschema:"the title of the drawer's table to sort, as object_get's sections name it: Pods (a node's or a workload's pods), Referenced By (what points at the object, with back references on); omitted: the drawer's only sortable table"`
	Column  string `json:"column" jsonschema:"the column to sort by: its id as the table's ids name it (name, node, namespace, ready, cpu, memory, status, ...) or its title"`
	Desc    bool   `json:"desc,omitempty" jsonschema:"descending order"`
}

func registerDrawerTables(g *registrar) {
	add(g, engine.Spec[drawerSortParams]{
		ID: "ui.sortDrawerTable", Label: "Sort Drawer Table", Danger: engine.DangerUI, Verbs: cacheReads,
		Description: "Sorts a table in the detail drawer by a column, as a click on the column's title does: the Pods " +
			"of a node, Deployment, Replica Set, Stateful Set, Daemon Set, Job or Cron Job by name, node, namespace, " +
			"ready, cpu, memory, status or any other of its columns, ascending unless desc is true. Numbers and metrics " +
			"sort by value, a pod without a measurement last in both directions. The drawer must show the table " +
			"(ui_navigate opens an object's drawer). The order is kept by the table's title for the session, so the next " +
			"object's table of the same title comes sorted the same way; a table nobody sorted is sorted by name, " +
			"Referenced By by kind. " +
			fmt.Sprintf("A table of more than %[1]d rows shows the first %[1]d in that order and counts the rest (omitted). ", engine.MaxDrawerTableRows) +
			"ui_state shows the orders (drawerSort), object_get the rows in that order and the table's sort. Returns " +
			"the new UI state.",
		Enabled: func(s *engine.Session, _ drawerSortParams) error {
			if _, err := connected(s); err != nil {
				return err
			}
			if s.UIState().Drawer == nil {
				return errDrawerClosed
			}
			return nil
		},
		Run: func(_ context.Context, s *engine.Session, p drawerSortParams) (any, error) {
			ref := s.UIState().Drawer
			if ref == nil {
				return nil, errDrawerClosed
			}
			m, err := s.Detail(*ref)
			if err != nil {
				return nil, err
			}
			title, t, err := sortableTable(m, p.Section)
			if err != nil {
				return nil, err
			}
			id, err := tableColumn(t, title, p.Column)
			if err != nil {
				return nil, err
			}
			return s.UpdateUI(func(u *engine.UIState) {
				sorts := maps.Clone(u.DrawerSort)
				if sorts == nil {
					sorts = map[string]engine.SortSpec{}
				}
				sorts[title] = engine.SortSpec{Column: id, Desc: p.Desc}
				u.DrawerSort = sorts
			}), nil
		},
	})
}

// errDrawerClosed refuses a drawer command while the drawer is closed.
var errDrawerClosed = errors.New("the detail drawer is closed; ui_navigate opens an object's drawer")

// sortableTable finds the drawer model's sortable table titled section (case-insensitively), or its
// only one when section is empty.
func sortableTable(m *engine.DetailModel, section string) (string, *model.DetailTable, error) {
	var titles []string
	for _, sec := range m.Sections {
		if sec.Table == nil || !sec.Table.Sortable {
			continue
		}
		titles = append(titles, sec.Title)
		if section != "" && strings.EqualFold(sec.Title, section) {
			return sec.Title, sec.Table, nil
		}
	}
	if len(titles) == 0 {
		return "", nil, fmt.Errorf("%w: the drawer of %s shows no sortable table", engine.ErrNotApplicable, m.Ref.String())
	}
	if section == "" && len(titles) == 1 {
		for _, sec := range m.Sections {
			if sec.Title == titles[0] && sec.Table != nil && sec.Table.Sortable {
				return sec.Title, sec.Table, nil
			}
		}
	}
	if section == "" {
		return "", nil, fmt.Errorf("the drawer of %s shows several sortable tables; name one: %s", m.Ref.String(), strings.Join(quoted(titles), ", "))
	}
	return "", nil, fmt.Errorf("the drawer of %s shows no sortable table %q; its sortable tables: %s", m.Ref.String(), section, strings.Join(quoted(titles), ", "))
}

// tableColumn returns the id of a sortable table's column named by its id or its title.
func tableColumn(t *model.DetailTable, title, column string) (string, error) {
	if t.ColumnIndex(column) >= 0 {
		return column, nil
	}
	for i, c := range t.Columns {
		if i < len(t.IDs) && strings.EqualFold(c, column) {
			return t.IDs[i], nil
		}
	}
	for _, id := range t.IDs {
		if strings.EqualFold(id, column) {
			return id, nil
		}
	}
	return "", fmt.Errorf("the drawer's %s table has no column %q; columns: %s", title, column, strings.Join(t.IDs, ", "))
}
