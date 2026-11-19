package engine

import (
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// RBAC objects an agent asks for (H-27, owner; every RBAC kind since the owner's A1): a Role,
// ClusterRole, RoleBinding or ClusterRoleBinding that an agent creates or changes always waits for
// the user's acceptance, also in a context where the user allowed agent writes (D-2, H-14). A
// binding hands on a role, and a role's rules decide what every existing binding to it grants: what
// either allows is not something the allowance of "scale, cordon, apply" was given for. Commands
// declare it with Spec.AgentsAsk, judged per invocation and only for agents: role.create,
// clusterRole.create and roleBinding.create always, object.apply when its Edit tab edits an RBAC
// object or its text holds one, object.create when its text holds one. clusterRoleBinding.create is
// destructive and asks every origin anyway. The GUI's own commands are unchanged: the user is the
// one asking.

// Kubernetes' RBAC API group and the kinds and resources it serves.
const (
	rbacGroup                   = "rbac.authorization.k8s.io"
	kindRole                    = "Role"
	kindClusterRole             = "ClusterRole"
	kindRoleBinding             = "RoleBinding"
	kindClusterRoleBinding      = "ClusterRoleBinding"
	resourceRoles               = "roles"
	resourceClusterRoles        = "clusterroles"
	resourceRoleBindings        = "rolebindings"
	resourceClusterRoleBindings = "clusterrolebindings"
)

// RBACKind reports whether apiVersion and kind name a Role, ClusterRole, RoleBinding or
// ClusterRoleBinding of Kubernetes' RBAC group. It matches without regard to case, so that it
// never judges an RBAC object to be something else: an over-match asks once too often, never once
// too few. An apiVersion that does not parse is judged by the kind alone.
func RBACKind(apiVersion, kind string) bool {
	switch {
	case strings.EqualFold(kind, kindRole), strings.EqualFold(kind, kindClusterRole),
		strings.EqualFold(kind, kindRoleBinding), strings.EqualFold(kind, kindClusterRoleBinding):
	default:
		return false
	}
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return true // ask rather than guess
	}
	return strings.EqualFold(gv.Group, rbacGroup)
}

// rbacResource reports whether gvr is one of the RBAC group's four resources.
func rbacResource(gvr schema.GroupVersionResource) bool {
	if gvr.Group != rbacGroup {
		return false
	}
	switch gvr.Resource {
	case resourceRoles, resourceClusterRoles, resourceRoleBindings, resourceClusterRoleBindings:
		return true
	}
	return false
}

// TextHoldsRBAC reports whether manifest text holds a Role, ClusterRole, RoleBinding or
// ClusterRoleBinding, as a document or as an item of a v1 List. Text that does not parse counts as holding one: the
// command refuses it before anything is sent, so asking costs nothing, and a parser's leniency
// can never let a binding past. Empty text holds none.
func TextHoldsRBAC(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	objs, err := ParseManifests(text)
	if err != nil {
		return true
	}
	for _, o := range objs {
		if RBACKind(o.GetAPIVersion(), o.GetKind()) {
			return true
		}
	}
	return false
}

// EditorHoldsRBAC reports whether an apply in the editor tab id writes an RBAC object: the tab is
// an Edit tab on one, or text (the command's own yaml parameter) holds one, or text is empty and
// the tab's text holds one. A tab that does not exist holds none: the apply is refused
// before anything is sent.
func (s *Session) EditorHoldsRBAC(id, text string) bool {
	if TextHoldsRBAC(text) {
		return true
	}
	e, err := s.editorOf(id)
	if err != nil {
		return false
	}
	if e.tab.Type == DockEdit && (rbacResource(e.target.GVR) || RBACKind(e.target.APIVersion, e.target.Kind)) {
		return true
	}
	return strings.TrimSpace(text) == "" && !HelmValuesTab(e.tab.Type) && TextHoldsRBAC(e.text)
}
