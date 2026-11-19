package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

func scrubbed(t *testing.T, m *secretMatcher, s string) string {
	t.Helper()
	out, _ := m.scrub(s)
	return out
}

// TestSecretMatcherForms (H-82): a value is replaced in each form it takes in text, by a
// placeholder that names its Secret and key only; certificates, a service account token Secret's
// namespace and CA, name keys, Helm release records and values under 10 bytes are not (A5, owner);
// a short value only where it stands alone; values that overlap are replaced together.
func TestSecretMatcherForms(t *testing.T) {
	const pass = `pa"ss\word-4711`
	certPEM, keyPEM := pemPair(t, "web.shop.svc", 9)
	key := strings.TrimSpace(keyPEM)
	cfg, _ := json.Marshal(map[string]any{"auths": map[string]any{"registry.example.io": map[string]string{
		"username": "robot", "password": "registry-pass-0815", "auth": base64.StdEncoding.EncodeToString([]byte("robot:registry-pass-0815"))}}})
	m := buildSecretMatcher([]*unstructured.Unstructured{
		secretObj("shop", "db", "Opaque", map[string]string{"password": pass + "\n", "username": "postgres_admin", "DB_HOST": "db.shop.svc.cluster.local",
			"pin": "1234567", "nine": "letmein89", "word": "letmein810"}),
		secretObj("shop", "web-tls", "kubernetes.io/tls", map[string]string{"tls.crt": certPEM, "tls.key": keyPEM}),
		secretObj("shop", "sa-token-x", "kubernetes.io/service-account-token", map[string]string{"token": "eyJhbGciOiJSUzI1NiJ9.e30.sig-0123456789",
			"namespace": "kube-system-ns", "ca.crt": certPEM}),
		secretObj("shop", "regcred", "kubernetes.io/dockerconfigjson", map[string]string{".dockerconfigjson": string(cfg)}),
		secretObj("shop", "sh.helm.release.v1.web.v1", helmReleaseSecretType, map[string]string{"release": "helm-release-payload"}),
		secretObj("shop", "overlap", "Opaque", map[string]string{"a": "abcdefghijklmnop-1234", "b": "1234-qrstuvwxyz0123"}),
	})
	label := SecretPlaceholder("shop", "db", "password")
	if label != "«secret shop/db/password»" {
		t.Fatalf("placeholder %q", label)
	}
	b64 := base64.StdEncoding.EncodeToString([]byte(pass + "\n"))
	jsonForm, _ := json.Marshal(pass)
	for in, want := range map[string]string{
		"db password " + pass + " used":                      "db password " + label + " used",
		"data: {password: " + b64 + "}":                      "data: {password: " + label + "}",
		`{"msg":"x","password":` + string(jsonForm) + `}`:    `{"msg":"x","password":"` + label + `"}`,
		"user=postgres_admin host=db.shop.svc.cluster.local": "user=postgres_admin host=db.shop.svc.cluster.local", // name keys
		"pin 1234567":                                    "pin 1234567",                                // under 10 bytes
		"nine letmein89":                                 "nine letmein89",                             // 9 bytes: under 10
		"word letmein810 and letmein8100":                "word «secret shop/db/word» and letmein8100", // stands alone
		"Bearer eyJhbGciOiJSUzI1NiJ9.e30.sig-0123456789": "Bearer «secret shop/sa-token-x/token»",
		"namespace kube-system-ns":                       "namespace kube-system-ns",
		"auth " + base64.StdEncoding.EncodeToString([]byte("robot:registry-pass-0815")): "auth «secret shop/regcred/.dockerconfigjson»",
		"pull with registry-pass-0815":             "pull with «secret shop/regcred/.dockerconfigjson»",
		"helm-release-payload":                     "helm-release-payload",
		"x abcdefghijklmnop-1234-qrstuvwxyz0123 y": "x «secret shop/overlap/a» y",
	} {
		if got := scrubbed(t, m, in); got != want {
			t.Errorf("%q:\n got %q\nwant %q", in, got, want)
		}
	}
	keyLabel := SecretPlaceholder("shop", "web-tls", "tls.key")
	if got := scrubbed(t, m, "key: "+key); strings.Contains(got, "PRIVATE KEY-----\n") || !strings.Contains(got, keyLabel) {
		t.Errorf("the whole key: %q", got)
	}
	keyJSON, _ := json.Marshal(key)
	if got := scrubbed(t, m, `{"key":`+string(keyJSON)+`}`); got != `{"key":"`+keyLabel+`"}` {
		t.Errorf("the key in a JSON line: %q", got)
	}
	for line := range strings.Lines(key) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "-----") {
			continue
		}
		if got := scrubbed(t, m, "    "+line); got != "    "+keyLabel {
			t.Errorf("a line of the key: %q", got)
		}
	}
	cert := strings.TrimSpace(certPEM)
	if got := scrubbed(t, m, cert); got != cert {
		t.Error("a certificate was replaced")
	}
	for _, f := range []string{"%v", "%+v", "%#v", "%s"} {
		if s := fmt.Sprintf(f, m); strings.Contains(s, "registry-pass") || strings.Contains(s, "letmein8") {
			t.Errorf("%s prints a value: %s", f, s)
		}
	}
}

// TestSecretMatcherFromTheCache (H-82, cache first): the matcher reads only the Secrets the cache
// holds already: none while no Secrets scope runs, and peeking starts no list or watch. It is built
// again only when the cached Secrets changed, not per read, and takes a new or changed value then.
func TestSecretMatcherFromTheCache(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.SecretsGVR, testkit.Secret("shop", "db", nil, map[string]string{"password": "first-pass-1234"}))
	c := cache.New(src, cache.Options{WarmPeriod: time.Minute})
	defer c.Close()
	b := &Backend{Context: "test", Cache: c}
	if m := b.secretMatcher(); scrubbedOr(m, "first-pass-1234") != "first-pass-1234" || src.Lists() != 0 {
		t.Fatalf("without a Secrets scope: %v, %d lists", m, src.Lists())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := c.Acquire(cache.Scope{GVR: testkit.SecretsGVR})
	defer h.Release()
	if !h.WaitForSync(ctx) {
		t.Fatal("not synced")
	}
	m1 := b.secretMatcher()
	if got := scrubbedOr(m1, "x first-pass-1234"); got != "x «secret shop/db/password»" {
		t.Fatalf("cached: %q", got)
	}
	if m2, m3 := b.secretMatcher(), b.secretMatcher(); m2 != m1 || m3 != m1 {
		t.Error("built again without a change")
	}
	src.Set(testkit.SecretsGVR, testkit.Secret("shop", "db", nil, map[string]string{"password": "second-pass-5678"}))
	if !testkit.WaitFor(ctx, func() bool { return scrubbedOr(b.secretMatcher(), "second-pass-5678") != "second-pass-5678" }) {
		t.Fatal("a changed value never replaced")
	}
	if got := scrubbedOr(b.secretMatcher(), "first-pass-1234"); got != "first-pass-1234" {
		t.Errorf("the old value is still replaced: %q", got)
	}
}

func scrubbedOr(m *secretMatcher, s string) string {
	out, _ := m.scrub(s)
	return out
}

// TestSecretMatcherConcurrentRebuilds runs reads while the cached Secrets change (go test -race):
// every read gets a matcher, none a half-built one.
func TestSecretMatcherConcurrentRebuilds(t *testing.T) {
	src := testkit.NewMemorySource()
	c := cache.New(src, cache.Options{WarmPeriod: time.Minute})
	defer c.Close()
	b := &Backend{Context: "test", Cache: c}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := c.Acquire(cache.Scope{GVR: testkit.SecretsGVR})
	defer h.Release()
	if !h.WaitForSync(ctx) {
		t.Fatal("not synced")
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range 4 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				out := scrubbedOr(b.secretMatcher(), "token value-0000-0000 here")
				if strings.Contains(out, "«") && out != "token «secret shop/s-0/k» here" {
					t.Errorf("read %q", out)
					return
				}
			}
		})
	}
	for i := range 50 {
		src.Set(testkit.SecretsGVR, testkit.Secret("shop", fmt.Sprintf("s-%d", i), nil, map[string]string{"k": fmt.Sprintf("value-%04d-0000", i)}))
	}
	if !testkit.WaitFor(ctx, func() bool {
		return strings.Contains(scrubbedOr(b.secretMatcher(), "value-0049-0000"), "«secret shop/s-49/k»")
	}) {
		t.Error("the last Secret never reached the matcher")
	}
	close(stop)
	wg.Wait()
}

// TestSecretMatcherKeepsArgoCDsShownKeys: the keys of Argo CD's repository and cluster Secrets that
// D-35 shows agents are not replaced (a repository's URL in its object_get), their credentials are.
func TestSecretMatcherKeepsArgoCDsShownKeys(t *testing.T) {
	repo := secretObj("argocd", "repo-shop", "Opaque", map[string]string{"url": "https://git.example.com/shop.git",
		"project": "default-project", "password": "argo-repo-token-4711"})
	repo.SetLabels(map[string]string{"argocd.argoproj.io/secret-type": "repository"})
	plain := secretObj("shop", "app", "Opaque", map[string]string{"url": "https://hooks.example.com/T0/B0/secret-path"})
	m := buildSecretMatcher([]*unstructured.Unstructured{repo, plain})
	for in, want := range map[string]string{
		"clone https://git.example.com/shop.git":           "clone https://git.example.com/shop.git",
		"token argo-repo-token-4711":                       "token «secret argocd/repo-shop/password»",
		"post https://hooks.example.com/T0/B0/secret-path": "post «secret shop/app/url»",
	} {
		if got := scrubbed(t, m, in); got != want {
			t.Errorf("%q:\n got %q\nwant %q", in, got, want)
		}
	}
}
