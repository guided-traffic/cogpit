package helm

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	chartcommon "helm.sh/helm/v4/pkg/chart/common"
	chartutil "helm.sh/helm/v4/pkg/chart/common/util"
	releaseutil "helm.sh/helm/v4/pkg/release/v1/util"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/guided-traffic/cogpit/internal/model"
)

// A release's revisions (the Revisions dock tab, D-131): each revision's
// values, user-supplied and all, and its rendered manifest, from the release record Helm keeps
// per revision, which the cache holds (the Releases kind's scope). As `helm get values|manifest
// --revision N` reads them, without asking the cluster. Values and a rendered Secret's data can
// hold credentials (H-8): the engine withholds them from agents.

// RecordName is the name of the record (a Secret, or a ConfigMap) Helm keeps one revision of a
// release in: "sh.helm.release.v1.<release>.v<revision>", as both drivers name it.
func RecordName(release string, revision int) string {
	return "sh.helm.release.v1." + release + ".v" + strconv.Itoa(revision)
}

// Revision is one revision of a release: its chart, status and description, its values and its
// rendered manifest, split into objects.
type Revision struct {
	Revision     int       `json:"revision"`
	Chart        string    `json:"chart"`
	ChartVersion string    `json:"chartVersion"`
	AppVersion   string    `json:"appVersion,omitempty"`
	Status       string    `json:"status"` // Helm's own spelling: "deployed", "failed"
	Updated      time.Time `json:"updated,omitzero"`
	Description  string    `json:"description,omitempty"`
	// Values is the revision's values as YAML: the user-supplied ones, or with All every value,
	// the chart's defaults coalesced as Helm renders them.
	Values string `json:"values"`
	All    bool   `json:"all"`
	// Manifest is the rendered manifest as Helm stored it; Objects its objects, in the order Helm
	// installs them, at most maxResources (Cut says there were more).
	Manifest string           `json:"manifest"`
	Objects  []ManifestObject `json:"objects"`
	Cut      bool             `json:"cut,omitempty"`

	values map[string]any // the values YAML is written from, for ValueChanges
}

// ManifestObject is one object of a rendered manifest.
type ManifestObject struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
	// Source is the template that rendered it, from Helm's "# Source:" line.
	Source string `json:"source,omitempty"`
	// YAML is the object in the Kubernetes docs' order (model.ObjectYAML).
	YAML string `json:"yaml"`
	// Secret marks a Secret, whose data and stringData the window masks until Show; Masked is its
	// YAML so masked (MaskSecret).
	Secret bool   `json:"secret,omitempty"`
	Masked string `json:"masked,omitempty"`
}

// Key identifies an object across revisions: its kind (by group), namespace and name.
func (o ManifestObject) Key() string {
	group, _, _ := strings.Cut(o.APIVersion, "/")
	if !strings.Contains(o.APIVersion, "/") {
		group = ""
	}
	return o.Kind + "." + group + "/" + o.Namespace + "/" + o.Name
}

// ErrRevisionGone says a release has no record of a revision: Helm pruned it (it keeps ten by
// default) or it never was.
var ErrRevisionGone = errors.New("the cluster keeps no record of this revision")

// RevisionOf decodes one revision's record (a Secret or a ConfigMap, from the cache). With all its
// Values hold every value, the chart's defaults included. The payload is untrusted input: a
// decoder that fails or panics is an error that carries no content of the record.
func RevisionOf(record *unstructured.Unstructured, all bool) (r Revision, err error) {
	defer func() {
		if p := recover(); p != nil {
			r, err = Revision{}, ErrNoValuesPayload
		}
	}()
	rls, err := DecodeRecord(record)
	if err != nil {
		return Revision{}, ErrNoValuesPayload
	}
	r = Revision{Revision: rls.Version, All: all, Manifest: rls.Manifest}
	if rls.Info != nil {
		r.Status, r.Updated = string(rls.Info.Status), rls.Info.LastDeployed
	}
	if rls.Chart != nil && rls.Chart.Metadata != nil {
		r.Chart, r.ChartVersion, r.AppVersion = rls.Chart.Metadata.Name, rls.Chart.Metadata.Version, rls.Chart.Metadata.AppVersion
	}
	if rls.Info != nil { // scrubbed as the release object's history has it (decodeRevision)
		public := []string{rls.Name, rls.Namespace, r.Chart, r.ChartVersion, r.AppVersion, chartText(r.Chart, r.ChartVersion)}
		r.Description = ScrubValuesExcept(rls.Info.Description, rls.Config, public)
	}
	r.values = rls.Config
	if all && rls.Chart != nil {
		vals, err := chartutil.CoalesceValues(rls.Chart, rls.Config)
		if err != nil {
			return Revision{}, errors.New("the release's values could not be merged with its chart's defaults")
		}
		r.values = vals
	}
	if len(r.values) > 0 {
		y, err := yaml.Marshal(r.values)
		if err != nil {
			return Revision{}, errors.New("the release's values could not be written as YAML")
		}
		r.Values = string(y)
	}
	r.Objects, r.Cut = ManifestObjects(rls.Manifest, rls.Namespace)
	return r, nil
}

// ManifestObjects returns the objects of a rendered manifest in the order Helm installs them
// (Helm's InstallOrder by kind, kinds it does not know after, by name), at most maxResources, and
// whether there were more. An object without a namespace is in the release's namespace unless its
// kind is a built-in cluster-scoped one. A document that does not parse, or is no object, is
// skipped: the manifest is untrusted input.
func ManifestObjects(manifest, releaseNamespace string) ([]ManifestObject, bool) {
	var out []ManifestObject
	cut := false
	for _, d := range splitDocuments(manifest) {
		if strings.TrimSpace(d) == "" {
			continue
		}
		if len(out) >= maxResources {
			cut = true
			break
		}
		var obj map[string]any
		if err := yaml.Unmarshal([]byte(d), &obj); err != nil || obj == nil {
			continue
		}
		u := &unstructured.Unstructured{Object: obj}
		o := ManifestObject{APIVersion: u.GetAPIVersion(), Kind: u.GetKind(), Name: u.GetName(), Namespace: u.GetNamespace(), Source: sourceOf(d)}
		if o.Kind == "" || o.Name == "" {
			continue
		}
		if _, namespaced, known := model.BuiltinKindKey(o.APIVersion, o.Kind); known && !namespaced {
			o.Namespace = ""
		} else if o.Namespace == "" {
			o.Namespace = releaseNamespace
		}
		y, err := model.ObjectYAML(obj)
		if err != nil {
			continue
		}
		o.YAML = string(y)
		if o.Kind == "Secret" && o.APIVersion == "v1" {
			o.Secret = true
			o.Masked = MaskSecret(obj, MaskSize)
		}
		out = append(out, o)
	}
	order := map[string]int{}
	for i, k := range releaseutil.InstallOrder {
		order[k] = i
	}
	rank := func(kind string) int {
		if i, ok := order[kind]; ok {
			return i
		}
		return len(order)
	}
	slices.SortStableFunc(out, func(a, b ManifestObject) int {
		return cmp.Or(cmp.Compare(rank(a.Kind), rank(b.Kind)), cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Name, b.Name))
	})
	return out, cut
}

// sourceOf returns the template a manifest document names on its "# Source:" line.
func sourceOf(doc string) string {
	for line := range strings.Lines(doc) {
		t := strings.TrimSpace(line)
		if s, ok := strings.CutPrefix(t, "# Source:"); ok {
			return strings.TrimSpace(s)
		}
		if t != "" && !strings.HasPrefix(t, "#") {
			return ""
		}
	}
	return ""
}

// MaskSize is how the window shows a Secret's value until Show: dots, as the Secret drawer shows
// them, and its size.
func MaskSize(n int) string {
	if n == 1 {
		return MaskedDots + " (1 byte)"
	}
	return MaskedDots + " (" + strconv.Itoa(n) + " bytes)"
}

// MaskedDots stand for a Secret's value, as the Secret drawer shows them.
const MaskedDots = "••••••••"

// MaskSecret returns a Secret's YAML (model.ObjectYAML) with each value of data and stringData
// replaced by mask(its size in bytes): data's decoded, stringData's as written.
func MaskSecret(obj map[string]any, mask func(n int) string) string {
	out := maps.Clone(obj)
	for _, field := range []string{"data", "stringData"} {
		m, ok := obj[field].(map[string]any)
		if !ok {
			continue
		}
		masked := make(map[string]any, len(m))
		for k, v := range m {
			s := fmt.Sprint(v)
			n := len(s)
			if field == "data" {
				if b, err := base64.StdEncoding.DecodeString(s); err == nil {
					n = len(b)
				}
			}
			masked[k] = mask(n)
		}
		out[field] = masked
	}
	y, err := model.ObjectYAML(out)
	if err != nil {
		return ""
	}
	return string(y)
}

// MaskManifest returns a rendered manifest with each Secret's data and stringData values replaced
// by mask(size) (MaskSecret); its other documents as they are. A Secret document is written anew in
// the Kubernetes docs' order under its "# Source:" line.
func MaskManifest(manifest string, mask func(n int) string) string {
	docs := splitDocuments(manifest)
	var b strings.Builder
	for i, d := range docs {
		if i > 0 {
			b.WriteString("---\n")
		}
		var obj map[string]any
		if yaml.Unmarshal([]byte(d), &obj) == nil && obj["kind"] == "Secret" && obj["apiVersion"] == "v1" {
			if s := sourceOf(d); s != "" {
				b.WriteString("# Source: " + s + "\n")
			}
			b.WriteString(MaskSecret(obj, mask))
			continue
		}
		b.WriteString(d)
	}
	return b.String()
}

// ValueChange is one value that differs between two revisions: its dotted key path, and its value
// in each as YAML on one line, "" where it is not set.
type ValueChange struct {
	Path string `json:"path"`
	From string `json:"from"`
	To   string `json:"to"`
}

// maxValueChanges bounds the changed keys listed; ValueChanges says when there were more.
const maxValueChanges = 1000

// ValueChanges returns the values that differ from revision from to revision to, by key path in
// order; a list or a map that is set on one side only is one change, lists on both sides are
// compared element by element ("hosts[1]").
func ValueChanges(from, to Revision) ([]ValueChange, bool) {
	var out []ValueChange
	walkChanges("", from.values, to.values, &out)
	slices.SortFunc(out, func(a, b ValueChange) int { return cmp.Compare(a.Path, b.Path) })
	if len(out) > maxValueChanges {
		return out[:maxValueChanges], true
	}
	return out, false
}

func walkChanges(path string, a, b map[string]any, out *[]ValueChange) {
	keys := slices.Sorted(maps.Keys(a))
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	for _, k := range keys {
		p := k
		if path != "" {
			p = path + "." + k
		}
		av, aok := a[k]
		bv, bok := b[k]
		changeAt(p, av, aok, bv, bok, out)
	}
}

// changeAt compares one value of both revisions: maps key by key, lists element by element
// ("hosts[1].host"), anything else whole.
func changeAt(p string, av any, aok bool, bv any, bok bool, out *[]ValueChange) {
	am, amap := asMap(av)
	bm, bmap := asMap(bv)
	al, alist := av.([]any)
	bl, blist := bv.([]any)
	switch {
	case amap && bmap:
		walkChanges(p, am, bm, out)
	case alist && blist:
		for i := range max(len(al), len(bl)) {
			var x, y any
			if i < len(al) {
				x = al[i]
			}
			if i < len(bl) {
				y = bl[i]
			}
			changeAt(p+"["+strconv.Itoa(i)+"]", x, i < len(al), y, i < len(bl), out)
		}
	case aok && bok && reflect.DeepEqual(av, bv):
	default:
		c := ValueChange{Path: p}
		if aok {
			c.From = oneLine(av)
		}
		if bok {
			c.To = oneLine(bv)
		}
		*out = append(*out, c)
	}
}

// oneLine is a value on one line, cut at 200 characters: a scalar as YAML writes it, a string
// with a line break quoted, a list or a map as JSON, which YAML reads too.
func oneLine(v any) string {
	var b []byte
	var err error
	switch x := v.(type) {
	case map[string]any, chartcommon.Values, []any:
		b, err = json.Marshal(x)
	case string:
		if strings.Contains(x, "\n") {
			b = []byte(strconv.Quote(x))
			break
		}
		b, err = yaml.Marshal(x)
	default:
		b, err = yaml.Marshal(x)
	}
	if err != nil {
		return "?"
	}
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// asMap returns v as a map of values: Helm's coalesced values nest as chartcommon.Values.
func asMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case chartcommon.Values:
		return m, true
	}
	return nil, false
}

// The marks of an object compared between two revisions.
const (
	ObjectAdded   = "+" // in the revision shown, not in the one compared
	ObjectRemoved = "-" // in the one compared, not in the revision shown
	ObjectChanged = "~"
	ObjectSame    = "="
)

// ObjectChange is an object of either revision with its mark.
type ObjectChange struct {
	ManifestObject
	Change string `json:"change"`
	// Was is the object as the compared revision rendered it; empty for an added one.
	Was string `json:"was,omitempty"`
}

// ObjectChanges marks the objects of revision to (the one shown) against revision from (the one
// compared): added, changed or the same, in to's order, and those from rendered that to does not
// after them, removed. Objects are matched by Key and compared by their YAML.
func ObjectChanges(from, to []ManifestObject) []ObjectChange {
	was := make(map[string]ManifestObject, len(from))
	for _, o := range from {
		was[o.Key()] = o
	}
	out := make([]ObjectChange, 0, len(to))
	seen := map[string]bool{}
	for _, o := range to {
		k := o.Key()
		seen[k] = true
		c := ObjectChange{ManifestObject: o, Change: ObjectAdded}
		if w, ok := was[k]; ok {
			c.Was = w.YAML
			c.Change = ObjectSame
			if w.YAML != o.YAML {
				c.Change = ObjectChanged
			}
		}
		out = append(out, c)
	}
	for _, o := range from {
		if !seen[o.Key()] {
			out = append(out, ObjectChange{ManifestObject: o, Change: ObjectRemoved, Was: o.YAML})
		}
	}
	return out
}

// Withhold returns the revision as an agent reads it: its values, and each rendered Secret's data
// and stringData values, replaced by text; the manifest's other documents as they are.
func (r Revision) Withhold(text string) Revision {
	mask := func(int) string { return text }
	r.values = nil
	if r.Values != "" {
		r.Values = text
	}
	r.Manifest = MaskManifest(r.Manifest, mask)
	r.Objects = slices.Clone(r.Objects)
	for i, o := range r.Objects {
		if o.Secret {
			r.Objects[i].YAML = MaskSecretYAML(o.YAML, mask)
			r.Objects[i].Masked = r.Objects[i].YAML
		}
	}
	return r
}

// MaskSecretYAML is MaskSecret on a Secret's YAML; a text that does not parse is masked whole.
func MaskSecretYAML(text string, mask func(int) string) string {
	var obj map[string]any
	if err := yaml.Unmarshal([]byte(text), &obj); err != nil || obj == nil {
		return mask(len(text))
	}
	return MaskSecret(obj, mask)
}
