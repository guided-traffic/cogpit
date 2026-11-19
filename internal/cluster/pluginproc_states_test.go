package cluster

import (
	"slices"
	"sync"
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

// Another plugin run is a neighbour of a run's search when its own search looked while this one did
// (it began before this search stopped looking, and had not stopped looking by this run's begin)
// and it had not ended by this one's begin; it shares the command line when it runs the same one.
// A search looks until searchWindow after its first new child: here the first child appears at
// once, but for a search still looking (client-go started its plugin late). What the search
// decides follows (WF38): with a neighbour, a lone new child in Cogpit's process group is not taken
// for the run's process, and with a neighbour of the same command line, neither is a lone child
// seen running it.
func TestSearchNeighbours(t *testing.T) {
	const ms = time.Millisecond
	login := []string{"/opt/homebrew/bin/pinniped", "login", "oidc", "--issuer=https://a.example"}
	otherLogin := []string{"/opt/homebrew/bin/pinniped", "login", "oidc", "--issuer=https://b.example"}
	type run struct {
		begin   time.Duration // from this run's begin
		end     time.Duration // from this run's begin, when ended is set
		ended   bool
		looking bool // its search still looks: no new child seen yet
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
		{"still looking, began three windows before", []run{{begin: -3 * searchWindow, looking: true, command: login}}, true, true},
		{"still looking, began three windows before, ended", []run{
			{begin: -3 * searchWindow, end: -ms, ended: true, looking: true, command: login}}, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, o := range tt.others {
				r := noteRun(t, neighbourBase.Add(o.begin), o.command)
				if o.ended {
					r.finished(neighbourBase.Add(o.end))
				}
				if !o.looking {
					r.stop(neighbourBase.Add(o.begin + searchWindow))
				}
			}
			s := noteRun(t, neighbourBase, login)
			near, same := s.neighbours(neighbourBase.Add(searchWindow))
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

// steppedChildren is a search's list of Cogpit's children that moves one step on at each look and
// stays at the last step: a test sets what each look sees, whatever the search's pace.
func steppedChildren(steps ...[]childProcess) func() []childProcess {
	var mu sync.Mutex
	n := 0
	return func() []childProcess {
		mu.Lock()
		defer mu.Unlock()
		out := steps[min(n, len(steps)-1)]
		n++
		return out
	}
}

// A terminal's shell leaves Cogpit's process group right after its fork (setsid, setpgid), and a
// look in that moment sees it in the group. The search counts a child as in the group by its last
// look: counted by its first, the shell made two children in the group with the run's, and a run
// whose own command line the search missed (a wrapper that replaced itself before the first look)
// had no process (CI, 2026-10-19 and 2026-11-05: the search found pid 0). The looks are the test's:
// the shell in the group at the first, out of it with the plugin there at every later one; a shell
// still in the group at the end leaves two children there, and none is taken.
func TestSearchCountsTheGroupAChildEndsIn(t *testing.T) {
	login := []string{"/no/such/plugin", "login"} // the wrapper replaced itself: never seen running it
	shellIn := childProcess{id: procID{pid: 101, start: 1}, group: true, argv: []string{"/bin/zsh", "-l"}}
	shellOut := shellIn
	shellOut.group = false
	plugin := childProcess{id: procID{pid: 102, start: 2}, group: true, argv: []string{"/bin/sleep", "900"}}
	older := childProcess{id: procID{pid: 100, start: 0}, group: true, argv: []string{"/bin/sleep", "900"}}
	for _, tt := range []struct {
		name  string
		steps [][]childProcess
		want  procID
	}{
		{"the shell left the group", [][]childProcess{{older, shellIn}, {older, shellOut, plugin}}, plugin.id},
		{"the shell stayed in the group", [][]childProcess{{older, shellIn}, {older, shellIn, plugin}}, procID{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			quietSearches(t)
			stop := make(chan struct{})
			defer close(stop)
			s := newProcSearch(time.Now(), login, map[procID]bool{older.id: true})
			s.children = steppedChildren(tt.steps...)
			defer func() { s.finished(time.Now()) }()
			go s.run(stop)
			select {
			case <-s.done:
			case <-time.After(30 * time.Second):
				t.Fatal("the search did not decide")
			}
			if s.found != tt.want {
				t.Errorf("the search found %+v, want %+v", s.found, tt.want)
			}
		})
	}
}
