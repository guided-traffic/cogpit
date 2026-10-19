package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/cluster"
)

// D-69 item 4: a background cluster's login, held while the cluster is not shown when its setting
// "Renew the login while the cluster is not shown" is off (connwatch_lapse.go).

// heldPoolSession is a pool session with cluster settings in memory, alpha's renewal in the
// background off.
func heldPoolSession(t *testing.T, names ...string) (*Session, *poolConnector) {
	t.Helper()
	pc := newPoolConnector(time.Minute, names...)
	s := New(Options{Connector: pc, SnapshotInterval: 10 * time.Millisecond, ConnWatch: fastWatch,
		ClusterSettings: NewClusterSettingsStore("")})
	t.Cleanup(s.Close)
	if _, err := s.ChangeClusterSettings("alpha", func(cs *ClusterSettings) error { cs.HoldBackgroundLogin = true; return nil }); err != nil {
		t.Fatal(err)
	}
	return s, pc
}

func entryOf(s *Session, contextID string) ConnectedCluster {
	c, _ := s.UIState().ConnectionOf(contextID)
	return c
}

// lapseAlpha connects alpha, then beta, so that alpha is held in the background, and lets alpha's
// login expire: it lapses.
func lapseAlpha(t *testing.T, s *Session, pc *poolConnector) *fakeProber {
	t.Helper()
	mustConnect(t, s, "alpha")
	alpha := pc.clusters["alpha"].prober
	if p := alpha.get(); p.held {
		t.Fatal("the shown alpha holds its login")
	}
	mustConnect(t, s, "beta")
	waitUntil(t, "alpha held in the background", func() bool { return alpha.get().held })
	alpha.set(func(f *fakeProber) { f.expired = true })
	waitUntil(t, "alpha's login lapsed", func() bool { return entryOf(s, "alpha").LoginLapsed })
	return alpha
}

// The setting is on by default and kept: off, the cluster settings file holds it as
// holdBackgroundLogin, and the next start reads it back.
func TestRenewLoginInBackgroundIsOnByDefaultAndKept(t *testing.T) {
	dir := t.TempDir()
	s := New(Options{Connector: newPoolConnector(time.Minute, "alpha"), ClusterSettings: NewClusterSettingsStore(dir)})
	t.Cleanup(s.Close)
	v, err := s.ClusterSettingsView("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if !v.RenewLoginInBackground {
		t.Fatal("the renewal in the background is off by default")
	}
	if _, err := s.ChangeClusterSettings("alpha", func(cs *ClusterSettings) error { cs.HoldBackgroundLogin = true; return nil }); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ClusterSettingsFile))
	if err != nil {
		t.Fatal(err)
	}
	var doc clusterSettingsDoc
	if err := json.Unmarshal(data, &doc); err != nil || !doc.Clusters["alpha"].HoldBackgroundLogin {
		t.Fatalf("the file: %s (%v)", data, err)
	}
	again := New(Options{Connector: newPoolConnector(time.Minute, "alpha"), ClusterSettings: NewClusterSettingsStore(dir)})
	t.Cleanup(again.Close)
	if v, _ := again.ClusterSettingsView("alpha"); v.RenewLoginInBackground {
		t.Error("the setting off was not read back")
	}
}

// TestABackgroundLoginLapsesWhileHeld: with the setting off, a cluster the window left holds its
// login; when it expires, the plugin does not run (no renewal), the connection stays Connected
// (not red) with LoginLapsed and its reason, the gate refuses with the lapsed login, writes are
// disabled with "login lapsed", its heartbeat pauses while the shown cluster's goes on, and an
// agent's read is told. Showing it renews the login once.
func TestABackgroundLoginLapsesWhileHeld(t *testing.T) {
	s, pc := heldPoolSession(t, "alpha", "beta")
	alpha := lapseAlpha(t, s, pc)
	e := entryOf(s, "alpha")
	if e.Connection.State != Connected || e.Connection.Reason != lapsedReason || e.Connection.Since.IsZero() || e.Shown {
		t.Errorf("alpha's entry %+v", e)
	}
	got := alpha.get()
	if got.renewals != 0 {
		t.Errorf("the held plugin renewed the login %d times", got.renewals)
	}
	if len(got.health) == 0 || got.health[len(got.health)-1] != cluster.HealthLoginLapsed {
		t.Errorf("alpha's gate %v", got.health)
	}
	if err := s.writeBlocked(DangerMutating, "alpha"); !errors.Is(err, errConnLoginLapsed) || err.Error() != "login lapsed: show the cluster to renew it" {
		t.Errorf("a write to alpha: %v", err)
	}
	if err := s.writeBlocked(DangerMutating, ""); err != nil {
		t.Errorf("a write to the shown beta: %v", err)
	}
	if note := lapsedNote("alpha", e.Connection); !strings.Contains(note, "lapsed") || !strings.Contains(note, "shown in Cogpit's window") {
		t.Errorf("the agents' note %q", note)
	}
	// The heartbeat pauses: beta beats on, alpha does not.
	beats, betaBeats := got.beats, pc.clusters["beta"].prober.get().beats
	s.NoteWake() // a wake checks every cluster: not a lapsed one
	waitUntil(t, "beta's heartbeats", func() bool { return pc.clusters["beta"].prober.get().beats >= betaBeats+5 })
	if n := alpha.get().beats; n != beats {
		t.Errorf("alpha's heartbeat ran %d times while its login lapsed", n-beats)
	}

	mustConnect(t, s, "alpha") // shown: renewed as the shown cluster's login is (D-58)
	waitUntil(t, "alpha renewed", func() bool {
		u := s.UIState()
		e := entryOf(s, "alpha")
		return u.Context == "alpha" && u.Connection.State == Connected && !e.LoginLapsed && u.Connection.Reason == ""
	})
	if got := alpha.get(); got.renewals != 1 || got.held {
		t.Errorf("after showing alpha: renewals %d, held %v", got.renewals, got.held)
	}
	if err := s.writeBlocked(DangerMutating, "alpha"); err != nil {
		t.Errorf("a write to the renewed alpha: %v", err)
	}
	if c := s.UIState().Connection; c.LoggingIn {
		t.Errorf("still logging in: %+v", c)
	}
}

// Log in again on a cluster that is not shown renews its lapsed login there: the hold is let go for
// the login only, the cluster stays in the background and holds its login again afterwards.
func TestLogInAgainRenewsALapsedBackgroundLogin(t *testing.T) {
	s, pc := heldPoolSession(t, "alpha", "beta")
	alpha := lapseAlpha(t, s, pc)
	if err := s.LoginContext("alpha"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "alpha renewed in the background", func() bool {
		e := entryOf(s, "alpha")
		return !e.LoginLapsed && e.Connection.State == Connected && e.Connection.Reason == "" && !e.Connection.LoggingIn
	})
	got := alpha.get()
	if got.renewals != 1 || !got.held {
		t.Errorf("renewals %d, held %v after the login", got.renewals, got.held)
	}
	if !containsSeq(got.holds, false, true) {
		t.Errorf("the hold was not let go for the login: %v", got.holds)
	}
	if s.UIState().Context != "beta" {
		t.Error("Log in again showed the cluster")
	}
}

// Turning the setting on again renews a lapsed login at once, in the background.
func TestTurningTheRenewalOnRenewsALapsedLogin(t *testing.T) {
	s, pc := heldPoolSession(t, "alpha", "beta")
	alpha := lapseAlpha(t, s, pc)
	if _, err := s.ChangeClusterSettings("alpha", func(cs *ClusterSettings) error { cs.HoldBackgroundLogin = false; return nil }); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "alpha renewed", func() bool { return !entryOf(s, "alpha").LoginLapsed && alpha.get().renewals == 1 })
	if alpha.get().held {
		t.Error("alpha still holds its login with the setting on")
	}
}

// With the setting on (the default), a background cluster's expired login is renewed as before:
// no hold, no lapse.
func TestABackgroundLoginIsRenewedByDefault(t *testing.T) {
	s, pc := poolSession(t, time.Minute, "alpha", "beta")
	mustConnect(t, s, "alpha")
	mustConnect(t, s, "beta")
	alpha := pc.clusters["alpha"].prober
	alpha.set(func(f *fakeProber) { f.expired = true })
	waitUntil(t, "alpha's login renewed", func() bool { return alpha.get().renewals == 1 })
	if e := entryOf(s, "alpha"); e.LoginLapsed || alpha.get().held {
		t.Errorf("alpha lapsed or held with the default setting: %+v", e)
	}
}

// A followed log of a cluster whose login lapsed and whose stream must open again waits with a
// notice line; once the cluster is shown and its login renewed, it goes on after the marker.
func TestALogOfALapsedClusterWaitsWithANotice(t *testing.T) {
	s, pc := heldPoolSession(t, "alpha", "beta")
	mustConnect(t, s, "alpha")
	srv := newServerLogs()
	key := "alpha-pod/app"
	t0 := time.Now().UTC().Truncate(time.Second)
	srv.add(key, t0, "before the lapse")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rc, err := s.openResumingLog(ctx, s.BackendOf("alpha"), srv, cluster.LogOptions{Namespace: "default", Pod: "alpha-pod", Container: "app", Follow: true, Timestamps: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(rc)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	next := func(what string) string {
		t.Helper()
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatalf("the log ended before %s", what)
			}
			return l
		case <-ctx.Done():
			t.Fatalf("no %s", what)
		}
		return ""
	}
	if l := next("the first line"); !strings.HasSuffix(l, "before the lapse") {
		t.Fatalf("first line %q", l)
	}
	mustConnect(t, s, "beta")
	alpha := pc.clusters["alpha"].prober
	waitUntil(t, "alpha held", func() bool { return alpha.get().held })
	alpha.set(func(f *fakeProber) { f.expired = true })
	srv.drop(key) // the stream ends: opening it again needs the login
	if l := next("the notice"); l != NoticePrefix+" "+lapsedNotice {
		t.Fatalf("after the break: %q, want the lapsed login's notice", l)
	}
	srv.add(key, t0.Add(2*time.Second), "while it lapsed")
	mustConnect(t, s, "alpha")
	if l := next("the marker"); !strings.Contains(l, interruptedMarkerStart) {
		t.Fatalf("after the renewal: %q, want the marker", l)
	}
	if l := next("the line after the renewal"); !strings.HasSuffix(l, "while it lapsed") {
		t.Fatalf("after the marker: %q", l)
	}
}

// containsSeq reports whether list holds want in this order, not necessarily next to each other.
func containsSeq(list []bool, want ...bool) bool {
	i := 0
	for _, v := range list {
		if i < len(want) && v == want[i] {
			i++
		}
	}
	return i == len(want)
}
