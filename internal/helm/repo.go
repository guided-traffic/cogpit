package helm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	repo "helm.sh/helm/v4/pkg/repo/v1"
	"sigs.k8s.io/yaml"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Chart repositories (G11, D-29, architecture §8): the repositories Helm's own repositories.yaml
// lists (repofile.go), their index (index.yaml) downloaded on an explicit update and, while the
// Charts view or a Helm Install tab is open, when its time-to-live has passed or the repository
// is new to Cogpit (engine/helmrepos.go), and the chart archives an install takes from them.
// Everything here is bounded and parsed with the Helm SDK; nothing writes into Helm's file or
// Helm's cache: the engine hands every path in, the indexes under Cogpit's own cache folder.
//
// A repository's credentials go only to its own scheme, host and port, on every hop of a
// redirect and for a chart URL its index points elsewhere, unless its entry says
// pass_credentials_all; an https repository's downloads never follow a URL or a redirect to plain
// http; a repository whose user and password would go over plain http downloads nothing over http
// until the user allowed it for that repository (ErrCredentialsOverHTTP, H-40); each repository
// with a CA file, a client certificate or insecure_skip_tls_verify gets a
// transport of its own, made for the download from the files as they are then, with the
// Preferences' proxy kept.

// Bounds on what Cogpit downloads from a repository (Cogpit's defaults, not Helm's: Helm reads an
// index whole and bounds only a chart's decompressed size, 100 MiB).
const (
	// MaxIndexBytes bounds a repository's index.yaml. Large public indexes are a few tens of MiB.
	MaxIndexBytes int64 = 32 << 20
	// MaxChartBytes bounds a chart archive (.tgz) as downloaded.
	MaxChartBytes int64 = 16 << 20
	// fetchTimeout bounds one download, connection and body.
	fetchTimeout = 2 * time.Minute
	// maxRedirects bounds the redirects one download follows.
	maxRedirects = 10
	// maxText bounds a chart's description and the other texts Cogpit keeps of an index entry.
	maxText = 512
	// maxListed bounds the keywords, maintainers and sources kept of an index entry.
	maxListed = 32
	// maxVersions bounds the versions kept per chart, newest first.
	maxVersions = 1000
)

// ErrNotHTTPS refuses a URL or a redirect that would take an https repository's download to plain
// http, or a URL that is neither http nor https.
var ErrNotHTTPS = errors.New("an https repository's downloads stay on https")

// ErrCredentialsOverHTTP refuses a download from a repository that would send its user and
// password over plain http before the user allowed that for it (credentials H-40, owner): nothing
// is requested, so neither the credentials nor a request without them leave Cogpit. The engine
// allows it per repository (Repository.AllowCredentialsOverHTTP). Match it with errors.Is.
var ErrCredentialsOverHTTP = errors.New("the repository sends its user name and password over plain http, unencrypted; " +
	"Cogpit sends them only once you allow it for this repository in Cogpit's window (the Charts view asks)")

// ErrDigestMismatch refuses a chart archive whose SHA-256 differs from the digest its repository's
// index lists: the archive is not the one the index describes.
var ErrDigestMismatch = errors.New("the chart archive does not match the digest its repository's index lists")

// Fetcher downloads repository indexes and chart archives, bounded. Its zero value is ready to use;
// it must not be copied once used.
type Fetcher struct {
	// Client makes the requests; nil: a client with Cogpit's timeout and the environment's proxy
	// settings. Its redirects are followed by Cogpit's rules (no https to http, at most 10),
	// whatever the client allows.
	Client *http.Client
	// MaxIndex and MaxChart bound a download; zero means MaxIndexBytes and MaxChartBytes.
	MaxIndex, MaxChart int64
	// Proxy, when set, picks the proxy of each request, as http.Transport.Proxy does (the engine
	// sets Cogpit's preference for the traffic that is no cluster's, else the environment's); it
	// is asked per request, so a changed preference applies to the next download. Nil: the
	// client's transport decides (the default client: the environment's HTTPS_PROXY). It applies
	// to a client whose transport is an *http.Transport (or nil); another transport keeps its own.
	Proxy func(*http.Request) (*url.URL, error)

	proxied     sync.Once
	proxiedBase http.RoundTripper // the transport with Proxy, made once
}

var defaultClient = &http.Client{Timeout: fetchTimeout}

// clientFor returns the client of one download from r: the fetcher's client, its transport with
// the fetcher's Proxy, r's TLS settings (a transport of its own, read from r's files now) and r's
// basic auth for r's own scheme, host and port (or every host with PassCredentialsAll), with
// Cogpit's redirect rules. done releases a transport made for the download.
func (f *Fetcher) clientFor(r *Repository) (c *http.Client, done func(), err error) {
	base := defaultClient
	if f != nil && f.Client != nil {
		base = f.Client
	}
	cc := *base
	rt := base.Transport
	if f != nil && f.Proxy != nil {
		rt = f.proxiedTransport(base.Transport)
	}
	done = func() {}
	if r.needsTLSConfig() {
		if rt == nil {
			rt = http.DefaultTransport
		}
		t, ok := rt.(*http.Transport)
		if !ok {
			return nil, nil, errors.New("the repository's TLS settings need an HTTP transport Cogpit can configure")
		}
		t = t.Clone()
		cfg, err := r.tlsConfig(t.TLSClientConfig)
		if err != nil {
			return nil, nil, err
		}
		t.TLSClientConfig = cfg
		rt, done = t, t.CloseIdleConnections
	}
	if r.auth != nil {
		if rt == nil {
			rt = http.DefaultTransport
		}
		rt = &authTransport{base: rt, auth: *r.auth, origin: origin(r.URL), all: r.PassCredentialsAll, plain: r.plainAuth}
	}
	cc.Transport = rt
	next := base.CheckRedirect
	https := r.https()
	cc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != schemeHTTPS && req.URL.Scheme != schemeHTTP {
			return fmt.Errorf("a redirect: %w", ErrNotHTTPS)
		}
		if req.URL.Scheme == schemeHTTP && (https || slices.ContainsFunc(via, func(v *http.Request) bool { return v.URL.Scheme == schemeHTTPS })) {
			return fmt.Errorf("a redirect from https to http: %w", ErrNotHTTPS)
		}
		if req.URL.User != nil {
			return errors.New("a redirect to a URL with a user or password is not followed")
		}
		if len(via) >= maxRedirects {
			return errors.New("too many redirects")
		}
		if next != nil {
			return next(req, via)
		}
		return nil
	}
	return &cc, done, nil
}

// tlsConfig is base (a clone; nil: Go's defaults) with r's CA file as the only roots (as helm
// uses it), r's client certificate and key, and insecure_skip_tls_verify. The files are read now,
// bounded; an error names a file's path, never its content.
func (r *Repository) tlsConfig(base *tls.Config) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if base != nil {
		cfg = base.Clone()
	}
	if r.CAFile != "" {
		pem, err := readBounded(r.CAFile)
		if err != nil {
			return nil, fmt.Errorf("the repository's CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("the repository's CA file %q holds no PEM certificate", r.CAFile)
		}
		cfg.RootCAs = pool
	}
	if r.ClientCertificate() {
		cert, err := readBounded(r.CertFile)
		if err != nil {
			return nil, fmt.Errorf("the repository's client certificate: %w", err)
		}
		key, err := readBounded(r.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("the repository's client key: %w", err)
		}
		pair, err := tls.X509KeyPair(cert, key)
		clear(key)
		if err != nil {
			return nil, fmt.Errorf("the repository's client certificate %q and key %q do not form a PEM key pair", r.CertFile, r.KeyFile)
		}
		cfg.Certificates = []tls.Certificate{pair}
	}
	if r.InsecureSkipTLSVerify {
		cfg.InsecureSkipVerify = true // #nosec G402 -- the user's Helm entry says insecure_skip_tls_verify; marked in the window
	}
	return cfg, nil
}

// authTransport adds a repository's basic auth to the requests for its own scheme, host and port
// (every request with all): each hop of a redirect is decided on its own, so Go's rule that keeps
// Authorization for a subdomain never applies. A plain http request gets it only with plain, the
// user's allowance for the repository (H-40); fetch refuses such a download before it starts, so
// this is the guard behind that check.
type authTransport struct {
	base   http.RoundTripper
	auth   basicAuth
	origin string
	all    bool
	plain  bool
}

func (a *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme == schemeHTTP && !a.plain {
		return a.base.RoundTrip(req)
	}
	if a.all || origin(req.URL.String()) == a.origin {
		req = req.Clone(req.Context())
		req.SetBasicAuth(a.auth.user, a.auth.pass)
	}
	return a.base.RoundTrip(req)
}

// CloseIdleConnections closes the base transport's idle connections.
func (a *authTransport) CloseIdleConnections() {
	if c, ok := a.base.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}

// origin is a URL's scheme, host (lower case) and port (the scheme's when the URL names none).
func origin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	port := u.Port()
	if port == "" {
		port = map[string]string{schemeHTTPS: "443", schemeHTTP: "80"}[u.Scheme]
	}
	return u.Scheme + "://" + net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}

// proxiedTransport returns the client's transport with the fetcher's Proxy, made once and kept,
// so that downloads reuse its connections: a clone of base (http.DefaultTransport for nil) whose
// Proxy asks f.Proxy for every request. A transport that is no *http.Transport is returned as it
// is.
func (f *Fetcher) proxiedTransport(base http.RoundTripper) http.RoundTripper {
	f.proxied.Do(func() {
		if base == nil {
			base = http.DefaultTransport
		}
		t, ok := base.(*http.Transport)
		if !ok {
			f.proxiedBase = base
			return
		}
		t = t.Clone()
		t.Proxy = func(req *http.Request) (*url.URL, error) { return f.Proxy(req) }
		f.proxiedBase = t
	})
	return f.proxiedBase
}

func (f *Fetcher) maxIndex() int64 {
	if f != nil && f.MaxIndex > 0 {
		return f.MaxIndex
	}
	return MaxIndexBytes
}

func (f *Fetcher) maxChart() int64 {
	if f != nil && f.MaxChart > 0 {
		return f.MaxChart
	}
	return MaxChartBytes
}

// get downloads a URL of repository r, at most limit bytes: https, or http for a repository served
// over http. An error never quotes r's credentials, and shows URLs redacted.
func (f *Fetcher) get(ctx context.Context, r *Repository, rawURL string, limit int64) ([]byte, error) {
	data, err := f.fetch(ctx, r, rawURL, limit)
	return data, r.scrubError(err)
}

func (f *Fetcher) fetch(ctx context.Context, r *Repository, rawURL string, limit int64) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, errors.New("the URL does not parse")
	}
	switch {
	case u.Scheme != schemeHTTPS && u.Scheme != schemeHTTP:
		return nil, ErrNotHTTPS
	case u.Scheme == schemeHTTP && r.https():
		return nil, ErrNotHTTPS
	case u.User != nil:
		return nil, errors.New("a URL with a user or password is not followed")
	case u.Scheme == schemeHTTP && r.HasBasicAuth() && !r.plainAuth:
		return nil, ErrCredentialsOverHTTP
	}
	shown := RedactURL(u.String())
	c, done, err := f.clientFor(r)
	if err != nil {
		return nil, err
	}
	defer done()
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errors.New("the URL does not make a request")
	}
	req.Header.Set("User-Agent", "Cogpit")
	resp, err := c.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			return nil, &url.Error{Op: ue.Op, URL: RedactURL(ue.URL), Err: ue.Err}
		}
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", shown, resp.Status)
	}
	if resp.ContentLength > limit {
		return nil, fmt.Errorf("%s: larger than the %s Cogpit downloads", shown, model.FormatBytes(limit))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s: larger than the %s Cogpit downloads", shown, model.FormatBytes(limit))
	}
	return data, nil
}

// Index is what Cogpit keeps of a repository's index: its charts by name, each with its versions
// newest first (the Helm SDK's order).
type Index struct {
	Generated time.Time    `json:"generated"`
	Charts    []IndexChart `json:"charts"`
}

// IndexChart is one chart of an index.
type IndexChart struct {
	Name     string         `json:"name"`
	Versions []ChartVersion `json:"versions"`
}

// ChartVersion is one version of a chart as its repository's index lists it.
type ChartVersion struct {
	Version     string    `json:"version"`
	AppVersion  string    `json:"appVersion,omitempty"`
	Description string    `json:"description,omitempty"`
	Home        string    `json:"home,omitempty"`
	Icon        string    `json:"icon,omitempty"`
	KubeVersion string    `json:"kubeVersion,omitempty"`
	Type        string    `json:"type,omitempty"`
	Keywords    []string  `json:"keywords,omitempty"`
	Maintainers []string  `json:"maintainers,omitempty"`
	Sources     []string  `json:"sources,omitempty"`
	Deprecated  bool      `json:"deprecated,omitempty"`
	Created     time.Time `json:"created,omitzero"`
	// Digest is the archive's SHA-256 as the index lists it; empty when the index lists none, and
	// the archive is then not verified.
	Digest string   `json:"digest,omitempty"`
	URLs   []string `json:"urls,omitempty"`
}

// Prerelease reports whether the version is a semantic-version prerelease ("1.2.0-rc.1"), which
// Helm's search leaves out unless asked for (--devel).
func (v ChartVersion) Prerelease() bool {
	core, _, _ := strings.Cut(v.Version, "+") // build metadata may hold a '-'
	return strings.Contains(core, "-")
}

// Latest returns the version a chart is listed with: the newest that is not a prerelease, as helm
// search shows it, else the newest.
func (c IndexChart) Latest() (ChartVersion, bool) {
	if len(c.Versions) == 0 {
		return ChartVersion{}, false
	}
	for _, v := range c.Versions {
		if !v.Prerelease() {
			return v, true
		}
	}
	return c.Versions[0], true
}

// Version returns a chart's version by its exact version string.
func (c IndexChart) Version(version string) (ChartVersion, bool) {
	i := slices.IndexFunc(c.Versions, func(v ChartVersion) bool { return v.Version == version })
	if i < 0 {
		return ChartVersion{}, false
	}
	return c.Versions[i], true
}

// Chart returns a chart of the index by name.
func (x *Index) Chart(name string) (IndexChart, bool) {
	if x == nil {
		return IndexChart{}, false
	}
	i, ok := slices.BinarySearchFunc(x.Charts, name, func(c IndexChart, n string) int { return strings.Compare(c.Name, n) })
	if !ok {
		return IndexChart{}, false
	}
	return x.Charts[i], true
}

// DownloadIndex downloads a repository's index.yaml (bounded), parses it with the Helm SDK and,
// only when it parses, replaces dest with it (0600, in a 0700 directory, atomically): a download
// that fails or does not parse leaves the previous index. It returns what Cogpit keeps of it.
func (f *Fetcher) DownloadIndex(ctx context.Context, r *Repository, dest string) (*Index, error) {
	indexURL, err := repo.ResolveReferenceURL(r.URL, "index.yaml")
	if err != nil {
		return nil, errors.New("the repository URL does not parse")
	}
	data, err := f.get(ctx, r, indexURL, f.maxIndex())
	if err != nil {
		return nil, fmt.Errorf("downloading the index: %w", err)
	}
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(dest)+"-*.tmp") // created 0600
	if err != nil {
		return nil, err
	}
	done := false
	defer func() {
		if !done {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	idx, err := loadIndex(tmp.Name(), f.maxIndex())
	if err != nil {
		return nil, err
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return nil, err
	}
	done = true
	return idx, nil
}

// LoadIndex reads an index Cogpit downloaded before (DownloadIndex) with the Helm SDK. A file
// larger than MaxIndexBytes is refused.
func LoadIndex(path string) (*Index, error) { return loadIndex(path, MaxIndexBytes) }

func loadIndex(path string, limit int64) (idx *Index, err error) {
	defer func() {
		if r := recover(); r != nil { // the index is untrusted input
			idx, err = nil, errors.New("the index could not be read")
		}
	}()
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, errors.New("the index is not a regular file")
	}
	if fi.Size() > limit {
		return nil, fmt.Errorf("the index is larger than the %s Cogpit reads", model.FormatBytes(limit))
	}
	f, err := repo.LoadIndexFile(path)
	if err != nil {
		return nil, errors.New("the index is not a Helm repository index (index.yaml)")
	}
	return summarize(f), nil
}

// summarize keeps what the Charts view, the drawer and an install need of an index, bounded.
func summarize(f *repo.IndexFile) *Index {
	out := &Index{Generated: f.Generated}
	for name, versions := range f.Entries {
		if !chartNamePattern.MatchString(name) {
			continue
		}
		c := IndexChart{Name: name}
		for _, v := range versions {
			if v == nil || v.Metadata == nil || v.Version == "" || len(c.Versions) >= maxVersions {
				continue
			}
			md := v.Metadata
			cv := ChartVersion{
				Version: clip(md.Version), AppVersion: clip(md.AppVersion), Description: clip(md.Description),
				Home: clip(md.Home), Icon: clip(md.Icon), KubeVersion: clip(md.KubeVersion), Type: clip(md.Type),
				Keywords: clipList(md.Keywords), Sources: clipList(md.Sources), Deprecated: md.Deprecated,
				Created: v.Created, Digest: clip(v.Digest), URLs: clipList(v.URLs),
			}
			for _, m := range md.Maintainers {
				if m != nil && m.Name != "" && len(cv.Maintainers) < maxListed {
					cv.Maintainers = append(cv.Maintainers, clip(m.Name))
				}
			}
			c.Versions = append(c.Versions, cv)
		}
		if len(c.Versions) > 0 {
			out.Charts = append(out.Charts, c)
		}
	}
	slices.SortFunc(out.Charts, func(a, b IndexChart) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func clip(s string) string {
	if len(s) <= maxText {
		return s
	}
	s = s[:maxText]
	for len(s) > 0 && !utf8.ValidString(s) { // a rune cut in half
		s = s[:len(s)-1]
	}
	return s + "…"
}

func clipList(in []string) []string {
	var out []string
	for _, s := range in {
		if s != "" && len(out) < maxListed {
			out = append(out, clip(s))
		}
	}
	return out
}

// Chart is a chart archive downloaded from a repository and loaded with the Helm SDK: what an
// install renders and installs.
type Chart struct {
	chart *chart.Chart

	Repository  string `json:"repository"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	AppVersion  string `json:"appVersion,omitempty"`
	Description string `json:"description,omitempty"`
	// Digest is the archive's SHA-256 (hex); Verified says it matched the digest its repository's
	// index lists, false when the index lists none.
	Digest   string `json:"digest"`
	Verified bool   `json:"verified"`
	Size     int    `json:"size"`
	// Origin is how the archive came when nothing authenticated it: its repository's Origin at
	// the download (H-41): kinds.HelmOriginHTTP, kinds.HelmOriginInsecureTLS, or "".
	Origin string `json:"origin,omitempty"`
}

// FetchChart downloads a chart version's archive from its repository (the index's first URL,
// resolved against the repository's URL; bounded; the repository's credentials only for its own
// scheme, host and port unless pass_credentials_all; never plain http for an https repository),
// checks it against the index's digest when the index lists one, and loads it with the Helm SDK.
// The archive must hold the chart and version the index names.
func (f *Fetcher) FetchChart(ctx context.Context, r *Repository, name string, v ChartVersion) (*Chart, error) {
	if len(v.URLs) == 0 {
		return nil, fmt.Errorf("chart %s %s: the index lists no URL", name, v.Version)
	}
	u, err := repo.ResolveReferenceURL(r.URL, v.URLs[0])
	if err != nil {
		return nil, fmt.Errorf("chart %s %s: the URL does not parse", name, v.Version)
	}
	data, err := f.get(ctx, r, u, f.maxChart())
	if err != nil {
		return nil, fmt.Errorf("chart %s %s: %w", name, v.Version, err)
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	verified := false
	if want := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(v.Digest)), "sha256:"); want != "" {
		if want != digest {
			return nil, fmt.Errorf("chart %s %s: %w", name, v.Version, ErrDigestMismatch)
		}
		verified = true
	}
	c, err := LoadChartArchive(data)
	if err != nil {
		return nil, fmt.Errorf("chart %s %s: %w", name, v.Version, err)
	}
	if c.Name != name || c.Version != v.Version {
		return nil, fmt.Errorf("chart %s %s: the archive holds chart %s %s", name, v.Version, c.Name, c.Version)
	}
	c.Repository, c.Digest, c.Verified, c.Origin = r.Name, digest, verified, r.Origin()
	return c, nil
}

// LoadChartArchive loads a chart archive (.tgz) with the Helm SDK. The archive is untrusted input:
// the SDK bounds its decompressed size, and a loader that panics is an error.
func LoadChartArchive(data []byte) (c *Chart, err error) {
	defer func() {
		if r := recover(); r != nil {
			c, err = nil, errors.New("the chart archive could not be read")
		}
	}()
	ch, err := loader.LoadArchive(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("the chart archive could not be read: %w", err)
	}
	if ch.Metadata == nil {
		return nil, errors.New("the chart archive holds no Chart.yaml")
	}
	sum := sha256.Sum256(data)
	return &Chart{
		chart: ch, Name: ch.Metadata.Name, Version: ch.Metadata.Version, AppVersion: ch.Metadata.AppVersion,
		Description: clip(ch.Metadata.Description), Digest: hex.EncodeToString(sum[:]), Size: len(data),
	}, nil
}

// DefaultValues returns the chart's values.yaml as the chart ships it, comments included: what a
// Helm Install tab starts with. A chart without one has none; one whose file is missing from the
// archive's raw files is written from its parsed values.
func (c *Chart) DefaultValues() string {
	if c == nil || c.chart == nil {
		return ""
	}
	for _, f := range c.chart.Raw {
		if f != nil && f.Name == "values.yaml" {
			return string(f.Data)
		}
	}
	if len(c.chart.Values) == 0 {
		return ""
	}
	y, err := yaml.Marshal(c.chart.Values)
	if err != nil {
		return ""
	}
	return string(y)
}

// Defaults returns the chart's parsed default values (shared: do not modify).
func (c *Chart) Defaults() map[string]any {
	if c == nil || c.chart == nil {
		return nil
	}
	return c.chart.Values
}

// checkInstallable refuses what helm install refuses before rendering: a library chart and a
// chart whose dependencies are not packaged with it (Cogpit does not fetch dependencies).
func (c *Chart) checkInstallable() error {
	if c == nil || c.chart == nil || c.chart.Metadata == nil {
		return errors.New("no chart")
	}
	if t := c.chart.Metadata.Type; t != "" && t != "application" {
		return fmt.Errorf("chart %s is a %s chart, which cannot be installed", c.Name, t)
	}
	var missing []string
	for _, d := range c.chart.Metadata.Dependencies {
		if d == nil {
			continue
		}
		if !slices.ContainsFunc(c.chart.Dependencies(), func(sub *chart.Chart) bool { return sub.Name() == d.Name }) {
			missing = append(missing, d.Name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("chart %s lacks its dependencies %s in its charts/ folder", c.Name, strings.Join(missing, ", "))
	}
	return nil
}
