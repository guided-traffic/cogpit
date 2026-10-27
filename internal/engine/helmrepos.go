package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// Helm chart repositories (G11, D-29, architecture §8): the repositories Helm's own
// repositories.yaml lists. The helm CLI adds and removes them (helm repo add, helm repo remove);
// Cogpit reads the file, never writes it or anything in Helm's folders. The file's size and
// modification time are looked at every 2 seconds (as the kubeconfig sync folders are) and the file
// read again when they changed, so a repository added, changed or removed there is in Cogpit within
// 5 seconds (the owner's bound); a file that does not parse keeps the last good list and says why,
// a missing file is an empty list, a read that failed is tried again at the next look. The
// credentials of an entry stay in memory (helm.Repository).
//
// The indexes are downloaded into Cogpit's own cache folder (<cache>/helm/indexes/<key>-index.yaml,
// the key made of the repository's name and URL, so a changed URL never reuses the old index; the
// lead's default, D-29's Built note): on an explicit update (helm.repo.update) and, as architecture
// §5 has it for data that cannot be watched, while the Charts view or a Helm Install tab is open,
// for an index older than its time-to-live, never downloaded, or of a repository that appeared or
// whose URL, credentials or TLS settings changed (at once: a change of the file wakes the poller).
// Their charts are the local kind helmcharts in the connected backend's local store (one object
// per chart), which the Charts view, the drawer and agents read like any kind.

// Defaults of the repositories (Cogpit's, not the owner's decision: an interval of the order of
// ten minutes keeps a repository's new versions visible without polling a server often).
const (
	// DefaultIndexTTL is how old an index may get while the Charts view or a Helm Install tab is
	// open before it is downloaded again.
	DefaultIndexTTL = 10 * time.Minute
	// indexPollEvery is how often the open view checks the indexes' age.
	indexPollEvery = time.Minute
	// DefaultHelmRepositoryFilePoll is how often Helm's repositories.yaml is looked at (its size and
	// modification time), as the kubeconfig sync folders are (DefaultKubeconfigPoll).
	DefaultHelmRepositoryFilePoll = 2 * time.Second
	// helmIndexDir holds the indexes under Cogpit's cache folder. The night run's indexes
	// (<cache>/helm/<name>-index.yaml) and its own list (<config>/helm/repositories.json) are left
	// where they are, unread (no implicit deletes).
	helmIndexDir = "helm/indexes"
	// helmReadmeDir holds the READMEs of the chart drawer (helmchartdrawer.go) under Cogpit's
	// cache folder.
	helmReadmeDir = "helm/readmes"
)

// Status of Helm's repositories file (HelmReposList.Status).
const (
	// HelmFileOK: the file was read and parsed.
	HelmFileOK = "ok"
	// HelmFileMissing: there is no file: no repositories (helm repo add makes it).
	HelmFileMissing = "missing"
	// HelmFileError: the file could not be read or does not parse; the last good list stays.
	HelmFileError = "error"
)

// helmFileHint is what the list says when Helm's file is missing or lists no repository.
const helmFileHint = "Add repositories with helm repo add <name> <url>; Cogpit lists them within seconds."

// ErrNoHelmRepos disables the repository commands of a session that reads none.
var ErrNoHelmRepos = errors.New("this Cogpit reads no Helm chart repositories")

// HelmRepoInfo is a repository of Helm's file as helm.repo.list reports it: its name, its URL
// without a user or password, what its entry says about the connection (never a credential), when
// its index was downloaded, how many charts it lists, and the error of its last download or why
// Cogpit does not use the entry.
type HelmRepoInfo struct {
	Name string `json:"name"`
	// URL is redacted: no user or password, the query's values hidden.
	URL string `json:"url"`
	// HasCredentials: the entry sends a user and password or a client certificate.
	HasCredentials bool `json:"hasCredentials"`
	// ClientCertificate: the entry names a client certificate and key; CAFile: a CA file.
	ClientCertificate bool `json:"clientCertificate,omitempty"`
	CAFile            bool `json:"caFile,omitempty"`
	// InsecureSkipTLSVerify: the server's certificate is not checked (insecure_skip_tls_verify).
	InsecureSkipTLSVerify bool `json:"insecureSkipTLSVerify,omitempty"`
	// PassCredentialsAll: the credentials go to every host the downloads reach.
	PassCredentialsAll bool `json:"passCredentialsAll,omitempty"`
	// PlainHTTP: the repository is served over http, unencrypted.
	PlainHTTP bool `json:"plainHTTP,omitempty"`
	// HTTPCredentials is set for a repository that would send its user and password over plain
	// http (credentials H-40): HTTPCredentialsRequired or HTTPCredentialsChanged while the user
	// has not allowed it (nothing is downloaded, Error says why), HTTPCredentialsAllowed once the
	// user did. HTTPToken, while it waits, is what helm.repo.allowHTTPCredentials takes back: it
	// changes with the repository's URL or user name and reveals neither.
	HTTPCredentials string    `json:"httpCredentials,omitempty"`
	HTTPToken       string    `json:"httpToken,omitempty"`
	Updated         time.Time `json:"updated,omitzero"`
	Charts          int       `json:"charts"`
	Error           string    `json:"error,omitempty"`
	Updating        bool      `json:"updating,omitempty"`
	// Unused: Cogpit does not use the entry (Error says why): nothing is downloaded, no charts.
	Unused bool `json:"unused,omitempty"`
}

// HelmReposList is helm.repo.list's result: Helm's repositories file, its status and the
// repositories it lists, in its order.
type HelmReposList struct {
	// File is Helm's repositories.yaml as the helm CLI resolves it.
	File string `json:"file"`
	// Status is HelmFileOK, HelmFileMissing or HelmFileError.
	Status string `json:"status"`
	// Error says why the file was not read (HelmFileError): the repositories are those of the
	// last time it was.
	Error string `json:"error,omitempty"`
	// Hint says how repositories are added, when there are none.
	Hint         string         `json:"hint,omitempty"`
	Repositories []HelmRepoInfo `json:"repositories"`
}

// repoEntry is one entry of Helm's file as Cogpit holds it.
type repoEntry struct {
	info HelmRepoInfo     // what is shown (Updated, Charts, Updating and a download's Error come from state)
	repo *helm.Repository // nil: not used
}

// repoState is what Cogpit holds of one repository's index.
type repoState struct {
	repo     *helm.Repository
	key, fp  string // helm.Repository.CacheKey and Fingerprint
	index    *helm.Index
	updated  time.Time
	err      string
	gen      uint64 // the generation of the index: the chart objects' resourceVersion
	since    uint64 // the generation it appeared at: part of its charts' UIDs
	updating bool
	fetchNow bool               // it appeared or changed while Cogpit ran: downloaded at once while shown
	cancel   context.CancelFunc // stops its running download
}

// HelmRepos keeps a session's chart repositories. Create it with NewHelmRepos and pass it as
// Options.HelmRepos; it belongs to one session.
type HelmRepos struct {
	file, cacheDir            string
	fetcher                   *helm.Fetcher
	ttl, pollEvery, fileEvery time.Duration
	now                       func() time.Time
	syncMu                    sync.Mutex // one read of Helm's file at a time
	fetchMu                   sync.Mutex // one download at a time
	kick                      chan struct{}
	settling                  func()                                       // tests: called when a changed file starts to settle
	read                      func(string) ([]helm.RepositoryEntry, error) // reads Helm's file: helm.ReadRepositoryFile (tests: one that fails)
	mu                        sync.Mutex
	loaded, parsed            bool   // Helm's file was looked at; a list was taken from it
	sig                       string // Helm's file's size and modification time at the last read
	fileStatus, fileErr       string
	entries                   []repoEntry
	state                     map[string]*repoState // the repositories used, by name
	gen                       uint64
	s                         *Session
	lastRev                   uint64
	poll                      chan struct{} // closed to stop the index poller; nil while none runs
	pubMu                     sync.Mutex
	pubTarget                 *Backend
	published                 map[string]string // chart object name → resourceVersion in pubTarget's local store
	readmes                   readmeStore       // the chart drawer's READMEs (helmchartdrawer.go)
	http                      *httpApprovals    // the user's allowances of credentials over plain http (helmhttp.go)
	icons                     chartIcons        // the charts' icons (helmicons.go, D-46)
}

// HelmReposOption configures NewHelmRepos.
type HelmReposOption func(*HelmRepos)

// WithChartFetcher downloads with f (tests: a client that trusts a local repository).
func WithChartFetcher(f *helm.Fetcher) HelmReposOption {
	return func(h *HelmRepos) { h.fetcher = f }
}

// WithIndexTTL sets the indexes' time-to-live and how often the open view checks it.
func WithIndexTTL(ttl, every time.Duration) HelmReposOption {
	return func(h *HelmRepos) { h.ttl, h.pollEvery = ttl, every }
}

// WithRepositoryFilePoll sets how often Helm's repositories file is looked at (tests).
func WithRepositoryFilePoll(every time.Duration) HelmReposOption {
	return func(h *HelmRepos) { h.fileEvery = every }
}

// NewHelmRepos returns the repositories of Helm's repositories file repoFile (the caller resolves
// it, helm.RepositoryConfigPath, so that no test can reach the user's own) with their indexes
// under cacheDir/helm/indexes, Cogpit's own cache folder. Nothing is read or written until used.
func NewHelmRepos(repoFile, cacheDir string, opts ...HelmReposOption) *HelmRepos {
	h := &HelmRepos{
		file: repoFile, cacheDir: filepath.Join(cacheDir, filepath.FromSlash(helmIndexDir)),
		fetcher: &helm.Fetcher{}, ttl: DefaultIndexTTL, pollEvery: indexPollEvery, fileEvery: DefaultHelmRepositoryFilePoll,
		now: time.Now, state: map[string]*repoState{}, kick: make(chan struct{}, 1), read: helm.ReadRepositoryFile,
		readmes: readmeStore{dir: filepath.Join(cacheDir, filepath.FromSlash(helmReadmeDir))},
		http:    newHTTPApprovals(""),
	}
	for _, o := range opts {
		o(h)
	}
	// The icons are fetched with the downloads' transport (its TLS settings), never their
	// repository's credentials (helm.IconFetcher).
	h.icons = chartIcons{dir: filepath.Join(cacheDir, filepath.FromSlash(helmIconDir)), fetcher: &helm.IconFetcher{Base: h.fetcher.Client},
		sem: make(chan struct{}, maxIconFetches)}
	return h
}

// HelmRepos returns the session's chart repositories, or nil when it reads none.
func (s *Session) HelmRepos() *HelmRepos { return s.opts.HelmRepos }

// File returns Helm's repositories file.
func (h *HelmRepos) File() string { return h.file }

func (h *HelmRepos) indexPath(key string) string {
	return filepath.Join(h.cacheDir, key+"-index.yaml")
}

// settle is how long Helm's file must stay unchanged before it is read: a quarter of the poll, at
// most 250 ms (Cogpit's default).
func (h *HelmRepos) settle() time.Duration {
	return min(h.fileEvery/4, 250*time.Millisecond)
}

// missingSignature is fileSignature's for a file that does not exist.
const missingSignature = "missing"

// fileSignature describes Helm's file as stat sees it (size, modification time), without reading
// it; missingSignature when it does not exist.
func (h *HelmRepos) fileSignature() string {
	fi, err := os.Stat(h.file)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return missingSignature
	case err != nil:
		return "unreadable " + err.Error()
	}
	return fmt.Sprintf("%d %d %s", fi.Size(), fi.ModTime().UnixNano(), fi.Mode())
}

// Sync reads Helm's repositories file again when its size or modification time changed since the
// last read (or it was never read) and takes over what changed. It reports whether the
// repositories changed. A file that does not parse keeps the last good list; a missing file is an
// empty list. A read that did not reach the file (on Windows: the file held by another program for
// longer than heldRetry) keeps the last good list too and is tried again at the next look, whether
// or not the file changed.
//
// helm repo add rewrites the file in place (truncated, then written), so a file seen changing is
// read only once its size and modification time stayed the same for a moment, and its list is
// taken only when they are still the same after reading: a half-written file is read on the next
// look (except on the first read, which takes what there is).
func (h *HelmRepos) Sync() (bool, error) {
	h.syncMu.Lock()
	defer h.syncMu.Unlock()
	sig := h.fileSignature()
	h.mu.Lock()
	if h.loaded && sig == h.sig {
		h.mu.Unlock()
		return false, nil
	}
	first := !h.loaded
	initial := !h.parsed // the first list read: the indexes of an earlier run count
	h.mu.Unlock()
	if !first {
		if h.settling != nil {
			h.settling() // tests: the file changes while Cogpit waits
		}
		time.Sleep(h.settle())
		if h.fileSignature() != sig {
			return false, nil // still being written
		}
	}

	entries, err := h.readFile()
	if !first && h.fileSignature() != sig {
		return false, nil // written while it was read
	}
	// A read that failed to open or read the file, or did not find the file stat found, did not
	// reach it: its signature is not kept, so the next look reads the file again.
	reached := !errors.As(err, new(*fs.PathError)) || (sig == missingSignature && errors.Is(err, fs.ErrNotExist))
	status, fileErr := HelmFileOK, ""
	switch {
	case errors.Is(err, fs.ErrNotExist):
		status, entries, err = HelmFileMissing, nil, nil
	case err != nil:
		status, fileErr = HelmFileError, err.Error()
	}
	var disk map[string]diskIndex
	if initial && err == nil {
		disk = h.readCachedIndexes(entries)
	}

	h.mu.Lock()
	h.sig, h.loaded = sig, true
	if !reached {
		h.sig = ""
	}
	changed := h.fileStatus != status || h.fileErr != fileErr
	h.fileStatus, h.fileErr = status, fileErr
	if err == nil {
		if h.apply(entries, disk) {
			changed = true
		}
		h.parsed = true
	}
	h.mu.Unlock()
	if err != nil && changed {
		h.logger().Warn("helm: Helm's repositories file was not read; the repositories stay as they were", "err", err)
	}
	return changed, nil
}

// readFile reads Helm's file. On Windows a read fails while another program holds the file open
// without sharing it (the moment a program renames it into place, a virus scanner); it runs again
// for up to heldRetry then, as Cogpit's own renames do (retryHeld).
func (h *HelmRepos) readFile() ([]helm.RepositoryEntry, error) {
	var entries []helm.RepositoryEntry
	err := retryHeld(func() error {
		var rerr error
		entries, rerr = h.read(h.file)
		return rerr
	})
	return entries, err
}

// diskIndex is an index Cogpit downloaded in an earlier run, read from its cache.
type diskIndex struct {
	index   *helm.Index
	updated time.Time
	err     string
}

// readCachedIndexes reads the indexes an earlier run downloaded for the repositories of the first
// read of Helm's file, by their cache key.
func (h *HelmRepos) readCachedIndexes(entries []helm.RepositoryEntry) map[string]diskIndex {
	out := map[string]diskIndex{}
	for _, e := range entries {
		if e.Repository == nil {
			continue
		}
		path := h.indexPath(e.Repository.CacheKey())
		fi, err := os.Stat(path)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				out[e.Name] = diskIndex{err: err.Error()}
			}
			continue
		}
		idx, err := helm.LoadIndex(path)
		if err != nil {
			out[e.Name] = diskIndex{err: err.Error()}
			continue
		}
		out[e.Name] = diskIndex{index: idx, updated: fi.ModTime()}
	}
	return out
}

// apply takes over the entries of Helm's file. disk holds the cached indexes of the first read
// (nil afterwards: a repository that appears later counts as never downloaded). The caller holds
// mu. It reports whether anything shown changed.
func (h *HelmRepos) apply(entries []helm.RepositoryEntry, disk map[string]diskIndex) bool {
	next := make([]repoEntry, 0, len(entries))
	state := map[string]*repoState{}
	for _, e := range entries {
		info := HelmRepoInfo{
			Name: e.Name, URL: e.URL, HasCredentials: e.HasCredentials, ClientCertificate: e.ClientCertificate,
			CAFile: e.CAFile, InsecureSkipTLSVerify: e.InsecureSkipTLSVerify, PassCredentialsAll: e.PassCredentialsAll,
			PlainHTTP: e.PlainHTTP,
		}
		if e.Repository == nil {
			info.Unused = true
			if e.Err != nil {
				info.Error = e.Err.Error()
			}
			next = append(next, repoEntry{info: info})
			continue
		}
		next = append(next, repoEntry{info: info, repo: e.Repository})
		r := e.Repository
		key, fp := r.CacheKey(), r.Fingerprint()
		old := h.state[e.Name]
		switch {
		case old != nil && old.fp == fp:
			old.repo = r
			state[e.Name] = old
		case old != nil && old.key == key: // the credentials or TLS settings changed: download again
			if old.cancel != nil {
				old.cancel()
			}
			st := *old
			st.repo, st.fp, st.fetchNow, st.err, st.cancel, st.updating = r, fp, true, "", nil, false
			h.gen++
			st.gen = h.gen // its charts' objects again, with the origin the change may have changed (H-41)
			state[e.Name] = &st
		default: // new, or its URL changed
			if old != nil && old.cancel != nil {
				old.cancel()
			}
			h.gen++
			st := &repoState{repo: r, key: key, fp: fp, since: h.gen, gen: h.gen, fetchNow: disk == nil}
			if d, ok := disk[e.Name]; ok {
				st.index, st.updated, st.err = d.index, d.updated, d.err
			}
			state[e.Name] = st
		}
	}
	for name, old := range h.state {
		if _, ok := state[name]; !ok && old.cancel != nil {
			old.cancel() // removed from Helm's file: its download stops
		}
	}
	changed := len(next) != len(h.entries) || len(state) != len(h.state)
	if !changed {
		for i := range next {
			if next[i].info != h.entries[i].info {
				changed = true
				break
			}
		}
	}
	if !changed {
		for name, st := range state {
			if h.state[name] != st {
				changed = true
				break
			}
		}
	}
	h.entries, h.state = next, state
	return changed
}

// ensureLoaded reads Helm's file once.
func (h *HelmRepos) ensureLoaded() error {
	h.mu.Lock()
	loaded := h.loaded
	h.mu.Unlock()
	if loaded {
		return nil
	}
	_, err := h.Sync()
	return err
}

// List returns Helm's repositories file, its status and its repositories in its order.
func (h *HelmRepos) List() (HelmReposList, error) {
	if err := h.ensureLoaded(); err != nil {
		return HelmReposList{}, err
	}
	h.mu.Lock()
	out := HelmReposList{File: h.file, Status: h.fileStatus, Error: h.fileErr}
	h.mu.Unlock()
	out.Repositories = h.infos(nil)
	if len(out.Repositories) == 0 && out.Status != HelmFileError {
		out.Hint = helmFileHint
	}
	return out, nil
}

// infos returns the repositories (only those named, if names is not nil), with what the user
// allowed of their credentials over plain http (helmhttp.go).
func (h *HelmRepos) infos(names []string) []HelmRepoInfo {
	out, repos := h.infosLocked(names)
	h.withHTTPState(out, repos)
	return out
}

func (h *HelmRepos) infosLocked(names []string) ([]HelmRepoInfo, map[string]*helm.Repository) {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]HelmRepoInfo, 0, len(h.entries))
	repos := map[string]*helm.Repository{}
	for _, e := range h.entries {
		if names != nil && !slices.Contains(names, e.info.Name) {
			continue
		}
		info := e.info
		if st := h.state[info.Name]; st != nil && e.repo != nil {
			info.Updated, info.Error, info.Updating = st.updated, st.err, st.updating
			if st.index != nil {
				info.Charts = len(st.index.Charts)
			}
			repos[info.Name] = e.repo
		}
		out = append(out, info)
	}
	return out, repos
}

// repo returns a repository Cogpit uses, by name, or why it does not use it.
func (h *HelmRepos) repo(name string) (*helm.Repository, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, e := range h.entries {
		if e.info.Name != name {
			continue
		}
		if e.repo == nil {
			return nil, fmt.Errorf("the Helm repository %s: %s", name, e.info.Error)
		}
		return e.repo, nil
	}
	return nil, fmt.Errorf("no Helm repository named %s in Helm's repositories file", name)
}

// Origin returns how a repository Cogpit uses reaches it when nothing authenticates its content
// (helm.Repository.Origin, H-41): kinds.HelmOriginHTTP, kinds.HelmOriginInsecureTLS, or "" (also
// for a repository it does not use).
func (h *HelmRepos) Origin(name string) string {
	if h == nil {
		return ""
	}
	r, err := h.repo(name)
	if err != nil {
		return ""
	}
	return r.Origin()
}

// Update reads Helm's file when it changed and downloads the indexes of the named repositories
// (all it uses when names is empty), one after the other. A repository whose download fails keeps
// the index it had, and its error is in the result; the returned error joins the failures.
func (h *HelmRepos) Update(ctx context.Context, names []string) ([]HelmRepoInfo, error) {
	if changed, err := h.Sync(); err != nil {
		return nil, err
	} else if changed {
		h.changed()
	}
	h.mu.Lock()
	var targets []string
	for _, e := range h.entries {
		if e.repo != nil && (len(names) == 0 || slices.Contains(names, e.info.Name)) {
			targets = append(targets, e.info.Name)
		}
	}
	h.mu.Unlock()
	for _, n := range names {
		if !slices.Contains(targets, n) {
			if _, err := h.repo(n); err != nil {
				return nil, err
			}
		}
	}
	h.fetchMu.Lock()
	var errs []error
	for _, name := range targets {
		if err := h.download(ctx, name); err != nil {
			errs = append(errs, fmt.Errorf("repository %s: %w", name, err))
		}
	}
	h.fetchMu.Unlock()
	h.publish()
	return h.infos(targets), errors.Join(errs...)
}

// errRepoChanged ends a download whose repository left Helm's file or changed meanwhile.
var errRepoChanged = errors.New("the repository changed in Helm's repositories file while its index downloaded; it is downloaded again")

// download downloads one repository's index into Cogpit's cache. The caller holds fetchMu. An
// index whose repository changed or left Helm's file while it downloaded is dropped.
func (h *HelmRepos) download(ctx context.Context, name string) error {
	h.mu.Lock()
	st := h.state[name]
	if st == nil {
		h.mu.Unlock()
		return fmt.Errorf("no Helm repository named %s in Helm's repositories file", name)
	}
	r, key, fp := st.repo, st.key, st.fp
	h.mu.Unlock()
	r, err := h.forDownload(r) // credentials over plain http only once the user allowed it (H-40)
	if err != nil {
		return err
	}
	h.mu.Lock()
	if st = h.state[name]; st == nil || st.fp != fp {
		h.mu.Unlock()
		return errRepoChanged
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	st.updating, st.cancel = true, cancel
	h.mu.Unlock()
	h.changed()

	idx, err := h.fetcher.DownloadIndex(ctx, r, h.indexPath(key))

	h.mu.Lock()
	cur := h.state[name]
	if cur != nil && cur.fp == fp {
		cur.updating, cur.cancel = false, nil
		if err != nil {
			cur.err = err.Error()
		} else {
			h.gen++
			cur.index, cur.updated, cur.err, cur.gen, cur.fetchNow = idx, h.now(), "", h.gen, false
		}
	}
	h.mu.Unlock()
	h.changed()
	if cur == nil || cur.fp != fp {
		return errRepoChanged
	}
	return err
}

// refreshStale downloads, while the Charts view or a Helm Install tab is open (architecture §5),
// the indexes older than their time-to-live, never downloaded, or of a repository that appeared or
// changed since Cogpit started.
func (h *HelmRepos) refreshStale(ctx context.Context) {
	if err := h.ensureLoaded(); err != nil {
		h.logger().Warn("helm: repositories", "err", err)
		return
	}
	h.fetchMu.Lock()
	defer h.fetchMu.Unlock()
	approvals, aErr := h.http.load()
	h.mu.Lock()
	var stale []string
	for _, e := range h.entries {
		st := h.state[e.info.Name]
		if e.repo != nil && e.repo.CredentialsOverHTTP() && (aErr != nil || waitsForHTTP(approvals, e.repo)) {
			continue // waits for the user's allowance; the Charts view and helm.repo.list say why (H-40)
		}
		if e.repo != nil && st != nil && (st.fetchNow || st.updated.IsZero() || h.now().Sub(st.updated) >= h.ttl) {
			stale = append(stale, e.info.Name)
		}
	}
	h.mu.Unlock()
	changed := false
	for _, name := range stale {
		if ctx.Err() != nil {
			break
		}
		if err := h.download(ctx, name); err != nil {
			if !errors.Is(err, errRepoChanged) {
				h.logger().Warn("helm: the repository's index was not refreshed", "repository", name, "err", err)
			}
			continue
		}
		changed = true
	}
	if changed {
		h.publish()
	}
}

// indexOf returns the index Cogpit holds of a repository it uses.
func (h *HelmRepos) indexOf(name string) *helm.Index {
	h.mu.Lock()
	defer h.mu.Unlock()
	if st := h.state[name]; st != nil {
		return st.index
	}
	return nil
}

// ChartVersion finds a chart's version in a repository's index: version "" is the version the
// chart is listed with (the newest that is not a prerelease). It returns the repository too.
func (h *HelmRepos) ChartVersion(repository, chart, version string) (*helm.Repository, helm.IndexChart, helm.ChartVersion, error) {
	if err := h.ensureLoaded(); err != nil {
		return nil, helm.IndexChart{}, helm.ChartVersion{}, err
	}
	r, err := h.repo(repository)
	if err != nil {
		return nil, helm.IndexChart{}, helm.ChartVersion{}, err
	}
	c, ok := h.indexOf(repository).Chart(chart)
	if !ok {
		return r, helm.IndexChart{}, helm.ChartVersion{}, fmt.Errorf("repository %s has no chart %s (helm_repo_update downloads its index again)", repository, chart)
	}
	var v helm.ChartVersion
	if version == "" {
		v, ok = c.Latest()
	} else {
		v, ok = c.Version(version)
	}
	if !ok {
		return r, c, helm.ChartVersion{}, fmt.Errorf("chart %s/%s has no version %s", repository, chart, version)
	}
	return r, c, v, nil
}

// FetchChart downloads a chart version from its repository, checked against the index's digest,
// with the repository's credentials as Helm's file holds them now.
func (h *HelmRepos) FetchChart(ctx context.Context, repository, chart, version string) (*helm.Chart, helm.IndexChart, error) {
	r, c, v, err := h.ChartVersion(repository, chart, version)
	if err != nil {
		return nil, c, err
	}
	if r, err = h.forDownload(r); err != nil { // credentials over plain http only once the user allowed it (H-40)
		return nil, c, err
	}
	ch, err := h.fetcher.FetchChart(ctx, r, chart, v)
	return ch, c, err
}

func (h *HelmRepos) logger() *slog.Logger {
	h.mu.Lock()
	s := h.s
	h.mu.Unlock()
	if s != nil {
		return s.log
	}
	return slog.Default()
}

// changed tells the window that the repositories' list or a download's state changed
// (EventHelmRepos); the Repositories dialog reads helm.repo.list again.
func (h *HelmRepos) changed() {
	h.mu.Lock()
	s := h.s
	h.mu.Unlock()
	if s != nil {
		s.emit(Event{Type: EventHelmRepos})
	}
}

// attach binds the repositories to their session: Helm's file is watched for as long as the
// session runs, the connected backend gets the charts, and the indexes are refreshed while the
// Charts view or a Helm Install tab is open. Nil-safe.
func (h *HelmRepos) attach(s *Session) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.s = s
	h.mu.Unlock()
	if h.fetcher != nil && h.fetcher.Proxy == nil {
		h.fetcher.Proxy = s.NonClusterProxy // the Preferences' proxy, else the environment's (proxy.go)
	}
	if h.icons.fetcher != nil && h.icons.fetcher.Proxy == nil {
		h.icons.fetcher.Proxy = s.NonClusterProxy // the icons' too (D-46)
	}
	s.Subscribe(h.onEvent)
	go h.watchFile(s)
}

// watchFile looks at Helm's repositories file every fileEvery (local file I/O only, no API server)
// until the session closes: a change is listed, published to the connected backend's charts and
// wakes the index poller, so that a new repository's index downloads at once while shown.
func (h *HelmRepos) watchFile(s *Session) {
	defer s.recoverTo("watching Helm's repositories file")
	t := time.NewTicker(h.fileEvery)
	defer t.Stop()
	for {
		if s.closed() {
			return
		}
		if changed, err := h.Sync(); err != nil {
			s.log.Warn("helm: Helm's repositories file", "err", err)
		} else if changed {
			h.publish()
			signal(h.kick)
			h.changed()
		}
		<-t.C
	}
}

// onEvent runs on the goroutine that changed the UI state: it must not block.
func (h *HelmRepos) onEvent(ev Event) {
	if ev.Type != EventUIState || ev.UIState == nil {
		return
	}
	u := ev.UIState
	h.mu.Lock()
	if u.Revision <= h.lastRev {
		h.mu.Unlock()
		return
	}
	h.lastRev = u.Revision
	s := h.s
	wanted := u.Connection.State.Live() && (u.View == kinds.HelmChartsKey || slices.ContainsFunc(u.Dock, func(t DockTab) bool { return t.Type == DockHelmInstall }))
	var stop chan struct{}
	start := false
	switch {
	case wanted && h.poll == nil:
		h.poll, start = make(chan struct{}), true
		stop = h.poll
	case !wanted && h.poll != nil:
		close(h.poll)
		h.poll = nil
	}
	h.mu.Unlock()
	if s == nil {
		return
	}
	if b := s.Backend(); b != nil && u.Connection.State.Live() && h.needsPublish(b) {
		s.goSafe("Helm charts", func() {
			if err := h.ensureLoaded(); err != nil {
				s.log.Warn("helm: repositories", "err", err)
			}
			h.publish()
		})
	}
	if start {
		go h.pollLoop(s, stop)
	}
}

// pollLoop refreshes stale indexes at once, then every pollEvery and whenever Helm's file
// changed, until stop is closed or the session ends. It is not one of the session's goroutines
// that Close waits for: a download it started ends with its own timeout.
func (h *HelmRepos) pollLoop(s *Session, stop chan struct{}) {
	defer s.recoverTo("Helm repository indexes")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	t := time.NewTicker(h.pollEvery)
	defer t.Stop()
	for {
		if s.isClosed() {
			return
		}
		h.refreshStale(ctx)
		select {
		case <-stop:
			return
		case <-t.C:
		case <-h.kick:
		}
	}
}

// needsPublish reports whether b has not been given the charts yet.
func (h *HelmRepos) needsPublish(b *Backend) bool {
	h.pubMu.Lock()
	defer h.pubMu.Unlock()
	return h.pubTarget != b
}

// publish writes the charts of every downloaded index into the connected backend's local store as
// the local kind helmcharts: objects whose index changed are replaced, those of a removed
// repository or chart deleted. A backend without the kind (the demo cluster) gets none.
func (h *HelmRepos) publish() {
	h.mu.Lock()
	s := h.s
	h.mu.Unlock()
	if s == nil {
		return
	}
	b := s.Backend()
	h.pubMu.Lock()
	defer h.pubMu.Unlock()
	if b == nil {
		h.pubTarget, h.published = nil, nil
		return
	}
	p, ok := b.Profile(kinds.HelmChartsKey)
	if !ok || !p.Local {
		h.pubTarget, h.published = b, map[string]string{}
		return
	}
	if h.pubTarget != b {
		h.pubTarget, h.published = b, map[string]string{}
	}
	want := h.chartObjects()
	for name, obj := range want {
		if h.published[name] == obj.GetResourceVersion() {
			continue
		}
		if err := b.PutLocal(p, obj); err != nil {
			s.log.Warn("helm: a chart was not listed", "chart", name, "err", err)
			continue
		}
		h.published[name] = obj.GetResourceVersion()
	}
	for name := range h.published {
		if _, ok := want[name]; !ok {
			_ = b.DeleteLocal(p, "", name)
			delete(h.published, name)
		}
	}
}

// chartObjects makes the charts' objects from the downloaded indexes, by object name.
func (h *HelmRepos) chartObjects() map[string]*unstructured.Unstructured {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]*unstructured.Unstructured{}
	for _, e := range h.entries {
		st := h.state[e.info.Name]
		if e.repo == nil || st == nil || st.index == nil {
			continue
		}
		rv := strconv.FormatUint(st.gen, 10)
		for _, c := range st.index.Charts {
			latest, ok := c.Latest()
			if !ok {
				continue
			}
			obj := kinds.HelmChartObject(helmChartAt(e.info.Name, e.repo.Origin(), c, latest), chartUID(e.info.Name, st, c.Name), rv)
			out[obj.GetName()] = obj
		}
	}
	return out
}

// helmChartAt is a chart of a repository's index as its object shows it at version v: v's
// properties, with every version of the chart, and the repository's origin when nothing
// authenticates it (H-41, A14).
func helmChartAt(repository, origin string, c helm.IndexChart, v helm.ChartVersion) kinds.HelmChart {
	kc := kinds.HelmChart{
		Repository: repository, Name: c.Name, Version: v.Version, AppVersion: v.AppVersion,
		Description: v.Description, Home: v.Home, Icon: v.Icon, KubeVersion: v.KubeVersion,
		Digest: v.Digest, Keywords: v.Keywords, Maintainers: v.Maintainers, Sources: v.Sources,
		Deprecated: v.Deprecated, Created: v.Created, Origin: origin,
	}
	for _, cv := range c.Versions {
		kc.Versions = append(kc.Versions, kinds.HelmChartVersion{Version: cv.Version, AppVersion: cv.AppVersion, Created: cv.Created})
	}
	return kc
}

// chartUID names a chart for as long as its repository is in Helm's file with the same name and
// URL: a repository removed and added again, or given another URL, gives its charts new UIDs.
func chartUID(repository string, st *repoState, chart string) string {
	sum := sha256.Sum256([]byte(repository + "\x00" + st.key + "\x00" + strconv.FormatUint(st.since, 10) + "\x00" + chart))
	return hex.EncodeToString(sum[:16])
}

// HelmChartRef is a chart's reference in the Charts view.
func HelmChartRef(context, repository, chart string) model.ObjectRef {
	return model.ObjectRef{Context: context, Kind: kinds.HelmChartsKey, Name: kinds.HelmChartName(repository, chart)}
}
