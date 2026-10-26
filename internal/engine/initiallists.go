package engine

import (
	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
)

// A cluster's initial lists (D-135; cache/stall.go). By default every read lists its objects once
// and then watches from that list's version (cache.Options.Lists), at the connection's lists'
// budget (cluster: 50 a second, burst 300). Streaming them (client-go's WatchList, Kubernetes'
// WatchListClient, beta) is an opt-in per cluster (ClusterSettings.Streaming, Cluster Settings ›
// General › Debug, cluster.settings.set streamInitialLists): on, from the next connect, each read
// gets its objects over the watch it keeps; a stream that stays silent for the cache's bound before
// its end makes that read and every later read of the connection list instead, and the connection's
// diagnostics log says so. The setting stays as the user chose it. Off applies to the connected
// cluster at once.

// StreamStall is how long a stream may stay silent before its read lists instead
// (cache.DefaultStreamStall).
const StreamStall = cache.DefaultStreamStall

// streamingOf reports whether a context streams its initial lists (its settings' opt-in).
func (c *ClusterSettingsStore) streamingOf(context string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.load() // an unreadable file is reported where it loads: listed then
	return c.by[context].Streaming
}

// attachStreams has the session hear of a connection's stalled streams. Connect and reconnect
// call it before the backend is published.
func (s *Session) attachStreams(b *Backend) {
	if b == nil || b.Cache == nil || b.Demo {
		return
	}
	b.Cache.OnStall(func(st cache.Stall) { s.streamStalled(b, st) })
}

// streamStalled notes a stalled stream in Connection Diagnostics' log. It runs on an informer's
// goroutine.
func (s *Session) streamStalled(b *Backend, st cache.Stall) {
	if log := b.diagLog(); log != nil {
		log.Add(cluster.DiagEntry{Kind: cluster.DiagFallback, What: st.Scope.String(), Text: stallText(st)})
	}
}

// stallText is the log line of a stalled stream: what it received, how the list in its place
// answered, and what Cogpit does about it.
func stallText(st cache.Stall) string {
	text := "the server's stream of the initial list (WatchList, beta) stalled: " + st.Err.Error()
	if st.ListErr != nil {
		text += "; the list in its place failed (" + st.ListErr.Error() + ")"
	} else {
		text += "; a list answered in " + FormatSpan(st.Listed)
	}
	return text + ": this connection lists from now on, the next one streams again"
}

// streamingChanged applies a cluster's streaming switched off to its connected backend at once:
// its reads list from now on, the streams still running end. Switched on, it applies from the
// next connect.
func (s *Session) streamingChanged(context string, on bool) {
	if on {
		return
	}
	for _, b := range s.Backends() {
		if b.Context == context && b.Cache != nil && !b.Demo {
			b.Cache.ListFromNowOn()
		}
	}
}

// streamsText is what the initial lists' line in Connection Diagnostics adds when a connection that
// began streaming lists every read now (DiagStreaming.Off), and why; "" otherwise.
func streamsText(b *Backend, settings ClusterSettings) string {
	if b == nil || b.Cache == nil || !b.Streaming || !b.Cache.Listing() {
		return ""
	}
	if !settings.Streaming {
		return "streaming switched off in Cluster Settings"
	}
	return "for this connection: a stream stalled"
}
