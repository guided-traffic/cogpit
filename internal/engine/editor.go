package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Editor tabs (G9). An Edit tab edits one object as YAML; a Create resource tab holds YAML for
// new objects. The engine keeps each tab's text and, for an Edit tab, the version of the object the
// edit is based on: the resourceVersion that every apply carries as its precondition. The base
// comes from the cache when the tab opens and moves only when an apply succeeds or the server
// reports a conflict, never from the text, so an apply cannot overwrite a change it has not shown.

// Titles and timings of editor tabs.
const (
	createTabTitle = "Create resource"
	// conflictWait bounds how long a conflict waits for the cache to see the newer version.
	conflictWait = 3 * time.Second
	// DefaultDiffContext is how many unchanged lines a diff keeps around each change.
	DefaultDiffContext = 3
)

// ErrNotEditorTab is returned for a dock tab id that is not an open editor tab.
var ErrNotEditorTab = errors.New("not an open editor tab")

// editor is the engine side of one editor tab.
type editor struct {
	tab  DockTab
	text string
	rev  uint64

	// Edit tabs only: the object, and the version the edit is based on.
	target   ObjectTarget
	baseRV   string
	baseUID  string
	baseText string
	// undo is what editor.undoRevert puts back (editrevert.go).
	undo *editUndo

	// Create resource tabs only: the text the tab opened with, or the one it created in full,
	// which is not unsaved work: a draft keeps only a text that differs (drafts.go).
	startText string

	// Helm Upgrade tabs only: the revision the values are based on (helmupgrade.go). Never
	// changed in place: a rebase sets a new one.
	helm *helmBase
	// Helm Install tabs only: the chart the tab installs (helminstall.go).
	install *helmInstallBase

	// pending is set while the editor exists but its tab is not in the dock yet: pruning, which
	// another command may run meanwhile, keeps it.
	pending bool
	// addedRev is the UI state revision whose dock first held the tab: pruning that works from an
	// older snapshot of the dock must not take the tab's absence there for a closed tab.
	addedRev uint64
}

// editStore holds the editors of a Session. Its zero value is ready to use. Entries whose tab is
// no longer in the dock are dropped on the next access: dock.close needs no hook.
type editStore struct {
	mu sync.Mutex
	m  map[string]*editor
}

// EditorState is an editor tab as editor.get reports it.
type EditorState struct {
	Tab      DockTab `json:"tab"`
	Text     string  `json:"yaml"`
	Revision uint64  `json:"revision"` // increases with every change of the text
	// Edit tabs: the version the edit is based on, the version the cache holds now, and whether
	// they differ (an apply would be refused with a conflict) or the object is gone.
	BaseResourceVersion   string `json:"baseResourceVersion,omitempty"`
	CachedResourceVersion string `json:"cachedResourceVersion,omitempty"`
	Stale                 bool   `json:"stale,omitempty"`
	Missing               bool   `json:"missing,omitempty"`
	// Helm Upgrade tabs: the release revision the values are based on and the one the cache
	// holds now; Stale and Missing say whether the release moved (an upgrade would be refused)
	// or is gone.
	BaseRevision   int `json:"baseRevision,omitempty"`
	CachedRevision int `json:"cachedRevision,omitempty"`
	// CanUndoRevert: editor.undoRevert would put back the text an Edit tab's revert replaced.
	CanUndoRevert bool `json:"canUndoRevert,omitempty"`
}

// EditDiff is the diff of an Edit tab's text against the object as the cache holds it now.
type EditDiff struct {
	Ref                   model.ObjectRef `json:"ref"`
	Changed               bool            `json:"changed"`
	Added                 int             `json:"added"`
	Removed               int             `json:"removed"`
	Lines                 []DiffLine      `json:"lines"`
	BaseResourceVersion   string          `json:"baseResourceVersion"`
	CachedResourceVersion string          `json:"cachedResourceVersion"`
	// Stale is true when the cache holds a newer version than the edit is based on: an apply
	// would be refused with a conflict, and the lines include the other change, reverted.
	Stale bool `json:"stale,omitempty"`
}

// DryRunResult is what the server said to an edit's dry run.
type DryRunResult struct {
	Ref                 model.ObjectRef `json:"ref"`
	Accepted            bool            `json:"accepted"`
	BaseResourceVersion string          `json:"baseResourceVersion"`
}

// ApplyResult is a successful apply: the version the server stored, which is now the tab's base,
// and the object as stored, which is now the tab's text.
type ApplyResult struct {
	Ref             model.ObjectRef `json:"ref"`
	ResourceVersion string          `json:"resourceVersion"`
	Text            string          `json:"yaml"`
	Revision        uint64          `json:"revision"`
}

// ConflictError reports that the server refused an apply or dry run with 409 Conflict: the object
// changed since the version the edit is based on. Nothing was written. When the cache has seen
// the newer version, the tab's base moved to it and Diff compares the edited text with it; the
// next apply overwrites that version only if it is asked for again.
type ConflictError struct {
	Ref                    model.ObjectRef `json:"ref"`
	BaseResourceVersion    string          `json:"baseResourceVersion"`
	CurrentResourceVersion string          `json:"currentResourceVersion,omitempty"` // empty: the cache has not seen it yet
	Recreated              bool            `json:"recreated,omitempty"`              // deleted and created again under the name
	Diff                   []DiffLine      `json:"diff,omitempty"`
	// Rebased is true when the edit is now based on CurrentResourceVersion, so that applying again
	// overwrites it. Only an apply rebases: a dry run, which agents run freely, never moves the
	// base the user's review compares against.
	Rebased bool `json:"rebased,omitempty"`
}

func (e *ConflictError) Error() string {
	what := "changed on the cluster"
	if e.Recreated {
		what = "was deleted and created again"
	}
	if e.CurrentResourceVersion == "" {
		return fmt.Sprintf("%s %s since version %s, which the edit is based on; nothing was applied. "+
			"Cogpit's cache has not seen the new version yet: compare again with editor_diff in a moment.",
			e.Ref.String(), what, e.BaseResourceVersion)
	}
	if !e.Rebased {
		return fmt.Sprintf("%s %s since version %s, which the edit is based on; nothing was applied, and the edit stays "+
			"based on version %s. The cluster holds version %s: editor_diff shows the text against it; an apply reports "+
			"this conflict again and only then bases the edit on the new version.",
			e.Ref.String(), what, e.BaseResourceVersion, e.BaseResourceVersion, e.CurrentResourceVersion)
	}
	return fmt.Sprintf("%s %s since version %s, which the edit was based on; nothing was applied. The edit is now "+
		"based on version %s: editor_diff shows the text against it, and applying again overwrites it.",
		e.Ref.String(), what, e.BaseResourceVersion, e.CurrentResourceVersion)
}

// editorOf returns a copy of an open editor tab's state, dropping editors whose tab was closed.
func (s *Session) editorOf(id string) (editor, error) {
	open := map[string]bool{}
	ui := s.UIState()
	for _, t := range ui.Dock {
		open[t.ID] = true
	}
	s.edits.mu.Lock()
	defer s.edits.mu.Unlock()
	for k, e := range s.edits.m {
		if !open[k] && !e.pending && e.addedRev <= ui.Revision {
			delete(s.edits.m, k)
		}
	}
	e, ok := s.edits.m[id]
	if !ok {
		return editor{}, fmt.Errorf("%w: %q", ErrNotEditorTab, id)
	}
	return *e, nil
}

// updateEditor changes an open editor under the store's lock and emits EventEditor.
func (s *Session) updateEditor(id string, fn func(e *editor)) (editor, error) {
	s.edits.mu.Lock()
	e, ok := s.edits.m[id]
	if !ok {
		s.edits.mu.Unlock()
		return editor{}, fmt.Errorf("%w: %q", ErrNotEditorTab, id)
	}
	fn(e)
	out := *e
	s.edits.mu.Unlock()
	s.emit(Event{Type: EventEditor, View: id, Revision: out.rev})
	return out, nil
}

// putEditor adds a new editor, pending until settleEditor: its tab is added to the dock after it.
func (s *Session) putEditor(e *editor) {
	s.edits.mu.Lock()
	defer s.edits.mu.Unlock()
	if s.edits.m == nil {
		s.edits.m = map[string]*editor{}
	}
	e.pending = true
	s.edits.m[e.tab.ID] = e
}

// settleEditor ends a new editor's pending state once its tab is in the dock, or drops it when
// no tab was added for it (another tab already edits the object).
func (s *Session) settleEditor(id string, added bool, rev uint64) {
	s.edits.mu.Lock()
	defer s.edits.mu.Unlock()
	if !added {
		delete(s.edits.m, id)
		return
	}
	if e, ok := s.edits.m[id]; ok {
		e.pending = false
		e.addedRev = rev
	}
}

// withTypeMeta returns obj with apiVersion and kind set from the target where the cache left them
// out, so that the editor shows and checks them.
func withTypeMeta(obj *unstructured.Unstructured, t ObjectTarget) *unstructured.Unstructured {
	if obj.GetAPIVersion() != "" && obj.GetKind() != "" {
		return obj
	}
	c := obj.DeepCopy()
	c.SetAPIVersion(t.APIVersion)
	c.SetKind(t.Kind)
	return c
}

// OpenEditTab opens an Edit tab on an object, filled from the cache, or activates the tab already
// open on it. A Secret's tab opens only for the GUI (D-19).
func (s *Session) OpenEditTab(ctx context.Context, ref model.ObjectRef) (EditorState, bool, error) {
	t, b, err := s.ResolveTarget(ctx, ref)
	if err != nil {
		return EditorState{}, false, err
	}
	if err := requireGUIForKind(ctx, t.APIVersion, t.Kind); err != nil {
		return EditorState{}, false, err
	}
	if _, err := b.ObjectWriter(); err != nil {
		return EditorState{}, false, err
	}
	obj, err := cachedObject(ctx, b, t)
	if err != nil {
		return EditorState{}, false, err
	}
	obj = withTypeMeta(obj, t)
	text, err := EditableYAML(obj)
	if err != nil {
		return EditorState{}, false, err
	}
	if err := GuardSecretText(ctx, text); err != nil { // a Helm release record in a ConfigMap: the GUI only
		return EditorState{}, false, err
	}
	r := t.Ref
	e := &editor{
		tab: DockTab{
			ID: s.NewDockID(DockEdit), Type: DockEdit, Title: t.Kind + " " + t.Ref.Name,
			Context: b.Context, Ref: &r,
		},
		text: text, rev: 1, target: t,
		baseRV: obj.GetResourceVersion(), baseUID: string(obj.GetUID()), baseText: text,
	}
	s.putEditor(e)
	tab, created := e.tab, true
	ui := s.UpdateUI(func(u *UIState) {
		for _, x := range u.Dock {
			if x.Type == DockEdit && x.Ref != nil && x.Ref.Same(t.Ref) && x.Context == t.Ref.Context {
				tab, created = x, false
				break
			}
		}
		if created {
			u.Dock = append(u.Dock, tab)
		}
		u.ShowDockTab(tab.ID) // the tab open already, maybe the active one: the dock opens too (AW9)
	})
	s.settleEditor(e.tab.ID, created, ui.Revision)
	st, err := s.EditorState(ctx, tab.ID)
	return st, created, err
}

// OpenCreateTab opens a Create resource tab holding text, for a connected context's cluster ("" the
// shown one, D-69).
func (s *Session) OpenCreateTab(contextID, text string) (EditorState, error) {
	b, err := s.TargetBackend(contextID)
	if err != nil {
		return EditorState{}, err
	}
	if len(text) > MaxManifestBytes {
		return EditorState{}, fmt.Errorf("%w: larger than the %s an editor takes", ErrManifest, model.FormatBytes(MaxManifestBytes))
	}
	e := &editor{
		tab:  DockTab{ID: s.NewDockID(DockCreate), Type: DockCreate, Title: createTabTitle, Context: b.Context},
		text: text, rev: 1, startText: text,
	}
	s.putEditor(e)
	ui := s.UpdateUI(func(u *UIState) {
		u.Dock = append(u.Dock, e.tab)
		u.ActiveDock = e.tab.ID
	})
	s.settleEditor(e.tab.ID, true, ui.Revision)
	return EditorState{Tab: e.tab, Text: text, Revision: 1}, nil
}

// EditorState returns an editor tab's text and, for an Edit tab, its base version and the version
// the cache holds now.
func (s *Session) EditorState(ctx context.Context, id string) (EditorState, error) {
	e, err := s.editorOf(id)
	if err != nil {
		return EditorState{}, err
	}
	st := EditorState{Tab: e.tab, Text: e.text, Revision: e.rev}
	if e.tab.Type == DockHelmUpgrade {
		s.helmEditorState(&st, e)
		return st, nil
	}
	if e.tab.Type != DockEdit {
		return st, nil
	}
	st.BaseResourceVersion = e.baseRV
	st.CanUndoRevert = e.canUndo()
	if b := s.BackendOf(e.tab.Context); b != nil && e.tab.Context != "" { // the tab's cluster, shown or not (D-69)
		obj, err := cachedObject(ctx, b, e.target)
		switch {
		case errors.Is(err, ErrNotFound):
			st.Missing = true
		case err == nil:
			st.CachedResourceVersion = obj.GetResourceVersion()
			st.Stale = st.CachedResourceVersion != e.baseRV
		}
	}
	return st, nil
}

// SetEditorText replaces an editor tab's text (the GUI as the user types; an agent handing over a
// manifest) and returns the text's new revision.
func (s *Session) SetEditorText(id, text string) (uint64, error) {
	if _, err := s.editorOf(id); err != nil {
		return 0, err
	}
	if len(text) > MaxManifestBytes {
		return 0, fmt.Errorf("%w: larger than the %s an editor takes", ErrManifest, model.FormatBytes(MaxManifestBytes))
	}
	e, err := s.updateEditor(id, func(e *editor) {
		if e.text != text {
			e.text = text
			e.rev++
		}
	})
	return e.rev, err
}

// editTarget returns an Edit tab's editor with the backend it belongs to: the tab must be an
// Edit tab of the connected cluster.
func (s *Session) editTarget(id string) (editor, *Backend, ObjectWriter, error) {
	e, err := s.editorOf(id)
	if err != nil {
		return editor{}, nil, nil, err
	}
	if e.tab.Type != DockEdit {
		return editor{}, nil, nil, fmt.Errorf("dock tab %q is a %s tab, not an Edit tab", id, e.tab.Type)
	}
	b, err := s.TargetBackend(e.tab.Context) // the tab's cluster, shown or not (D-69)
	if err != nil {
		return editor{}, nil, nil, err
	}
	w, err := b.ObjectWriter()
	if err != nil {
		return editor{}, nil, nil, err
	}
	return e, b, w, nil
}

// editedObject parses an Edit tab's text (the tab's own when text is empty) and checks that it is
// still the tab's object: the same apiVersion, kind, name and namespace. An edit cannot rename,
// move or retype an object.
func editedObject(e editor, text string) (*unstructured.Unstructured, error) {
	if text == "" {
		text = e.text
	}
	obj, err := ParseManifest(text)
	if err != nil {
		return nil, err
	}
	t := e.target
	if obj.GetAPIVersion() != t.APIVersion {
		return nil, fmt.Errorf("%w: apiVersion must stay %s; an edit cannot change an object's API version", ErrManifest, t.APIVersion)
	}
	if obj.GetKind() != t.Kind {
		return nil, fmt.Errorf("%w: kind must stay %s; an edit cannot change an object's kind", ErrManifest, t.Kind)
	}
	if obj.GetName() != t.Ref.Name {
		return nil, fmt.Errorf("%w: metadata.name must stay %s; an edit cannot rename an object", ErrManifest, t.Ref.Name)
	}
	if t.Namespaced && obj.GetNamespace() != t.Ref.Namespace {
		return nil, fmt.Errorf("%w: metadata.namespace must stay %s; an edit cannot move an object", ErrManifest, t.Ref.Namespace)
	}
	if !t.Namespaced && obj.GetNamespace() != "" {
		return nil, fmt.Errorf("%w: %s are cluster-scoped: metadata.namespace must stay empty", ErrManifest, t.Ref.Kind)
	}
	return obj, nil
}

// forUpdate returns the edited object as an update sends it: managedFields dropped (the server
// keeps its own), and the resourceVersion and UID of the edit's base set by the engine, whatever
// the text says.
func forUpdate(edited *unstructured.Unstructured, e editor) *unstructured.Unstructured {
	c := edited.DeepCopy()
	unstructured.RemoveNestedField(c.Object, "metadata", "managedFields")
	c.SetResourceVersion(e.baseRV)
	if e.baseUID != "" {
		c.SetUID(types.UID(e.baseUID))
	}
	return c
}

// diffAgainst returns the line diff of the edited object against another version of it: the
// edited side gets that version's resourceVersion and UID, which the engine sets anyway, so that
// only real changes show.
func diffAgainst(current, edited *unstructured.Unstructured) ([]DiffLine, error) {
	before, err := EditableYAML(current)
	if err != nil {
		return nil, err
	}
	c := edited.DeepCopy()
	c.SetResourceVersion(current.GetResourceVersion())
	if uid := current.GetUID(); uid != "" {
		c.SetUID(uid)
	}
	after, err := EditableYAML(c)
	if err != nil {
		return nil, err
	}
	return DiffLines(before, after), nil
}

// EditDiff returns the diff of an Edit tab's text (or text, when given) against the object as the
// cache holds it now, keeping context unchanged lines around each change (negative: all).
func (s *Session) EditDiff(ctx context.Context, id, text string, context int) (EditDiff, error) {
	if err := s.GuardSecretEditor(ctx, id, text); err != nil {
		return EditDiff{}, err
	}
	e, b, _, err := s.editTarget(id)
	if err != nil {
		return EditDiff{}, err
	}
	edited, err := editedObject(e, text)
	if err != nil {
		return EditDiff{}, err
	}
	cached, err := cachedObject(ctx, b, e.target)
	if err != nil {
		return EditDiff{}, err
	}
	cached = withTypeMeta(cached, e.target)
	lines, err := diffAgainst(cached, edited)
	if err != nil {
		return EditDiff{}, err
	}
	// An agent's lines leave Flux's credential fields out of both sides (H-63, agentview.go); the
	// counts are the real diff's. A private turn reads them as the window does (readslikeuser.go).
	shown := lines
	if ReadsAsAgent(ctx) {
		if shown, err = diffAgainst(withholdFluxObject(ctx, cached), withholdFluxObject(ctx, edited)); err != nil {
			return EditDiff{}, err
		}
	}
	d := EditDiff{
		Ref: e.target.Ref, Changed: DiffChanged(lines), Lines: CompactDiff(shown, context),
		BaseResourceVersion: e.baseRV, CachedResourceVersion: cached.GetResourceVersion(),
		Stale: cached.GetResourceVersion() != e.baseRV,
	}
	for _, l := range lines {
		switch l.Op {
		case DiffAdded:
			d.Added++
		case DiffRemoved:
			d.Removed++
		}
	}
	return d, nil
}

// DryRunEdit sends an Edit tab's text (or text) to the server as an update with dryRun=All and
// the base version as precondition: the server validates and admits it, and stores nothing. A
// conflict returns *ConflictError.
func (s *Session) DryRunEdit(ctx context.Context, id, text string) (DryRunResult, error) {
	if err := s.GuardSecretEditor(ctx, id, text); err != nil {
		return DryRunResult{}, err
	}
	e, b, w, err := s.editTarget(id)
	if err != nil {
		return DryRunResult{}, err
	}
	edited, err := editedObject(e, text)
	if err != nil {
		return DryRunResult{}, err
	}
	if _, err := w.UpdateObject(ctx, e.target.GVR, forUpdate(edited, e), true); err != nil {
		return DryRunResult{}, scrubSecretWrite(s.writeError(ctx, b, e, edited, err, false), edited)
	}
	return DryRunResult{Ref: e.target.Ref, Accepted: true, BaseResourceVersion: e.baseRV}, nil
}

// ApplyEdit applies an Edit tab's text (or text): a dry run first, then the update, both with the
// resourceVersion the edit is based on as precondition. On success the stored object becomes the
// tab's text and base. On 409 Conflict nothing is written and *ConflictError says what changed.
// The caller has checked the confirmation's context; ApplyEdit checks it again.
func (s *Session) ApplyEdit(ctx context.Context, id, text string, textRevision uint64) (ApplyResult, error) {
	if err := s.GuardSecretEditor(ctx, id, text); err != nil {
		return ApplyResult{}, err
	}
	e, b, w, err := s.editTarget(id)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := CheckConfirmedContext(ctx, b.Context); err != nil {
		return ApplyResult{}, err
	}
	if text == "" { // the tab's text, as the revision the caller (and the user) reviewed names it
		if textRevision == 0 {
			return ApplyResult{}, fmt.Errorf("give yaml, or textRevision: the revision of the tab %s's text to apply (editor_get shows it); nothing was written", id)
		}
		if e.rev != textRevision {
			return ApplyResult{}, fmt.Errorf("%w: the tab %s's text changed since text revision %d (now %d); nothing was written: review it and apply again",
				ErrTargetsChanged, id, textRevision, e.rev)
		}
	}
	edited, err := editedObject(e, text)
	if err != nil {
		return ApplyResult{}, err
	}
	obj := forUpdate(edited, e)
	if _, err := w.UpdateObject(ctx, e.target.GVR, obj, true); err != nil {
		return ApplyResult{}, scrubSecretWrite(s.writeError(ctx, b, e, edited, err, true), edited)
	}
	stored, err := w.UpdateObject(ctx, e.target.GVR, obj, false)
	if err != nil {
		return ApplyResult{}, scrubSecretWrite(s.writeError(ctx, b, e, edited, err, true), edited)
	}
	stored = withTypeMeta(stored, e.target)
	newText, err := EditableYAML(stored)
	if err != nil {
		newText = text
		if newText == "" {
			newText = e.text
		}
	}
	after, err := s.updateEditor(id, func(x *editor) {
		x.baseRV, x.baseUID, x.baseText = stored.GetResourceVersion(), string(stored.GetUID()), newText
		if x.text != newText {
			x.text = newText
			x.rev++
		}
	})
	if err != nil { // the tab was closed while the apply ran; the apply stands
		after = editor{rev: e.rev}
	}
	s.Emit(Event{Type: EventToast, Level: LevelInfo, Message: e.target.Kind + " " + e.target.Ref.Key() + " saved"})
	return ApplyResult{Ref: e.target.Ref, ResourceVersion: stored.GetResourceVersion(), Text: newText, Revision: after.rev}, nil
}

// EditScalesToZero reports whether applying an Edit tab's text (or text) sets spec.replicas to 0
// on an object whose cached spec.replicas is not 0 (a missing count is the API's default, 1):
// the same change Scale to zero makes, so object.apply confirms it as destructive. When the
// cached object cannot be read, an edited count of 0 is enough.
func (s *Session) EditScalesToZero(id, text string) bool {
	e, err := s.editorOf(id)
	if err != nil || e.tab.Type != DockEdit {
		return false
	}
	if text == "" {
		text = e.text
	}
	edited, err := ParseManifest(text)
	if err != nil {
		return false
	}
	if n, ok := replicasOf(edited); !ok || n != 0 {
		return false
	}
	b := s.BackendOf(e.tab.Context) // the tab's cluster, shown or not (D-69)
	if b == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
	defer cancel()
	cached, err := cachedObject(ctx, b, e.target)
	if err != nil {
		return true
	}
	n, ok := replicasOf(cached)
	return !ok || n != 0
}

// replicasOf reads spec.replicas as a whole number; ok is false when it is absent or not a number.
func replicasOf(obj *unstructured.Unstructured) (int64, bool) {
	v, found, _ := unstructured.NestedFieldNoCopy(obj.Object, "spec", "replicas")
	if !found {
		return 0, false
	}
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case float64:
		return int64(n), n == float64(int64(n))
	}
	return 0, false
}

// writeError turns a failed dry run or update into what the caller sees: *ConflictError for a 409,
// after rebasing the tab on the version the cache now holds when rebase is set (an apply); a plain
// error naming the object otherwise.
func (s *Session) writeError(ctx context.Context, b *Backend, e editor, edited *unstructured.Unstructured, err error, rebase bool) error {
	if apierrors.IsNotFound(err) {
		return fmt.Errorf("%s no longer exists on the cluster; nothing was applied: %w", e.target.Ref.String(), err)
	}
	if !apierrors.IsConflict(err) {
		return fmt.Errorf("%s: %w", e.target.Ref.String(), err)
	}
	ce := &ConflictError{Ref: e.target.Ref, BaseResourceVersion: e.baseRV}
	current, seen := waitForNewer(ctx, b, e.target, e.baseRV, conflictWait)
	if !seen {
		return ce
	}
	if current == nil {
		return fmt.Errorf("%s was deleted on the cluster since version %s; nothing was applied", e.target.Ref.String(), e.baseRV)
	}
	current = withTypeMeta(current, e.target)
	ce.CurrentResourceVersion = current.GetResourceVersion()
	ce.Recreated = e.baseUID != "" && string(current.GetUID()) != e.baseUID
	if lines, derr := diffAgainst(current, edited); derr == nil {
		ce.Diff = CompactDiff(lines, DefaultDiffContext)
	}
	if !rebase {
		return ce
	}
	baseText, _ := EditableYAML(current)
	ce.Rebased = true
	if _, uerr := s.updateEditor(e.tab.ID, func(x *editor) {
		x.baseRV, x.baseUID, x.baseText = current.GetResourceVersion(), string(current.GetUID()), baseText
	}); uerr != nil {
		return fmt.Errorf("%w (and the tab is closed)", ce)
	}
	return ce
}
