package fyneui

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/base64"
	hexenc "encoding/hex"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// The drawer of a TLS Secret with its certificates decoded (D-30): the Secrets fixture with
// storefront-tls holding a real chain (a leaf that expires in 20 days, an intermediate, a root) in
// tls.crt, the root in ca.crt and the leaf's key in tls.key. The certificates are files of testdata
// (made once by testkit.NewCert under cryptotest.SetGlobalRandom's seed 30) and the leaf's key comes
// from its fixed scalar, so the fingerprints and the PNGs are the same on every run without the
// process's random source set: setting it back raced with an earlier test's HTTP/2 health check
// reading it. Every name is made up.

// storefrontKeyScalar is the leaf's P-256 private scalar (a test key of a made-up name).
const storefrontKeyScalar = "7aeb12af397013078cfba4aa96fef257daaeb3c11bd880f2b91aa4f98bf35d00"

// storefrontKeyPEM is the leaf's key as a PKCS #8 PRIVATE KEY block, as a TLS Secret's tls.key
// holds it.
func storefrontKeyPEM(t *testing.T) string {
	t.Helper()
	raw, err := hexenc.DecodeString(storefrontKeyScalar)
	if err != nil {
		t.Fatal(err)
	}
	k, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), raw)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

// readTestdata returns a file of testdata as text.
func readTestdata(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// certsSecretsFake is secretsFake with the drawer open on storefront-tls, whose certificates are
// real; it returns the PEM texts the drawer must never show.
func certsSecretsFake(t *testing.T) (*fakeEngine, model.ObjectRef, []string) {
	chain, ca, key := readTestdata(t, "certs/storefront-tls.crt"), readTestdata(t, "certs/storefront-ca.crt"), storefrontKeyPEM(t)
	enc := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

	f, _ := secretsFake(t)
	objs := fixtureSecretObjects()
	var tls *unstructured.Unstructured
	for _, o := range objs {
		if o.GetName() == "storefront-tls" {
			tls = o
		}
	}
	tls.Object["data"] = map[string]any{"tls.crt": enc(chain), "ca.crt": enc(ca), "tls.key": enc(key)}
	f.tables["secrets"] = kindSnapshot(t, "secrets", objs)
	p, _ := kinds.ByKey("secrets")
	ref := model.ObjectRef{Context: "demo", Kind: "secrets", Namespace: "storefront", Name: "storefront-tls"}
	m, err := engine.BuildDetail(p, ref, tls, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	f.details[ref.String()] = m
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	var never []string
	for _, v := range []string{chain, ca, key} {
		never = append(never, enc(v))
		for _, line := range strings.Split(v, "\n") {
			if len(line) >= 16 && !strings.HasPrefix(line, "-----") {
				never = append(never, line)
			}
		}
	}
	return f, ref, append(never, "PRIVATE KEY", "BEGIN CERTIFICATE")
}

// The drawer shows a group per certificate key, tls.crt then ca.crt, a section per certificate of
// the chain titled by its common name, each field a line of its own with the drawer's copy icon
// (the copy is the value as shown), the leaf's Not After as a warning pill; no PEM text or key.
func TestSecretDrawerShowsCertificates(t *testing.T) {
	f, ref, never := certsSecretsFake(t)
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && sh.drawer.last.Ref == ref })
	for _, s := range []string{"Certificates · tls.crt", "Certificates · ca.crt", "1 · shop.example.com", "2 · Storefront Issuing CA",
		"3 · Storefront Root CA", "Storefront Root CA", "CN=shop.example.com,O=Example Shop", "4F:2A:9C:11:E0", "www.shop.example.com",
		"203.0.113.10", "ECDSA P-256", "Yes, path length 0", "Server Authentication", "SHA-256 Fingerprint"} {
		if !drawerHasText(sh.drawer, s) {
			t.Errorf("the drawer shows no %q", s)
		}
	}
	var serial *selLine
	for _, l := range findAll[*selLine](sh.drawer.details) {
		if l.line.text == "4F:2A:9C:11:E0" {
			serial = l
		}
	}
	if serial == nil || serial.hook == nil || serial.hook.text != "4F:2A:9C:11:E0" {
		t.Errorf("the serial number has no copy icon that copies it: %+v", serial)
	}
	notAfter, keyMatches, chain := false, false, false
	for _, p := range findAll[*pill](sh.drawer.details) {
		ok := p.fg == statusPillInk(model.StatusOK) // a healthy status is a quiet chip
		switch {
		case strings.HasSuffix(p.text, "(in 20 days)") && p.fg == colorStatusWarning:
			notAfter = true
		case p.text == "Yes (tls.key)" && ok: // the owner's answer of 2026-10-13: tls.key checked
			keyMatches = true
		case p.text == "Yes, each certificate is signed by the next" && ok:
			chain = true
		}
	}
	if !notAfter || !keyMatches || !chain {
		t.Errorf("pills: the leaf's Not After a warning %v, the key matching %v, the chain in order %v", notAfter, keyMatches, chain)
	}
	for _, s := range never {
		if drawerHasText(sh.drawer, s) {
			t.Errorf("the drawer shows %q", s[:min(len(s), 40)])
		}
	}
}

// renderSecretCertificates renders the TLS Secret's drawer scrolled to its certificates.
func renderSecretCertificates(t *testing.T, mode ThemeMode, name string) {
	f, ref, _ := certsSecretsFake(t)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the drawer", func() bool {
		return len(sh.kinds) > 0 && sh.view.snap != nil && sh.drawer.last != nil && sh.drawer.last.Ref == ref && sh.diag.total > 0
	})
	settle(w)
	scrollDrawerTo(t, sh, "Certificates · tls.crt")
	settle(w)
	savePNG(t, w, name)
}

func TestRenderSecretCertificatesDark(t *testing.T) {
	renderSecretCertificates(t, ThemeDark, "secret_certs_dark.png")
}
