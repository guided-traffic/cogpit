package engine

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/cache"
)

// A stalled stream's log line says what the stream received and that the connection lists from
// now on; the server's own refusal and an early end keep their texts (D-new, D-133).
func TestFallbackText(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{&cache.StreamStallError{Silent: 3 * time.Second, Answered: true, Objects: 33},
			"the server's stream of the initial list (WatchList) stalled: 33 objects received, then nothing for 3s before the end of the initial list; listed instead, and every read of this connection lists from now on"},
		{&cache.StreamStallError{Silent: 3 * time.Second},
			"the server's stream of the initial list (WatchList) stalled: the server did not answer the stream's request within 3s; listed instead, and every read of this connection lists from now on"},
		{errors.New("sendInitialEvents is forbidden"),
			"the server did not stream the initial list (WatchList); listed instead: sendInitialEvents is forbidden"},
		{nil, "the server did not stream the initial list (WatchList); listed instead: the stream ended before the list was complete"},
	} {
		if got := fallbackText(tc.err); got != tc.want {
			t.Errorf("fallbackText(%v) =\n%s\nwant\n%s", tc.err, got, tc.want)
		}
	}
}

// The report's initial lists say once the connection lists every read since a stream stalled.
func TestReportSaysStreamsOff(t *testing.T) {
	r := ReportOf(Diagnostics{Context: "c", Listing: true}, true)
	if !r.Streaming.Off {
		t.Fatalf("streaming = %+v", r.Streaming)
	}
	text := reportText(r, nil, time.Now())
	if !strings.Contains(text, "listed after the server did not stream them 0 · streams off: a stream stalled, every read lists") {
		t.Errorf("report:\n%s", text)
	}
	if strings.Contains(reportText(ReportOf(Diagnostics{Context: "c"}, true), nil, time.Now()), "streams off") {
		t.Error("a connection that streams says the streams are off")
	}
}
