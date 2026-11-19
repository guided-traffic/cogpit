package engine

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"unicode/utf8"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// A Secret's values edited in the drawer, in Cogpit's own window only (the owner's audit answer;
// decisions D-59). The drawer shows a Secret's keys masked; Show reveals one value (secret.reveal,
// GUI-only) into an editor below its key, as a Config Map's values are edited (D-55), and Save at
// the end of the data reviews every edited value and writes them at once. secret.reviewData shows a
// masked diff (the keys that change and their sizes, never a value) and the server's dry run;
// secret.saveData writes, as configMap.saveData writes a Config Map: the Secret from the cache with
// the values set, base64-encoded in data, a dry run, then the update with the version the values
// are based on as precondition; a Secret that changed since is a *DataConflictError with the masked
// changes against the newer version, written over only by a save that names it. Given the edited
// keys' values at the base version (from), the review rebases the edit onto a newer version none of
// those keys changed in, as a Config Map's does.
//
// Both commands are GUI-only (Spec.GUIOnly): Dispatch refuses every other origin before anything
// is read, asked or sent, so no agent, no CLI call and no MCP client reaches a path that writes a
// Secret's value, and each checks the origin again itself. An agent's refused call is audited with
// the Secret and the keys only (auditSecretDataParams). No value reaches an error (the checks name
// keys; an API message that quotes a value is scrubbed), a toast, a log, a draft (the window
// keeps unsaved values in its memory only) or the result. Keys are added (their values typed as
// text, stored base64-encoded) and removed in the same write (add and remove, the owner's audit
// decision; datakeys.go); a save that removes one is destructive, and the masked diff lists it. A
// binary value is not edited (it can be removed), a Helm release record is not edited, and an
// immutable Secret is refused.

// The ids of the drawer's Secret commands.
const (
	SecretReviewDataCommand = "secret.reviewData"
	SecretSaveDataCommand   = "secret.saveData"
)

// What a key of a masked diff does.
const (
	SecretKeyChanged = "changed"
	SecretKeyAdded   = "added"
	SecretKeyRemoved = "removed"
)

// maxSecretValues bounds the values one save carries: the API server keeps a Secret's data within
// 1 MiB.
const maxSecretValues = 1 << 20

// ErrSecretDataGUIOnly refuses an agent or the CLI the drawer's Secret commands (D-59). It matches
// ErrSecretGUIOnly with errors.Is.
var ErrSecretDataGUIOnly error = secretDataRefusal{}

type secretDataRefusal struct{}

func (secretDataRefusal) Error() string {
	return "a Secret's values are changed only by the user, in Cogpit's own window (the drawer's Save, D-59): agents and " +
		"the CLI cannot review or write them"
}

func (secretDataRefusal) Is(target error) bool { return target == ErrSecretGUIOnly }

// SecretKeyChange is one key of a masked diff: what changes and the value's size before and after,
// never the value.
type SecretKeyChange struct {
	Key      string `json:"key"`
	Change   string `json:"change"`             // SecretKeyChanged, SecretKeyAdded or SecretKeyRemoved
	Bytes    int    `json:"bytes,omitempty"`    // the value's size before
	NewBytes int    `json:"newBytes,omitempty"` // and after
}

// SecretDataReview is what secret.reviewData reports: the masked diff of the Secret with the given
// values against the Secret as the cache holds it, and the server's dry run of the save.
type SecretDataReview struct {
	Ref model.ObjectRef `json:"ref"`
	// UID is the Secret's the review is of: a save that removes keys acts on it only.
	UID string `json:"uid,omitempty"`
	// Changes are the keys changed, added and removed, with their sizes; against a newer version
	// (Stale) what a save over it would write (an added key that appeared there is changed, a
	// removed key gone there is left out).
	Changes []SecretKeyChange `json:"changes"`
	// BaseResourceVersion is the version the values are based on, CachedResourceVersion the one
	// the changes compare with; Stale and RebasedFrom as in ConfigMapDataReview.
	BaseResourceVersion   string `json:"baseResourceVersion"`
	CachedResourceVersion string `json:"cachedResourceVersion"`
	Stale                 bool   `json:"stale,omitempty"`
	RebasedFrom           string `json:"rebasedFrom,omitempty"`
	Accepted              bool   `json:"accepted,omitempty"`
	Refused               string `json:"refused,omitempty"`
}

// SecretDataSaved is a successful save: the keys whose values changed, the keys added and removed,
// and the version stored.
type SecretDataSaved struct {
	Ref             model.ObjectRef `json:"ref"`
	Keys            []string        `json:"keys"`
	KeysAdded       []string        `json:"keysAdded,omitempty"`
	KeysRemoved     []string        `json:"keysRemoved,omitempty"`
	ResourceVersion string          `json:"resourceVersion"`
}

// secretReadOnly says why the values of a Secret (with its type set) cannot be saved from the
// drawer; "" when they can.
func secretReadOnly(obj *unstructured.Unstructured) string {
	if immutable, _ := model.NestedBool(obj, "immutable"); immutable {
		return "The Secret is immutable: its data cannot change."
	}
	if t, _ := obj.Object["type"].(string); t == helmReleaseSecretType {
		return "The Secret is a Helm release record, which Helm writes: Helm › Releases shows the release."
	}
	return ""
}

// requireGUI refuses every origin but the GUI the drawer's Secret commands; Dispatch has refused
// them already (GUI-only), this keeps the functions safe on their own.
func requireGUI(ctx context.Context) error {
	if OriginOf(ctx) != OriginGUI {
		return ErrSecretDataGUIOnly
	}
	return nil
}

// secretTarget resolves ref for the drawer's Secret commands: a core Secret of the connected
// cluster.
func (s *Session) secretTarget(ctx context.Context, ref model.ObjectRef) (ObjectTarget, *Backend, error) {
	if ref.Kind != secretsKind {
		return ObjectTarget{}, nil, fmt.Errorf("%w: this applies to %s", ErrNotApplicable, secretsKind)
	}
	t, b, err := s.ResolveTarget(ctx, ref)
	if err != nil {
		return ObjectTarget{}, nil, err
	}
	if t.GVR.Group != "" || t.GVR.Resource != secretsKind {
		return ObjectTarget{}, nil, fmt.Errorf("%w: %s is not a Secret", ErrNotApplicable, ref.String())
	}
	return t, b, nil
}

// SecretSavable is secret.saveData's check, from the cache without waiting: the backend can write
// objects, and the Secret is in the cache with values that may change. The registry adds the
// access reviews' answer on its update verb. It never names a value.
func (s *Session) SecretSavable(ref model.ObjectRef) error {
	if ref.Kind != secretsKind {
		return fmt.Errorf("%w: this applies to %s", ErrNotApplicable, secretsKind)
	}
	b, err := s.TargetBackend(ref.Context) // the Secret's cluster, shown or not (D-69)
	if err != nil {
		return err
	}
	if _, err := b.ObjectWriter(); err != nil {
		return err
	}
	obj, err := s.Lookup(ref)
	switch {
	case errors.Is(err, ErrNotSynced):
		return nil // the command waits for the list and checks the Secret when it runs
	case err != nil:
		return err
	}
	if r := secretReadOnly(withTypeMeta(obj, ObjectTarget{APIVersion: "v1", Kind: "Secret"})); r != "" {
		return errors.New(r)
	}
	return nil
}

// secretEdit is a checked change of a Secret's values: as configMapEdit, the values given as the
// editors hold them (plain text).
type secretEdit struct {
	configMapEdit
}

// prepareSecretEdit checks a change of a Secret's data against the Secret as the cache holds it:
// the window's own call for the Secret the drawer shows, each changed key a text value of its data,
// an added key not one of them at the version the edit is based on, a removed key one, its data
// allowed to change. The edited object carries the values base64-encoded and base as its
// resourceVersion precondition. No error names a value.
func (s *Session) prepareSecretEdit(ctx context.Context, ref model.ObjectRef, base string, edit DataEdit) (secretEdit, error) {
	if err := requireGUI(ctx); err != nil {
		return secretEdit{}, err
	}
	if ref.Kind == secretsKind && !s.drawerShows(ref) {
		return secretEdit{}, errors.New("a Secret's values are saved only from the drawer that shows it; nothing was saved")
	}
	if base == "" {
		return secretEdit{}, errors.New("give resourceVersion: the version of the Secret the values are based on; nothing was saved")
	}
	if err := edit.check(); err != nil {
		return secretEdit{}, err
	}
	if size := edit.size(); size > maxSecretValues {
		return secretEdit{}, fmt.Errorf("the values are %s, more than the %s a Secret holds; nothing was saved",
			model.FormatBytes(int64(size)), model.FormatBytes(maxSecretValues))
	}
	t, b, err := s.secretTarget(ctx, ref)
	if err != nil {
		return secretEdit{}, err
	}
	w, err := b.ObjectWriter()
	if err != nil {
		return secretEdit{}, err
	}
	cached, err := cachedObject(ctx, b, t)
	if err != nil {
		return secretEdit{}, err
	}
	cached = withTypeMeta(cached, t)
	if r := secretReadOnly(cached); r != "" {
		return secretEdit{}, fmt.Errorf("%s: %s Nothing was saved", t.Ref.String(), r)
	}
	// Against the version the edit is based on its keys are checked strictly; against a newer one
	// as a save over it would write them (withConfigMapEdit).
	edited, changes, err := withSecretEdit(cached, edit, cached.GetResourceVersion() == base)
	if err != nil {
		return secretEdit{}, fmt.Errorf("%s: %w", t.Ref.String(), err)
	}
	edited.SetResourceVersion(base)
	return secretEdit{configMapEdit{t: t, b: b, w: w, base: base, cached: cached, edited: edited, changes: changes, edit: edit}}, nil
}

// withSecretEdit returns a copy of a Secret with an edit applied to its data, the values
// base64-encoded, without managedFields, and what it changes. A changed key its data does not hold,
// a changed value that is not text and a new value that is not text are errors that name the key
// only; strict as withConfigMapEdit's.
func withSecretEdit(obj *unstructured.Unstructured, edit DataEdit, strict bool) (*unstructured.Unstructured, dataChanges, error) {
	c := obj.DeepCopy()
	unstructured.RemoveNestedField(c.Object, "metadata", "managedFields")
	data, _, _ := unstructured.NestedMap(c.Object, secretDataField)
	if data == nil {
		data = map[string]any{}
	}
	var ch dataChanges
	for _, k := range slices.Sorted(maps.Keys(edit.Values)) {
		old, ok := data[k]
		if !ok {
			return nil, ch, fmt.Errorf("its data has no key %q: a new key is given in add, not in data; nothing was saved", k)
		}
		raw, ok := decodeValue(old)
		text := ok && isText(raw)
		wipeBytes(raw)
		if !text {
			return nil, ch, fmt.Errorf("the value of %q is binary data, which is not edited here; nothing was saved", k)
		}
		enc, err := encodeSecretText(k, edit.Values[k])
		if err != nil {
			return nil, ch, err
		}
		if old != enc {
			ch.changed = append(ch.changed, k)
		}
		data[k] = enc
	}
	for _, k := range slices.Sorted(maps.Keys(edit.Add)) {
		enc, err := encodeSecretText(k, edit.Add[k])
		if err != nil {
			return nil, ch, err
		}
		old, ok := data[k]
		switch {
		case ok && strict:
			return nil, ch, fmt.Errorf("its data holds the key %q already: its value changes in data, not in add; nothing was saved", k)
		case ok && old != enc:
			ch.changed = append(ch.changed, k) // appeared in a newer version: a save over it changes its value
		case !ok:
			ch.added = append(ch.added, k)
		}
		data[k] = enc
	}
	for _, k := range slices.Sorted(slices.Values(edit.Remove)) {
		_, ok := data[k]
		switch {
		case !ok && strict:
			return nil, ch, fmt.Errorf("its data has no key %q to remove; nothing was saved", k)
		case !ok:
			continue // gone in a newer version already
		}
		delete(data, k)
		ch.removed = append(ch.removed, k)
	}
	slices.Sort(ch.changed)
	if err := unstructured.SetNestedMap(c.Object, data, secretDataField); err != nil {
		return nil, ch, err
	}
	return c, ch, nil
}

// encodeSecretText encodes a value typed as text for a Secret's data; a value that is not text is
// an error naming the key only.
func encodeSecretText(key, value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("the new value of %q is not text; nothing was saved", key)
	}
	return base64.StdEncoding.EncodeToString([]byte(value)), nil
}

// encodedFrom returns the values the edit is based on as the Secret's data holds them,
// base64-encoded, so that the rebase compares encoded texts and decodes nothing.
func encodedFrom(from map[string]string) map[string]string {
	if len(from) == 0 {
		return nil
	}
	out := make(map[string]string, len(from))
	for k, v := range from {
		out[k] = base64.StdEncoding.EncodeToString([]byte(v))
	}
	return out
}

// maskedChanges lists the keys whose values differ between two Secrets' data, sorted, each with
// its sizes: the diff the user reviews, which holds no value.
func maskedChanges(before, after *unstructured.Unstructured) []SecretKeyChange {
	old, now := model.NestedMap(before.Object, secretDataField), model.NestedMap(after.Object, secretDataField)
	keys := slices.Sorted(maps.Keys(old))
	for k := range now {
		if _, ok := old[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	out := []SecretKeyChange{}
	for _, k := range keys {
		a, inOld := old[k]
		b, inNew := now[k]
		switch {
		case !inNew:
			size, _ := measure(a)
			out = append(out, SecretKeyChange{Key: k, Change: SecretKeyRemoved, Bytes: size})
		case !inOld:
			size, _ := measure(b)
			out = append(out, SecretKeyChange{Key: k, Change: SecretKeyAdded, NewBytes: size})
		default:
			if c, ok := changedValue(k, a, b); ok {
				out = append(out, c)
			}
		}
	}
	return out
}

// changedValue compares one key's values, decoded and zeroed after: a change with both sizes, or
// false when the values are the same.
func changedValue(key string, a, b any) (SecretKeyChange, bool) {
	ra, okA := decodeValue(a)
	rb, okB := decodeValue(b)
	defer wipeBytes(ra)
	defer wipeBytes(rb)
	if okA && okB && bytes.Equal(ra, rb) {
		return SecretKeyChange{}, false
	}
	if !okA || !okB { // not base64: compare as stored
		if sa, sb := fmt.Sprint(a), fmt.Sprint(b); sa == sb {
			return SecretKeyChange{}, false
		}
	}
	return SecretKeyChange{Key: key, Change: SecretKeyChanged, Bytes: len(ra), NewBytes: len(rb)}, true
}

// ReviewSecretData shows the masked diff of the Secret with the given edit against the Secret as
// the cache holds it and, when the edit changes something and is based on the cached version, asks
// the server for a dry run of the save. edit.From holds the changed and removed keys' values at
// base: a newer version none of the edit's keys changed in rebases the edit (RebasedFrom), as
// ReviewConfigMapData does. For the window only.
func (s *Session) ReviewSecretData(ctx context.Context, ref model.ObjectRef, base string, edit DataEdit) (SecretDataReview, error) {
	return s.reviewSecretData(ctx, ref, base, edit, true)
}

func (s *Session) reviewSecretData(ctx context.Context, ref model.ObjectRef, base string, edit DataEdit, retry bool) (SecretDataReview, error) {
	e, err := s.prepareSecretEdit(ctx, ref, base, edit)
	if err != nil {
		return SecretDataReview{}, err
	}
	rebased := e.rebase(encodedFrom(edit.From), plainDataValue) // compares the encoded texts: nothing decoded
	r := SecretDataReview{
		Ref: e.t.Ref, UID: string(e.cached.GetUID()), Changes: maskedChanges(e.cached, e.edited),
		BaseResourceVersion: e.base, CachedResourceVersion: e.cached.GetResourceVersion(), RebasedFrom: rebased,
	}
	r.Stale = r.CachedResourceVersion != e.base
	if len(r.Changes) == 0 || r.Stale {
		return r, nil
	}
	if _, err := e.w.UpdateObject(ctx, e.t.GVR, e.edited, true); err != nil {
		werr := s.secretWriteError(ctx, e, err)
		var ce *DataConflictError
		switch {
		case errors.As(werr, &ce) && retry && edit.rebasable(edit.From) && ce.CurrentResourceVersion != "":
			return s.reviewSecretData(ctx, ref, base, edit, false)
		case errors.As(werr, &ce):
			r.Stale, r.RebasedFrom, r.BaseResourceVersion = true, "", base
			if ce.CurrentResourceVersion != "" {
				r.CachedResourceVersion, r.Changes = ce.CurrentResourceVersion, ce.Changes
			}
		case apierrors.IsNotFound(err):
			return SecretDataReview{}, werr
		default:
			r.Refused = werr.Error()
		}
		return r, nil
	}
	r.Accepted = true
	return r, nil
}

// SaveSecretData writes an edit into a Secret: a dry run first, then the update, both with base as
// the resourceVersion precondition. A Secret that changed since base is not written: a
// *DataConflictError holds the masked changes against the newer version. A removal acts only on
// the Secret the user confirmed (its UID). For the window only.
func (s *Session) SaveSecretData(ctx context.Context, ref model.ObjectRef, base string, edit DataEdit) (SecretDataSaved, error) {
	e, err := s.prepareSecretEdit(ctx, ref, base, edit)
	if err != nil {
		return SecretDataSaved{}, err
	}
	if err := CheckConfirmedContext(ctx, e.b.Context); err != nil {
		return SecretDataSaved{}, err
	}
	if e.cached.GetResourceVersion() != base {
		return SecretDataSaved{}, secretConflict(e, e.cached)
	}
	if e.changes.none() {
		return SecretDataSaved{}, fmt.Errorf("%s: no value differs from version %s; nothing was written", e.t.Ref.String(), base)
	}
	if err := checkRemovalTarget(ctx, e.configMapEdit); err != nil {
		return SecretDataSaved{}, err
	}
	if _, err := e.w.UpdateObject(ctx, e.t.GVR, e.edited, true); err != nil {
		return SecretDataSaved{}, s.secretWriteError(ctx, e, err)
	}
	stored, err := e.w.UpdateObject(ctx, e.t.GVR, e.edited, false)
	if err != nil {
		return SecretDataSaved{}, s.secretWriteError(ctx, e, err)
	}
	s.Emit(Event{Type: EventToast, Level: LevelInfo, Message: e.t.Kind + " " + e.t.Ref.Key() + " saved"})
	return SecretDataSaved{Ref: e.t.Ref, Keys: orEmpty(e.changes.changed), KeysAdded: e.changes.added, KeysRemoved: e.changes.removed,
		ResourceVersion: stored.GetResourceVersion()}, nil
}

// secretConflict reports that a Secret is no longer at the version the edit is based on, with the
// masked changes of the edit against current (nil: the cache has not seen it yet).
func secretConflict(e secretEdit, current *unstructured.Unstructured) error {
	ce := &DataConflictError{Ref: e.t.Ref, BaseResourceVersion: e.base}
	if current == nil {
		return ce
	}
	current = withTypeMeta(current, e.t)
	ce.CurrentResourceVersion = current.GetResourceVersion()
	if edited, _, err := withSecretEdit(current, e.edit, false); err == nil {
		ce.Changes = maskedChanges(current, edited)
	}
	return ce
}

// secretWriteError turns a failed dry run or update into what the window sees, with no value in
// it: a *DataConflictError for a 409 once the cache holds the newer version (or after
// conflictWait without it), else the error with the Secret's values scrubbed from its text.
func (s *Session) secretWriteError(ctx context.Context, e secretEdit, err error) error {
	switch {
	case apierrors.IsNotFound(err):
		return fmt.Errorf("%s no longer exists on the cluster; nothing was saved", e.t.Ref.String())
	case !apierrors.IsConflict(err):
		return fmt.Errorf("%s: %w", e.t.Ref.String(), scrubSecretWrite(err, mergedForScrub(e)))
	}
	current, seen := waitForNewer(ctx, e.b, e.t, e.base, conflictWait)
	switch {
	case !seen:
		return &DataConflictError{Ref: e.t.Ref, BaseResourceVersion: e.base}
	case current == nil:
		return fmt.Errorf("%s was deleted on the cluster since version %s; nothing was saved", e.t.Ref.String(), e.base)
	}
	return secretConflict(e, current)
}

// mergedForScrub is the edited Secret with the cached values beside the new ones, so that an API
// message quoting either is scrubbed.
func mergedForScrub(e secretEdit) *unstructured.Unstructured {
	m := e.edited.DeepCopy()
	data := model.NestedMap(m.Object, secretDataField)
	for k, v := range model.NestedMap(e.cached.Object, secretDataField) {
		data["was."+k] = v
	}
	m.Object[secretDataField] = data
	return m
}

// The parameters of secret.reviewData and secret.saveData the audit log keeps as they are.
var auditedSecretDataFields = []string{paramResourceVersion}

// The drawer's commands' parameters: the version the values are based on, the values at it, the
// keys added and the keys removed.
const (
	paramResourceVersion = "resourceVersion"
	paramFrom            = "from"
	paramAdd             = "add"
	paramRemove          = "remove"
)

// auditSecretDataParams returns the drawer's Secret commands' parameters as the audit log keeps
// them (an agent's refused call: the window's own are not audited): the Secret, the version, the
// keys of data, add and from with each value replaced by model.Redacted, the keys to remove, and
// only the names of any other parameters, never their contents.
func auditSecretDataParams(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var params map[string]any
	if json.Unmarshal(raw, &params) != nil {
		return json.RawMessage(fmt.Sprintf(`"<%d bytes that are not the drawer's Secret parameters; not logged>"`, len(raw)))
	}
	out := map[string]any{}
	var others []string
	for _, k := range slices.Sorted(maps.Keys(params)) {
		switch v := params[k]; {
		case k == ParamRef:
			out[k] = auditRef(v)
		case slices.Contains(auditedSecretDataFields, k):
			if str, ok := v.(string); ok {
				out[k] = str
			} else {
				out[k] = fmt.Sprintf("<a %T; not logged>", v)
			}
		case k == secretDataField || k == paramFrom || k == paramAdd:
			out[k] = auditMapKeys(v)
		case k == paramRemove:
			out[k] = auditKeyList(v)
		default:
			others = append(others, k)
		}
	}
	if len(others) > 0 {
		out["otherParams"] = others
	}
	return marshalParams(out, json.RawMessage(`"<not logged>"`))
}

// auditRef keeps an object reference's string fields; anything else by its type.
func auditRef(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return fmt.Sprintf("<a %T; not logged>", v)
	}
	out := map[string]any{}
	for _, f := range []string{LinkContext, columnKind, columnNamespace, metadataName} {
		if s, ok := m[f].(string); ok {
			out[f] = s
		}
	}
	return out
}

// auditKeyList keeps a list of keys (the keys removed, which hold no value); anything else by its
// type.
func auditKeyList(v any) any {
	list, ok := v.([]any)
	if !ok {
		return fmt.Sprintf("<a %T, not a list of keys; not logged>", v)
	}
	out := make([]any, 0, len(list))
	for _, k := range list {
		if s, ok := k.(string); ok {
			out = append(out, s)
		} else {
			out = append(out, fmt.Sprintf("<a %T; not logged>", k))
		}
	}
	return out
}

// auditMapKeys keeps the keys of a map of values and replaces every value.
func auditMapKeys(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return fmt.Sprintf("<a %T, not a map of values; not logged>", v)
	}
	out := make(map[string]any, len(m))
	for k := range m {
		out[k] = model.Redacted
	}
	return out
}

// secretDataAudited reports whether a command's audit line keeps the drawer's Secret parameters'
// keys only.
func secretDataAudited(id string) bool {
	return id == SecretReviewDataCommand || id == SecretSaveDataCommand
}
