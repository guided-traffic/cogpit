package agent

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A connection's CA file (engine.ChatConnection.CAFile, the owner's decision): the CA certificates
// of an https server with a private CA. For that connection they replace the system's roots, as a
// kubeconfig's certificate-authority does for kubectl: only the file's CAs vouch for the endpoint,
// a public CA cannot (N141, the owner's decision in the audit; until then the file was added to the
// system's roots). A server that moves to a public certificate fails until the field is cleared,
// with an error that says the connection trusts only its CA file (transportError).
//
// How the roots are built, checked in Go 1.27.2's crypto/x509 (cert_pool.go, verify.go
// Certificate.Verify): a pool from x509.NewCertPool is not the system's, so Verify never hands it
// to the platform verifier (macOS's Security framework, its trust store and the user's trust
// settings) and checks the chain with Go's own verifier against the file's certificates alone.
// x509.SystemCertPool with the file appended would keep the system's mark, and Verify would ask
// the platform verifier first and Go only when that failed: every public CA trusted beside the
// file, which is what the owner chose against. Since Go 1.27, SSL_CERT_FILE or SSL_CERT_DIR in the
// environment change the system's roots only, so they do not reach a connection with a CA file.
//
// The file is read whenever a request is sent (a turn, a connection test, the models list), so a
// changed file counts from the next; the client is kept per connection and file content, so the
// connections it opened are reused. The file may hold anything else besides certificates (only
// CERTIFICATE blocks are taken); its content is never logged or quoted in an error.

// maxCAFile bounds a CA file: a bundle of a few hundred certificates.
const maxCAFile = 1 << 20

// loadCAPool reads a CA file and returns a pool of its certificates alone, the roots of its
// connection in place of the system's, and a digest of the file. Errors name the file and why,
// never its content.
func loadCAPool(path string) (*x509.CertPool, [sha256.Size]byte, error) {
	var sum [sha256.Size]byte
	data, err := readCAFile(path)
	if err != nil {
		return nil, sum, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, sum, fmt.Errorf("the connection's CA file %s holds no PEM certificate (-----BEGIN CERTIFICATE-----)", path)
	}
	return pool, sha256.Sum256(data), nil
}

// readCAFile reads a CA file: a regular file (a symlink to one is fine) of at most maxCAFile.
func readCAFile(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("the connection's CA file %s is not an absolute path", path)
	}
	f, err := os.Open(path) // the file the user named for this connection, in the window only
	if err != nil {
		return nil, caFileError(path, err)
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return nil, caFileError(path, err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("the connection's CA file %s is not a file", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxCAFile+1))
	if err != nil {
		return nil, caFileError(path, err)
	}
	if len(data) > maxCAFile {
		return nil, fmt.Errorf("the connection's CA file %s is larger than 1 MiB", path)
	}
	return data, nil
}

// caFileError names why a CA file could not be read, without repeating the path in Go's words.
func caFileError(path string, err error) error {
	var pe *fs.PathError
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("the connection's CA file %s does not exist", path)
	case errors.Is(err, fs.ErrPermission):
		return fmt.Errorf("the connection's CA file %s may not be read (permission denied)", path)
	case errors.As(err, &pe):
		return fmt.Errorf("the connection's CA file %s could not be read: %v", path, pe.Err)
	}
	return fmt.Errorf("the connection's CA file %s could not be read: %v", path, err)
}

// caClient is a connection's HTTP client with its CA file's certificates, and the file's digest it
// was built from.
type caClient struct {
	sum [sha256.Size]byte
	hc  *http.Client
}

// httpClientFor returns the client a connection's requests go through: the agent's own, or, for a
// connection with a CA file, one whose TLS trusts the file's certificates only, not the system's
// roots. It reads the file on every call.
func (a *Agent) httpClientFor(c engine.ChatConnection) (*http.Client, error) {
	if c.CAFile == "" {
		return a.hc, nil
	}
	pool, sum, err := loadCAPool(c.CAFile)
	if err != nil {
		return nil, err
	}
	key := c.ID + "\x00" + c.CAFile
	a.mu.Lock()
	defer a.mu.Unlock()
	if old, ok := a.caClients[key]; ok {
		if bytes.Equal(old.sum[:], sum[:]) {
			return old.hc, nil
		}
		old.hc.CloseIdleConnections() // the file changed: its old trust goes with its connections
	}
	rt := a.hc.Transport
	if rt == nil {
		rt = http.DefaultTransport
	}
	tr, ok := rt.(*http.Transport)
	if !ok {
		return nil, errors.New("a connection's CA file needs Cogpit's own HTTP transport")
	}
	tr = tr.Clone()
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if tr.TLSClientConfig != nil {
		cfg = tr.TLSClientConfig.Clone()
		cfg.MinVersion = max(cfg.MinVersion, tls.VersionTLS12)
	}
	cfg.RootCAs = pool
	tr.TLSClientConfig = cfg
	hc := &http.Client{Transport: tr, CheckRedirect: a.hc.CheckRedirect, Timeout: a.hc.Timeout}
	a.caClients[key] = caClient{sum: sum, hc: hc}
	return hc, nil
}
