package fyneui

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Dispatches in order run one after the other in the order they were made: the second waits for
// the first to finish, so a setting changed twice in quick succession ends with the later value
// (CI: TestHideMetricsFromTheSettings saw the older write land last).
func TestDispatchInOrderKeepsTheOrder(t *testing.T) {
	f := newFakeEngine(t)
	gate := make(chan struct{})
	got := make(chan int, 2)
	f.handlers["test.write"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p struct{ N int }
		_ = json.Unmarshal(inv.Params, &p)
		if p.N == 1 {
			<-gate
		}
		got <- p.N
		return nil, nil
	}
	sh, _, _ := newTestShell(t, f, Options{})
	sh.dispatchInOrder("test.write", map[string]int{"n": 1}, nil)
	sh.dispatchInOrder("test.write", map[string]int{"n": 2}, nil)
	select {
	case n := <-got:
		t.Fatalf("write %d ran while the first waited", n)
	case <-time.After(50 * time.Millisecond): // nothing may run: the first holds the gate
	}
	close(gate)
	for want := 1; want <= 2; want++ {
		select {
		case n := <-got:
			if n != want {
				t.Fatalf("write %d ran, want %d", n, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("write %d never ran", want)
		}
	}
}
