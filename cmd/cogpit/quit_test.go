package main

import (
	"slices"
	"testing"
)

// TestQuitHungEndsAsANormalQuit: when Fyne's teardown hangs, the quit bound's goroutine runs run's
// end as run's return would, the last added first and each once, and then exits with a normal
// quit's code 0; the main goroutine's run, should it return later, runs nothing again.
func TestQuitHungEndsAsANormalQuit(t *testing.T) {
	var got []string
	var end runEnd
	for _, name := range []string{"cpu profile", "audit log", "session", "kubeconfig watch", "MCP server"} {
		end.add(func() { got = append(got, name) })
	}
	exits := []int{}
	quitHung(nil, &end, func(code int) {
		got = append(got, "exit")
		exits = append(exits, code)
	})()
	end.run() // run's deferred end, had it returned
	want := []string{"MCP server", "kubeconfig watch", "session", "audit log", "cpu profile", "exit"}
	if !slices.Equal(got, want) || !slices.Equal(exits, []int{0}) {
		t.Errorf("ran %q with exit codes %v, want %q and [0]", got, exits, want)
	}
}
