//go:build kind

package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// kyvernoRefusedNamespace holds the ServiceAccount of TestKindKyvernoRefusedClusterReports; the
// test deletes it with its cluster role and binding.
const kyvernoRefusedNamespace = "cogpit-kyverno-ref"

// TestKindKyvernoRefusedClusterReports checks, against a real Kyverno (make kind-kyverno), a user
// who may list the policy reports but not the cluster policy reports: the integration names the
// refused kind in its status and does not list it again while the cluster is shown; once the user
// may list them, cluster.refreshAccess reads them, the status drops the refusal, and the Namespace's
// violation, which only a cluster report carries, is listed.
func TestKindKyvernoRefusedClusterReports(t *testing.T) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	admin, err := cluster.Connect(ctx, cluster.Options{KubeconfigPaths: []string{kubeconfig}, Context: contextName})
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	served, err := admin.ServedResources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	polr := schema.GroupVersionResource{Group: kinds.PolicyReportGroup, Version: "v1alpha2", Resource: "policyreports"}
	if !served[polr] {
		t.Skip("the cluster serves no policy reports: run make kind-kyverno first")
	}
	kube := admin.Kube()
	if _, err := kube.CoreV1().Namespaces().Get(ctx, kyvernoNamespace, metav1.GetOptions{}); err != nil {
		t.Skipf("no %s: run make kind-kyverno first (%v)", kyvernoNamespace, err)
	}
	freshNamespace(ctx, t, kube, kyvernoRefusedNamespace)

	const sa = "kyverno-reader"
	if _, err := kube.CoreV1().ServiceAccounts(kyvernoRefusedNamespace).Create(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: sa}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	read := []string{"get", "list", "watch"}
	role := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: kyvernoRefusedNamespace},
		Rules: []rbacv1.PolicyRule{
			{Verbs: read, APIGroups: []string{""}, Resources: []string{"pods", "namespaces"}},
			{Verbs: read, APIGroups: []string{kinds.PolicyReportGroup}, Resources: []string{"policyreports"}},
		},
	}
	if _, err := kube.RbacV1().ClusterRoles().Create(ctx, role, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_ = kube.RbacV1().ClusterRoleBindings().Delete(bg, kyvernoRefusedNamespace, metav1.DeleteOptions{})
		_ = kube.RbacV1().ClusterRoles().Delete(bg, kyvernoRefusedNamespace, metav1.DeleteOptions{})
	})
	if _, err := kube.RbacV1().ClusterRoleBindings().Create(ctx, &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: kyvernoRefusedNamespace},
		Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: sa, Namespace: kyvernoRefusedNamespace}},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: kyvernoRefusedNamespace},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	expiry := int64(900)
	tr, err := kube.CoreV1().ServiceAccounts(kyvernoRefusedNamespace).CreateToken(ctx, sa, &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: &expiry},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// The token lives only in this temporary kubeconfig; it is never logged.
	path := writeTokenKubeconfig(t, admin, sa, kyvernoRefusedNamespace, tr.Status.Token)

	s := engine.New(engine.Options{
		Connector:       &engine.ClusterConnector{KubeconfigPaths: []string{path}},
		ClusterSettings: engine.NewClusterSettingsStore(t.TempDir()),
	})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChangeClusterSettings(sa, func(c *engine.ClusterSettings) error {
		c.SetIntegrations(engine.IntAll, engine.IntKyverno, 0)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, sa); err != nil {
		t.Fatal(err)
	}
	status := func() engine.IntegrationStatus {
		for _, st := range s.IntegrationStatuses(sa) {
			if st.Name == engine.IntegrationKyverno {
				return st
			}
		}
		return engine.IntegrationStatus{}
	}
	refused := func(st engine.IntegrationStatus) bool {
		return st.State == engine.IntegrationFound && strings.Contains(st.Detail, kinds.ClusterPolicyReportsKey)
	}
	if !testkit.WaitFor(ctx, func() bool { return refused(status()) }) {
		t.Fatalf("the status never named the refused cluster policy reports: %+v", status())
	}
	b := s.Backend()
	clusterReads := func() int64 {
		var n int64
		for k, c := range b.Conn.Counter().Snapshot().ByVerbResource {
			if strings.Contains(k, " clusterpolicyreports.") {
				n += c
			}
		}
		return n
	}
	before := clusterReads()
	time.Sleep(5 * time.Second) // client-go's backoff would list a held refused scope again within it
	if n := clusterReads(); n != before || !refused(status()) {
		t.Errorf("the refused cluster policy reports: %d reads, then %d; status %+v", before, n, status())
	}

	// The user may list them now: Refresh Access reads them.
	role.Rules = append(role.Rules, rbacv1.PolicyRule{Verbs: read, APIGroups: []string{kinds.PolicyReportGroup}, Resources: []string{"clusterpolicyreports"}})
	if _, err := kube.RbacV1().ClusterRoles().Update(ctx, role, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Dispatch(ctx, engine.Invocation{ID: "cluster.refreshAccess", Params: []byte(`{}`), Origin: engine.OriginGUI}); err != nil {
		t.Fatal(err)
	}
	if !testkit.WaitFor(ctx, func() bool { st := status(); return st.State == engine.IntegrationFound && !refused(st) }) {
		t.Fatalf("after cluster.refreshAccess the status still names the refusal: %+v", status())
	}
	params, _ := json.Marshal(map[string]any{"namespaces": []string{kyvernoNamespace}})
	nsViolation := func() bool {
		res, err := s.Dispatch(ctx, engine.Invocation{ID: "kyverno.violations", Params: params, Origin: engine.OriginExternalAgent})
		if err != nil {
			return false
		}
		return slices.ContainsFunc(res.(commands.KyvernoViolationsResult).Violations, func(v engine.PolicyViolation) bool {
			return v.Object.Kind == "namespaces" && v.Object.Name == kyvernoNamespace
		})
	}
	if !testkit.WaitFor(ctx, nsViolation) {
		t.Errorf("the Namespace's violation from the cluster report is not listed after Refresh Access (%d reads)", clusterReads())
	}
}
