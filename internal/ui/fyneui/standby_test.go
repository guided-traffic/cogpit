package fyneui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestStandbyWindowStaysUsable is D-58's (e) with the window: the real engine and client-go stack
// against the fake API server behind a proxy that goes silent, as a cluster does for a computer
// that slept (deterministic, so CI runs it; the docker pause of a kind node is
// standby_freeze_test.go's, by hand). Frozen: the connection is Interrupted within the heartbeat
// window, the top bar's dot turns amber and reads "Reconnecting…", the banner says since when, a
// write is refused at once, and the main goroutine's work never takes 100 ms. Thawed: Connected
// again, the banner gone, the pod's log resumed with the line written meanwhile after the marker.
func TestStandbyWindowStaysUsable(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: "standby-token"})
	defer srv.Close()
	srv.Set(testkit.PodsGVR, testkit.Pod("shop", "web-0"))
	t0 := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	srv.AddLog("shop", "web-0", "app", t0, "before the sleep")
	proxy := testkit.NewFreezableProxy(t, strings.TrimPrefix(srv.URL, "https://"))
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, proxy.ProxyKubeconfig(srv, "fake", ""), 0o600); err != nil {
		t.Fatal(err)
	}
	watch := engine.ConnWatchOptions{Heartbeat: time.Second, ProbeTimeout: 400 * time.Millisecond, Confirm: 200 * time.Millisecond,
		LoginWait: 3 * time.Second, BackoffFirst: 100 * time.Millisecond, BackoffMax: 800 * time.Millisecond}
	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{path}}, ConnWatch: watch,
		SnapshotInterval: 20 * time.Millisecond})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(ThemeDark))
	w := test.NewTempWindow(t, nil)
	w.SetPadded(false)
	q := &mainQueue{}
	sh := newShell(s, a, w, Options{AutoConnect: "fake"}, q.do)
	s.SetConfirmer(sh.confirmer)
	w.SetContent(sh.root)
	w.Resize(fyne.NewSize(1440, 900))
	sh.start()
	defer func() {
		sh.stop()
		q.drain()
	}()
	var worst time.Duration
	step := func() {
		t1 := time.Now()
		q.drain()
		_ = s.UIState()
		worst = max(worst, time.Since(t1))
	}
	until := func(what string, limit time.Duration, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(limit)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			step()
			time.Sleep(10 * time.Millisecond)
		}
		step()
	}
	until("the pods", 30*time.Second, func() bool {
		tbl := s.Table("pods")
		return tbl != nil && tbl.Synced && len(tbl.Rows) == 1
	})
	pod := model.ObjectRef{Context: "fake", Kind: "pods", Namespace: "shop", Name: "web-0"}
	sh.dispatch("pod.logs", map[string]any{"ref": pod}, nil)
	var lt *logTab
	shown := func() []string {
		if c, ok := sh.dock.contents[sh.ui.ActiveDock]; ok && c.logs != nil && c.logs.view != nil {
			lt = c.logs
			return lt.view.ShownLines()
		}
		return nil
	}
	until("the log's line", 30*time.Second, func() bool { return slices.ContainsFunc(shown(), standbyLine("before the sleep")) })
	until("the log to follow", 10*time.Second, func() bool { return srv.LogFollowers("shop", "web-0", "app") == 1 })
	worst = 0

	proxy.Freeze()
	frozen := time.Now()
	until("the interruption", 20*time.Second, func() bool { return sh.ui.Connection.State == engine.Interrupted })
	if d, window := time.Since(frozen), watch.Heartbeat+2*watch.ProbeTimeout+watch.Confirm+2*time.Second; d > window {
		t.Errorf("interrupted after %v, want within %v", d, window)
	}
	if sh.top.dot.color != colorContainerRunning || barSaysState(sh) { // blinking green, no word (F13)
		t.Errorf("the top bar: dot %v, words %q", sh.top.dot.color, barTexts(sh))
	}
	if !sh.conn.root.Visible() || !strings.Contains(sh.conn.title.text, "interrupted · since") {
		t.Errorf("the banner: visible %v, %q", sh.conn.root.Visible(), sh.conn.title.text)
	}
	raw, _ := json.Marshal(map[string]any{"ref": pod})
	start := time.Now()
	_, err := s.Dispatch(context.Background(), engine.Invocation{ID: "object.delete", Params: raw, Origin: engine.OriginGUI})
	var de *engine.DisabledError
	if !errors.As(err, &de) || de.Reason != "connection interrupted" || time.Since(start) > 100*time.Millisecond {
		t.Errorf("a delete while interrupted: %v after %v", err, time.Since(start))
	}
	srv.AddLog("shop", "web-0", "app", time.Now().UTC(), "while it slept")
	for range 100 { // a second of the user working on
		step()
		time.Sleep(10 * time.Millisecond)
	}

	proxy.Thaw()
	until("the reconnect", 20*time.Second, func() bool { return sh.ui.Connection.State == engine.Connected })
	if sh.conn.root.Visible() {
		t.Error("the banner shows after the reconnect")
	}
	until("the resumed log", 20*time.Second, func() bool { return slices.ContainsFunc(shown(), standbyLine("while it slept")) })
	lines := shown()
	marker := slices.IndexFunc(lines, standbyLine("── no connection "))
	after := slices.IndexFunc(lines, standbyLine("while it slept"))
	if marker < 0 || after < marker || slices.IndexFunc(lines, standbyLine("before the sleep")) > marker {
		t.Errorf("the log after the reconnect: %q", lines)
	}
	if n := countFunc(lines, standbyLine("before the sleep")); n != 1 {
		t.Errorf("the line before the sleep shows %d times", n)
	}
	// 100 ms on a plain run; the race detector and coverage slow every step several times (CI's
	// shared runner measured 136 ms under the race detector, run 38978583383), so there the bound
	// only tells a stall from slow work.
	limit := 100 * time.Millisecond
	if raceEnabled || testing.CoverMode() != "" {
		limit = 500 * time.Millisecond
	}
	if worst > limit {
		t.Errorf("the main goroutine's work took %v at worst, more than %v", worst, limit)
	} else {
		t.Logf("the main goroutine's work took %v at worst", worst)
	}
}

func standbyLine(sub string) func(string) bool {
	return func(s string) bool { return strings.Contains(s, sub) }
}

func countFunc(list []string, f func(string) bool) int {
	n := 0
	for _, s := range list {
		if f(s) {
			n++
		}
	}
	return n
}
