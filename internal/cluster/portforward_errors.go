package cluster

import (
	"errors"
	"net/http"
	"sync/atomic"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// DialRefusedError is a port-forward dial the server (or a proxy before it) answered with an HTTP
// status instead of the upgrade. client-go's SPDY dialer returns that as a plain error holding the
// server's message only; this keeps the status code so that a refusal can be told from a passing
// failure (IsPermanentForwardError). The text is the dialer's, unchanged.
type DialRefusedError struct {
	StatusCode int
	Err        error
}

func (e *DialRefusedError) Error() string { return e.Err.Error() }
func (e *DialRefusedError) Unwrap() error { return e.Err }

// statusRecorder is the RoundTripper under the SPDY dialer's client; it remembers the status code
// of the last response.
type statusRecorder struct {
	next http.RoundTripper
	code atomic.Int32
}

func (r *statusRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := r.next.RoundTrip(req)
	if err == nil && resp != nil {
		r.code.Store(int32(resp.StatusCode)) //nolint:gosec // an HTTP status code
	}
	return resp, err
}

// refusal wraps err in a *DialRefusedError when the last response was an error status; other
// errors (no response, the upgrade itself failing) pass unchanged.
func (r *statusRecorder) refusal(err error) error {
	if r == nil || err == nil {
		return err
	}
	if code := int(r.code.Load()); code >= http.StatusBadRequest {
		return &DialRefusedError{StatusCode: code, Err: err}
	}
	return err
}

// IsPermanentForwardError reports whether err, returned by DialPortForward, is one that waiting
// does not cure, so that a forward fails at once and frees its local port instead of retrying
// (D-20, owner). The list is what the API server answers to the portforward subresource:
//
//   - 404 Not Found: the pod does not exist (any more), or its namespace is gone;
//   - 403 Forbidden: the user may not create pods/portforward there (RBAC does not change by waiting);
//   - 401 Unauthorized: the credentials were rejected, after client-go's own refresh.
//
// The code comes from DialRefusedError (the SPDY dialer, which is what a refusal ends in: the
// WebSocket upgrade failure falls back to it) or from a typed *apierrors.StatusError.
//
// Everything else is retried with the backoff: a network error, a dial timeout, 5xx and 429 (the
// server is busy or restarting), and 400 Bad Request, which is what the API server answers while
// a pod is not running yet ("pod does not have a host assigned") and so passes by itself. A
// refused connection to the pod's port is not a dial error at all: the application may still be
// starting, and the forward goes on reconnecting. An error that carries no status (a proxy that
// resets the connection) is retried.
func IsPermanentForwardError(err error) bool {
	if err == nil {
		return false
	}
	var refused *DialRefusedError
	if errors.As(err, &refused) {
		switch refused.StatusCode {
		case http.StatusNotFound, http.StatusForbidden, http.StatusUnauthorized:
			return true
		}
		return false
	}
	return apierrors.IsNotFound(err) || apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err)
}
