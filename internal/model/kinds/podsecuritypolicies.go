package kinds

import (
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// fieldVolumes is the policy's list of allowed volume types, its Volume column and search field.
const fieldVolumes = "volumes"

// podSecurityPolicies has the columns of Lens's documentation (read, not captured): Name,
// Privileged, Volume, Age, and the view "is available only with clusters prior to Kubernetes 1.25".
// Kubernetes removed policy/v1beta1 PodSecurityPolicy in 1.25; the connector offers a profile only
// where discovery finds its resource, so the view appears on such clusters alone. Inferred, capture
// needed: Privileged as "true" or "false" (as Lens shows the Cron Jobs' Suspend), Volume as the
// volume types the policy allows (spec.volumes, "*" for all), no status token (a policy has no
// state), and the drawer: what the policy allows and requires, field by field of the API, with the
// user and group rules as a table.
var podSecurityPolicies = &model.KindProfile{
	Key:      "podsecuritypolicies",
	Group:    "policy",
	Version:  "v1beta1",
	Resource: "podsecuritypolicies",
	Kind:     "PodSecurityPolicy",
	Title:    "Pod Security Policies",
	Columns: []model.Column{
		nameColumn,
		{ID: "privileged", Title: "Privileged", Type: model.ColumnText, Width: 100},
		{ID: fieldVolumes, Title: "Volume", Type: model.ColumnText},
		ageColumn,
	},
	Cells: podSecurityPolicyCells,
	Fields: map[string]fieldFunc{
		"privileged": func(obj *unstructured.Unstructured) []string {
			return []string{pspBool(obj, "privileged")}
		},
		fieldVolumes: func(obj *unstructured.Unstructured) []string {
			return model.NestedStrings(obj, fieldSpec, fieldVolumes)
		},
		"allowedHostPath": func(obj *unstructured.Unstructured) []string {
			return namesOf(model.NestedSlice(obj, fieldSpec, "allowedHostPaths"), "pathPrefix")
		},
		"allowedCapability": func(obj *unstructured.Unstructured) []string {
			return model.NestedStrings(obj, fieldSpec, "allowedCapabilities")
		},
	},
	Detail: podSecurityPolicyDetail,
}

func podSecurityPolicyCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	return []model.Cell{
		nameCell(obj),
		{Text: pspBool(obj, "privileged")},
		{Text: strings.Join(model.NestedStrings(obj, fieldSpec, fieldVolumes), ", ")},
		model.AgeCell(created(obj)),
	}, model.StatusNone
}

// pspBool is a boolean field of the policy's spec as text; a missing field is the API's default,
// false.
func pspBool(obj any, field string) string {
	v, _ := model.NestedBool(obj, fieldSpec, field)
	return strconv.FormatBool(v)
}

// podSecurityPolicyDetail is the drawer (inferred): Properties with what the policy allows (a list
// field the policy leaves out is left out), then the user and group rules as a table.
func podSecurityPolicyDetail(obj *unstructured.Unstructured) []model.DetailSection {
	spec := model.NestedMap(obj, fieldSpec)
	var d detailItems
	d.add("Privileged", pspBool(obj, "privileged"))
	escalation := "true (default)"
	if v, ok := model.NestedBool(spec, "allowPrivilegeEscalation"); ok {
		escalation = strconv.FormatBool(v)
	}
	d.add("Allow Privilege Escalation", escalation)
	if v, ok := model.NestedBool(spec, "defaultAllowPrivilegeEscalation"); ok {
		d.add("Default Allow Privilege Escalation", strconv.FormatBool(v))
	}
	d.add("Read-only Root Filesystem", pspBool(obj, "readOnlyRootFilesystem"))
	d.add("Host Namespaces", hostNamespaces(spec))
	d.add("Host Ports", hostPortRanges(spec))
	volumes := "none"
	if v := model.NestedStrings(spec, fieldVolumes); len(v) > 0 {
		volumes = strings.Join(v, ", ")
	}
	d.add("Volumes", volumes)
	d.add("Allowed Host Paths", allowedHostPaths(spec))
	d.add("Allowed Flex Volumes", strings.Join(namesOf(model.NestedSlice(spec, "allowedFlexVolumes"), "driver"), ", "))
	d.add("Allowed CSI Drivers", strings.Join(namesOf(model.NestedSlice(spec, "allowedCSIDrivers"), "name"), ", "))
	for _, f := range []struct{ item, field string }{
		{"Allowed Capabilities", "allowedCapabilities"},
		{"Default Add Capabilities", "defaultAddCapabilities"},
		{"Required Drop Capabilities", "requiredDropCapabilities"},
		{"Allowed Unsafe Sysctls", "allowedUnsafeSysctls"},
		{"Forbidden Sysctls", "forbiddenSysctls"},
		{"Allowed Proc Mount Types", "allowedProcMountTypes"},
	} {
		d.add(f.item, strings.Join(model.NestedStrings(spec, f.field), ", "))
	}
	d.add("SELinux", seLinuxRule(spec))
	d.put(runtimeClassChips(spec))
	if def := model.NestedString(spec, "runtimeClass", "defaultRuntimeClassName"); def != "" {
		d.put(linkedItem("Default Runtime Class", def, linkTo(runtimeClasses.Key, "", def)))
	}
	sections := withSection(nil, model.TitleProperties, d)
	if t := idRulesTable(spec); t != nil {
		sections = append(sections, model.DetailSection{Title: "Users and Groups", Table: t})
	}
	return sections
}

// hostNamespaces names the host namespaces the policy lets pods share, "none" when it allows none.
func hostNamespaces(spec map[string]any) string {
	var out []string
	for _, ns := range []struct{ field, name string }{{"hostNetwork", "Network"}, {"hostPID", "PID"}, {"hostIPC", "IPC"}} {
		if v, _ := model.NestedBool(spec, ns.field); v {
			out = append(out, ns.name)
		}
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, ", ")
}

// hostPortRanges lists the host port ranges the policy allows: "80, 8000-8080".
func hostPortRanges(spec map[string]any) string {
	return strings.Join(idRanges(model.NestedSlice(spec, "hostPorts")), ", ")
}

// idRanges renders ranges of {min, max} as "min-max", or the number alone when both are the same.
func idRanges(ranges []any) []string {
	var out []string
	for _, r := range ranges {
		lo, okLo := model.NestedInt64(r, "min")
		hi, okHi := model.NestedInt64(r, "max")
		switch {
		case !okLo && !okHi:
			continue
		case okLo && okHi && lo != hi:
			out = append(out, itoa(lo)+"-"+itoa(hi))
		case okLo:
			out = append(out, itoa(lo))
		default:
			out = append(out, itoa(hi))
		}
	}
	return out
}

// allowedHostPaths lists the host path prefixes the policy allows, a line each, "(read-only)"
// marking those pods may mount read-only only.
func allowedHostPaths(spec map[string]any) string {
	var lines []string
	for _, p := range model.NestedSlice(spec, "allowedHostPaths") {
		prefix := model.NestedString(p, "pathPrefix")
		if prefix == "" {
			continue
		}
		if ro, _ := model.NestedBool(p, "readOnly"); ro {
			prefix += " (read-only)"
		}
		lines = append(lines, prefix)
	}
	return strings.Join(lines, "\n")
}

// namesOf returns the field of each element of list that sets it.
func namesOf(list []any, field string) []string {
	var out []string
	for _, e := range list {
		if v := model.NestedString(e, field); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// seLinuxRule is the SELinux rule and the options it requires: "MustRunAs (level s0:c1,c2)".
func seLinuxRule(spec map[string]any) string {
	rule := model.NestedString(spec, "seLinux", "rule")
	if rule == "" {
		return ""
	}
	var opts []string
	for _, o := range strings.Fields("user role type level") { // in the order a context names them
		if v := model.NestedString(spec, "seLinux", "seLinuxOptions", o); v != "" {
			opts = append(opts, o+" "+v)
		}
	}
	if len(opts) == 0 {
		return rule
	}
	return rule + " (" + strings.Join(opts, ", ") + ")"
}

// runtimeClassChips lists the runtime classes the policy allows, each a link to its class ("*",
// any class, stays plain).
func runtimeClassChips(spec map[string]any) model.DetailItem {
	names := model.NestedStrings(spec, "runtimeClass", "allowedRuntimeClassNames")
	chips := make([]model.Chip, 0, len(names))
	for _, n := range names {
		if n == "" {
			continue
		}
		c := model.Chip{Text: n}
		if n != "*" {
			c.Link = linkTo(runtimeClasses.Key, "", n)
		}
		chips = append(chips, c)
	}
	return model.ChipsItem("Allowed Runtime Classes", chips)
}

// idRulesTable lists the rules for the user and group ids of the pods' containers: the field, its
// rule and the ranges it allows. Nil when the policy sets none (the API requires them, so only a
// policy read in part has none).
func idRulesTable(spec map[string]any) *model.DetailTable {
	t := &model.DetailTable{Columns: []string{"Field", "Rule", "Ranges"}}
	for _, f := range []struct{ name, field string }{
		{"Run As User", "runAsUser"},
		{"Run As Group", "runAsGroup"},
		{"Supplemental Groups", "supplementalGroups"},
		{"FS Group", "fsGroup"},
	} {
		rule := model.NestedString(spec, f.field, "rule")
		ranges := idRanges(model.NestedSlice(spec, f.field, "ranges"))
		if rule == "" && len(ranges) == 0 {
			continue
		}
		t.Rows = append(t.Rows, []string{f.name, rule, strings.Join(ranges, ", ")})
	}
	if len(t.Rows) == 0 {
		return nil
	}
	return t
}
