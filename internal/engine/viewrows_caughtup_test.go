package engine

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/model"
)

// TestCaughtUpFollowsTheColumns: a snapshot has caught up with the UI state only once it shows the
// view's columns as the state arranges and hides them, not only its filters and sort; view.rows
// waits for it (a snapshot taken before ui.setColumnOrder or ui.setColumns came back with the
// kind's columns: CI). A column the cluster's settings hide counts as hidden.
func TestCaughtUpFollowsTheColumns(t *testing.T) {
	cols := []model.Column{{ID: "name"}, {ID: "namespace"}, {ID: "node"}, {ID: "cpu"}}
	u := UIState{View: "pods"}
	old := &TableSnapshot{View: "pods", Columns: cols}
	if !old.CaughtUp(u) {
		t.Fatal("a snapshot with the kind's columns has caught up with a state that arranges none")
	}

	u.ColumnOrder = map[string][]string{"pods": {"node", "name"}}
	u.HiddenColumns = map[string][]string{"pods": {"namespace"}}
	if old.CaughtUp(u) {
		t.Error("a snapshot in the kind's order caught up with a state that reorders the columns")
	}
	ordered := &TableSnapshot{View: "pods", Columns: cols, Order: ColumnOrder(cols, u.ColumnOrder["pods"])}
	if ordered.CaughtUp(u) {
		t.Error("a snapshot that shows the namespace caught up with a state that hides it")
	}
	shown := ColumnsShown(cols, u.HiddenColumns["pods"], true)
	done := &TableSnapshot{View: "pods", Columns: shown, Order: ordered.Order}
	if !done.CaughtUp(u) {
		t.Error("a snapshot with the state's order and hidden columns has not caught up")
	}

	metrics := ColumnsShown(cols, []string{"namespace", "cpu"}, true)
	withMetrics := &TableSnapshot{View: "pods", Columns: metrics, Order: ordered.Order, MetricsHidden: []string{"cpu"}}
	if !withMetrics.CaughtUp(u) {
		t.Error("a column the cluster's settings hide made the snapshot fall behind")
	}
}
