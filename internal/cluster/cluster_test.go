package cluster

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// secretToken is a credential planted in test kubeconfigs; no error may contain it.
const secretToken = "tok-3f9a2c-SECRET-must-not-leak-0042"

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// kubeconfigFor returns a kubeconfig with one context pointing at server with secretToken.
func kubeconfigFor(contextName, server, namespace string) string {
	ns := ""
	if namespace != "" {
		ns = "\n    namespace: " + namespace
	}
	return `apiVersion: v1
kind: Config
current-context: ` + contextName + `
clusters:
- name: c-` + contextName + `
  cluster:
    server: ` + server + `
users:
- name: u-` + contextName + `
  user:
    token: ` + secretToken + `
contexts:
- name: ` + contextName + `
  context:
    cluster: c-` + contextName + `
    user: u-` + contextName + ns + "\n"
}

func TestListContextsMergesInPrecedenceOrder(t *testing.T) {
	first := writeFile(t, "first", kubeconfigFor("alpha", "https://alpha.example", "team-a"))
	second := writeFile(t, "second", kubeconfigFor("beta", "https://beta.example", ""))
	// The third file defines alpha's cluster and context again and sets another current-context:
	// the first file to set a value wins.
	third := writeFile(t, "third", `apiVersion: v1
kind: Config
current-context: beta
clusters:
- name: c-alpha
  cluster:
    server: https://overridden.example
contexts:
- name: alpha
  context: {cluster: c-alpha, user: nobody, namespace: ignored}
`)
	got, err := ListContexts([]string{first, second, third})
	if err != nil {
		t.Fatalf("ListContexts: %v", err)
	}
	want := []kubeconfig.Context{
		{Name: "alpha", Cluster: "c-alpha", User: "u-alpha", Namespace: "team-a", Server: "https://alpha.example", Current: true},
		{Name: "beta", Cluster: "c-beta", User: "u-beta", Server: "https://beta.example"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestListContextsSkipsBrokenAndMissingFilesWithSafeErrors(t *testing.T) {
	good := writeFile(t, "good", kubeconfigFor("good", "https://good.example", ""))
	// Duplicate user names: client-go's own error would print the users list, tokens included.
	broken := writeFile(t, "broken", "users:\n- name: u\n  user:\n    token: "+secretToken+"\n- name: u\n  user:\n    token: x\n")
	missing := filepath.Join(t.TempDir(), "missing")

	got, err := ListContexts([]string{broken, good, missing})
	if len(got) != 1 || got[0].Name != "good" {
		t.Errorf("contexts of the good file not returned: %+v", got)
	}
	if err == nil {
		t.Fatal("no error for the broken and the missing file")
	}
	for _, want := range []string{broken, missing} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not name %s: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), secretToken) {
		t.Errorf("error leaks the token: %v", err)
	}
}

func TestListContextsDefaultRulesUseKUBECONFIG(t *testing.T) {
	a := writeFile(t, "a", kubeconfigFor("a", "https://a.example", ""))
	b := writeFile(t, "b", kubeconfigFor("b", "https://b.example", ""))
	t.Setenv("KUBECONFIG", a+string(os.PathListSeparator)+filepath.Join(t.TempDir(), "absent")+string(os.PathListSeparator)+b)
	got, err := ListContexts(nil)
	if err != nil {
		t.Fatalf("a missing file in $KUBECONFIG is not an error: %v", err)
	}
	if len(got) != 2 || got[0].Name != "a" || !got[0].Current || got[1].Name != "b" || got[1].Current {
		t.Errorf("got %+v", got)
	}

	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "absent"))
	got, err = ListContexts(nil)
	if err != nil || len(got) != 0 {
		t.Errorf("no kubeconfig at all: got %+v, %v", got, err)
	}
}

// TestNamedKubeconfigFiles: explicit paths win, else $KUBECONFIG's entries (never ~/.kube/config);
// absolute, clean, without duplicates, and nothing is read (the files need not exist).
func TestNamedKubeconfigFiles(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "sub", "..", "b")
	t.Setenv("KUBECONFIG", a+string(os.PathListSeparator)+b+string(os.PathListSeparator)+a)
	if got := NamedKubeconfigFiles(nil); len(got) != 2 || got[0] != a || got[1] != filepath.Join(dir, "b") {
		t.Errorf("from $KUBECONFIG: %v", got)
	}
	if got := NamedKubeconfigFiles([]string{b}); len(got) != 1 || got[0] != filepath.Join(dir, "b") {
		t.Errorf("explicit: %v", got)
	}
	t.Setenv("KUBECONFIG", "")
	if got := NamedKubeconfigFiles(nil); len(got) != 0 {
		t.Errorf("nothing named: %v", got)
	}
}

func TestConnectAgainstFakeServer(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: secretToken})
	defer srv.Close()
	path := srv.WriteKubeconfig(t, "fake", "team-a")

	c, err := Connect(context.Background(), Options{KubeconfigPaths: []string{path}})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()
	if c.Context() != "fake" || c.Namespace() != "team-a" || c.Server() != srv.URL || c.ServerVersion() != "v1.37.1-fake" {
		t.Errorf("context=%q namespace=%q server=%q version=%q", c.Context(), c.Namespace(), c.Server(), c.ServerVersion())
	}
	if got := c.KubeconfigPaths(); len(got) != 1 || got[0] != path {
		t.Errorf("KubeconfigPaths = %v, want [%s]", got, path)
	}
	cfg := c.RESTConfig()
	if cfg.QPS != DefaultQPS || cfg.Burst != DefaultBurst || cfg.Timeout != 0 {
		t.Errorf("QPS=%v Burst=%d Timeout=%v, want 5, 10, 0", cfg.QPS, cfg.Burst, cfg.Timeout)
	}
	st := c.Counter().Snapshot()
	if st.Total != 1 || st.ByVerb[VerbDiscovery] != 1 {
		t.Errorf("Connect cost %+v, want exactly one discovery request", st.ByVerb)
	}
	if srv.Requests()["discovery"] != 1 {
		t.Errorf("server saw %v", srv.Requests())
	}

	// A copy of the REST config keeps counting.
	if _, err := c.Kube().Discovery().ServerVersion(); err != nil {
		t.Fatal(err)
	}
	if c.Counter().Snapshot().ByVerb[VerbDiscovery] != 2 {
		t.Error("the typed client does not count")
	}
}

func TestConnectNamespaceDefaultsAndExplicitContext(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{})
	defer srv.Close()
	// The contexts list ends the document: append a second context to it.
	path := writeFile(t, "kc", string(srv.Kubeconfig("one", ""))+`- name: two
  context:
    cluster: fake
    user: fake
    namespace: second
`)
	c, err := Connect(context.Background(), Options{KubeconfigPaths: []string{path}, Context: "two", QPS: 50, Burst: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.Context() != "two" || c.Namespace() != "second" {
		t.Errorf("context=%q namespace=%q", c.Context(), c.Namespace())
	}
	if cfg := c.RESTConfig(); cfg.QPS != 50 || cfg.Burst != 100 {
		t.Errorf("QPS=%v Burst=%d", cfg.QPS, cfg.Burst)
	}

	c1, err := Connect(context.Background(), Options{KubeconfigPaths: []string{path}})
	if err != nil {
		t.Fatal(err)
	}
	defer c1.Close()
	if c1.Context() != "one" || c1.Namespace() != "default" {
		t.Errorf("current context: context=%q namespace=%q", c1.Context(), c1.Namespace())
	}
}

func TestConnectErrorsNameTheContextNeverTheCredential(t *testing.T) {
	// An address nobody listens on.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	refused := "http://" + l.Addr().String()
	_ = l.Close()

	// A server that rejects every credential.
	unauthorized := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: "the-right-token"})
	defer unauthorized.Close()

	// A server that never answers.
	hang := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer hang.Close()

	tests := []struct {
		name    string
		config  string
		context string
		timeout time.Duration
		want    string
		check   func(error) bool
	}{
		{name: "connection refused", config: kubeconfigFor("lab", refused, ""), want: "cannot reach the API server " + refused},
		// TLS with a trusted CA, so client-go really sends the token, and the server rejects it.
		{name: "401", config: string(unauthorized.KubeconfigWithToken("lab", "", secretToken)), want: "401 Unauthorized", check: apierrors.IsUnauthorized},
		{name: "untrusted certificate", config: kubeconfigFor("lab", unauthorized.URL, ""), want: "certificate"},
		{name: "timeout", config: kubeconfigFor("lab", hang.URL, ""), timeout: 200 * time.Millisecond, want: "no answer from the API server"},
		{name: "unknown context", config: kubeconfigFor("lab", refused, ""), context: "nope", want: "no such context"},
		{name: "no current context", config: strings.Replace(kubeconfigFor("lab", refused, ""), "current-context: lab", "", 1), want: "no current-context"},
		{name: "basic auth in the server URL", config: kubeconfigFor("lab", "http://admin:"+secretToken+"@"+strings.TrimPrefix(refused, "http://"), ""), want: "cannot reach the API server " + refused},
		{
			// client-go's own decoding error would print the users list, tokens included.
			name:   "duplicate users",
			config: strings.Replace(kubeconfigFor("lab", refused, ""), "contexts:", "- name: u-lab\n  user:\n    token: "+secretToken+"\ncontexts:", 1),
			want:   "kubeconfig file",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeFile(t, "kc", tt.config)
			c, err := Connect(context.Background(), Options{KubeconfigPaths: []string{path}, Context: tt.context, ConnectTimeout: tt.timeout})
			if err == nil {
				c.Close()
				t.Fatal("Connect succeeded")
			}
			msg := err.Error()
			if strings.Contains(msg, secretToken) {
				t.Errorf("error leaks the token: %s", msg)
			}
			if !strings.Contains(msg, tt.want) {
				t.Errorf("error %q does not say %q", msg, tt.want)
			}
			var ce *ConnectError
			if !errors.As(err, &ce) {
				t.Errorf("not a *ConnectError: %T", err)
			}
			if tt.context != "" && !strings.Contains(msg, `"`+tt.context+`"`) {
				t.Errorf("error does not name the context: %s", msg)
			}
			if tt.check != nil && !tt.check(err) {
				t.Errorf("errors.As does not reach the API error: %v", err)
			}
			// Unwrapped errors must not leak either: someone may log them.
			for e := errors.Unwrap(err); e != nil; e = errors.Unwrap(e) {
				if strings.Contains(e.Error(), secretToken) {
					t.Errorf("wrapped error leaks the token: %s", e.Error())
				}
			}
		})
	}
}

func TestServedResourcesToleratesBrokenGroupsAndCaches(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{BrokenGroups: []string{"metrics.k8s.io"}})
	defer srv.Close()
	c, err := Connect(context.Background(), Options{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "")}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx := context.Background()

	got, err := c.ServedResources(ctx)
	if err != nil {
		t.Fatalf("ServedResources: %v", err)
	}
	for _, gvr := range []schema.GroupVersionResource{testkit.PodsGVR, testkit.ServicesGVR, testkit.NamespacesGVR, testkit.DeploymentsGVR} {
		if !got[gvr] {
			t.Errorf("%v missing from %v", gvr, got)
		}
	}
	if got[schema.GroupVersionResource{Version: "v1", Resource: "pods/log"}] {
		t.Error("subresource listed")
	}
	if len(got) != 4 {
		t.Errorf("got %d resources: %v", len(got), got)
	}

	before := c.Counter().Snapshot().ByVerb[VerbDiscovery]
	if before < 3 {
		t.Errorf("discovery requests not counted as discovery: %d", before)
	}
	got[testkit.PodsGVR] = false // the caller's copy
	again, err := c.ServedResources(ctx)
	if err != nil || !again[testkit.PodsGVR] {
		t.Fatalf("cached result: %v %v", again, err)
	}
	if after := c.Counter().Snapshot().ByVerb[VerbDiscovery]; after != before {
		t.Errorf("cached discovery cost %d requests", after-before)
	}
	c.InvalidateServedResources()
	if _, err := c.ServedResources(ctx); err != nil {
		t.Fatal(err)
	}
	if after := c.Counter().Snapshot().ByVerb[VerbDiscovery]; after == before {
		t.Error("InvalidateServedResources did not refresh")
	}
}

func TestEnsureToolPath(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{filepath.Join(home, "bin"), filepath.Join(home, ".local", "bin")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin"+string(os.PathListSeparator)+filepath.Join(home, "bin"))
	toolDirsBefore := toolDirs
	toolDirs = []string{filepath.Join(home, "missing"), "/usr/bin"}
	defer func() { toolDirs = toolDirsBefore }()

	if runtime.GOOS == "windows" {
		// A program started from the Start menu or Explorer has the PATH a terminal has (both
		// read it from the user's environment in the registry): nothing is added there.
		before := os.Getenv("PATH")
		EnsureToolPath()
		if got := os.Getenv("PATH"); got != before {
			t.Errorf("PATH changed on Windows to %q", got)
		}
		return
	}
	EnsureToolPath()
	want := strings.Join([]string{"/usr/bin", filepath.Join(home, "bin"), filepath.Join(home, ".local", "bin")}, string(os.PathListSeparator))
	if got := os.Getenv("PATH"); got != want {
		t.Errorf("PATH = %q, want %q", got, want)
	}
	EnsureToolPath() // idempotent
	if got := os.Getenv("PATH"); got != want {
		t.Errorf("second call changed PATH to %q", got)
	}

	t.Setenv("PATH", "")
	EnsureToolPath()
	if got := os.Getenv("PATH"); !strings.HasPrefix(got, "/usr/bin") {
		t.Errorf("empty PATH: got %q", got)
	}
}
