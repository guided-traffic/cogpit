package cluster

import (
	"slices"
	"testing"
	"time"
)

// neighbourBase is when the runs of TestSearchNeighbours begin: far from now, so that a run another
// test begins meanwhile is none of their neighbours and prunes none of their searches
// (newProcSearch drops only the searches that began three windows before the new one). The other
// way round, these runs prune every earlier search of the package's registry: no test of the
// package runs in parallel (t.Parallel), so none loses a search it still needs.
var neighbourBase = time.Date(2101, 1, 1, 12, 0, 0, 0, time.UTC)

// noteRun notes the search of a plugin run that begins at at, as pluginHold.begin does; the test
// forgets it again at its end.
func noteRun(t *testing.T, at time.Time, command []string) *procSearch {
	t.Helper()
	s := newProcSearch(at, command, nil)
	t.Cleanup(func() {
		searches.mu.Lock()
		defer searches.mu.Unlock()
		searches.recent = slices.DeleteFunc(searches.recent, func(o *procSearch) bool { return o == s })
	})
	return s
}

// Another plugin run is a neighbour of a run's search when it began less than searchWindow before
// or after this one and had not ended by this one's begin; it shares the command line when it runs
// the same one. What the search decides follows (WF38): with a neighbour, a lone new child in
// Cogpit's process group is not taken for the run's process, and with a neighbour of the same
// command line, neither is a lone child seen running it.
func TestSearchNeighbours(t *testing.T) {
	const ms = time.Millisecond
	login := []string{"/opt/homebrew/bin/pinniped", "login", "oidc", "--issuer=https://a.example"}
	otherLogin := []string{"/opt/homebrew/bin/pinniped", "login", "oidc", "--issuer=https://b.example"}
	type run struct {
		begin   time.Duration // from this run's begin
		end     time.Duration // from this run's begin, when ended is set
		ended   bool
		command []string
	}
	for _, tt := range []struct {
		name       string
		others     []run
		near, same bool
	}{
		{"alone", nil, false, false},
		{"under way, began 100 ms before, the same command line", []run{{begin: -100 * ms, command: login}}, true, true},
		{"under way, begins 100 ms after, another command line", []run{{begin: 100 * ms, command: otherLogin}}, true, false},
		{"ended after this one began", []run{{begin: -300 * ms, end: 50 * ms, ended: true, command: login}}, true, true},
		{"ended before this one began", []run{{begin: -100 * ms, end: -10 * ms, ended: true, command: login}}, false, false},
		{"ended as this one began", []run{{begin: -100 * ms, end: 0, ended: true, command: login}}, false, false},
		{"under way, began a window before", []run{{begin: -searchWindow, command: login}}, false, false},
		{"under way, began just inside the window", []run{{begin: -searchWindow + ms, command: login}}, true, true},
		{"under way, begins a window after", []run{{begin: searchWindow, command: login}}, false, false},
		{"another command line near, the same one a window away", []run{
			{begin: -100 * ms, command: otherLogin}, {begin: -searchWindow - 10*ms, command: login}}, true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, o := range tt.others {
				r := noteRun(t, neighbourBase.Add(o.begin), o.command)
				if o.ended {
					r.finished(neighbourBase.Add(o.end))
				}
			}
			s := noteRun(t, neighbourBase, login)
			near, same := s.neighbours()
			if near != tt.near || same != tt.same {
				t.Fatalf("near %v, same %v; want near %v, same %v", near, same, tt.near, tt.same)
			}
			child := procID{pid: 4242, start: 1}
			grouped := decideProcess(map[procID]*sighting{child: {group: true}}, near, same)
			matched := decideProcess(map[procID]*sighting{child: {group: true, matched: true}}, near, same)
			if (grouped == child) != !tt.near || (matched == child) != !tt.same {
				t.Errorf("the decision: %+v for a lone child in the group (the child wanted: %v), %+v for one running the command line (wanted: %v)",
					grouped, !tt.near, matched, !tt.same)
			}
		})
	}
}
