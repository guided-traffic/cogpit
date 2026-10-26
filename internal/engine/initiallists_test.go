package engine

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The server's own refusal and an early end keep their fallback texts (D-133).
func TestFallbackText(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{errors.New("sendInitialEvents is forbidden"),
			"the server did not stream the initial list (WatchList); listed instead: sendInitialEvents is forbidden"},
		{nil, "the server did not stream the initial list (WatchList); listed instead: the stream ended before the list was complete"},
	} {
		if got := fallbackText(tc.err); got != tc.want {
			t.Errorf("fallbackText(%v) =\n%s\nwant\n%s", tc.err, got, tc.want)
		}
	}
}

// A stalled stream's line says what the stream received, how the list in its place answered and
// what Cogpit does about it (D-new).
func TestStallText(t *testing.T) {
	stall := &cache.StreamStallError{Silent: 10 * time.Second, Answered: true, Objects: 33}
	for _, tc := range []struct {
		st        cache.Stall
		confirmed bool
		want      string
	}{
		{cache.Stall{Err: stall, Listed: 120 * time.Millisecond}, true,
			"the server's stream of the initial list (WatchList) stalled: 33 objects received, then nothing for 10s before the end of the initial list; a list answered in 120 ms: the stream stalled, not the connection; streams are off for this cluster from now on (Cluster Settings › General)"},
		{cache.Stall{Err: &cache.StreamStallError{Silent: 10 * time.Second}, Listed: 4200 * time.Millisecond}, false,
			"the server's stream of the initial list (WatchList) stalled: the server did not answer the stream's request within 10s; the list in its place took 4.2 s: the connection, not the stream, it seems; this connection lists, the next one streams again"},
		{cache.Stall{Err: stall, ListErr: errors.New("connection reset by peer")}, false,
			"the server's stream of the initial list (WatchList) stalled: 33 objects received, then nothing for 10s before the end of the initial list; the list in its place failed (connection reset by peer): the connection, not the stream, it seems; this connection lists, the next one streams again"},
	} {
		if got := stallText(tc.st, tc.confirmed); got != tc.want {
			t.Errorf("stallText =\n%s\nwant\n%s", got, tc.want)
		}
	}
}

// A stall whose list answered at once switches the cluster's streams off in its settings, once; one
// whose list answered late or failed leaves them (D-new).
func TestStreamStalledSwitchesTheStreamsOff(t *testing.T) {
	stall := &cache.StreamStallError{Silent: 10 * time.Second, Answered: true, Objects: 3}
	pods := cache.Scope{GVR: testkit.PodsGVR, Namespace: "team-a"}
	for _, tc := range []struct {
		name string
		st   cache.Stall
		off  bool
	}{
		{"a quick list", cache.Stall{Scope: pods, Err: stall, Listed: 80 * time.Millisecond}, true},
		{"a slow list", cache.Stall{Scope: pods, Err: stall, Listed: 3 * time.Second}, false},
		{"a failed list", cache.Stall{Scope: pods, Err: stall, ListErr: errors.New("EOF")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New(Options{ClusterSettings: NewClusterSettingsStore(t.TempDir())})
			t.Cleanup(s.Close)
			b := &Backend{Context: "acme-stage"}
			s.streamStalled(b, tc.st)
			if tc.off {
				waitUntil(t, "the streams off", func() bool { cs, _ := s.ClusterSettingsOf("acme-stage"); return cs.StreamsOff != nil })
			} else {
				time.Sleep(50 * time.Millisecond)
			}
			cs, _ := s.ClusterSettingsOf("acme-stage")
			if got := cs.StreamsOff != nil; got != tc.off {
				t.Fatalf("streams off = %+v, want %v", cs.StreamsOff, tc.off)
			}
			if !tc.off {
				return
			}
			first := *cs.StreamsOff
			if first.By != StreamsOffByCogpit || first.At.IsZero() || !strings.Contains(first.Why, "pods in team-a: 3 objects received") ||
				!strings.HasSuffix(first.Why, "a list answered in 80 ms") {
				t.Errorf("streams off = %+v", first)
			}
			if ui := s.UIState().ClusterSettings["acme-stage"]; ui.StreamsOff == nil {
				t.Error("the UI state does not show the streams off")
			}
			s.streamStalled(b, tc.st) // a second stall keeps the first reason
			time.Sleep(50 * time.Millisecond)
			if cs, _ := s.ClusterSettingsOf("acme-stage"); !equalStreamsOff(cs.StreamsOff, &first) {
				t.Errorf("a second stall changed the switch: %+v", cs.StreamsOff)
			}
		})
	}
}

// Only who Cogpit knows may have switched the streams off, and a file's unknown one is dropped.
func TestStreamsOffChecked(t *testing.T) {
	bad := ClusterSettings{StreamsOff: &StreamsOff{By: "agent"}}
	if err := bad.Check(); err == nil {
		t.Error("an unknown switcher passes the check")
	}
	bad.normalize()
	if bad.StreamsOff != nil {
		t.Errorf("normalize kept %+v", bad.StreamsOff)
	}
	if err := (ClusterSettings{StreamsOff: &StreamsOff{By: StreamsOffByUser, At: time.Now()}}).Check(); err != nil {
		t.Error(err)
	}
	if got := cutBytes("ab€", 4); got != "ab" {
		t.Errorf("cutBytes = %q", got)
	}
}

// The report's initial lists say why the connection lists every read.
func TestReportSaysStreamsOff(t *testing.T) {
	r := ReportOf(Diagnostics{Context: "c", Streams: "for this connection: a stream stalled"}, true)
	text := reportText(r, nil, time.Now())
	if !strings.Contains(text, "listed after the server did not stream them 0 · streams off, every read lists: for this connection: a stream stalled") {
		t.Errorf("report:\n%s", text)
	}
	if strings.Contains(reportText(ReportOf(Diagnostics{Context: "c"}, true), nil, time.Now()), "streams off") {
		t.Error("a connection that streams says the streams are off")
	}
}
