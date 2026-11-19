package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// Kubeconfig sources (G2, docs/developer/architecture.md §6): kubeconfig files the user adds,
// and sync folders whose kubeconfig files Cogpit picks up as they appear. Each file is read on its own, never merged with another, so
// a context in one file can never borrow the user or cluster entry of another (a dropped file
// naming the user's real user entry with its own server). An exec credential plugin of a source
// runs only after the user allowed it in the GUI (trust-boundaries H-1; ExecApproval).
//
// Cogpit reads the files the user adds and the files in sync folders; it never writes them. It
// writes only its own files under its configuration directory: the list of sources and the
// approvals, each owner-only.

// Files and directories under Cogpit's configuration directory.
const (
	// KubeconfigSourcesFile lists the sources the user added.
	KubeconfigSourcesFile = "kubeconfigs.json"
	// ExecApprovalsFile holds the credential plugins the user allowed.
	ExecApprovalsFile = "exec-approvals.json"
	// DefaultKubeconfigPoll is how often WatchKubeconfigs looks at the sources for changes.
	DefaultKubeconfigPoll = 2 * time.Second
)

// legacyPastedKind is the kind of the sources earlier builds wrote for pasted kubeconfigs.
const legacyPastedKind SourceKind = "pasted"

// SourceKind is what a kubeconfig source is.
type SourceKind string

// Source kinds.
const (
	SourceFile   SourceKind = "file"   // a kubeconfig file the user added; read, never written
	SourceFolder SourceKind = "folder" // a sync folder: every kubeconfig file in it, as files come and go
)

// Approval states of an exec credential plugin (ContextInfo.Exec, ExecPluginInfo.State), and since
// A28 of a connection that does not check the server's certificate (kind kubeconfig.LoginInsecure).
const (
	ExecApproved = "approved" // the user allowed exactly this plugin (or unchecked connection) for this context and file
	ExecRequired = "required" // the user has not allowed it yet
	ExecChanged  = "changed"  // the user allowed it, and it changed since (command, arguments, environment, server or how it is reached)
)

// ErrExecNotApproved marks a refused connection: the context's credential plugin may run only
// after the user allowed it in Cogpit's window. Match it with errors.Is.
var ErrExecNotApproved = errors.New("the credential plugin is not allowed")

// ErrGUIOnly refuses the kubeconfig commands that only the user's own action in Cogpit's window may
// run: adding a source, removing a source, allowing a credential plugin.
var ErrGUIOnly = errors.New("only the user, in Cogpit's own window, can do this; agents and the CLI are refused " +
	"(a kubeconfig can name a program to run on this computer)")

// KubeconfigSource is one source the user added.
type KubeconfigSource struct {
	// ID names the source in context ids, unique among the sources: a file's name ("dev.yaml"),
	// or a folder's name with a slash ("kubeconfigs/").
	ID   string     `json:"id"`
	Kind SourceKind `json:"kind"`
	// Path is the file or folder, absolute.
	Path  string    `json:"path"`
	Added time.Time `json:"added"`
	// Default marks the user's own kubeconfig folder (~/.kube), which Cogpit syncs without being
	// asked, as Lens does; the user can remove it like any other source. It is not stored in the
	// list of sources; removing it is (sourcesDoc.DefaultFolderRemoved).
	Default bool `json:"default,omitempty"`
	// Session marks a file named for this run ($KUBECONFIG, Cogpit's --kubeconfig; D-28): offered
	// at start as a source of its own, read alone, removable for the session, never stored.
	Session bool `json:"session,omitempty"`
}

// ExecApproval is the user's allowance of one credential plugin for one context of one file, or
// of a connection to a server whose certificate the context does not check (Kind
// kubeconfig.LoginInsecure, A28): the same store, the same binding.
type ExecApproval struct {
	File    string `json:"file"`
	Context string `json:"context"`
	// Fingerprint binds the plugin exactly (kubeconfig.ExecPlugin.Fingerprint): a change asks again.
	Fingerprint string    `json:"fingerprint"`
	Command     string    `json:"command"`
	Approved    time.Time `json:"approved"`
	// Kind is what was allowed (kubeconfig.ExecPlugin.Kind), for whoever reads the file; the
	// fingerprint alone decides. Empty in approvals written before 2026-10-14.
	Kind string `json:"kind,omitempty"`
}

// ExecApprovalError refuses a connection whose credential plugin the user has not allowed.
type ExecApprovalError struct {
	Plugin *kubeconfig.ExecPlugin
	State  string // ExecRequired or ExecChanged
}

func (e *ExecApprovalError) Error() string {
	cmd := kubeconfig.DisplayText(e.Plugin.Command, 200)
	file := kubeconfig.DisplayText(e.Plugin.File, 400)
	if e.Plugin.Kind() == kubeconfig.LoginInsecure {
		server := kubeconfig.DisplayText(e.Plugin.Server, 200)
		if e.State == ExecChanged {
			return fmt.Sprintf("the server %s that %s names for this context, or how it is reached (its proxy), changed since you "+
				"allowed connecting to it without checking its certificate (insecure-skip-tls-verify); Cogpit connects only after "+
				"you review and allow it again: connect from the navigator's clusters in Cogpit's window (⌘P)", server, file)
		}
		return fmt.Sprintf("the kubeconfig %s switches the check of the server %s's certificate off for this context "+
			"(insecure-skip-tls-verify), so whoever answers at that address gets its credential; Cogpit connects only after "+
			"you review and allow that once: connect from the navigator's clusters in Cogpit's window (⌘P)", file, server)
	}
	if e.Plugin.Kind() == kubeconfig.LoginTokenFile {
		tok := kubeconfig.DisplayText(e.Plugin.TokenFile, 400)
		server := kubeconfig.DisplayText(e.Plugin.Server, 200)
		if e.State == ExecChanged {
			return fmt.Sprintf("the token file %s that %s names (or the server %s it goes to) changed since you allowed it; Cogpit "+
				"reads it only after you review and allow it again: connect from the navigator's clusters in Cogpit's window (⌘P)", tok, file, server)
		}
		return fmt.Sprintf("the kubeconfig %s names the file %s, outside its own folder, as the token it sends to %s, which you "+
			"have not allowed yet; Cogpit reads it only after you review and allow it: connect from the navigator's clusters in "+
			"Cogpit's window (⌘P)", file, tok, server)
	}
	if e.State == ExecChanged {
		return fmt.Sprintf("the credential plugin %s of %s changed since you allowed it; Cogpit runs it only after "+
			"you review and allow it again: connect from the navigator's clusters in Cogpit's window (⌘P)", cmd, file)
	}
	return fmt.Sprintf("the kubeconfig %s runs the program %s to log in, which you have not allowed yet; Cogpit runs it "+
		"only after you review and allow it: connect from the navigator's clusters in Cogpit's window (⌘P)", file, cmd)
}

// Is makes errors.Is(err, ErrExecNotApproved) true.
func (e *ExecApprovalError) Is(target error) bool { return target == ErrExecNotApproved }

// SourceContextID is the id of a context from a source file: "<context> [<label>]". The label
// names the file, so a context name that two files share gives two ids (D-28). Only a connector
// without sources (tests) knows bare context names as ids.
func SourceContextID(context, label string) string { return context + " [" + label + "]" }

// KubeconfigFile is one kubeconfig file of a source, read on its own.
type KubeconfigFile struct {
	Source KubeconfigSource
	// Label names the file in its contexts' ids: the source's id, or the folder's id followed by
	// the file's name ("kubeconfigs/team.yaml").
	Label string
	Path  string
	// Contexts and Exec are as cluster.InspectFile returned them; nil when Err is set.
	Contexts []kubeconfig.Context
	Exec     map[string]*kubeconfig.ExecPlugin
	Err      error
}

// SourceStatus is a source with what reading it found, for kubeconfig.sources.
type SourceStatus struct {
	KubeconfigSource
	// Files are a folder's kubeconfig files (their labels), or the source's one file.
	Files []string `json:"files,omitempty"`
	// Contexts counts the contexts the source offers.
	Contexts int `json:"contexts"`
	// Skipped counts a folder's files that are not kubeconfigs or define no context.
	Skipped int `json:"skipped,omitempty"`
	// SkippedFiles name the first of them with why, as "<file>: <reason>", the reason one of a few
	// fixed texts (never a file's content or its parser's message), so that a sync that finds no
	// cluster can say why.
	SkippedFiles []string `json:"skippedFiles,omitempty"`
	// Error says why the source (or its one file) cannot be read.
	Error string `json:"error,omitempty"`
}

// maxSkippedShown bounds the skipped files a source status names.
const maxSkippedShown = 20

// skipReason says why a folder's file offers no context, in a fixed text: a parser's message can
// quote the file, and the file can hold credentials.
func skipReason(err error) string {
	switch {
	case err == nil, errors.Is(err, kubeconfig.ErrNoContexts):
		return "defines no context"
	default:
		return "not a kubeconfig, or unreadable"
	}
}

// maxPrivateDoc bounds the sources and approvals files: they hold a few entries.
const maxPrivateDoc = 1 << 20

const sourcesVersion, approvalsVersion = 1, 1

type sourcesDoc struct {
	Version int                `json:"version"`
	Sources []KubeconfigSource `json:"sources"`
	// DefaultFolderRemoved is set once the user removed the default synced folder (~/.kube).
	DefaultFolderRemoved bool `json:"defaultFolderRemoved,omitempty"`
}

type approvalsDoc struct {
	Version   int            `json:"version"`
	Approvals []ExecApproval `json:"approvals"`
}

// KubeconfigSources keeps the sources the user added and the credential plugins the user allowed,
// in files under one directory (the app's: <user config dir>/Cogpit). It is safe for concurrent
// use; two Cogpit processes changing the files at once may lose one change (the files are not
// locked).
type KubeconfigSources struct {
	dir string
	now func() time.Time
	mu  sync.Mutex // serialises the read-modify-write of the files

	// defaultDir is the folder synced by default (~/.kube); empty: none.
	defaultDir string
	// sessionFiles are the files named for this run ($KUBECONFIG, --kubeconfig); removedSession are
	// those the user removed since (kept in memory only: they are back at the next start).
	sessionFiles   []string
	removedSession map[string]bool

	// proxies are the cluster settings whose proxy (ClusterSettings.Proxy) a context's connection
	// goes through in place of its kubeconfig's: every login described from the files binds its
	// context's (A23, D-26). Set by the connector that owns both (ClusterConnector.sources); nil:
	// no proxy of Cogpit's is known.
	proxies atomic.Pointer[ClusterSettingsStore]

	// removed is told of each source removed for good (OnRemove); nil: nobody.
	removed atomic.Pointer[func(KubeconfigSource)]
}

// OnRemove tells fn of each source removed for good, after the removal and outside the sources'
// lock, so that what the app keeps for a source outside the engine goes with it (the bookmark of
// the file or folder picked in the system's panel, cmd/cogpit). A source of this run's
// ($KUBECONFIG, --kubeconfig) is removed for the run only and not told. nil stops telling.
func (k *KubeconfigSources) OnRemove(fn func(KubeconfigSource)) {
	if fn == nil {
		k.removed.Store(nil)
		return
	}
	k.removed.Store(&fn)
}

// SourcesOption configures NewKubeconfigSources.
type SourcesOption func(*KubeconfigSources)

// WithDefaultFolder syncs dir by default, as Lens syncs ~/.kube: the user's own kubeconfig folder
// is a source from the first start on (shown as a sync folder, removable), while it exists and
// until the user removes it. Its files, config included, are read on their own like any other
// source's (D-28). The caller passes the home directory's .kube; the engine never looks up the
// home directory itself, so that tests can never read the developer's real one.
func WithDefaultFolder(dir string) SourcesOption {
	return func(k *KubeconfigSources) {
		if strings.TrimSpace(dir) == "" {
			return
		}
		if abs, err := absClean(dir); err == nil {
			k.defaultDir = abs
		}
	}
}

// WithSessionFiles offers the files the user named for this run as sources of their own (D-28):
// the files $KUBECONFIG lists and Cogpit's --kubeconfig paths (cluster.NamedKubeconfigFiles). Each
// is read alone, never merged, and removable for the session; none is stored. A file that does not
// exist or is not a regular file (/dev/null) is not offered, and neither is one a synced folder
// owns already: a file directly in ~/.kube belongs to that folder, even after the user removed
// it, so that removing the folder hides it. The caller reads the environment; the engine never
// does.
func WithSessionFiles(paths []string) SourcesOption {
	return func(k *KubeconfigSources) {
		for _, p := range paths {
			if abs, err := absClean(p); err == nil && !slices.Contains(k.sessionFiles, abs) {
				k.sessionFiles = append(k.sessionFiles, abs)
			}
		}
	}
}

// NewKubeconfigSources returns the sources kept in dir. Nothing is read or written until used.
func NewKubeconfigSources(dir string, opts ...SourcesOption) *KubeconfigSources {
	k := &KubeconfigSources{dir: dir, now: time.Now}
	for _, o := range opts {
		o(k)
	}
	return k
}

// Dir returns the directory the sources live in.
func (k *KubeconfigSources) Dir() string { return k.dir }

func (k *KubeconfigSources) sourcesPath() string   { return filepath.Join(k.dir, KubeconfigSourcesFile) }
func (k *KubeconfigSources) approvalsPath() string { return filepath.Join(k.dir, ExecApprovalsFile) }

// List returns the sources: the default synced folder first (while it exists and was not
// removed), then the others in the order they were added, then this run's session files.
func (k *KubeconfigSources) List() ([]KubeconfigSource, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	doc, err := k.loadDoc()
	if err != nil {
		return nil, err
	}
	return k.effective(doc), nil
}

// effective is the stored sources with the default folder in front, unless it is removed, absent
// or already stored as a source of its own, and the session files behind them.
func (k *KubeconfigSources) effective(doc sourcesDoc) []KubeconfigSource {
	out := doc.Sources
	if k.defaultDir != "" && !doc.DefaultFolderRemoved && !slices.ContainsFunc(out, func(s KubeconfigSource) bool {
		return s.Kind == SourceFolder && s.Path == k.defaultDir
	}) {
		if fi, err := os.Stat(k.defaultDir); err == nil && fi.IsDir() {
			def := KubeconfigSource{ID: uniqueID(doc.Sources, filepath.Base(k.defaultDir)+"/"), Kind: SourceFolder, Path: k.defaultDir, Default: true}
			out = append([]KubeconfigSource{def}, out...)
		}
	}
	return append(slices.Clone(out), k.sessionSources(out)...)
}

// sessionSources are the session files still offered after the sources in offered: not removed
// this session, existing regular files, not added as a file source, and not owned by a sync
// folder (offered, or the default folder even when the user removed it).
func (k *KubeconfigSources) sessionSources(offered []KubeconfigSource) []KubeconfigSource {
	var out []KubeconfigSource
	for _, p := range k.sessionFiles {
		if k.removedSession[p] {
			continue
		}
		if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
			continue
		}
		dir, candidate := filepath.Dir(p), kubeconfig.SyncCandidate(filepath.Base(p))
		owned := candidate && k.defaultDir != "" && dir == k.defaultDir
		for _, s := range offered {
			owned = owned || (s.Kind == SourceFile && s.Path == p) || (candidate && s.Kind == SourceFolder && s.Path == dir)
		}
		if owned {
			continue
		}
		out = append(out, KubeconfigSource{ID: uniqueID(append(slices.Clone(offered), out...), filepath.Base(p)), Kind: SourceFile, Path: p, Session: true})
	}
	return out
}

// loadDoc reads the stored sources file.
func (k *KubeconfigSources) loadDoc() (sourcesDoc, error) {
	var doc sourcesDoc
	if err := readPrivateJSON(k.sourcesPath(), maxPrivateDoc, &doc); err != nil {
		return doc, fmt.Errorf("the kubeconfig sources file %s is unreadable (%v); Cogpit leaves it as it is", k.sourcesPath(), err)
	}
	if doc.Version != 0 && doc.Version != sourcesVersion {
		return doc, fmt.Errorf("the kubeconfig sources file %s has format version %d, this Cogpit reads %d", k.sourcesPath(), doc.Version, sourcesVersion)
	}
	list := make([]KubeconfigSource, 0, len(doc.Sources))
	for _, s := range doc.Sources {
		if s.Kind == legacyPastedKind {
			// Pasting was removed (D-21). An entry written by an earlier build is no longer offered; its
			// file under kubeconfigs/ is the user's and stays on disk, and the next save of the list
			// drops the entry.
			continue
		}
		if s.ID == "" || !filepath.IsAbs(s.Path) || (s.Kind != SourceFile && s.Kind != SourceFolder) {
			return doc, fmt.Errorf("the kubeconfig sources file %s has an entry without an id, an absolute path or a known kind", k.sourcesPath())
		}
		s.Default, s.Session = false, false // never taken from the file: only the options make them
		list = append(list, s)
	}
	doc.Sources = list
	return doc, nil
}

func (k *KubeconfigSources) saveDoc(doc sourcesDoc) error {
	if doc.Sources == nil {
		doc.Sources = []KubeconfigSource{}
	}
	doc.Version = sourcesVersion
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := ensurePrivateDir(k.dir); err != nil {
		return fmt.Errorf("kubeconfig sources: %w", err)
	}
	if err := writePrivateFile(k.sourcesPath(), append(data, '\n')); err != nil {
		return fmt.Errorf("kubeconfig sources: %w", err)
	}
	return nil
}

// AddFile adds a kubeconfig file as a source: Cogpit reads it, follows its changes and never
// writes it. The file must be a kubeconfig that defines a context. It returns the source and the
// file's contexts.
func (k *KubeconfigSources) AddFile(path string) (KubeconfigSource, []kubeconfig.Context, error) {
	abs, err := absClean(path)
	if err != nil {
		return KubeconfigSource{}, nil, err
	}
	f, err := cluster.InspectFile(abs)
	if err != nil {
		return KubeconfigSource{}, nil, err
	}
	if len(f.Contexts) == 0 {
		return KubeconfigSource{}, nil, fmt.Errorf("kubeconfig file %s: %w", abs, kubeconfig.ErrNoContexts)
	}
	src, err := k.add(SourceFile, abs, filepath.Base(abs))
	return src, f.Contexts, err
}

// AddFolder adds a sync folder: every kubeconfig file in it (not in its subfolders) becomes
// available, and files that appear or disappear later are picked up or dropped.
func (k *KubeconfigSources) AddFolder(path string) (KubeconfigSource, error) {
	abs, err := absClean(path)
	if err != nil {
		return KubeconfigSource{}, err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return KubeconfigSource{}, fmt.Errorf("folder %s does not exist", abs)
		}
		return KubeconfigSource{}, fmt.Errorf("folder %s: %w", abs, err)
	}
	if !fi.IsDir() {
		return KubeconfigSource{}, fmt.Errorf("%s is not a folder; add a single file with Add Kubeconfig File", abs)
	}
	return k.add(SourceFolder, abs, filepath.Base(abs)+"/")
}

// add appends a file or folder source, unless one of the same kind and path exists.
func (k *KubeconfigSources) add(kind SourceKind, path, base string) (KubeconfigSource, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	doc, err := k.loadDoc()
	if err != nil {
		return KubeconfigSource{}, err
	}
	// A session file is not "added": adding it makes it a stored source, which replaces it.
	all := slices.DeleteFunc(k.effective(doc), func(s KubeconfigSource) bool { return s.Session })
	for _, s := range all {
		if s.Kind == kind && s.Path == path {
			return KubeconfigSource{}, fmt.Errorf("%s is added already (as %q)", path, s.ID)
		}
	}
	id := uniqueID(all, base)
	src := KubeconfigSource{ID: id, Kind: kind, Path: path, Added: k.now().UTC()}
	doc.Sources = append(doc.Sources, src)
	if err := k.saveDoc(doc); err != nil {
		return KubeconfigSource{}, err
	}
	return src, nil
}

// uniqueID returns base, or base with " (2)", " (3)", … before a trailing slash, whichever no
// source has.
func uniqueID(list []KubeconfigSource, base string) string {
	taken := func(id string) bool {
		return slices.ContainsFunc(list, func(s KubeconfigSource) bool { return s.ID == id })
	}
	if !taken(base) {
		return base
	}
	stem, slash := strings.CutSuffix(base, "/")
	for n := 2; ; n++ {
		id := stem + " (" + strconv.Itoa(n) + ")"
		if slash {
			id += "/"
		}
		if !taken(id) {
			return id
		}
	}
}

// Remove removes a source: Cogpit's link to the file or folder only. The file or folder, and the
// files in it, stay as they are. The approvals of the source's files are forgotten. Removing the
// default folder is remembered, so that it does not come back.
func (k *KubeconfigSources) Remove(id string) (KubeconfigSource, error) {
	src, gone, err := k.remove(id)
	if fn := k.removed.Load(); gone && fn != nil {
		(*fn)(src)
	}
	return src, err
}

// remove removes a source under the lock; gone reports a stored source removed for good.
func (k *KubeconfigSources) remove(id string) (src KubeconfigSource, gone bool, err error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	doc, err := k.loadDoc()
	if err != nil {
		return KubeconfigSource{}, false, err
	}
	all := k.effective(doc)
	i := slices.IndexFunc(all, func(s KubeconfigSource) bool { return s.ID == id })
	if i < 0 {
		return KubeconfigSource{}, false, fmt.Errorf("no kubeconfig source %q", id)
	}
	src = all[i]
	if src.Session {
		// Only for this run: the file is named again by the next start's $KUBECONFIG, and the user's
		// approvals of its plugins are kept for it.
		if k.removedSession == nil {
			k.removedSession = map[string]bool{}
		}
		k.removedSession[src.Path] = true
		return src, false, nil
	}
	if src.Default {
		doc.DefaultFolderRemoved = true
	} else {
		doc.Sources = slices.DeleteFunc(doc.Sources, func(s KubeconfigSource) bool { return s.ID == id })
	}
	if err := k.saveDoc(doc); err != nil {
		return KubeconfigSource{}, false, err
	}
	return src, true, k.forget(func(a ExecApproval) bool { return sourceHas(src, a.File) })
}

// sourceHas reports whether file is the source's file or a file of its folder.
func sourceHas(src KubeconfigSource, file string) bool {
	if src.Kind == SourceFolder {
		return filepath.Dir(file) == src.Path
	}
	return file == src.Path
}

// Files reads every source: one KubeconfigFile per file source, one per
// kubeconfig file in a sync folder (files that are not kubeconfigs or define no context are left
// out and counted in the statuses). The error reports a sources file that cannot be read.
func (k *KubeconfigSources) Files() ([]KubeconfigFile, []SourceStatus, error) {
	list, err := k.List()
	if err != nil {
		return nil, nil, err
	}
	var files []KubeconfigFile
	statuses := make([]SourceStatus, 0, len(list))
	for _, src := range list {
		st := SourceStatus{KubeconfigSource: src}
		switch src.Kind {
		case SourceFolder:
			entries, err := cluster.ListSyncFolder(src.Path)
			if err != nil {
				st.Error = err.Error()
				break
			}
			for _, e := range entries {
				f := inspect(src, sourceLabel(src, e.Path), e.Path)
				if f.Err != nil || len(f.Contexts) == 0 {
					st.Skipped++
					if len(st.SkippedFiles) < maxSkippedShown {
						st.SkippedFiles = append(st.SkippedFiles, filepath.Base(e.Path)+": "+skipReason(f.Err))
					}
					continue
				}
				files = append(files, f)
				st.Files = append(st.Files, f.Label)
				st.Contexts += len(f.Contexts)
			}
		default:
			f := inspect(src, sourceLabel(src, src.Path), src.Path)
			files = append(files, f)
			st.Files = []string{f.Label}
			if f.Err != nil {
				st.Error = f.Err.Error()
			}
			st.Contexts = len(f.Contexts)
		}
		statuses = append(statuses, st)
	}
	k.bindClusterProxies(files)
	return files, statuses, nil
}

// UseClusterSettings makes the logins the sources describe bind the proxy Cogpit sets for their
// context in cs (ClusterSettings.Proxy, A23): an approval then names, and is bound to, the proxy
// the connection really goes through. ClusterConnector calls it with its own settings; nil keeps
// the settings set before.
func (k *KubeconfigSources) UseClusterSettings(cs *ClusterSettingsStore) {
	if cs != nil {
		k.proxies.Store(cs)
	}
}

// bindClusterProxies records in every login of files the proxy Cogpit sets for its context, as it
// may be shown (RedactProxyURL), which the fingerprint binds.
func (k *KubeconfigSources) bindClusterProxies(files []KubeconfigFile) {
	cs := k.proxies.Load()
	if cs == nil {
		return
	}
	for _, f := range files {
		for name, p := range f.Exec {
			raw, _ := cs.proxyOf(SourceContextID(name, f.Label)) // an unreadable file is reported where it loads
			p.SetClusterProxy(RedactProxyURL(raw))
		}
	}
}

func inspect(src KubeconfigSource, label, path string) KubeconfigFile {
	f := KubeconfigFile{Source: src, Label: label, Path: path}
	in, err := cluster.InspectFile(path)
	if err != nil {
		f.Err = err
		return f
	}
	f.Contexts, f.Exec = in.Contexts, in.Exec
	return f
}

// Find returns the source file and the context name of a source context id.
func (k *KubeconfigSources) Find(id string) (KubeconfigFile, string, bool, error) {
	if !strings.HasSuffix(id, "]") {
		return KubeconfigFile{}, "", false, nil
	}
	files, _, err := k.Files()
	if err != nil {
		return KubeconfigFile{}, "", false, err
	}
	for _, f := range files {
		for _, c := range f.Contexts {
			if SourceContextID(c.Name, f.Label) == id {
				return f, c.Name, true, nil
			}
		}
	}
	return KubeconfigFile{}, "", false, nil
}

// Contexts returns the contexts of every source file as context infos, sorted by id, and an error
// naming each file that could not be read (a sync folder's files that are not kubeconfigs are not
// errors).
func (k *KubeconfigSources) Contexts() ([]ContextInfo, error) {
	files, _, err := k.Files()
	if err != nil {
		return nil, err
	}
	approvals, aErr := k.approvals()
	var out []ContextInfo
	var problems []error
	if aErr != nil {
		problems = append(problems, aErr)
	}
	for _, f := range files {
		if f.Err != nil {
			problems = append(problems, fmt.Errorf("kubeconfig source %q: %w", f.Source.ID, f.Err))
			continue
		}
		for _, c := range f.Contexts {
			info := ContextInfo{
				Name: SourceContextID(c.Name, f.Label), Cluster: c.Cluster, User: c.User, Namespace: c.Namespace,
				Server: c.Server, Source: f.Label, KubeContext: c.Name, File: f.Path, Insecure: c.Insecure,
				ClientCert: c.ClientCert,
			}
			if p := f.Exec[c.Name]; p != nil {
				info.Exec, info.ExecKind = approvalState(approvals, p), p.Kind()
			}
			out = append(out, info)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, errors.Join(problems...)
}

// approvals reads the approvals file.
func (k *KubeconfigSources) approvals() ([]ExecApproval, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.loadApprovals()
}

func (k *KubeconfigSources) loadApprovals() ([]ExecApproval, error) {
	var doc approvalsDoc
	if err := readPrivateJSON(k.approvalsPath(), maxPrivateDoc, &doc); err != nil {
		return nil, fmt.Errorf("the credential plugin approvals file %s is unreadable (%v); Cogpit allows no plugin "+
			"until it is fixed or moved aside", k.approvalsPath(), err)
	}
	if doc.Version != 0 && doc.Version != approvalsVersion {
		return nil, fmt.Errorf("the credential plugin approvals file %s has format version %d, this Cogpit reads %d",
			k.approvalsPath(), doc.Version, approvalsVersion)
	}
	return doc.Approvals, nil
}

func (k *KubeconfigSources) saveApprovals(list []ExecApproval) error {
	if list == nil {
		list = []ExecApproval{}
	}
	data, err := json.MarshalIndent(approvalsDoc{Version: approvalsVersion, Approvals: list}, "", "  ")
	if err != nil {
		return err
	}
	if err := writePrivateFile(k.approvalsPath(), append(data, '\n')); err != nil {
		return fmt.Errorf("credential plugin approvals: %w", err)
	}
	return nil
}

// approvalState is the state of plugin p against the approvals.
func approvalState(approvals []ExecApproval, p *kubeconfig.ExecPlugin) string {
	state := ExecRequired
	fp := p.Fingerprint()
	for _, a := range approvals {
		if a.File != p.File || a.Context != p.Context {
			continue
		}
		if fp != "" && a.Fingerprint == fp {
			return ExecApproved
		}
		state = ExecChanged
	}
	return state
}

// ExecState returns whether plugin p may run: ExecApproved, ExecRequired or ExecChanged. An
// unreadable approvals file allows nothing.
func (k *KubeconfigSources) ExecState(p *kubeconfig.ExecPlugin) (string, error) {
	approvals, err := k.approvals()
	if err != nil {
		return ExecRequired, err
	}
	return approvalState(approvals, p), nil
}

// Approve records the user's allowance of plugin p for its file and context, replacing an earlier
// one for them.
func (k *KubeconfigSources) Approve(p *kubeconfig.ExecPlugin) error { return k.ApproveAll(p) }

// ApproveAll records the allowance of every plugin in ps, each for its own file and context and
// with its own fingerprint, in one write: all or none. A plugin without a fingerprint refuses the
// whole call.
func (k *KubeconfigSources) ApproveAll(ps ...*kubeconfig.ExecPlugin) error {
	for _, p := range ps {
		if p.Fingerprint() == "" {
			return errors.New("credential plugin approvals: the plugin has no fingerprint")
		}
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	list, err := k.loadApprovals()
	if err != nil {
		return err
	}
	now := k.now().UTC()
	for _, p := range ps {
		list = slices.DeleteFunc(list, func(a ExecApproval) bool { return a.File == p.File && a.Context == p.Context })
		list = append(list, ExecApproval{File: p.File, Context: p.Context, Fingerprint: p.Fingerprint(), Command: p.Command, Approved: now, Kind: p.Kind()})
	}
	return k.saveApprovals(list)
}

// forget drops the approvals drop matches.
func (k *KubeconfigSources) forget(drop func(ExecApproval) bool) error {
	list, err := k.loadApprovals()
	if err != nil {
		return err
	}
	kept := slices.DeleteFunc(slices.Clone(list), drop)
	if len(kept) == len(list) {
		return nil
	}
	return k.saveApprovals(kept)
}

// Policy is the cluster.Options.ExecPolicy of the sources' contexts: it refuses a plugin the user
// has not allowed, or allowed in another form, with an *ExecApprovalError.
func (k *KubeconfigSources) Policy(p *kubeconfig.ExecPlugin) error {
	state, err := k.ExecState(p)
	if err != nil {
		return err
	}
	if state != ExecApproved {
		return &ExecApprovalError{Plugin: p, State: state}
	}
	return nil
}

// signature describes the sources' files as stat sees them (size, modification time), without
// reading them: WatchKubeconfigs compares it between polls.
func (k *KubeconfigSources) signature() string {
	list, err := k.List()
	if err != nil {
		return "error " + err.Error()
	}
	var b strings.Builder
	stat := func(p string) {
		fi, err := os.Stat(p)
		if err != nil {
			fmt.Fprintf(&b, "%s missing\n", p)
			return
		}
		fmt.Fprintf(&b, "%s %d %d\n", p, fi.Size(), fi.ModTime().UnixNano())
	}
	for _, s := range list {
		fmt.Fprintf(&b, "%s %s %s\n", s.ID, s.Kind, s.Path)
		if s.Kind != SourceFolder {
			stat(s.Path)
			continue
		}
		files, err := cluster.ListSyncFolder(s.Path)
		if err != nil {
			fmt.Fprintf(&b, "%s unreadable\n", s.Path)
			continue
		}
		for _, f := range files {
			fmt.Fprintf(&b, "%s %d %d\n", f.Path, f.Size, f.ModTime)
		}
	}
	return b.String()
}

// FileFate says whether a source file is still there for Cogpit to read.
type FileFate int

// File fates.
const (
	FileOffered  FileFate = iota // the file exists and a source still covers it, or this cannot be told
	FileGone                     // the file does not exist any more
	FileUnlisted                 // the file exists, but no source covers it any more (the source was removed)
)

// Fate looks at path, a file one of the sources offered: is it still on disk, and does a source
// still cover it (the added file itself, or a regular file of a sync folder under the folder's
// naming rules)? A failure that is neither "does not exist" nor a clear answer from the source list
// (a permission error, an unreadable list of sources) counts as FileOffered: Cogpit closes a
// connection only on a plain fact.
func (k *KubeconfigSources) Fate(path string) FileFate { return k.FateOf(path, "") }

// FateOf is Fate for a file offered under label, the label in its contexts' ids
// (SourceContextID): the file is covered only while a source still offers it under that label.
// One file can be offered twice, by a file source of its own and by a synced folder it lies in,
// under two labels and so as two contexts; removing the file source leaves the file covered by the
// folder, but not the context the removed source offered (the owner's walk 2026-10-17, WF3: a
// connection made from a removed file source stayed open because the user's home folder was
// synced too). An empty label asks whether any source covers the file.
func (k *KubeconfigSources) FateOf(path, label string) FileFate {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return FileGone
	}
	list, err := k.List()
	if err != nil {
		return FileOffered
	}
	for _, s := range list {
		covers := (s.Kind == SourceFile && s.Path == path) ||
			(s.Kind == SourceFolder && filepath.Dir(path) == s.Path && kubeconfig.SyncCandidate(filepath.Base(path)))
		if covers && (label == "" || sourceLabel(s, path) == label) {
			return FileOffered
		}
	}
	return FileUnlisted
}

// sourceLabel is the label under which src offers its file at path: the source's id for a file,
// the folder's id and the file's name for a file of a synced folder (".kube/config").
func sourceLabel(src KubeconfigSource, path string) string {
	if src.Kind == SourceFolder {
		return strings.TrimSuffix(src.ID, "/") + "/" + filepath.Base(path)
	}
	return src.ID
}

// ContextSourceLabel is the label of a source context id ("kind-dev [.kube/config]" →
// ".kube/config"); "" for an id without one.
func ContextSourceLabel(id string) string {
	rest, ok := strings.CutSuffix(id, "]")
	if !ok {
		return ""
	}
	if i := strings.LastIndex(rest, " ["); i > 0 {
		return rest[i+len(" ["):]
	}
	return ""
}

// absClean returns path made absolute and clean; an empty path is an error.
func absClean(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("no path given")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}
