package kinds

import (
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
)

func TestStateCell(t *testing.T) {
	if c := stateCell(model.StatusNone, "ignored"); c != (model.Cell{}) {
		t.Errorf("no signal: got %+v, want an empty cell", c)
	}
	c := stateCell(model.StatusWarning, "3 restarts")
	if c.Text != "3 restarts" || c.Status != model.StatusWarning || c.Link != nil || c.Signals == nil || len(*c.Signals) != 1 {
		t.Fatalf("warning: got %+v", c)
	}
	if s := (*c.Signals)[0]; s.Source != model.SignalState || s.Slot != 0 || s.Text != "3 restarts" || s.Status != model.StatusWarning {
		t.Errorf("the state's signal %+v", s)
	}
	if e := stateCell(model.StatusError, "CrashLoopBackOff"); e.Num <= c.Num {
		t.Errorf("an error sorts below a warning: %d, %d", e.Num, c.Num)
	}
	if signalsColumn.Type != model.ColumnSignals || signalsColumn.ID != "signals" || signalsColumn.Title != "Signals" || signalsColumn.Slots != 1 {
		t.Errorf("signalsColumn = %+v", signalsColumn)
	}
	if got := model.CellText(signalsColumn, c, time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)); got != "3 restarts" {
		t.Errorf("CellText = %q, want the reason", got)
	}
}

func TestNamespaceCellLinks(t *testing.T) {
	obj := map[string]any{"metadata": map[string]any{"name": "web", "namespace": "shop"}}
	c := namespaceCell(obj)
	want := model.ObjectRef{Kind: "namespaces", Name: "shop"}
	if c.Text != "shop" || c.Link == nil || *c.Link != want {
		t.Errorf("namespaceCell = %+v, link %v; want text shop and link %+v", c, c.Link, want)
	}
	if c := namespaceCell(map[string]any{}); c.Link != nil || c.Text != "" {
		t.Errorf("cluster-scoped object: got %+v, want no text and no link", c)
	}
	if l := linkTo("nodes", "", ""); l != nil {
		t.Errorf("linkTo without a name = %+v, want nil", l)
	}
}
