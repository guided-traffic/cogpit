package helm

import (
	"cmp"
	"slices"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The release drawer's Resources: the objects a release's manifest renders, by kind, as Lens lists
// them (captured: one table of Name and Namespace per kind, with links). They come from the newest
// revision's manifest in the cached release Secret, not from the cluster: what the release says it
// owns.

// resource is one object of a release's manifest.
type resource struct {
	apiVersion, kind, namespace, name string
}

// manifestHead is what manifestResources reads of a document.
type manifestHead struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Items []manifestHead `json:"items"`
}

// manifestResources returns the objects of a rendered manifest, at most maxResources, sorted by
// kind and name. An object without a namespace is in the release's namespace, unless its kind is a
// built-in cluster-scoped kind (Helm installs namespaced objects into the release's namespace). A
// document that does not parse is skipped: the manifest is untrusted input.
func manifestResources(manifest, releaseNamespace string) []resource {
	var out []resource
	add := func(h manifestHead) {
		if h.Kind == "" || h.Metadata.Name == "" || len(out) >= maxResources {
			return
		}
		ns := h.Metadata.Namespace
		if _, namespaced, known := model.BuiltinKindKey(h.APIVersion, h.Kind); known && !namespaced {
			ns = ""
		} else if ns == "" {
			ns = releaseNamespace
		}
		out = append(out, resource{apiVersion: h.APIVersion, kind: h.Kind, namespace: ns, name: h.Metadata.Name})
	}
	for _, d := range splitDocuments(manifest) {
		if strings.TrimSpace(d) == "" || len(out) >= maxResources {
			continue
		}
		var h manifestHead
		if err := yaml.Unmarshal([]byte(d), &h); err != nil {
			continue
		}
		if strings.HasSuffix(h.Kind, "List") && h.Metadata.Name == "" {
			for _, it := range h.Items {
				add(it)
			}
			continue
		}
		add(h)
	}
	slices.SortFunc(out, func(a, b resource) int {
		return cmp.Or(cmp.Compare(a.kind, b.kind), cmp.Compare(a.namespace, b.namespace), cmp.Compare(a.name, b.name))
	})
	return slices.CompactFunc(out, func(a, b resource) bool { return a == b })
}

// resourceSections renders resources as the drawer's Resources group: one section per kind,
// titled by the kind, with a table of Name (a link for built-in kinds) and Namespace (a link).
func resourceSections(rs []resource) []model.DetailSection {
	columns := []model.Column{
		{ID: fieldName, Title: "Name", Type: model.ColumnText},
		{ID: fieldNamespace, Title: titleNamespace, Type: model.ColumnText},
	}
	var out []model.DetailSection
	for i := 0; i < len(rs); {
		j := i
		var rows [][]model.Cell
		for ; j < len(rs) && rs[j].kind == rs[i].kind; j++ {
			r := rs[j]
			name := model.Cell{Text: r.name, Link: model.RefTo(r.apiVersion, r.kind, r.namespace, r.name)}
			ns := model.Cell{Text: r.namespace}
			if r.namespace != "" {
				ns.Link = &model.ObjectRef{Kind: kindNamespaces, Name: r.namespace}
			}
			rows = append(rows, []model.Cell{name, ns})
		}
		out = append(out, model.DetailSection{Title: rs[i].kind, Group: GroupResources, Table: model.CellTable(columns, rows)})
		i = j
	}
	return out
}

// splitDocuments splits a manifest into its YAML documents at the separators, lines that start
// with "---", as Helm does (`(?m)^---[ \t]*`): what follows the dashes and blanks on the
// separator's line is the start of the next document ("--- {kind: Secret, ...}"). The documents
// are substrings of text, so that a large manifest is not copied line by line.
func splitDocuments(text string) []string {
	var docs []string
	bodyStart := 0
	for pos := 0; pos < len(text); {
		end := len(text)
		if i := strings.IndexByte(text[pos:], '\n'); i >= 0 {
			end = pos + i + 1
		}
		if rest, ok := strings.CutPrefix(text[pos:end], "---"); ok {
			docs = append(docs, text[bodyStart:pos])
			content := strings.TrimLeft(rest, " \t")
			bodyStart = end
			if c := strings.TrimSpace(content); c != "" && !strings.HasPrefix(c, "#") {
				bodyStart = end - len(content)
			}
		}
		pos = end
	}
	return append(docs, text[bodyStart:])
}
