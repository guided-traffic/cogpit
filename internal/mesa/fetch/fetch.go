// Package fetch puts Mesa's software OpenGL for Windows in place (D-142): it downloads the release
// archive Cogpit pins, checks it against the pinned SHA-256 before it reads a byte of it as an
// archive, and unpacks the x64 opengl32.dll and libgallium_wgl.dll into a folder. Only
// internal/mesa's Windows file imports it, so its 7z decoder is linked into cogpit.exe alone (make
// check-windows-only).
//
// Layer L4, with internal/mesa. It talks to the archive's host only.
package fetch

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/bodgit/sevenzip"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Members are the archive's files Install puts in place, in the order it renames them into the
// folder: the driver before opengl32.dll, so that a folder never holds an opengl32.dll without the
// driver it loads.
var Members = []string{"x64/libgallium_wgl.dll", "x64/opengl32.dll"}

// MaxArchive is the largest archive Install downloads and MaxMember the largest file it unpacks
// (26.2.4: an archive of 67.0 MiB, a libgallium_wgl.dll of 59.0 MiB).
const (
	MaxArchive = 256 << 20
	MaxMember  = 256 << 20
)

// progressStep is how many downloaded bytes pass between two progress reports.
const progressStep = 1 << 20

// Source is the archive Install downloads and the SHA-256 it must have (lower-case hex).
type Source struct {
	URL, SHA256 string
}

// Step is how far Install got.
type Step int

const (
	// Downloading: Done of Total bytes received, Total -1 while the size is unknown.
	Downloading Step = iota
	// Unpacking: the archive matched its SHA-256; the two files are being unpacked.
	Unpacking
)

// Progress is Install's report of how far it got.
type Progress struct {
	Step        Step
	Done, Total int64
}

// String is the report for a person, in binary units (D-32).
func (p Progress) String() string {
	switch {
	case p.Step == Unpacking:
		return "Unpacking opengl32.dll and libgallium_wgl.dll"
	case p.Total < 0:
		return model.FormatBytes(p.Done) + " downloaded"
	default:
		return model.FormatBytes(p.Done) + " of " + model.FormatBytes(p.Total) + " downloaded"
	}
}

// Client is the HTTP client Install downloads with: the system's proxy variables (HTTPS_PROXY),
// TLS 1.2 or later, an answer's headers within 30 seconds, and redirects to https only (GitHub
// sends a release asset from another host).
func Client() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	t.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: t, CheckRedirect: httpsOnly}
}

// httpsOnly refuses a redirect to anything but https and a chain of more than ten.
func httpsOnly(req *http.Request, via []*http.Request) error {
	if req.URL.Scheme != "https" {
		return fmt.Errorf("redirected to %s, which is not https", req.URL)
	}
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	return nil
}

// Writable reports whether Install can put files into dir: it makes a temporary file there and
// removes it again.
func Writable(dir string) error {
	f, err := os.CreateTemp(dir, ".cogpit-write-test-*")
	if err != nil {
		return err
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		return err
	}
	return os.Remove(name)
}

// Install downloads src into a temporary file in dir, checks its SHA-256, unpacks Members into
// temporary files there and renames them into place under their base names; the archive goes
// again, and so does every temporary file when Install fails. progress, when not nil, gets a
// report every MiB of the download and one before unpacking. An error names the request and what
// came back.
func Install(ctx context.Context, client *http.Client, src Source, dir string, progress func(Progress)) error {
	if progress == nil {
		progress = func(Progress) {}
	}
	archive, err := os.CreateTemp(dir, ".mesa-*.7z")
	if err != nil {
		return fmt.Errorf("a temporary file in %s: %w", dir, err)
	}
	defer func() { _ = os.Remove(archive.Name()) }()
	defer func() { _ = archive.Close() }() // before the removal: Windows removes no open file
	size, err := download(ctx, client, src, archive, progress)
	if err != nil {
		return err
	}
	progress(Progress{Step: Unpacking})
	return unpack(archive, size, path.Base(src.URL), dir)
}

// download writes src's answer to w and checks it against src.SHA256; it returns the size.
func download(ctx context.Context, client *http.Client, src Source, w io.Writer, progress func(Progress)) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.URL, nil)
	if err != nil {
		return 0, fmt.Errorf("GET %s: %w", src.URL, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err // a *url.Error: "Get \"<url>\": <what failed>"
	}
	defer func() { _ = resp.Body.Close() }()
	what := "GET " + src.URL
	if final := resp.Request.URL.String(); final != src.URL {
		what += " (redirected to " + final + ")"
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("%s: %s", what, resp.Status)
	}
	if resp.ContentLength > MaxArchive {
		return 0, fmt.Errorf("%s: %d bytes, more than the %d Cogpit downloads", what, resp.ContentLength, MaxArchive)
	}
	h := sha256.New()
	pw := &progressWriter{total: resp.ContentLength, report: progress}
	pw.report(Progress{Step: Downloading, Total: pw.total})
	n, err := io.Copy(io.MultiWriter(w, h, pw), io.LimitReader(resp.Body, MaxArchive+1))
	if err != nil {
		return n, fmt.Errorf("%s: reading the answer after %d bytes: %w", what, n, err)
	}
	if n > MaxArchive {
		return n, fmt.Errorf("%s: more than the %d bytes Cogpit downloads", what, MaxArchive)
	}
	pw.report(Progress{Step: Downloading, Done: n, Total: pw.total})
	if got := hex.EncodeToString(h.Sum(nil)); got != src.SHA256 {
		return n, fmt.Errorf("%s: %d bytes with the SHA-256 %s, not the %s this Cogpit pins", what, n, got, src.SHA256)
	}
	return n, nil
}

// progressWriter reports the bytes written every progressStep.
type progressWriter struct {
	done, total, next int64
	report            func(Progress)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if p.done >= p.next {
		p.next = p.done + progressStep
		p.report(Progress{Step: Downloading, Done: p.done, Total: p.total})
	}
	return len(b), nil
}

// unpack puts Members from the checked archive (name, for errors) into dir: each into a temporary
// file in the archive's order, which reads a solid archive once, then all renamed in Members'
// order.
func unpack(archive io.ReaderAt, size int64, name, dir string) (err error) {
	r, err := sevenzip.NewReader(archive, size)
	if err != nil {
		return fmt.Errorf("%s: not a 7z archive Cogpit reads: %w", name, err)
	}
	temps := map[string]string{} // member → its temporary file
	defer func() {
		for _, t := range temps {
			_ = os.Remove(t) // gone already when renamed
		}
	}()
	for _, f := range r.File {
		if !wanted(f.Name) || temps[f.Name] != "" {
			continue
		}
		t, err := unpackOne(f, dir)
		if err != nil {
			return fmt.Errorf("%s: %s: %w", name, f.Name, err)
		}
		temps[f.Name] = t
	}
	for _, m := range Members {
		if temps[m] == "" {
			return fmt.Errorf("%s holds no %s", name, m)
		}
	}
	for _, m := range Members {
		to := filepath.Join(dir, path.Base(m))
		if err := os.Rename(temps[m], to); err != nil {
			return fmt.Errorf("putting %s in place: %w", to, err)
		}
	}
	return nil
}

func wanted(member string) bool {
	for _, m := range Members {
		if m == member {
			return true
		}
	}
	return false
}

// unpackOne writes f into a temporary file in dir and returns its path.
func unpackOne(f *sevenzip.File, dir string) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer func() { _ = rc.Close() }()
	t, err := os.CreateTemp(dir, "."+path.Base(f.Name)+".*")
	if err != nil {
		return "", err
	}
	n, err := io.Copy(t, io.LimitReader(rc, MaxMember+1))
	if cerr := t.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > MaxMember {
		err = fmt.Errorf("more than the %d bytes Cogpit unpacks", MaxMember)
	}
	if err != nil {
		_ = os.Remove(t.Name())
		return "", err
	}
	return t.Name(), nil
}
