package fyneui

import (
	"runtime"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// TestPerfGateRunsEveryPhase runs the gate measurement offscreen over the real engine and demo
// cluster, with short phases: start-up, scroll, a switch through every view (each shows rows),
// and typing into the YAML editor. It proves the measurement, not the timings.
func TestPerfGateRunsEveryPhase(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the 10,000-pod demo cluster")
	}
	defer func(s, v, y time.Duration, b int) {
		gateScrollFor, gateViewDwell, gateYAMLFor, gateYAMLBytes = s, v, y, b
	}(gateScrollFor, gateViewDwell, gateYAMLFor, gateYAMLBytes)
	gateScrollFor, gateViewDwell, gateYAMLFor, gateYAMLBytes = 300*time.Millisecond, 500*time.Millisecond, 300*time.Millisecond, 200<<10

	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{IncludeDemo: true}})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(ThemeDark))
	w := test.NewTempWindow(t, nil)
	q := &mainQueue{}
	var got PerfStats
	reported := false
	opts := Options{AutoConnect: engine.DemoContext, PerfMode: PerfGate, PerfReport: func(ps PerfStats) { got, reported = ps, true }}
	sh := newShell(s, a, w, opts, q.do)
	w.SetContent(sh.root)
	w.Resize(fyne.NewSize(1440, 900))
	sh.start()
	sh.startPerf()
	defer func() {
		sh.stop()
		q.drain()
	}()
	// The meter posts a callback every frame; a main goroutine slower than that (the race detector
	// on a loaded runner) never empties the queue, so drain only what is queued now and check the
	// deadline between batches.
	deadline := time.Now().Add(60 * time.Second)
	for !reported {
		if time.Now().After(deadline) {
			t.Fatal("the gate measurement did not finish")
		}
		drainQueued(q)
		time.Sleep(2 * time.Millisecond)
	}

	var names []string
	for _, p := range got.Phases {
		names = append(names, p.Name)
	}
	if len(names) != 4 || names[0] != "startup" || names[1] != "scroll" || names[2] != "views" || names[3] != "yaml" {
		t.Fatalf("phases %v", names)
	}
	views := got.Phases[2]
	if len(views.Switches) != 7 { // the demo's views but Pods, Applications among them (2026-10-13), then Pods
		t.Fatalf("switches %+v", views.Switches)
	}
	for _, sw := range views.Switches {
		if sw.RowsAfterMs < 0 || sw.Rows == 0 {
			t.Errorf("view %s never showed rows: %+v", sw.View, sw)
		}
	}
	keys := got.Phases[3].Keystrokes
	// Go's clock on Windows advances in steps of about half a millisecond, so a keystroke's work
	// there may measure 0.
	if keys == nil || keys.Count == 0 || keys.DocBytes < 200<<10 || keys.WorkMaxMs < 0 ||
		(keys.WorkMaxMs == 0 && runtime.GOOS != "windows") {
		t.Errorf("keystrokes %+v", keys)
	}
	if got.Samples == 0 || got.DurationS <= 0 {
		t.Errorf("scroll stats %+v", got)
	}
	t.Logf("switches %+v; keys %+v", views.Switches, *keys)
}

// TestPerfGateFinishesOnASlowMainGoroutine runs the gate with every main-goroutine callback taking
// longer than a frame, as on a loaded CI runner with the race detector (how
// TestPerfGateRunsEveryPhase once hung until go test's 10-minute timeout): the meter must report
// once, stop posting, and leave a queue that drains. Before, the meter kept posting after its
// report, so the queue never emptied, and a sample still queued at the report ended the
// measurement again and closed the YAML window twice, which panics in Fyne's test driver while it
// holds its window lock and blocks every later call into the driver.
func TestPerfGateFinishesOnASlowMainGoroutine(t *testing.T) {
	defer func(s, v, y time.Duration, b int) {
		gateScrollFor, gateViewDwell, gateYAMLFor, gateYAMLBytes = s, v, y, b
	}(gateScrollFor, gateViewDwell, gateYAMLFor, gateYAMLBytes)
	gateScrollFor, gateViewDwell, gateYAMLFor, gateYAMLBytes = 200*time.Millisecond, 100*time.Millisecond, 200*time.Millisecond, 64<<10

	f := connectedFake(t, 50)
	for _, k := range f.kinds {
		snap := *f.tables["pods"]
		snap.View = k.Kind
		f.tables[k.Kind] = &snap
	}
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(ThemeDark))
	w := test.NewTempWindow(t, nil)
	q := &mainQueue{}
	slow := func(fn func()) { q.do(func() { time.Sleep(perfFrame + 4*time.Millisecond); fn() }) }
	reports := 0
	opts := Options{PerfMode: PerfGate, PerfReport: func(PerfStats) { reports++ }}
	sh := newShell(f, a, w, opts, slow)
	w.SetContent(sh.root)
	w.Resize(fyne.NewSize(1440, 900))
	sh.start()
	windows := len(a.Driver().AllWindows())
	sh.startPerf()
	defer func() {
		sh.stop()
		q.drain()
	}()
	deadline := time.Now().Add(60 * time.Second)
	for reports == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the gate measurement did not finish")
		}
		drainQueued(q)
	}
	// Reported: the meter no longer posts, so the backlog ends, and the samples in it do nothing.
	for drainQueued(q) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("the main queue still grows after the meter reported")
		}
	}
	if reports != 1 {
		t.Errorf("reported %d times", reports)
	}
	if n := len(a.Driver().AllWindows()); n != windows {
		t.Errorf("%d windows after the measurement, %d before: the YAML window is still open", n, windows)
	}
}

// drainQueued runs the functions queued when it is called, not those they or other goroutines
// queue meanwhile, and returns how many it ran. Unlike mainQueue.drain it always returns.
func drainQueued(q *mainQueue) int {
	q.mu.Lock()
	fns := q.fns
	q.fns = nil
	q.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
	return len(fns)
}
