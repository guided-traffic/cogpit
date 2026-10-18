package engine

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"unicode"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The Secrets drawer (D-13 revised 2026-10-12, H-8). The cache holds Secrets in full: the Secrets
// view watches them with their values, so the drawer reads the Secret it shows from the cache like
// any other object and makes no request of its own. The drawer model lists every key with its
// value masked and its size, for the GUI and agents alike; secret.reveal hands one value of the
// Secret the drawer shows to the user's own window and refuses every other origin. Values never
// reach the drawer model, the YAML (model.Redact), logs, errors, toasts or the audit log; the one
// exception is object.get for the built-in agent over a connection the user marked private, which
// reads like the user (readLikeUser below, D-10, H-94).

// RevealCommand is the command a masked Secret value's "Show" button runs: GUI-only.
const RevealCommand = "secret.reveal"

// secretsKind is the kind key of core Secrets.
const secretsKind = "secrets"

// secretDataTitle is the title of a Secret drawer's section of keys.
const secretDataTitle = "Data"

// Caps on what the drawer lists and a reveal shows. A Secret holds up to 1 MiB; the drawer lists
// keys and shows one value at a time, so a reveal shows only the beginning of a value.
const (
	secretValueBytes = 4 << 10 // bytes of one value a reveal shows
	secretValueLines = 40      // lines of one value a reveal shows
	secretKeysShown  = 100     // keys the drawer lists
)

// isSecrets reports whether a profile is the core Secrets kind (not a virtual kind made from
// Secrets, such as Helm's releases).
func isSecrets(p *model.KindProfile) bool {
	return p != nil && p.Group == "" && p.Resource == secretsKind && p.APIObject()
}

// secretDataSection is a Secret's Data section in the drawer: each key, sorted (at most
// secretKeysShown), its value masked with its size and a "Show" action beside a text value, a
// binary value by its size only. It reads the values to measure them and to tell text from binary,
// and keeps none of them.
func secretDataSection(ref model.ObjectRef, obj *unstructured.Unstructured) model.DetailSection {
	sec := model.DetailSection{Title: secretDataTitle}
	data := model.NestedMap(obj.Object, "data")
	names := make([]string, 0, len(data))
	for k := range data {
		names = append(names, k)
	}
	slices.Sort(names)
	if len(names) == 0 {
		sec.Items = []model.DetailItem{{Value: "No data"}}
		return sec
	}
	for i, k := range names {
		if i == secretKeysShown {
			sec.Items = append(sec.Items, model.DetailItem{Value: strconv.Itoa(len(names)-i) + " more keys are not listed"})
			break
		}
		size, text := measure(data[k])
		it := model.DetailItem{Name: k}
		if text {
			it.Value = MaskedValue + " (" + byteSize(size) + ")"
			params, _ := json.Marshal(RevealParams{Ref: ref, Key: k, ResourceVersion: obj.GetResourceVersion()})
			it.Actions = []model.ItemAction{{Label: "Show", Command: RevealCommand, Params: params}}
		} else {
			it.Value = "binary data, " + byteSize(size)
		}
		sec.Items = append(sec.Items, it)
	}
	return sec
}

// measure returns the size of one value of a Secret's data (the API sends base64) and whether it
// can be shown as text. A value that is not base64 counts as binary. The decoded bytes are zeroed
// before it returns.
func measure(v any) (size int, text bool) {
	raw, ok := decodeValue(v)
	if !ok {
		s, _ := v.(string)
		return base64.StdEncoding.DecodedLen(len(s)), false
	}
	defer wipeBytes(raw)
	return len(raw), isText(raw)
}

// decodeValue decodes one value of a Secret's data; the caller zeroes the bytes.
func decodeValue(v any) ([]byte, bool) {
	s, ok := v.(string)
	if !ok {
		return nil, false
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, false
	}
	return raw, true
}

// isText reports whether a value can be shown as text: valid UTF-8 without control characters
// other than tabs and line breaks.
func isText(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	for _, r := range string(b) {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	return true
}

// clip returns the beginning of a text value within the caps, cut at a line or character boundary.
func clip(b []byte) []byte {
	if len(b) > secretValueBytes {
		cut := secretValueBytes
		for cut > 0 && !utf8.RuneStart(b[cut]) {
			cut--
		}
		b = b[:cut]
	}
	lines := 0
	for i, c := range b {
		if c == '\n' {
			if lines++; lines == secretValueLines {
				return b[:i]
			}
		}
	}
	return b
}

// wipeBytes overwrites a decoded value before it is dropped: it would otherwise stay in memory
// until the collector reuses it (best effort: the cached object's base64 text and a revealed
// value's string are Go strings, which cannot be overwritten).
func wipeBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// MaskedValue is what the drawer shows in place of a Secret's value.
const MaskedValue = "••••••••"

// RevealParams are secret.reveal's parameters: the Secret the drawer shows and one of its keys.
type RevealParams struct {
	Ref model.ObjectRef `json:"ref" jsonschema:"the Secret the drawer shows: kind secrets, its namespace and name"`
	Key string          `json:"key" jsonschema:"the key whose value to show"`
	// ResourceVersion is the Secret's version the drawer listed the key at: a Secret changed or
	// created again since is refused, and the drawer's revealed value of an older version no
	// longer matches the action (R-6 of the fourth run's review).
	ResourceVersion string `json:"resourceVersion,omitempty" jsonschema:"the Secret's resourceVersion the drawer listed the key at; a Secret changed since is refused"`
	// Whole asks for the value uncut: the drawer's editor holds what Save writes back (D-59).
	Whole bool `json:"whole,omitempty" jsonschema:"the whole value, not cut at 4 KiB and 40 lines: what the drawer's editor needs to save it"`
}

// SecretValue is one revealed value, for the user's own window only.
type SecretValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Size  int    `json:"size"`          // the whole value's size in bytes
	Cut   bool   `json:"cut,omitempty"` // Value is the beginning only (4 KiB, 40 lines)
}

// ErrNoSecretData refuses a reveal for a Secret the drawer does not show.
var ErrNoSecretData = errors.New("a Secret's values are shown only for the Secret open in the drawer")

// drawerShows reports whether the drawer is open on ref.
func (s *Session) drawerShows(ref model.ObjectRef) bool {
	st := s.state
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.drawer != nil && st.drawer.ref.Same(ref)
}

// secretValueOf returns one value of the Secret the drawer shows, decoded, from the cache; the
// caller zeroes it. With a version, a Secret the cache holds at another one is refused
// (ErrTargetsChanged). It never puts a value into its error.
func (s *Session) secretValueOf(p RevealParams) ([]byte, error) {
	ref, key := p.Ref, p.Key
	if ref.Kind != secretsKind || !s.drawerShows(ref) {
		return nil, ErrNoSecretData
	}
	obj, err := s.Lookup(ref)
	if err != nil {
		return nil, err
	}
	if p.ResourceVersion != "" && obj.GetResourceVersion() != p.ResourceVersion {
		return nil, fmt.Errorf("%w: the Secret changed since the drawer listed its keys; show the value again", ErrTargetsChanged)
	}
	v, ok := model.NestedMap(obj.Object, "data")[key]
	if !ok {
		return nil, fmt.Errorf("the Secret has no key %q", key)
	}
	raw, ok := decodeValue(v)
	if !ok || !isText(raw) {
		wipeBytes(raw)
		return nil, errors.New("binary values are shown by their size only")
	}
	return raw, nil
}

// SecretKeyShown reports, without a value, whether the drawer shows ref and the Secret has a text
// value under key in the cache: secret.reveal's Enabled check.
func (s *Session) SecretKeyShown(p RevealParams) error {
	raw, err := s.secretValueOf(p)
	wipeBytes(raw)
	return err
}

// SecretValue returns one value of the Secret the drawer shows, from the cache, cut to 4 KiB and
// 40 lines unless the whole value is asked for (the drawer's editor, D-59). The caller
// (secret.reveal) hands it to the GUI only.
func (s *Session) SecretValue(p RevealParams) (SecretValue, error) {
	raw, err := s.secretValueOf(p)
	if err != nil {
		return SecretValue{}, err
	}
	defer wipeBytes(raw)
	shown := raw
	if !p.Whole {
		shown = clip(raw)
	}
	return SecretValue{Key: p.Key, Value: string(shown), Size: len(raw), Cut: len(shown) < len(raw)}, nil
}

// readLikeUser gives a turn of the built-in agent over a connection the user marked private what
// the user can read of a Secret (D-10, H-94; the owner's decision of 2026-10-14 late evening, "the
// chat reads like the user"): the drawer's Data section with each text value as the drawer's Show
// reveals it (the first 4 KiB and 40 lines, the cut said), a binary value by its size, and the
// YAML as the object's Edit tab holds it, its values unredacted (base64 in data). A Helm release
// record kept in a ConfigMap gets its YAML likewise (its payload is the release's values, which
// helm.release.values gives the private turn decoded). Only Dispatch's private turn reaches it
// (PrivateTurn): the window's own drawer keeps the values masked until the user clicks Show, every
// other caller reads them redacted. The items are marked private, so that a model passed through
// model.WithholdPrivate withholds them again. m is the model detail built for this read alone.
func readLikeUser(p *model.KindProfile, obj *unstructured.Unstructured, m *DetailModel) {
	if obj == nil || m == nil {
		return
	}
	secret := isSecrets(p)
	if secret {
		if i := slices.IndexFunc(m.Sections, func(s model.DetailSection) bool { return s.Title == secretDataTitle }); i >= 0 {
			m.Sections = slices.Clone(m.Sections)
			m.Sections[i] = secretValuesSection(obj)
		}
	}
	if secret || model.IsHelmRecord(obj) {
		if y, err := model.ObjectYAML(obj.Object); err == nil { // the Kubernetes order (D-86)
			m.YAML = string(y)
		}
	}
}

// secretValuesSection is a Secret's Data section with its text values shown (readLikeUser): each
// key, sorted (at most secretKeysShown), its value as a reveal shows it, a binary one by its size.
func secretValuesSection(obj *unstructured.Unstructured) model.DetailSection {
	sec := model.DetailSection{Title: secretDataTitle}
	data := model.NestedMap(obj.Object, "data")
	names := make([]string, 0, len(data))
	for k := range data {
		names = append(names, k)
	}
	slices.Sort(names)
	if len(names) == 0 {
		sec.Items = []model.DetailItem{{Value: "No data"}}
		return sec
	}
	for i, k := range names {
		if i == secretKeysShown {
			sec.Items = append(sec.Items, model.DetailItem{Value: strconv.Itoa(len(names)-i) + " more keys are not listed"})
			break
		}
		sec.Items = append(sec.Items, model.DetailItem{Name: k, Value: shownValue(data[k]), Private: true})
	}
	return sec
}

// shownValue is one value of a Secret's data as a reveal shows it, the decoded bytes zeroed before
// it returns: the text cut at 4 KiB and 40 lines with the cut said, or a binary value by its size.
func shownValue(v any) string {
	raw, ok := decodeValue(v)
	if !ok || !isText(raw) {
		size, _ := measure(v)
		wipeBytes(raw)
		return "binary data, " + byteSize(size)
	}
	defer wipeBytes(raw)
	shown := clip(raw)
	if len(shown) == len(raw) {
		return string(shown)
	}
	return string(shown) + "\n[the first " + byteSize(len(shown)) + " of " + byteSize(len(raw)) +
		"; the YAML's data holds the whole value, base64-encoded]"
}

// byteSize renders a value's size as every byte amount is (D-32): "7 B", "4.0 KiB", "1.5 MiB".
func byteSize(n int) string { return model.FormatBytes(int64(n)) }
