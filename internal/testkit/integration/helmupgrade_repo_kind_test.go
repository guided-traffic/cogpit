//go:build kind

package integration_test

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// repoUpgradeNamespace is the namespace of the kind test of an upgrade to a repository's version
// (agent U of the night run); the test creates and deletes it.
const repoUpgradeNamespace = "cogpit-u-upgrade"

// uChart is the test chart u-web at a version: a ConfigMap with the greeting and the chart's
// version, and from 0.2.0 on a second ConfigMap, which an upgrade to it creates and a downgrade
// deletes.
func uChart(version string) testkit.TestChart {
	c := testkit.TestChart{
		Name: "u-web", Version: version, AppVersion: "1.0", Description: "Cogpit's upgrade test chart",
		Values: "# What the ConfigMap greets with.\ngreeting: hello\n",
		Templates: map[string]string{
			"templates/cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: {{ .Release.Name }}-config\n" +
				"data:\n  greeting: {{ .Values.greeting | quote }}\n  chart: {{ .Chart.Version | quote }}\n",
		},
	}
	if version != "0.1.0" {
		c.Templates["templates/extra.yaml"] = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: {{ .Release.Name }}-extra\n"
	}
	return c
}

// TestKindHelmUpgradeFromRepository installs version 0.1.0 of a chart served by an httptest
// repository (the SDK runs in Cogpit's process; the cluster never reaches the repository) into its
// own namespace through Cogpit's engine on the kind cluster, then upgrades it from the Helm Upgrade
// tab to 0.2.0, which the tab offers and starts on, with the requests counted; an agent's upgrade
// back to 0.1.0 with inline values is confirmed naming both versions; then the release is
// uninstalled and the namespace deleted.
func TestKindHelmUpgradeFromRepository(t *testing.T) {
	kubeconfig, contextName := kindEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	conn, err := cluster.Connect(ctx, cluster.Options{KubeconfigPaths: []string{kubeconfig}, Context: contextName})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	kc := conn.Kube()
	kindNamespace(ctx, t, kc, repoUpgradeNamespace)

	repo := testkit.NewHelmRepo(t, uChart("0.1.0"), uChart("0.2.0"))
	helmFile := testkit.NewHelmRepoFile(t) // Helm's repositories.yaml, temporary (D-29)
	helmFile.Write(repo.Entry("u"))
	journal := engine.NewMemoryHelmJournal()
	k := &kindSession{audit: &auditBuffer{}, journal: journal}
	var mu sync.Mutex
	k.s = engine.New(engine.Options{
		Connector:        &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}},
		SnapshotInterval: 20 * time.Millisecond,
		Audit:            k.audit,
		Helm:             engine.NewHelmOps(journal),
		HelmRepos:        engine.NewHelmRepos(helmFile.Path, t.TempDir(), engine.WithChartFetcher(&helm.Fetcher{Client: repo.Client()})),
		Confirmer: engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
			mu.Lock()
			defer mu.Unlock()
			k.asked = append(k.asked, req)
			return true, nil
		}),
	})
	defer k.s.Close()
	if err := commands.RegisterAll(k.s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := k.s.Connect(ctx, contextName); err != nil {
		t.Fatal(err)
	}
	if _, err := k.dispatch(ctx, engine.OriginGUI, "helm.repo.update", map[string]any{"names": []string{"u"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.s.Objects(ctx, helm.ReleaseKind, nil); err != nil {
		t.Fatal(err)
	}

	// Version 0.1.0 installed through the engine.
	const name = "u-web"
	ref := engine.HelmReleaseRef(contextName, repoUpgradeNamespace, name)
	if _, err := k.dispatch(ctx, engine.OriginGUI, "helm.chart.install", map[string]any{
		"repository": "u", "chart": "u-web", "version": "0.1.0", "values": "greeting: one\n", "namespace": repoUpgradeNamespace, "name": name}); err != nil {
		t.Fatalf("install: %v", err)
	}
	t.Cleanup(func() { // a failed test leaves no release behind
		_, _ = k.dispatch(context.Background(), engine.OriginGUI, "helm.release.uninstall", map[string]any{"ref": ref})
	})
	waitAt := func(rev int, version string) {
		t.Helper()
		if !testkit.WaitFor(ctx, func() bool {
			obj, err := k.s.Lookup(ref)
			if err != nil {
				return false
			}
			_, v := helm.ReleaseChart(obj)
			return helm.ReleaseRevision(obj) == rev && helm.ReleaseStatus(obj) == "deployed" && v == version
		}) {
			t.Fatalf("release %s never deployed at revision %d with chart %s", name, rev, version)
		}
	}
	waitAt(1, "0.1.0")

	// The GUI's path: the tab offers the repository's versions and starts on 0.2.0.
	res, err := k.dispatch(ctx, engine.OriginGUI, helm.UpgradeTabCommand, map[string]any{"ref": ref})
	if err != nil {
		t.Fatal(err)
	}
	tab := res.(commands.EditResult)
	if c := tab.Tab.Helm; c == nil || c.Repository != "u" || c.ReleaseVersion != "0.1.0" || c.Version != "0.2.0" || strings.Join(c.Versions, ",") != "0.2.0,0.1.0" ||
		tab.Text != "greeting: one\n" {
		t.Fatalf("tab %+v %+v", tab.Tab.Helm, tab)
	}
	if _, err := k.dispatch(ctx, engine.OriginGUI, "editor.setText", map[string]any{"id": tab.Tab.ID, "yaml": "greeting: two\n"}); err != nil {
		t.Fatal(err)
	}
	counter := k.s.Backend().Conn.Counter()
	before := counter.Snapshot()
	res, err = k.dispatch(ctx, engine.OriginGUI, "helm.release.upgrade", map[string]any{"ref": ref, "tab": tab.Tab.ID,
		"textRevision": kindTabRevision(ctx, t, k, tab.Tab.ID), "version": "0.2.0", "repository": "u"})
	if err != nil {
		t.Fatalf("upgrade to 0.2.0: %v", err)
	}
	t.Logf("requests of the upgrade to 0.2.0 (the SDK's, and the cache's meanwhile): %v", delta(before, counter.Snapshot()))
	if r := res.(commands.HelmResult); r.Revision != 2 {
		t.Errorf("result %+v", r)
	}
	waitAt(2, "0.2.0")
	cm, err := kc.CoreV1().ConfigMaps(repoUpgradeNamespace).Get(ctx, name+"-config", metav1.GetOptions{})
	if err != nil || cm.Data["greeting"] != "two" || cm.Data["chart"] != "0.2.0" {
		t.Fatalf("the ConfigMap after the upgrade: %v %v", cm, err)
	}
	if _, err := kc.CoreV1().ConfigMaps(repoUpgradeNamespace).Get(ctx, name+"-extra", metav1.GetOptions{}); err != nil {
		t.Errorf("the ConfigMap 0.2.0 adds: %v", err)
	}
	if n := repo.Requests("/u-web-0.2.0.tgz"); n != 1 {
		t.Errorf("the archive was downloaded %d times", n)
	}
	if list, _ := journal.List(); len(list) != 0 {
		t.Errorf("journal after the upgrade: %+v", list)
	}

	// An agent's upgrade back to 0.1.0 with inline values: confirmed naming both versions.
	before = counter.Snapshot()
	if _, err := k.dispatch(ctx, engine.OriginExternalAgent, "helm.release.upgrade", map[string]any{
		"ref": ref, "values": "greeting: agent-s3cr3t\n", "revision": 2, "version": "0.1.0"}); err != nil {
		t.Fatalf("an agent's upgrade to 0.1.0: %v", err)
	}
	t.Logf("requests of the downgrade to 0.1.0: %v", delta(before, counter.Snapshot()))
	waitAt(3, "0.1.0")
	if _, err := kc.CoreV1().ConfigMaps(repoUpgradeNamespace).Get(ctx, name+"-extra", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("the ConfigMap 0.1.0 does not have: %v", err)
	}
	mu.Lock()
	asked := slices.Clone(k.asked)
	mu.Unlock()
	if len(asked) != 1 || asked[0].Command != "helm.release.upgrade" || asked[0].Review != "greeting: agent-s3cr3t\n" ||
		!strings.Contains(asked[0].Detail, "Chart: u-web 0.2.0 → 0.1.0, an older version. Repository: u.") {
		t.Errorf("confirmations %+v", asked)
	}
	if strings.Contains(k.audit.String(), "agent-s3cr3t") {
		t.Error("the agent's value is in the audit log")
	}

	if _, err := k.dispatch(ctx, engine.OriginGUI, "helm.release.uninstall", map[string]any{"ref": ref}); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := kc.CoreV1().ConfigMaps(repoUpgradeNamespace).Get(ctx, name+"-config", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("the ConfigMap after the uninstall: %v", err)
	}
	if err := kc.CoreV1().Namespaces().Delete(ctx, repoUpgradeNamespace, metav1.DeleteOptions{}); err != nil {
		t.Errorf("deleting the namespace: %v", err)
	}
}
