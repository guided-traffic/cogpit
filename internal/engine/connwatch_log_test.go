package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The connection watch's attempts in the connection log (D-66): an interruption is an entry of its
// own with its cause's error picture and the reconnect after it the next attempt (WF37), a rejected
// login a login attempt, and a credential plugin
// that fails a check adds its scrubbed standard error to the attempt.

// attemptText is an attempt's lines, one per line, each with its step.
func attemptText(a ConnectAttempt) string {
	var b strings.Builder
	for _, l := range a.Lines {
		b.WriteString(l.Level + " " + l.Step + ": " + l.Text + "\n")
	}
	return b.String()
}

func TestTheWatchWritesReconnectAndLoginAttempts(t *testing.T) {
	s, p := watchedSession(t, fastWatch)
	p.set(func(f *fakeProber) { f.down = true })
	waitState(t, s, Interrupted)
	p.set(func(f *fakeProber) { f.down = false })
	waitState(t, s, Connected)
	p.set(func(f *fakeProber) { f.reject = true })
	waitState(t, s, LoginExpired)
	p.set(func(f *fakeProber) { f.reject = false })
	if err := s.Login(); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, Connected)

	got, err := s.ConnectAttempts("test", 0)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, a := range got {
		kinds = append(kinds, a.Kind+"/"+a.Outcome)
	}
	want := []string{"login/ok", "login/failed", "reconnect/ok", "interruption/interrupted", "connect/ok"}
	if strings.Join(kinds, " ") != strings.Join(want, " ") {
		t.Fatalf("attempts %v, want %v", kinds, want)
	}
	if text := attemptText(got[3]); !strings.Contains(text, "WARN : Connection interrupted: no answer from the API server") ||
		!strings.Contains(text, "WARN error: Class: timeout") || strings.Contains(text, "Reconnected") {
		t.Errorf("the interruption:\n%s", text)
	}
	if text := attemptText(got[2]); !reconnectOnly(text, "test") {
		t.Errorf("the reconnect, its own steps and its success only:\n%s", text)
	}
	if text := attemptText(got[1]); !strings.Contains(text, "rejected the login (401 Unauthorized)") || !strings.Contains(text, "Login expired") {
		t.Errorf("the rejected login:\n%s", text)
	}
	if text := attemptText(got[0]); !strings.Contains(text, "Logging in again") || !strings.Contains(text, "Logged in again to test") {
		t.Errorf("the login:\n%s", text)
	}
	if s.state.logs.problem("test") {
		t.Error("a reconnect or login marked the context as a connect problem")
	}
}

// reconnectOnly reports whether a reconnect attempt's text holds its own lines only: it begins with
// "Reconnecting", says "Reconnected" once, and its only warnings are its failed tries before that.
// A try may fail before the prober is up again (the test turns it up once it saw the interruption,
// and the first try comes 20 ms after it: a slow runner, Windows on 2026-10-23, tried first); the
// interruption's own lines never belong here.
func reconnectOnly(text, ctx string) bool {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) == 0 || lines[0] != "INFO : Reconnecting to "+ctx {
		return false
	}
	reconnected := -1
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "INFO : Reconnected to "+ctx+" after "):
			if reconnected >= 0 {
				return false
			}
			reconnected = i
		case strings.HasPrefix(line, "WARN : A try to reach the cluster failed: "):
			if reconnected >= 0 {
				return false // a failed try after the success
			}
		case strings.HasPrefix(line, "WARN"), strings.HasPrefix(line, "ERROR"):
			return false // the interruption's lines, or anything else that went wrong
		}
	}
	return reconnected > 0
}

// The check above takes a reconnect's own failed tries and nothing of the interruption.
func TestReconnectOnly(t *testing.T) {
	const (
		begin = "INFO : Reconnecting to test\n"
		try   = "WARN : A try to reach the cluster failed: dial tcp 10.0.0.1:6443: connect: network is unreachable; the next in 40ms\n"
		ok    = "INFO : Reconnected to test after 141 ms\n"
	)
	for text, want := range map[string]bool{
		begin + ok:       true,
		begin + try + ok: true,
		begin + ok + "INFO : The API server did not confirm the user\n":               true,
		begin + "WARN : Connection interrupted: no answer from the API server\n" + ok: false,
		begin + "WARN error: Class: timeout\n" + ok:                                   false,
		begin + ok + try: false,
		begin + ok + ok:  false,
		begin + try:      false,
		try + begin + ok: false,
		"":               false,
	} {
		if got := reconnectOnly(text, "test"); got != want {
			t.Errorf("%q: %v, want %v", text, got, want)
		}
	}
}

// pluginProber is a connection whose credential plugin fails while fail is set: its heartbeats
// end with the plugin's error and scrubbed standard error, as cluster.Conn's do.
type pluginProber struct {
	mu   sync.Mutex
	fail bool
}

func (p *pluginProber) Heartbeat(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fail {
		return &cluster.PluginError{
			Err:    errors.New(`Get "https://team.example:6443/version": getting credentials: exec: executable kubelogin failed with exit code 1`),
			Stderr: "error: the refresh token expired\nrun kubelogin again",
		}
	}
	return nil
}
func (p *pluginProber) Reachable(context.Context) error { return nil }
func (p *pluginProber) SetHealth(cluster.Health)        {}
func (p *pluginProber) ResetConnections()               {}
func (p *pluginProber) ArmLogin()                       {}

func TestAFailedPluginsStandardErrorIsInTheInterruption(t *testing.T) {
	p := &pluginProber{}
	fc := &fakeConnector{src: testkit.NewMemorySource(), profiles: testProfiles(), conn: &cluster.Conn{}, health: p}
	s := New(Options{Connector: fc, SnapshotInterval: 10 * time.Millisecond, ConnWatch: fastWatch})
	t.Cleanup(s.Close)
	connectTest(t, s)
	p.mu.Lock()
	p.fail = true
	p.mu.Unlock()
	waitState(t, s, Interrupted)
	waitUntil(t, "the plugin's standard error in the interruption", func() bool {
		got, _ := s.ConnectAttempts("test", 2)
		return len(got) == 2 && got[1].Kind == AttemptInterruption && strings.Contains(attemptText(got[1]), "WARN plugin: run kubelogin again")
	})
	got, _ := s.ConnectAttempts("test", 3)
	if text := attemptText(got[1]); !strings.Contains(text, "WARN error: Class: the credential plugin failed: exec: executable kubelogin failed with exit code 1") ||
		!strings.Contains(text, "WARN plugin: The credential plugin's standard error (scrubbed, at most 4 KiB):") ||
		!strings.Contains(text, "WARN plugin: run kubelogin again") {
		t.Errorf("the interruption:\n%s", text)
	}
	if got[0].Kind != AttemptReconnect {
		t.Errorf("the attempt after the interruption: %s", got[0].Kind)
	}
	if text := attemptText(got[2]); strings.Contains(text, "refresh token") {
		t.Errorf("the plugin's output went to the connect that stood:\n%s", text)
	}
}
