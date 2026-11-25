package engine

import (
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestDebugProfiles: a profile is parsed from its name (empty: General), named by its label;
// Restricted's security context is what Pod Security's restricted level asks of a container, run as
// the target's user, nobody when that is root or only the image sets it; Netadmin adds NET_ADMIN
// and NET_RAW; General sets none. The confirmation names the profile and what it grants.
func TestDebugProfiles(t *testing.T) {
	for in, want := range map[string]DebugProfile{"": DebugGeneral, "general": DebugGeneral, " Restricted ": DebugRestricted, "netadmin": DebugNetadmin} {
		if got, err := ParseDebugProfile(in); err != nil || got != want {
			t.Errorf("ParseDebugProfile(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := ParseDebugProfile("sysadmin"); err == nil {
		t.Error("sysadmin parsed")
	}
	for _, p := range DebugProfiles {
		if DebugProfileLabeled(p.Label()) != p {
			t.Errorf("label %q", p.Label())
		}
	}

	pod := testkit.Pod("shop", "web-0")
	spec := pod.Object["spec"].(map[string]any)
	spec["securityContext"] = map[string]any{"runAsUser": int64(1000)}
	spec["containers"] = []any{
		map[string]any{"name": "app"},
		map[string]any{"name": "proxy", "securityContext": map[string]any{"runAsUser": int64(101)}},
		map[string]any{"name": "root", "securityContext": map[string]any{"runAsUser": int64(0)}},
	}
	spec["initContainers"] = []any{map[string]any{"name": "init"}}
	users := DebugUsers(pod)
	if want := map[string]int64{"": 1000, "app": 1000, "proxy": 101, "root": 0, "init": 1000}; len(users) != len(want) {
		t.Errorf("users %v", users)
	} else {
		for k, v := range want {
			if users[k] != v {
				t.Errorf("users %v, want %v", users, want)
			}
		}
	}
	for target, want := range map[string]int64{"": 1000, "app": 1000, "proxy": 101, "root": DebugNobody} {
		if got := DebugUser(users, target); got != want {
			t.Errorf("DebugUser(%q) = %d, want %d", target, got, want)
		}
	}
	if got := DebugUser(DebugUsers(testkit.Pod("shop", "web-1")), "app"); got != DebugNobody {
		t.Errorf("without users in the spec: %d", got)
	}

	if DebugSecurityContext(DebugGeneral, 1000) != nil {
		t.Error("General sets a security context")
	}
	sc := DebugSecurityContext(DebugRestricted, 101)
	if sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation || sc.Capabilities == nil ||
		!slices.Equal(sc.Capabilities.Drop, []corev1.Capability{"ALL"}) || len(sc.Capabilities.Add) != 0 ||
		sc.RunAsNonRoot == nil || !*sc.RunAsNonRoot || sc.RunAsUser == nil || *sc.RunAsUser != 101 ||
		sc.SeccompProfile == nil || sc.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault || sc.Privileged != nil {
		t.Errorf("restricted %+v", sc)
	}
	if sc := DebugSecurityContext(DebugNetadmin, 0); sc == nil || sc.Capabilities == nil ||
		!slices.Equal(sc.Capabilities.Add, []corev1.Capability{"NET_ADMIN", "NET_RAW"}) || len(sc.Capabilities.Drop) != 0 || sc.RunAsUser != nil {
		t.Errorf("netadmin %+v", sc)
	}

	if got := DebugEffects("app", "busybox:1.37", DebugRestricted, 1000)[2:4]; !slices.Equal(got, []Effect{{Name: "Profile", Value: "Restricted"},
		{Name: "Rights", Value: "user 1000, no capabilities, no privilege escalation, seccomp RuntimeDefault"}}) {
		t.Errorf("restricted rows %+v", got)
	}
	if got := DebugEffects("", "busybox:1.37", DebugNetadmin, 0)[1:3]; !slices.Equal(got, []Effect{{Name: "Profile", Value: "Netadmin"},
		{Name: "Rights", Value: "the image's user, the runtime's default capabilities and NET_ADMIN, NET_RAW", Tone: EffectWarning}}) {
		t.Errorf("netadmin rows %+v", got)
	}

	ns := testkit.Namespace("shop")
	if DebugProfileFor(ns) != DebugGeneral || DebugProfileFor(nil) != DebugGeneral {
		t.Error("an unlabelled namespace preselects another profile")
	}
	ns.SetLabels(map[string]string{"pod-security.kubernetes.io/enforce": "baseline"})
	if DebugProfileFor(ns) != DebugGeneral {
		t.Error("baseline preselects Restricted")
	}
	ns.SetLabels(map[string]string{"pod-security.kubernetes.io/enforce": "restricted"})
	if DebugProfileFor(ns) != DebugRestricted {
		t.Error("restricted does not preselect Restricted")
	}
}
