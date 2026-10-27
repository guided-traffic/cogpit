package engine

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"math/rand/v2"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The measurement behind MinScrubbedValue and boundedBelow (agents-and-mcp H-82): a synthetic but
// realistic cluster's Secrets and 10,000 log lines of the kinds a cluster prints (an ingress's
// access log, Go services' JSON lines, controllers' klog lines, PostgreSQL, Spring Boot with stack
// traces, operators), 1 % of them with a value leaked on purpose in the forms a log prints one (a
// database URL, a bearer token, an API key, a JSON-escaped private key, a Secret's data dumped in
// base64, a registry auth, a webhook URL, short human passwords). Every number is from this
// corpus, not from a real cluster's logs: see the H-82 entry for what that does not show.

// valueClass says what a Secret's value is: a credential, or a name that only sits in a Secret
// (a user or host name, a database, a port).
type valueClass int

const (
	classCredential valueClass = iota
	className
)

// measuredSecrets is a cluster's Secrets: 15 namespaces, each with legacy service account token
// Secrets, TLS Secrets, database credentials, API keys, basic auth, a registry credential, a
// configuration file and Helm release records. classes says what each data key holds.
type measuredSecrets struct {
	objs    []*unstructured.Unstructured
	classes map[string]valueClass // placeholder → class
	// for the leak lines
	tokens, passwords, shortPasswords, apiKeys, webhooks, keysPEM, dockerAuths, users []string
	b64Passwords                                                                      []string
}

var measureNamespaces = []string{"default", "kube-system", "monitoring", "cert-manager", "ingress-nginx", "shop", "payments",
	"auth", "logging", "argocd", "flux-system", "data", "ml", "staging", "internal-tools"}

var measureApps = []string{"orders", "checkout", "catalog", "keycloak", "grafana", "loki", "billing", "search", "gateway", "notifier"}

const alnum = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

func randString(r *rand.Rand, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = alnum[r.IntN(len(alnum))]
	}
	return string(b)
}

func randHex(r *rand.Rand, n int) string {
	const hex = "0123456789abcdef"
	b := make([]byte, n)
	for i := range b {
		b[i] = hex[r.IntN(16)]
	}
	return string(b)
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// pemPair makes an ECDSA key and a certificate for name, PEM, signed by itself.
func pemPair(t testing.TB, name string, serial int64) (certPEM, keyPEM string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), cryptoRand{})
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: name}, DNSNames: []string{name},
		NotBefore: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), NotAfter: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)}
	der, err := x509.CreateCertificate(cryptoRand{}, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kder, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder}))
}

// cryptoRand is a deterministic byte source for the test's keys (the measurement repeats).
type cryptoRand struct{}

var measureRand = rand.New(rand.NewPCG(7, 11))

func (cryptoRand) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(measureRand.UintN(256))
	}
	return len(p), nil
}

func secretObj(ns, name, typ string, data map[string]string) *unstructured.Unstructured {
	d := map[string]any{}
	for k, v := range data {
		d[k] = base64.StdEncoding.EncodeToString([]byte(v))
	}
	return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Secret", "type": typ,
		"metadata": map[string]any{"namespace": ns, "name": name}, "data": d}}
}

func newMeasuredSecrets(t testing.TB) *measuredSecrets { return newMeasuredSecretsSeed(t, 1, "") }

// newMeasuredSecretsSeed is newMeasuredSecrets with other values, in namespaces with suffix.
func newMeasuredSecretsSeed(t testing.TB, seed uint64, suffix string) *measuredSecrets {
	t.Helper()
	r := rand.New(rand.NewPCG(seed, 2))
	ms := &measuredSecrets{classes: map[string]valueClass{}}
	add := func(ns, name, typ string, data map[string]string, names ...string) {
		ms.objs = append(ms.objs, secretObj(ns, name, typ, data))
		for k := range data {
			ms.classes[SecretPlaceholder(ns, name, k)] = classCredential
		}
		for _, k := range names {
			ms.classes[SecretPlaceholder(ns, name, k)] = className
		}
	}
	caPEM, _ := pemPair(t, "kubernetes", 1)
	// Human passwords, some of them words a log line holds anyway (a development database whose
	// password is "postgres" or "password").
	short := []string{"hunter2", "changeme", "Passw0rd!", "secret123", "admin123", "welcome1", "letmein", "qwerty123",
		"postgres", "password", "admin", "secret", "guest", "rabbitmq"}
	users := []string{"postgres", "app_user", "admin", "keycloak", "grafana", "root", "orders", "billing_rw"}
	for i, ns := range measureNamespaces {
		ns += suffix
		for _, sa := range []string{"default", "builder", "deployer", ns + "-controller"} {
			name := sa + "-token-" + strings.ToLower(randString(r, 5))
			payload, _ := json.Marshal(map[string]string{"iss": "kubernetes/serviceaccount", "kubernetes.io/serviceaccount/namespace": ns,
				"kubernetes.io/serviceaccount/secret.name": name, "kubernetes.io/serviceaccount/service-account.name": sa,
				"kubernetes.io/serviceaccount/service-account.uid": randHex(r, 8) + "-" + randHex(r, 4) + "-" + randHex(r, 4) + "-" + randHex(r, 12),
				"sub": "system:serviceaccount:" + ns + ":" + sa})
			sig := make([]byte, 256)
			for j := range sig {
				sig[j] = byte(r.UintN(256))
			}
			token := b64url([]byte(`{"alg":"RS256","kid":"`+randString(r, 43)+`"}`)) + "." + b64url(payload) + "." + b64url(sig)
			ms.tokens = append(ms.tokens, token)
			add(ns, name, "kubernetes.io/service-account-token", map[string]string{"token": token, "ca.crt": caPEM, "namespace": ns})
		}
		for j := range 2 {
			app := measureApps[(i+j)%len(measureApps)]
			crt, key := pemPair(t, app+"."+ns+".svc", int64(100+i*10+j))
			ms.keysPEM = append(ms.keysPEM, key)
			add(ns, app+"-tls", "kubernetes.io/tls", map[string]string{"tls.crt": crt, "tls.key": key, "ca.crt": caPEM})
		}
		for j := range 2 {
			app := measureApps[(i+3*j)%len(measureApps)]
			var pass string
			switch r.IntN(4) {
			case 0:
				pass = short[r.IntN(len(short))]
				ms.shortPasswords = append(ms.shortPasswords, pass)
			case 1:
				pass = randString(r, 12)
				ms.passwords = append(ms.passwords, pass)
			default:
				pass = randString(r, 24)
				ms.passwords = append(ms.passwords, pass)
			}
			user := users[(i+j)%len(users)]
			ms.users = append(ms.users, user)
			ms.b64Passwords = append(ms.b64Passwords, base64.StdEncoding.EncodeToString([]byte(pass)))
			add(ns, app+"-db", "Opaque", map[string]string{"username": user, "password": pass,
				"host": app + "-postgresql." + ns + ".svc.cluster.local", "port": "5432", "database": app},
				"username", "host", "port", "database")
		}
		apiKey := "sk_live_" + randString(r, 24)
		hook := "https://hooks.slack.com/services/T0" + randString(r, 8) + "/B0" + randString(r, 8) + "/" + randString(r, 24)
		ms.apiKeys, ms.webhooks = append(ms.apiKeys, apiKey), append(ms.webhooks, hook)
		add(ns, measureApps[i%len(measureApps)]+"-api", "Opaque", map[string]string{"api-key": apiKey, "webhook-url": hook})
		add(ns, "basic-"+ns, "kubernetes.io/basic-auth", map[string]string{"username": "admin", "password": randString(r, 16)}, "username")
		regUser, regPass := "robot$"+ns+"+puller", randString(r, 32)
		auth := base64.StdEncoding.EncodeToString([]byte(regUser + ":" + regPass))
		ms.dockerAuths = append(ms.dockerAuths, auth)
		cfg, _ := json.Marshal(map[string]any{"auths": map[string]any{"registry.example.io": map[string]string{
			"username": regUser, "password": regPass, "auth": auth}}})
		add(ns, "regcred", "kubernetes.io/dockerconfigjson", map[string]string{".dockerconfigjson": string(cfg)})
		add(ns, "app-config", "Opaque", map[string]string{"config.yaml": "server:\n  port: 8080\n  logLevel: info\nfeatures:\n  search: true\n"})
		for j := range 2 {
			add(ns, fmt.Sprintf("sh.helm.release.v1.%s.v%d", measureApps[j], j+1), helmReleaseSecretType,
				map[string]string{"release": randString(r, 2000)})
		}
	}
	return ms
}

// leakLine is a log line with a value leaked on purpose, and the value.
type leakLine struct {
	line  string
	value string
}

// measuredLogs are 10,000 log lines, every hundredth one leaking a value.
func measuredLogs(ms *measuredSecrets) ([]string, map[int]leakLine) {
	r := rand.New(rand.NewPCG(3, 4))
	ts := time.Date(2026, 10, 14, 13, 0, 0, 0, time.UTC)
	ip := func() string { return fmt.Sprintf("10.244.%d.%d", r.IntN(8), 2+r.IntN(250)) }
	pick := func(s []string) string { return s[r.IntN(len(s))] }
	paths := []string{"/api/v1/orders", "/api/v1/orders/" + fmt.Sprint(r.IntN(99999)), "/healthz", "/readyz", "/metrics", "/login",
		"/static/app.3f2a9c61.js", "/api/v2/catalog/search?q=shoes&page=2", "/oauth2/callback?state=abc", "/favicon.ico"}
	agents := []string{"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_6) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15",
		"kube-probe/1.33", "Prometheus/3.1.0", "curl/8.7.1", "Go-http-client/2.0"}
	msgs := []string{"request served", "cache miss", "order created", "payment authorized", "user logged in", "retrying request",
		"connection pool exhausted", "token refreshed", "shutting down", "listening"}
	javaClasses := []string{"com.example.orders.OrderService", "org.springframework.web.servlet.DispatcherServlet",
		"org.hibernate.engine.jdbc.spi.SqlExceptionHelper", "com.zaxxer.hikari.pool.HikariPool"}
	var lines []string
	leaks := map[int]leakLine{}
	for i := range 10000 {
		ts = ts.Add(time.Duration(r.IntN(900)) * time.Millisecond)
		ns := pick(measureNamespaces)
		app := pick(measureApps)
		if i%100 == 50 {
			var l leakLine
			switch (i / 100) % 10 {
			case 0:
				p := pick(ms.passwords)
				l = leakLine{fmt.Sprintf(`%s ERROR connecting to postgres://%s:%s@%s-postgresql.%s.svc:5432/%s failed: timeout`,
					ts.Format(time.RFC3339), pick(ms.users), p, app, ns, app), p}
			case 1:
				tok := pick(ms.tokens)
				l = leakLine{fmt.Sprintf(`DEBUG http request GET https://kubernetes.default.svc/api/v1/namespaces/%s/pods headers: Authorization: Bearer %s`, ns, tok), tok}
			case 2:
				k := pick(ms.apiKeys)
				l = leakLine{fmt.Sprintf(`{"level":"debug","ts":"%s","msg":"calling payment provider","api_key":"%s","amount":1299}`, ts.Format(time.RFC3339Nano), k), k}
			case 3:
				key := pick(ms.keysPEM)
				q, _ := json.Marshal(strings.TrimSpace(key))
				l = leakLine{fmt.Sprintf(`{"level":"warn","msg":"loaded TLS key","key":%s}`, q), strings.TrimSpace(key)}
			case 4:
				b := pick(ms.b64Passwords)
				l = leakLine{fmt.Sprintf(`I1014 13:01:02.123456       1 reconcile.go:88] "applying" secret="%s/%s-db" data={"password":"%s"}`, ns, app, b), b}
			case 5:
				a := pick(ms.dockerAuths)
				l = leakLine{fmt.Sprintf(`time="%s" level=debug msg="pulling with auth %s" image=registry.example.io/%s/%s:1.4.2`, ts.Format(time.RFC3339), a, ns, app), a}
			case 6:
				h := pick(ms.webhooks)
				l = leakLine{fmt.Sprintf(`%s INFO notifier posting alert to %s`, ts.Format(time.RFC3339), h), h}
			case 7, 8:
				p := pick(ms.shortPasswords)
				l = leakLine{fmt.Sprintf(`%s WARN using default credentials user=admin password=%s for %s`, ts.Format(time.RFC3339), p, app), p}
			default:
				p := pick(ms.passwords)
				l = leakLine{fmt.Sprintf(`2026-10-14 13:01:02.123 UTC [%d] DETAIL:  password authentication failed, tried "%s"`, 1000+r.IntN(9000), p), p}
			}
			leaks[len(lines)] = l
			lines = append(lines, l.line)
			continue
		}
		var line string
		switch k := r.IntN(100); {
		case k < 30:
			line = fmt.Sprintf(`%s - - [%s] "%s %s HTTP/1.1" %d %d "-" "%s" %d %.3f [%s-%s-80] [] %s:8080 %d %.3f %d %s`,
				ip(), ts.Format("02/Jan/2006:15:04:05 -0700"), pick([]string{"GET", "GET", "POST", "PUT"}), pick(paths),
				[]int{200, 200, 200, 201, 204, 301, 404, 500}[r.IntN(8)], r.IntN(20000), pick(agents), 300+r.IntN(400), r.Float64(),
				ns, app, ip(), r.IntN(20000), r.Float64(), 200, randHex(r, 32))
		case k < 55:
			line = fmt.Sprintf(`{"level":"%s","ts":"%s","caller":"%s/handler.go:%d","msg":"%s","method":"GET","path":"%s","status":%d,"duration":%.4f,"trace_id":"%s","user_id":"%s-%s-%s"}`,
				pick([]string{"info", "info", "debug", "warn", "error"}), ts.Format(time.RFC3339Nano), app, 20+r.IntN(300), pick(msgs), pick(paths),
				[]int{200, 200, 401, 404, 500}[r.IntN(5)], r.Float64()/10, randHex(r, 32), randHex(r, 8), randHex(r, 4), randHex(r, 12))
		case k < 65:
			line = fmt.Sprintf(`I%s       1 controller.go:%d] "Reconciling" controller="%s" namespace="%s" name="%s-%s" reconcileID="%s-%s"`,
				ts.Format("0102 15:04:05.000000"), 100+r.IntN(400), pick([]string{"deployment", "certificate", "helmrelease", "kustomization"}),
				ns, app, strings.ToLower(randString(r, 5)), randHex(r, 8), randHex(r, 12))
		case k < 75:
			u := pick(ms.users)
			line = fmt.Sprintf(`%s UTC [%d] %s:  %s`, ts.Format("2006-01-02 15:04:05.000"), 1000+r.IntN(9000), pick([]string{"LOG", "LOG", "ERROR", "FATAL"}),
				pick([]string{
					fmt.Sprintf("connection authorized: user=%s database=%s application_name=%s", u, app, app),
					fmt.Sprintf("connection received: host=%s port=%d", ip(), 30000+r.IntN(30000)),
					"checkpoint complete: wrote 412 buffers (2.5%); 0 WAL file(s) added, 0 removed, 1 recycled",
					fmt.Sprintf("duration: %.3f ms  statement: SELECT * FROM orders WHERE customer_id = %d", r.Float64()*40, r.IntN(99999)),
					fmt.Sprintf(`password authentication failed for user "%s"`, u),
				}))
		case k < 85:
			line = fmt.Sprintf(`%s %s 1 --- [nio-8080-exec-%d] %s : %s`, ts.Format("2006-01-02T15:04:05.000Z07:00"),
				pick([]string{"INFO", "INFO", "WARN", "ERROR"}), 1+r.IntN(10), pick(javaClasses), pick([]string{
					"HikariPool-1 - Starting...", "Completed initialization in 2 ms", "Request processing failed: java.lang.NullPointerException",
					"\tat com.example.orders.OrderService.place(OrderService.java:142)", "\tat org.springframework.web.servlet.FrameworkServlet.service(FrameworkServlet.java:885)",
					fmt.Sprintf("SQL Error: 0, SQLState: 08001 (%s-postgresql.%s.svc.cluster.local:5432)", app, ns)}))
		default:
			line = pick([]string{
				fmt.Sprintf(`time="%s" level=info msg="sync complete" application=%s/%s revision=%s`, ts.Format(time.RFC3339), ns, app, randHex(r, 40)),
				fmt.Sprintf(`I1014 cert-manager/certificates "msg"="certificate is up to date" "name"="%s-tls" "namespace"="%s"`, app, ns),
				fmt.Sprintf(`level=info ts=%s caller=main.go:%d msg="Starting postgres exporter" version=0.15.0`, ts.Format(time.RFC3339), 100+r.IntN(200)),
				fmt.Sprintf(`%s ingress-nginx admission: validated %s/%s-ingress`, ts.Format(time.RFC3339), ns, app),
				fmt.Sprintf(`node-exporter: collector=filesystem duration_seconds=%.6f success=1`, r.Float64()/100),
			})
		}
		lines = append(lines, line)
	}
	return lines, leaks
}

// scrubNaive replaces every candidate in line by a plain search per value (the reference the
// matcher must agree with), each distinct value once with its first label as the matcher keeps
// it; bounded applies the stand-alone rule to values under boundedBelow. It returns the labels of
// the values it replaced.
func scrubNaive(line string, cands []scrubCandidate, bounded bool) (string, []string) {
	m := &secretMatcher{}
	var spans []scrubSpan
	var labels []string
	seen := map[string]bool{}
	for _, c := range cands {
		if seen[c.value] {
			continue
		}
		seen[c.value] = true
		for from := 0; ; {
			i := strings.Index(line[from:], c.value)
			if i < 0 {
				break
			}
			start, end := from+i, from+i+len(c.value)
			if !bounded || len(c.value) >= boundedBelow || standsAlone(line, start, end) {
				spans = append(spans, scrubSpan{start: start, end: end, pat: int32(len(m.pats))})
				m.pats = append(m.pats, scrubPattern{value: c.value, label: c.label})
				labels = append(labels, c.label)
			}
			from = start + 1
		}
	}
	if len(spans) == 0 {
		return line, nil
	}
	return replaceSpans(line, spans, m), labels
}

// measureRules are the rules the measurement compares, the last one Cogpit's.
var measureRules = []struct {
	name               string
	bounded, skipNames bool
}{
	{"every match", false, false},
	{"stand-alone", true, false},
	{"stand-alone, no names", true, true},
}

// placeholders finds the placeholders in a scrubbed line, whose key names ("password") must not
// count as the value.
var placeholders = regexp.MustCompile(`«secret [^»]*»`)

// leaked reports whether the value stays in a scrubbed line.
func leaked(scrubbed, value string) bool {
	return strings.Contains(placeholders.ReplaceAllString(scrubbed, "«»"), value)
}

// TestSecretScrubMeasure measures, over the corpus, how many log lines without a leak get a value
// replaced, for minimum lengths of 4 to 16 bytes and three rules: every match; short values
// (under boundedBelow) only where they stand alone; and that without the values of name keys
// (isNameKey). It counts the lines where a name was replaced (a user, host or database name that
// also sits in a Secret) apart from those where a credential was (a password that is a word of
// the line anyway, "password" or "postgres"), and the planted leaks each catches. It checks that
// the matcher Cogpit uses agrees with a plain search line by line, catches every planted leak of
// at least MinScrubbedValue bytes, and alters a line without a leak only where a password that is
// a word of it stands. Run with -v for the table (agents-and-mcp H-82).
func TestSecretScrubMeasure(t *testing.T) {
	ms := newMeasuredSecrets(t)
	lines, leaks := measuredLogs(ms)
	words := []string{"postgres", "password", "rabbitmq", "changeme"}
	wordPasswords := 0
	for _, p := range ms.shortPasswords {
		if slices.Contains(words, p) {
			wordPasswords++
		}
	}
	short := 0
	for _, l := range leaks {
		if len(l.value) < MinScrubbedValue {
			short++
		}
	}
	t.Logf("%d Secrets, %d log lines, %d planted leaks (%d of a value under %d bytes); %d database passwords, %d of them a word of 8 bytes",
		len(ms.objs), len(lines), len(leaks), short, MinScrubbedValue, len(ms.passwords)+len(ms.shortPasswords), wordPasswords)
	t.Logf("%-22s  min  lines with a name replaced  lines with a credential replaced, no leak  leaks caught", "rule")
	rules := measureRules
	if !testing.Verbose() {
		rules = nil // the table (18 plain searches over the corpus) only with -v; the check below always
	}
	for _, rule := range rules {
		for _, minLen := range []int{4, 6, 8, 10, 12, 16} {
			cands, _ := secretCandidates(ms.objs, minLen, rule.skipNames)
			nameLines, wordLines, caught := 0, 0, 0
			for i, line := range lines {
				out, labels := scrubNaive(line, cands, rule.bounded)
				if l, ok := leaks[i]; ok {
					if !leaked(out, l.value) {
						caught++
					}
					continue
				}
				if len(labels) == 0 {
					continue
				}
				credential := false
				for _, lb := range labels {
					credential = credential || ms.classes[lb] == classCredential
				}
				if credential {
					wordLines++
				} else {
					nameLines++
				}
			}
			t.Logf("%-22s  %3d  %26d  %41d  %d of %d", rule.name, minLen, nameLines, wordLines, caught, len(leaks))
		}
	}

	m := buildSecretMatcher(ms.objs)
	cands, _ := secretCandidates(ms.objs, MinScrubbedValue, true)
	valueOf := map[string][]string{}
	for _, c := range cands {
		valueOf[c.label] = append(valueOf[c.label], c.value)
	}
	missed, altered := 0, 0
	for i, line := range lines {
		got, changed := m.scrub(line)
		want, labels := scrubNaive(line, cands, true)
		if got != want {
			t.Fatalf("line %d: the matcher and a plain search disagree:\n%s\n%s", i, got, want)
		}
		l, leak := leaks[i]
		switch {
		case leak && len(l.value) >= MinScrubbedValue && leaked(got, l.value):
			missed++
		case !leak && changed:
			altered++
			for _, lb := range labels {
				if ms.classes[lb] != classCredential || !slices.ContainsFunc(valueOf[lb], func(v string) bool { return slices.Contains(words, v) }) {
					t.Errorf("line %d: %s replaced in a line without a leak: %s", i, lb, got)
				}
			}
		}
	}
	if missed > 0 {
		t.Errorf("%d planted leaks of at least %d bytes stayed", missed, MinScrubbedValue)
	}
	t.Logf("Cogpit's matcher (min %d, stand-alone under %d, no names), %v: %d lines without a leak altered, each where a password that is a word of it stands",
		MinScrubbedValue, boundedBelow, m, altered)
}

// BenchmarkSecretScrub is the cost of a read: 10,000 log lines through the matcher of the corpus's
// Secrets (b.N rounds of 10,000 lines).
func BenchmarkSecretScrub(b *testing.B) {
	ms := newMeasuredSecrets(b)
	lines, _ := measuredLogs(ms)
	m := buildSecretMatcher(ms.objs)
	total := 0
	for _, l := range lines {
		total += len(l)
	}
	b.SetBytes(int64(total))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for _, l := range lines {
			m.scrub(l)
		}
	}
}

// BenchmarkSecretMatcherBuild is the cost of a rebuild after the cached Secrets changed, for the
// corpus's Secrets and for ten times as many.
func BenchmarkSecretMatcherBuild(b *testing.B) {
	for _, n := range []int{1, 10} {
		var objs []*unstructured.Unstructured
		for i := range n {
			objs = append(objs, newMeasuredSecretsSeed(b, uint64(1+i), fmt.Sprintf("-%d", i)).objs...)
		}
		b.Run(fmt.Sprintf("%dSecrets", len(objs)), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				buildSecretMatcher(objs)
			}
		})
	}
}
