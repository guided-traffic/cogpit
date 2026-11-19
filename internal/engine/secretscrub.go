package engine

import (
	"cmp"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// The cached Secrets' values in what agents read (agents-and-mcp H-82, owner): a value of a Secret
// Cogpit's cache holds, at least MinScrubbedValue bytes long, is replaced by «secret
// <namespace>/<name>/<key>» wherever an agent reads cluster text: a logs or terminal tab's lines
// (dock.read), an object's YAML, drawer values and events (object.get), an editor's text and diff
// (agentview.go). The window shows the text as it is. The placeholder names the Secret and key,
// never any part of the value. A value under 16 bytes is replaced only where it stands alone
// (boundedBelow), and the values of keys that name things (a user, a host, a database:
// isNameKey) are left out: both measured (TestSecretScrubMeasure).
//
// The matcher is built from the Secrets the cache holds already (cache.Cache.Peek: no list, no
// watch, no warm period extended; cache first) and built again only when they changed
// (cache.Cache.PeekVersion), never per read. A read scans its text once, whatever the number of
// values: each 8-byte window of the text is looked up in a bit filter, and only the rare window
// that passes it in an index of the values' windows, so 10,000 log lines cost about 2.5 ms
// (BenchmarkSecretScrub, 1.3 ns per byte) instead of one search per value and line (about 700 ms,
// BenchmarkSecretScrubPlainSearch). The values never reach a log, an error or the audit log: the
// matcher prints as a count.

// Bounds of the values the matcher replaces.
const (
	// MinScrubbedValue is the shortest value (in bytes, in each of its forms) that is replaced:
	// shorter ones ("admin", "5432", "true") would replace ordinary words in every log line.
	// Measured over 10,000 log lines (TestSecretScrubMeasure, agents-and-mcp H-82); 10 bytes
	// instead of 8 decided by the owner (A5).
	MinScrubbedValue = 10
	// maxScrubbedValue bounds a value the matcher holds: a longer one (a keystore, a bundle) is
	// never printed whole into a log line, which the dock cuts at 8 KiB anyway.
	maxScrubbedValue = 64 << 10
	// minPEMLine is the shortest body line of a PEM private key replaced on its own.
	minPEMLine = 16
)

// scrubWindow is the width of the windows the matcher indexes, a uint64 key (windowKey); at most
// MinScrubbedValue, so every replaced value holds one.
const scrubWindow = 8

// Secret types and keys whose values are not secret: a service account token Secret's namespace
// and CA certificate. Every certificate is public too (D-30): a value that holds certificates only
// is not replaced, so the cluster's CA in every namespace's kube-root-ca.crt stays readable.
var publicSecretKeys = map[string][]string{
	"kubernetes.io/service-account-token": {namespaceKey, "ca.crt"},
	"bootstrap.kubernetes.io/token": {"token-id", "description", "expiration", "auth-extra-groups",
		"usage-bootstrap-authentication", "usage-bootstrap-signing"},
}

// namespaceKey is a data key, and a key's last word, that holds a namespace (a service account token
// Secret's, for one).
const namespaceKey = "namespace"

// helmReleaseSecretType is a Helm release record: the release, its values included, gzipped and
// base64-encoded twice, which no log prints; Secrets of this type are left out.
const helmReleaseSecretType = "helm.sh/release.v1" // #nosec G101 -- a Secret type's name, no credential

// SecretPlaceholder is what an agent reads in place of a value of the Secret namespace/name, key
// key.
func SecretPlaceholder(namespace, name, key string) string {
	return "«secret " + namespace + "/" + name + "/" + key + "»"
}

// secretPlaceholderPrefix starts every placeholder: an agent's text that holds it is refused a
// write (agentview.go), since writing it back would replace the value in the cluster.
const secretPlaceholderPrefix = "«secret "

// scrubPattern is one form of one value.
type scrubPattern struct {
	value string
	off   int    // the offset of the window the index holds it under
	label string // the placeholder
	// bounded: a short value (under boundedBelow bytes) is replaced only where it stands on its
	// own, not inside a longer word ("postgres" in "postgresql", "admin123" in "admin1234").
	bounded bool
}

// boundedBelow is the length under which a value is replaced only where no letter, digit, '_' or
// '-' touches it (measured: TestSecretScrubMeasure). Longer values are replaced wherever they
// stand: a token inside a longer string is still the token.
const boundedBelow = 16

// wordByte reports whether c continues a word around a short value: a letter, a digit, '_' or '-'
// (a Kubernetes name's characters, but '.').
func wordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

// standsAlone reports whether s[start:end] is touched by no word byte (wordByte).
func standsAlone(s string, start, end int) bool {
	return (start == 0 || !wordByte(s[start-1])) && (end == len(s) || !wordByte(s[end]))
}

// secretMatcher replaces the values of a set of Secrets. It is immutable once built and safe for
// concurrent use.
type secretMatcher struct {
	pats   []scrubPattern
	index  map[uint64][]int32
	filter [1 << 16 / 64]uint64
	values int // the values (not their forms) it replaces
}

// String keeps the values out of every print: %v, %+v, %#v and slog show a count.
func (m *secretMatcher) String() string {
	if m == nil {
		return "secretMatcher(none)"
	}
	return fmt.Sprintf("secretMatcher(%d values, %d forms)", m.values, len(m.pats))
}

// GoString is String.
func (m *secretMatcher) GoString() string { return m.String() }

// LogValue is String.
func (m *secretMatcher) LogValue() slog.Value { return slog.StringValue(m.String()) }

// windowKey is the 8 bytes of s at i, little-endian: the scan rolls the same key byte by byte.
func windowKey(s string, i int) uint64 {
	var k uint64
	for j := range scrubWindow {
		k |= uint64(s[i+j]) << (8 * j)
	}
	return k
}

// filterBit is a window key's bit in the filter.
func filterBit(k uint64) uint64 { return (k * 0x9E3779B97F4A7C15) >> 48 }

// scrubCandidate is a value form before the index is built.
type scrubCandidate struct{ value, label string }

// buildSecretMatcher makes the matcher of secrets: each Secret once (by namespace and name; the
// cache may hold one in two scopes), in namespace, name and key order, so that a value two
// Secrets share is labelled by the first.
func buildSecretMatcher(secrets []*unstructured.Unstructured) *secretMatcher {
	return newSecretMatcher(secretCandidates(secrets, MinScrubbedValue, true))
}

// nameKeyWords are the last words of the data keys whose values name things, not credentials: a
// user, a host, a port, a database, a namespace. Their values (a database named after its
// application, "postgres", a service's host name) stand in every log line about the application,
// and replacing them hid what the lines are about: 1,238 of the 10,000 measured lines, none of
// them a leak (TestSecretScrubMeasure, H-82). They are left out, as Argo CD's Secrets show such
// keys (D-35); the lead's decision for the owner to confirm (D-44).
var nameKeyWords = []string{"user", "username", "login", "host", "hostname", "server", "port", "database", "db", "dbname",
	namespaceKey, "schema", "region", "email"}

// argoPublicKeys are the keys of an Argo CD repository or cluster Secret whose values D-35 shows the
// window and agents (the owner's decision): replacing them would hide from an agent what its
// object_get of the repository shows, its URL. A URL's user information is replaced in what D-35
// shows; a log that prints it keeps it, which H-82 names.
func argoPublicKeys(s *unstructured.Unstructured) []string {
	switch s.GetLabels()[kinds.ArgoSecretTypeLabel] {
	case "repository", "repo-creds":
		return kinds.ArgoRepositoryKeys
	case "cluster":
		return kinds.ArgoClusterKeys
	}
	return nil
}

// isNameKey reports whether a data key holds a name by its last word, case and separators ('-',
// '_', '.') aside: "username", "POSTGRES_USER", "db.host", "MYSQL_DATABASE"; not "password",
// "DB_PASSWORD", "url" (a URL can carry a password).
func isNameKey(key string) bool {
	words := strings.FieldsFunc(strings.ToLower(key), func(r rune) bool { return r == '-' || r == '_' || r == '.' })
	return len(words) > 0 && slices.Contains(nameKeyWords, words[len(words)-1])
}

// secretCandidates are the forms of secrets' values at least minLen bytes long, the values of
// name keys left out when skipNames is set (isNameKey), and the number of values they come from.
func secretCandidates(secrets []*unstructured.Unstructured, minLen int, skipNames bool) ([]scrubCandidate, int) {
	secrets = slices.Clone(secrets)
	slices.SortFunc(secrets, func(a, b *unstructured.Unstructured) int {
		return cmp.Or(strings.Compare(a.GetNamespace(), b.GetNamespace()), strings.Compare(a.GetName(), b.GetName()))
	})
	secrets = slices.CompactFunc(secrets, func(a, b *unstructured.Unstructured) bool {
		return a.GetNamespace() == b.GetNamespace() && a.GetName() == b.GetName()
	})
	var cands []scrubCandidate
	values := 0
	for _, s := range secrets {
		typ, _ := s.Object["type"].(string)
		if typ == helmReleaseSecretType {
			continue
		}
		public := argoPublicKeys(s)
		data, _ := s.Object["data"].(map[string]any)
		keys := make([]string, 0, len(data))
		for k := range data {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			enc, ok := data[k].(string)
			if !ok || slices.Contains(publicSecretKeys[typ], k) || slices.Contains(public, k) || skipNames && isNameKey(k) {
				continue
			}
			label := SecretPlaceholder(s.GetNamespace(), s.GetName(), k)
			n := len(cands)
			cands = appendValueForms(cands, enc, typ, label, minLen)
			if len(cands) > n {
				values++
			}
		}
	}
	return cands, values
}

// appendValueForms adds the forms a value can take in text: as stored (base64), decoded with its
// surrounding white space trimmed, decoded as JSON escapes it (a JSON log line), each body line of
// a PEM private key, and the passwords and tokens inside a registry credential. A value that holds
// certificates only is public (D-30) and adds nothing.
func appendValueForms(cands []scrubCandidate, enc, typ, label string, minLen int) []scrubCandidate {
	add := func(v string) {
		if len(v) >= minLen && len(v) <= maxScrubbedValue {
			cands = append(cands, scrubCandidate{value: v, label: label})
		}
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		add(enc) // not base64: the API server would not have stored it; keep what is there
		return cands
	}
	if certificatesOnly(raw) {
		return cands
	}
	add(enc)
	text := strings.TrimSpace(string(raw))
	add(text)
	if utf8.ValidString(text) {
		for _, q := range jsonForms(text) {
			if q != text {
				add(q)
			}
		}
	}
	if strings.Contains(text, "PRIVATE KEY-----") {
		for line := range strings.Lines(text) {
			line = strings.TrimSpace(line)
			if len(line) >= max(minPEMLine, minLen) && !strings.HasPrefix(line, "-----") && !strings.Contains(line, ":") {
				add(line)
			}
		}
	}
	if typ == "kubernetes.io/dockerconfigjson" || typ == "kubernetes.io/dockercfg" {
		for _, v := range registrySecrets(raw) {
			add(v)
		}
	}
	return cands
}

// jsonForms are s as JSON strings escape it, without the quotes: Go's encoder (<, > and & as
// <…) and one that leaves them.
func jsonForms(s string) []string {
	var out []string
	if b, err := json.Marshal(s); err == nil {
		out = append(out, string(b[1:len(b)-1]))
	}
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if enc.Encode(s) == nil {
		q := strings.TrimSuffix(sb.String(), "\n")
		out = append(out, q[1:len(q)-1])
	}
	return out
}

// certificatesOnly reports whether raw holds certificates and nothing else: PEM blocks of
// certificates only, or one DER certificate. It decodes no private key.
func certificatesOnly(raw []byte) bool {
	text := string(raw)
	if n := strings.Count(text, "-----BEGIN "); n > 0 {
		return n == strings.Count(text, "-----BEGIN CERTIFICATE-----")+strings.Count(text, "-----BEGIN TRUSTED CERTIFICATE-----")
	}
	if len(raw) > 0 && raw[0] == 0x30 { // a DER SEQUENCE
		_, err := x509.ParseCertificate(raw)
		return err == nil
	}
	return false
}

// registrySecrets are the passwords, auth strings and tokens of a registry credential
// (.dockerconfigjson's auths, or the older .dockercfg's top level).
func registrySecrets(raw []byte) []string {
	type entry struct {
		Password      string `json:"password"`
		Auth          string `json:"auth"`
		IdentityToken string `json:"identitytoken"`
		RegistryToken string `json:"registrytoken"`
	}
	var doc struct {
		Auths map[string]entry `json:"auths"`
	}
	var out []string
	collect := func(m map[string]entry) {
		for _, e := range m {
			out = append(out, e.Password, e.Auth, e.IdentityToken, e.RegistryToken)
		}
	}
	if json.Unmarshal(raw, &doc) == nil && len(doc.Auths) > 0 {
		collect(doc.Auths)
		return out
	}
	var old map[string]entry
	if json.Unmarshal(raw, &old) == nil {
		collect(old)
	}
	return out
}

// newSecretMatcher indexes the candidates, each distinct value once (the first label wins): under
// the window, among up to four spread over the value, that the fewest values share, so that values
// with a common start (every service account token begins with the same JWT header) do not pile up
// under one key.
func newSecretMatcher(cands []scrubCandidate, values int) *secretMatcher {
	m := &secretMatcher{index: map[uint64][]int32{}, values: values}
	seen := map[string]bool{}
	for _, c := range cands {
		if seen[c.value] {
			continue
		}
		seen[c.value] = true
		last := len(c.value) - scrubWindow
		best, bestKey, bestN := 0, uint64(0), -1
		for _, off := range []int{last / 2, last, 0, last / 4} {
			k := windowKey(c.value, off)
			if n := len(m.index[k]); bestN < 0 || n < bestN {
				best, bestKey, bestN = off, k, n
			}
		}
		m.index[bestKey] = append(m.index[bestKey], int32(len(m.pats))) // #nosec G115 -- bounded by the cache's Secrets
		m.pats = append(m.pats, scrubPattern{value: c.value, off: best, label: c.label, bounded: len(c.value) < boundedBelow})
		b := filterBit(bestKey)
		m.filter[b>>6] |= 1 << (b & 63)
	}
	return m
}

// scrubSpan is one occurrence of a value in a text.
type scrubSpan struct {
	start, end int
	pat        int32
}

// find returns every occurrence of a value in s, in no particular order.
func (m *secretMatcher) find(s string) []scrubSpan {
	n := len(s)
	if m == nil || len(m.pats) == 0 || n < scrubWindow {
		return nil
	}
	var spans []scrubSpan
	k := windowKey(s, 0)
	for i := 0; ; i++ {
		if b := filterBit(k); m.filter[b>>6]&(1<<(b&63)) != 0 {
			for _, pi := range m.index[k] {
				p := &m.pats[pi]
				start := i - p.off
				end := start + len(p.value)
				if start >= 0 && end <= n && s[start:end] == p.value && (!p.bounded || standsAlone(s, start, end)) {
					spans = append(spans, scrubSpan{start: start, end: end, pat: pi})
				}
			}
		}
		if i+scrubWindow >= n {
			return spans
		}
		k = k>>8 | uint64(s[i+scrubWindow])<<56
	}
}

// scrub returns s with every occurrence of a value replaced by its placeholder, and whether it
// replaced any. Occurrences that overlap are replaced together, by the placeholder of the one that
// starts first (the longest of those), so that no part of a value stays.
func (m *secretMatcher) scrub(s string) (string, bool) {
	spans := m.find(s)
	if len(spans) == 0 {
		return s, false
	}
	return replaceSpans(s, spans, m), true
}

// replaceSpans replaces the spans of s by their patterns' placeholders, overlapping ones together.
func replaceSpans(s string, spans []scrubSpan, m *secretMatcher) string {
	slices.SortFunc(spans, func(a, b scrubSpan) int { return cmp.Or(cmp.Compare(a.start, b.start), cmp.Compare(b.end, a.end)) })
	var b strings.Builder
	b.Grow(len(s))
	last := 0
	for i := 0; i < len(spans); {
		start, end, label := spans[i].start, spans[i].end, m.pats[spans[i].pat].label
		j := i + 1
		for ; j < len(spans) && spans[j].start < end; j++ {
			end = max(end, spans[j].end)
		}
		b.WriteString(s[last:start])
		b.WriteString(label)
		last, i = end, j
	}
	b.WriteString(s[last:])
	return b.String()
}

// secretScrub holds a backend's matcher and the version of the cached Secrets it was built from.
// Its zero value is ready to use.
type secretScrub struct {
	mu      sync.Mutex
	built   bool
	version string
	m       *secretMatcher
}

// cachedSecrets selects the cache's scopes that hold Secrets with their values: the Secrets view's
// (in all namespaces or per namespace) and label-selected ones (Helm's release records).
func cachedSecrets(s cache.Scope) bool {
	return s.GVR.Group == "" && s.GVR.Resource == "secrets" && !s.MetadataOnly
}

// secretMatcher returns the matcher of the Secrets b's cache holds now, built again only when they
// changed since the last call; nil without a cache.
func (b *Backend) secretMatcher() *secretMatcher {
	if b == nil || b.Cache == nil {
		return nil
	}
	version := b.Cache.PeekVersion(cachedSecrets) // before the objects: they are at least this new
	sc := &b.scrub
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.built && sc.version == version {
		return sc.m
	}
	var secrets []*unstructured.Unstructured
	for _, p := range b.Cache.Peek(cachedSecrets) {
		secrets = append(secrets, p.Objects...)
	}
	sc.m, sc.version, sc.built = buildSecretMatcher(secrets), version, true
	return sc.m
}
