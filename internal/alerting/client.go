package alerting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/client-go/kubernetes"
)

// alertsPath is the request Cogpit polls an Alertmanager with, with alertsParams: the active alerts
// that are not inhibited, the silenced ones included and told apart by their status (Alertmanager
// API v2, since Alertmanager 0.16). It doubles as detection's probe. The silences page adds
// silencesPath while it is shown, and the user's silences are the only writes (silences.go): no
// other path is asked, so the configuration that /api/v2/status would return (its receivers,
// routes and URLs) never reaches Cogpit (D-77, D-166).
const alertsPath = "/api/v2/alerts"

var alertsParams = map[string]string{"active": "true", "silenced": "true", "inhibited": "false"}

// proxy sends requests to a service through the API server's service proxy: get a GET and returns
// the body, at most limit bytes; send another method with an optional JSON body, and returns the
// body and, for an answer that is no success, a *RequestError that names the request.
type proxy interface {
	get(ctx context.Context, t Target, path string, params map[string]string, limit int64) ([]byte, error)
	send(ctx context.Context, t Target, method, path string, body []byte, limit int64) ([]byte, error)
}

// kubeProxy is the production proxy: client-go's service ProxyGet on the connection's own clients,
// so the request is counted ("get services/proxy"), rate limited and authenticated like every
// other.
type kubeProxy struct{ kube kubernetes.Interface }

func (k kubeProxy) get(ctx context.Context, t Target, path string, params map[string]string, limit int64) ([]byte, error) {
	body, err := k.kube.CoreV1().Services(t.Namespace).ProxyGet(t.Scheme, t.Service, t.Port, t.prefix()+path, params).Stream(ctx)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	b, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%w: larger than %d MiB", errTooLarge, limit>>20)
	}
	return b, nil
}

// send implements proxy: the request through client-go's REST client of the core API, the service
// proxy's path built as ProxyGet builds it, so the request is counted ("create services/proxy" for
// a POST, "delete services/proxy" for a DELETE), rate limited and authenticated like every other.
func (k kubeProxy) send(ctx context.Context, t Target, method, path string, body []byte, limit int64) ([]byte, error) {
	req := k.kube.CoreV1().RESTClient().Verb(method).Namespace(t.Namespace).Resource("services").
		Name(utilnet.JoinSchemeNamePort(t.Scheme, t.Service, t.Port)).SubResource("proxy").Suffix(t.prefix() + path)
	if body != nil {
		req = req.SetHeader("Content-Type", "application/json").Body(body)
	}
	url := req.URL().String()
	answer, err := req.DoRaw(ctx)
	if int64(len(answer)) > limit {
		answer = answer[:limit]
	}
	if err != nil {
		return nil, newRequestError(method, url, t, err, answer)
	}
	return answer, nil
}

// RequestError is a write to Alertmanager that failed (a silence created or expired): it names
// the request (the method, the whole URL through the API server's service proxy, the Alertmanager
// it reaches), the answer's status, and the answer: the API server's own message when it refused
// (a 403 of RBAC), else Alertmanager's own text, one line of at most maxAnswer bytes (Alertmanager
// says in it why it refuses a silence). Never a credential: the request carries none in its URL.
type RequestError struct {
	Method string
	URL    string
	Where  string // the Alertmanager, Target.Where
	Status string // "forbidden (HTTP 403)", "timeout", …
	Answer string
	err    error
}

// maxAnswer bounds the answer a RequestError quotes.
const maxAnswer = 300

func newRequestError(method, url string, t Target, err error, answer []byte) *RequestError {
	e := &RequestError{Method: method, URL: url, Where: t.Where(), Status: errorClass(err), err: err}
	switch {
	case fromService(err) || len(answer) > 0 && !isStatus(answer):
		e.Answer = oneLine(string(answer), maxAnswer)
	default:
		var st apierrors.APIStatus
		if errors.As(err, &st) {
			e.Answer = oneLine(st.Status().Message, maxAnswer)
		}
	}
	return e
}

// Error implements error.
func (e *RequestError) Error() string {
	s := fmt.Sprintf("%s %s (Alertmanager %s): %s", e.Method, e.URL, e.Where, e.Status)
	if e.Answer != "" {
		s += ": " + e.Answer
	}
	return s
}

// Unwrap returns the request's error.
func (e *RequestError) Unwrap() error { return e.err }

// isStatus reports an answer that is the API server's Status object rather than the service's own.
func isStatus(b []byte) bool {
	var st struct {
		Kind string `json:"kind"`
	}
	return json.Unmarshal(b, &st) == nil && st.Kind == "Status"
}

// oneLine makes text one line of at most n bytes: its whitespace runs one space, a JSON string's
// quotes (Alertmanager answers its errors as one) gone.
func oneLine(text string, n int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 1 && text[0] == '"' && text[len(text)-1] == '"' {
		var unq string
		if json.Unmarshal([]byte(text), &unq) == nil {
			text = strings.Join(strings.Fields(unq), " ")
		}
	}
	if len(text) > n {
		return clip(text, n) + "…"
	}
	return text
}

// errTooLarge is an answer beyond Options.MaxBytes.
var errTooLarge = errors.New("the answer is too large")

// errorText is the API server's own message of a failed request ("services \"x\" is forbidden: User
// ... cannot get resource \"services/proxy\" ..."), else the failure's class. An answer that was
// not a Status (the proxied service's own body, which client-go quotes in the error) is never
// quoted: its class stands in, so that nothing Alertmanager or another service answered reaches a
// state line, a log or an agent.
func errorText(err error) string {
	var st apierrors.APIStatus
	if errors.As(err, &st) {
		s := st.Status()
		if s.Details != nil {
			for _, c := range s.Details.Causes {
				if c.Type == metav1.CauseTypeUnexpectedServerResponse {
					return errorClass(err)
				}
			}
		}
		if s.Message != "" && s.Code < 500 || s.Reason == metav1.StatusReasonServiceUnavailable && s.Message != "" {
			return s.Message
		}
	}
	return errorClass(err)
}

// errorClass names a failed request by its class: the HTTP status the API server or the service
// proxy answered, a timeout, a refused connection, an answer Cogpit cannot read. It never includes
// the error's text.
func errorClass(err error) string {
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
	case errors.Is(err, errNotAlertmanager):
		return "not an Alertmanager's answer"
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

// httpClass names an HTTP status the way the state lines show it.
func httpClass(code int) string {
	name := map[int]string{401: "unauthorized", 403: "forbidden", 404: "not found", 429: "too many requests",
		500: "server error", 502: "bad gateway", 503: "unavailable", 504: "gateway timeout"}[code]
	if name == "" {
		return "HTTP " + strconv.Itoa(code)
	}
	return name + " (HTTP " + strconv.Itoa(code) + ")"
}

// forbidden reports a refusal of the service proxy itself: the user may not get services/proxy.
// A 401 or 403 the proxied service answered with a body of its own (an authenticating proxy in
// front of Alertmanager) is not: the service does not answer Cogpit, which is "unavailable".
func forbidden(err error) bool {
	return (apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err)) && !fromService(err)
}

// fromService reports an error whose answer came from the proxied service, not from the API server.
func fromService(err error) bool {
	var st apierrors.APIStatus
	if !errors.As(err, &st) || st.Status().Details == nil {
		return false
	}
	for _, c := range st.Status().Details.Causes {
		if c.Type == metav1.CauseTypeUnexpectedServerResponse {
			return true
		}
	}
	return false
}
