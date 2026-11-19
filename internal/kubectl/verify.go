package kubectl

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/x509"
	_ "embed" // sigstore's trusted root
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"sync"

	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	protorekor "github.com/sigstore/protobuf-specs/gen/pb-go/rekor/v1"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

// The Kubernetes release signs every binary with sigstore's keyless signing: cosign signs the
// SHA-256 digest of the file with a key it made for that one release, sigstore's certificate
// authority (Fulcio) certified that key for ten minutes for the identity Google's OIDC issuer
// vouched for, and cosign recorded the signature in sigstore's public transparency log (Rekor) as a
// hashedrekord entry: the digest, the signature and the certificate. dl.k8s.io publishes the
// signature as <file>.sig (base64 of an ASN.1 ECDSA signature) and the certificate as <file>.cert
// (base64 of a PEM certificate), and no sigstore bundle
// (https://kubernetes.io/docs/tasks/administer-cluster/verify-signed-artifacts/; no
// <file>.sigstore.json for kubectl 1.37.1). The log entry is asked for at Rekor (rekor.go), as
// cosign verify-blob does with --certificate and --signature.
//
// Verifier checks the three with sigstore-go (decision D-50) against the trusted root Cogpit embeds
// (sigstore_trusted_root.json, sigstore's public-good instance, refreshed by make sigstore-root):
// the entry records exactly this digest, signature and certificate; its inclusion proof leads to a
// checkpoint the log signed; its signed entry timestamp is the log's, and the time it gives lies
// inside the certificate's ten minutes; the certificate chains to Fulcio at that time, carries a
// signed certificate timestamp of a certificate transparency log the trusted root names, and names
// the release's identity and issuer; and the signature was made over the digest by the
// certificate's key.

// Signer is who must have signed: the identity Fulcio wrote into the certificate's subject
// alternative name, and the OIDC issuer that vouched for it.
type Signer struct {
	Identity string
	Issuer   string
}

// KubernetesRelease is the signer of Kubernetes' release binaries, as Kubernetes documents it and
// as kubectl 1.36.1's and 1.37.1's certificates name it (darwin/arm64).
var KubernetesRelease = Signer{
	Identity: "krel-staging@k8s-releng-prod.iam.gserviceaccount.com",
	Issuer:   "https://accounts.google.com",
}

var (
	// ErrSignature marks a download whose signature, signing certificate or transparency-log entry
	// does not verify.
	ErrSignature = errors.New("the signature does not verify")
	// ErrNotLogged marks a signature that sigstore's transparency log has no entry for. It is an
	// ErrSignature.
	ErrNotLogged = fmt.Errorf("%w: sigstore's transparency log has no entry for it", ErrSignature)
)

// Verifier checks a release file's signature, signing certificate and transparency-log entry.
type Verifier struct {
	Signer Signer
	v      *verify.Verifier
}

//go:embed sigstore_trusted_root.json
var sigstoreTrustedRoot []byte

var defaultVerifier = sync.OnceValue(func() *Verifier {
	tr, err := root.NewTrustedRootFromJSON(sigstoreTrustedRoot)
	if err != nil {
		panic("kubectl: the embedded sigstore trusted root does not parse: " + err.Error())
	}
	v, err := NewVerifier(tr, KubernetesRelease, 1)
	if err != nil {
		panic("kubectl: " + err.Error())
	}
	return v
})

// DefaultVerifier is the Kubernetes release's signer under sigstore's public-good instance, whose
// trusted root Cogpit embeds; the signing certificate must carry one signed certificate timestamp.
func DefaultVerifier() *Verifier { return defaultVerifier() }

// NewVerifier returns a Verifier for signer under the trusted material: its Fulcio certificate
// authorities, Rekor logs and certificate transparency logs. scts is how many signed certificate
// timestamps of the material's certificate transparency logs the signing certificate must carry:
// one for sigstore's public instance, none for a test instance without a CT log.
func NewVerifier(trusted root.TrustedMaterial, signer Signer, scts int) (*Verifier, error) {
	opts := []verify.VerifierOption{verify.WithTransparencyLog(1), verify.WithObserverTimestamps(1)}
	if scts > 0 {
		opts = append(opts, verify.WithSignedCertificateTimestamps(scts))
	}
	v, err := verify.NewVerifier(trusted, opts...)
	if err != nil {
		return nil, fmt.Errorf("the sigstore verifier: %w", err)
	}
	return &Verifier{Signer: signer, v: v}, nil
}

// Signed is a release file's signature as the release publishes it beside the file.
type Signed struct {
	Digest []byte            // the SHA-256 the release signed (.sha256)
	Sig    []byte            // the ASN.1 ECDSA signature (.sig, decoded)
	Cert   *x509.Certificate // the signing certificate (.cert, decoded)
}

// ParseSigned reads a .sig and a .cert file and checks, offline, that they fit digest: the
// signature was made over it by the certificate's key. Whether the certificate is the release's
// and the signature is in the log is Verify's. A file that does not parse or a signature that does
// not fit is an ErrSignature.
func ParseSigned(digest, sigFile, certFile []byte) (*Signed, error) {
	cert, err := parseCert(certFile)
	if err != nil {
		return nil, err
	}
	sig, err := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(sigFile)))
	if err != nil || len(sig) == 0 {
		return nil, fmt.Errorf("%w: the .sig file is not base64 of a signature", ErrSignature)
	}
	pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("%w: the certificate's key is %T, not ECDSA", ErrSignature, cert.PublicKey)
	}
	if !ecdsa.VerifyASN1(pub, digest, sig) {
		return nil, fmt.Errorf("%w: it was not made over the published SHA-256 by the certificate's key", ErrSignature)
	}
	return &Signed{Digest: digest, Sig: sig, Cert: cert}, nil
}

// parseCert decodes a .cert file: base64 of a PEM certificate, or the PEM itself.
func parseCert(data []byte) (*x509.Certificate, error) {
	data = bytes.TrimSpace(data)
	if !bytes.HasPrefix(data, []byte("-----BEGIN")) {
		dec, err := base64.StdEncoding.DecodeString(string(data))
		if err != nil {
			return nil, fmt.Errorf("%w: the certificate is neither PEM nor base64", ErrSignature)
		}
		data = dec
	}
	b, _ := pem.Decode(data)
	if b == nil || b.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("%w: no PEM certificate in the .cert file", ErrSignature)
	}
	c, err := x509.ParseCertificate(b.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: the certificate does not parse: %v", ErrSignature, err)
	}
	return c, nil
}

// certPEM is the certificate as the log's entry holds it.
func (s *Signed) certPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.Cert.Raw})
}

// Verify checks s and the log's entries for it with sigstore-go: see the comment at the top of
// this file. One verified entry suffices; an entry that does not verify fails the check. Any
// failure is an ErrSignature; no entries is ErrNotLogged.
func (v *Verifier) Verify(s *Signed, entries []LogEntry) error {
	if len(entries) == 0 {
		return ErrNotLogged
	}
	tles := make([]*protorekor.TransparencyLogEntry, 0, len(entries))
	for i := range entries {
		tle, err := entries[i].proto()
		if err != nil {
			return fmt.Errorf("%w: the transparency log's entry: %v", ErrSignature, err)
		}
		tles = append(tles, tle)
	}
	b, err := bundle.NewBundle(&protobundle.Bundle{
		MediaType: "application/vnd.dev.sigstore.bundle.v0.3+json",
		VerificationMaterial: &protobundle.VerificationMaterial{
			Content:     &protobundle.VerificationMaterial_Certificate{Certificate: &protocommon.X509Certificate{RawBytes: s.Cert.Raw}},
			TlogEntries: tles,
		},
		Content: &protobundle.Bundle_MessageSignature{MessageSignature: &protocommon.MessageSignature{
			MessageDigest: &protocommon.HashOutput{Algorithm: protocommon.HashAlgorithm_SHA2_256, Digest: s.Digest},
			Signature:     s.Sig,
		}},
	})
	if err != nil {
		return fmt.Errorf("%w: %s", ErrSignature, oneLine(err))
	}
	id, err := verify.NewShortCertificateIdentity(v.Signer.Issuer, "", v.Signer.Identity, "")
	if err != nil {
		return fmt.Errorf("%w: %s", ErrSignature, oneLine(err))
	}
	if _, err := v.v.Verify(b, verify.NewPolicy(verify.WithArtifactDigest("sha256", s.Digest), verify.WithCertificateIdentity(id))); err != nil {
		return fmt.Errorf("%w: %s", ErrSignature, oneLine(err))
	}
	return nil
}

// maxReason bounds the text of sigstore-go's error in a download's error (Cogpit's default).
const maxReason = 300

// oneLine is err's text on one line of at most maxReason bytes, for the terminal's line:
// sigstore-go's errors can print a proof's hashes over several lines.
func oneLine(err error) string {
	s := strings.Join(strings.Fields(err.Error()), " ")
	if len(s) > maxReason {
		s = strings.ToValidUTF8(s[:maxReason], "") + "…"
	}
	return s
}
