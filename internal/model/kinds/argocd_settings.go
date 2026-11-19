package kinds

import (
	"encoding/base64"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Argo CD's settings: Settings › Repositories and Settings › Clusters (Lens's documentation, read:
// Repositories Name, Type, URL, Project, Age; Clusters Name, Server, Age). Argo CD keeps both as
// Secrets labelled argocd.argoproj.io/secret-type (repository and repo-creds; cluster), so they are
// virtual kinds over the cached Secrets, as Helm's releases are, and they come with Argo CD's
// Application definition (the engine offers them while the cluster serves it).
//
// Those Secrets hold credentials: passwords, SSH and TLS keys, GitHub App keys, bearer tokens, a
// cluster's whole client configuration. D-35 (a night decision for the owner to confirm): the
// views, the drawer and agents see only an allowlist of keys that are configuration, never
// credentials: a repository's name, url, type and project; a cluster's name, server, namespaces,
// clusterResources and project. Every other key is named (the Secrets view lists key names too),
// never read; user information in a URL (https://user:token@host) is replaced with <redacted>.
// Derive builds new objects of Cogpit's own kind from the allowed keys alone, so no other value
// reaches a row, the drawer, the YAML, object_get, a search or a log.

// ArgoSecretTypeLabel is the label Argo CD finds its repository and cluster Secrets by.
const ArgoSecretTypeLabel = "argocd.argoproj.io/secret-type" // #nosec G101 -- a label's name, no credential

// Kind keys of the settings views: Cogpit's own bare plurals (architecture §4.5).
const (
	ArgoRepositoriesKey = "argocdrepositories"
	ArgoClustersKey     = "argocdclusters"
)

// Argo CD's Secret types.
const (
	argoTypeRepository = "repository"
	argoTypeRepoCreds  = "repo-creds" // #nosec G101 -- a Secret type's name, no credential
	argoTypeCluster    = "cluster"
)

// ArgoRepositoryKeys and ArgoClusterKeys are the allowlists of D-35: the only keys of Argo CD's
// repository and cluster Secrets whose values Cogpit reads.
var (
	ArgoRepositoryKeys = []string{fieldName, "url", "type", "project"}
	ArgoClusterKeys    = []string{fieldName, "server", keyNamespaces, "clusterResources", "project"}
)

// argoValueBytes bounds a value Cogpit shows: configuration, not documents.
const argoValueBytes = 2 << 10

// argoHiddenNamespace is the settings views' Namespace column, hidden: Lens's documented columns
// leave it out (Argo CD keeps its settings in its own namespace).
var argoHiddenNamespace = model.Column{ID: namespaceColumn.ID, Title: namespaceColumn.Title, Type: model.ColumnText, Hidden: true}

// Column ids of the settings views.
const (
	idArgoURL         = "url"
	idArgoServerCol   = "server"
	idArgoDisplayName = "displayName"
	idArgoSecretType  = "secretType"
	idArgoAuth        = "authentication"
	idArgoClusterRes  = "clusterResources"
	fieldArgoKeys     = "hiddenKeys"
	kindArgoSecret    = "Secret"
	fieldAPIVersion   = "apiVersion"
	fieldNamespace    = "namespace"
)

// ArgoSettings returns the profiles of the settings views, Repositories then Clusters.
func ArgoSettings() []*model.KindProfile {
	return []*model.KindProfile{argoRepositories, argoClusters}
}

var argoRepositories = &model.KindProfile{
	Key:           ArgoRepositoriesKey,
	Version:       "v1",
	Resource:      keySecrets,
	Kind:          "ArgoCDRepository",
	DetailTitle:   titleArgoRepo,
	Title:         "Repositories",
	Namespaced:    true,
	LabelSelector: ArgoSecretTypeLabel + " in (" + argoTypeRepository + "," + argoTypeRepoCreds + ")",
	Columns: []model.Column{
		nameColumn,
		typeColumn,
		{ID: idArgoURL, Title: "URL", Type: model.ColumnText},
		{ID: idArgoProject, Title: titleArgoProject, Type: model.ColumnText, Width: 110},
		ageColumn,
		argoHiddenNamespace,
		{ID: idArgoDisplayName, Title: "Display Name", Type: model.ColumnText, Hidden: true},
		{ID: idArgoSecretType, Title: titleKind, Type: model.ColumnText, Hidden: true},
		{ID: idArgoAuth, Title: "Authentication", Type: model.ColumnText, Hidden: true},
	},
	Cells: argoRepoCells,
	Fields: map[string]fieldFunc{
		idArgoURL: func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, fieldSpec, idArgoURL))
		},
		idType: func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, fieldSpec, idType))
		},
		idArgoProject: func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, fieldSpec, idArgoProject))
		},
	},
	Derive: func(objs []*unstructured.Unstructured) []*unstructured.Unstructured {
		return argoDerive(objs, "ArgoCDRepository", ArgoRepositoryKeys, argoTypeRepository, argoTypeRepoCreds)
	},
	Detail: argoRepoDetail,
}

var argoClusters = &model.KindProfile{
	Key:           ArgoClustersKey,
	Version:       "v1",
	Resource:      keySecrets,
	Kind:          "ArgoCDCluster",
	DetailTitle:   "Cluster",
	Title:         "Clusters",
	Namespaced:    true,
	LabelSelector: ArgoSecretTypeLabel + "=" + argoTypeCluster,
	Columns: []model.Column{
		nameColumn,
		{ID: idArgoServerCol, Title: titleArgoServer, Type: model.ColumnText},
		ageColumn,
		argoHiddenNamespace,
		{ID: idArgoDisplayName, Title: "Display Name", Type: model.ColumnText, Hidden: true},
		{ID: keyNamespaces, Title: titleArgoNamespaces, Type: model.ColumnText, Hidden: true},
		{ID: idArgoClusterRes, Title: "Cluster Resources", Type: model.ColumnText, Hidden: true},
		{ID: idArgoProject, Title: titleArgoProject, Type: model.ColumnText, Hidden: true},
	},
	Cells: argoClusterCells,
	Fields: map[string]fieldFunc{
		idArgoServerCol: func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, fieldSpec, idArgoServerCol))
		},
		idArgoProject: func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, fieldSpec, idArgoProject))
		},
	},
	Derive: func(objs []*unstructured.Unstructured) []*unstructured.Unstructured {
		return argoDerive(objs, "ArgoCDCluster", ArgoClusterKeys, argoTypeCluster)
	},
	Detail: argoClusterDetail,
}

// argoDerive makes one object of Cogpit's own kind per Secret of the given Argo CD types: its
// name, namespace, UID, resourceVersion, creation time and labels, the Secret type, the values of
// the allowed keys (decoded, bounded, user information in URLs redacted) and the names of the
// other keys. It never copies a value of another key, an annotation or the Secret's type field.
func argoDerive(objs []*unstructured.Unstructured, kind string, allowed []string, types ...string) []*unstructured.Unstructured {
	out := make([]*unstructured.Unstructured, 0, len(objs))
	for _, sec := range objs {
		typ := sec.GetLabels()[ArgoSecretTypeLabel]
		if k := sec.GetKind(); (k != "" && k != kindArgoSecret) || !slices.Contains(types, typ) { // a list item may come without its kind
			continue
		}
		spec := map[string]any{idArgoSecretType: typ}
		var others []string
		data := model.NestedMap(sec, "data")
		for _, k := range sortedKeys(data) {
			if !slices.Contains(allowed, k) {
				others = append(others, k)
				continue
			}
			if v := argoValue(data[k]); v != "" {
				spec[k] = v
			}
		}
		if len(others) > 0 {
			keys := make([]any, len(others))
			for i, k := range others {
				keys[i] = k
			}
			spec[fieldArgoKeys] = keys
		}
		meta := map[string]any{
			fieldName: sec.GetName(), fieldNamespace: sec.GetNamespace(),
			"uid": string(sec.GetUID()), "resourceVersion": sec.GetResourceVersion(),
		}
		if ts, ok := model.NestedValue(sec, "metadata", "creationTimestamp"); ok {
			meta["creationTimestamp"] = ts
		}
		if l := sec.GetLabels(); len(l) > 0 {
			labels := make(map[string]any, len(l))
			for k, v := range l {
				labels[k] = v
			}
			meta["labels"] = labels
		}
		out = append(out, &unstructured.Unstructured{Object: map[string]any{
			fieldAPIVersion: model.LocalGroup + "/v1", idKind: kind, "metadata": meta, fieldSpec: spec,
		}})
	}
	return out
}

// argoValue decodes an allowed value for display: base64 as the API serves a Secret's data, cut
// at argoValueBytes, control characters dropped, user information in a URL redacted. A value that
// does not decode, or is not text, shows nothing.
func argoValue(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil || !utf8.Valid(raw) {
		return ""
	}
	if len(raw) > argoValueBytes {
		raw = raw[:argoValueBytes]
		for !utf8.Valid(raw) && len(raw) > 0 {
			raw = raw[:len(raw)-1]
		}
	}
	text := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u202e' || r == '\u202d' {
			return -1
		}
		return r
	}, string(raw))
	return RedactURLUserinfo(strings.TrimSpace(text))
}

// RedactURLUserinfo replaces the user information of a URL with <redacted>: https://user:token@host
// and https://token@host carry credentials (D-35). An SSH URL's user alone (ssh://git@host/x) and
// the SCP form (git@host:x) name no secret and stay; user:password@host without a scheme is
// redacted too.
func RedactURLUserinfo(s string) string {
	if i := strings.Index(s, "://"); i >= 0 {
		scheme, rest := strings.ToLower(s[:i]), s[i+3:]
		host := rest
		if end := strings.IndexAny(rest, "/?#"); end >= 0 {
			host = rest[:end]
		}
		at := strings.LastIndex(host, "@")
		if at < 0 {
			return s
		}
		if strings.Contains(scheme, "ssh") && !strings.Contains(host[:at], ":") {
			return s // the SSH user, no password
		}
		return s[:i+3] + model.Redacted + "@" + rest[at+1:]
	}
	head := s
	if end := strings.IndexAny(s, "/"); end >= 0 {
		head = s[:end]
	}
	if at := strings.LastIndex(head, "@"); at >= 0 && strings.Contains(head[:at], ":") {
		return model.Redacted + "@" + s[at+1:]
	}
	return s
}

// argoSecretTypeText names a Secret type the way the settings views show it (inferred).
func argoSecretTypeText(typ string) string {
	switch typ {
	case argoTypeRepository:
		return titleArgoRepo
	case argoTypeRepoCreds:
		return "Credential Template"
	case argoTypeCluster:
		return "Cluster"
	}
	return typ
}

// argoRepoAuth says how a repository authenticates, from the names of its Secret's keys alone,
// never their values (inferred): Argo CD's credential keys for each method.
func argoRepoAuth(obj any) string {
	keys := model.NestedStrings(obj, fieldSpec, fieldArgoKeys)
	has := func(names ...string) bool {
		for _, n := range names {
			if slices.Contains(keys, n) {
				return true
			}
		}
		return false
	}
	var out []string
	if has("sshPrivateKey") {
		out = append(out, "SSH key")
	}
	if has("password", "username") {
		out = append(out, "Username and password")
	}
	if has("bearerToken") {
		out = append(out, "Bearer token")
	}
	if has("githubAppPrivateKey", "githubAppID") {
		out = append(out, "GitHub App")
	}
	if has("gcpServiceAccountKey") {
		out = append(out, "Google Cloud service account")
	}
	if has("tlsClientCertData", "tlsClientCertKey") {
		out = append(out, "TLS client certificate")
	}
	if len(out) == 0 {
		return "None"
	}
	return strings.Join(out, ", ")
}

func argoRepoCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	spec := model.NestedMap(obj, fieldSpec)
	return []model.Cell{
		nameCell(obj),
		{Text: model.NestedString(spec, idType)},
		{Text: model.NestedString(spec, idArgoURL)},
		{Text: model.NestedString(spec, idArgoProject), Link: linkTo(ArgoProjectsKey, obj.GetNamespace(), model.NestedString(spec, idArgoProject))},
		model.AgeCell(created(obj)),
		namespaceCell(obj),
		{Text: model.NestedString(spec, "name")},
		{Text: argoSecretTypeText(model.NestedString(spec, idArgoSecretType))},
		{Text: argoRepoAuth(obj)},
	}, model.StatusNone
}

func argoClusterCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	spec := model.NestedMap(obj, fieldSpec)
	return []model.Cell{
		nameCell(obj),
		{Text: model.NestedString(spec, idArgoServerCol)},
		model.AgeCell(created(obj)),
		namespaceCell(obj),
		{Text: model.NestedString(spec, "name")},
		{Text: model.NestedString(spec, keyNamespaces)},
		{Text: model.NestedString(spec, idArgoClusterRes)},
		{Text: model.NestedString(spec, idArgoProject), Link: linkTo(ArgoProjectsKey, obj.GetNamespace(), model.NestedString(spec, idArgoProject))},
	}, model.StatusNone
}

// argoSettingsHead is the start of a settings drawer: created, the Secret it is read from (a
// link), its namespace.
func argoSettingsHead(obj *unstructured.Unstructured) detailItems {
	var d detailItems
	d.add("Created", timestamp(created(obj)))
	d.put(linkedItem("Secret", obj.GetName(), linkTo(keySecrets, obj.GetNamespace(), obj.GetName())))
	d.put(linkedItem("Namespace", obj.GetNamespace(), linkTo(keyNamespaces, "", obj.GetNamespace())))
	d.add(titleKind, argoSecretTypeText(model.NestedString(obj, fieldSpec, idArgoSecretType)))
	return d
}

// argoHiddenKeys names the Secret's other keys, whose values Cogpit never reads (D-35).
func argoHiddenKeys(obj any) model.DetailItem {
	keys := model.NestedStrings(obj, fieldSpec, fieldArgoKeys)
	return model.DetailItem{Name: "Values Not Shown", Value: strings.Join(keys, "\n"), Fold: "hidden keys"}
}

func argoRepoDetail(obj *unstructured.Unstructured) []model.DetailSection {
	d := argoSettingsHead(obj)
	spec := model.NestedMap(obj, fieldSpec)
	d.add("Name", model.NestedString(spec, "name"))
	d.add("URL", model.NestedString(spec, idArgoURL))
	d.add("Type", model.NestedString(spec, idType))
	d.put(linkedItem(titleArgoProject, model.NestedString(spec, idArgoProject), linkTo(ArgoProjectsKey, obj.GetNamespace(), model.NestedString(spec, idArgoProject))))
	d.add("Authentication", argoRepoAuth(obj))
	d.put(argoHiddenKeys(obj))
	return withSection(nil, model.TitleProperties, d)
}

func argoClusterDetail(obj *unstructured.Unstructured) []model.DetailSection {
	d := argoSettingsHead(obj)
	spec := model.NestedMap(obj, fieldSpec)
	d.add("Name", model.NestedString(spec, "name"))
	d.add(titleArgoServer, model.NestedString(spec, idArgoServerCol))
	d.add(titleArgoNamespaces, strings.Join(strings.Split(model.NestedString(spec, keyNamespaces), ","), "\n"))
	d.add("Cluster Resources", model.NestedString(spec, idArgoClusterRes))
	d.put(linkedItem(titleArgoProject, model.NestedString(spec, idArgoProject), linkTo(ArgoProjectsKey, obj.GetNamespace(), model.NestedString(spec, idArgoProject))))
	d.put(argoHiddenKeys(obj))
	return withSection(nil, model.TitleProperties, d)
}
