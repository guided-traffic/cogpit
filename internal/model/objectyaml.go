package model

import (
	"bytes"
	"slices"
	"strings"

	"sigs.k8s.io/yaml"
)

// The order Cogpit writes an object's YAML in (the owner's WF15, D-86): as the Kubernetes docs
// write a manifest, apiVersion, kind and metadata first, metadata's name and namespace first in
// it; the body's fields after them in YAML's sorted order, status last. Below the top level and
// metadata every map keeps YAML's sorted order, as before.
var (
	objectFirst   = []string{"apiVersion", "kind", "metadata"}
	objectLast    = []string{"status"}
	metadataFirst = []string{"name", "generateName", "namespace", "labels", "annotations"}
)

// ObjectYAML is an object's YAML in the Kubernetes order (objectFirst, metadataFirst, objectLast).
// Each top-level field (and metadata's) is written by sigs.k8s.io/yaml on its own, so every
// scalar is quoted exactly as yaml.Marshal of the whole object quotes it ("on", "yes" and "0x1f"
// stay strings when the text is parsed again); only the order differs. It is display order only:
// parsing the text gives the same object.
func ObjectYAML(obj map[string]any) ([]byte, error) {
	var b bytes.Buffer
	for _, k := range orderedKeys(obj, objectFirst, objectLast) {
		if meta, ok := obj[k].(map[string]any); ok && k == "metadata" && len(meta) > 0 {
			b.WriteString("metadata:\n")
			for _, mk := range orderedKeys(meta, metadataFirst, nil) {
				y, err := yaml.Marshal(map[string]any{mk: meta[mk]})
				if err != nil {
					return nil, err
				}
				indentYAML(&b, y)
			}
			continue
		}
		y, err := yaml.Marshal(map[string]any{k: obj[k]})
		if err != nil {
			return nil, err
		}
		b.Write(y)
	}
	if len(obj) == 0 {
		return yaml.Marshal(obj) // "{}\n", as before
	}
	return b.Bytes(), nil
}

// orderedKeys are m's keys: those of first that m holds, in first's order, then the others
// sorted, then those of last that m holds.
func orderedKeys(m map[string]any, first, last []string) []string {
	out := make([]string, 0, len(m))
	for _, k := range first {
		if _, ok := m[k]; ok {
			out = append(out, k)
		}
	}
	var rest []string
	for k := range m {
		if !slices.Contains(first, k) && !slices.Contains(last, k) {
			rest = append(rest, k)
		}
	}
	slices.Sort(rest)
	out = append(out, rest...)
	for _, k := range last {
		if _, ok := m[k]; ok && !slices.Contains(first, k) {
			out = append(out, k)
		}
	}
	return out
}

// indentYAML writes y, one mapping entry's YAML, two spaces in: an entry of metadata. Empty lines
// stay empty, so a block scalar's empty lines keep their meaning.
func indentYAML(b *bytes.Buffer, y []byte) {
	for line := range strings.SplitAfterSeq(string(y), "\n") {
		if strings.TrimRight(line, "\n") != "" {
			b.WriteString("  ")
		}
		b.WriteString(line)
	}
}
