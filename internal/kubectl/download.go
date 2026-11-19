package kubectl

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"

	"github.com/guided-traffic/cogpit/internal/model"
)

// DefaultMirror is where kubectl is downloaded from: the Kubernetes project's release host. A
// mirror has the same layout below its base URL: <base>/v<version>/bin/<os>/<arch>/kubectl, with
// kubectl.sha256, kubectl.sig and kubectl.cert beside the binary. The signature's transparency-log
// entry comes from Rekor (DefaultRekor) whatever the mirror.
const DefaultMirror = "https://dl.k8s.io/release"

// Bounds of a download (Cogpit's defaults): kubectl 1.36.1 for darwin/arm64 is about 57 MB, its
// .sha256 64 bytes, its .sig 96 and its .cert 1,440.
const (
	MaxBinarySize = 128 << 20
	maxSmallFile  = 16 << 10
	maxMirrorLen  = 2048
	maxRedirects  = 5
)

// ErrChecksum marks a download whose SHA-256 is not the one the release publishes.
var ErrChecksum = errors.New("the file's SHA-256 is not the published one")

// Downloader downloads kubectl releases and verifies them before it puts them in place.
type Downloader struct {
	// Mirror is the base URL; empty: DefaultMirror. CheckMirror says what it may be.
	Mirror string
	// Client makes the requests; nil: one that takes the proxy from the environment
	// (HTTPS_PROXY). Redirects are followed to https URLs only, whatever the client says.
	Client *http.Client
	// Proxy, when set, picks the proxy of each request in place of the client's transport's, as
	// http.Transport.Proxy does (the engine passes the Preferences' HTTP proxy for non-cluster
	// traffic, else the environment's).
	Proxy func(*http.Request) (*url.URL, error)
	// Verifier checks the signature and its transparency-log entry; nil: DefaultVerifier.
	Verifier *Verifier
	// Rekor is the transparency log's base URL, asked for the signature's entry through Client
	// and Proxy as the files are; empty: DefaultRekor. Not a preference: tests set it.
	Rekor string
	// GOOS and GOARCH name the release's platform; empty: this process's.
	GOOS, GOARCH string
	// MaxSize bounds the binary; 0: MaxBinarySize.
	MaxSize int64
}

// Progress reports a running download: the bytes received and the total (-1 while the server has
// not said). It is called from the downloading goroutine.
type Progress func(done, total int64)

// CheckMirror resolves a mirror preference to the base URL downloads use: DefaultMirror for "",
// else an https URL without user information, query or fragment, its trailing slash dropped.
func CheckMirror(mirror string) (string, error) {
	mirror = strings.TrimSpace(mirror)
	if mirror == "" {
		return DefaultMirror, nil
	}
	if len(mirror) > maxMirrorLen || strings.IndexFunc(mirror, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("the download mirror is one line of at most %d bytes", maxMirrorLen)
	}
	u, err := url.Parse(mirror)
	switch {
	case err != nil || u.Host == "":
		return "", errors.New("the download mirror is an https URL, such as " + DefaultMirror)
	case u.Scheme != "https":
		return "", fmt.Errorf("the download mirror must be an https URL, not %s", u.Scheme)
	case u.User != nil:
		return "", errors.New("the download mirror's URL must not hold a user or password")
	case u.RawQuery != "" || u.Fragment != "" || u.ForceQuery:
		return "", errors.New("the download mirror's URL has no query or fragment: give the base under which v<version>/bin/<os>/<arch>/kubectl lies")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// URL is where release v of kubectl for goos and goarch lies under the mirror.
func (d *Downloader) URL(v Version) (string, error) {
	base, err := CheckMirror(d.Mirror)
	if err != nil {
		return "", err
	}
	goos, goarch := d.platform()
	return base + "/" + v.Tag() + "/bin/" + goos + "/" + goarch + "/" + BinaryName(goos), nil
}

func (d *Downloader) platform() (string, string) {
	goos, goarch := d.GOOS, d.GOARCH
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	return goos, goarch
}

// Download fetches release v into <dir>/<version>/kubectl and returns that path. The published
// SHA-256 (.sha256), signature (.sig) and signing certificate (.cert) are read first; the
// signature must fit the published digest, sigstore's transparency log (Rekor) is asked for its
// entry of the three, and the Verifier checks them all before the binary is asked for (a log that
// cannot be asked is ErrLogUnreachable, and nothing is downloaded). The binary (at most MaxSize
// bytes) goes into a temporary file beside its place, must have that digest, is made executable
// (0755) and renamed into place, replacing an earlier copy. The directories are made owner-only
// (0700) where they do not exist. Nothing is left behind on failure.
func (d *Downloader) Download(ctx context.Context, v Version, dir string, progress Progress) (string, error) {
	u, err := d.URL(v)
	if err != nil {
		return "", err
	}
	if dir == "" || !filepath.IsAbs(dir) {
		return "", fmt.Errorf("the directory for binaries %q is not an absolute path", dir)
	}
	client := d.client()
	fail := func(err error) (string, error) { return "", fmt.Errorf("kubectl %s from %s: %w", v, u, err) }

	sum, err := fetchSmall(ctx, client, u+".sha256")
	if err != nil {
		return fail(err)
	}
	want, err := parseSHA256(sum)
	if err != nil {
		return fail(err)
	}
	sig, err := fetchSmall(ctx, client, u+".sig")
	if err != nil {
		return fail(err)
	}
	certFile, err := fetchSmall(ctx, client, u+".cert")
	if err != nil {
		return fail(err)
	}
	// The release signs the digest it publishes; the binary must then have that digest.
	signed, err := ParseSigned(want, sig, certFile)
	if err != nil {
		return fail(err)
	}
	rekor := d.Rekor
	if rekor == "" {
		rekor = DefaultRekor
	}
	entries, err := lookupRekor(ctx, client, rekor, signed)
	if err != nil {
		return fail(err)
	}
	ver := d.Verifier
	if ver == nil {
		ver = DefaultVerifier()
	}
	if err := ver.Verify(signed, entries); err != nil {
		return fail(err)
	}

	target := filepath.Join(dir, v.String())
	if err := os.MkdirAll(target, 0o700); err != nil {
		return fail(err)
	}
	name := BinaryName(d.goos())
	tmp, err := os.CreateTemp(target, "."+name+"-*.part")
	if err != nil {
		return fail(err)
	}
	keep := false
	defer func() {
		if !keep {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	got, err := d.fetchBinary(ctx, client, u, tmp, progress)
	if err != nil {
		return fail(err)
	}
	if !bytes.Equal(got, want) {
		return fail(fmt.Errorf("%w: got %x, the release publishes %x", ErrChecksum, got, want))
	}
	if err := tmp.Chmod(0o755); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		return fail(err)
	}
	dst := filepath.Join(target, name)
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return fail(err)
	}
	keep = true
	return dst, nil
}

func (d *Downloader) goos() string { goos, _ := d.platform(); return goos }

// fetchBinary streams the binary into f and returns its SHA-256.
func (d *Downloader) fetchBinary(ctx context.Context, client *http.Client, u string, f *os.File, progress Progress) ([]byte, error) {
	limit := d.MaxSize
	if limit <= 0 {
		limit = MaxBinarySize
	}
	resp, err := get(ctx, client, u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// A size beside its limit is not named: one decimal can make a size just over the limit read
	// like it (D-32).
	if resp.ContentLength > limit {
		return nil, fmt.Errorf("the binary is larger than the %s Cogpit accepts", model.FormatBytes(limit))
	}
	h := sha256.New()
	pr := &progressReader{r: io.LimitReader(resp.Body, limit+1), total: resp.ContentLength, report: progress}
	n, err := io.Copy(io.MultiWriter(f, h), pr)
	if err != nil {
		return nil, fmt.Errorf("downloading: %w", err)
	}
	if n > limit {
		return nil, fmt.Errorf("the binary is larger than the %s Cogpit accepts", model.FormatBytes(limit))
	}
	if cl := resp.ContentLength; cl >= 0 && n != cl {
		// The difference, not the two sizes, which one decimal can make read alike (D-32).
		if n < cl {
			return nil, fmt.Errorf("received %s less than the %s announced", model.FormatBytes(cl-n), model.FormatBytes(cl))
		}
		return nil, fmt.Errorf("received %s more than the %s announced", model.FormatBytes(n-cl), model.FormatBytes(cl))
	}
	pr.flush()
	return h.Sum(nil), nil
}

// client is d's client with redirects limited to https.
func (d *Downloader) client() *http.Client {
	var c http.Client
	if d.Client != nil {
		c = *d.Client
	} else {
		tr := http.DefaultTransport.(*http.Transport).Clone() // the proxy from the environment
		tr.ResponseHeaderTimeout = 30 * time.Second
		c.Transport = tr
	}
	if d.Proxy != nil {
		tr, ok := c.Transport.(*http.Transport)
		if !ok || tr == nil {
			tr = http.DefaultTransport.(*http.Transport)
		}
		tr = tr.Clone()
		tr.Proxy = d.Proxy
		c.Transport = tr
	}
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return errors.New("too many redirects")
		}
		if req.URL.Scheme != "https" {
			return fmt.Errorf("redirected to %s, which is not https", req.URL.Redacted())
		}
		return nil
	}
	return &c
}

func get(ctx context.Context, client *http.Client, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%s answered %s", path.Base(resp.Request.URL.Path), resp.Status)
	}
	return resp, nil
}

// fetchSmall reads one of the small files beside the binary, at most maxSmallFile bytes.
func fetchSmall(ctx context.Context, client *http.Client, u string) ([]byte, error) {
	resp, err := get(ctx, client, u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSmallFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSmallFile {
		return nil, fmt.Errorf("%s is larger than %s", path.Base(u), model.FormatBytes(maxSmallFile))
	}
	return data, nil
}

// parseSHA256 reads a .sha256 file: the hex digest, optionally followed by a file name.
func parseSHA256(data []byte) ([]byte, error) {
	f := strings.Fields(string(data))
	if len(f) == 0 {
		return nil, errors.New("the .sha256 file is empty")
	}
	sum, err := hex.DecodeString(f[0])
	if err != nil || len(sum) != sha256.Size {
		return nil, errors.New("the .sha256 file holds no SHA-256 digest")
	}
	return sum, nil
}

// progressReader reports the bytes read, at most once per MiB and at the end.
type progressReader struct {
	r          io.Reader
	done, sent int64
	total      int64
	report     Progress
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	if p.done-p.sent >= 1<<20 {
		p.flush()
	}
	return n, err
}

func (p *progressReader) flush() {
	if p.report != nil && p.done != p.sent {
		p.sent = p.done
		p.report(p.done, p.total)
	}
}
