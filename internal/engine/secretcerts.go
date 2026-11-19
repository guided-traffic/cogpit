package engine

import (
	"encoding/base64"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The certificates a Secret holds, decoded for the drawer and for agents (D-30, owner): every key
// whose value holds PEM CERTIFICATE blocks or DER certificates, a TLS Secret's tls.crt and ca.crt
// first, gets a group of sections in the drawer, one section per certificate in the value's order,
// each field its own item (so each has the drawer's copy icon); DetailModel.Certificates carries
// the same fields for agents with the times in UTC and the days left. A certificate is public
// material; the PEM text, the value it came from and every private key stay out: a key named like a
// private key (privateKeyName) is never searched for certificates, and the parser decodes nothing
// but certificates (model.DecodeCertificates). The one use of a private key is the check whether it
// belongs to its certificate (secretcerts_checks.go), which keeps nothing of it. Built from the
// cached object, no request; the decoded values are zeroed after parsing, as the Data section's
// are.

// Bounds (Cogpit's defaults, not the owner's): a value larger than certValueBytes is not decoded;
// at most model.MaxCertificatesPerKey certificates per key and certsPerSecret per Secret.
const (
	certValueBytes = 1 << 20
	certsPerSecret = 64
	certTitleBytes = 80 // a section title taken from a certificate's name is cut here
	certFoldNames  = 10 // a list of more names than this folds behind its count
)

// tlsSecretType is the type of a Secret that holds a TLS certificate and its key.
const tlsSecretType = "kubernetes.io/tls"

// The keys of a TLS Secret, by the Kubernetes documentation of the type.
const (
	tlsCertKey = "tls.crt"
	tlsKeyKey  = "tls.key"
	caCertKey  = "ca.crt"
)

// Texts of the drawer's certificate sections.
const (
	certGroupPrefix  = "Certificates · "
	certNone         = "no certificate in this value"
	certTooLarge     = "not decoded: the value is larger than 1.0 MiB" // certValueBytes, written as D-32 writes bytes
	certNotDecodedT  = "Not decoded"
	certMoreTitle    = "More"
	certSectionTitle = "Certificate"
)

// errCertPanic reports a panic while decoding a Secret's certificates; it names nothing of the
// Secret's values.
var errCertPanic = errors.New("decoding a Secret's certificates panicked")

// secretCertificates decodes the certificates in a Secret's values at now: per key that holds a
// PEM CERTIFICATE block or DER certificates, in the order tls.crt, ca.crt, then the other keys
// sorted; then the checks (certificateChecks: the paired private key, the chain's order). A TLS
// Secret's tls.crt that holds none says so; a value over 1 MiB is not decoded, which a key named
// like a certificate (*.crt, *.pem, *.cer, *.der) says. A panic in the decoder yields what was
// decoded before and errCertPanic.
func secretCertificates(obj *unstructured.Unstructured, now time.Time) (out []model.CertificateKey, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errCertPanic
		}
	}()
	data := model.NestedMap(obj.Object, "data")
	tlsType := model.NestedString(obj.Object, "type") == tlsSecretType
	budget := certsPerSecret
	decoded := map[string]model.DecodedValue{} // for the checks; dropped when this returns
	for _, k := range certKeyOrder(data) {
		if privateKeyName(k) {
			continue
		}
		mustHold := tlsType && k == tlsCertKey
		s, _ := data[k].(string)
		if base64.StdEncoding.DecodedLen(len(s)) > certValueBytes {
			if mustHold || certificateName(k) {
				out = append(out, model.CertificateKey{Key: k, NotDecoded: certTooLarge})
			}
			continue
		}
		raw, ok := decodeValue(data[k])
		var d model.DecodedValue
		if ok {
			d = model.DecodeCertificates(raw, min(budget, model.MaxCertificatesPerKey))
		}
		wipeBytes(raw)
		if d.Empty() {
			if mustHold {
				out = append(out, model.CertificateKey{Key: k, NotDecoded: certNone})
			}
			continue
		}
		ck := model.CertificateKey{Key: k, DER: d.DER, Certificates: d.Certificates, Omitted: d.Omitted}
		for i := range ck.Certificates {
			ck.Certificates[i].Evaluate(now)
		}
		budget -= len(ck.Certificates)
		decoded[k] = d
		out = append(out, ck)
	}
	certificateChecks(out, decoded, data)
	return out, nil
}

// certKeyOrder returns a Secret's keys with tls.crt and ca.crt first, then the others sorted.
func certKeyOrder(data map[string]any) []string {
	keys := make([]string, 0, len(data))
	for k := range data {
		if k != tlsCertKey && k != caCertKey {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	for _, k := range []string{caCertKey, tlsCertKey} {
		if _, ok := data[k]; ok {
			keys = append([]string{k}, keys...)
		}
	}
	return keys
}

// privateKeyName reports whether a key's name says it holds a private key (Cogpit's default): a
// TLS Secret's tls.key, any name ending in .key, and names ending in key.pem or key.der
// (server-key.pem, Let's Encrypt's privkey.pem). Such a value is never searched for certificates;
// it is read only to compare it with its certificate (secretcerts_checks.go).
func privateKeyName(k string) bool {
	l := strings.ToLower(k)
	return k == tlsKeyKey || strings.HasSuffix(l, ".key") || strings.HasSuffix(l, "key.pem") || strings.HasSuffix(l, "key.der")
}

// certificateExts are the endings of keys named like certificates.
var certificateExts = []string{".crt", ".pem", ".cer", ".der"}

// certificateName reports whether a key's name says it holds a certificate.
func certificateName(k string) bool {
	_, ok := certificateBase(k)
	return ok
}

// certificateBase returns a certificate key's name without its ending (tls.crt: tls).
func certificateBase(k string) (string, bool) {
	l := strings.ToLower(k)
	for _, ext := range certificateExts {
		if strings.HasSuffix(l, ext) {
			return k[:len(k)-len(ext)], true
		}
	}
	return "", false
}

// certificateSections are the drawer's sections of a Secret's certificates: per key a group
// "Certificates · <key>", one section per certificate titled by its subject's common name (numbered
// in a value with several), a "Not decoded" section for a value that was not, and a "More" section
// that counts the certificates after the bound. Times are written in now's zone (the Preferences'
// time zone), as Created is.
func certificateSections(keys []model.CertificateKey, now time.Time) []model.DetailSection {
	var out []model.DetailSection
	for _, k := range keys {
		group := certGroupPrefix + k.Key
		if k.NotDecoded != "" {
			out = append(out, model.DetailSection{Title: certNotDecodedT, Group: group, Items: []model.DetailItem{{Value: k.NotDecoded}}})
			continue
		}
		if sec, ok := certChecksSection(k, group); ok {
			out = append(out, sec)
		}
		total := len(k.Certificates) + k.Omitted
		for i, c := range k.Certificates {
			out = append(out, model.DetailSection{Title: certTitle(c, i, total), Group: group, Items: certItems(c, now)})
		}
		if k.Omitted > 0 {
			text := strconv.Itoa(k.Omitted) + " more certificates are not decoded"
			if k.Omitted == 1 {
				text = "1 more certificate is not decoded"
			}
			out = append(out, model.DetailSection{Title: certMoreTitle, Group: group, Items: []model.DetailItem{{Value: text}}})
		}
	}
	return out
}

// certTitle is a certificate's section title: its subject's common name, else its subject, cut at
// 80 bytes; numbered when the value holds several.
func certTitle(c model.Certificate, i, total int) string {
	name := c.SubjectCN
	if name == "" {
		name = c.Subject
	}
	if c.Invalid || name == "" {
		name = certSectionTitle
	}
	if len(name) > certTitleBytes {
		cut := certTitleBytes
		for cut > 0 && !utf8.RuneStart(name[cut]) {
			cut--
		}
		name = name[:cut] + "…"
	}
	if total > 1 {
		return strconv.Itoa(i+1) + " · " + name
	}
	return name
}

// certItems are one certificate's drawer items, each field its own, empty ones left out.
func certItems(c model.Certificate, now time.Time) []model.DetailItem {
	if c.Invalid {
		return []model.DetailItem{{Value: model.NotACertificate, Status: model.StatusError}}
	}
	var items []model.DetailItem
	add := func(name, value string) {
		if value != "" {
			items = append(items, model.DetailItem{Name: name, Value: value})
		}
	}
	names := func(name string, list []string) {
		if len(list) == 0 {
			return
		}
		it := model.DetailItem{Name: name, Value: strings.Join(list, "\n")}
		if len(list) > certFoldNames {
			it.Fold = name
		}
		items = append(items, it)
	}
	pair := func(name, cn, dn string) {
		if cn == "" {
			add(name, dn)
			return
		}
		add(name, cn)
		add(name+" DN", dn)
	}
	pair("Subject", c.SubjectCN, c.Subject)
	pair("Issuer", c.IssuerCN, c.Issuer)
	add("Serial Number", c.SerialNumber)
	notBefore := model.DetailItem{Name: "Not Before", Value: certTime(c.NotBefore, now)}
	if c.NotYetValid {
		notBefore.Value += " (not yet valid, in " + certSpan(c.NotBefore.Sub(now)) + ")"
		notBefore.Status = model.StatusError
	}
	left := "in " + certSpan(c.NotAfter.Sub(now))
	if c.Expired {
		left = "expired " + certSpan(now.Sub(c.NotAfter)) + " ago"
	}
	items = append(items, notBefore,
		model.DetailItem{Name: "Not After", Value: certTime(c.NotAfter, now) + " (" + left + ")", Status: c.Status})
	names("DNS Names", c.DNSNames)
	names("IP Addresses", c.IPAddresses)
	names("URIs", c.URIs)
	names("E-mail Addresses", c.EmailAddresses)
	if c.NamesOmitted > 0 {
		add("Names Not Shown", strconv.Itoa(c.NamesOmitted)+" more (at most 100 of each kind are shown)")
	}
	add("Public Key", c.PublicKey)
	add("Signature Algorithm", c.SignatureAlgorithm)
	ca := "No"
	if c.IsCA {
		ca = "Yes, no path length limit"
		if c.MaxPathLen != nil {
			ca = "Yes, path length " + strconv.Itoa(*c.MaxPathLen)
		}
	}
	add("CA", ca)
	add("Key Usage", strings.Join(c.KeyUsage, ", "))
	add("Extended Key Usage", strings.Join(c.ExtKeyUsage, ", "))
	add("Self-Signed", yesNo(c.SelfSigned))
	add("SHA-256 Fingerprint", c.SHA256Fingerprint)
	add("Subject Key ID", c.SubjectKeyID)
	add("Authority Key ID", c.AuthorityKeyID)
	return items
}

// certTime writes a certificate's time as a mark, which the drawer writes in the user's zone and
// format as it writes Created (D-31, BuildDetailIn).
func certTime(t, _ time.Time) string { return model.MarkTime(t) }

// certSpan writes a positive duration in whole days, else whole hours, else "less than an hour".
func certSpan(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return plural(int(d/(24*time.Hour)), "day")
	case d >= time.Hour:
		return plural(int(d/time.Hour), "hour")
	}
	return "less than an hour"
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

func yesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}
