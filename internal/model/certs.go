package model

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// The certificates a Secret holds, decoded (D-30, owner). A certificate is public material: its
// fields reach the drawer and agents, while the PEM text, the value it came from and any private
// key never do. Only PEM blocks of type CERTIFICATE are decoded; the parser hands pem.Decode
// nothing but such a block, so a PRIVATE KEY block next to it is never decoded, let alone parsed. A
// value without one is read as DER certificates when it is shaped like one (owner). The one place a
// private key is parsed is keymatch.go, to compare it with a certificate. Pure: no I/O.

// Bounds on what is decoded (Cogpit's defaults, not the owner's).
const (
	// MaxCertificatesPerKey is how many certificates of one value are decoded; the rest are counted.
	MaxCertificatesPerKey = 16
	// CertificateWarnBefore is how long before its end a certificate's validity turns to a warning.
	CertificateWarnBefore = 30 * 24 * time.Hour
	// maxCertText caps each text taken from a certificate (a name, a SAN), in bytes.
	maxCertText = 1024
	// maxCertNames caps each list of names (DNS names, IP addresses, URIs, e-mail addresses).
	maxCertNames = 100
)

// NotACertificate is what an invalid PEM CERTIFICATE block shows; the block is never quoted.
const NotACertificate = "not a valid certificate"

const (
	pemCertBegin = "-----BEGIN CERTIFICATE-----"
	pemCertEnd   = "-----END CERTIFICATE-----"
	pemAnyBegin  = "-----BEGIN "
)

// Certificate is what Cogpit shows of one X.509 certificate: its public fields as text, its times
// in UTC and, once Evaluate ran, its validity at a moment. It holds nothing of the PEM text or the
// DER bytes. The JSON shape is part of MCP's object_get result (DetailModel.Certificates); fields
// are only ever added, and every added field is optional. An invalid block marshals as
// {"invalid": true, "error": "not a valid certificate"}.
type Certificate struct {
	// Invalid is true for a PEM CERTIFICATE block that does not decode or parse; no other field is
	// set.
	Invalid bool `json:"invalid,omitempty"`
	// SubjectCN and IssuerCN are the common names; Subject and Issuer the whole distinguished names
	// in RFC 2253 form ("CN=web,O=Example,C=DE").
	SubjectCN string `json:"subjectCommonName,omitempty"`
	Subject   string `json:"subject"`
	IssuerCN  string `json:"issuerCommonName,omitempty"`
	Issuer    string `json:"issuer"`
	// SerialNumber is hex, its bytes separated by colons ("0A:1B:2C"); a negative one starts "-".
	SerialNumber string `json:"serialNumber"`
	// NotBefore and NotAfter are UTC (RFC 3339 in JSON).
	NotBefore time.Time `json:"notBefore"`
	NotAfter  time.Time `json:"notAfter"`
	// ExpiresInDays is the whole days left until NotAfter at the moment Evaluate was given, rounded
	// down: 0 on the last day, negative once expired.
	ExpiresInDays int  `json:"expiresInDays"`
	Expired       bool `json:"expired"`
	NotYetValid   bool `json:"notYetValid,omitempty"`
	// Status is ok while valid with more than 30 days left, warning with 30 days or fewer, error
	// when expired or not yet valid (Cogpit's thresholds).
	Status Status `json:"status,omitempty"`
	// The subject alternative names, at most 100 of each (NamesOmitted counts the rest).
	DNSNames       []string `json:"dnsNames,omitempty"`
	IPAddresses    []string `json:"ipAddresses,omitempty"`
	URIs           []string `json:"uris,omitempty"`
	EmailAddresses []string `json:"emailAddresses,omitempty"`
	NamesOmitted   int      `json:"namesOmitted,omitempty"`
	// PublicKey is the key's algorithm with its size or curve: "RSA 2048", "ECDSA P-256", "Ed25519".
	PublicKey          string `json:"publicKey"`
	SignatureAlgorithm string `json:"signatureAlgorithm"`
	// IsCA is the basic constraints' CA flag; MaxPathLen a CA's path length limit, absent without.
	IsCA       bool `json:"isCA"`
	MaxPathLen *int `json:"maxPathLen,omitempty"`
	// KeyUsage and ExtKeyUsage name the usages ("Digital Signature", "Server Authentication"); an
	// extended usage Cogpit has no name for is its object identifier.
	KeyUsage    []string `json:"keyUsage,omitempty"`
	ExtKeyUsage []string `json:"extKeyUsage,omitempty"`
	// SelfSigned is true when the issuer is the subject and the certificate's own key verifies its
	// signature (for a SHA-1 signature, which Go no longer verifies: when no other key is named).
	SelfSigned bool `json:"selfSigned"`
	// SHA256Fingerprint is the SHA-256 of the DER bytes, hex with colons.
	SHA256Fingerprint string `json:"sha256Fingerprint"`
	SubjectKeyID      string `json:"subjectKeyId,omitempty"`
	AuthorityKeyID    string `json:"authorityKeyId,omitempty"`
}

// MarshalJSON writes an invalid certificate as {"invalid": true, "error": ...}, without the zero
// values of the fields it does not have.
func (c Certificate) MarshalJSON() ([]byte, error) {
	if c.Invalid {
		return json.Marshal(struct {
			Invalid bool   `json:"invalid"`
			Error   string `json:"error"`
		}{true, NotACertificate})
	}
	type plain Certificate
	return json.Marshal(plain(c))
}

// CertificateKey is one key of a Secret whose value holds certificates, and what was decoded of it.
type CertificateKey struct {
	Key string `json:"key"`
	// DER is true for a value of binary (DER) certificates rather than PEM text.
	DER bool `json:"der,omitempty"`
	// Certificates are the value's certificates in their order: a chain's leaf first, then its
	// intermediates and CA, as the value lists them.
	Certificates []Certificate `json:"certificates,omitempty"`
	// Omitted counts the certificates after the first MaxCertificatesPerKey, which are not decoded.
	Omitted int `json:"omitted,omitempty"`
	// NotDecoded says why nothing of the value was decoded (too large, or no certificate where one
	// belongs); Certificates is then empty.
	NotDecoded string `json:"notDecoded,omitempty"`
	// PairedKey names the Secret's key whose private key was compared with the first certificate's
	// public key, and KeyMatches is the outcome: true, false, or why the key could not be read.
	// Neither is set when no key pairs with this one.
	PairedKey  string    `json:"pairedKey,omitempty"`
	KeyMatches *KeyMatch `json:"keyMatches,omitempty"`
	// ChainInOrder is whether each certificate is signed by the next one in the value (no trust
	// store, no revocation); absent when the chain was not checked. ChainBreak is then, when it is
	// false, the position (from 1) of the first certificate the next one did not sign.
	ChainInOrder *bool `json:"chainInOrder,omitempty"`
	ChainBreak   int   `json:"chainBreak,omitempty"`
	// Anchor is where the chain leads: a public CA, one this computer trusts, or an internal or
	// self-signed one (R1, certanchor.go); absent for a value without a valid first certificate.
	Anchor *CertAnchor `json:"anchor,omitempty"`
}

// HoldsCertificate reports whether a value holds a PEM CERTIFICATE block, without decoding it.
func HoldsCertificate(data []byte) bool {
	return bytes.Contains(data, []byte(pemCertBegin))
}

// DecodedValue is what was decoded of one value: its certificates as Cogpit shows them, the
// number not decoded after the bound, whether the value was DER, and the parsed certificates for
// the checks (ChainInOrder, MatchPrivateKey), which never leave the model. It holds no byte of the
// value: the parsed certificates own copies of their DER.
type DecodedValue struct {
	Certificates []Certificate
	Omitted      int
	DER          bool
	parsed       []*x509.Certificate // parsed[i] belongs to Certificates[i]; nil for an invalid entry
}

// Empty reports whether the value held no certificate at all.
func (d DecodedValue) Empty() bool { return len(d.Certificates) == 0 && d.Omitted == 0 }

// DecodeCertificates decodes the certificates of a value, in order, at most limit of them (0
// decodes none and only counts them), and counts the ones after those. A value with a PEM
// CERTIFICATE block is read as PEM: every other block (a private key, a certificate request) is
// skipped without being decoded, because pem.Decode only ever sees the text from a CERTIFICATE
// block's BEGIN line to its END line, and a block that holds another BEGIN line before its END is
// invalid; a block that does not decode or parse is an Invalid entry. Any other value is read as
// DER, one certificate after the other, when its first element is shaped like a certificate and
// parses as one (decodeDER); otherwise it holds none.
func DecodeCertificates(data []byte, limit int) DecodedValue {
	limit = min(max(limit, 0), MaxCertificatesPerKey)
	if HoldsCertificate(data) {
		return decodePEM(data, limit)
	}
	return decodeDER(data, limit)
}

// ParseCertificates is DecodeCertificates with a limit of 0 or less meaning
// MaxCertificatesPerKey: the certificates and the count not decoded.
func ParseCertificates(data []byte, limit int) (certs []Certificate, omitted int) {
	if limit <= 0 {
		limit = MaxCertificatesPerKey
	}
	d := DecodeCertificates(data, limit)
	return d.Certificates, d.Omitted
}

// add appends a parsed certificate, or an Invalid entry for nil.
func (d *DecodedValue) add(c *x509.Certificate) {
	if c == nil {
		d.Certificates = append(d.Certificates, Certificate{Invalid: true})
	} else {
		d.Certificates = append(d.Certificates, DescribeCertificate(c))
	}
	d.parsed = append(d.parsed, c)
}

func decodePEM(data []byte, limit int) DecodedValue {
	var d DecodedValue
	begin, end, other := []byte(pemCertBegin), []byte(pemCertEnd), []byte(pemAnyBegin)
	rest := data
	for {
		i := bytes.Index(rest, begin)
		if i < 0 {
			return d
		}
		rest = rest[i:]
		if len(d.Certificates) == limit {
			d.Omitted = bytes.Count(rest, begin)
			return d
		}
		body := rest[len(begin):]
		j := bytes.Index(body, end)
		if k := bytes.Index(body, other); j < 0 || (k >= 0 && k < j) {
			// No END line, or another block begins first: not a certificate; go on from that block.
			d.add(nil)
			if k < 0 {
				return d
			}
			rest = body[k:]
			continue
		}
		n := len(begin) + j + len(end)
		d.add(parseBlock(rest[:n]))
		rest = rest[n:]
	}
}

// parseBlock decodes and parses one PEM CERTIFICATE block (BEGIN line to END line); nil when it
// is no certificate. pem.Decode allocates the DER, so the certificate shares nothing with the
// value.
func parseBlock(block []byte) *x509.Certificate {
	b, _ := pem.Decode(block)
	if b == nil || b.Type != "CERTIFICATE" {
		return nil
	}
	c, err := x509.ParseCertificate(b.Bytes)
	if err != nil {
		return nil
	}
	return c
}

// decodeDER reads a value as DER certificates, one ASN.1 element after the other. The first
// element decides: when it is shaped like a certificate (certificateShaped) and parses as one,
// the value holds certificates, and each later element is a certificate or an Invalid entry
// (trailing bytes that are no element are one Invalid entry); otherwise the value holds none. A
// private key is never handed to the certificate parser: its outer SEQUENCE starts with an
// INTEGER.
func decodeDER(data []byte, limit int) DecodedValue {
	var d DecodedValue
	rest := data
	for len(rest) > 0 {
		first := d.Empty()
		elem, next, ok := derElement(rest)
		var c *x509.Certificate
		if ok && certificateShaped(elem) && (first || len(d.Certificates) < limit) {
			// A copy: the caller zeroes the value it decoded, and a parsed certificate keeps slices
			// of its input.
			if parsed, err := x509.ParseCertificate(bytes.Clone(elem)); err == nil {
				c = parsed
			}
		}
		switch {
		case first && c == nil:
			return DecodedValue{}
		case len(d.Certificates) < limit:
			d.add(c)
		default:
			d.Omitted++
		}
		if !ok {
			break
		}
		rest = next
	}
	d.DER = !d.Empty()
	return d
}

// derElement splits the first DER element (tag, length, content) off data.
func derElement(data []byte) (elem, rest []byte, ok bool) {
	var raw asn1.RawValue
	rest, err := asn1.Unmarshal(data, &raw)
	if err != nil {
		return nil, nil, false
	}
	return raw.FullBytes, rest, true
}

// certificateShaped reports whether a DER element is a SEQUENCE whose first element is a SEQUENCE,
// as a certificate (its tbsCertificate) is; a private key in PKCS #1, PKCS #8 or SEC 1 starts with
// an INTEGER.
func certificateShaped(elem []byte) bool {
	var outer, inner asn1.RawValue
	if _, err := asn1.Unmarshal(elem, &outer); err != nil || !isSequence(outer) {
		return false
	}
	if _, err := asn1.Unmarshal(outer.Bytes, &inner); err != nil {
		return false
	}
	return isSequence(inner)
}

func isSequence(v asn1.RawValue) bool {
	return v.Class == asn1.ClassUniversal && v.Tag == asn1.TagSequence && v.IsCompound
}

// ChainInOrder reports whether each decoded certificate is signed by the next one
// (x509.CheckSignatureFrom: the next must be a CA whose key verifies the signature; no trust
// store, no revocation) and, when not, the position (from 1) of the first one the next did not
// sign. checked is false for fewer than two certificates, a value with an invalid entry, and a
// signature algorithm Go no longer verifies (SHA-1).
func (d DecodedValue) ChainInOrder() (inOrder, checked bool, broken int) {
	if len(d.parsed) < 2 {
		return false, false, 0
	}
	for _, c := range d.parsed {
		if c == nil {
			return false, false, 0
		}
	}
	for i := 0; i+1 < len(d.parsed); i++ {
		err := d.parsed[i].CheckSignatureFrom(d.parsed[i+1])
		var insecure x509.InsecureAlgorithmError
		switch {
		case err == nil:
		case errors.As(err, &insecure):
			return false, false, 0
		default:
			return false, true, i + 1
		}
	}
	return true, true, 0
}

// DescribeCertificate returns what Cogpit shows of a parsed certificate (not yet evaluated).
func DescribeCertificate(c *x509.Certificate) Certificate {
	fp := sha256.Sum256(c.Raw)
	out := Certificate{
		SubjectCN:          certText(c.Subject.CommonName),
		Subject:            certText(distinguishedName(c.RawSubject, c.Subject)),
		IssuerCN:           certText(c.Issuer.CommonName),
		Issuer:             certText(distinguishedName(c.RawIssuer, c.Issuer)),
		SerialNumber:       serialHex(c),
		NotBefore:          c.NotBefore.UTC(),
		NotAfter:           c.NotAfter.UTC(),
		PublicKey:          publicKeyText(c),
		SignatureAlgorithm: c.SignatureAlgorithm.String(),
		IsCA:               c.BasicConstraintsValid && c.IsCA,
		KeyUsage:           keyUsageNames(c.KeyUsage),
		ExtKeyUsage:        extKeyUsageNames(c.ExtKeyUsage, c.UnknownExtKeyUsage),
		SelfSigned:         selfSigned(c),
		SHA256Fingerprint:  hexColons(fp[:]),
		SubjectKeyID:       hexColons(c.SubjectKeyId),
		AuthorityKeyID:     hexColons(c.AuthorityKeyId),
	}
	if out.IsCA && (c.MaxPathLen > 0 || (c.MaxPathLen == 0 && c.MaxPathLenZero)) {
		n := c.MaxPathLen
		out.MaxPathLen = &n
	}
	var dropped int
	names := func(in []string) []string {
		if len(in) == 0 {
			return nil
		}
		if len(in) > maxCertNames {
			dropped += len(in) - maxCertNames
			in = in[:maxCertNames]
		}
		out := make([]string, len(in))
		for i, s := range in {
			out[i] = certText(s)
		}
		return out
	}
	out.DNSNames = names(c.DNSNames)
	ips := make([]string, len(c.IPAddresses))
	for i, ip := range c.IPAddresses {
		ips[i] = ip.String()
	}
	out.IPAddresses = names(ips)
	uris := make([]string, len(c.URIs))
	for i, u := range c.URIs {
		uris[i] = u.String()
	}
	out.URIs = names(uris)
	out.EmailAddresses = names(c.EmailAddresses)
	out.NamesOmitted = dropped
	return out
}

// Evaluate sets the certificate's validity at now: the days left, expired, not yet valid and the
// status (Cogpit's thresholds: a warning at 30 days or fewer left, an error when expired or not yet
// valid). An invalid entry stays as it is.
func (c *Certificate) Evaluate(now time.Time) {
	if c.Invalid {
		return
	}
	left := c.NotAfter.Sub(now)
	c.ExpiresInDays = int(math.Floor(left.Hours() / 24))
	c.Expired = now.After(c.NotAfter)
	c.NotYetValid = now.Before(c.NotBefore)
	switch {
	case c.Expired || c.NotYetValid:
		c.Status = StatusError
	case left <= CertificateWarnBefore:
		c.Status = StatusWarning
	default:
		c.Status = StatusOK
	}
}

// distinguishedName renders a name in RFC 2253 form from its raw sequence, which keeps the order
// and the attributes the certificate has; the parsed name when the raw one does not parse.
func distinguishedName(raw []byte, parsed pkix.Name) string {
	var seq pkix.RDNSequence
	if rest, err := asn1.Unmarshal(raw, &seq); err == nil && len(rest) == 0 {
		return seq.String()
	}
	return parsed.String()
}

// serialHex renders a serial number as hex bytes separated by colons.
func serialHex(c *x509.Certificate) string {
	if c.SerialNumber == nil {
		return ""
	}
	b := c.SerialNumber.Bytes()
	if len(b) == 0 {
		b = []byte{0}
	}
	if c.SerialNumber.Sign() < 0 {
		return "-" + hexColons(b)
	}
	return hexColons(b)
}

// hexColons renders bytes as upper-case hex pairs separated by colons; "" for none.
func hexColons(b []byte) string {
	const digits = "0123456789ABCDEF"
	out := make([]byte, 0, 3*len(b))
	for i, x := range b {
		if i > 0 {
			out = append(out, ':')
		}
		out = append(out, digits[x>>4], digits[x&0x0f])
	}
	return string(out)
}

// publicKeyText names the certificate's public key: algorithm and size or curve.
func publicKeyText(c *x509.Certificate) string {
	switch k := c.PublicKey.(type) {
	case *rsa.PublicKey:
		if k.N != nil {
			return "RSA " + strconv.Itoa(k.N.BitLen())
		}
	case *ecdsa.PublicKey:
		if k.Curve != nil {
			return "ECDSA " + k.Curve.Params().Name
		}
	case ed25519.PublicKey:
		return "Ed25519"
	case *ecdh.PublicKey:
		if k.Curve() == ecdh.X25519() {
			return "X25519"
		}
	}
	return c.PublicKeyAlgorithm.String()
}

// selfSigned reports whether a certificate names itself as its issuer and its own key verifies its
// signature. Go refuses SHA-1 signatures; such a certificate counts as self-signed when it names
// no other authority key.
func selfSigned(c *x509.Certificate) bool {
	if !bytes.Equal(c.RawSubject, c.RawIssuer) {
		return false
	}
	err := c.CheckSignature(c.SignatureAlgorithm, c.RawTBSCertificate, c.Signature)
	if err == nil {
		return true
	}
	var insecure x509.InsecureAlgorithmError
	return errors.As(err, &insecure) && (len(c.AuthorityKeyId) == 0 || bytes.Equal(c.AuthorityKeyId, c.SubjectKeyId))
}

// keyUsages are the key usage bits by their names in RFC 5280's order.
var keyUsages = []struct {
	bit  x509.KeyUsage
	name string
}{
	{x509.KeyUsageDigitalSignature, "Digital Signature"},
	{x509.KeyUsageContentCommitment, "Content Commitment"},
	{x509.KeyUsageKeyEncipherment, "Key Encipherment"},
	{x509.KeyUsageDataEncipherment, "Data Encipherment"},
	{x509.KeyUsageKeyAgreement, "Key Agreement"},
	{x509.KeyUsageCertSign, "Certificate Sign"},
	{x509.KeyUsageCRLSign, "CRL Sign"},
	{x509.KeyUsageEncipherOnly, "Encipher Only"},
	{x509.KeyUsageDecipherOnly, "Decipher Only"},
}

func keyUsageNames(u x509.KeyUsage) []string {
	var out []string
	for _, k := range keyUsages {
		if u&k.bit != 0 {
			out = append(out, k.name)
		}
	}
	return out
}

// extKeyUsages names the extended key usages crypto/x509 knows.
var extKeyUsages = map[x509.ExtKeyUsage]string{
	x509.ExtKeyUsageAny:                            "Any",
	x509.ExtKeyUsageServerAuth:                     "Server Authentication",
	x509.ExtKeyUsageClientAuth:                     "Client Authentication",
	x509.ExtKeyUsageCodeSigning:                    "Code Signing",
	x509.ExtKeyUsageEmailProtection:                "E-mail Protection",
	x509.ExtKeyUsageIPSECEndSystem:                 "IPsec End System",
	x509.ExtKeyUsageIPSECTunnel:                    "IPsec Tunnel",
	x509.ExtKeyUsageIPSECUser:                      "IPsec User",
	x509.ExtKeyUsageTimeStamping:                   "Time Stamping",
	x509.ExtKeyUsageOCSPSigning:                    "OCSP Signing",
	x509.ExtKeyUsageMicrosoftServerGatedCrypto:     "Microsoft Server Gated Crypto",
	x509.ExtKeyUsageNetscapeServerGatedCrypto:      "Netscape Server Gated Crypto",
	x509.ExtKeyUsageMicrosoftCommercialCodeSigning: "Microsoft Commercial Code Signing",
	x509.ExtKeyUsageMicrosoftKernelCodeSigning:     "Microsoft Kernel Code Signing",
}

func extKeyUsageNames(known []x509.ExtKeyUsage, unknown []asn1.ObjectIdentifier) []string {
	var out []string
	for _, u := range known {
		if name, ok := extKeyUsages[u]; ok {
			out = append(out, name)
		} else {
			out = append(out, "usage "+strconv.Itoa(int(u)))
		}
	}
	for _, oid := range unknown {
		out = append(out, oid.String())
	}
	return out
}

// certText makes a text taken from a certificate safe to show on one line: control characters
// and the bidirectional overrides become \x / \u escapes (a name with a line break would read as
// two names, an override reorders what follows), invalid UTF-8 becomes U+FFFD, and it is cut at
// 1 KiB with "…".
func certText(s string) string {
	clean := true
	for _, r := range s {
		if r == utf8.RuneError || unsafeRune(r) {
			clean = false
			break
		}
	}
	if !clean {
		var b strings.Builder
		for _, r := range strings.ToValidUTF8(s, "\uFFFD") {
			switch {
			case r < 0x80 && unsafeRune(r):
				b.WriteString(`\x`)
				b.WriteString(hex2(byte(r)))
			case unsafeRune(r):
				q := strconv.QuoteRuneToASCII(r)
				b.WriteString(q[1 : len(q)-1])
			default:
				b.WriteRune(r)
			}
		}
		s = b.String()
	}
	if len(s) > maxCertText {
		cut := maxCertText
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut] + "…"
	}
	return s
}

// unsafeRune reports whether a rune must not reach a drawer line as it is.
func unsafeRune(r rune) bool {
	switch {
	case unicode.IsControl(r):
		return true
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069, r == 0x200e, r == 0x200f,
		r == 0x2028, r == 0x2029:
		return true
	}
	return false
}

func hex2(b byte) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[b>>4], digits[b&0x0f]})
}
