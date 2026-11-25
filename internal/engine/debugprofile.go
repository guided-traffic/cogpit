package engine

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The debug container's profiles (D-102), after kubectl debug --profile: the security context the
// ephemeral container gets. Pod Security admission and policy engines check an ephemeral container
// as any other, so a namespace that enforces the restricted level, or a policy that asks the same
// of ephemeral containers, refuses one without a security context of its own; Restricted passes
// them, at the cost of what netshoot does with root and the runtime's capabilities.

// DebugProfile is a debug container's profile.
type DebugProfile string

const (
	// DebugGeneral sets no security context: the image's user and the container runtime's default
	// capabilities, as far as the pod's own settings allow. Unlike kubectl's general it adds no
	// SYS_PTRACE.
	DebugGeneral DebugProfile = "general"
	// DebugRestricted is what Pod Security's restricted level asks of a container: no privilege
	// escalation, every capability dropped, a user other than root, seccomp RuntimeDefault. The
	// user is the target's (DebugUser), so the container still reads the target's processes.
	DebugRestricted DebugProfile = "restricted"
	// DebugNetadmin adds NET_ADMIN and NET_RAW to the runtime's defaults, for tcpdump, iptables and
	// tc on a runtime that does not grant NET_RAW.
	DebugNetadmin DebugProfile = "netadmin"
)

// DebugProfiles are the profiles in the order the dialog offers them.
var DebugProfiles = []DebugProfile{DebugGeneral, DebugRestricted, DebugNetadmin}

// DebugNobody is the user a Restricted debug container runs as when the pod names no user other
// than root for its target: the conventional nobody.
const DebugNobody int64 = 65534

// podSecurityEnforce is the namespace label that sets the Pod Security level admission enforces.
const podSecurityEnforce = "pod-security.kubernetes.io/enforce"

// ParseDebugProfile returns the profile s names; empty is General.
func ParseDebugProfile(s string) (DebugProfile, error) {
	switch p := DebugProfile(strings.ToLower(strings.TrimSpace(s))); p {
	case "":
		return DebugGeneral, nil
	case DebugGeneral, DebugRestricted, DebugNetadmin:
		return p, nil
	}
	return "", fmt.Errorf("no debug profile %q: general, restricted or netadmin", s)
}

// Label is the profile as the dialog and the confirmation name it.
func (p DebugProfile) Label() string {
	switch p {
	case DebugRestricted:
		return "Restricted"
	case DebugNetadmin:
		return "Netadmin"
	}
	return "General"
}

// DebugProfileLabeled returns the profile whose Label is label, General for any other.
func DebugProfileLabeled(label string) DebugProfile {
	for _, p := range DebugProfiles {
		if p.Label() == label {
			return p
		}
	}
	return DebugGeneral
}

// DebugProfileFor is the profile the dialog preselects in a namespace: Restricted where Pod
// Security admission enforces the restricted level, which refuses General; else General. A policy
// engine's rules are not read: there the user picks Restricted.
func DebugProfileFor(namespace *unstructured.Unstructured) DebugProfile {
	if namespace != nil && namespace.GetLabels()[podSecurityEnforce] == "restricted" {
		return DebugRestricted
	}
	return DebugGeneral
}

// DebugUsers are the user IDs a pod's containers and init containers run as by its spec: their own
// securityContext.runAsUser, else the pod's; the key "" holds the pod's. A container whose user
// only its image sets is missing.
func DebugUsers(pod *unstructured.Unstructured) map[string]int64 {
	out := map[string]int64{}
	podUser, podSet := model.NestedInt64(pod.Object, "spec", "securityContext", "runAsUser")
	if podSet {
		out[""] = podUser
	}
	for _, field := range []string{"containers", "initContainers"} {
		for _, c := range model.NestedSlice(pod.Object, "spec", field) {
			name := model.NestedString(c, "name")
			if u, ok := model.NestedInt64(c, "securityContext", "runAsUser"); ok {
				out[name] = u
			} else if podSet {
				out[name] = podUser
			}
		}
	}
	return out
}

// DebugUser is the user a Restricted debug container runs as: its target's (the pod's without a
// target) as DebugUsers has it, so that it reads the target's processes and files as their owner;
// DebugNobody when that is root or unknown.
func DebugUser(users map[string]int64, target string) int64 {
	if u := users[target]; u > 0 {
		return u
	}
	return DebugNobody
}

// DebugSecurityContext is the security context of a debug container with profile p, run as user
// under Restricted; nil for General.
func DebugSecurityContext(p DebugProfile, user int64) *corev1.SecurityContext {
	switch p {
	case DebugRestricted:
		no, yes := false, true
		return &corev1.SecurityContext{
			AllowPrivilegeEscalation: &no,
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
			RunAsNonRoot:             &yes,
			RunAsUser:                &user,
			SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		}
	case DebugNetadmin:
		return &corev1.SecurityContext{Capabilities: &corev1.Capabilities{Add: []corev1.Capability{"NET_ADMIN", "NET_RAW"}}}
	}
	return nil
}

// debugProfileEffects are a profile's rows in the confirmation: its name and what it grants.
func debugProfileEffects(p DebugProfile, user int64) []Effect {
	rights := Effect{Name: "Rights", Value: "the image's user, the runtime's default capabilities"}
	switch p {
	case DebugRestricted:
		rights.Value = fmt.Sprintf("user %d, no capabilities, no privilege escalation, seccomp RuntimeDefault", user)
	case DebugNetadmin:
		rights = Effect{Name: "Rights", Value: "the image's user, the runtime's default capabilities and NET_ADMIN, NET_RAW",
			Tone: EffectWarning}
	}
	return []Effect{{Name: "Profile", Value: p.Label()}, rights}
}
