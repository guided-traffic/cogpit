package kinds

import (
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Back references (Preferences › App › Back references, D-152): a drawer's Referenced By section
// lists the objects of other kinds that point at the drawer's object, by a label selector (a pod's
// Services, Network Policies and Pod Disruption Budgets) or by name (the pods that mount or read a
// ConfigMap). Each comes from an index of the referring kind over the cache, read while such a
// drawer is open and the preference is on (engine drawerNeeds); nothing is asked of the API server.

// TitleReferencedBy is the title of a drawer's back references section.
const TitleReferencedBy = "Referenced By"

const (
	keyNetworkPolicies      = "networkpolicies"
	keyPodDisruptionBudgets = "poddisruptionbudgets"

	idVia     = "via"
	allPods   = "*"
	viaRoleRf = "roleRef"
)

// backRef is one way objects of another kind point at an object: the referrers' index, the keys
// the object is found under in it, and how a referrer found there points at the object.
type backRef struct {
	index model.RelatedIndex
	kind  string // the referrers' Kind, as the table's Kind column shows it: "Service"
	title string // their plural, as the section names what it read: "Services"
	keys  func(target *unstructured.Unstructured) []string
	// via returns how referrer points at target, a line each; none: it does not (a Service found
	// under one of the pod's labels whose selector's other pairs do not match).
	via func(referrer, target *unstructured.Unstructured) []string
}

// backRefs are the back references of each kind key, in the order the section lists them.
var backRefs = map[string][]backRef{
	podsResource: {
		{index: model.RelatedIndex{Kind: keyServices, Name: "bySelector", Keys: serviceSelectorKeys},
			kind: titleService, title: "Services", keys: podSelectorKeys, via: serviceSelectsPod},
		{index: model.RelatedIndex{Kind: keyNetworkPolicies, Name: "byPodSelector", Keys: selectorKeysAt("spec", "podSelector")},
			kind: "NetworkPolicy", title: "Network Policies", keys: podSelectorKeys, via: selectorSelectsPod("spec.podSelector", "spec", "podSelector")},
		{index: model.RelatedIndex{Kind: keyPodDisruptionBudgets, Name: "bySelector", Keys: selectorKeysAt("spec", "selector")},
			kind: "PodDisruptionBudget", title: "Pod Disruption Budgets", keys: podSelectorKeys, via: selectorSelectsPod("spec.selector", "spec", "selector")},
	},
	keyConfigMaps: {
		named(podsResource, "byConfigMap", "Pod", "Pods", podConfigMapUses, namespacedKey),
	},
	keySecrets: {
		named(podsResource, "bySecret", "Pod", "Pods", podSecretUses, namespacedKey),
		named("ingresses", "bySecret", "Ingress", "Ingresses", ingressSecretUses, namespacedKey),
		named(keyServiceAccounts, "bySecret", kindServiceAccount, "Service Accounts", accountSecretUses, namespacedKey),
	},
	keyServiceAccounts: {
		named(podsResource, "byServiceAccount", "Pod", "Pods", podAccountUses, namespacedKey),
		named("rolebindings", "byServiceAccount", "RoleBinding", "Role Bindings", bindingAccountUses, namespacedKey),
		named("clusterrolebindings", "byServiceAccount", "ClusterRoleBinding", "Cluster Role Bindings", bindingAccountUses, namespacedKey),
	},
	keyServices: {
		named("ingresses", "byService", "Ingress", "Ingresses", ingressServiceUses, namespacedKey),
	},
	keyRoles: {
		named("rolebindings", "byRole", "RoleBinding", "Role Bindings", bindingRoleUses, roleKey),
	},
	keyClusterRoles: {
		named("rolebindings", "byRole", "RoleBinding", "Role Bindings", bindingRoleUses, clusterRoleKey),
		named("clusterrolebindings", "byRole", "ClusterRoleBinding", "Cluster Role Bindings", bindingRoleUses, clusterRoleKey),
	},
	keyStorageClasses: {
		named(persistentVolumeClaims.Key, "byStorageClass", "PersistentVolumeClaim", "Persistent Volume Claims", storageClassUses, nameKey),
		named(keyPersistentVolumes, "byStorageClass", "PersistentVolume", "Persistent Volumes", storageClassUses, nameKey),
	},
}

// BackRefNeeds are the indexes the back references of a kind's drawer read; nil for a kind
// without.
func BackRefNeeds(kind string) []model.RelatedIndex {
	refs := backRefs[kind]
	if len(refs) == 0 {
		return nil
	}
	out := make([]model.RelatedIndex, len(refs))
	for i, br := range refs {
		out[i] = br.index
	}
	return out
}

// BackRefSection is the Referenced By section of obj, an object of the kind key: a sortable table
// of the referrers' Kind, Name (a link), their Namespace for a cluster-scoped object, and Via, how
// each points at it. Without referrers it says which kinds it found none in; a kind whose index is
// not listed yet, not served or not readable is named under Not read. False for a kind without
// back references.
func BackRefSection(kind string, obj *unstructured.Unstructured, rel model.Related) (model.DetailSection, bool) {
	refs := backRefs[kind]
	if len(refs) == 0 || obj == nil {
		return model.DetailSection{}, false
	}
	rel = relatedOr(rel)
	clusterWide := obj.GetNamespace() == ""
	cols := []model.Column{{ID: idKind, Title: titleKind, Type: model.ColumnText}, nameColumn}
	if clusterWide {
		cols = append(cols, namespaceColumn)
	}
	cols = append(cols, model.Column{ID: idVia, Title: "Via", Type: model.ColumnText})
	var rows [][]model.Cell
	var read, unread []string
	for _, br := range refs {
		if !rel.Listed(br.index) {
			unread = append(unread, br.title)
			continue
		}
		read = append(read, br.title)
		for _, o := range br.referrers(obj, rel) {
			via := br.via(o, obj)
			if len(via) == 0 {
				continue
			}
			row := []model.Cell{{Text: br.kind}, {Text: o.GetName(), Link: linkTo(br.index.Kind, o.GetNamespace(), o.GetName())}}
			if clusterWide {
				row = append(row, namespaceCell(o))
			}
			rows = append(rows, append(row, model.Cell{Text: strings.Join(via, "\n")}))
		}
	}
	sec := model.DetailSection{Title: TitleReferencedBy}
	if len(unread) > 0 {
		sec.Items = append(sec.Items, model.DetailItem{Name: "Not read", Value: strings.Join(unread, ", ")})
	}
	switch {
	case len(rows) > 0:
		sec.Table = model.SortableCellTable(cols, rows)
		sec.Table.Sort = &model.TableSort{Column: idKind} // grouped by Kind until the user sorts it
	case len(read) > 0:
		sec.Items = append(sec.Items, model.DetailItem{Name: "Found", Value: "None in " + strings.Join(read, ", ")})
	}
	return sec, true
}

// referrers are the objects found under target's keys, each once, by namespace and name.
func (br backRef) referrers(target *unstructured.Unstructured, rel model.Related) []*unstructured.Unstructured {
	var out []*unstructured.Unstructured
	seen := map[string]bool{}
	for _, key := range br.keys(target) {
		for _, o := range rel.Objects(br.index, key) {
			id := o.GetNamespace() + "/" + o.GetName()
			if !seen[id] {
				seen[id] = true
				out = append(out, o)
			}
		}
	}
	slices.SortFunc(out, func(a, b *unstructured.Unstructured) int {
		if c := strings.Compare(a.GetNamespace(), b.GetNamespace()); c != 0 {
			return c
		}
		return strings.Compare(a.GetName(), b.GetName())
	})
	return out
}

func relatedOr(rel model.Related) model.Related {
	if rel == nil {
		return model.NoRelated
	}
	return rel
}

// The label selectors. A Service's selector is a map of pairs, empty for one whose endpoints are
// kept by hand; a Network Policy's pod selector and a budget's selector are label selectors, whose
// empty form selects every pod of the namespace (a budget without one selects none). An object is
// indexed under "<namespace>/<key>=<value>" for each pair it requires, and a label selector without
// pairs (empty, or of expressions only) under "<namespace>/*"; a pod looks under each of its labels
// and under "*", and via checks the whole selector.

func serviceSelectorKeys(svc *unstructured.Unstructured) []string {
	return pairKeys(svc.GetNamespace(), model.NestedStringMap(svc, "spec", "selector"))
}

func selectorKeysAt(fields ...string) func(*unstructured.Unstructured) []string {
	return func(obj *unstructured.Unstructured) []string {
		sel := model.NestedMap(obj, fields...)
		if sel == nil {
			return nil
		}
		if keys := pairKeys(obj.GetNamespace(), model.NestedStringMap(sel, "matchLabels")); len(keys) > 0 {
			return keys
		}
		return []string{obj.GetNamespace() + "/" + allPods}
	}
}

func podSelectorKeys(pod *unstructured.Unstructured) []string {
	return append(pairKeys(pod.GetNamespace(), pod.GetLabels()), pod.GetNamespace()+"/"+allPods)
}

func pairKeys(ns string, pairs map[string]string) []string {
	out := make([]string, 0, len(pairs))
	for _, p := range model.SortedPairs(pairs) {
		out = append(out, ns+"/"+p)
	}
	return out
}

func serviceSelectsPod(svc, pod *unstructured.Unstructured) []string {
	sel := model.NestedStringMap(svc, "spec", "selector")
	if len(sel) == 0 || svc.GetNamespace() != pod.GetNamespace() {
		return nil
	}
	labels := pod.GetLabels()
	for k, v := range sel {
		if got, ok := labels[k]; !ok || got != v {
			return nil
		}
	}
	return []string{"spec.selector"}
}

// selectorSelectsPod is the via of a label selector at fields, written path: "spec.podSelector",
// "spec.podSelector (all pods)" for the empty selector.
func selectorSelectsPod(path string, fields ...string) func(ref, pod *unstructured.Unstructured) []string {
	return func(ref, pod *unstructured.Unstructured) []string {
		sel := model.NestedMap(ref, fields...)
		if sel == nil || ref.GetNamespace() != pod.GetNamespace() || !selects(sel, pod.GetLabels()) {
			return nil
		}
		if len(model.NestedMap(sel, "matchLabels")) == 0 && len(model.NestedSlice(sel, "matchExpressions")) == 0 {
			return []string{path + " (all pods)"}
		}
		return []string{path}
	}
}

// selects reports whether a label selector selects labels, as the API server reads it; an
// operator it does not know selects nothing.
func selects(sel map[string]any, labels map[string]string) bool {
	for k, v := range model.NestedStringMap(sel, "matchLabels") {
		if got, ok := labels[k]; !ok || got != v {
			return false
		}
	}
	for _, e := range model.NestedSlice(sel, "matchExpressions") {
		got, has := labels[model.NestedString(e, "key")]
		values := model.NestedStrings(e, "values")
		var ok bool
		switch model.NestedString(e, "operator") {
		case opIn:
			ok = has && slices.Contains(values, got)
		case opNotIn:
			ok = !has || !slices.Contains(values, got)
		case opExists:
			ok = has
		case opDoesNotExist:
			ok = !has
		}
		if !ok {
			return false
		}
	}
	return true
}

// The references by name. A use is one place a referrer names an object: the key the object is
// found under ("<namespace>/<name>", a cluster-scoped one's name, a role's "Role/<namespace>/<name>")
// and how ("volume config", "env web: LOG_LEVEL").
type use struct{ key, via string }

// named is a backRef of references by name: the referrers of kind key are indexed under the keys of
// their uses, the target is found under targetKey, and via lists a referrer's uses of that key.
func named(key, index, kind, title string, uses func(*unstructured.Unstructured) []use, targetKey func(*unstructured.Unstructured) string) backRef {
	return backRef{
		index: model.RelatedIndex{Kind: key, Name: index, Keys: func(obj *unstructured.Unstructured) []string {
			var out []string
			for _, u := range uses(obj) {
				out = appendUnique(out, u.key)
			}
			return out
		}},
		kind: kind, title: title,
		keys: func(t *unstructured.Unstructured) []string { return nonEmpty(targetKey(t)) },
		via: func(ref, t *unstructured.Unstructured) []string {
			key := targetKey(t)
			var out []string
			for _, u := range uses(ref) {
				if u.key == key {
					out = appendUnique(out, u.via)
				}
			}
			return mergeVias(out)
		},
	}
}

func namespacedKey(obj *unstructured.Unstructured) string {
	return obj.GetNamespace() + "/" + obj.GetName()
}

func nameKey(obj *unstructured.Unstructured) string { return obj.GetName() }

func roleKey(obj *unstructured.Unstructured) string {
	return kindRole + "/" + obj.GetNamespace() + "/" + obj.GetName()
}

func clusterRoleKey(obj *unstructured.Unstructured) string {
	return kindClusterRole + "/" + obj.GetName()
}

// mergeVias joins lines of one head, "env web: A" and "env web: B" into "env web: A, B", in the
// order the heads first appear.
func mergeVias(lines []string) []string {
	var out []string
	at := map[string]int{}
	for _, l := range lines {
		head, tail, ok := strings.Cut(l, ": ")
		if !ok {
			out = append(out, l)
			continue
		}
		if i, seen := at[head]; seen {
			out[i] += ", " + tail
			continue
		}
		at[head] = len(out)
		out = append(out, l)
	}
	return out
}

// podObjectUses are a pod's uses of the ConfigMaps or Secrets it names in its volumes (projected
// ones too), its containers' envFrom and env, and, for Secrets, its image pull secrets: the places
// podSecrets reads.
func podObjectUses(pod *unstructured.Unstructured, volume, volumeName, projected, envFrom, keyRef string, pullSecrets bool) []use {
	ns := pod.GetNamespace()
	var out []use
	add := func(name, via string) {
		if name != "" {
			out = append(out, use{ns + "/" + name, via})
		}
	}
	for _, v := range model.NestedSlice(pod, "spec", "volumes") {
		vol := model.NestedString(v, fieldName)
		add(model.NestedString(v, volume, volumeName), "volume "+vol)
		for _, src := range model.NestedSlice(v, "projected", "sources") {
			add(model.NestedString(src, projected, fieldName), "volume "+vol+" (projected)")
		}
	}
	if pullSecrets {
		for _, s := range model.NestedSlice(pod, "spec", "imagePullSecrets") {
			add(model.NestedString(s, fieldName), "imagePullSecrets")
		}
	}
	for _, group := range []string{fieldInitContainers, fieldContainers, "ephemeralContainers"} {
		for _, c := range model.NestedSlice(pod, "spec", group) {
			cname := model.NestedString(c, fieldName)
			for _, e := range model.NestedSlice(c, "envFrom") {
				add(model.NestedString(e, envFrom, fieldName), "envFrom "+cname)
			}
			for _, e := range model.NestedSlice(c, "env") {
				add(model.NestedString(e, "valueFrom", keyRef, fieldName), "env "+cname+": "+model.NestedString(e, fieldName))
			}
		}
	}
	return out
}

func podConfigMapUses(pod *unstructured.Unstructured) []use {
	return podObjectUses(pod, "configMap", fieldName, "configMap", "configMapRef", "configMapKeyRef", false)
}

func podSecretUses(pod *unstructured.Unstructured) []use {
	return podObjectUses(pod, "secret", "secretName", "secret", "secretRef", "secretKeyRef", true)
}

func podAccountUses(pod *unstructured.Unstructured) []use {
	account := model.NestedString(pod, "spec", "serviceAccountName")
	if account == "" {
		account = model.NestedString(pod, "spec", "serviceAccount") // deprecated alias
	}
	if account == "" {
		return nil
	}
	return []use{{pod.GetNamespace() + "/" + account, "spec.serviceAccountName"}}
}

func ingressSecretUses(ing *unstructured.Unstructured) []use {
	var out []use
	for _, t := range model.NestedSlice(ing, "spec", "tls") {
		if name := model.NestedString(t, "secretName"); name != "" {
			via := "tls"
			if hosts := model.NestedStrings(t, fieldHosts); len(hosts) > 0 {
				via += ": " + strings.Join(hosts, ", ")
			}
			out = append(out, use{ing.GetNamespace() + "/" + name, via})
		}
	}
	return out
}

// ingressServiceUses are the backends that name a Service: the default backend, and each path as
// "<host><path>" ("*" for a rule without a host).
func ingressServiceUses(ing *unstructured.Unstructured) []use {
	ns := ing.GetNamespace()
	var out []use
	if name := model.NestedString(ing, "spec", "defaultBackend", "service", fieldName); name != "" {
		out = append(out, use{ns + "/" + name, "defaultBackend"})
	}
	for _, r := range model.NestedSlice(ing, "spec", "rules") {
		for _, p := range model.NestedSlice(r, "http", "paths") {
			if name := model.NestedString(p, "backend", "service", fieldName); name != "" {
				out = append(out, use{ns + "/" + name, ingressHost(r) + model.NestedString(p, "path")})
			}
		}
	}
	return out
}

func accountSecretUses(sa *unstructured.Unstructured) []use {
	var out []use
	for _, field := range []string{"imagePullSecrets", "secrets"} {
		for _, s := range model.NestedSlice(sa, field) {
			if name := model.NestedString(s, fieldName); name != "" {
				out = append(out, use{sa.GetNamespace() + "/" + name, field})
			}
		}
	}
	return out
}

// bindingAccountUses are a binding's ServiceAccount subjects; one without a namespace in a
// RoleBinding is the binding's (subjectNamespace).
func bindingAccountUses(b *unstructured.Unstructured) []use {
	var out []use
	for _, s := range subjects(b) {
		if model.NestedString(s, idKind) != kindServiceAccount {
			continue
		}
		ns, name := subjectNamespace(s, b.GetNamespace()), model.NestedString(s, fieldName)
		if ns != "" && name != "" {
			out = append(out, use{ns + "/" + name, "subjects"})
		}
	}
	return out
}

// bindingRoleUses is a binding's roleRef: a Role in the binding's namespace, or a ClusterRole.
func bindingRoleUses(b *unstructured.Unstructured) []use {
	name := model.NestedString(b, viaRoleRf, fieldName)
	if name == "" {
		return nil
	}
	switch model.NestedString(b, viaRoleRf, idKind) {
	case kindClusterRole:
		return []use{{kindClusterRole + "/" + name, viaRoleRf}}
	case kindRole:
		if b.GetNamespace() != "" {
			return []use{{kindRole + "/" + b.GetNamespace() + "/" + name, viaRoleRf}}
		}
	}
	return nil
}

// storageClassUses is a claim's or a volume's class: spec.storageClassName, else the older
// annotation (pvcStorageClass, which a volume's class follows too).
func storageClassUses(obj *unstructured.Unstructured) []use {
	if _, set := model.NestedValue(obj, "spec", "storageClassName"); set {
		if name := model.NestedString(obj, "spec", "storageClassName"); name != "" {
			return []use{{name, "spec.storageClassName"}}
		}
		return nil
	}
	if name := model.NestedString(obj, "metadata", "annotations", storageClassAnnotation); name != "" {
		return []use{{name, "metadata.annotations[" + storageClassAnnotation + "]"}}
	}
	return nil
}
