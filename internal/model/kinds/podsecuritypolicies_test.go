package kinds_test

import (
	"slices"
	"testing"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

func FuzzPodSecurityPolicies(f *testing.F) { fuzzProfile(f, "podsecuritypolicies") }

// TestPodSecurityPoliciesColumns pins the columns of Lens's documentation (read, not captured):
// Name, Privileged, Volume, Age, at policy/v1beta1, which clusters served until 1.25.
func TestPodSecurityPoliciesColumns(t *testing.T) {
	assertColumnOrder(t, "podsecuritypolicies", []model.Column{
		{ID: "name", Title: "Name"}, {ID: "privileged", Title: "Privileged"},
		{ID: "volumes", Title: "Volume"}, {ID: "age", Title: "Age"},
	})
	p, _ := kinds.ByKey("podsecuritypolicies")
	if p.Group != "policy" || p.Version != "v1beta1" || p.Namespaced || len(p.FallbackVersions) != 0 {
		t.Errorf("%s/%s namespaced %v fallbacks %v: Pod Security Policies are policy/v1beta1, cluster-scoped",
			p.Group, p.Version, p.Namespaced, p.FallbackVersions)
	}
}

func TestPodSecurityPoliciesStatusTexts(t *testing.T) {
	const none = model.StatusNone
	// A policy has no state: no row is coloured (inferred). Privileged reads "true" or "false",
	// the API's default false when the policy leaves it out (inferred, as Lens shows the Cron Jobs'
	// Suspend).
	checkStatusTexts(t, []statusText{
		{"podsecuritypolicies", "healthy", "privileged", "false", none, none},
		{"podsecuritypolicies", "healthy", "volumes", "configMap, emptyDir, projected, secret, downwardAPI, csi, persistentVolumeClaim, ephemeral", none, none},
		{"podsecuritypolicies", "failing", "privileged", "true", none, none},
		{"podsecuritypolicies", "failing", "volumes", "*", none, none},
		{"podsecuritypolicies", "minimal", "privileged", "false", none, none},
		{"podsecuritypolicies", "minimal", "volumes", "", none, none},
		{"podsecuritypolicies", "terminating", "volumes", "configMap, secret", none, model.StatusTerminating},
	})
}

// The drawer says what a policy allows, field by field: the API's defaults where the policy is
// silent, host namespaces and ports, host paths with their read-only mark, the rules for user
// and group ids as a table, and the runtime classes as links (inferred).
func TestPodSecurityPolicyDetail(t *testing.T) {
	p, _ := kinds.ByKey("podsecuritypolicies")
	cases := map[string][]model.DetailSection{}
	for _, c := range loadCases(t, "podsecuritypolicies") {
		cases[c.name] = p.DetailSections(c.obj, model.NoRelated)
	}
	for _, tt := range []struct{ name, item, want string }{
		{"healthy", "Allow Privilege Escalation", "false"},
		{"healthy", "Host Namespaces", "none"},
		{"healthy", "Required Drop Capabilities", "ALL"},
		{"healthy", "SELinux", "RunAsAny"},
		{"failing", "Privileged", "true"},
		{"failing", "Host Namespaces", "Network, PID, IPC"},
		{"failing", "Host Ports", "0-65535"},
		{"failing", "Volumes", "*"},
		{"minimal", "Allow Privilege Escalation", "true (default)"},
		{"minimal", "Volumes", "none"},
		{"minimal", "Read-only Root Filesystem", "false"},
		{"hostaccess", "Host Ports", "9100, 10250-10255"},
		{"hostaccess", "Allowed Host Paths", "/var/log (read-only)\n/var/lib/node-agent"},
		{"hostaccess", "Default Allow Privilege Escalation", "false"},
		{"hostaccess", "Allowed Flex Volumes", "example.com/lvm"},
		{"hostaccess", "Allowed CSI Drivers", "secrets-store.csi.k8s.io"},
		{"hostaccess", "Allowed Capabilities", "NET_ADMIN, SYS_TIME"},
		{"hostaccess", "Forbidden Sysctls", "kernel.*"},
		{"hostaccess", "SELinux", "MustRunAs (type spc_t, level s0:c123,c456)"},
		{"hostaccess", "Allowed Runtime Classes", "gvisor\n*"},
		{"hostaccess", "Default Runtime Class", "gvisor"},
	} {
		if got := findItem(cases[tt.name], tt.item).Value; got != tt.want {
			t.Errorf("%s: %s = %q, want %q", tt.name, tt.item, got, tt.want)
		}
	}
	if it := findItem(cases["minimal"], "SELinux"); it.Value != "" {
		t.Errorf("minimal: SELinux %q, want left out", it.Value)
	}
	gvisor := &model.ObjectRef{Kind: "runtimeclasses", Name: "gvisor"}
	chips := findItem(cases["hostaccess"], "Allowed Runtime Classes").Chips
	if len(chips) != 2 || chips[0].Link == nil || *chips[0].Link != *gvisor || chips[1].Link != nil {
		t.Errorf("runtime class chips %+v", chips)
	}
	if l := findItem(cases["hostaccess"], "Default Runtime Class").Link; l == nil || *l != *gvisor {
		t.Errorf("default runtime class link %+v", l)
	}

	ids := findTable(t, cases["hostaccess"], "Users and Groups")
	want := [][]string{
		{"Run As User", "MustRunAs", "1000-1999, 3000"},
		{"Run As Group", "MayRunAs", "1000-1999"},
		{"Supplemental Groups", "RunAsAny", ""},
		{"FS Group", "MayRunAs", "2000"},
	}
	if !slices.Equal(ids.Columns, []string{"Field", "Rule", "Ranges"}) || !slices.EqualFunc(ids.Rows, want, slices.Equal) {
		t.Errorf("Users and Groups: %v %v", ids.Columns, ids.Rows)
	}
	for _, s := range cases["minimal"] {
		if s.Title == "Users and Groups" {
			t.Errorf("minimal: a rules table without rules: %+v", s.Table)
		}
	}
}

// The searchable fields agents use: privileged, the volume types, host path prefixes and allowed
// capabilities.
func TestPodSecurityPolicyFields(t *testing.T) {
	p, _ := kinds.ByKey("podsecuritypolicies")
	for _, c := range loadCases(t, "podsecuritypolicies") {
		if c.name != "hostaccess" {
			continue
		}
		for field, want := range map[string][]string{
			"privileged":        {"false"},
			"volumes":           {"hostPath", "flexVolume", "csi", "secret"},
			"allowedHostPath":   {"/var/log", "/var/lib/node-agent"},
			"allowedCapability": {"NET_ADMIN", "SYS_TIME"},
		} {
			if got := model.FieldValues(p, field, c.obj); !slices.Equal(got, want) {
				t.Errorf("%s = %q, want %q", field, got, want)
			}
		}
	}
}
