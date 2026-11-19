package engine

import (
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// secretOf builds a Secret of a type whose values are the given bytes.
func secretOf(name, typ string, data map[string][]byte) *unstructured.Unstructured {
	d := map[string]any{}
	for k, v := range data {
		d[k] = base64.StdEncoding.EncodeToString(v)
	}
	return testkit.Object("v1", "Secret", "shop", name, map[string]any{"type": typ, "data": d})
}

// keyForms returns every form a private key's value could leak in: the value, its base64, the
// PEM body's lines and its DER in base64.
func keyForms(key []byte) []string {
	out := []string{string(key), base64.StdEncoding.EncodeToString(key)}
	for _, line := range strings.Split(string(key), "\n") {
		if len(line) >= 16 && !strings.HasPrefix(line, "-----") {
			out = append(out, line)
		}
	}
	if b, _ := pem.Decode(key); b != nil {
		out = append(out, base64.StdEncoding.EncodeToString(b.Bytes))
	} else {
		out = append(out, base64.StdEncoding.EncodeToString(key)[:40])
	}
	return out
}

func assertNoKey(t *testing.T, what string, b []byte, key []byte) {
	t.Helper()
	for _, f := range keyForms(key) {
		if f != "" && bytes.Contains(b, []byte(f)) {
			t.Errorf("%s carries the private key (%q…)", what, f[:min(len(f), 24)])
		}
	}
	if bytes.Contains(b, []byte("PRIVATE KEY")) {
		t.Errorf("%s carries a PEM private key header", what)
	}
}

// A TLS Secret's tls.key is compared with tls.crt's first certificate: Yes for a matching RSA,
// ECDSA or Ed25519 pair, No (an error) for another key, "Key not readable" with the reason for an
// encrypted key or garbage; agents read keyMatches as true, false or the reason. The key appears
// nowhere in the drawer model, in no form.
func TestSecretKeyMatchesCertificate(t *testing.T) {
	ca := testkit.NewCert(t, testkit.CertSpec{CommonName: "CA", CA: true, Key: testkit.Ed25519Key(t)})
	rsaLeaf := testkit.NewCert(t, testkit.CertSpec{CommonName: "rsa", Parent: ca, Key: testkit.RSAKey(t, 2048)})
	ecLeaf := testkit.NewCert(t, testkit.CertSpec{CommonName: "ecdsa", Parent: ca, Key: testkit.ECDSAKey(t)})
	edLeaf := testkit.NewCert(t, testkit.CertSpec{CommonName: "ed25519", Parent: ca, Key: testkit.Ed25519Key(t)})
	other := testkit.NewCert(t, testkit.CertSpec{CommonName: "other", Parent: ca})
	encrypted := pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: []byte("an encrypted key, made up")})
	garbage := []byte("-----BEGIN PRIVATE KEY-----\nbm90IGEga2V5IGF0IGFsbCwgZ2FyYmFnZQ==\n-----END PRIVATE KEY-----\n")
	for _, c := range []struct {
		name   string
		cert   *testkit.TestCert
		key    []byte
		value  string
		status model.Status
		agent  string
	}{
		{"RSA", rsaLeaf, rsaLeaf.KeyPEM(t), "Yes (tls.key)", model.StatusOK, "true"},
		{"ECDSA", ecLeaf, ecLeaf.KeyPEM(t), "Yes (tls.key)", model.StatusOK, "true"},
		{"Ed25519", edLeaf, edLeaf.KeyPEM(t), "Yes (tls.key)", model.StatusOK, "true"},
		{"mismatched", ecLeaf, other.KeyPEM(t), "No (tls.key holds another key)", model.StatusError, "false"},
		{"encrypted", ecLeaf, encrypted, "Encrypted key: TLS and Ingress cannot use it (tls.key)", model.StatusWarning, `"encrypted"`}, // A100
		{"garbage", ecLeaf, garbage, "Key not readable (tls.key: not readable)", model.StatusUnknown, `"not readable"`},
	} {
		obj := secretOf("tls-"+c.name, tlsSecretType, map[string][]byte{"tls.crt": testkit.Chain(c.cert, ca), "tls.key": c.key})
		m := certDetail(t, obj)
		checks, ok := checkSections(m)["tls.crt"]
		if !ok {
			t.Fatalf("%s: no Checks section: %+v", c.name, m.Sections)
		}
		if it := itemOf(checks, keyMatchItem); it.Value != c.value || it.Status != c.status {
			t.Errorf("%s: %+v", c.name, it)
		}
		if it := itemOf(checks, chainItem); it.Value != "Yes, each certificate is signed by the next" || it.Status != model.StatusOK {
			t.Errorf("%s: chain %+v", c.name, it)
		}
		ck := m.Certificates[0]
		if ck.PairedKey != "tls.key" || ck.KeyMatches == nil || ck.ChainInOrder == nil || !*ck.ChainInOrder {
			t.Errorf("%s: for agents %+v", c.name, ck)
		}
		b, _ := json.Marshal(m)
		if !strings.Contains(string(b), `"keyMatches":`+c.agent) || !strings.Contains(string(b), `"pairedKey":"tls.key"`) {
			t.Errorf("%s: agents do not read keyMatches %s", c.name, c.agent)
		}
		assertNoKey(t, c.name+": the drawer model", b, c.key)
	}
}

// The pairing is Cogpit's default: <base>.crt with <base>.key or <base>-key.pem when exactly one
// is there, else the one key with certificates with the one key named like a private key; nothing
// when that is ambiguous. A chain is checked for a paired value or one that starts with a leaf,
// not for a CA bundle; a chain out of order is an error naming where it breaks.
func TestSecretCertificatePairingAndChain(t *testing.T) {
	leaf, inter, root := func() (*testkit.TestCert, *testkit.TestCert, *testkit.TestCert) {
		f := newCertFixture(t)
		return f.leaf, f.inter, f.root
	}()
	otherRoot := testkit.NewCert(t, testkit.CertSpec{CommonName: "Other Root", CA: true, Key: testkit.Ed25519Key(t)})
	key := leaf.KeyPEM(t)
	for _, c := range []struct {
		name   string
		data   map[string][]byte
		paired map[string]string // certificate key → paired key; absent: none
	}{
		{"base.key", map[string][]byte{"server.crt": testkit.Chain(leaf, inter), "server.key": key}, map[string]string{"server.crt": "server.key"}},
		{"base-key.pem", map[string][]byte{"server.pem": testkit.Chain(leaf, inter), "server-key.pem": key}, map[string]string{"server.pem": "server-key.pem"}},
		{"one and one", map[string][]byte{"cert.pem": testkit.Chain(leaf, inter), "privkey.pem": key}, map[string]string{"cert.pem": "privkey.pem"}},
		{"Let's Encrypt's files", map[string][]byte{"cert.pem": leaf.PEM(), "chain.pem": inter.PEM(), "fullchain.pem": testkit.Chain(leaf, inter), "privkey.pem": key}, nil},
		{"two keys for one base", map[string][]byte{"srv.crt": leaf.PEM(), "srv.key": key, "srv-key.pem": key}, nil},
		{"ca.crt beside tls.crt", map[string][]byte{"tls.crt": testkit.Chain(leaf, inter), "tls.key": key, "ca.crt": root.PEM()}, map[string]string{"tls.crt": "tls.key"}},
	} {
		m := certDetail(t, secretOf("pairs", "Opaque", c.data))
		for _, ck := range m.Certificates {
			want := c.paired[ck.Key]
			if ck.PairedKey != want {
				t.Errorf("%s: %s paired with %q, want %q", c.name, ck.Key, ck.PairedKey, want)
			}
			if want != "" && (ck.KeyMatches == nil || !ck.KeyMatches.Matches) {
				t.Errorf("%s: %s does not match: %+v", c.name, ck.Key, ck.KeyMatches)
			}
		}
		b, _ := json.Marshal(m)
		assertNoKey(t, c.name+": the drawer model", b, key)
	}

	broken := certDetail(t, secretOf("broken", tlsSecretType, map[string][]byte{"tls.crt": testkit.Chain(leaf, root), "tls.key": key}))
	if it := itemOf(checkSections(broken)["tls.crt"], chainItem); it.Value != "No, certificate 1 is not signed by certificate 2" || it.Status != model.StatusError {
		t.Errorf("a chain without its intermediate: %+v", it)
	}
	if ck := broken.Certificates[0]; ck.ChainInOrder == nil || *ck.ChainInOrder || ck.ChainBreak != 1 {
		t.Errorf("for agents %+v", ck)
	}
	bundle := certDetail(t, secretOf("bundle", "Opaque", map[string][]byte{"ca.crt": testkit.Chain(root, otherRoot)}))
	if _, ok := checkSections(bundle)["ca.crt"]; ok || bundle.Certificates[0].ChainInOrder != nil {
		t.Errorf("a CA bundle was checked as a chain: %+v", bundle.Certificates[0])
	}
	unpaired := certDetail(t, secretOf("unpaired", "Opaque", map[string][]byte{"web.crt": testkit.Chain(leaf, inter, root)}))
	if it := itemOf(checkSections(unpaired)["web.crt"], chainItem); it.Status != model.StatusOK || itemOf(checkSections(unpaired)["web.crt"], keyMatchItem).Value != "" {
		t.Errorf("a leaf's chain without a key: %+v", checkSections(unpaired))
	}
}

// DER certificates are decoded like PEM ones (a DER ca.der, a DER tls.crt with its key); DER that
// is no certificate shows nothing: garbage under a certificate's name, and a DER private key under
// one, whose bytes appear nowhere.
func TestSecretDERCertificates(t *testing.T) {
	f := newCertFixture(t)
	keyDER, err := x509.MarshalPKCS8PrivateKey(f.leaf.Key)
	if err != nil {
		t.Fatal(err)
	}
	tls := certDetail(t, secretOf("der-tls", tlsSecretType, map[string][]byte{
		"tls.crt": bytes.Join([][]byte{f.leaf.Cert.Raw, f.inter.Cert.Raw}, nil), "tls.key": f.keyPEM,
	}))
	if ck := tls.Certificates[0]; !ck.DER || len(ck.Certificates) != 2 || ck.Certificates[0].SubjectCN != "storefront.example.com" ||
		ck.KeyMatches == nil || !ck.KeyMatches.Matches {
		t.Errorf("a DER tls.crt: %+v", ck)
	}
	m := certDetail(t, secretOf("der", "Opaque", map[string][]byte{
		"ca.der":     bytes.Clone(f.root.Cert.Raw),
		"blob.crt":   []byte("\x30\x03\x02\x01\x07 not a certificate"),
		"server.crt": keyDER, // a DER private key under a certificate's name
	}))
	var keys []string
	for _, ck := range m.Certificates {
		keys = append(keys, ck.Key)
	}
	if strings.Join(keys, ",") != "ca.der" || !m.Certificates[0].DER || m.Certificates[0].Certificates[0].SubjectCN != "Example Root CA" {
		t.Fatalf("DER keys %v: %+v", keys, m.Certificates)
	}
	if secs := certSections(m); len(secs["ca.der"]) != 1 || len(secs["blob.crt"]) != 0 || len(secs["server.crt"]) != 0 {
		t.Errorf("sections %v", secs)
	}
	b, _ := json.Marshal(m)
	if bytes.Contains(b, []byte(base64.StdEncoding.EncodeToString(keyDER))) || bytes.Contains(b, []byte(base64.StdEncoding.EncodeToString(keyDER)[:40])) {
		t.Error("the DER private key reached the drawer model")
	}
	none := certDetail(t, secretOf("not-a-cert", tlsSecretType, map[string][]byte{"tls.crt": keyDER, "tls.key": keyDER}))
	if secs := certSections(none)["tls.crt"]; len(secs) != 1 || secs[0].Items[0].Value != certNone {
		t.Errorf("a TLS Secret whose tls.crt holds a DER key: %+v", certSections(none))
	}
	b, _ = json.Marshal(none)
	if bytes.Contains(b, []byte(base64.StdEncoding.EncodeToString(keyDER)[:40])) {
		t.Error("the DER private key reached the drawer model")
	}
}

// A combined PEM, the certificate and its private key in one value, gets the key check against
// the value's own key (A99, owner); a value with certificates alone does not.
func TestSecretCombinedPEMKeyCheck(t *testing.T) {
	ca := testkit.NewCert(t, testkit.CertSpec{CommonName: "CA", CA: true, Key: testkit.Ed25519Key(t)})
	leaf := testkit.NewCert(t, testkit.CertSpec{CommonName: "web", Parent: ca, Key: testkit.ECDSAKey(t)})
	other := testkit.NewCert(t, testkit.CertSpec{CommonName: "other", Parent: ca})
	for _, c := range []struct {
		name, value string
		key         []byte
		status      model.Status
	}{
		{"own key", "Yes (in this value)", leaf.KeyPEM(t), model.StatusOK},
		{"another key", "No (in this value holds another key)", other.KeyPEM(t), model.StatusError},
	} {
		combined := append(append([]byte{}, testkit.Chain(leaf, ca)...), c.key...)
		m := certDetail(t, secretOf("combined-"+strings.ReplaceAll(c.name, " ", "-"), "Opaque", map[string][]byte{"server.pem": combined}))
		checks, ok := checkSections(m)["server.pem"]
		if !ok {
			t.Fatalf("%s: no Checks section: %+v", c.name, m.Sections)
		}
		if it := itemOf(checks, keyMatchItem); it.Value != c.value || it.Status != c.status {
			t.Errorf("%s: %+v", c.name, it)
		}
		if b, _ := json.Marshal(m); strings.Contains(string(b), "PRIVATE KEY") {
			t.Errorf("%s: the key reached the model", c.name)
		}
	}
	m := certDetail(t, secretOf("certs-only", "Opaque", map[string][]byte{"ca.pem": testkit.Chain(leaf, ca)}))
	if checks, ok := checkSections(m)["ca.pem"]; ok && itemOf(checks, keyMatchItem).Value != "" {
		t.Errorf("a value without a key got a key check: %+v", checks)
	}
}

// A checked chain says where it leads (R1): a chain to an internal CA names it, in the Checks
// section and for agents.
func TestSecretChainAnchor(t *testing.T) {
	ca := testkit.NewCert(t, testkit.CertSpec{CommonName: "Corp Root", CA: true, Key: testkit.Ed25519Key(t)})
	leaf := testkit.NewCert(t, testkit.CertSpec{CommonName: "web", Parent: ca, Key: testkit.ECDSAKey(t)})
	m := certDetail(t, secretOf("anchor", tlsSecretType, map[string][]byte{"tls.crt": testkit.Chain(leaf, ca), "tls.key": leaf.KeyPEM(t)}))
	checks, ok := checkSections(m)["tls.crt"]
	if !ok {
		t.Fatalf("no Checks section: %+v", m.Sections)
	}
	if it := itemOf(checks, anchorItem); it.Value != "An internal or self-signed CA (Corp Root)" {
		t.Errorf("anchor %+v", it)
	}
	if a := m.Certificates[0].Anchor; a == nil || a.Kind != model.AnchorInternal || a.Root != "Corp Root" {
		t.Errorf("for agents %+v", a)
	}
}
