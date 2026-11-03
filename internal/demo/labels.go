package demo

import (
	"hash/fnv"
	"strconv"
	"strings"
)

// The Kubernetes recommended labels the demo's deployments carry, as charts and GitOps tools set
// them on a real cluster: a user deployment "<domain>-<role>" is the <role> component of the
// application instance <domain> in its namespace. Every workload of an instance shares its version
// and manager, both chosen by a hash of the namespace and the domain, never by the random
// generator, so the rest of the cluster stays as it was.
const (
	labelInstance  = "app.kubernetes.io/instance"
	labelName      = "app.kubernetes.io/name"
	labelComponent = "app.kubernetes.io/component"
	labelPartOf    = "app.kubernetes.io/part-of"
	labelVersion   = "app.kubernetes.io/version"
	labelManagedBy = "app.kubernetes.io/managed-by"
)

// managers are the app.kubernetes.io/managed-by values the demo's applications draw from.
var managers = []string{"Helm", "Helm", "kustomize", "argocd"}

// deploymentLabels are a deployment's metadata labels: app=<name> as before, and the recommended
// labels for a deployment outside kube-system, whose system components carry none.
func deploymentLabels(namespace, name string) map[string]any {
	labels := map[string]any{appLabel: name}
	if namespace == kubeSystem {
		return labels
	}
	domain, role, _ := strings.Cut(name, "-")
	if role == "" {
		role = domain
	}
	if i := strings.LastIndexByte(role, '-'); i > 0 { // "api-2", a second deployment of the role
		if _, err := strconv.Atoi(role[i+1:]); err == nil {
			role = role[:i]
		}
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(namespace + "/" + domain))
	sum := h.Sum32()
	labels[labelInstance] = domain
	labels[labelName] = domain
	labels[labelComponent] = role
	labels[labelPartOf] = strings.TrimPrefix(namespace, "demo-")
	labels[labelVersion] = strconv.Itoa(1+int(sum%3)) + "." + strconv.Itoa(int(sum/3%20)) + "." + strconv.Itoa(int(sum/60%10))
	labels[labelManagedBy] = managers[sum/600%uint32(len(managers))]
	return labels
}
