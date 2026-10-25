package engine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The Revisions dock tab (plan/helm-revisions.md, a prototype): a release's revisions, each one's
// values (user-supplied or all) and rendered manifest, compared with another revision. Everything
// comes from the release records the cache holds, one per revision (the Releases kind's scope):
// opening a revision decodes its record, comparing decodes a second one, and nothing asks the
// API server. The tab's state is the engine's (DockTab.HelmRevisions), so agents see and drive
// the tab the user sees; what it shows of values and rendered Secrets agents read withheld
// (helm.release.revision).

// DockHelmRevisions: a release's revisions, their values and manifests (ui.helm.revisions).
const DockHelmRevisions DockType = "helmRevisions"

// helmRevisionsTitle starts a Revisions tab's title.
const helmRevisionsTitle = "Revisions · "

// The Revisions tab's modes.
const (
	RevisionsValues   = "values"
	RevisionsManifest = "manifest"
)

// HelmRevisionsTab is what a Revisions tab shows: the revision, the one it is compared with (0:
// none), values or manifest, and within them which values, which object, the raw manifest.
type HelmRevisionsTab struct {
	Revision int    `json:"revision"`
	Compare  int    `json:"compare,omitempty"`
	Mode     string `json:"mode"`
	// All shows every value, the chart's defaults included; else the user-supplied ones.
	All bool `json:"all,omitempty"`
	// Raw shows the manifest as Helm stored it (manifest.yaml) instead of its objects.
	Raw bool `json:"raw,omitempty"`
	// Object is the selected object of the manifest (helm.ManifestObject.Key); "" for the first.
	Object string `json:"object,omitempty"`
	// LineDiff unfolds the values' line diff under the changed keys.
	LineDiff bool `json:"lineDiff,omitempty"`
}

// HelmRevisionsPatch changes a Revisions tab's state: each field set replaces the tab's.
type HelmRevisionsPatch struct {
	Revision *int    `json:"revision,omitempty"`
	Compare  *int    `json:"compare,omitempty"`
	Mode     *string `json:"mode,omitempty"`
	All      *bool   `json:"all,omitempty"`
	Raw      *bool   `json:"raw,omitempty"`
	Object   *string `json:"object,omitempty"`
	LineDiff *bool   `json:"lineDiff,omitempty"`
}

// ErrNotRevisionsTab says a dock tab is no Revisions tab.
var ErrNotRevisionsTab = errors.New("not a Revisions tab")

// withRelease runs fn with a release object from the cache and its backend, after waiting (bounded
// by ctx) for the release records' initial lists, and with a function that finds the record of one
// of its revisions (0: the newest) while the cache handle is held.
func (s *Session) withRelease(ctx context.Context, ref model.ObjectRef, fn func(b *Backend, ref model.ObjectRef, obj *unstructured.Unstructured, record func(rev int) (*unstructured.Unstructured, error)) error) error {
	if ref.Kind != helm.ReleaseKind {
		return fmt.Errorf("%w: %s is not a Helm release", ErrNotApplicable, ref.String())
	}
	b, ref, p, err := s.resolve(ref)
	if err != nil {
		return err
	}
	h := b.Acquire(p)
	defer h.Release()
	if err := waitSynced(ctx, h, p.Key); err != nil {
		return err
	}
	obj, ok := KindGet(h, p, ref.Namespace, ref.Name)
	if !ok {
		return fmt.Errorf("%s: %w", ref.String(), ErrNotFound)
	}
	record := func(rev int) (*unstructured.Unstructured, error) {
		if rev == 0 {
			rev = helm.ReleaseRevision(obj)
		}
		if !slices.Contains(helm.ReleaseRevisions(obj), rev) {
			return nil, fmt.Errorf("release %s, revision %d: %w", ref.Name, rev, helm.ErrRevisionGone)
		}
		r, ok := revisionRecord(h, obj, rev)
		if !ok {
			return nil, fmt.Errorf("release %s, revision %d: %w", ref.Name, rev, helm.ErrRevisionGone)
		}
		return r, nil
	}
	return fn(b, ref, obj, record)
}

// revisionRecord returns the record of one revision of a release from the cache: the Secret, or
// the ConfigMap for a release kept in ConfigMaps, named as Helm names it (releaseRecord for the
// newest).
func revisionRecord(h *cache.Handle, obj *unstructured.Unstructured, rev int) (*unstructured.Unstructured, bool) {
	driver, _ := helm.ReleaseRecord(obj)
	name := helm.RecordName(obj.GetName(), rev)
	parts := h.Parts()
	if parts == nil {
		parts = []*cache.Handle{h}
	}
	for _, part := range parts {
		if part.Scope().GVR.Resource == driver.Resource() {
			return part.Get(obj.GetNamespace(), name)
		}
	}
	return nil, false
}

// ReleaseRevision returns one revision of a release from the cache, revision 0 the newest: its
// values (all: with the chart's defaults) and manifest. The caller decides who may see them
// (helm.release.revision withholds values and Secrets from agents).
func (s *Session) ReleaseRevision(ctx context.Context, ref model.ObjectRef, rev int, all bool) (helm.Revision, error) {
	var out helm.Revision
	err := s.withRelease(ctx, ref, func(_ *Backend, ref model.ObjectRef, _ *unstructured.Unstructured, record func(int) (*unstructured.Unstructured, error)) error {
		rec, err := record(rev)
		if err != nil {
			return err
		}
		if out, err = helm.RevisionOf(rec, all); err != nil {
			return fmt.Errorf("release %s, revision %d: %w", ref.Name, rev, err)
		}
		return nil
	})
	return out, err
}

// What helm.release.diff compares.
const (
	DiffValues    = "values"
	DiffAllValues = "allValues"
	DiffManifest  = "manifest"
)

// RevisionDiff is what changed from one revision (From, the one compared) to another (To, the one
// shown): the values' changed keys and their line diff, or the manifest's objects marked added,
// removed, changed or the same, with one object's line diff when asked for.
type RevisionDiff struct {
	Ref  model.ObjectRef `json:"ref"`
	From int             `json:"from"`
	To   int             `json:"to"`
	What string          `json:"what"`
	// Changes are the values that differ, by key path (values, allValues); ChangesCut says the
	// list stops at its bound.
	Changes    []helm.ValueChange `json:"changes,omitempty"`
	ChangesCut bool               `json:"changesCut,omitempty"`
	// Lines is the line diff of the values, or of Object's YAML (manifest), compacted to three
	// lines of context around each change (none for a Secret masked alike on both sides).
	Lines []DiffLine `json:"lines,omitempty"`
	// Objects are both revisions' objects marked (manifest), Added to Same their counts.
	Objects []helm.ObjectChange `json:"objects,omitempty"`
	Object  string              `json:"object,omitempty"`
	Added   int                 `json:"added,omitempty"`
	Removed int                 `json:"removed,omitempty"`
	Changed int                 `json:"changed,omitempty"`
	Same    int                 `json:"same,omitempty"`
}

// diffContext is how many unchanged lines a revision diff keeps around each change.
const diffContext = 3

// ReleaseRevisionDiff compares two revisions of a release from the cache: what changed from from
// to to. For the manifest, object names one object (helm.ManifestObject.Key) whose line diff Lines
// holds. mask, when set, replaces a rendered Secret's values on both sides before the object's
// diff (helm.MaskSize: the window's masked view; Withheld: an agent's).
func (s *Session) ReleaseRevisionDiff(ctx context.Context, ref model.ObjectRef, from, to int, what, object string, mask func(int) string) (RevisionDiff, error) {
	if what == "" {
		what = DiffValues
	}
	if what != DiffValues && what != DiffAllValues && what != DiffManifest {
		return RevisionDiff{}, fmt.Errorf("what %q: one of %s, %s, %s", what, DiffValues, DiffAllValues, DiffManifest)
	}
	var a, b helm.Revision
	err := s.withRelease(ctx, ref, func(_ *Backend, r model.ObjectRef, _ *unstructured.Unstructured, record func(int) (*unstructured.Unstructured, error)) error {
		ref = r
		for _, x := range []struct {
			rev *int
			out *helm.Revision
		}{{&from, &a}, {&to, &b}} {
			rec, err := record(*x.rev)
			if err != nil {
				return err
			}
			if *x.out, err = helm.RevisionOf(rec, what == DiffAllValues); err != nil {
				return fmt.Errorf("release %s, revision %d: %w", ref.Name, *x.rev, err)
			}
			*x.rev = x.out.Revision
		}
		return nil
	})
	if err != nil {
		return RevisionDiff{}, err
	}
	return CompareRevisions(ref, a, b, what, object, mask), nil
}

// CompareRevisions is ReleaseRevisionDiff on two decoded revisions. The objects it lists carry no
// YAML: helm.release.revision reads it, Lines the one object's diff.
func CompareRevisions(ref model.ObjectRef, a, b helm.Revision, what, object string, mask func(int) string) RevisionDiff {
	d := RevisionDiff{Ref: ref, From: a.Revision, To: b.Revision, What: what, Object: object}
	if what != DiffManifest {
		d.Changes, d.ChangesCut = helm.ValueChanges(a, b)
		d.Lines = CompactDiff(DiffLines(a.Values, b.Values), diffContext)
		return d
	}
	d.Objects = helm.ObjectChanges(a.Objects, b.Objects)
	for i, c := range d.Objects {
		switch c.Change {
		case helm.ObjectAdded:
			d.Added++
		case helm.ObjectRemoved:
			d.Removed++
		case helm.ObjectChanged:
			d.Changed++
		default:
			d.Same++
		}
		if object != "" && c.Key() == object {
			was, is := c.Was, c.YAML
			if c.Change == helm.ObjectRemoved {
				is = ""
			}
			if mask != nil && c.Secret {
				was, is = maskOr(was, mask), maskOr(is, mask)
			}
			d.Lines = CompactDiff(DiffLines(was, is), diffContext)
		}
		d.Objects[i].YAML, d.Objects[i].Masked, d.Objects[i].Was = "", "", ""
	}
	return d
}

// OpenHelmRevisionsTab opens a release's Revisions tab at revision rev (0: the newest), compared
// with compare (nil: the newest revision for an older one, none for the newest; 0: none), or
// activates the tab open on the release and shows that revision there. It returns the tab and
// whether it was opened.
func (s *Session) OpenHelmRevisionsTab(ctx context.Context, ref model.ObjectRef, rev int, compare *int, mode string) (DockTab, bool, error) {
	var tab DockTab
	created := false
	err := s.withRelease(ctx, ref, func(b *Backend, ref model.ObjectRef, obj *unstructured.Unstructured, _ func(int) (*unstructured.Unstructured, error)) error {
		newest := helm.ReleaseRevision(obj)
		revs := helm.ReleaseRevisions(obj)
		if rev == 0 {
			rev = newest
		}
		if !slices.Contains(revs, rev) {
			return fmt.Errorf("release %s, revision %d: %w", ref.Name, rev, helm.ErrRevisionGone)
		}
		against := 0
		switch {
		case compare != nil:
			against = *compare
		case rev != newest:
			against = newest
		}
		if against != 0 && (against == rev || !slices.Contains(revs, against)) {
			return fmt.Errorf("release %s: compare with revision %d: %w", ref.Name, against, helm.ErrRevisionGone)
		}
		if mode != "" && mode != RevisionsValues && mode != RevisionsManifest {
			return fmt.Errorf("mode %q: %s or %s", mode, RevisionsValues, RevisionsManifest)
		}
		r := ref
		tab = DockTab{
			ID: s.NewDockID(DockHelmRevisions), Type: DockHelmRevisions, Title: helmRevisionsTitle + ref.Name, Context: b.Context, Ref: &r,
			HelmRevisions: &HelmRevisionsTab{Revision: rev, Compare: against, Mode: cmp.Or(mode, RevisionsValues)},
		}
		created = true
		s.UpdateUI(func(u *UIState) {
			for i, x := range u.Dock {
				if x.Type == DockHelmRevisions && x.Ref != nil && x.Ref.Same(ref) && x.Context == b.Context {
					st := *x.HelmRevisions
					st.Revision, st.Compare, st.Object = rev, against, ""
					if mode != "" {
						st.Mode = mode
					}
					u.Dock[i].HelmRevisions = &st
					tab, created = u.Dock[i], false
					break
				}
			}
			if created {
				u.Dock = append(u.Dock, tab)
			}
			u.ShowDockTab(tab.ID)
		})
		return nil
	})
	return tab, created, err
}

// SetHelmRevisionsTab changes a Revisions tab's state (ui.helm.revisions.set). A revision must be
// one the release's records hold; a new revision or comparison selects the first object again.
func (s *Session) SetHelmRevisionsTab(id string, p HelmRevisionsPatch) (DockTab, error) {
	tab, ok := s.UIState().DockTab(id)
	if !ok {
		return DockTab{}, fmt.Errorf("no dock tab %q is open; ui_state lists the open ones (dock)", id)
	}
	if tab.Type != DockHelmRevisions || tab.HelmRevisions == nil || tab.Ref == nil {
		return DockTab{}, fmt.Errorf("dock tab %q: %w", id, ErrNotRevisionsTab)
	}
	obj, err := s.Lookup(*tab.Ref)
	if err != nil {
		return DockTab{}, err
	}
	revs := helm.ReleaseRevisions(obj)
	st := *tab.HelmRevisions
	if p.Revision != nil {
		if !slices.Contains(revs, *p.Revision) {
			return DockTab{}, fmt.Errorf("release %s, revision %d: %w", tab.Ref.Name, *p.Revision, helm.ErrRevisionGone)
		}
		if *p.Revision != st.Revision {
			st.Object = ""
		}
		st.Revision = *p.Revision
		if st.Compare == st.Revision { // compared with itself: no comparison
			st.Compare = 0
		}
	}
	if p.Compare != nil {
		if *p.Compare != 0 && (*p.Compare == st.Revision || !slices.Contains(revs, *p.Compare)) {
			return DockTab{}, fmt.Errorf("release %s: compare with revision %d: %w", tab.Ref.Name, *p.Compare, helm.ErrRevisionGone)
		}
		st.Compare = *p.Compare
	}
	if p.Mode != nil {
		if *p.Mode != RevisionsValues && *p.Mode != RevisionsManifest {
			return DockTab{}, fmt.Errorf("mode %q: %s or %s", *p.Mode, RevisionsValues, RevisionsManifest)
		}
		st.Mode = *p.Mode
	}
	for _, b := range []struct {
		from *bool
		to   *bool
	}{{p.All, &st.All}, {p.Raw, &st.Raw}, {p.LineDiff, &st.LineDiff}} {
		if b.from != nil {
			*b.to = *b.from
		}
	}
	if p.Object != nil {
		st.Object = *p.Object
	}
	var out DockTab
	s.UpdateUI(func(u *UIState) {
		for i, x := range u.Dock {
			if x.ID == id {
				u.Dock[i].HelmRevisions = &st
				out = u.Dock[i]
			}
		}
	})
	return out, nil
}

// maskOr masks a Secret's YAML, "" (no object on that side) staying "".
func maskOr(text string, mask func(int) string) string {
	if text == "" {
		return ""
	}
	return helm.MaskSecretYAML(text, mask)
}

// The ways a comparison reads (Preferences.RevisionsDiff): which revision is the diff's old side.
const (
	// RevisionsComparedToShown: from the revision compared to the one shown, the default ("" too):
	// against the current revision what a rollback to the one shown would change, against the
	// revision before what the one shown changed.
	RevisionsComparedToShown = "comparedToShown"
	// RevisionsOlderToNewer: from the older of the two to the newer, as a history reads.
	RevisionsOlderToNewer = "olderToNewer"
	// RevisionsShownToCompared: from the revision shown to the one compared.
	RevisionsShownToCompared = "shownToCompared"
)

// CheckRevisionsDiff refuses a way of reading a comparison Cogpit does not know.
func CheckRevisionsDiff(way string) error {
	switch way {
	case "", RevisionsComparedToShown, RevisionsOlderToNewer, RevisionsShownToCompared:
		return nil
	}
	return fmt.Errorf("revisionsDiff %q: %s, %s or %s (empty: %s)", way, RevisionsComparedToShown, RevisionsOlderToNewer, RevisionsShownToCompared, RevisionsComparedToShown)
}

// RevisionsDiffSides returns the revisions a comparison goes from and to, as the preference way
// reads one of the revision shown against the one compared.
func RevisionsDiffSides(way string, shown, compared int) (from, to int) {
	switch way {
	case RevisionsOlderToNewer:
		return min(shown, compared), max(shown, compared)
	case RevisionsShownToCompared:
		return shown, compared
	}
	return compared, shown
}
