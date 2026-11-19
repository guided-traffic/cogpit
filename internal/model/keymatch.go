package model

import (
	"crypto"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strings"
)

// Whether a Secret's private key belongs to its certificate (D-30, the owner's answer): the private
// key is parsed in memory, its public half compared with the first certificate's public key, and
// nothing else of it is kept. The parsed key lives in one function call and is never returned,
// stored or cached; the outcome is yes, no, or a fixed reason; no error of the parsers leaves this
// file, so nothing can quote the key. Pure: no I/O.

// MaxPrivateKeyBytes bounds a private key value Cogpit reads (Cogpit's default): a PEM RSA key of
// 16384 bits is about 12 KiB.
const MaxPrivateKeyBytes = 64 << 10

// Why a private key could not be compared.
const (
	KeyEncrypted   = "encrypted"            // PKCS #8 ENCRYPTED PRIVATE KEY, or a PEM block with Proc-Type ENCRYPTED
	KeyUnsupported = "unsupported key type" // an OpenSSH key, or a PKCS #8 algorithm Go does not know
	KeyNotReadable = "not readable"         // no private key Go can parse
	KeyTooLarge    = "too large"            // over MaxPrivateKeyBytes
)

// KeyMatch is whether a private key belongs to a certificate. For agents it is true, false, or
// the reason the key could not be compared ("encrypted", "unsupported key type", "not readable",
// "too large").
type KeyMatch struct {
	Matches bool
	Reason  string // set when the key could not be compared; Matches is then false
}

// MarshalJSON writes true, false or the reason.
func (k KeyMatch) MarshalJSON() ([]byte, error) {
	if k.Reason != "" {
		return json.Marshal(k.Reason)
	}
	return json.Marshal(k.Matches)
}

// UnmarshalJSON reads true, false or a reason.
func (k *KeyMatch) UnmarshalJSON(b []byte) error {
	var matches bool
	if err := json.Unmarshal(b, &matches); err == nil {
		*k = KeyMatch{Matches: matches}
		return nil
	}
	var reason string
	if err := json.Unmarshal(b, &reason); err != nil {
		return errors.New("keyMatches is neither a boolean nor a reason")
	}
	*k = KeyMatch{Reason: reason}
	return nil
}

// MatchPrivateKey compares the private key in a value (PEM or DER: PKCS #1, PKCS #8, SEC 1 EC;
// RSA, ECDSA, Ed25519, X25519) with the first decoded certificate's public key. ok is false when
// there is no first certificate to compare with. The caller zeroes the value; the PEM blocks this
// decodes are zeroed here, an Ed25519 key too; an RSA or ECDSA key's numbers stay in memory until
// the collector reuses them (Go offers no way to wipe them).
func (d DecodedValue) MatchPrivateKey(value []byte) (m KeyMatch, ok bool) {
	if !d.HasLeaf() {
		return KeyMatch{}, false
	}
	pub, reason := privatePublicKey(value)
	if reason != "" {
		return KeyMatch{Reason: reason}, true
	}
	eq, canCompare := pub.(interface{ Equal(crypto.PublicKey) bool })
	return KeyMatch{Matches: canCompare && eq.Equal(d.parsed[0].PublicKey)}, true
}

// HasLeaf reports whether the value's first certificate was decoded: the one a private key is
// compared with.
func (d DecodedValue) HasLeaf() bool { return len(d.parsed) > 0 && d.parsed[0] != nil }

// HoldsPrivateKeyBlock reports whether a PEM value holds a block whose type ends in PRIVATE KEY:
// a combined PEM, certificate and key in one value (A99). It reads the blocks' types only and
// zeroes every block it decoded.
func HoldsPrivateKeyBlock(value []byte) bool {
	if len(value) > MaxPrivateKeyBytes {
		return false
	}
	found := false
	for rest := value; ; {
		var b *pem.Block
		if b, rest = pem.Decode(rest); b == nil {
			break
		}
		found = found || strings.HasSuffix(b.Type, "PRIVATE KEY")
		wipe(b.Bytes)
	}
	return found
}

// privatePublicKey parses the private key of a value and returns its public half, or why it could
// not. In PEM, the first block whose type ends in PRIVATE KEY is the key; other blocks (EC
// PARAMETERS, a certificate) are passed over.
func privatePublicKey(value []byte) (crypto.PublicKey, string) {
	if len(value) > MaxPrivateKeyBytes {
		return nil, KeyTooLarge
	}
	rest, sawPEM := value, false
	for {
		var b *pem.Block
		if b, rest = pem.Decode(rest); b == nil {
			break
		}
		sawPEM = true
		pub, reason, found := pemPrivateKey(b)
		wipe(b.Bytes)
		if found {
			return pub, reason
		}
	}
	if sawPEM {
		return nil, KeyNotReadable
	}
	return derPrivateKey(value)
}

// pemPrivateKey reads one PEM block; found is false for a block that is no private key.
func pemPrivateKey(b *pem.Block) (pub crypto.PublicKey, reason string, found bool) {
	switch {
	case b.Type == "ENCRYPTED PRIVATE KEY", strings.Contains(b.Headers["Proc-Type"], "ENCRYPTED"):
		return nil, KeyEncrypted, true
	case b.Type == "OPENSSH PRIVATE KEY":
		return nil, KeyUnsupported, true
	case strings.HasSuffix(b.Type, "PRIVATE KEY"):
		pub, reason := derPrivateKey(b.Bytes)
		return pub, reason, true
	}
	return nil, "", false
}

// derPrivateKey parses a DER private key as PKCS #8, PKCS #1 or SEC 1 and returns its public
// half. The parsers' errors stay here.
func derPrivateKey(der []byte) (crypto.PublicKey, string) {
	k8, err := x509.ParsePKCS8PrivateKey(der)
	if err == nil {
		return publicHalf(k8)
	}
	unknown := strings.Contains(err.Error(), "unknown") || strings.Contains(err.Error(), "unsupported")
	if k1, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return k1.Public(), ""
	}
	if kec, err := x509.ParseECPrivateKey(der); err == nil {
		return kec.Public(), ""
	}
	if unknown {
		return nil, KeyUnsupported
	}
	return nil, KeyNotReadable
}

// publicHalf returns a parsed private key's public key; an Ed25519 key's bytes are zeroed after.
func publicHalf(k any) (crypto.PublicKey, string) {
	switch key := k.(type) {
	case ed25519.PrivateKey:
		pub := ed25519.PublicKey(append([]byte(nil), key.Public().(ed25519.PublicKey)...))
		wipe(key)
		return pub, ""
	case *ecdh.PrivateKey:
		return key.PublicKey(), ""
	case crypto.Signer:
		return key.Public(), ""
	}
	return nil, KeyUnsupported
}

// wipe overwrites bytes that held key material.
func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
