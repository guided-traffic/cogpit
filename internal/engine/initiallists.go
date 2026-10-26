package engine

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
)

// A cluster's streamed initial lists (D-new; cache/stall.go). On Automatic, the default, every read
// streams its initial list (client-go's WatchList), and a stream that stays silent for the cache's
// bound before its end makes that read and every later read of the connection list instead. When
// the list in its place answers within streamsOffConfirm, the stream stalled and not the
// connection: Cogpit switches the cluster's streams off in its Cluster Settings (StreamsOff,
// by Cogpit), and every later connect lists from the start. A list that answers late or fails
// points at the connection: the switch stays this connection's. The user switches them off or back
// to Automatic in Cluster Settings › General (cluster.settings.set streamInitialLists): off applies
// to the connected cluster at once, Automatic from its next connect.

// streamsOffConfirm is how soon the list in a stalled stream's place must answer for Cogpit to
// switch the cluster's streams off.
const streamsOffConfirm = 2 * time.Second

// Who switched a cluster's streams off (StreamsOff.By).
const (
	StreamsOffByUser   = "user"
	StreamsOffByCogpit = "cogpit"
)

// maxStreamsOffWhy bounds the reason Cogpit keeps.
const maxStreamsOffWhy = 1000

// StreamsOff says a cluster's initial lists are listed and then watched, never streamed: the user
// switched the streams off, or Cogpit did after a stream stalled while a list answered at once.
type StreamsOff struct {
	By string    `json:"by"`
	At time.Time `json:"at"`
	// Why is Cogpit's reason, the stall and the list's answer; empty for the user's.
	Why string `json:"why,omitempty"`
}

// equalStreamsOff reports whether two switches are the same; none equals only none.
func equalStreamsOff(a, b *StreamsOff) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.By == b.By && a.At.Equal(b.At) && a.Why == b.Why
}

// checkStreamsOff says why a switch cannot be kept.
func checkStreamsOff(o *StreamsOff) error {
	switch {
	case o == nil:
		return nil
	case o.By != StreamsOffByUser && o.By != StreamsOffByCogpit:
		return fmt.Errorf("streams switched off by %q: by %s or %s", o.By, StreamsOffByUser, StreamsOffByCogpit)
	case len(o.Why) > maxStreamsOffWhy:
		return fmt.Errorf("the reason the streams are off has %d bytes, at most %d", len(o.Why), maxStreamsOffWhy)
	}
	return nil
}

// streamsOffOf reports whether a context's streams are off in its settings.
func (c *ClusterSettingsStore) streamsOffOf(context string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.load() // an unreadable file is reported where it loads: streamed then
	return c.by[context].StreamsOff != nil
}

// attachStreams has the session hear of a connection's stalled streams. Connect and reconnect
// call it before the backend is published.
func (s *Session) attachStreams(b *Backend) {
	if b == nil || b.Cache == nil || b.Demo {
		return
	}
	b.Cache.OnStall(func(st cache.Stall) { s.streamStalled(b, st) })
}

// streamStalled notes a stalled stream in Connection Diagnostics' log and, when the list in its
// place answered at once, switches the cluster's streams off in its settings. It runs on an
// informer's goroutine: the settings are written on one of the session's.
func (s *Session) streamStalled(b *Backend, st cache.Stall) {
	confirmed := st.ListErr == nil && st.Listed < streamsOffConfirm
	if log := b.diagLog(); log != nil {
		log.Add(cluster.DiagEntry{Kind: cluster.DiagFallback, What: st.Scope.String(), Text: stallText(st, confirmed)})
	}
	if !confirmed {
		return
	}
	why := fmt.Sprintf("%s: %s; a list answered in %s", st.Scope, st.Err, FormatSpan(st.Listed))
	at := time.Now()
	s.goSafe("streams off", func() {
		_, err := s.ChangeClusterSettings(b.Context, func(cs *ClusterSettings) error {
			if cs.StreamsOff != nil {
				return errStreamsOffAlready
			}
			cs.StreamsOff = &StreamsOff{By: StreamsOffByCogpit, At: at, Why: cutBytes(why, maxStreamsOffWhy)}
			return nil
		})
		if err != nil && !errors.Is(err, errStreamsOffAlready) && !errors.Is(err, ErrNoClusterSettings) {
			s.log.Warn("engine: the cluster's streams could not be switched off in its settings", "context", b.Context, "err", err)
		}
	})
}

// cutBytes cuts s to at most n bytes, at a character's boundary.
func cutBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}

// errStreamsOffAlready leaves the settings as they are: the streams were off already.
var errStreamsOffAlready = errors.New("the streams are off already")

// stallText is the log line of a stalled stream: what it received, how the list in its place
// answered, and what Cogpit does about it.
func stallText(st cache.Stall, confirmed bool) string {
	text := "the server's stream of the initial list (WatchList) stalled: " + st.Err.Error()
	switch {
	case confirmed:
		return text + "; a list answered in " + FormatSpan(st.Listed) +
			": the stream stalled, not the connection; streams are off for this cluster from now on (Cluster Settings › General)"
	case st.ListErr != nil:
		return text + "; the list in its place failed (" + st.ListErr.Error() +
			"): the connection, not the stream, it seems; this connection lists, the next one streams again"
	}
	return text + "; the list in its place took " + FormatSpan(st.Listed) +
		": the connection, not the stream, it seems; this connection lists, the next one streams again"
}

// streamsOffChanged applies a cluster's streams switched off to its connected backend at once:
// its reads list from now on, the streams still running end. Switched back to Automatic, the
// connection keeps listing until the next connect.
func (s *Session) streamsOffChanged(context string, off bool) {
	if !off {
		return
	}
	for _, b := range s.Backends() {
		if b.Context == context && b.Cache != nil && !b.Demo {
			b.Cache.ListFromNowOn()
		}
	}
}

// streamsText is what the initial lists' line in Connection Diagnostics adds while a connection
// lists every read (DiagStreaming.Off): why; "" while it streams.
func streamsText(b *Backend, settings ClusterSettings) string {
	if b == nil || b.Cache == nil || !b.Cache.Listing() {
		return ""
	}
	switch {
	case settings.StreamsOff == nil:
		return "for this connection: a stream stalled"
	case settings.StreamsOff.By == StreamsOffByCogpit:
		return "Cogpit switched them off after a stream stalled (Cluster Settings)"
	}
	return "switched off in Cluster Settings"
}
