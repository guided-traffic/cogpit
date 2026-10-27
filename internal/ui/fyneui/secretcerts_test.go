package fyneui

import (
	"crypto/x509"
	"encoding/base64"
	"strings"
	"testing"
	"testing/cryptotest"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The drawer of a TLS Secret with its certificates decoded (D-30): the Secrets fixture with
// storefront-tls holding a real chain (a leaf that expires in 20 days, an intermediate, a root) in
// tls.crt, the root in ca.crt and the leaf's key in tls.key. Keys and signatures come from a fixed
// seed, so the fingerprints and the PNGs are the same on every run. Every name is made up.

// certsSecretsFake is secretsFake with the drawer open on storefront-tls, whose certificates are
// real; it returns the PEM texts the drawer must never show.
func certsSecretsFake(t *testing.T) (*fakeEngine, model.ObjectRef, []string) {
	cryptotest.SetGlobalRandom(t, 30)
	root := testkit.NewCert(t, testkit.CertSpec{
		CommonName: "Storefront Root CA", Organization: []string{"Example Shop"}, Serial: 1, CA: true,
		NotBefore: fixedNow.AddDate(-2, 0, 0), NotAfter: fixedNow.AddDate(8, 0, 0), Key: testkit.RSAKey(t, 2048),
	})
	inter := testkit.NewCert(t, testkit.CertSpec{
		CommonName: "Storefront Issuing CA", Organization: []string{"Example Shop"}, Serial: 0x1001, CA: true, PathLenZero: true,
		NotBefore: fixedNow.AddDate(-1, 0, 0), NotAfter: fixedNow.AddDate(2, 0, 0), Key: testkit.ECDSAKey(t), Parent: root,
	})
	leaf := testkit.NewCert(t, testkit.CertSpec{
		CommonName: "shop.example.com", Organization: []string{"Example Shop"}, Serial: 0x4f2a9c11e0,
		NotBefore: fixedNow.AddDate(0, 0, -70), NotAfter: fixedNow.AddDate(0, 0, 20),
		DNSNames:    []string{"shop.example.com", "www.shop.example.com"},
		IPAddresses: []string{"203.0.113.10"},
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		Key:         testkit.ECDSAKey(t), Parent: inter,
	})
	chain, ca, key := string(testkit.Chain(leaf, inter, root)), string(root.PEM()), string(leaf.KeyPEM(t))
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
