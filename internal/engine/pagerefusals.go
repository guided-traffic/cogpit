package engine

import (
	"sync/atomic"

	"github.com/guided-traffic/cogpit/internal/cache"
)

// A read the API server refuses is given back and shown (D-177): a refused scope a handle holds is
// listed again at every step of client-go's backoff for as long as it is held, so a reader that
// holds its handles while it is shown (a list view and its related objects' reads, a drawer, the
// Workloads and cluster Overviews, the Flux and Argo CD dashboards, the Image Security and Velero
// pages, the Trivy integration) gives a refused one, or a refused namespace of one,
// back with its refusal kept (cache.Handle.ReleaseWithErr, ReleaseRefusedNamespaces): the handle
// goes on reporting the refusal, which the reader shows, and nothing refused is listed again.
// cluster.refreshAccess reads it again: the reader starts again once the refusals were cleared
// (retake). The Kyverno integration does the same with reads of its own (kyverno.go, dropRefused).

// refusedReads is what a reader keeps of the reads it gave back as refused.
type refusedReads struct {
	gen  uint64      // the backend's refusals generation when the reader started (nsState.refusalsGen)
	gave atomic.Bool // a read went back as refused
}

// startAt notes the backend's refusals generation, read before the reader acquired.
func (r *refusedReads) startAt(gen uint64) { r.gen = gen }

// giveBack gives back each handle of hs (nil skipped) whose read the API server refused wholly
// (refusedAll: its one scope, or every namespace it reads), its refusal kept, and of a read
// namespace by namespace each refused namespace, the others read on; it reports whether it gave
// one back now. A pending handle has no error and stays. A namespaced kind's read in all
// namespaces given back is followed: the cache told the backend of the refusal (OnRefused) before
// the handle's error shows, so the reader's plan key changes and it starts again namespace by
// namespace.
func (r *refusedReads) giveBack(hs ...*cache.Handle) bool {
	gave := false
	for _, h := range hs {
		switch {
		case h == nil:
		case refusedAll(h) && h.ReleaseWithErr():
			gave = true
		case h.ReleaseRefusedNamespaces() > 0:
			gave = true
		}
	}
	if gave {
		r.gave.Store(true)
	}
	return gave
}

// retake reports whether the reader gave a read back and cluster.refreshAccess cleared the
// refusals since: it starts again and reads it again.
func (r *refusedReads) retake(b *Backend) bool {
	return b != nil && r.gave.Load() && b.ns.refusalsGen() != r.gen
}
