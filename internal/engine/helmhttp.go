package engine

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/guided-traffic/cogpit/internal/helm"
)

// A Helm repository's user and password over plain http (credentials H-40, D-29; owner): a
// repository of Helm's file whose URL is http:// and that has basic auth downloads nothing until
// the user allowed, once, that its credentials cross the network unencrypted. Then it is used as
// helm uses it, the background refresh included. The allowance is kept like a kubeconfig credential
// plugin's (kubeconfigs_approvals.go): in a file of Cogpit's own, owner-only, bound to the
// repository's name, its URL and its user name (helm.Repository.HTTPBinding); a changed URL or user
// name asks again, a changed password does not. Only the user's own action in Cogpit's window gives
// it (helm.repo.allowHTTPCredentials is GUI-only, D-2): an agent never allows the requests it would
// make, and it reads why a repository is not downloaded in helm.repo.list. A repository without
// credentials over http needs no allowance (only trust-boundaries H-41 applies to it). The helm
// package refuses such a download by itself (helm.ErrCredentialsOverHTTP); the engine hands it a
// copy the user allowed.

// HelmHTTPApprovalsFile holds the allowances, under Cogpit's configuration directory.
const HelmHTTPApprovalsFile = "helm-http-approvals.json"

// States of a repository whose credentials would go over plain http (HelmRepoInfo.HTTPCredentials).
const (
	// HTTPCredentialsRequired: the user has not allowed it; nothing is downloaded.
	HTTPCredentialsRequired = "required"
	// HTTPCredentialsChanged: the user allowed it for another URL or user name; nothing is
	// downloaded until the user allows it again.
	HTTPCredentialsChanged = "changed"
	// HTTPCredentialsAllowed: the user allowed it; downloads run as helm's do.
	HTTPCredentialsAllowed = "allowed"
)

// httpApprovalsVersion is the format of the allowances file.
const httpApprovalsVersion = 1

// ErrHelmHTTPGUIOnly refuses an agent or the CLI the allowance to send a repository's credentials
// over plain http.
var ErrHelmHTTPGUIOnly = errors.New("only the user, in Cogpit's own window, can let Cogpit send a Helm repository's " +
	"user name and password over plain http; agents and the CLI are refused, so that no agent allows its own requests " +
	"(credentials H-40)")

// ErrHelmHTTPChangedSinceShown refuses an allowance whose token is not the repository's current
// one: its URL or user name changed in Helm's file, or it left the file, while the user looked.
// Nothing was allowed. Match it with errors.Is.
var ErrHelmHTTPChangedSinceShown = errors.New("the repository changed in Helm's repositories file since it was shown; " +
	"nothing was allowed; review it again")

// HTTPCredentialsError is a download refused because the user has not allowed the repository's
// credentials over plain http (or allowed them for another URL or user). It matches
// helm.ErrCredentialsOverHTTP with errors.Is.
type HTTPCredentialsError struct {
	Repository string
	State      string // HTTPCredentialsRequired or HTTPCredentialsChanged
}

func (e *HTTPCredentialsError) Error() string {
	why := "Cogpit sends them only once you allow it, in Cogpit's window (the Charts view asks)"
	if e.State == HTTPCredentialsChanged {
		why = "you allowed it for another URL or user name of this repository, so Cogpit asks again, in its window (the Charts view asks)"
	}
	return "the Helm repository " + e.Repository + " sends its user name and password over plain http, unencrypted; " +
		why + "; nothing is downloaded from it until then, and agents cannot allow it"
}

// Is makes errors.Is(err, helm.ErrCredentialsOverHTTP) true.
func (e *HTTPCredentialsError) Is(target error) bool { return target == helm.ErrCredentialsOverHTTP }

// HelmHTTPApproval is one allowance as the file keeps it: the repository's name and its URL as
// Cogpit shows it (no user, password or query values), what it binds (no credential), and when.
type HelmHTTPApproval struct {
	Name     string    `json:"name"`
	URL      string    `json:"url"`
	Binding  string    `json:"binding"`
	Approved time.Time `json:"approved"`
}

type helmHTTPDoc struct {
	Version   int                `json:"version"`
	Approvals []HelmHTTPApproval `json:"approvals"`
}

// HelmHTTPAllowItem is one repository the user allows: its name and the token helm.repo.list
// showed for it (HelmRepoInfo.HTTPToken).
type HelmHTTPAllowItem struct {
	Name  string `json:"name" jsonschema:"the repository's name in Helm's repositories file"`
	Token string `json:"token" jsonschema:"the repository's httpToken as helm_repo_list returned it"`
}

// httpApprovals keeps the allowances: in path (owner-only), or in memory for the session when path
// is empty (no configuration directory; tests). It is safe for concurrent use.
type httpApprovals struct {
	path string
	mu   sync.Mutex
	mem  []HelmHTTPApproval
	salt [32]byte // makes the tokens of this process; never leaves it
}

func newHTTPApprovals(path string) *httpApprovals {
	a := &httpApprovals{path: path}
	if _, err := rand.Read(a.salt[:]); err != nil {
		panic("helm: no randomness for the repositories' tokens: " + err.Error()) // crypto/rand does not fail on supported systems
	}
	return a
}

// WithHTTPApprovals keeps the user's allowances of credentials over plain http in path (Cogpit's
// configuration directory, HelmHTTPApprovalsFile; app.Dirs.HelmHTTPApprovalsPath). Without it they
// last for the session.
func WithHTTPApprovals(path string) HelmReposOption {
	return func(h *HelmRepos) { h.http = newHTTPApprovals(path) }
}

// load returns the allowances. An unreadable file allows nothing and says why.
func (a *httpApprovals) load() ([]HelmHTTPApproval, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.loadLocked()
}

func (a *httpApprovals) loadLocked() ([]HelmHTTPApproval, error) {
	if a.path == "" {
		return slices.Clone(a.mem), nil
	}
	var doc helmHTTPDoc
	if err := readPrivateJSON(a.path, maxPrivateDoc, &doc); err != nil {
		return nil, fmt.Errorf("the file of Helm repositories allowed over plain http, %s, is unreadable (%v); Cogpit "+
			"sends no credentials over http until it is fixed or moved aside", a.path, err)
	}
	if doc.Version != 0 && doc.Version != httpApprovalsVersion {
		return nil, fmt.Errorf("the file of Helm repositories allowed over plain http, %s, has format version %d, this "+
			"Cogpit reads %d", a.path, doc.Version, httpApprovalsVersion)
	}
	return doc.Approvals, nil
}

// allow records the allowances of rs, each replacing an earlier one of its name, in one write.
func (a *httpApprovals) allow(rs []*helm.Repository, now time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	list, err := a.loadLocked()
	if err != nil {
		return err
	}
	for _, r := range rs {
		list = slices.DeleteFunc(list, func(x HelmHTTPApproval) bool { return x.Name == r.Name })
		list = append(list, HelmHTTPApproval{Name: r.Name, URL: helm.RedactURL(r.URL), Binding: r.HTTPBinding(), Approved: now.UTC()})
	}
	if a.path == "" {
		a.mem = list
		return nil
	}
	data, err := json.MarshalIndent(helmHTTPDoc{Version: httpApprovalsVersion, Approvals: list}, "", "  ")
	if err != nil {
		return err
	}
	if err := writePrivateFile(a.path, append(data, '\n')); err != nil {
		return fmt.Errorf("the file of Helm repositories allowed over plain http: %w", err)
	}
	return nil
}

// token is what the window hands back to allow r: it changes with r's binding and is
// meaningless outside this process (it reveals nothing about the user name).
func (a *httpApprovals) token(r *helm.Repository) string {
	sum := sha256.Sum256(append(a.salt[:], r.HTTPBinding()...))
	return hex.EncodeToString(sum[:16])
}

// httpState is r's state against the allowances: "" for a repository that sends no credentials
// over plain http.
func httpState(approvals []HelmHTTPApproval, r *helm.Repository) string {
	if r == nil || !r.CredentialsOverHTTP() {
		return ""
	}
	state := HTTPCredentialsRequired
	binding := r.HTTPBinding()
	for _, x := range approvals {
		if x.Name != r.Name {
			continue
		}
		if x.Binding == binding {
			return HTTPCredentialsAllowed
		}
		state = HTTPCredentialsChanged
	}
	return state
}

// forDownload returns the repository a download may use: r itself when it sends no credentials
// over plain http, a copy that may send them when the user allowed it, else an
// *HTTPCredentialsError (or why the allowances could not be read).
func (h *HelmRepos) forDownload(r *helm.Repository) (*helm.Repository, error) {
	if !r.CredentialsOverHTTP() {
		return r, nil
	}
	approvals, err := h.http.load()
	if err != nil {
		return nil, err
	}
	if state := httpState(approvals, r); state != HTTPCredentialsAllowed {
		return nil, &HTTPCredentialsError{Repository: r.Name, State: state}
	}
	return r.AllowCredentialsOverHTTP(), nil
}

// withHTTPState adds to infos what the allowances say about each repository that would send its
// credentials over plain http: its state, its token while it waits for the user, and why nothing is
// downloaded from it. repos are the repositories Cogpit uses, by name.
func (h *HelmRepos) withHTTPState(infos []HelmRepoInfo, repos map[string]*helm.Repository) {
	var approvals []HelmHTTPApproval
	var aErr error
	loaded := false
	for i := range infos {
		r := repos[infos[i].Name]
		if r == nil || !r.CredentialsOverHTTP() {
			continue
		}
		if !loaded {
			approvals, aErr = h.http.load()
			loaded = true
		}
		info := &infos[i]
		if aErr != nil {
			info.HTTPCredentials, info.Error = HTTPCredentialsRequired, aErr.Error()
			continue
		}
		info.HTTPCredentials = httpState(approvals, r)
		if info.HTTPCredentials != HTTPCredentialsAllowed {
			info.HTTPToken = h.http.token(r)
			info.Error = (&HTTPCredentialsError{Repository: r.Name, State: info.HTTPCredentials}).Error()
		}
	}
}

// waitsForHTTP reports whether r would send its credentials over plain http and the user has not
// allowed it: the background refresh leaves it alone (the Charts view says why).
func waitsForHTTP(approvals []HelmHTTPApproval, r *helm.Repository) bool {
	s := httpState(approvals, r)
	return s != "" && s != HTTPCredentialsAllowed
}

// AllowHTTP records the user's allowance to send the listed repositories' credentials over plain
// http, each provided it is still the repository the window showed (its token). All or none: a
// repository that left Helm's file, changed its URL or user name, or sends no credentials over
// http refuses the whole call and nothing is recorded. The repositories then download at once
// while the Charts view or a Helm Install tab is open. Only the GUI's command calls it
// (helm.repo.allowHTTPCredentials, D-2).
func (h *HelmRepos) AllowHTTP(items []HelmHTTPAllowItem) ([]HelmRepoInfo, error) {
	if len(items) == 0 {
		return nil, errors.New("no Helm repository to allow")
	}
	if err := h.ensureLoaded(); err != nil {
		return nil, err
	}
	h.mu.Lock()
	byName := map[string]*helm.Repository{}
	for _, e := range h.entries {
		if e.repo != nil {
			byName[e.info.Name] = e.repo
		}
	}
	h.mu.Unlock()
	var allow []*helm.Repository
	var changed, names []string
	seen := map[string]bool{}
	for _, it := range items {
		if seen[it.Name] {
			return nil, fmt.Errorf("the repository %q is listed twice", it.Name)
		}
		seen[it.Name] = true
		r := byName[it.Name]
		switch {
		case r == nil:
			changed = append(changed, it.Name)
		case !r.CredentialsOverHTTP():
			return nil, fmt.Errorf("the Helm repository %s sends no user name and password over plain http: there is nothing to allow", it.Name)
		case h.http.token(r) != it.Token:
			changed = append(changed, it.Name)
		default:
			allow = append(allow, r)
			names = append(names, it.Name)
		}
	}
	if len(changed) > 0 {
		return nil, fmt.Errorf("%w (%s)", ErrHelmHTTPChangedSinceShown, strings.Join(changed, ", "))
	}
	if err := h.http.allow(allow, h.now()); err != nil {
		return nil, err
	}
	h.mu.Lock()
	for _, n := range names {
		if st := h.state[n]; st != nil {
			st.fetchNow, st.err = true, ""
		}
	}
	h.mu.Unlock()
	h.changed()
	signal(h.kick) // downloads at once while the Charts view or a Helm Install tab is open
	return h.infos(names), nil
}
