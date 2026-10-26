package engine

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Connection Diagnostics' log (D-133): one cluster.DiagLog per context, kept across the context's
// connections in this run (a reconnect keeps what led to it) and in memory only. At most
// diagLogContexts contexts keep one; the one used least recently goes first (the lead's bound, so
// that a run that connects many contexts does not grow without end, the owner's "Limit").

// diagLogContexts bounds the contexts that keep a diagnostics log.
const diagLogContexts = 8

// diagLogs are the session's diagnostics logs by context id. It has its own lock.
type diagLogs struct {
	mu    sync.Mutex
	byCtx map[string]*cluster.DiagLog
	used  map[string]uint64
	tick  uint64
}

// of returns a context's log, making it when there is none.
func (d *diagLogs) of(contextID string) *cluster.DiagLog {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.byCtx == nil {
		d.byCtx, d.used = map[string]*cluster.DiagLog{}, map[string]uint64{}
	}
	d.tick++
	d.used[contextID] = d.tick
	if l := d.byCtx[contextID]; l != nil {
		return l
	}
	l := cluster.NewDiagLog()
	d.byCtx[contextID] = l
	for len(d.byCtx) > diagLogContexts {
		oldest, at := "", uint64(0)
		for ctx, t := range d.used {
			if ctx != contextID && (oldest == "" || t < at) {
				oldest, at = ctx, t
			}
		}
		delete(d.byCtx, oldest)
		delete(d.used, oldest)
	}
	return l
}

// diagLog is the diagnostics log the connect's connection notes in; nil records in a log of the
// connection's own.
func (r *connectSteps) diagLog() *cluster.DiagLog {
	if r == nil {
		return nil
	}
	return r.diag
}

// wireDiagnostics has a real cluster's cache name its requests by their scope and note in the
// connection's diagnostics log the reads the server did not stream and those slow to load.
func wireDiagnostics(conn *cluster.Conn, ch *cache.Cache) {
	log := conn.DiagLog()
	if log == nil {
		return
	}
	ch.LabelRequests(func(ctx context.Context, s cache.Scope) context.Context {
		return cluster.WithRequestLabel(ctx, s.String())
	})
	ch.OnDiagnostic(func(d cache.Diagnostic) {
		switch d.Kind {
		case cache.DiagStreamFallback:
			log.Add(cluster.DiagEntry{Kind: cluster.DiagFallback, What: d.Scope.String(), Text: fallbackText(d.Err)})
		case cache.DiagLoaded:
			if d.Took >= cluster.SlowLoad {
				log.Add(cluster.DiagEntry{Kind: cluster.DiagSlowLoad, What: d.Scope.String(), Took: d.Took,
					Text: "the initial list took long; the requests above say why"})
			}
		}
	})
}

// fallbackText says why a read's initial list was listed instead of streamed: the server's error,
// or a stream that ended early. A stream the cache ended because it stalled has its own line
// (initiallists.go, D-135).
func fallbackText(err error) string {
	if err != nil {
		return "the server did not stream the initial list (WatchList); listed instead: " + err.Error()
	}
	return "the server did not stream the initial list (WatchList); listed instead: the stream ended before the list was complete"
}

// noteNamespaced writes in the diagnostics log that a kind is read namespace by namespace now,
// after the API server refused its read in all namespaces.
func (b *Backend) noteNamespaced(p *model.KindProfile) {
	log := b.diagLog()
	if log == nil {
		return
	}
	text := "the user may not list it in all namespaces: read namespace by namespace"
	if ns := b.namedNamespaces(); len(ns) > 0 {
		text += ", in " + strings.Join(ns, ", ")
	}
	log.Add(cluster.DiagEntry{Kind: cluster.DiagNamespaced, What: p.Resource + groupSuffix(p.Group), Text: text})
}

// diagLog is the backend's diagnostics log; nil for the demo cluster and a backend without a
// connection.
func (b *Backend) diagLog() *cluster.DiagLog {
	if b == nil || b.Conn == nil {
		return nil
	}
	return b.Conn.DiagLog()
}

// groupSuffix is ".<group>" for a resource outside the core group.
func groupSuffix(group string) string {
	if group == "" {
		return ""
	}
	return "." + group
}

// errNoDiagnostics is the report's error for a cluster without a connection's log (the demo
// cluster).
var errNoDiagnostics = errors.New("the demo cluster has no connection to diagnose")
