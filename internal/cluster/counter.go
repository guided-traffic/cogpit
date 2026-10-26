package cluster

import (
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// Request verbs a RequestCounter counts by. They follow the API server's own request verbs, plus
// "watchlist" for the streaming initial list and "discovery" for discovery and OpenAPI documents.
const (
	VerbList             = "list"
	VerbWatch            = "watch"
	VerbWatchList        = "watchlist"
	VerbGet              = "get"
	VerbCreate           = "create"
	VerbUpdate           = "update"
	VerbPatch            = "patch"
	VerbDelete           = "delete"
	VerbDeleteCollection = "deletecollection"
	VerbDiscovery        = "discovery"
	VerbOther            = "other"
)

// RequestStats is a snapshot of the API requests one connection has made.
type RequestStats struct {
	// Total is the number of requests, watches included.
	Total int64 `json:"total"`
	// ByVerb counts requests by verb: "list", "watch", "watchlist", "get", "create", "update",
	// "patch", "delete", "deletecollection", "discovery", "other".
	ByVerb map[string]int64 `json:"byVerb"`
	// ByVerbResource counts requests by "verb resource": "list pods", "watch services",
	// "get pods/log", "watchlist deployments.apps". A resource outside the core group carries its
	// group after a dot, the way kubectl names it; a subresource follows a slash. Requests without
	// a resource (discovery, other) count under the verb alone.
	ByVerbResource map[string]int64 `json:"byVerbResource"`
}

// RequestCounter counts the API requests of one connection, by verb and resource. It is the
// evidence for the API-load budgets P9–P11 and feeds the status bar. It looks only at the method
// and the URL, never at headers or bodies, so it cannot see a credential. It is safe for
// concurrent use.
type RequestCounter struct {
	// prefix is the path of the server URL (a cluster behind a proxy such as
	// https://proxy.example/k8s/clusters/c-1); it is stripped before a request is classified.
	// Set once by Connect before the first request.
	prefix string

	mu             sync.Mutex
	total          int64
	byVerb         map[string]int64
	byVerbResource map[string]int64
}

// NewRequestCounter returns an empty counter.
func NewRequestCounter() *RequestCounter {
	return &RequestCounter{byVerb: map[string]int64{}, byVerbResource: map[string]int64{}}
}

// Wrap returns a round-tripper that counts every request before passing it to next. Install it
// with rest.Config.Wrap.
func (rc *RequestCounter) Wrap(next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	return &countingRoundTripper{rc: rc, next: next}
}

// Record counts one request. Wrap calls it; it is exported for clients that do not go through a
// rest.Config transport.
func (rc *RequestCounter) Record(method string, u *url.URL) {
	if rc.prefix != "" && u != nil && strings.HasPrefix(u.Path, rc.prefix+"/") {
		stripped := *u
		stripped.Path = strings.TrimPrefix(u.Path, rc.prefix)
		u = &stripped
	}
	verb, resource := Classify(method, u)
	key := verb
	if resource != "" {
		key = verb + " " + resource
	}
	rc.mu.Lock()
	rc.total++
	rc.byVerb[verb]++
	rc.byVerbResource[key]++
	rc.mu.Unlock()
}

// Snapshot returns the counts so far. The maps are copies the caller owns.
func (rc *RequestCounter) Snapshot() RequestStats {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	st := RequestStats{
		Total:          rc.total,
		ByVerb:         make(map[string]int64, len(rc.byVerb)),
		ByVerbResource: make(map[string]int64, len(rc.byVerbResource)),
	}
	for k, v := range rc.byVerb {
		st.ByVerb[k] = v
	}
	for k, v := range rc.byVerbResource {
		st.ByVerbResource[k] = v
	}
	return st
}

// countingRoundTripper counts, then delegates. It implements the optional interfaces client-go
// and net/http look for on a transport.
type countingRoundTripper struct {
	rc   *RequestCounter
	next http.RoundTripper
}

func (c *countingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	c.rc.Record(req.Method, req.URL)
	return c.next.RoundTrip(req)
}

// WrappedRoundTripper lets client-go (utilnet.RoundTripperWrapper) find the transport below.
func (c *countingRoundTripper) WrappedRoundTripper() http.RoundTripper { return c.next }

// CloseIdleConnections lets http.Client.CloseIdleConnections reach the transport below.
func (c *countingRoundTripper) CloseIdleConnections() {
	if ci, ok := c.next.(interface{ CloseIdleConnections() }); ok {
		ci.CloseIdleConnections()
	}
}

// watchSegment is the deprecated /watch/ path prefix: GET /api/v1/watch/pods.
const watchSegment = "watch"

// namespaceSubresources are the subresources of a Namespace object; for them
// /api/v1/namespaces/<name>/<x> names the namespace, not a resource in it (the API server's
// request-info rule).
var namespaceSubresources = map[string]bool{"status": true, "finalize": true}

// Classify returns the verb and resource of an API request from its method and URL, following
// the API server's own request-info rules. The resource is "" for discovery and unknown paths.
func Classify(method string, u *url.URL) (verb, resource string) {
	if u == nil {
		return VerbOther, ""
	}
	parts := splitPath(u.Path)
	if len(parts) == 0 {
		return VerbOther, ""
	}
	var group string
	switch parts[0] {
	case "version", "openapi":
		return VerbDiscovery, ""
	case "api":
		// /api, /api/v1: discovery. /api/v1/<rest>: a resource.
		if len(parts) <= 2 {
			return VerbDiscovery, ""
		}
		parts = parts[2:]
	case "apis":
		// /apis, /apis/<group>, /apis/<group>/<version>: discovery.
		if len(parts) <= 3 {
			return VerbDiscovery, ""
		}
		group = parts[1]
		parts = parts[3:]
	default:
		return VerbOther, ""
	}

	// The deprecated /watch/ path prefix: GET /api/v1/watch/pods.
	legacyWatch := false
	if parts[0] == watchSegment {
		legacyWatch = true
		parts = parts[1:]
		if len(parts) == 0 {
			return VerbOther, ""
		}
	}
	// /namespaces/<ns>/<resource>/... names a resource in a namespace, except for a namespace's
	// own subresources.
	if parts[0] == "namespaces" && len(parts) > 2 && !namespaceSubresources[parts[2]] {
		parts = parts[2:]
	}
	res, name, sub := parts[0], "", ""
	if len(parts) > 1 {
		name = parts[1]
	}
	if len(parts) > 2 {
		sub = parts[2]
	}

	resource = res
	if group != "" {
		resource += "." + group
	}
	if sub != "" {
		resource += "/" + sub
	}

	q := u.Query()
	switch method {
	case http.MethodGet, http.MethodHead, "":
		switch {
		case legacyWatch || isTrue(q.Get("watch")):
			if isTrue(q.Get("sendInitialEvents")) {
				return VerbWatchList, resource
			}
			return VerbWatch, resource
		case name != "":
			return VerbGet, resource
		default:
			return VerbList, resource
		}
	case http.MethodPost:
		return VerbCreate, resource
	case http.MethodPut:
		return VerbUpdate, resource
	case http.MethodPatch:
		return VerbPatch, resource
	case http.MethodDelete:
		if name == "" {
			return VerbDeleteCollection, resource
		}
		return VerbDelete, resource
	default:
		return VerbOther, resource
	}
}

func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

// isTrue reads a boolean query parameter the way the API server does.
func isTrue(v string) bool {
	switch strings.ToLower(v) {
	case "true", "1":
		return true
	}
	return false
}
