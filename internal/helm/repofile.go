package helm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"helm.sh/helm/v4/pkg/cli"
	repo "helm.sh/helm/v4/pkg/repo/v1"
	"sigs.k8s.io/yaml"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// Helm's own repositories.yaml (D-29, architecture §8): the chart repositories the helm CLI keeps
// (helm repo add, helm repo remove), which Cogpit reads and never writes. Read bounded, parsed with
// the Helm SDK's repo.File and repo.Entry types, each entry checked into a Repository: its name,
// its URL without a user or password, and how Cogpit connects to it (basic auth, a CA file, a
// client certificate and key, insecure_skip_tls_verify, pass_credentials_all), as the helm CLI
// uses it. The credentials stay in memory, unexported, and never print.

// Bounds on the local files Cogpit reads for the repositories (Cogpit's default for the local files
// it reads, as a kubeconfig's: kubeconfig.MaxFileSize).
const (
	// MaxRepositoryFileBytes bounds Helm's repositories.yaml, and a CA file, a client certificate
	// and its key that an entry names.
	MaxRepositoryFileBytes int64 = 1 << 20
	// MaxRepositories bounds the entries of Helm's file Cogpit uses (Cogpit's default); the rest
	// are listed as not used.
	MaxRepositories = 256
)

// Schemes a chart repository is served over.
const (
	schemeHTTPS = "https"
	schemeHTTP  = "http"
)

// redactedText replaces a credential wherever Cogpit shows text that could have held one.
const redactedText = "•••"

// RepositoryConfigPath returns the repositories.yaml the helm CLI uses, by the helm CLI's own rules
// (the Helm SDK's cli.EnvSettings): $HELM_REPOSITORY_CONFIG, else repositories.yaml in
// $HELM_CONFIG_HOME, else in Helm's configuration folder (helmpath: $XDG_CONFIG_HOME/helm, else
// ~/Library/Preferences/helm on macOS, ~/.config/helm on Linux, %APPDATA%\helm on Windows). It
// reads the environment only, never the file.
func RepositoryConfigPath() string { return cli.New().RepositoryConfig }

// repoNamePattern is a repository's name as Cogpit can list its charts: letters, digits, '.', '-'
// and '_', the first a letter or a digit. A dot is taken (A36): the chart's id
// doubles it (kinds.HelmChartName), so the id still tells the repository from the chart.
var repoNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

// chartNamePattern is a chart's name in an index that Cogpit lists: Helm's chart names, without a
// path separator, the first character a letter or a digit (which kinds.SplitHelmChartName relies
// on: the dot before it is the id's separator).
var chartNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,200}$`)

// CheckRepositoryName checks that Cogpit can list a repository's charts under its name. Helm
// allows more (helm repo add refuses only a '/', read in Helm v4.3.0's repo_add.go); such a
// repository is listed with this error and not used (Cogpit's default).
func CheckRepositoryName(name string) error {
	if !repoNamePattern.MatchString(name) {
		return errors.New("a repository Cogpit uses has a name of 1 to 63 letters, digits, '.', '-' or '_', the first a " +
			"letter or a digit; remove it with helm repo remove and add it again under such a name")
	}
	return nil
}

// Repository is a chart repository as Helm's repositories.yaml names it, ready to download from.
// Its credentials are unexported: %v, %+v, %#v, slog and JSON show its name and redacted URL only.
type Repository struct {
	// Name is the repository's name in Helm's file.
	Name string
	// URL is what Cogpit requests: http or https, without a user or password (those in Helm's
	// file's URL are the repository's basic auth, as Go's HTTP client, and so the helm CLI, sends
	// them; Cogpit's default).
	URL string
	// CAFile, CertFile and KeyFile are the files Helm's entry names, read at every download (a
	// rotated file applies). A client certificate is used when both CertFile and KeyFile are set,
	// as helm uses it.
	CAFile, CertFile, KeyFile string
	// InsecureSkipTLSVerify switches the server certificate's check off, as Helm's entry says.
	InsecureSkipTLSVerify bool
	// PassCredentialsAll sends the basic auth to every host the downloads reach (redirects, chart
	// URLs the index points elsewhere), as Helm's entry says; otherwise only to the repository's
	// own scheme, host and port.
	PassCredentialsAll bool

	auth *basicAuth
	// plainAuth lets the basic auth go over plain http: set only on a copy the engine hands to a
	// download once the user allowed it (AllowCredentialsOverHTTP, credentials H-40).
	plainAuth bool
}

// basicAuth is a repository's user and password; it never prints.
type basicAuth struct{ user, pass string }

func (basicAuth) String() string               { return "<redacted>" }
func (basicAuth) GoString() string             { return "<redacted>" }
func (basicAuth) LogValue() slog.Value         { return slog.StringValue("<redacted>") }
func (basicAuth) MarshalJSON() ([]byte, error) { return []byte(`"<redacted>"`), nil }

// String names the repository and its redacted URL.
func (r Repository) String() string { return r.Name + " (" + RedactURL(r.URL) + ")" }

// GoString is String: %#v never prints the credentials.
func (r Repository) GoString() string { return r.String() }

// LogValue is String: a log line never carries the credentials.
func (r Repository) LogValue() slog.Value { return slog.StringValue(r.String()) }

// MarshalJSON writes the name and the redacted URL only.
func (r Repository) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}{r.Name, RedactURL(r.URL)})
}

// PublicRepository is a repository without credentials or TLS settings (tests, and a repository
// built in code).
func PublicRepository(name, rawURL string) *Repository {
	return &Repository{Name: name, URL: strings.TrimRight(rawURL, "/")}
}

// WithBasicAuth returns a copy of r that sends a user and password (tests, and a repository built
// in code).
func (r Repository) WithBasicAuth(user, pass string) *Repository {
	r.auth = &basicAuth{user: user, pass: pass}
	return &r
}

// HasBasicAuth reports whether the repository sends a user and password.
func (r *Repository) HasBasicAuth() bool { return r != nil && r.auth != nil }

// ClientCertificate reports whether the repository presents a client certificate.
func (r *Repository) ClientCertificate() bool { return r != nil && r.CertFile != "" && r.KeyFile != "" }

// HasCredentials reports whether the repository sends credentials: a user and password, or a
// client certificate.
func (r *Repository) HasCredentials() bool { return r.HasBasicAuth() || r.ClientCertificate() }

// PlainHTTP reports whether the repository is served over plain http.
func (r *Repository) PlainHTTP() bool {
	u, err := url.Parse(r.URL)
	return err == nil && u.Scheme == schemeHTTP
}

// CredentialsOverHTTP reports whether the repository would send its user and password over plain
// http: it is served over http and has basic auth (a client certificate needs TLS and is not sent
// over http at all). Such a repository downloads only from a copy AllowCredentialsOverHTTP made
// (credentials H-40, D-29).
func (r *Repository) CredentialsOverHTTP() bool { return r.HasBasicAuth() && r.PlainHTTP() }

// AllowCredentialsOverHTTP returns a copy of r whose basic auth may go over plain http: the
// engine makes it for a download once the user allowed that for the repository (H-40). r itself
// is unchanged.
func (r Repository) AllowCredentialsOverHTTP() *Repository {
	r.plainAuth = true
	return &r
}

// HTTPBinding is what the user's allowance of credentials over plain http binds (H-40, owner): the
// repository's name, its URL and its user name, as a SHA-256 in hex. A changed URL or user gives
// another binding, and the allowance no longer applies; a changed password keeps it. No password
// goes into it, so it may be kept on disk; empty for a repository without basic auth.
func (r *Repository) HTTPBinding() string {
	if r == nil || r.auth == nil {
		return ""
	}
	h := sha256.New()
	for _, s := range []string{"cogpit-helm-http-credentials", r.Name, r.URL, r.auth.user} {
		_, _ = io.WriteString(h, s+"\x00")
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Origin says how the repository's content reaches Cogpit when nothing authenticates it (H-41,
// owner, A14): kinds.HelmOriginHTTP for a repository served over plain http,
// kinds.HelmOriginInsecureTLS for one over https whose entry says insecure_skip_tls_verify; ""
// for https with the server's certificate checked. Plain http wins: its entry's TLS setting does
// not apply.
func (r *Repository) Origin() string {
	switch {
	case r == nil:
		return ""
	case r.PlainHTTP():
		return kinds.HelmOriginHTTP
	case r.InsecureSkipTLSVerify && r.https():
		return kinds.HelmOriginInsecureTLS
	}
	return ""
}

func (r *Repository) https() bool {
	u, err := url.Parse(r.URL)
	return err == nil && u.Scheme == schemeHTTPS
}

func (r *Repository) needsTLSConfig() bool {
	return r.CAFile != "" || r.ClientCertificate() || r.InsecureSkipTLSVerify
}

// CacheKey names the repository's index in Cogpit's cache: its name and URL, so that a changed URL
// never reuses the old index. No credential goes into it.
func (r *Repository) CacheKey() string {
	sum := sha256.Sum256([]byte(r.Name + "\x00" + r.URL))
	return hex.EncodeToString(sum[:16])
}

// Fingerprint changes whenever anything Cogpit connects with changes: the URL, the credentials,
// the TLS settings. Kept in memory only (never written: it is derived from the password).
func (r *Repository) Fingerprint() string {
	h := sha256.New()
	for _, s := range []string{r.Name, r.URL, r.CAFile, r.CertFile, r.KeyFile,
		fmt.Sprint(r.InsecureSkipTLSVerify, r.PassCredentialsAll, r.auth != nil)} {
		_, _ = io.WriteString(h, s+"\x00")
	}
	if r.auth != nil {
		_, _ = io.WriteString(h, r.auth.user+"\x00"+r.auth.pass)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// scrub replaces the repository's password, and its user name when it has three or more bytes,
// in text that is about to leave the download: a guard, since the URLs Cogpit requests hold no
// credentials.
func (r *Repository) scrub(s string) string {
	if r == nil || r.auth == nil {
		return s
	}
	if r.auth.pass != "" {
		s = strings.ReplaceAll(s, r.auth.pass, redactedText)
		if q := url.QueryEscape(r.auth.pass); q != r.auth.pass {
			s = strings.ReplaceAll(s, q, redactedText)
		}
	}
	if len(r.auth.user) >= 3 {
		s = strings.ReplaceAll(s, r.auth.user, redactedText)
	}
	return s
}

// scrubError returns err with the repository's credentials scrubbed from its text; errors.Is
// still finds the sentinels it wraps (errors.As does not reach past it, so that nothing prints
// the original text).
func (r *Repository) scrubError(err error) error {
	if err == nil {
		return nil
	}
	msg := r.scrub(err.Error())
	return &scrubbedError{msg: msg, orig: err}
}

type scrubbedError struct {
	msg  string
	orig error
}

func (e *scrubbedError) Error() string        { return e.msg }
func (e *scrubbedError) Is(target error) bool { return errors.Is(e.orig, target) }

// RedactURL is a URL as Cogpit shows it: without a user or password, its query's values hidden
// (a token may sit there). A URL that does not parse is not shown.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "(a URL that does not parse)"
	}
	u.User = nil
	if u.RawQuery != "" {
		q := u.Query()
		parts := make([]string, 0, len(q))
		for k := range q {
			parts = append(parts, url.QueryEscape(k)+"="+redactedText)
		}
		u.RawQuery = strings.Join(parts, "&")
	}
	return u.String()
}

// RepositoryEntry is one entry of Helm's repositories.yaml as Cogpit read it: what can be shown of
// it, and the Repository Cogpit downloads from, or why it does not use it.
type RepositoryEntry struct {
	// Name is the entry's name; URL its URL redacted (RedactURL).
	Name, URL string
	// Repository is nil when Cogpit does not use the entry; Err says why.
	Repository *Repository
	Err        error
	// What the entry says about its connection, shown also when it is not used.
	HasCredentials, ClientCertificate, CAFile, InsecureSkipTLSVerify, PassCredentialsAll, PlainHTTP bool
}

// ReadRepositoryFile reads Helm's repositories.yaml: at most MaxRepositoryFileBytes, a regular file
// (a symbolic link to one is followed), parsed with the Helm SDK's types. A file that does not
// exist matches fs.ErrNotExist. An error never quotes the file's content: a parse error names the
// line only.
func ReadRepositoryFile(path string) (entries []RepositoryEntry, err error) {
	data, err := readBounded(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		return nil, fmt.Errorf("the repositories file: %w", err)
	}
	defer func() {
		if r := recover(); r != nil { // the file is the user's, but still input
			entries, err = nil, fmt.Errorf("the repositories file %s could not be read", path)
		}
	}()
	var f repo.File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("the repositories file %s does not parse%s", path, parseLine(err))
	}
	named, used := map[string]bool{}, 0
	for _, e := range f.Repositories {
		if e == nil {
			continue
		}
		entry := checkEntry(e)
		switch {
		case named[e.Name]:
			entry.Repository, entry.Err = nil, errors.New("a second entry of this name in Helm's file: helm uses the first")
		case used >= MaxRepositories && entry.Repository != nil:
			entry.Repository, entry.Err = nil, fmt.Errorf("only the first %d repositories of the file are used", MaxRepositories)
		}
		named[e.Name] = true
		if entry.Repository != nil {
			used++
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// lineRef finds the line a YAML parse error names.
var lineRef = regexp.MustCompile(`line (\d+)`)

func parseLine(err error) string {
	if m := lineRef.FindStringSubmatch(err.Error()); m != nil {
		return " (line " + m[1] + ")"
	}
	return ""
}

// checkEntry turns one entry of Helm's file into what Cogpit uses of it.
func checkEntry(e *repo.Entry) RepositoryEntry {
	out := RepositoryEntry{
		Name: e.Name, URL: RedactURL(strings.TrimSpace(e.URL)),
		CAFile: e.CAFile != "", InsecureSkipTLSVerify: e.InsecureSkipTLSVerify, PassCredentialsAll: e.PassCredentialsAll,
		ClientCertificate: e.CertFile != "" && e.KeyFile != "",
	}
	r, err := newRepository(e)
	if r != nil {
		out.HasCredentials, out.PlainHTTP = r.HasCredentials(), r.PlainHTTP()
	} else {
		out.HasCredentials = (e.Username != "" && e.Password != "") || out.ClientCertificate || strings.Contains(e.URL, "@")
		out.PlainHTTP = strings.HasPrefix(strings.ToLower(strings.TrimSpace(e.URL)), schemeHTTP+"://")
	}
	out.Repository, out.Err = r, err
	return out
}

// newRepository checks an entry of Helm's file. An error never quotes the URL or a credential.
func newRepository(e *repo.Entry) (*Repository, error) {
	if err := CheckRepositoryName(e.Name); err != nil {
		return nil, err
	}
	u, err := url.Parse(strings.TrimSpace(e.URL))
	switch {
	case err != nil:
		return nil, errors.New("its URL does not parse")
	case u.Scheme == "oci":
		return nil, errors.New("an OCI registry: Cogpit reads chart repositories served over http or https only")
	case u.Scheme != schemeHTTPS && u.Scheme != schemeHTTP:
		return nil, errors.New("its URL is neither https nor http")
	case u.Host == "" || u.Opaque != "":
		return nil, errors.New("its URL names no host")
	}
	r := &Repository{
		Name: e.Name, CAFile: e.CAFile, CertFile: e.CertFile, KeyFile: e.KeyFile,
		InsecureSkipTLSVerify: e.InsecureSkipTLSVerify, PassCredentialsAll: e.PassCredentialsAll,
	}
	for _, p := range []struct{ what, path string }{{"CA file", e.CAFile}, {"client certificate", e.CertFile}, {"client key", e.KeyFile}} {
		if p.path != "" && !filepath.IsAbs(p.path) {
			return nil, fmt.Errorf("its %s %q is no absolute path: Cogpit does not run in the folder helm ran in", p.what, p.path)
		}
	}
	switch {
	case e.Username != "" && e.Password != "":
		r.auth = &basicAuth{user: e.Username, pass: e.Password}
	case u.User != nil && u.User.Username() != "":
		pass, _ := u.User.Password()
		r.auth = &basicAuth{user: u.User.Username(), pass: pass}
	}
	u.User = nil
	u.Fragment, u.RawFragment = "", ""
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	r.URL = u.String()
	return r, nil
}

// readBounded reads a regular file of at most MaxRepositoryFileBytes (a symbolic link to one is
// followed; anything else, a named pipe for one, is refused before it is opened). An error names
// the file's path, never its content.
func readBounded(path string) ([]byte, error) {
	limit := MaxRepositoryFileBytes
	if fi, err := os.Stat(path); err != nil {
		return nil, err
	} else if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	f, err := os.Open(path) // #nosec G304 -- a path the user's Helm configuration names
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	if fi.Size() > limit {
		return nil, fmt.Errorf("%s is larger than the %s Cogpit reads", path, model.FormatBytes(limit))
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than the %s Cogpit reads", path, model.FormatBytes(limit))
	}
	return data, nil
}
