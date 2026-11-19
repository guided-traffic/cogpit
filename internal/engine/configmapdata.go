package engine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// A Config Map's values edited in the drawer (the owner's F3, decided; decisions D-55). The drawer
// shows each key of the data with its value in an editor and a Save at the end, as Lens's drawer
// does. configMap.readData hands the drawer the values whole from the cache (the drawer model's are
// cut for agents and the YAML has them all), with the resourceVersion they are of;
// configMap.reviewData diffs the values the user changed against the object as the cache holds it
// and asks the server for a dry run; configMap.saveData writes them. The write is the Edit tab's
// (editor.go): the object from the cache with the changed values set, sent through the backend's
// ObjectWriter, a server-side dry run first, then the update with the resourceVersion the values
// are based on as its precondition. When the Config Map changed since, nothing is written and the
// caller gets a *DataConflictError with the values against the newer version; writing them over it
// takes a second call that names the newer version. A review that is given the edited keys' values
// at the version the values are based on (from) rebases them onto the newer version when none of
// those keys changed there (the owner's answer): the diff and the dry run are then against the
// newer version, which the caller saves with, after the user has seen them. Keys are added and
// removed in the same write (add and remove, the owner's audit decision; datakeys.go); a save that
// removes one is destructive. Binary data and a Helm release record's payload are not edited, and
// an immutable Config Map is refused.

// configMapAPIKind is a Config Map's kind as its manifest names it.
const configMapAPIKind = "ConfigMap"

// ConfigMapDataKeys is how many keys of a Config Map's data configMap.readData returns, sorted: the
// keys the drawer lists (kinds' configMapItems).
const ConfigMapDataKeys = 100

// maxConfigMapValues bounds the text one save carries: the API server keeps a Config Map within
// 1 MiB.
const maxConfigMapValues = 1 << 20

// ConfigMapValue is one key of a Config Map's data as configMap.readData returns it.
type ConfigMapValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	// Note stands in for a value that is not shown (a Helm release record's payload, which holds
	// the release's values); such a value cannot be changed here.
	Note string `json:"note,omitempty"`
}

// ConfigMapData is a Config Map's data from the cache: the version it is of and its first
// ConfigMapDataKeys keys, sorted, each with its whole value.
type ConfigMapData struct {
	Ref             model.ObjectRef  `json:"ref"`
	ResourceVersion string           `json:"resourceVersion"`
	Values          []ConfigMapValue `json:"values"`
	// More counts the keys of data beyond the first ConfigMapDataKeys, which are not returned.
	More int `json:"more,omitempty"`
	// ReadOnly says why no value of this Config Map can be saved: it is immutable, or a Helm
	// release record.
	ReadOnly string `json:"readOnly,omitempty"`
	// Unsaved are the values the user edited in this Config Map's drawer and has not saved
	// (configMap.keepEdits, drafts_drawer.go): for the window only, never for an agent or the CLI.
	Unsaved *ConfigMapEdits `json:"unsaved,omitempty"`
}

// ForAgent replaces the cached Secrets' values in the values an agent reads (H-82).
func (d ConfigMapData) ForAgent(v *AgentView) any {
	d.Unsaved = nil // the window's only; ConfigMapData never fills it for an agent
	d.Values = slices.Clone(d.Values)
	for i := range d.Values {
		d.Values[i].Value = v.Text(d.Values[i].Value)
	}
	return d
}

// ConfigMapDataReview is what configMap.reviewData reports: the diff of the object with the given
// values against the object as the cache holds it, and the server's dry run of the save.
type ConfigMapDataReview struct {
	Ref model.ObjectRef `json:"ref"`
	// UID is the Config Map's the review is of: a save that removes keys acts on it only.
	UID string `json:"uid,omitempty"`
	// Keys are the keys whose values change, KeysAdded the keys added, KeysRemoved the keys removed,
	// each sorted. Against a newer version (Stale) they are what a save over it would write: an
	// added key that appeared there is a change of its value, a removed key gone there is left out.
	Keys        []string   `json:"keys"`
	KeysAdded   []string   `json:"keysAdded,omitempty"`
	KeysRemoved []string   `json:"keysRemoved,omitempty"`
	Changed     bool       `json:"changed"`
	Added       int        `json:"added"`
	Removed     int        `json:"removed"`
	Lines       []DiffLine `json:"lines"`
	// BaseResourceVersion is the version the values are based on; CachedResourceVersion the one
	// the lines compare with. Stale: they differ, and a save based on BaseResourceVersion is
	// refused with a conflict; the lines are the values against the newer version.
	BaseResourceVersion   string `json:"baseResourceVersion"`
	CachedResourceVersion string `json:"cachedResourceVersion"`
	Stale                 bool   `json:"stale,omitempty"`
	// RebasedFrom is the version the values were based on when the Config Map changed since but
	// none of the edited keys did there: the values are rebased onto BaseResourceVersion, the
	// version the cache holds, and the lines and the dry run are against it. A save names
	// BaseResourceVersion.
	RebasedFrom string `json:"rebasedFrom,omitempty"`
	// Accepted: the server accepted the save in a dry run (asked only when something changes and
	// the values are based on the version the cache holds). Refused is its reason when it did not.
	Accepted bool   `json:"accepted,omitempty"`
	Refused  string `json:"refused,omitempty"`
}

// ForAgent replaces the cached Secrets' values in the diff an agent reads (H-82).
func (r ConfigMapDataReview) ForAgent(v *AgentView) any {
	r.Lines = v.DiffLines(r.Lines)
	return r
}

// ConfigMapDataSaved is a successful save: the keys whose values changed, the keys added and
// removed, and the version the server stored.
type ConfigMapDataSaved struct {
	Ref             model.ObjectRef `json:"ref"`
	Keys            []string        `json:"keys"`
	KeysAdded       []string        `json:"keysAdded,omitempty"`
	KeysRemoved     []string        `json:"keysRemoved,omitempty"`
	ResourceVersion string          `json:"resourceVersion"`
}

// DataConflictError reports that a Config Map, or a Secret saved from the drawer
// (secretdataedit.go), changed since the version the values are based on: nothing was written.
// Diff holds a Config Map's values against the version the cache holds now
// (CurrentResourceVersion; empty when the cache has not seen it yet), Changes a Secret's masked
// changes against it, never a value; a save naming that version writes them over it.
type DataConflictError struct {
	Ref                    model.ObjectRef   `json:"ref"`
	BaseResourceVersion    string            `json:"baseResourceVersion"`
	CurrentResourceVersion string            `json:"currentResourceVersion,omitempty"`
	Diff                   []DiffLine        `json:"diff,omitempty"`
	Changes                []SecretKeyChange `json:"changes,omitempty"`
}

func (e *DataConflictError) Error() string {
	if e.CurrentResourceVersion == "" {
		return fmt.Sprintf("%s changed on the cluster since version %s, which the values are based on; nothing was saved. "+
			"Cogpit's cache has not seen the new version yet: review the values again in a moment.", e.Ref.String(), e.BaseResourceVersion)
	}
	if e.Ref.Kind == secretsKind {
		return fmt.Sprintf("%s changed on the cluster since version %s, which the values are based on; nothing was saved. It is at "+
			"version %s now: Save in the drawer shows the changes against it, and Save over version %s writes them over it.",
			e.Ref.String(), e.BaseResourceVersion, e.CurrentResourceVersion, e.CurrentResourceVersion)
	}
	return fmt.Sprintf("%s changed on the cluster since version %s, which the values are based on; nothing was saved. It is at "+
		"version %s now: configMap_reviewData with that resourceVersion shows the values against it, and configMap_saveData "+
		"with it writes them over it.", e.Ref.String(), e.BaseResourceVersion, e.CurrentResourceVersion)
}

// configMapReadOnly says why the values of a Config Map (with its type set) cannot be saved; ""
// when they can.
func configMapReadOnly(obj *unstructured.Unstructured) string {
	if immutable, _ := model.NestedBool(obj, "immutable"); immutable {
		return "The Config Map is immutable: its data cannot change."
	}
	if model.IsHelmRecord(obj) {
		return "The Config Map is a Helm release record, which Helm writes: Helm › Releases shows the release."
	}
	return ""
}

// asConfigMap returns a cached Config Map with its apiVersion and kind, which the cache may leave
// out.
func asConfigMap(obj *unstructured.Unstructured) *unstructured.Unstructured {
	return withTypeMeta(obj, ObjectTarget{APIVersion: "v1", Kind: configMapAPIKind})
}

// configMapTarget resolves ref for the Config Map commands: a core Config Map of the connected
// cluster.
func (s *Session) configMapTarget(ctx context.Context, ref model.ObjectRef) (ObjectTarget, *Backend, error) {
	if ref.Kind != configMapsKind {
		return ObjectTarget{}, nil, fmt.Errorf("%w: this applies to %s", ErrNotApplicable, configMapsKind)
	}
	t, b, err := s.ResolveTarget(ctx, ref)
	if err != nil {
		return ObjectTarget{}, nil, err
	}
	if t.GVR.Group != "" || t.GVR.Resource != configMapsKind {
		return ObjectTarget{}, nil, fmt.Errorf("%w: %s is not a Config Map", ErrNotApplicable, ref.String())
	}
	return t, b, nil
}

// ConfigMapSavable is configMap.saveData's check, from the cache without waiting: the backend can
// write objects, and the Config Map is in the cache with data that may change (a kind still being
// listed passes: the command waits for it when it runs and checks then). The registry adds the
// access reviews' answer on its update verb.
func (s *Session) ConfigMapSavable(ref model.ObjectRef) error {
	if ref.Kind != configMapsKind {
		return fmt.Errorf("%w: this applies to %s", ErrNotApplicable, configMapsKind)
	}
	b, err := s.TargetBackend(ref.Context) // the Config Map's cluster, shown or not (D-69)
	if err != nil {
		return err
	}
	if _, err := b.ObjectWriter(); err != nil {
		return err
	}
	obj, err := s.Lookup(ref)
	switch {
	case errors.Is(err, ErrNotSynced):
		return nil // the command waits for the list and checks the Config Map when it runs
	case err != nil:
		return err
	}
	if r := configMapReadOnly(asConfigMap(obj)); r != "" {
		return errors.New(r)
	}
	return nil
}

// ConfigMapData returns a Config Map's data from the cache, each value whole, with the version it
// is of, and for the window the values the user edited in its drawer and has not saved. A Helm
// release record's payload is never returned (it holds the release's values).
func (s *Session) ConfigMapData(ctx context.Context, ref model.ObjectRef) (ConfigMapData, error) {
	t, b, err := s.configMapTarget(ctx, ref)
	if err != nil {
		return ConfigMapData{}, err
	}
	obj, err := cachedObject(ctx, b, t)
	if err != nil {
		return ConfigMapData{}, err
	}
	obj = withTypeMeta(obj, t)
	out := ConfigMapData{Ref: t.Ref, ResourceVersion: obj.GetResourceVersion(), Values: []ConfigMapValue{}, ReadOnly: configMapReadOnly(obj)}
	if OriginOf(ctx) == OriginGUI {
		out.Unsaved = s.keptConfigMapEdits(t.Ref)
	}
	data := model.NestedMap(obj.Object, "data")
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if len(keys) > ConfigMapDataKeys {
		out.More = len(keys) - ConfigMapDataKeys
		keys = keys[:ConfigMapDataKeys]
	}
	record := model.IsHelmRecord(obj)
	for _, k := range keys {
		v, _ := data[k].(string)
		if record && k == model.HelmRecordKey {
			out.Values = append(out.Values, ConfigMapValue{Key: k, Note: "Helm release record, " + model.FormatBytes(int64(len(v))) +
				" encoded, the release's values included: not shown"})
			continue
		}
		out.Values = append(out.Values, ConfigMapValue{Key: k, Value: v})
	}
	return out, nil
}

// configMapEdit is a checked change of a Config Map's data: its target, the object as the cache
// holds it, and that object with the edit applied as an update sends it.
type configMapEdit struct {
	t       ObjectTarget
	b       *Backend
	w       ObjectWriter
	base    string
	cached  *unstructured.Unstructured
	edited  *unstructured.Unstructured
	changes dataChanges // the keys whose values change, added and removed
	edit    DataEdit    // the edit as given
}

// prepareConfigMapEdit checks a change of a Config Map's data against the Config Map as the cache
// holds it: a changed key must be a key of its data, an added key must not be one at the version
// the edit is based on, a removed key must be one, and its data must be allowed to change. The
// edited object carries base as its resourceVersion precondition.
func (s *Session) prepareConfigMapEdit(ctx context.Context, ref model.ObjectRef, base string, edit DataEdit) (configMapEdit, error) {
	if base == "" {
		return configMapEdit{}, errors.New("give resourceVersion: the version of the Config Map the values are based on " +
			"(configMap_readData returns it); nothing was saved")
	}
	if err := edit.check(); err != nil {
		return configMapEdit{}, err
	}
	if size := edit.size(); size > maxConfigMapValues {
		return configMapEdit{}, fmt.Errorf("the values are %s, more than the %s a Config Map holds; nothing was saved",
			model.FormatBytes(int64(size)), model.FormatBytes(maxConfigMapValues))
	}
	t, b, err := s.configMapTarget(ctx, ref)
	if err != nil {
		return configMapEdit{}, err
	}
	w, err := b.ObjectWriter()
	if err != nil {
		return configMapEdit{}, err
	}
	cached, err := cachedObject(ctx, b, t)
	if err != nil {
		return configMapEdit{}, err
	}
	cached = withTypeMeta(cached, t)
	if r := configMapReadOnly(cached); r != "" {
		return configMapEdit{}, fmt.Errorf("%s: %s Nothing was saved", t.Ref.String(), r)
	}
	// Against the version the edit is based on its keys are checked strictly; against a newer one
	// (a stale review, rebased or shown) as a save over it would write them.
	edited, changes, err := withConfigMapEdit(cached, edit, cached.GetResourceVersion() == base)
	if err != nil {
		return configMapEdit{}, fmt.Errorf("%s: %w", t.Ref.String(), err)
	}
	edited.SetResourceVersion(base)
	return configMapEdit{t: t, b: b, w: w, base: base, cached: cached, edited: edited, changes: changes, edit: edit}, nil
}

// withConfigMapEdit returns a copy of a Config Map with an edit applied to its data, without
// managedFields (the server keeps its own), and what it changes. A changed key its data does not
// hold is an error (a key is added only through add); so is binary data. strict: the object is the
// version the edit is based on, where an added key it holds and a removed key it does not hold are
// errors; else an added key it holds is a change of its value and a removed key it does not hold is
// left out.
func withConfigMapEdit(obj *unstructured.Unstructured, edit DataEdit, strict bool) (*unstructured.Unstructured, dataChanges, error) {
	c := obj.DeepCopy()
	unstructured.RemoveNestedField(c.Object, "metadata", "managedFields")
	data, _, _ := unstructured.NestedMap(c.Object, "data")
	if data == nil {
		data = map[string]any{}
	}
	binary, _, _ := unstructured.NestedMap(c.Object, "binaryData")
	var ch dataChanges
	for _, k := range slices.Sorted(maps.Keys(edit.Values)) {
		old, ok := data[k]
		switch {
		case !ok && binary[k] != nil:
			return nil, ch, fmt.Errorf("%q is binary data, which is not edited here; nothing was saved", k)
		case !ok:
			return nil, ch, fmt.Errorf("its data has no key %q: a new key is given in add, not in data; nothing was saved", k)
		case old != edit.Values[k]:
			ch.changed = append(ch.changed, k)
		}
		data[k] = edit.Values[k]
	}
	for _, k := range slices.Sorted(maps.Keys(edit.Add)) {
		old, ok := data[k]
		switch {
		case binary[k] != nil:
			return nil, ch, fmt.Errorf("%q is a key of the binary data already; nothing was saved", k)
		case ok && strict:
			return nil, ch, fmt.Errorf("its data holds the key %q already: its value changes in data, not in add; nothing was saved", k)
		case ok && old != edit.Add[k]:
			ch.changed = append(ch.changed, k) // appeared in a newer version: a save over it changes its value
		case !ok:
			ch.added = append(ch.added, k)
		}
		data[k] = edit.Add[k]
	}
	for _, k := range slices.Sorted(slices.Values(edit.Remove)) {
		_, ok := data[k]
		switch {
		case !ok && binary[k] != nil:
			return nil, ch, fmt.Errorf("%q is binary data, which is not edited here; nothing was saved", k)
		case !ok && strict:
			return nil, ch, fmt.Errorf("its data has no key %q to remove; nothing was saved", k)
		case !ok:
			continue // gone in a newer version already
		}
		delete(data, k)
		ch.removed = append(ch.removed, k)
	}
	slices.Sort(ch.changed)
	if err := unstructured.SetNestedMap(c.Object, data, "data"); err != nil {
		return nil, ch, err
	}
	return c, ch, nil
}

// ReviewConfigMapData diffs the Config Map with the given edit against the object as the cache
// holds it, keeping context unchanged lines around each change (negative: all), and, when the
// edit changes something and is based on the cached version, asks the server for a dry run of the
// save. edit.From holds the changed and removed keys' values at base: when the Config Map changed
// since base but none of the edit's keys did (an added key still absent), the edit is rebased onto
// the cached version (RebasedFrom), and a dry run that meets a still newer version reviews it once
// more against that one. Without from, or when a key of the edit changed, a newer version is
// reported as Stale with the edit against it.
func (s *Session) ReviewConfigMapData(ctx context.Context, ref model.ObjectRef, base string, edit DataEdit, context int) (ConfigMapDataReview, error) {
	return s.reviewConfigMapData(ctx, ref, base, edit, context, true)
}

func (s *Session) reviewConfigMapData(ctx context.Context, ref model.ObjectRef, base string, edit DataEdit, context int, retry bool) (ConfigMapDataReview, error) {
	if err := guardAgentValues(ctx, edit); err != nil {
		return ConfigMapDataReview{}, err
	}
	e, err := s.prepareConfigMapEdit(ctx, ref, base, edit)
	if err != nil {
		return ConfigMapDataReview{}, err
	}
	rebased := e.rebase(edit.From, plainDataValue)
	lines, err := diffAgainst(e.cached, e.edited)
	if err != nil {
		return ConfigMapDataReview{}, err
	}
	r := ConfigMapDataReview{
		Ref: e.t.Ref, UID: string(e.cached.GetUID()), Keys: orEmpty(e.changes.changed), KeysAdded: e.changes.added,
		KeysRemoved: e.changes.removed, Changed: DiffChanged(lines), Lines: CompactDiff(lines, context),
		BaseResourceVersion: e.base, CachedResourceVersion: e.cached.GetResourceVersion(), RebasedFrom: rebased,
	}
	r.Stale = r.CachedResourceVersion != e.base
	r.Added, r.Removed = diffCounts(lines)
	if !r.Changed || r.Stale {
		return r, nil
	}
	if _, err := e.w.UpdateObject(ctx, e.t.GVR, e.edited, true); err != nil {
		werr := s.configMapWriteError(ctx, e, err)
		var ce *DataConflictError
		switch {
		case errors.As(werr, &ce) && retry && edit.rebasable(edit.From) && ce.CurrentResourceVersion != "":
			// The cache holds the version the server met by now: review against it, which
			// rebases the edit again when none of its keys changed there.
			return s.reviewConfigMapData(ctx, ref, base, edit, context, false)
		case errors.As(werr, &ce):
			r.Stale, r.RebasedFrom, r.BaseResourceVersion = true, "", base
			if ce.CurrentResourceVersion != "" {
				r.CachedResourceVersion, r.Lines = ce.CurrentResourceVersion, ce.Diff
			}
		case apierrors.IsNotFound(err):
			return ConfigMapDataReview{}, werr
		default:
			r.Refused = werr.Error()
		}
		return r, nil
	}
	r.Accepted = true
	return r, nil
}

// rebase moves the edit onto the version the cache holds when it is based on an older one and
// none of its keys changed since (DataEdit.unchangedSince): from holds the changed and removed
// keys' values at the older version, value reads a value of the cached data. It returns the older
// version; "" when the edit stays where it is.
func (e *configMapEdit) rebase(from map[string]string, value func(any) (string, bool)) string {
	cached := e.cached.GetResourceVersion()
	data, binary := model.NestedMap(e.cached.Object, "data"), model.NestedMap(e.cached.Object, "binaryData")
	if cached == e.base || !e.edit.unchangedSince(data, binary, from, value) {
		return ""
	}
	older := e.base
	e.base = cached
	e.edited.SetResourceVersion(cached)
	return older
}

// plainDataValue reads one value of a Config Map's data: the text itself.
func plainDataValue(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

// SaveConfigMapData writes an edit into a Config Map: a dry run first, then the update, both with
// base as the resourceVersion precondition. A Config Map that changed since base (the cache holds
// another version, or the server answers 409) is not written: *DataConflictError says what the
// edit is against the newer version. The caller has checked the confirmation's context;
// SaveConfigMapData checks it again, and for a removal the Config Map the user confirmed (its UID).
func (s *Session) SaveConfigMapData(ctx context.Context, ref model.ObjectRef, base string, edit DataEdit) (ConfigMapDataSaved, error) {
	if err := guardAgentValues(ctx, edit); err != nil {
		return ConfigMapDataSaved{}, err
	}
	e, err := s.prepareConfigMapEdit(ctx, ref, base, edit)
	if err != nil {
		return ConfigMapDataSaved{}, err
	}
	if err := CheckConfirmedContext(ctx, e.b.Context); err != nil {
		return ConfigMapDataSaved{}, err
	}
	if e.cached.GetResourceVersion() != base {
		return ConfigMapDataSaved{}, dataConflict(e, e.cached)
	}
	if e.changes.none() {
		return ConfigMapDataSaved{}, fmt.Errorf("%s: no value differs from version %s; nothing was written", e.t.Ref.String(), base)
	}
	if err := checkRemovalTarget(ctx, e); err != nil {
		return ConfigMapDataSaved{}, err
	}
	if _, err := e.w.UpdateObject(ctx, e.t.GVR, e.edited, true); err != nil {
		return ConfigMapDataSaved{}, s.configMapWriteError(ctx, e, err)
	}
	stored, err := e.w.UpdateObject(ctx, e.t.GVR, e.edited, false)
	if err != nil {
		return ConfigMapDataSaved{}, s.configMapWriteError(ctx, e, err)
	}
	s.Emit(Event{Type: EventToast, Level: LevelInfo, Message: e.t.Kind + " " + e.t.Ref.Key() + " saved"})
	return ConfigMapDataSaved{Ref: e.t.Ref, Keys: orEmpty(e.changes.changed), KeysAdded: e.changes.added, KeysRemoved: e.changes.removed,
		ResourceVersion: stored.GetResourceVersion()}, nil
}

// checkRemovalTarget refuses a save that removes keys from another object than the one the user
// confirmed: a Config Map or a Secret deleted and created again under its name while the
// confirmation was open (its UID, ActUID).
func checkRemovalTarget(ctx context.Context, e configMapEdit) error {
	if len(e.changes.removed) == 0 {
		return nil
	}
	cached := string(e.cached.GetUID())
	uid, err := ActUID(ctx, e.t.Ref, func() (string, bool) { return cached, cached != "" })
	if err != nil {
		return err
	}
	if uid != cached {
		return fmt.Errorf("%s was replaced on the cluster since the removal was confirmed; nothing was saved", e.t.Ref.String())
	}
	return nil
}

// orEmpty returns keys, or an empty list for none.
func orEmpty(keys []string) []string {
	if keys == nil {
		return []string{}
	}
	return keys
}

// ConfigMapDataReviewText is the change an agent's save would write, for the user's confirmation:
// the diff's lines, "- " removed and "+ " added, with a few unchanged lines around each change.
// "" when it cannot be worked out; the save then refuses what it would have shown.
func (s *Session) ConfigMapDataReviewText(ref model.ObjectRef, base string, edit DataEdit) string {
	ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
	defer cancel()
	e, err := s.prepareConfigMapEdit(ctx, ref, base, edit)
	if err != nil {
		return ""
	}
	lines, err := diffAgainst(e.cached, e.edited)
	if err != nil {
		return ""
	}
	return DiffText(CompactDiff(lines, DefaultDiffContext))
}

// DiffText writes diff lines as text: "- " before a removed line, "+ " before an added one, two
// spaces before an unchanged one.
func DiffText(lines []DiffLine) string {
	var b []byte
	for _, l := range lines {
		switch l.Op {
		case DiffRemoved:
			b = append(b, "- "...)
		case DiffAdded:
			b = append(b, "+ "...)
		default:
			b = append(b, "  "...)
		}
		b = append(b, l.Text...)
		b = append(b, '\n')
	}
	return string(b)
}

// diffCounts counts a diff's added and removed lines.
func diffCounts(lines []DiffLine) (added, removed int) {
	for _, l := range lines {
		switch l.Op {
		case DiffAdded:
			added++
		case DiffRemoved:
			removed++
		}
	}
	return added, removed
}

// guardAgentValues refuses an agent's values, changed or added, that hold a placeholder Cogpit
// showed in place of a Secret's value: writing it would replace that value in the cluster (H-82).
func guardAgentValues(ctx context.Context, edit DataEdit) error {
	for _, values := range []map[string]string{edit.Values, edit.Add} {
		for _, v := range values {
			if err := GuardAgentMarkers(ctx, v); err != nil {
				return err
			}
		}
	}
	return nil
}

// dataConflict reports that a Config Map is no longer at the version the edit is based on: current
// is the version the cache holds now, nil when it has not seen it yet.
func dataConflict(e configMapEdit, current *unstructured.Unstructured) error {
	ce := &DataConflictError{Ref: e.t.Ref, BaseResourceVersion: e.base}
	if current == nil {
		return ce
	}
	current = withTypeMeta(current, e.t)
	ce.CurrentResourceVersion = current.GetResourceVersion()
	// A changed key the newer version dropped leaves the diff empty; saving again reports it.
	if edited, _, err := withConfigMapEdit(current, e.edit, false); err == nil {
		if lines, err := diffAgainst(current, edited); err == nil {
			ce.Diff = CompactDiff(lines, DefaultDiffContext)
		}
	}
	return ce
}

// configMapWriteError turns a failed dry run or update into what the caller sees: a
// *DataConflictError for a 409 once the cache holds the newer version (or after conflictWait
// without it), a plain error naming the Config Map otherwise.
func (s *Session) configMapWriteError(ctx context.Context, e configMapEdit, err error) error {
	switch {
	case apierrors.IsNotFound(err):
		return fmt.Errorf("%s no longer exists on the cluster; nothing was saved: %w", e.t.Ref.String(), err)
	case !apierrors.IsConflict(err):
		return fmt.Errorf("%s: %w", e.t.Ref.String(), err)
	}
	current, seen := waitForNewer(ctx, e.b, e.t, e.base, conflictWait)
	switch {
	case !seen:
		return &DataConflictError{Ref: e.t.Ref, BaseResourceVersion: e.base}
	case current == nil:
		return fmt.Errorf("%s was deleted on the cluster since version %s; nothing was saved", e.t.Ref.String(), e.base)
	}
	return dataConflict(e, current)
}
