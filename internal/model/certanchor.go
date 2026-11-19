package model

import (
	"crypto/x509"
	"strings"
	"sync"

	"golang.org/x/crypto/x509roots/fallback/bundle"
)

// The kind of certificate authority a Secret's certificate chain leads to (the owner's R1; the
// lead's design, a night decision for the owner to confirm): a public CA (a root of Mozilla's NSS
// root program, the bundle golang.org/x/crypto/x509roots ships, embedded in Cogpit and independent
// of the computer), one this computer trusts (its trust store: a corporate root installed on the
// Mac counts here, not as public), or an internal or self-signed one. The chain is judged as the
// value holds it: the first certificate, the value's others as intermediates; at a time inside the
// first certificate's validity, so that an expired certificate still names its anchor. Pure but for
// the system pool the caller passes.

// Anchor kinds (CertAnchor.Kind).
const (
	AnchorPublic   = "public"   // a root of the public root program
	AnchorSystem   = "system"   // a root this computer trusts, not a public one
	AnchorInternal = "internal" // neither: an internal CA or a self-signed certificate
)

// CertAnchor is where a chain leads: its kind and the root's name (its common name, else its
// subject).
type CertAnchor struct {
	Kind string `json:"kind"`
	Root string `json:"root,omitempty"`
}

var (
	publicRootsOnce sync.Once
	publicRoots     *x509.CertPool
)

// PublicRoots is the public root program's pool (NSS, through x509roots), built once.
func PublicRoots() *x509.CertPool {
	publicRootsOnce.Do(func() {
		pool := x509.NewCertPool()
		for r := range bundle.Roots() {
			c, err := x509.ParseCertificate(r.Certificate)
			if err != nil {
				continue
			}
			pool.AddCertWithConstraint(c, r.Constraint)
		}
		publicRoots = pool
	})
	return publicRoots
}

// Anchor classifies where the value's chain leads (CertAnchor); ok is false for a value without a
// valid first certificate. system is the computer's trust store; nil skips that step.
func (d DecodedValue) Anchor(system *x509.CertPool) (CertAnchor, bool) {
	if !d.HasLeaf() {
		return CertAnchor{}, false
	}
	leaf := d.parsed[0]
	inter := x509.NewCertPool()
	for _, c := range d.parsed[1:] {
		if c != nil {
			inter.AddCert(c)
		}
	}
	at := leaf.NotBefore.Add(leaf.NotAfter.Sub(leaf.NotBefore) / 2)
	verify := func(roots *x509.CertPool) (string, bool) {
		if roots == nil {
			return "", false
		}
		chains, err := leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: inter, CurrentTime: at,
			KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}})
		if err != nil || len(chains) == 0 || len(chains[0]) == 0 {
			return "", false
		}
		return certName(chains[0][len(chains[0])-1]), true
	}
	if root, ok := verify(PublicRoots()); ok {
		return CertAnchor{Kind: AnchorPublic, Root: root}, true
	}
	if root, ok := verify(system); ok {
		return CertAnchor{Kind: AnchorSystem, Root: root}, true
	}
	top := leaf
	for _, c := range d.parsed[1:] {
		if c != nil {
			top = c
		}
	}
	root := strings.TrimSpace(top.Issuer.CommonName)
	if root == "" {
		root = top.Issuer.String()
	}
	return CertAnchor{Kind: AnchorInternal, Root: root}, true
}

// certName is a certificate's common name, else its subject.
func certName(c *x509.Certificate) string {
	if n := strings.TrimSpace(c.Subject.CommonName); n != "" {
		return n
	}
	return c.Subject.String()
}
