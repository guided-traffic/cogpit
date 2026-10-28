package metrics

import (
	"context"
	"errors"
	"net"
	"strconv"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// The status line under the drawer's charts (G12, owner 2026-10-12, evening): for each of a
// cluster's providers, whether Cogpit reached it and whether it answered as expected, when it last
// did, the last failure, and what it serves now. Agents read the same in metrics.get and ui.state.

// The roles a provider can serve (ProviderStatus.Serves).
const (
	// ServesHistory: the charts' history (a range query when a drawer opens or a range changes).
	ServesHistory = "history"
	// ServesLive: the current values (the lists, and the charts after the history).
	ServesLive = "live"
	// ServesNetwork and ServesFilesystem: the variants only Prometheus has.
	ServesNetwork    = "network"
	ServesFilesystem = "filesystem"
)

// ProviderStatus is what Cogpit knows about one of a cluster's metrics providers. It never carries
// a credential, a response body or a value from a Secret: LastError is a class and a short reason.
type ProviderStatus struct {
	// Provider is the provider's id: "prometheus", "metrics-server".
	Provider string `json:"provider"`
	// Label names it: "Prometheus", "Kubernetes Metrics Server".
	Label string `json:"label"`
	// Service is where a Prometheus was found, as Lens writes it: "monitoring/prometheus-server:http"
	// (namespace/service:port, the port by name or number); empty for the Metrics Server and while
	// none is found.
	Service string `json:"service,omitempty"`
	// Namespace and ServiceName are where a found Prometheus runs, for the links of the source line
	// (the drawer's and the Overview's: the Namespace and the Service open); empty otherwise.
	Namespace   string `json:"namespace,omitempty"`
	ServiceName string `json:"serviceName,omitempty"`
	// Status: available (found and answering), notServed (not found), forbidden, unavailable (found
	// but not answering), unknown (not asked yet: Prometheus is looked for when a drawer opens or the
	// Metrics Server fails).
	Status Status `json:"status"`
	// LastOK is when it last answered as expected; zero when it never did.
	LastOK time.Time `json:"lastOK,omitzero"`
	// LastError is the last failure as a class and a short reason ("unavailable (HTTP 503)",
	// "forbidden (HTTP 403)", "timeout"); LastErrorAt is when it happened. A provider that answered
	// since keeps them, so that the status line can say what happened last.
	LastError   string    `json:"lastError,omitempty"`
	LastErrorAt time.Time `json:"lastErrorAt,omitzero"`
	// Serves is what it serves now: "history", "live", "network", "filesystem"; empty while it
	// serves nothing (not found, failing, or not needed).
	Serves []string `json:"serves,omitempty"`
	// Unused says the charts the status line is under never ask it, whatever its state: the
	// Metrics Server under a volume's (ProvidersFor).
	Unused bool `json:"unused,omitempty"`
}

// Identified is a Detector that knows which provider it looks for, so that the status line can name
// the provider before (and while) none is found. The Metrics Server and PrometheusDetector are.
type Identified interface {
	ID() string
	Label() string
}

// Located is a provider found at a place the status line names: a Prometheus's
// "namespace/service:port".
type Located interface {
	Where() string
}

// ServiceLocated is a provider reached through a Service of the cluster: a Prometheus. The source
// line links its namespace and its name.
type ServiceLocated interface {
	ServiceRef() (namespace, name string)
}

// Named is a found provider whose name differs from its detector's: a Thanos or a VictoriaMetrics
// found where a Prometheus was looked for (promflavor.go). The status line names it so.
type Named interface {
	Name() string
}

// errorClass is a failed request as a class and a short reason for the status line: the HTTP status
// the API server or the service proxy answered, a timeout, a refused connection, an answer Cogpit
// cannot read. It never includes the error's text, which for a proxied answer can carry the
// service's response body.
func errorClass(err error) string {
	var se *StateError
	if errors.As(err, &se) && se.State.Reason != "" {
		return se.State.Reason
	}
	var st apierrors.APIStatus
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded), apierrors.IsTimeout(err), apierrors.IsServerTimeout(err):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, errTooLarge):
		return "answer too large"
	case errors.Is(err, errUnreadable):
		return "unreadable answer"
	case errors.As(err, &st) && st.Status().Code != 0:
		return httpClass(int(st.Status().Code))
	}
	var nerr net.Error
	if errors.As(err, &nerr) {
		if nerr.Timeout() {
			return "timeout"
		}
		return "connection failed"
	}
	var oerr *net.OpError
	if errors.As(err, &oerr) {
		return "connection failed"
	}
	return "request failed"
}

// httpClass names an HTTP status the way the status line shows it.
func httpClass(code int) string {
	name := ""
	switch code {
	case 401:
		name = "unauthorized"
	case 403:
		name = "forbidden"
	case 404:
		name = "not found"
	case 429:
		name = "too many requests"
	case 500:
		name = "server error"
	case 502:
		name = "bad gateway"
	case 503:
		name = "unavailable"
	case 504:
		name = "gateway timeout"
	}
	if name == "" {
		return "HTTP " + strconv.Itoa(code)
	}
	return name + " (HTTP " + strconv.Itoa(code) + ")"
}

// errTooLarge and errUnreadable are failures of an answer, not of the request.
var (
	errTooLarge   = errors.New("the answer is too large")
	errUnreadable = errors.New("the answer is not readable")
)
