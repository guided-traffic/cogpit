package engine

import (
	"slices"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// A backup's scope as the Velero page reads it (velero.go): from a Backup's spec or a Schedule's
// template, the namespaces it names with Velero's own matching (its include and exclude lists are
// globs: `*`, `?`, `[a-z]`; `*` or an empty include list is every namespace; an exclude wins),
// and whether a label selector or a resource filter narrows it to some of a namespace's objects.
// Velero 1.17 and 1.18 match the same way (pkg/util/collections, gobwas/glob without separators);
// 1.18 rejects `{`, `}` and `!` in namespace patterns, 1.17 takes them, so the matcher knows
// them too.

// veleroScope is a spec's scope.
type veleroScope struct {
	included, excluded []string
	selector           string // the label selectors as text ("app=postgres", "a=1 or b=2"); "" without
	resources          string // the resource filters as text ("persistentvolumeclaims", "not secrets"); "" without
}

// veleroScopeOf reads a BackupSpec (a Backup's spec, a Schedule's spec.template, a Restore's spec).
func veleroScopeOf(spec map[string]any) veleroScope {
	sc := veleroScope{
		included: stringsAt(spec, "includedNamespaces"),
		excluded: stringsAt(spec, "excludedNamespaces"),
	}
	var sels []string
	if ls, ok := spec["labelSelector"].(map[string]any); ok {
		if t := selectorText(ls); t != "" {
			sels = append(sels, t)
		}
	}
	if ors, ok := spec["orLabelSelectors"].([]any); ok {
		for _, o := range ors {
			if ls, ok := o.(map[string]any); ok {
				if t := selectorText(ls); t != "" {
					sels = append(sels, t)
				}
			}
		}
	}
	sc.selector = strings.Join(sels, " or ")
	sc.resources = resourceFilterText(spec)
	return sc
}

// partial reports whether the scope takes only some of a namespace's objects.
func (sc veleroScope) partial() bool { return sc.selector != "" || sc.resources != "" }

// all reports whether the include list names every namespace.
func (sc veleroScope) all() bool { return len(sc.included) == 0 || slices.Contains(sc.included, "*") }

// covers reports whether a namespace is in the scope's namespaces, as Velero decides it.
func (sc veleroScope) covers(ns string) bool {
	if globsMatch(sc.excluded, ns) {
		return false
	}
	return sc.all() || globsMatch(sc.included, ns)
}

// excludes reports whether the exclude list names the namespace.
func (sc veleroScope) excludes(ns string) bool { return globsMatch(sc.excluded, ns) }

// touches reports whether the scope concerns any of the namespaces (none: every one): the page's
// namespace filter.
func (sc veleroScope) touches(nsSet map[string]struct{}) bool {
	if len(nsSet) == 0 {
		return true
	}
	for ns := range nsSet {
		if sc.covers(ns) {
			return true
		}
	}
	return false
}

// text is the scope in a few words: "all namespaces", "all namespaces − 3", "shop", "shop, billing
// +2", each with its selector and resource filter after a middle dot ("database · app=postgres").
func (sc veleroScope) text() string {
	var base string
	switch {
	case sc.all() && len(sc.excluded) > 0:
		base = "all namespaces − " + strconv.Itoa(len(sc.excluded))
	case sc.all():
		base = "all namespaces"
	default:
		base = strings.Join(sc.included[:min(2, len(sc.included))], ", ")
		if more := len(sc.included) - 2; more > 0 {
			base += " +" + strconv.Itoa(more)
		}
		if len(sc.excluded) > 0 {
			base += " − " + strconv.Itoa(len(sc.excluded))
		}
	}
	parts := []string{base}
	if sc.selector != "" {
		parts = append(parts, sc.selector)
	}
	if sc.resources != "" {
		parts = append(parts, sc.resources)
	}
	return strings.Join(parts, " · ")
}

// key identifies the scope, for comparing two backups' (a manual backup done again).
func (sc veleroScope) key() string {
	return strings.Join(sc.included, ",") + "|" + strings.Join(sc.excluded, ",") + "|" + sc.selector + "|" + sc.resources
}

// stringsAt is a string list of a map, nil when absent.
func stringsAt(m map[string]any, key string) []string {
	raw, _ := m[key].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// selectorText writes a metav1.LabelSelector as kubectl does: "app=postgres,tier in (db,cache)".
func selectorText(ls map[string]any) string {
	var parts []string
	if ml, ok := ls["matchLabels"].(map[string]any); ok {
		keys := make([]string, 0, len(ml))
		for k := range ml {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			v, _ := ml[k].(string)
			parts = append(parts, k+"="+v)
		}
	}
	exprs, _ := ls["matchExpressions"].([]any)
	for _, e := range exprs {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		key, _ := m["key"].(string)
		vals := strings.Join(stringsAt(m, "values"), ",")
		switch op, _ := m["operator"].(string); op {
		case "In":
			parts = append(parts, key+" in ("+vals+")")
		case "NotIn":
			parts = append(parts, key+" notin ("+vals+")")
		case "Exists":
			parts = append(parts, key)
		case "DoesNotExist":
			parts = append(parts, "!"+key)
		}
	}
	return strings.Join(parts, ",")
}

// resourceFilterText writes a spec's resource filters, old style (includedResources,
// excludedResources) and new (included/excludedNamespaceScopedResources), in a few words: the
// included kinds, "not" and the excluded ones. Cluster-scoped filters leave a namespace's objects
// alone and are not written. "" when every kind is in.
func resourceFilterText(spec map[string]any) string {
	var inc []string
	for _, k := range []string{"includedResources", "includedNamespaceScopedResources"} {
		for _, r := range stringsAt(spec, k) {
			if r != "*" {
				inc = append(inc, r)
			}
		}
	}
	exc := append(stringsAt(spec, "excludedResources"), stringsAt(spec, "excludedNamespaceScopedResources")...)
	var parts []string
	if len(inc) > 0 {
		parts = append(parts, strings.Join(inc, ","))
	}
	if len(exc) > 0 {
		parts = append(parts, "not "+strings.Join(exc, ","))
	}
	return strings.Join(parts, " ")
}

// globsMatch reports whether any pattern matches s.
func globsMatch(patterns []string, s string) bool {
	for _, p := range patterns {
		if globMatch(p, s) {
			return true
		}
	}
	return false
}

// globMatch matches s against a glob as gobwas/glob does without separators: `*` any run of
// characters, `?` one, `[abc]`, `[a-z]` and `[!a-z]` a class, `{a,b}` alternatives, `\` escapes.
// A malformed pattern matches nothing, as Velero's compile error makes it.
func globMatch(pattern, s string) bool {
	for _, alt := range expandBraces(pattern) {
		if ok, valid := globMatchRunes([]rune(alt), []rune(s)); valid && ok {
			return true
		}
	}
	return false
}

// expandBraces turns the first {a,b} group of a pattern into one pattern per alternative,
// recursively; a pattern without braces is itself.
func expandBraces(p string) []string {
	open := strings.IndexByte(p, '{')
	if open < 0 {
		return []string{p}
	}
	end := strings.IndexByte(p[open:], '}')
	if end < 0 {
		return []string{p}
	}
	end += open
	var out []string
	for _, alt := range strings.Split(p[open+1:end], ",") {
		out = append(out, expandBraces(p[:open]+alt+p[end+1:])...)
	}
	return out
}

// globMatchRunes matches with backtracking over the last star; valid is false for a malformed
// class.
func globMatchRunes(p, s []rune) (ok, valid bool) {
	pi, si := 0, 0
	star, starS := -1, 0
	for si < len(s) {
		if pi < len(p) {
			switch p[pi] {
			case '*':
				star, starS = pi, si
				pi++
				continue
			case '?':
				pi++
				si++
				continue
			case '[':
				matched, next, good := classMatch(p, pi, s[si])
				if !good {
					return false, false
				}
				if matched {
					pi, si = next, si+1
					continue
				}
			case '\\':
				if pi+1 < len(p) && p[pi+1] == s[si] {
					pi, si = pi+2, si+1
					continue
				}
			default:
				if p[pi] == s[si] {
					pi++
					si++
					continue
				}
			}
		}
		if star < 0 {
			return false, true
		}
		pi, starS = star+1, starS+1
		si = starS
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p), true
}

// classMatch matches r against the class that starts at p[at] ('['): whether it matches, the
// index after the class, and whether the class is well formed.
func classMatch(p []rune, at int, r rune) (matched bool, next int, valid bool) {
	i := at + 1
	negate := i < len(p) && p[i] == '!'
	if negate {
		i++
	}
	start := i
	for i < len(p) && (p[i] != ']' || i == start) {
		lo := p[i]
		hi := lo
		if i+2 < len(p) && p[i+1] == '-' && p[i+2] != ']' {
			hi = p[i+2]
			i += 2
		}
		if lo <= r && r <= hi {
			matched = true
		}
		i++
	}
	if i >= len(p) {
		return false, 0, false
	}
	return matched != negate, i + 1, true
}

// The labels and annotation Cogpit reads a namespace's GitOps tool from: Flux's kustomize and helm
// controllers label what they apply with the name of their Kustomization or HelmRelease; Argo CD
// writes its tracking annotation (its default since 3.0), or its instance label where a cluster
// tracks by that label. Argo CD's older default, app.kubernetes.io/instance, is also Helm's and
// tells nothing.
const (
	fluxKustomizeLabel = "kustomize.toolkit.fluxcd.io/name"
	fluxHelmLabel      = "helm.toolkit.fluxcd.io/name"
	argoInstanceLabel  = "argocd.argoproj.io/instance"
	argoTrackingAnno   = "argocd.argoproj.io/tracking-id"
)

// The tools' names as the page writes them.
const (
	veleroFluxCD = "Flux CD"
	veleroArgoCD = "Argo CD"
)

// gitOpsTools names the GitOps tools whose marks an object carries: Flux CD, Argo CD, both, or
// neither.
func gitOpsTools(o *unstructured.Unstructured) (flux, argo bool) {
	labels := o.GetLabels()
	flux = labels[fluxKustomizeLabel] != "" || labels[fluxHelmLabel] != ""
	argo = labels[argoInstanceLabel] != "" || o.GetAnnotations()[argoTrackingAnno] != ""
	return flux, argo
}

// veleroExcludeLabel is Velero's label that leaves an object out of every backup. On a Namespace
// it leaves out the Namespace object alone: Velero checks it on each item (item_backupper.go), and
// the namespace's other objects stay in scope.
const veleroExcludeLabel = "velero.io/exclude-from-backup"

// excludedByLabel reports whether an object carries Velero's exclude label.
func excludedByLabel(o *unstructured.Unstructured) bool {
	return o.GetLabels()[veleroExcludeLabel] == "true"
}

// narrowing is what narrows the scope, for a reason: "app=postgres", "persistentvolumeclaims".
func (sc veleroScope) narrowing() string {
	switch {
	case sc.selector != "" && sc.resources != "":
		return sc.selector + " and " + sc.resources
	case sc.selector != "":
		return sc.selector
	}
	return sc.resources
}
