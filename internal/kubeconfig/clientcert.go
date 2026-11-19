package kubeconfig

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"strconv"
	"time"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// A context's client certificate and how long it is valid (D-75, the owner's F22): the cluster list
// shows the time left or "expired" for a context whose user logs in with a client certificate, and
// never blocks or warns against connecting because of it. Only the certificate is read, and of it
// only its two dates are kept: never its PEM, never its subject, and never the private key
// (client-key-data, client-key), which nothing here looks at.

// CertValidity is when a client certificate is valid: from NotBefore through NotAfter, as the
// certificate says (UTC). State is set where a list is made for a reader (the engine's
// cluster.contexts: StateAt at that moment); a reader that keeps the list computes it again.
type CertValidity struct {
	NotBefore time.Time `json:"notBefore"`
	NotAfter  time.Time `json:"notAfter"`
	// State is CertValid, CertExpiring, CertExpired or CertNotYetValid at the time of the listing.
	State string `json:"state,omitempty"`
}

// The states of a client certificate (CertValidity.StateAt).
const (
	CertValid       = "valid"
	CertExpiring    = "expiring"
	CertExpired     = "expired"
	CertNotYetValid = "notYetValid"
)

// CertExpiringWindow is how close to its end a certificate counts as expiring: 14 days, or a fifth
// of its lifetime when that is shorter, so that a certificate issued for hours (a login that hands
// out short-lived certificates) is not marked for most of its life (D-75).
const CertExpiringWindow = 14 * 24 * time.Hour

// Fixed texts for a client certificate that cannot be read: they never quote the data.
var (
	errNoCertificate  = errors.New("no PEM CERTIFICATE block")
	errBadCertificate = errors.New("not an X.509 certificate")
)

// ParseCertValidity returns the validity of the first certificate in PEM data: the leaf, the one
// a TLS client presents (crypto/tls's X509KeyPair takes the first CERTIFICATE block, as client-go
// does). Blocks of other types before it are skipped. Its errors are fixed texts.
func ParseCertValidity(data []byte) (CertValidity, error) {
	rest := data
	for {
		block, r := pem.Decode(rest)
		if block == nil {
			return CertValidity{}, errNoCertificate
		}
		rest = r
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return CertValidity{}, errBadCertificate
		}
		return CertValidity{NotBefore: cert.NotBefore.UTC(), NotAfter: cert.NotAfter.UTC()}, nil
	}
}

// StateAt is the certificate's state at now: expired after NotAfter (crypto/x509's own check),
// not yet valid before NotBefore, expiring within its window (expiringWindow), valid otherwise.
func (v CertValidity) StateAt(now time.Time) string {
	switch {
	case now.After(v.NotAfter):
		return CertExpired
	case now.Before(v.NotBefore):
		return CertNotYetValid
	case v.NotAfter.Sub(now) <= v.expiringWindow():
		return CertExpiring
	}
	return CertValid
}

// expiringWindow is CertExpiringWindow, or a fifth of the certificate's lifetime when shorter.
func (v CertValidity) expiringWindow() time.Duration {
	w := CertExpiringWindow
	if life := v.NotAfter.Sub(v.NotBefore); life > 0 && life/5 < w {
		w = life / 5
	}
	return w
}

// Describe says at now how long the certificate is valid, after its subject: "expires in 23 days",
// "expired 3 days ago", "expired just now", "not valid yet" (the cluster lists, cogpit-cli).
func (v CertValidity) Describe(now time.Time) string {
	switch v.StateAt(now) {
	case CertExpired:
		ago := now.Sub(v.NotAfter)
		if ago < time.Minute {
			return "expired just now"
		}
		return "expired " + Span(ago) + " ago"
	case CertNotYetValid:
		return "not valid yet"
	}
	return "expires in " + Span(v.NotAfter.Sub(now))
}

// Span is a duration in its largest unit, in words: "3 years" from two years on, "23 days" from two
// days on, then "5 h", "12 min" and "less than a minute" (kubectl's steps for days and years).
func Span(d time.Duration) string {
	const day = 24 * time.Hour
	switch {
	case d >= 730*day:
		return strconv.Itoa(int(d/(365*day))) + " years"
	case d >= 2*day:
		return strconv.Itoa(int(d/day)) + " days"
	case d >= time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + " h"
	case d >= time.Minute:
		return strconv.Itoa(int(d/time.Minute)) + " min"
	}
	return "less than a minute"
}

// At returns a copy of v with State set for now.
func (v CertValidity) At(now time.Time) *CertValidity {
	v.State = v.StateAt(now)
	return &v
}

// clientCertOf fills a context's client certificate from its user entry: the validity of
// client-certificate-data, or the path of a client-certificate file for package cluster to read.
// Nothing for a user that logs in through an exec credential plugin or an auth provider (the
// credential they hand out, a certificate included, is not known before they run), and nothing of
// the private key.
func clientCertOf(c *Context, ai *clientcmdapi.AuthInfo) {
	if ai == nil || ai.Exec != nil || ai.AuthProvider != nil {
		return
	}
	switch {
	case len(ai.ClientCertificateData) > 0:
		v, err := ParseCertValidity(ai.ClientCertificateData)
		if err != nil {
			c.ClientCertProblem = "client-certificate-data: " + err.Error()
			return
		}
		c.ClientCert = &v
	case ai.ClientCertificate != "":
		c.ClientCertFile = ai.ClientCertificate
	}
}
