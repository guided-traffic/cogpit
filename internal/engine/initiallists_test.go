package engine

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/cache"
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
// that the connection lists from now on (D-135).
func TestStallText(t *testing.T) {
	stall := &cache.StreamStallError{Silent: 10 * time.Second, Answered: true, Objects: 33}
	for _, tc := range []struct {
		st   cache.Stall
		want string
	}{
		{cache.Stall{Err: stall, Listed: 120 * time.Millisecond},
			"the server's stream of the initial list (WatchList, beta) stalled: 33 objects received, then nothing for 10s before the end of the initial list; a list answered in 120 ms: this connection lists from now on, the next one streams again"},
		{cache.Stall{Err: &cache.StreamStallError{Silent: 10 * time.Second}, ListErr: errors.New("connection reset by peer")},
			"the server's stream of the initial list (WatchList, beta) stalled: the server did not answer the stream's request within 10s; the list in its place failed (connection reset by peer): this connection lists from now on, the next one streams again"},
	} {
		if got := stallText(tc.st); got != tc.want {
			t.Errorf("stallText =\n%s\nwant\n%s", got, tc.want)
		}
	}
}

// Diagnostics says why a connection that began streaming lists now; nothing for one that lists by
// default.
func TestStreamsText(t *testing.T) {
	listing := cache.New(nil, cache.Options{Lists: true})
	t.Cleanup(listing.Close)
	streaming := cache.New(nil, cache.Options{})
	t.Cleanup(streaming.Close)
	for _, tc := range []struct {
		b    *Backend
		on   bool
		want string
	}{
		{&Backend{Cache: listing}, false, ""},
		{&Backend{Cache: streaming, Streaming: true}, true, ""},
		{&Backend{Cache: listing, Streaming: true}, true, "for this connection: a stream stalled"},
		{&Backend{Cache: listing, Streaming: true}, false, "streaming switched off in Cluster Settings"},
	} {
		if got := streamsText(tc.b, ClusterSettings{Streaming: tc.on}); got != tc.want {
			t.Errorf("streamsText(streaming %v, listing %v, setting %v) = %q, want %q", tc.b.Streaming, tc.b.Cache.Listing(), tc.on, got, tc.want)
		}
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
