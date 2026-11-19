package kubectl

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sigstore/sigstore-go/pkg/root"

	"github.com/guided-traffic/cogpit/internal/testkit/sigstoretest"
)

// release is one real kubectl release's published signature and Rekor's answer for it, read from
// dl.k8s.io and rekor.sigstore.dev (darwin/arm64; 1.36.1 and 1.37.1). Public material: the files
// every download reads.
type release struct {
	signed  *Signed
	entries []LogEntry
	answer  []byte // Rekor's answer as it came
}

func readRelease(t *testing.T, version string) release {
	t.Helper()
	read := func(ext string) []byte {
		data, err := os.ReadFile("testdata/kubectl-v" + version + "-darwin-arm64." + ext)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	digest, err := parseSHA256(read("sha256"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseSigned(digest, read("sig"), read("cert"))
	if err != nil {
		t.Fatalf("kubectl %s's signature: %v", version, err)
	}
	answer := read("rekor.json")
	entries, err := parseRekorAnswer(answer)
	if err != nil || len(entries) != 1 {
		t.Fatalf("kubectl %s's log entries: %d, %v", version, len(entries), err)
	}
	return release{signed: s, entries: entries, answer: answer}
}

// TestRealReleaseSignature verifies kubectl 1.36.1's and 1.37.1's published signatures with the
// embedded trusted root, offline, as a download does with what dl.k8s.io and Rekor answer: the
// release signs the SHA-256 it publishes, so the signature is checked without the 57 MB binary.
// The certificates carry a signed certificate timestamp, which the default verifier asks for.
func TestRealReleaseSignature(t *testing.T) {
	for _, version := range []string{"1.36.1", "1.37.1"} {
		r := readRelease(t, version)
		if err := DefaultVerifier().Verify(r.signed, r.entries); err != nil {
			t.Errorf("kubectl %s: %v", version, err)
		}
		if d := r.signed.Cert.NotAfter.Sub(r.signed.Cert.NotBefore); d > 15*time.Minute {
			t.Errorf("kubectl %s: a signing certificate valid for %s", version, d)
		}
	}
}

// TestRealReleaseRefused: each part of the check refuses the real releases' material on its own.
func TestRealReleaseRefused(t *testing.T) {
	r136, r137 := readRelease(t, "1.36.1"), readRelease(t, "1.37.1")
	changed := func(change func(e *LogEntry)) []LogEntry {
		e := r137.entries[0]
		e.Body = append([]byte(nil), e.Body...)
		e.Verification.SignedEntryTimestamp = append([]byte(nil), e.Verification.SignedEntryTimestamp...)
		p := *e.Verification.InclusionProof
		p.Hashes = append([]string(nil), p.Hashes...)
		e.Verification.InclusionProof = &p
		change(&e)
		return []LogEntry{e}
	}
	flipHex := func(h string) string {
		b, _ := hex.DecodeString(h)
		b[0] ^= 1
		return hex.EncodeToString(b)
	}
	otherSigner := func(s Signer) *Verifier {
		tr, err := root.NewTrustedRootFromJSON(sigstoreTrustedRoot)
		if err != nil {
			t.Fatal(err)
		}
		v, err := NewVerifier(tr, s, 1)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	cases := map[string]struct {
		v       *Verifier
		s       *Signed
		entries []LogEntry
		want    error
	}{
		"another identity": {otherSigner(Signer{Identity: "someone@example.com", Issuer: KubernetesRelease.Issuer}), r137.signed, r137.entries, ErrSignature},
		"another issuer":   {otherSigner(Signer{Identity: KubernetesRelease.Identity, Issuer: "https://token.actions.githubusercontent.com"}), r137.signed, r137.entries, ErrSignature},
		"no log entry":     {DefaultVerifier(), r137.signed, nil, ErrNotLogged},
		// Rekor's entry for another release: its digest, signature and certificate are not these.
		"another artifact's entry": {DefaultVerifier(), r137.signed, r136.entries, ErrSignature},
		// The signature and certificate of 1.37.1 over a digest that is not the one they signed.
		"another digest": {DefaultVerifier(), &Signed{Digest: r136.signed.Digest, Sig: r137.signed.Sig, Cert: r137.signed.Cert}, r137.entries, ErrSignature},
		"a changed signed entry timestamp": {DefaultVerifier(), r137.signed, changed(func(e *LogEntry) {
			e.Verification.SignedEntryTimestamp[10] ^= 1
		}), ErrSignature},
		"a changed integrated time": {DefaultVerifier(), r137.signed, changed(func(e *LogEntry) { e.IntegratedTime++ }), ErrSignature},
		"a changed inclusion proof": {DefaultVerifier(), r137.signed, changed(func(e *LogEntry) {
			e.Verification.InclusionProof.Hashes[3] = flipHex(e.Verification.InclusionProof.Hashes[3])
		}), ErrSignature},
		"a changed checkpoint": {DefaultVerifier(), r137.signed, changed(func(e *LogEntry) {
			e.Verification.InclusionProof.Checkpoint = strings.Replace(e.Verification.InclusionProof.Checkpoint, "\n3133599539\n", "\n3133599540\n", 1)
		}), ErrSignature},
		"no inclusion proof": {DefaultVerifier(), r137.signed, changed(func(e *LogEntry) { e.Verification.InclusionProof = nil }), ErrSignature},
		"a log the trusted root does not know": {DefaultVerifier(), r137.signed, changed(func(e *LogEntry) {
			e.LogID = flipHex(e.LogID)
		}), ErrSignature},
		"a changed body": {DefaultVerifier(), r137.signed, changed(func(e *LogEntry) {
			e.Body = []byte(strings.Replace(string(e.Body), `"hashedrekord"`, `"hashedrekord" `, 1))
		}), ErrSignature},
	}
	for name, c := range cases {
		err := c.v.Verify(c.s, c.entries)
		t.Logf("%s: %v", name, err)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
	// What a download reads offline already refuses a digest the signature was not made over.
	raw137, _ := os.ReadFile("testdata/kubectl-v1.37.1-darwin-arm64.sig")
	cert137, _ := os.ReadFile("testdata/kubectl-v1.37.1-darwin-arm64.cert")
	tampered := append([]byte(nil), r137.signed.Digest...)
	tampered[0] ^= 1
	if _, err := ParseSigned(tampered, raw137, cert137); !errors.Is(err, ErrSignature) {
		t.Errorf("a tampered SHA-256: %v, want ErrSignature", err)
	}
}

// TestParseSigned: the .sig and .cert files are read as dl.k8s.io publishes them, and refused when
// they are not.
func TestParseSigned(t *testing.T) {
	ss := sigstoretest.New(t)
	digest := sha256.Sum256([]byte("kubectl"))
	certFile, key := ss.Issue(t, sigstoretest.Leaf{})
	sigFile := sigstoretest.SignFile(t, key, digest[:])
	if _, err := ParseSigned(digest[:], sigFile, certFile); err != nil {
		t.Fatalf("a good pair: %v", err)
	}
	pemCert, _ := base64.StdEncoding.DecodeString(string(certFile))
	if _, err := ParseSigned(digest[:], sigFile, pemCert); err != nil {
		t.Errorf("a PEM certificate: %v", err)
	}
	_, otherKey := ss.Issue(t, sigstoretest.Leaf{})
	for name, files := range map[string][2][]byte{
		"another key's signature": {sigstoretest.SignFile(t, otherKey, digest[:]), certFile},
		"a .sig not base64":       {[]byte("***"), certFile},
		"an empty .sig":           {nil, certFile},
		"a .cert neither":         {sigFile, []byte("%%%")},
		"base64 of no PEM":        {sigFile, []byte(base64.StdEncoding.EncodeToString([]byte("hello")))},
	} {
		if _, err := ParseSigned(digest[:], files[0], files[1]); !errors.Is(err, ErrSignature) {
			t.Errorf("%s: %v, want ErrSignature", name, err)
		}
	}
}

// TestVerifierRefuses: with a sigstore instance of the test's own, a logged signature verifies,
// and every part of the certificate and the log refuses on its own.
func TestVerifierRefuses(t *testing.T) {
	ss := sigstoretest.New(t)
	v, err := NewVerifier(ss.TrustedMaterial(), KubernetesRelease, 0)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("kubectl"))
	signed := func(sigFile, certFile []byte) *Signed {
		s, err := ParseSigned(digest[:], sigFile, certFile)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	entries := func(e map[string]any) []LogEntry {
		t.Helper()
		data, err := json.Marshal([]map[string]any{{"uuid": e}})
		if err != nil {
			t.Fatal(err)
		}
		out, err := parseRekorAnswer(data)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	certFile, key := ss.Issue(t, sigstoretest.Leaf{})
	sigFile := sigstoretest.SignFile(t, key, digest[:])
	good := entries(ss.Log(t, digest[:], sigFile, certFile))
	if err := v.Verify(signed(sigFile, certFile), good); err != nil {
		t.Fatalf("a logged signature: %v", err)
	}
	// The default verifier asks for a signed certificate timestamp, which this instance cannot give.
	strict, _ := NewVerifier(ss.TrustedMaterial(), KubernetesRelease, 1)
	err = strict.Verify(signed(sigFile, certFile), good)
	t.Logf("no signed certificate timestamp: %v", err)
	if !errors.Is(err, ErrSignature) {
		t.Errorf("no signed certificate timestamp: %v", err)
	}

	logged := func(l sigstoretest.Leaf) (*Signed, []LogEntry) {
		c, k := ss.Issue(t, l)
		s := sigstoretest.SignFile(t, k, digest[:])
		return signed(s, c), entries(ss.Log(t, digest[:], s, c))
	}
	other := sigstoretest.New(t)
	foreignCert, foreignKey := other.Issue(t, sigstoretest.Leaf{})
	foreignSig := sigstoretest.SignFile(t, foreignKey, digest[:])
	for name, l := range map[string]sigstoretest.Leaf{
		"another identity":     {Identity: "mallory@example.com"},
		"another issuer":       {Issuer: "https://token.actions.githubusercontent.com"},
		"no issuer":            {NoIssuer: true},
		"not for code signing": {EKU: x509.ExtKeyUsageServerAuth},
	} {
		s, e := logged(l)
		err := v.Verify(s, e)
		t.Logf("%s: %v", name, err)
		if !errors.Is(err, ErrSignature) {
			t.Errorf("%s: %v, want ErrSignature", name, err)
		}
	}
	_, elsewhere := logged(sigstoretest.Leaf{})
	for _, c := range []struct {
		name    string
		s       *Signed
		entries []LogEntry
		want    error
	}{
		{"another instance's certificate and log", signed(foreignSig, foreignCert), entries(other.Log(t, digest[:], foreignSig, foreignCert)), ErrSignature},
		{"another authority's certificate in this log", signed(foreignSig, foreignCert), entries(ss.Log(t, digest[:], foreignSig, foreignCert)), ErrSignature},
		{"another signature's entry, same identity", signed(sigFile, certFile), elsewhere, ErrSignature},
		{"no entry", signed(sigFile, certFile), nil, ErrNotLogged},
	} {
		err := v.Verify(c.s, c.entries)
		t.Logf("%s: %v", c.name, err)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
}

// TestEmbeddedTrustedRoot: the embedded trusted root is sigstore's public-good instance: the Rekor
// log whose key signs the Kubernetes release's entries, the Fulcio root and intermediate, and a
// certificate transparency log valid now;
// and the identity is the one Kubernetes documents.
func TestEmbeddedTrustedRoot(t *testing.T) {
	v := DefaultVerifier()
	if v.Signer != KubernetesRelease || !strings.HasSuffix(v.Signer.Identity, "@k8s-releng-prod.iam.gserviceaccount.com") {
		t.Errorf("signer %+v", v.Signer)
	}
	tr, err := root.NewTrustedRootFromJSON(sigstoreTrustedRoot)
	if err != nil {
		t.Fatal(err)
	}
	const rekorV1 = "c0d23d6ad406973f9559f3ba2d1ca01f84147d8ffc5b8445c224f98b9591801d"
	if l := tr.RekorLogs()[rekorV1]; l == nil || l.BaseURL != DefaultRekor {
		t.Errorf("Rekor logs %v: no %s at %s", tr.RekorLogs(), rekorV1, DefaultRekor)
	}
	fps := map[string]bool{}
	for _, ca := range tr.FulcioCertificateAuthorities() {
		f, ok := ca.(*root.FulcioCertificateAuthority)
		if !ok {
			continue
		}
		for _, c := range append([]*x509.Certificate{f.Root}, f.Intermediates...) {
			sum := sha256.Sum256(c.Raw)
			fps[hex.EncodeToString(sum[:])] = true
		}
	}
	for _, want := range []string{
		"3ba7b6cc4e95469d4d334b49cb257ad8537076fa84b0ca87ff4ecfe6a54680c1", // CN=sigstore
		"15d795348226b4649f750f5802592c393bee7cc53c3b86982175b7ad087efe47", // CN=sigstore-intermediate
	} {
		if !fps[want] {
			t.Errorf("no certificate authority %s in %v", want, fps)
		}
	}
	now := time.Now()
	valid := 0
	for _, l := range tr.CTLogs() {
		if !l.ValidityPeriodStart.After(now) && (l.ValidityPeriodEnd.IsZero() || l.ValidityPeriodEnd.After(now)) {
			valid++
		}
	}
	if valid == 0 {
		t.Error("no certificate transparency log valid now")
	}
}
