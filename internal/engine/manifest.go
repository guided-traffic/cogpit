package engine

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utiljson "k8s.io/apimachinery/pkg/util/json"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Limits of the YAML the editor commands take. A manifest is user or agent input: its size is
// capped before it is parsed (conventions: cap buffers sized by input).
const (
	MaxManifestBytes     = 4 << 20 // 4 MiB of YAML
	MaxManifestDocuments = 100     // documents in one Create
)

// ErrManifest marks YAML that is not a valid manifest: it does not parse, is not an object, or
// lacks apiVersion, kind or name. Match it with errors.Is.
var ErrManifest = errors.New("invalid manifest")

// ParseManifests splits YAML text into its documents ("---") and parses each into an object.
// Documents with only comments or whitespace are skipped; a v1 List contributes its items. Every
// object has a non-empty apiVersion and kind and, if present, a metadata mapping; names are the
// caller's to check. Any input yields objects or an error wrapping ErrManifest, never a panic.
func ParseManifests(text string) (objs []*unstructured.Unstructured, err error) {
	defer func() {
		if r := recover(); r != nil {
			objs, err = nil, fmt.Errorf("%w: the YAML could not be parsed (%v)", ErrManifest, r)
		}
	}()
	if len(text) > MaxManifestBytes {
		return nil, fmt.Errorf("%w: larger than the %s an editor takes", ErrManifest, model.FormatBytes(MaxManifestBytes))
	}
	r := utilyaml.NewYAMLReader(bufio.NewReader(strings.NewReader(text)))
	for n := 1; ; n++ {
		doc, rerr := r.Read()
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			return nil, fmt.Errorf("%w: document %d: %v", ErrManifest, n, rerr)
		}
		if blankYAML(doc) {
			continue
		}
		obj, perr := parseDocument(doc)
		if perr != nil {
			return nil, fmt.Errorf("%w: document %d: %v", ErrManifest, n, perr)
		}
		items, lerr := listItems(obj)
		if lerr != nil {
			return nil, fmt.Errorf("%w: document %d: %v", ErrManifest, n, lerr)
		}
		objs = append(objs, items...)
		if len(objs) > MaxManifestDocuments {
			return nil, fmt.Errorf("%w: more than %d objects in one text", ErrManifest, MaxManifestDocuments)
		}
	}
	if len(objs) == 0 {
		return nil, fmt.Errorf("%w: the text holds no object", ErrManifest)
	}
	return objs, nil
}

// blankYAML reports whether a document holds only whitespace, comments and document markers.
func blankYAML(doc []byte) bool {
	for _, line := range bytes.Split(doc, []byte("\n")) {
		t := bytes.TrimSpace(line)
		if len(t) == 0 || t[0] == '#' || bytes.Equal(t, []byte("---")) || bytes.Equal(t, []byte("...")) {
			continue
		}
		return false
	}
	return true
}

func parseDocument(doc []byte) (*unstructured.Unstructured, error) {
	j, err := yaml.YAMLToJSON(doc)
	if err != nil {
		return nil, err
	}
	var v any
	// apimachinery's JSON decoding keeps integers as int64, as unstructured content needs.
	if err := utiljson.Unmarshal(j, &v); err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("not a YAML mapping (an object with apiVersion, kind and metadata)")
	}
	return checkObject(m)
}

// stringMetadata are the metadata fields that must be strings when present: the engine reads and
// sets them, and unstructured's setters silently skip a field of the wrong type.
var stringMetadata = []string{metadataName, "generateName", "namespace", "resourceVersion", "uid"}

const metadataName = "name"

func checkObject(m map[string]any) (*unstructured.Unstructured, error) {
	av, _ := m["apiVersion"].(string)
	kind, _ := m["kind"].(string)
	if strings.TrimSpace(av) == "" {
		return nil, errors.New("apiVersion is missing")
	}
	if strings.TrimSpace(kind) == "" {
		return nil, errors.New("kind is missing")
	}
	if md, ok := m["metadata"]; ok && md != nil {
		mm, ok := md.(map[string]any)
		if !ok {
			return nil, errors.New("metadata is not a mapping")
		}
		for _, f := range stringMetadata {
			if x, ok := mm[f]; ok && x != nil {
				if _, ok := x.(string); !ok {
					return nil, fmt.Errorf("metadata.%s is not a string", f)
				}
			}
		}
	}
	return &unstructured.Unstructured{Object: m}, nil
}

// listItems expands a v1 List into its items, each checked like a document; any other object is
// returned as it is.
func listItems(obj *unstructured.Unstructured) ([]*unstructured.Unstructured, error) {
	if obj.GetAPIVersion() != "v1" || obj.GetKind() != "List" {
		return []*unstructured.Unstructured{obj}, nil
	}
	raw, ok := obj.Object["items"].([]any)
	if !ok {
		return nil, errors.New("a List without items")
	}
	out := make([]*unstructured.Unstructured, 0, len(raw))
	for i, it := range raw {
		m, ok := it.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("item %d is not an object", i+1)
		}
		o, err := checkObject(m)
		if err != nil {
			return nil, fmt.Errorf("item %d: %v", i+1, err)
		}
		if o.GetKind() == "List" {
			return nil, fmt.Errorf("item %d: a List inside a List", i+1)
		}
		out = append(out, o)
	}
	return out, nil
}

// ParseManifest parses text that must hold exactly one object, as an Edit tab's does.
func ParseManifest(text string) (*unstructured.Unstructured, error) {
	objs, err := ParseManifests(text)
	if err != nil {
		return nil, err
	}
	if len(objs) != 1 {
		return nil, fmt.Errorf("%w: the text holds %d objects; an edit changes one", ErrManifest, len(objs))
	}
	return objs[0], nil
}

// EditableYAML is an object as the editor shows it: its YAML without managedFields (the cache
// drops them already; a server response still has them), in the Kubernetes order (apiVersion,
// kind, metadata first: model.ObjectYAML, D-86). It returns an error, never a panic, for content
// that YAML cannot represent.
func EditableYAML(obj *unstructured.Unstructured) (string, error) {
	c := obj.DeepCopy()
	unstructured.RemoveNestedField(c.Object, "metadata", "managedFields")
	b, err := model.ObjectYAML(c.Object)
	if err != nil {
		return "", fmt.Errorf("the object cannot be shown as YAML: %w", err)
	}
	return string(b), nil
}

// kindSecret is the Kind whose values Cogpit redacts.
const kindSecret = "Secret"

// RedactedKind reports whether Cogpit redacts the YAML of a kind (Secrets: model.Redact). Such
// objects are edited and created from YAML in the GUI only, where the editor shows their values
// (D-19, secretedit.go); agents and the CLI never see or write them.
func RedactedKind(apiVersion, kind string) bool {
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return kind == kindSecret // an apiVersion that does not parse: refuse rather than guess
	}
	// The normalised form: "/v1" and "v1" name the same core group.
	probe := &unstructured.Unstructured{Object: map[string]any{"apiVersion": gv.String(), "kind": kind}}
	return model.Redact(probe) != probe
}

// CompactDiff keeps the changed lines of a diff and up to context unchanged lines around each
// change; the line numbers of the lines it keeps show where it left lines out. A negative context
// keeps every line. A diff without changes compacts to nothing.
func CompactDiff(lines []DiffLine, context int) []DiffLine {
	if context < 0 {
		return lines
	}
	if !DiffChanged(lines) {
		return []DiffLine{}
	}
	keep := make([]bool, len(lines))
	for i, l := range lines {
		if l.Op == DiffSame {
			continue
		}
		for j := max(0, i-context); j <= min(len(lines)-1, i+context); j++ {
			keep[j] = true
		}
	}
	out := make([]DiffLine, 0, len(lines))
	for i, l := range lines {
		if keep[i] {
			out = append(out, l)
		}
	}
	return out
}
