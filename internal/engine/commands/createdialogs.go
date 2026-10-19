package commands

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/api/validate/content"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The create dialogs (G7). Lens Desktop creates Namespaces, Service Accounts, Roles, Cluster
// Roles, Role Bindings, Cluster Role Bindings and Resource Quotas from small dialogs behind its
// views' "+" instead of a YAML tab; none of them is captured, so what they ask for is inferred
// (resource-parity.md, Create dialogs). Each is a command here: it builds one object from its
// parameters, checks them as the API server would (names, required fields), and creates it the
// way a Create resource tab creates its text (engine.CreateManifest): a server-side dry run, then
// the create, after the confirmation's context and object are checked. The GUI's dialogs
// (internal/ui/fyneui/createdialog.go) dispatch these commands; agents call the same tools.
//
// Danger (the lead's default, 2026-10-12, not the owner's decision; privilege-footprint.md): every
// create is mutating, so an agent's waits for the user's acceptance (D-2). A binding is a
// privilege grant, so its confirmation says what it grants to whom (Effects); an agent's role
// binding, role or cluster role waits for the user even where the user allowed agent writes for
// the context (AgentsAsk, the owner's decisions of 2026-10-14, H-27 and A1); and two kinds of
// grant are confirmed for every origin, the GUI too: a cluster role binding (cluster-wide, so registered destructive) and a role
// binding to cluster-admin, or to a role Cogpit's cache holds whose rules allow every verb on
// every resource (escalated to destructive). Roles themselves grant nothing until bound.

// The RBAC names the dialogs use.
const (
	rbacAPIGroup          = "rbac.authorization.k8s.io"
	rbacKindRole          = "Role"
	rbacKindClusterRole   = "ClusterRole"
	subjectUser           = "User"
	subjectGroup          = "Group"
	subjectServiceAccount = "ServiceAccount"
	clusterAdminRole      = "cluster-admin"
	wildcard              = "*"
	maxCreateItems        = 100 // rules, subjects, hard limits, and entries of one list
	maxDetailItems        = 10  // rules or subjects a confirmation names before "and N more"

	// Field names of the manifests.
	fieldName      = "name"
	fieldKind      = "kind"
	fieldNamespace = "namespace"
)

// createKind is one kind a create dialog makes.
type createKind struct {
	gvr        schema.GroupVersionResource
	kind       string // the API kind, "RoleBinding"
	noun       string // in messages, "role binding"
	namespaced bool
	nameRule   func(string) []string // the API server's rule for the object's name
}

var (
	createNamespaceKind = createKind{gvr: schema.GroupVersionResource{Version: "v1", Resource: "namespaces"},
		kind: "Namespace", noun: fieldNamespace, nameRule: validation.IsDNS1123Label}
	createServiceAccountKind = createKind{gvr: schema.GroupVersionResource{Version: "v1", Resource: "serviceaccounts"},
		kind: "ServiceAccount", noun: "service account", namespaced: true, nameRule: validation.IsDNS1123Subdomain}
	createResourceQuotaKind = createKind{gvr: schema.GroupVersionResource{Version: "v1", Resource: "resourcequotas"},
		kind: "ResourceQuota", noun: "resource quota", namespaced: true, nameRule: validation.IsDNS1123Subdomain}
	// RBAC objects' names are path segments (no "/" or "%", not "." or ".."): "system:auth-delegator".
	createRoleKind = createKind{gvr: schema.GroupVersionResource{Group: rbacAPIGroup, Version: "v1", Resource: "roles"},
		kind: rbacKindRole, noun: "role", namespaced: true, nameRule: content.IsPathSegmentName}
	createClusterRoleKind = createKind{gvr: schema.GroupVersionResource{Group: rbacAPIGroup, Version: "v1", Resource: "clusterroles"},
		kind: rbacKindClusterRole, noun: "cluster role", nameRule: content.IsPathSegmentName}
	createRoleBindingKind = createKind{gvr: schema.GroupVersionResource{Group: rbacAPIGroup, Version: "v1", Resource: "rolebindings"},
		kind: "RoleBinding", noun: "role binding", namespaced: true, nameRule: content.IsPathSegmentName}
	createClusterRoleBindingKind = createKind{gvr: schema.GroupVersionResource{Group: rbacAPIGroup, Version: "v1", Resource: "clusterrolebindings"},
		kind: "ClusterRoleBinding", noun: "cluster role binding", nameRule: content.IsPathSegmentName}
)

// verb is what creating the kind needs.
func (k createKind) verb() engine.Verb {
	return engine.Verb{Verb: nwVerbCreate, Group: k.gvr.Group, Resource: k.gvr.Resource}
}

// enabled is the Enabled check of a create command: a cluster Cogpit can write to, and the create
// verb not denied by the access reviews in the namespace the object would land in. It does not
// block and does not judge the parameters: Targets and Run do.
func (k createKind) enabled(s *engine.Session, contextID, namespace string) error {
	b, err := writableOn(s, contextID)
	if err != nil {
		return err
	}
	if _, err := b.ObjectWriter(); err != nil {
		return err
	}
	ns := ""
	if k.namespaced {
		if ns, err = s.CreateNamespace(b.Context, namespace); err != nil {
			return err
		}
	}
	return s.CreatePermitted(b.Context, ns, k.verb())
}

// object starts a new object's manifest and its reference for the confirmation: the name checked
// against the API server's rule for the kind, a namespaced kind's namespace resolved
// (engine.CreateNamespace) and checked.
func (k createKind) object(s *engine.Session, contextID, name, namespace string) (map[string]any, model.ObjectRef, error) {
	b, err := s.TargetBackend(contextID) // the context's cluster, "" the shown one (D-69)
	if err != nil {
		return nil, model.ObjectRef{}, err
	}
	name = strings.TrimSpace(name)
	if err := checkName(k.noun, name, k.nameRule); err != nil {
		return nil, model.ObjectRef{}, err
	}
	meta := map[string]any{fieldName: name}
	ref := model.ObjectRef{Context: b.Context, Kind: engine.KindKeyFor(b, k.gvr), Name: name}
	if k.namespaced {
		ns, err := s.CreateNamespace(b.Context, namespace)
		if err != nil {
			return nil, model.ObjectRef{}, err
		}
		if err := checkName(fieldNamespace, ns, validation.IsDNS1123Label); err != nil {
			return nil, model.ObjectRef{}, err
		}
		meta[fieldNamespace], ref.Namespace = ns, ns
	}
	return map[string]any{"apiVersion": k.gvr.GroupVersion().String(), fieldKind: k.kind, "metadata": meta}, ref, nil
}

// checkName refuses an empty name and one the rule refuses, saying why.
func checkName(noun, name string, rule func(string) []string) error {
	if name == "" {
		return fmt.Errorf("the %s needs a name", noun)
	}
	if errs := rule(name); len(errs) > 0 {
		return fmt.Errorf("%s name %q is not valid: %s", noun, name, strings.Join(errs, "; "))
	}
	return nil
}

// createSpec registers the parts every create command shares; build makes the object.
func createSpec[P any](id, label, menuPath, description string, k createKind, danger engine.Danger,
	namespace func(P) string, build func(*engine.Session, P) (map[string]any, model.ObjectRef, error)) engine.Spec[P] {
	return engine.Spec[P]{
		ID: id, Label: label, MenuPath: menuPath, Kinds: []string{k.gvr.Resource}, Danger: danger,
		Verbs: []engine.Verb{k.verb()}, Description: description,
		Enabled: func(s *engine.Session, p P) error { return k.enabled(s, contextOf(p), namespace(p)) },
		Targets: func(s *engine.Session, p P) ([]model.ObjectRef, error) {
			_, ref, err := build(s, p)
			if err != nil {
				return nil, err
			}
			return []model.ObjectRef{ref}, nil
		},
		Run: func(ctx context.Context, s *engine.Session, p P) (any, error) {
			obj, _, err := build(s, p)
			if err != nil {
				return nil, err
			}
			item, err := s.CreateManifest(ctx, contextOf(p), obj)
			if err == nil && item.Created {
				revealCreated(ctx, s, engine.CreateResult{Items: []engine.CreateItem{item}, Created: 1}) // A102
			}
			return item, err
		},
	}
}

// The parameters of the create commands. A namespaced kind's namespace is optional, as
// object.create's is: empty uses the namespace filter's single namespace, else the context's
// default namespace (a dialog shows it filled in, Prefill).

type namespaceCreateParams struct {
	engine.OnCluster
	Name string `json:"name" jsonschema:"the namespace's name: a DNS-1123 label (lowercase letters, digits and '-', starting and ending with a letter or digit, at most 63 characters)"`
}

type serviceAccountCreateParams struct {
	engine.OnCluster
	Name      string `json:"name" jsonschema:"the account's name: a DNS-1123 subdomain (lowercase letters, digits, '-' and '.', at most 253 characters)"`
	Namespace string `json:"namespace,omitempty" jsonschema:"the namespace to create it in; empty uses the namespace filter's single namespace, else the context's default"`
}

// policyRuleParams is one rule of a role, as Kubernetes' PolicyRule.
type policyRuleParams struct {
	APIGroups       []string `json:"apiGroups,omitempty" jsonschema:"the API groups of the resources: an empty string is the core group (pods, services, configmaps), apps the group of deployments, * every group; required with resources"`
	Resources       []string `json:"resources,omitempty" jsonschema:"resources by their plural name, a subresource after a slash (pods, deployments, pods/log), * every resource"`
	Verbs           []string `json:"verbs" jsonschema:"the API verbs the rule allows: get, list, watch, create, update, patch, delete, deletecollection, or * for every verb"`
	ResourceNames   []string `json:"resourceNames,omitempty" jsonschema:"only the objects with these names; empty: every object of the resources"`
	NonResourceURLs []string `json:"nonResourceURLs,omitempty" jsonschema:"cluster roles only, instead of apiGroups and resources: URL paths such as /healthz or /metrics (a trailing * matches a prefix)"`
}

type roleCreateParams struct {
	engine.OnCluster
	Name      string             `json:"name" jsonschema:"the role's name: no '/' or '%', not '.' or '..'"`
	Namespace string             `json:"namespace,omitempty" jsonschema:"the namespace to create it in; empty uses the namespace filter's single namespace, else the context's default"`
	Rules     []policyRuleParams `json:"rules,omitempty" jsonschema:"what the role allows, at most 100 rules; none creates a role that grants nothing until rules are added"`
}

type clusterRoleCreateParams struct {
	engine.OnCluster
	Name  string             `json:"name" jsonschema:"the cluster role's name: no '/' or '%', not '.' or '..' (system:... names are allowed)"`
	Rules []policyRuleParams `json:"rules,omitempty" jsonschema:"what the cluster role allows, at most 100 rules; none creates a role that grants nothing until rules are added"`
}

type roleRefParams struct {
	Kind string `json:"kind" jsonschema:"Role (a role in the binding's namespace) or ClusterRole"`
	Name string `json:"name" jsonschema:"the role's name"`
}

// subjectParams is one subject of a binding, as Kubernetes' Subject.
type subjectParams struct {
	Kind      string `json:"kind" jsonschema:"User, Group or ServiceAccount"`
	Name      string `json:"name" jsonschema:"the user's or group's name as the cluster's authentication reports it, or the service account's name"`
	Namespace string `json:"namespace,omitempty" jsonschema:"a ServiceAccount's namespace: for a role binding empty means the binding's namespace, for a cluster role binding it is required; Users and Groups have none"`
}

type roleBindingCreateParams struct {
	engine.OnCluster
	Name      string          `json:"name" jsonschema:"the binding's name: no '/' or '%', not '.' or '..'"`
	Namespace string          `json:"namespace,omitempty" jsonschema:"the namespace the binding grants the role in; empty uses the namespace filter's single namespace, else the context's default"`
	RoleRef   roleRefParams   `json:"roleRef" jsonschema:"the role it grants: a Role of the binding's namespace or a ClusterRole, whose permissions then apply in the binding's namespace only"`
	Subjects  []subjectParams `json:"subjects" jsonschema:"who gets the role: at least one, at most 100"`
}

type clusterRoleBindingCreateParams struct {
	engine.OnCluster
	Name        string          `json:"name" jsonschema:"the binding's name: no '/' or '%', not '.' or '..'"`
	ClusterRole string          `json:"clusterRole" jsonschema:"the name of the cluster role it grants, in every namespace and on cluster-scoped resources"`
	Subjects    []subjectParams `json:"subjects" jsonschema:"who gets the cluster role: at least one, at most 100; a ServiceAccount names its namespace"`
}

type resourceQuotaCreateParams struct {
	engine.OnCluster
	Name      string            `json:"name" jsonschema:"the quota's name: a DNS-1123 subdomain (lowercase letters, digits, '-' and '.')"`
	Namespace string            `json:"namespace,omitempty" jsonschema:"the namespace it limits; empty uses the namespace filter's single namespace, else the context's default"`
	Hard      map[string]string `json:"hard" jsonschema:"the hard limits, at least one: a resource (pods, services, configmaps, secrets, persistentvolumeclaims, requests.cpu, requests.memory, requests.storage, limits.cpu, limits.memory, count/deployments.apps, ...) and its quantity (10, 500m, 4Gi)"`
}

func registerCreateDialogs(g *registrar) {
	add(g, createSpec("namespace.create", "Create Namespace", "Namespace/Create",
		"Creates a Namespace after a server-side dry run. An agent's request waits for the user's acceptance. Returns the new namespace's reference.",
		createNamespaceKind, engine.DangerMutating,
		func(namespaceCreateParams) string { return "" },
		func(s *engine.Session, p namespaceCreateParams) (map[string]any, model.ObjectRef, error) {
			return createNamespaceKind.object(s, p.Context, p.Name, "")
		}))

	sa := createSpec("serviceAccount.create", "Create Service Account", "ServiceAccount/Create",
		"Creates a ServiceAccount in a namespace after a server-side dry run. An agent's request waits for the user's acceptance. Returns its reference.",
		createServiceAccountKind, engine.DangerMutating,
		func(p serviceAccountCreateParams) string { return p.Namespace },
		func(s *engine.Session, p serviceAccountCreateParams) (map[string]any, model.ObjectRef, error) {
			return createServiceAccountKind.object(s, p.Context, p.Name, p.Namespace)
		})
	sa.Prefill = func(s *engine.Session, p serviceAccountCreateParams) (serviceAccountCreateParams, error) {
		ns, err := s.CreateNamespace(p.Context, p.Namespace)
		p.Namespace = ns
		return p, err
	}
	add(g, sa)

	role := createSpec("role.create", "Create Role", "Role/Create",
		"Creates a Role in a namespace with the given rules (API groups, resources, verbs, optional resource names) after a "+
			"server-side dry run; the API server refuses rules that grant more than the user holds unless the user may escalate "+
			"roles. An agent's request always waits for the user's acceptance, which names the rules, also where the user "+
			"allowed agent writes. Returns its reference.",
		createRoleKind, engine.DangerMutating,
		func(p roleCreateParams) string { return p.Namespace },
		func(s *engine.Session, p roleCreateParams) (map[string]any, model.ObjectRef, error) {
			obj, ref, err := createRoleKind.object(s, p.Context, p.Name, p.Namespace)
			if err != nil {
				return nil, ref, err
			}
			rules, err := buildRules(p.Rules, false)
			obj["rules"] = rules
			return obj, ref, err
		})
	role.Prefill = func(s *engine.Session, p roleCreateParams) (roleCreateParams, error) {
		ns, err := s.CreateNamespace(p.Context, p.Namespace)
		p.Namespace = ns
		return p, err
	}
	role.Effects = func(_ *engine.Session, p roleCreateParams) []engine.Effect { return rulesEffects(p.Rules) }
	// Its rules are what every binding to it grants: an agent's always waits for the user (A1).
	role.AgentsAsk = func(*engine.Session, roleCreateParams) bool { return true }
	add(g, role)

	clusterRole := createSpec("clusterRole.create", "Create Cluster Role", "ClusterRole/Create",
		"Creates a ClusterRole with the given rules (API groups and resources, or non-resource URLs) after a server-side dry "+
			"run; the API server refuses rules that grant more than the user holds unless the user may escalate roles. An "+
			"agent's request always waits for the user's acceptance, which names the rules, also where the user allowed "+
			"agent writes. Returns its reference.",
		createClusterRoleKind, engine.DangerMutating,
		func(clusterRoleCreateParams) string { return "" },
		func(s *engine.Session, p clusterRoleCreateParams) (map[string]any, model.ObjectRef, error) {
			obj, ref, err := createClusterRoleKind.object(s, p.Context, p.Name, "")
			if err != nil {
				return nil, ref, err
			}
			rules, err := buildRules(p.Rules, true)
			obj["rules"] = rules
			return obj, ref, err
		})
	clusterRole.Effects = func(_ *engine.Session, p clusterRoleCreateParams) []engine.Effect { return rulesEffects(p.Rules) }
	clusterRole.AgentsAsk = func(*engine.Session, clusterRoleCreateParams) bool { return true } // A1
	add(g, clusterRole)

	rb := createSpec("roleBinding.create", "Create Role Binding", "RoleBinding/Create",
		"Creates a RoleBinding that grants a Role of its namespace, or a ClusterRole within its namespace, to users, groups "+
			"and service accounts, after a server-side dry run; the API server refuses it unless the user holds the role's "+
			"permissions or may bind it. An agent's request always waits for the user's acceptance, which names the role and "+
			"the subjects, also where the user allowed agent writes; a binding to cluster-admin, or to a role that allows "+
			"every verb on every resource, is confirmed by the user for every origin. Returns its reference.",
		createRoleBindingKind, engine.DangerMutating,
		func(p roleBindingCreateParams) string { return p.Namespace },
		func(s *engine.Session, p roleBindingCreateParams) (map[string]any, model.ObjectRef, error) {
			obj, ref, err := createRoleBindingKind.object(s, p.Context, p.Name, p.Namespace)
			if err != nil {
				return nil, ref, err
			}
			roleRef, err := buildRoleRef(p.RoleRef.Kind, p.RoleRef.Name, false)
			if err != nil {
				return nil, ref, err
			}
			subjects, err := buildSubjects(p.Subjects, ref.Namespace)
			obj["roleRef"], obj["subjects"] = roleRef, subjects
			return obj, ref, err
		})
	rb.Prefill = func(s *engine.Session, p roleBindingCreateParams) (roleBindingCreateParams, error) {
		ns, err := s.CreateNamespace(p.Context, p.Namespace)
		p.Namespace = ns
		return p, err
	}
	// A grant: an agent's always waits for the user, whatever the allowance of agent writes (H-27).
	rb.AgentsAsk = func(*engine.Session, roleBindingCreateParams) bool { return true }
	rb.Undoable = true // escalated, still a grant deleting the binding takes back (A101)
	rb.Escalate = func(s *engine.Session, p roleBindingCreateParams) bool {
		return grantsAll(s, p.Context, p.RoleRef.Kind, p.RoleRef.Name, p.Namespace)
	}
	rb.Effects = func(s *engine.Session, p roleBindingCreateParams) []engine.Effect {
		ns, _ := s.CreateNamespace(p.Context, p.Namespace)
		return bindingEffects(canonicalRoleKind(p.RoleRef.Kind), strings.TrimSpace(p.RoleRef.Name), ns, p.Subjects)
	}
	add(g, rb)

	crb := createSpec("clusterRoleBinding.create", "Create Cluster Role Binding", "ClusterRoleBinding/Create",
		"Creates a ClusterRoleBinding that grants a ClusterRole in every namespace and on cluster-scoped resources to users, "+
			"groups and service accounts, after a server-side dry run; the API server refuses it unless the user holds the "+
			"role's permissions or may bind it. A cluster-wide grant: the user always confirms it, naming the role and the "+
			"subjects, whoever asks. Returns its reference.",
		createClusterRoleBindingKind, engine.DangerDestructive,
		func(clusterRoleBindingCreateParams) string { return "" },
		func(s *engine.Session, p clusterRoleBindingCreateParams) (map[string]any, model.ObjectRef, error) {
			obj, ref, err := createClusterRoleBindingKind.object(s, p.Context, p.Name, "")
			if err != nil {
				return nil, ref, err
			}
			roleRef, err := buildRoleRef(rbacKindClusterRole, p.ClusterRole, true)
			if err != nil {
				return nil, ref, err
			}
			subjects, err := buildSubjects(p.Subjects, "")
			obj["roleRef"], obj["subjects"] = roleRef, subjects
			return obj, ref, err
		})
	crb.Undoable = true // deleting the binding takes the grant back (A101)
	crb.Effects = func(_ *engine.Session, p clusterRoleBindingCreateParams) []engine.Effect {
		return bindingEffects(rbacKindClusterRole, strings.TrimSpace(p.ClusterRole), "", p.Subjects)
	}
	add(g, crb)

	quota := createSpec("resourceQuota.create", "Create Resource Quota", "ResourceQuota/Create",
		"Creates a ResourceQuota in a namespace with hard limits by resource (pods, requests.cpu, limits.memory, "+
			"count/deployments.apps, ...) after a server-side dry run, which also checks the resource names. An agent's "+
			"request waits for the user's acceptance, which names the limits. Returns its reference.",
		createResourceQuotaKind, engine.DangerMutating,
		func(p resourceQuotaCreateParams) string { return p.Namespace },
		func(s *engine.Session, p resourceQuotaCreateParams) (map[string]any, model.ObjectRef, error) {
			obj, ref, err := createResourceQuotaKind.object(s, p.Context, p.Name, p.Namespace)
			if err != nil {
				return nil, ref, err
			}
			hard, err := buildHard(p.Hard)
			obj["spec"] = map[string]any{"hard": hard}
			return obj, ref, err
		})
	quota.Prefill = func(s *engine.Session, p resourceQuotaCreateParams) (resourceQuotaCreateParams, error) {
		ns, err := s.CreateNamespace(p.Context, p.Namespace)
		p.Namespace = ns
		return p, err
	}
	quota.Effects = func(_ *engine.Session, p resourceQuotaCreateParams) []engine.Effect { return quotaEffects(p.Hard) }
	add(g, quota)
}

// cleanList trims a list's entries and drops repeated ones; it refuses an empty entry (unless
// emptyOK: the core API group is ""), an entry with spaces inside, and more than maxCreateItems.
func cleanList(what string, in []string, emptyOK bool) ([]any, error) {
	if len(in) > maxCreateItems {
		return nil, fmt.Errorf("%s: %d entries, at most %d", what, len(in), maxCreateItems)
	}
	out := make([]any, 0, len(in))
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		switch {
		case v == "" && !emptyOK:
			return nil, fmt.Errorf("%s: an empty entry", what)
		case strings.ContainsAny(v, " \t\r\n"):
			return nil, fmt.Errorf("%s: %q has spaces in it", what, v)
		case seen[v]:
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out, nil
}

// buildRules checks a role's rules as the API server does and returns them for the manifest: each
// needs verbs, and either API groups and resources (resource names optional) or, in a cluster role
// only, non-resource URLs.
func buildRules(rules []policyRuleParams, cluster bool) ([]any, error) {
	if len(rules) > maxCreateItems {
		return nil, fmt.Errorf("%d rules, at most %d", len(rules), maxCreateItems)
	}
	out := make([]any, 0, len(rules))
	for i, r := range rules {
		at := fmt.Sprintf("rule %d", i+1)
		verbs, err := cleanList(at+": verbs", r.Verbs, false)
		if err != nil {
			return nil, err
		}
		if len(verbs) == 0 {
			return nil, fmt.Errorf("%s needs at least one verb (get, list, watch, ... or *)", at)
		}
		rule := map[string]any{"verbs": verbs}
		if len(r.NonResourceURLs) > 0 {
			if !cluster {
				return nil, fmt.Errorf("%s: only a cluster role has non-resource URLs", at)
			}
			if len(r.APIGroups)+len(r.Resources)+len(r.ResourceNames) > 0 {
				return nil, fmt.Errorf("%s names either non-resource URLs or API groups and resources, not both", at)
			}
			urls, err := cleanList(at+": non-resource URLs", r.NonResourceURLs, false)
			if err != nil {
				return nil, err
			}
			for _, u := range urls {
				if s := u.(string); s != wildcard && !strings.HasPrefix(s, "/") {
					return nil, fmt.Errorf("%s: non-resource URL %q is not a path (/healthz, /metrics, or *)", at, s)
				}
			}
			rule["nonResourceURLs"] = urls
			out = append(out, rule)
			continue
		}
		if len(r.Resources) == 0 {
			if cluster {
				return nil, fmt.Errorf("%s needs resources (pods, deployments, ... or *) or non-resource URLs", at)
			}
			return nil, fmt.Errorf("%s needs resources (pods, deployments, ... or *)", at)
		}
		if len(r.APIGroups) == 0 {
			return nil, fmt.Errorf(`%s needs API groups: "" for the core group (pods, services), apps for deployments, or *`, at)
		}
		groups, err := cleanList(at+": API groups", r.APIGroups, true)
		if err != nil {
			return nil, err
		}
		res, err := cleanList(at+": resources", r.Resources, false)
		if err != nil {
			return nil, err
		}
		rule["apiGroups"], rule["resources"] = groups, res
		if len(r.ResourceNames) > 0 {
			names, err := cleanList(at+": resource names", r.ResourceNames, false)
			if err != nil {
				return nil, err
			}
			rule["resourceNames"] = names
		}
		out = append(out, rule)
	}
	return out, nil
}

// canonicalRoleKind returns Role or ClusterRole for a kind in any case, else the kind as given.
func canonicalRoleKind(kind string) string {
	kind = strings.TrimSpace(kind)
	for _, k := range []string{rbacKindRole, rbacKindClusterRole} {
		if strings.EqualFold(kind, k) {
			return k
		}
	}
	return kind
}

// buildRoleRef checks a binding's role reference: a Role or a ClusterRole (a cluster role binding
// takes only a ClusterRole) and its name.
func buildRoleRef(kind, name string, clusterOnly bool) (map[string]any, error) {
	kind, name = canonicalRoleKind(kind), strings.TrimSpace(name)
	switch {
	case kind == rbacKindClusterRole:
	case kind == rbacKindRole && !clusterOnly:
	case clusterOnly:
		return nil, fmt.Errorf("a cluster role binding grants a ClusterRole, not a %s", kind)
	default:
		return nil, fmt.Errorf("role kind %q: want Role or ClusterRole", kind)
	}
	if err := checkName(strings.ToLower(kind), name, content.IsPathSegmentName); err != nil {
		return nil, fmt.Errorf("the binding's role: %w", err)
	}
	return map[string]any{"apiGroup": rbacAPIGroup, fieldKind: kind, fieldName: name}, nil
}

// subjectKind returns User, Group or ServiceAccount for a kind in any case, else "".
func subjectKind(kind string) string {
	kind = strings.TrimSpace(kind)
	for _, k := range []string{subjectUser, subjectGroup, subjectServiceAccount} {
		if strings.EqualFold(kind, k) {
			return k
		}
	}
	return ""
}

// buildSubjects checks a binding's subjects and returns them for the manifest. bindingNS is a role
// binding's namespace, the default of its service accounts'; "" for a cluster role binding, whose
// service accounts must name theirs.
func buildSubjects(in []subjectParams, bindingNS string) ([]any, error) {
	if len(in) == 0 {
		return nil, fmt.Errorf("a binding needs at least one subject: a %s, %s or %s", subjectUser, subjectGroup, subjectServiceAccount)
	}
	if len(in) > maxCreateItems {
		return nil, fmt.Errorf("%d subjects, at most %d", len(in), maxCreateItems)
	}
	out := make([]any, 0, len(in))
	for i, sub := range in {
		at := fmt.Sprintf("subject %d", i+1)
		name, ns := strings.TrimSpace(sub.Name), strings.TrimSpace(sub.Namespace)
		if name == "" {
			return nil, fmt.Errorf("%s needs a name", at)
		}
		switch kind := subjectKind(sub.Kind); kind {
		case subjectUser, subjectGroup:
			if ns != "" {
				return nil, fmt.Errorf("%s: a %s has no namespace; only a %s does", at, kind, subjectServiceAccount)
			}
			out = append(out, map[string]any{fieldKind: kind, "apiGroup": rbacAPIGroup, fieldName: name})
		case subjectServiceAccount:
			if ns == "" {
				ns = bindingNS
			}
			if ns == "" {
				return nil, fmt.Errorf("%s: a service account in a cluster role binding needs its namespace", at)
			}
			if err := checkName("service account", name, validation.IsDNS1123Subdomain); err != nil {
				return nil, fmt.Errorf("%s: %w", at, err)
			}
			if err := checkName(fieldNamespace, ns, validation.IsDNS1123Label); err != nil {
				return nil, fmt.Errorf("%s: %w", at, err)
			}
			out = append(out, map[string]any{fieldKind: kind, fieldName: name, fieldNamespace: ns})
		default:
			return nil, fmt.Errorf("%s: kind %q: want %s, %s or %s", at, sub.Kind, subjectUser, subjectGroup, subjectServiceAccount)
		}
	}
	return out, nil
}

// buildHard checks a quota's hard limits: at least one, each a resource name without spaces and a
// quantity. Whether the API server knows the resource name is its dry run's to say.
func buildHard(hard map[string]string) (map[string]any, error) {
	if len(hard) == 0 {
		return nil, fmt.Errorf("a resource quota needs at least one hard limit, e.g. pods: 10")
	}
	if len(hard) > maxCreateItems {
		return nil, fmt.Errorf("%d hard limits, at most %d", len(hard), maxCreateItems)
	}
	out := make(map[string]any, len(hard))
	for key, v := range hard {
		if key == "" || strings.ContainsAny(key, " \t\r\n") {
			return nil, fmt.Errorf("hard limit %q: a resource name such as pods, requests.cpu or count/deployments.apps", key)
		}
		v = strings.TrimSpace(v)
		if _, err := resource.ParseQuantity(v); err != nil {
			return nil, fmt.Errorf("hard limit %s: %q is not a quantity (10, 500m, 4Gi)", key, v)
		}
		out[key] = v
	}
	return out, nil
}

// grantsAll reports whether a role binding to the role would grant everything: the cluster role
// cluster-admin, or a role Cogpit's cache holds already (no watch is started for it) whose rules
// allow every verb on every resource of every API group. Such a binding is confirmed for every
// origin (Escalate).
func grantsAll(s *engine.Session, contextID, kind, name, namespace string) bool {
	kind, name = canonicalRoleKind(kind), strings.TrimSpace(name)
	if kind == rbacKindClusterRole && name == clusterAdminRole {
		return true
	}
	b := s.BackendOf(contextID) // the binding's cluster, "" the shown one (D-69)
	if b == nil || name == "" {
		return false
	}
	var ref model.ObjectRef
	switch kind {
	case rbacKindClusterRole:
		ref = model.ObjectRef{Context: b.Context, Kind: engine.KindKeyFor(b, createClusterRoleKind.gvr), Name: name}
	case rbacKindRole:
		ns, err := s.CreateNamespace(b.Context, namespace)
		if err != nil {
			return false
		}
		ref = model.ObjectRef{Context: b.Context, Kind: engine.KindKeyFor(b, createRoleKind.gvr), Namespace: ns, Name: name}
	default:
		return false
	}
	if p, ok := b.Profile(ref.Kind); !ok || !b.Cached(p) {
		return false
	}
	obj, err := s.Lookup(ref)
	if err != nil {
		return false
	}
	rules, _, _ := unstructured.NestedSlice(obj.Object, "rules")
	for _, r := range rules {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		has := func(field string) bool {
			vals, _, _ := unstructured.NestedStringSlice(m, field)
			return slices.Contains(vals, wildcard)
		}
		if has("verbs") && has("resources") && has("apiGroups") {
			return true
		}
	}
	return false
}

// bindingEffects are a binding's rows in the confirmation: the role it grants, to whom (one
// subject a line), and the owner's "immediately, cluster-wide" or "in namespace <ns>" (A101,
// 2026-10-14 evening), cluster-wide in the warning colour.
func bindingEffects(roleKind, roleName, namespace string, subjects []subjectParams) []engine.Effect {
	texts := make([]string, 0, len(subjects))
	for _, sub := range subjects {
		kind, name, ns := subjectKind(sub.Kind), strings.TrimSpace(sub.Name), strings.TrimSpace(sub.Namespace)
		if kind == "" {
			kind = sub.Kind
		}
		if kind == subjectServiceAccount {
			if ns == "" {
				ns = namespace
			}
			name = ns + "/" + name
		}
		texts = append(texts, kind+" → "+name)
	}
	when := engine.Effect{Name: "Takes effect", Value: "immediately, in namespace " + namespace}
	if namespace == "" {
		when = engine.Effect{Name: "Takes effect", Value: "immediately, cluster-wide", Tone: engine.EffectWarning}
	}
	return []engine.Effect{
		{Name: "Grants", Value: roleKind + " → " + roleName, Tone: engine.EffectNames},
		{Name: "To", Value: cappedWith(texts, "\n"), Tone: engine.EffectNames},
		when,
	}
}

// rulesEffects are a role's row in the confirmation: its rules, one a line, the first few.
func rulesEffects(rules []policyRuleParams) []engine.Effect {
	if len(rules) == 0 {
		return []engine.Effect{{Name: "Rules", Value: "none: the role grants nothing until rules are added"}}
	}
	texts := make([]string, 0, len(rules))
	for _, r := range rules {
		verbs := strings.Join(r.Verbs, ", ")
		if len(r.NonResourceURLs) > 0 {
			texts = append(texts, verbs+" on "+strings.Join(r.NonResourceURLs, ", "))
			continue
		}
		t := verbs + " on " + strings.Join(r.Resources, ", ")
		if groups := slices.DeleteFunc(slices.Clone(r.APIGroups), func(g string) bool { return strings.TrimSpace(g) == "" }); len(groups) > 0 {
			t += " (" + strings.Join(groups, ", ") + ")"
		}
		if len(r.ResourceNames) > 0 {
			t += " named " + strings.Join(r.ResourceNames, ", ")
		}
		texts = append(texts, t)
	}
	return []engine.Effect{{Name: "Rules", Value: cappedWith(texts, "\n"), Tone: engine.EffectNames}}
}

// quotaEffects are a quota's row in the confirmation: its limits, sorted, one a line.
func quotaEffects(hard map[string]string) []engine.Effect {
	keys := make([]string, 0, len(hard))
	for k := range hard {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	texts := make([]string, 0, len(keys))
	for _, k := range keys {
		texts = append(texts, k+" "+strings.TrimSpace(hard[k]))
	}
	return []engine.Effect{{Name: "Hard limits", Value: cappedWith(texts, "\n"), Tone: engine.EffectNames}}
}

// cappedWith joins the first maxDetailItems texts with sep, then "and N more".
func cappedWith(texts []string, sep string) string {
	if len(texts) > maxDetailItems {
		return strings.Join(texts[:maxDetailItems], sep) + fmt.Sprintf(" and %d more", len(texts)-maxDetailItems)
	}
	return strings.Join(texts, sep)
}
