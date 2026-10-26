package testkit

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
)

// FakeResource is one resource a FakeAPIServer serves.
type FakeResource struct {
	GVR        schema.GroupVersionResource
	Kind       string
	Namespaced bool
}

// DefaultFakeResources are what a FakeAPIServer serves unless told otherwise.
var DefaultFakeResources = []FakeResource{
	{GVR: PodsGVR, Kind: "Pod", Namespaced: true},
	{GVR: ServicesGVR, Kind: "Service", Namespaced: true},
	{GVR: NamespacesGVR, Kind: "Namespace"},
	{GVR: DeploymentsGVR, Kind: "Deployment", Namespaced: true},
}

// FakeAPIOptions configure a FakeAPIServer.
type FakeAPIOptions struct {
	// Token, when set, is the bearer token every request must carry; others get 401.
	Token string
	// DisableWatchList makes the server reject watches with sendInitialEvents=true, like a server
	// without the WatchList feature; client-go then falls back to a list and a watch.
	DisableWatchList bool
	// StallWatchList makes streamed initial lists stall, as some clusters' do (D-135): the server
	// answers and sends the objects, then never the bookmark that ends them. Lists and plain
	// watches answer as before.
	StallWatchList bool
	// WarmingCaches are resources ("pods", "deployments.apps") whose first watch the server
	// answers 429 "storage is (re)initializing" with Retry-After 1, as an API server does while it
	// builds its cache of the resource (D-135); every request after it is answered.
	WarmingCaches []string
	// BrokenGroups are API groups listed in /apis whose discovery answers 503, like an aggregated
	// API whose backend is down.
	BrokenGroups []string
	// Resources are the resources served; nil means DefaultFakeResources.
	Resources []FakeResource
	// Access, when set, answers access reviews from its rules (fakeaccess.go); nil answers them
	// 404, as a server that does not serve them, counted as the client counts them.
	Access *FakeAccess
	// Identity is who the server says its user is, the answer to a SelfSubjectReview
	// (fakewhoami.go); nil answers as DefaultFakeIdentity.
	Identity *FakeIdentity
	// ClientCert, when set, is the client certificate login the server accepts: every request
	// must come over a connection that presented a certificate it accepts (the leaf); others get
	// 401, as an API server answers a certificate that expired or that its client CA did not sign.
	// SetClientCert changes it.
	ClientCert func(*x509.Certificate) bool
}

// FakeAPIServer is a minimal Kubernetes API server over HTTPS (httptest, HTTP/2), for tests that
// measure what a real client-go stack sends: discovery (/version, /api, /apis), list, get and
// watch with resourceVersions, watch streams that stay open (streamed JSON), and streaming initial lists
// (sendInitialEvents with the initial-events-end bookmark). Tests add, update and delete objects
// while clients watch. It counts the requests it serves, by "verb resource".
type FakeAPIServer struct {
	// URL is the server's base URL.
	URL string

	srv      *httptest.Server
	opts     FakeAPIOptions
	stopping chan struct{}
	stopOnce sync.Once

	mu        sync.Mutex
	rv        int64
	resources map[schema.GroupVersionResource]FakeResource
	objs      map[schema.GroupVersionResource]map[string]*unstructured.Unstructured
	history   map[schema.GroupVersionResource][]memEvent
	watchers  map[*apiWatcher]struct{}
	requests  map[string]int64
	warmed    map[string]bool         // the WarmingCaches whose first watch was answered 429
	proxies   map[string]http.Handler // "namespace/name:port" -> the service behind the proxy
	scoped    map[string]int64        // resource reads by "verb resource" and namespace (nsrefusing.go)
	logs      fakeLogs                // the containers' logs (fakelogs.go)
}

type apiWatcher struct {
	gvr      schema.GroupVersionResource
	req      fakeRequest // namespace and label selection; label changes are not turned into adds and deletes
	ch       chan memEvent
	overflow chan struct{}
	once     sync.Once
}

// NewFakeAPIServer starts a server; Close stops it.
func NewFakeAPIServer(opts FakeAPIOptions) *FakeAPIServer {
	if opts.Resources == nil {
		opts.Resources = DefaultFakeResources
	}
	s := &FakeAPIServer{
		opts:      opts,
		stopping:  make(chan struct{}),
		resources: map[schema.GroupVersionResource]FakeResource{},
		objs:      map[schema.GroupVersionResource]map[string]*unstructured.Unstructured{},
		history:   map[schema.GroupVersionResource][]memEvent{},
		watchers:  map[*apiWatcher]struct{}{},
		requests:  map[string]int64{},
		proxies:   map[string]http.Handler{},
	}
	for _, r := range opts.Resources {
		s.resources[r.GVR] = r
	}
	// TLS, because clientcmd sends user credentials only to https servers, and HTTP/2, because
	// that is how client-go talks to a real API server (one connection, watches as streams).
	s.srv = httptest.NewUnstartedServer(http.HandlerFunc(s.serveHTTP))
	s.srv.EnableHTTP2 = true
	if opts.ClientCert != nil { // the handshake asks for a certificate; serveHTTP decides
		s.srv.TLS = &tls.Config{ClientAuth: tls.RequestClientCert, MinVersion: tls.VersionTLS12}
	}
	s.srv.StartTLS()
	s.URL = s.srv.URL
	return s
}

// CAData returns the PEM certificate clients must trust to talk to the server.
func (s *FakeAPIServer) CAData() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.srv.Certificate().Raw})
}

// Close ends every open watch and stops the server.
func (s *FakeAPIServer) Close() {
	s.stopOnce.Do(func() { close(s.stopping) })
	s.srv.Close()
}

// Set adds or replaces an object, assigns its resourceVersion and sends ADDED or MODIFIED to the
// open watches.
func (s *FakeAPIServer) Set(gvr schema.GroupVersionResource, obj *unstructured.Unstructured) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rv++
	obj = obj.DeepCopy()
	obj.SetResourceVersion(strconv.FormatInt(s.rv, 10))
	byKey := s.objs[gvr]
	if byKey == nil {
		byKey = map[string]*unstructured.Unstructured{}
		s.objs[gvr] = byKey
	}
	key := objKey(obj.GetNamespace(), obj.GetName())
	typ := watch.Added
	if _, ok := byKey[key]; ok {
		typ = watch.Modified
	}
	byKey[key] = obj
	s.publish(gvr, memEvent{rv: s.rv, typ: typ, obj: obj})
}

// Delete removes an object and sends DELETED to the open watches.
func (s *FakeAPIServer) Delete(gvr schema.GroupVersionResource, namespace, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := objKey(namespace, name)
	obj, ok := s.objs[gvr][key]
	if !ok {
		return
	}
	delete(s.objs[gvr], key)
	s.rv++
	obj = obj.DeepCopy()
	obj.SetResourceVersion(strconv.FormatInt(s.rv, 10))
	s.publish(gvr, memEvent{rv: s.rv, typ: watch.Deleted, obj: obj})
}

// publish records an event and hands it to the matching watches. Called with s.mu held. A watch
// whose buffer is full is ended, as a real server ends a watcher that cannot keep up.
func (s *FakeAPIServer) publish(gvr schema.GroupVersionResource, ev memEvent) {
	s.history[gvr] = append(s.history[gvr], ev)
	for w := range s.watchers {
		if w.gvr != gvr || !w.req.matches(ev.obj) {
			continue
		}
		select {
		case w.ch <- ev:
		default:
			w.once.Do(func() { close(w.overflow) })
		}
	}
}

// Requests returns how many requests the server served, by "verb resource" in the format of
// cluster.RequestStats.ByVerbResource ("list pods", "watchlist deployments.apps", "discovery").
func (s *FakeAPIServer) Requests() map[string]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int64, len(s.requests))
	for k, v := range s.requests {
		out[k] = v
	}
	return out
}

// OpenWatches returns the number of watch streams open right now.
func (s *FakeAPIServer) OpenWatches() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.watchers)
}

// OpenWatchesBy returns the watch streams open right now by resource, named as in Requests
// without the verb ("secrets", "secrets with owner=helm", "secrets (metadata)").
func (s *FakeAPIServer) OpenWatchesBy() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]int{}
	for w := range s.watchers {
		out[w.req.key]++
	}
	return out
}

// SetToken changes the bearer token the server accepts: the token a client holds stops working, as
// when a cluster rotates or revokes it.
func (s *FakeAPIServer) SetToken(token string) {
	s.mu.Lock()
	s.opts.Token = token
	s.mu.Unlock()
}

func (s *FakeAPIServer) token() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.opts.Token
}

// SetClientCert changes the client certificates the server accepts (a server started with
// ClientCert only): a connection whose certificate it no longer accepts gets 401, as when the
// certificate expired.
func (s *FakeAPIServer) SetClientCert(accept func(*x509.Certificate) bool) {
	s.mu.Lock()
	s.opts.ClientCert = accept
	s.mu.Unlock()
}

// clientCertRejected reports whether the server checks client certificates and r's connection
// presented none it accepts.
func (s *FakeAPIServer) clientCertRejected(r *http.Request) bool {
	s.mu.Lock()
	accept := s.opts.ClientCert
	s.mu.Unlock()
	if accept == nil {
		return false
	}
	return r.TLS == nil || len(r.TLS.PeerCertificates) == 0 || !accept(r.TLS.PeerCertificates[0])
}

// Kubeconfig returns a kubeconfig document whose one context, contextName, points at the server
// (trusting its certificate) with the server's token and the given default namespace (empty:
// none).
func (s *FakeAPIServer) Kubeconfig(contextName, namespace string) []byte {
	return s.KubeconfigWithToken(contextName, namespace, s.token())
}

// KubeconfigWithToken is Kubeconfig with another bearer token (empty: none), for tests of
// rejected credentials.
func (s *FakeAPIServer) KubeconfigWithToken(contextName, namespace, token string) []byte {
	user := "{}"
	if token != "" {
		user = "\n    token: " + token
	}
	ns := ""
	if namespace != "" {
		ns = "\n    namespace: " + namespace
	}
	return fmt.Appendf(nil, `apiVersion: v1
kind: Config
current-context: %[1]s
clusters:
- name: fake
  cluster:
    server: %[2]s
    certificate-authority-data: %[5]s
users:
- name: fake
  user: %[3]s
contexts:
- name: %[1]s
  context:
    cluster: fake
    user: fake%[4]s
`, contextName, s.URL, user, ns, base64.StdEncoding.EncodeToString(s.CAData()))
}

// KubeconfigWithClientCert is Kubeconfig whose user logs in with a client certificate and its key,
// both in the file (client-certificate-data, client-key-data), as a cloud's kubeconfig download
// holds them: for tests of a certificate renewed in the file (D-81).
func (s *FakeAPIServer) KubeconfigWithClientCert(contextName string, certPEM, keyPEM []byte) []byte {
	return fmt.Appendf(nil, `apiVersion: v1
kind: Config
current-context: %[1]s
clusters:
- name: fake
  cluster:
    server: %[2]s
    certificate-authority-data: %[3]s
users:
- name: fake
  user:
    client-certificate-data: %[4]s
    client-key-data: %[5]s
contexts:
- name: %[1]s
  context:
    cluster: fake
    user: fake
`, contextName, s.URL, base64.StdEncoding.EncodeToString(s.CAData()),
		base64.StdEncoding.EncodeToString(certPEM), base64.StdEncoding.EncodeToString(keyPEM))
}

// WriteKubeconfig writes Kubeconfig to a file in t.TempDir() and returns its path.
func (s *FakeAPIServer) WriteKubeconfig(t testing.TB, contextName, namespace string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(p, s.Kubeconfig(contextName, namespace), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return p
}

// fakeRequest is a parsed resource request.
type fakeRequest struct {
	res       FakeResource
	namespace string
	name      string
	sub       string
	selector  labels.Selector // from ?labelSelector=; nil selects everything
	metadata  bool            // the client asked for PartialObjectMetadata (Accept: ...;as=PartialObjectMetadata...)
	key       string          // the resource as Requests names it: "secrets with owner=helm"
}

// matches reports whether an object is in the request's selection (namespace and labels).
func (req fakeRequest) matches(o *unstructured.Unstructured) bool {
	if req.namespace != "" && o.GetNamespace() != req.namespace {
		return false
	}
	return req.selector == nil || req.selector.Matches(labels.Set(o.GetLabels()))
}

// render returns an object as the request receives it: in full, or as PartialObjectMetadata.
func (req fakeRequest) render(o *unstructured.Unstructured) map[string]any {
	if !req.metadata {
		return o.DeepCopy().Object
	}
	out := map[string]any{"apiVersion": "meta.k8s.io/v1", "kind": "PartialObjectMetadata"}
	if md, ok := o.DeepCopy().Object["metadata"]; ok {
		out["metadata"] = md
	}
	return out
}

func (s *FakeAPIServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if token := s.token(); (token != "" && r.Header.Get("Authorization") != "Bearer "+token) || s.clientCertRejected(r) {
		s.count("other", "")
		writeStatus(w, http.StatusUnauthorized, "Unauthorized", "Unauthorized")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case r.URL.Path == "/version":
		s.count("discovery", "")
		writeJSON(w, http.StatusOK, map[string]any{
			"major": "1", "minor": "37", "gitVersion": "v1.37.1-fake", "platform": "fake/fake",
		})
	case r.URL.Path == "/api":
		s.count("discovery", "")
		writeJSON(w, http.StatusOK, map[string]any{
			"kind": "APIVersions", "versions": []string{"v1"},
			"serverAddressByClientCIDRs": []any{map[string]any{"clientCIDR": "0.0.0.0/0", "serverAddress": strings.TrimPrefix(s.URL, "https://")}},
		})
	case r.URL.Path == "/apis":
		s.count("discovery", "")
		writeJSON(w, http.StatusOK, s.groupList())
	case parts[0] == "api" && len(parts) == 2:
		s.count("discovery", "")
		s.serveResourceList(w, "", parts[1])
	case parts[0] == "apis" && len(parts) == 2:
		s.count("discovery", "")
		s.serveGroup(w, parts[1])
	case parts[0] == "apis" && len(parts) == 3:
		s.count("discovery", "")
		s.serveResourceList(w, parts[1], parts[2])
	case r.Method == http.MethodPost && len(parts) == 4 && parts[1] == "authorization.k8s.io" && parts[2] == "v1":
		s.serveReview(w, r, parts[3])
	case r.Method == http.MethodPost && len(parts) == 4 && parts[1] == "authentication.k8s.io" && parts[3] == "selfsubjectreviews":
		s.serveWhoAmI(w, r, parts[2])
	case parts[0] == "api" || parts[0] == "apis":
		s.serveResource(w, r, parts)
	default:
		s.count("other", "")
		writeStatus(w, http.StatusNotFound, "NotFound", "the server could not find the requested resource")
	}
}

func (s *FakeAPIServer) count(verb, resource string) {
	key := verb
	if resource != "" {
		key += " " + resource
	}
	s.mu.Lock()
	s.requests[key]++
	s.mu.Unlock()
}

func (s *FakeAPIServer) isBroken(group string) bool {
	for _, g := range s.opts.BrokenGroups {
		if g == group {
			return true
		}
	}
	return false
}

func (s *FakeAPIServer) groupList() map[string]any {
	versions := map[string][]string{}
	for gvr := range s.resources {
		if gvr.Group != "" && !contains(versions[gvr.Group], gvr.Version) {
			versions[gvr.Group] = append(versions[gvr.Group], gvr.Version)
		}
	}
	for _, g := range s.opts.BrokenGroups {
		if _, ok := versions[g]; !ok {
			versions[g] = []string{"v1"}
		}
	}
	names := make([]string, 0, len(versions))
	for g := range versions {
		names = append(names, g)
	}
	sort.Strings(names)
	groups := make([]any, 0, len(names))
	for _, g := range names {
		groups = append(groups, groupJSON(g, versions[g]))
	}
	return map[string]any{"kind": "APIGroupList", "apiVersion": "v1", "groups": groups}
}

func groupJSON(group string, versions []string) map[string]any {
	vs := make([]any, 0, len(versions))
	for _, v := range versions {
		vs = append(vs, map[string]any{"groupVersion": group + "/" + v, "version": v})
	}
	return map[string]any{"name": group, "versions": vs, "preferredVersion": vs[0]}
}

func (s *FakeAPIServer) serveGroup(w http.ResponseWriter, group string) {
	for _, g := range s.groupList()["groups"].([]any) {
		if g.(map[string]any)["name"] == group {
			writeJSON(w, http.StatusOK, g)
			return
		}
	}
	writeStatus(w, http.StatusNotFound, "NotFound", "group not found")
}

func (s *FakeAPIServer) serveResourceList(w http.ResponseWriter, group, version string) {
	if s.isBroken(group) {
		writeStatus(w, http.StatusServiceUnavailable, "ServiceUnavailable", "the backend of this aggregated API is down")
		return
	}
	var list []any
	for gvr, r := range s.resources {
		if gvr.Group != group || gvr.Version != version {
			continue
		}
		list = append(list, map[string]any{
			"name": gvr.Resource, "singularName": strings.ToLower(r.Kind), "namespaced": r.Namespaced, "kind": r.Kind,
			"verbs": []string{"get", "list", "watch", "create", "update", "patch", "delete", "deletecollection"},
		})
		if gvr.Resource == "pods" {
			list = append(list, map[string]any{"name": "pods/log", "namespaced": true, "kind": "Pod", "verbs": []string{"get"}})
		}
	}
	if list == nil {
		writeStatus(w, http.StatusNotFound, "NotFound", "group version not found")
		return
	}
	gv := version
	if group != "" {
		gv = group + "/" + version
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": "APIResourceList", "apiVersion": "v1", "groupVersion": gv, "resources": list})
}

// parseResource parses /api/v1/... and /apis/<group>/<version>/... the way the API server does.
func (s *FakeAPIServer) parseResource(parts []string) (fakeRequest, bool) {
	var gv schema.GroupVersion
	switch {
	case parts[0] == "api" && len(parts) > 2:
		gv, parts = schema.GroupVersion{Version: parts[1]}, parts[2:]
	case parts[0] == "apis" && len(parts) > 3:
		gv, parts = schema.GroupVersion{Group: parts[1], Version: parts[2]}, parts[3:]
	default:
		return fakeRequest{}, false
	}
	var req fakeRequest
	if parts[0] == "namespaces" && len(parts) > 2 && parts[2] != "status" && parts[2] != "finalize" {
		req.namespace, parts = parts[1], parts[2:]
	}
	res, ok := s.resources[gv.WithResource(parts[0])]
	if !ok {
		return fakeRequest{}, false
	}
	req.res = res
	if len(parts) > 1 {
		req.name = parts[1]
	}
	if len(parts) > 2 {
		req.sub = parts[2]
	}
	return req, true
}

func (s *FakeAPIServer) serveResource(w http.ResponseWriter, r *http.Request, parts []string) {
	req, ok := s.parseResource(parts)
	if !ok {
		s.count("other", "")
		writeStatus(w, http.StatusNotFound, "NotFound", "the server could not find the requested resource")
		return
	}
	resource := req.res.GVR.Resource
	if req.res.GVR.Group != "" {
		resource += "." + req.res.GVR.Group
	}
	q := r.URL.Query()
	if sel := q.Get("labelSelector"); sel != "" {
		parsed, err := labels.Parse(sel)
		if err != nil {
			s.count("other", resource)
			writeStatus(w, http.StatusBadRequest, "BadRequest", "invalid label selector: "+err.Error())
			return
		}
		req.selector = parsed
		resource += " with " + parsed.String()
	}
	if strings.Contains(r.Header.Get("Accept"), "as=PartialObjectMetadata") {
		req.metadata = true
		resource += " (metadata)"
	}
	req.key = resource
	if req.sub == "proxy" {
		s.serveProxy(w, r, req)
		return
	}
	if req.sub == "log" && req.res.GVR == PodsGVR { // fakelogs.go
		s.serveLog(w, r, req)
		return
	}
	watching := q.Get("watch") == "true" || q.Get("watch") == "1"
	initial := q.Get("sendInitialEvents") == "true"
	if s.refuseRead(w, r, req, resource, watching, initial) { // FakeAccess.Enforce (nsrefusing.go)
		return
	}
	switch {
	case r.Method != http.MethodGet:
		s.count("other", resource)
		writeStatus(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "the fake API server only reads")
	case watching && s.warming(resource):
		s.count(map[bool]string{true: "watchlist", false: "watch"}[initial], resource)
		w.Header().Set("Retry-After", "1")
		writeStatus(w, http.StatusTooManyRequests, "TooManyRequests", "storage is (re)initializing")
	case watching && initial:
		s.count("watchlist", resource)
		if s.opts.DisableWatchList {
			writeStatus(w, http.StatusUnprocessableEntity, "Invalid", "sendInitialEvents is forbidden for watch unless the WatchList feature gate is enabled")
			return
		}
		s.serveWatch(w, r, req, true)
	case watching:
		s.count("watch", resource)
		s.serveWatch(w, r, req, false)
	case req.name != "":
		s.count("get", resource)
		s.serveGet(w, req)
	default:
		s.count("list", resource)
		s.serveList(w, req)
	}
}

// SetProxy puts a service behind the API server's service proxy: a request for
// /api/v1/namespaces/<namespace>/services/<service>/proxy/<path> reaches h with the path after
// "/proxy" and is counted as "get services/proxy". service is the name as the request writes it,
// "name:port" or "name"; a request that spells the port the other way (its number for its name, or
// back) reaches the same handler when the fake holds the Service (proxyBySpelling). A service
// nobody set answers 503 like a service without endpoints.
func (s *FakeAPIServer) SetProxy(namespace, service string, h http.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if h == nil {
		delete(s.proxies, namespace+"/"+service)
		return
	}
	s.proxies[namespace+"/"+service] = h
}

func (s *FakeAPIServer) serveProxy(w http.ResponseWriter, r *http.Request, req fakeRequest) {
	s.count("get", "services/proxy")
	s.mu.Lock()
	h := s.proxies[req.namespace+"/"+req.name]
	if h == nil {
		h = s.proxyBySpelling(req.namespace, req.name)
	}
	s.mu.Unlock()
	if h == nil {
		writeStatus(w, http.StatusServiceUnavailable, "ServiceUnavailable", "no endpoints available for service "+req.name)
		return
	}
	rest := r.URL.Path
	if i := strings.Index(rest, "/proxy"); i >= 0 {
		rest = rest[i+len("/proxy"):]
	}
	if rest == "" {
		rest = "/"
	}
	r2 := r.Clone(r.Context())
	r2.URL.Path = rest
	h.ServeHTTP(w, r2)
}

// proxyBySpelling finds the handler of a service proxy request whose port is spelled otherwise
// than SetProxy named it, as the API server resolves "name:9090" and "name:http-web" to the same
// port of the Service: the Service's own ports (from the objects the fake holds) say which number
// and which name are the same port. Nil without such a Service or handler. Called with s.mu held.
func (s *FakeAPIServer) proxyBySpelling(namespace, name string) http.Handler {
	scheme, svc, port := "", name, ""
	parts := strings.Split(name, ":")
	switch len(parts) {
	case 2:
		svc, port = parts[0], parts[1]
	case 3:
		scheme, svc, port = parts[0]+":", parts[1], parts[2]
	}
	obj, ok := s.objs[ServicesGVR][objKey(namespace, svc)]
	if !ok || port == "" {
		return nil
	}
	ports, _, _ := unstructured.NestedSlice(obj.Object, "spec", "ports")
	for _, raw := range ports {
		p, _ := raw.(map[string]any)
		pname, _ := p["name"].(string)
		num := fmt.Sprint(p["port"])
		if port != pname && port != num {
			continue
		}
		for _, alt := range []string{num, pname} {
			if h := s.proxies[namespace+"/"+scheme+svc+":"+alt]; alt != "" && h != nil {
				return h
			}
		}
	}
	return nil
}

func (s *FakeAPIServer) serveGet(w http.ResponseWriter, req fakeRequest) {
	s.mu.Lock()
	obj, ok := s.objs[req.res.GVR][objKey(req.namespace, req.name)]
	s.mu.Unlock()
	if !ok || req.sub != "" {
		writeStatus(w, http.StatusNotFound, "NotFound", fmt.Sprintf("%s %q not found", req.res.GVR.Resource, req.name))
		return
	}
	writeJSON(w, http.StatusOK, req.render(obj))
}

// snapshot returns the objects of req's scope, sorted by key, and the current resourceVersion.
// Called with s.mu held.
func (s *FakeAPIServer) snapshot(req fakeRequest) ([]*unstructured.Unstructured, int64) {
	byKey := s.objs[req.res.GVR]
	keys := make([]string, 0, len(byKey))
	for k, o := range byKey {
		if req.matches(o) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := make([]*unstructured.Unstructured, 0, len(keys))
	for _, k := range keys {
		out = append(out, byKey[k])
	}
	return out, s.rv
}

func (s *FakeAPIServer) serveList(w http.ResponseWriter, req fakeRequest) {
	s.mu.Lock()
	objs, rv := s.snapshot(req)
	items := make([]any, 0, len(objs))
	for _, o := range objs {
		items = append(items, req.render(o))
	}
	s.mu.Unlock()
	apiVersion, kind := req.res.GVR.GroupVersion().String(), req.res.Kind+"List"
	if req.metadata {
		apiVersion, kind = "meta.k8s.io/v1", "PartialObjectMetadataList"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"apiVersion": apiVersion, "kind": kind,
		"metadata": map[string]any{"resourceVersion": strconv.FormatInt(rv, 10)},
		"items":    items,
	})
}

// serveWatch streams watch events as chunked JSON until the client goes away, the timeout ends,
// the server stops or the watch falls behind. With initial (sendInitialEvents), it first sends
// the current objects as ADDED and the bookmark that ends the initial events.
func (s *FakeAPIServer) serveWatch(w http.ResponseWriter, r *http.Request, req fakeRequest, initial bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeStatus(w, http.StatusInternalServerError, "InternalError", "streaming unsupported")
		return
	}
	timeout := 30 * time.Minute
	if sec, err := strconv.Atoi(r.URL.Query().Get("timeoutSeconds")); err == nil && sec > 0 {
		timeout = time.Duration(sec) * time.Second
	}
	fromRV := r.URL.Query().Get("resourceVersion")

	wt := &apiWatcher{gvr: req.res.GVR, req: req, ch: make(chan memEvent, 4096), overflow: make(chan struct{})}
	var first []map[string]any
	s.mu.Lock()
	if initial || fromRV == "" || fromRV == "0" {
		// Synthetic ADDED events for the current state, as a real server sends them.
		objs, rv := s.snapshot(req)
		for _, o := range objs {
			first = append(first, map[string]any{"type": string(watch.Added), "object": req.render(o)})
		}
		if initial && !s.opts.StallWatchList {
			bmAPIVersion, bmKind := req.res.GVR.GroupVersion().String(), req.res.Kind
			if req.metadata {
				bmAPIVersion, bmKind = "meta.k8s.io/v1", "PartialObjectMetadata"
			}
			first = append(first, map[string]any{"type": string(watch.Bookmark), "object": map[string]any{
				"apiVersion": bmAPIVersion, "kind": bmKind,
				"metadata": map[string]any{
					"resourceVersion": strconv.FormatInt(rv, 10),
					"annotations":     map[string]any{"k8s.io/initial-events-end": "true"},
				},
			}})
		}
	} else {
		from, _ := strconv.ParseInt(fromRV, 10, 64)
		for _, ev := range s.history[req.res.GVR] {
			if ev.rv > from && req.matches(ev.obj) {
				first = append(first, map[string]any{"type": string(ev.typ), "object": req.render(ev.obj)})
			}
		}
	}
	s.watchers[wt] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.watchers, wt)
		s.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	for _, ev := range first {
		if enc.Encode(ev) != nil {
			return
		}
	}
	flusher.Flush()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case ev := <-wt.ch:
			if enc.Encode(map[string]any{"type": string(ev.typ), "object": req.render(ev.obj)}) != nil {
				return
			}
			flusher.Flush()
		case <-wt.overflow:
			return
		case <-r.Context().Done():
			return
		case <-timer.C:
			return
		case <-s.stopping:
			return
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// warming reports whether a watch of resource is the first one of a cache that is warming up
// (FakeAPIOptions.WarmingCaches), and marks it answered.
func (s *FakeAPIServer) warming(resource string) bool {
	if !contains(s.opts.WarmingCaches, resource) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.warmed[resource] {
		return false
	}
	if s.warmed == nil {
		s.warmed = map[string]bool{}
	}
	s.warmed[resource] = true
	return true
}

func writeStatus(w http.ResponseWriter, code int, reason, message string) {
	writeJSON(w, code, map[string]any{
		"kind": "Status", "apiVersion": "v1", "metadata": map[string]any{},
		"status": "Failure", "reason": reason, "message": message, "code": code,
	})
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
