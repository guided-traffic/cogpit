//go:build kind

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/kube"
	"helm.sh/helm/v4/pkg/release/common"
	rspb "helm.sh/helm/v4/pkg/release/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// helmNamespace is the namespace of the Helm kind tests (agent H of the third run); the tests
// delete it afterwards.
const helmNamespace = "cogpit-h"

// kindNamespace creates a fresh namespace on the kind cluster, waiting while an earlier one of the
// same name terminates, and deletes it when the test ends.
func kindNamespace(ctx context.Context, t *testing.T, kc kubernetes.Interface, name string) {
	t.Helper()
	_ = kc.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{})
	if !testkit.WaitFor(ctx, func() bool {
		_, err := kc.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}, metav1.CreateOptions{})
		return err == nil
	}) {
		t.Fatalf("cannot create the namespace %s", name)
	}
	t.Cleanup(func() {
		_ = kc.CoreV1().Namespaces().Delete(context.Background(), name, metav1.DeleteOptions{})
	})
}

// kindEnv returns the kind cluster's kubeconfig and context, or skips the test.
func kindEnv(t *testing.T) (string, string) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	return kubeconfig, contextName
}

// kindSession is an engine on the kind cluster with a confirmer that accepts and records, an
// audit log, and the given Helm journal.
type kindSession struct {
	s       *engine.Session
	audit   *auditBuffer
	mu      sync.Mutex
	asked   []engine.ConfirmRequest
	events  []engine.Event
	journal engine.HelmJournal
}

type auditBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (a *auditBuffer) Write(p []byte) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.buf.Write(p)
}

func (a *auditBuffer) String() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.buf.String()
}

func newKindSession(ctx context.Context, t *testing.T, kubeconfig, contextName string, journal engine.HelmJournal) *kindSession {
	t.Helper()
	k := &kindSession{audit: &auditBuffer{}, journal: journal}
	k.s = engine.New(engine.Options{
		Connector:        &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}},
		SnapshotInterval: 20 * time.Millisecond,
		Audit:            k.audit,
		Helm:             engine.NewHelmOps(journal),
		Confirmer: engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
			k.mu.Lock()
			k.asked = append(k.asked, req)
			k.mu.Unlock()
			return true, nil
		}),
	})
	if err := commands.RegisterAll(k.s.Registry()); err != nil {
		t.Fatal(err)
	}
	k.s.Subscribe(func(ev engine.Event) {
		k.mu.Lock()
		k.events = append(k.events, ev)
		k.mu.Unlock()
	})
	if err := k.s.Connect(ctx, contextName); err != nil {
		t.Fatal(err)
	}
	return k
}

func (k *kindSession) dispatch(ctx context.Context, origin engine.Origin, id string, params any) (any, error) {
	raw, _ := json.Marshal(params)
	return k.s.Dispatch(ctx, engine.Invocation{ID: id, Params: raw, Origin: origin})
}

func (k *kindSession) detail(ctx context.Context, t *testing.T, origin engine.Origin, ref model.ObjectRef) *engine.DetailModel {
	t.Helper()
	res, err := k.dispatch(ctx, origin, "object.get", map[string]any{"ref": ref})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res)
	var m engine.DetailModel
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return &m
}

// TestKindHelmReleases installs a local test chart with the Helm SDK and upgrades it, then drives
// the release through Cogpit's engine on the kind cluster: the Releases list, the drawer's history,
// values and manifest, a rollback, a simulated crash of a second rollback (the journal written, the
// release left pending-rollback, the SDK never run) that the next session offers and finishes, and
// the uninstall.
func TestKindHelmReleases(t *testing.T) {
	kubeconfig, contextName := kindEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	conn, err := cluster.Connect(ctx, cluster.Options{KubeconfigPaths: []string{kubeconfig}, Context: contextName})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	kc := conn.Kube()
	kindNamespace(ctx, t, kc, helmNamespace)

	// Install and upgrade with the Helm SDK, as a user's helm would.
	const name = "h-web"
	cfg, err := helm.NewActionConfig(conn.RESTConfig(), helmNamespace, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	chrt, err := loader.Load("testdata/helmchart")
	if err != nil {
		t.Fatal(err)
	}
	install := action.NewInstall(cfg)
	install.ReleaseName, install.Namespace = name, helmNamespace
	install.WaitStrategy, install.Timeout = kube.HookOnlyStrategy, time.Minute
	if _, err := install.RunWithContext(ctx, chrt, map[string]any{"greeting": "v1", "password": "values-s3cr3t"}); err != nil {
		t.Fatalf("install: %v", err)
	}
	upgrade := action.NewUpgrade(cfg)
	upgrade.Namespace = helmNamespace
	upgrade.WaitStrategy, upgrade.Timeout = kube.HookOnlyStrategy, time.Minute
	if _, err := upgrade.RunWithContext(ctx, name, chrt, map[string]any{"greeting": "v2", "password": "values-s3cr3t"}); err != nil {
		t.Fatalf("upgrade: %v", err)
	}

	journal := engine.NewMemoryHelmJournal()
	k := newKindSession(ctx, t, kubeconfig, contextName, journal)
	ref := model.ObjectRef{Context: contextName, Kind: helm.ReleaseKind, Namespace: helmNamespace, Name: name}
	waitRelease := func(k *kindSession, rev int, status string) {
		t.Helper()
		if !testkit.WaitFor(ctx, func() bool {
			obj, err := k.s.Lookup(ref)
			if rev == 0 {
				return errors.Is(err, engine.ErrNotFound)
			}
			return err == nil && helm.ReleaseRevision(obj) == rev && helm.ReleaseStatus(obj) == status
		}) {
			obj, err := k.s.Lookup(ref)
			t.Fatalf("release never at revision %d %s: %v %v", rev, status, obj, err)
		}
	}

	// The list: one row, revision 2, deployed.
	res, err := k.dispatch(ctx, engine.OriginExternalAgent, "resources.list", map[string]any{"kind": helm.ReleaseKind, "namespaces": []string{helmNamespace}})
	if err != nil {
		t.Fatal(err)
	}
	var list commands.ListResult
	b, _ := json.Marshal(res)
	_ = json.Unmarshal(b, &list)
	if list.Total != 1 || list.Cell(0, "Revision") != "2" || list.Cell(0, "Status") != "Deployed" || list.Cell(0, "Chart") != "cogpit-test" {
		t.Fatalf("releases: %s", b)
	}

	// The drawer as Lens's: values and notes withheld from agents, the resources by kind; the
	// history from helm.release.history.
	m := k.detail(ctx, t, engine.OriginExternalAgent, ref)
	if b, _ := json.Marshal(m); strings.Contains(string(b), "values-s3cr3t") {
		t.Fatalf("object.get for an agent carries a value: %s", b)
	}
	if sectionOf(m, helm.TitleValues) != model.Withheld || sectionOf(m, helm.TitleNotes) != model.Withheld {
		t.Errorf("values and notes for an agent: %q, %q", sectionOf(m, helm.TitleValues), sectionOf(m, helm.TitleNotes))
	}
	var kinds []string
	for _, s := range m.Sections {
		if s.Group == helm.GroupResources {
			kinds = append(kinds, s.Title)
		}
	}
	if strings.Join(kinds, ",") != "ConfigMap,Secret" {
		t.Errorf("resources %v", kinds)
	}
	hres, err := k.dispatch(ctx, engine.OriginExternalAgent, "helm.release.history", map[string]any{"ref": ref})
	if err != nil {
		t.Fatal(err)
	}
	if h := hres.(commands.ReleaseHistory); len(h.History) != 2 || h.History[0].Status != "deployed" || h.History[1].Status != "superseded" {
		t.Errorf("history %+v", h)
	}
	if cli := k.detail(ctx, t, engine.OriginCLI, ref); !strings.Contains(sectionOf(cli, helm.TitleValues), "greeting: v2") {
		t.Errorf("values for the CLI: %q", sectionOf(cli, helm.TitleValues))
	}

	// Roll back to revision 1 through the engine.
	if _, err := k.dispatch(ctx, engine.OriginGUI, "helm.release.rollback", map[string]any{"ref": ref, "revision": 1}); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	waitRelease(k, 3, "deployed")
	if greeting(ctx, t, kc, name) != "v1" {
		t.Error("the rollback did not restore revision 1's ConfigMap")
	}
	if list, _ := journal.List(); len(list) != 0 {
		t.Errorf("journal after the rollback: %+v", list)
	}
	k.s.Close()

	// A crash during a rollback to revision 2: Cogpit recorded it, the SDK created revision 4 as
	// pending-rollback, and the process died before the release's objects changed.
	rev2, err := cfg.Releases.Get(name, 2)
	if err != nil {
		t.Fatal(err)
	}
	pending := *rev2.(*rspb.Release)
	info := *pending.Info
	info.Status, info.Description, info.LastDeployed = common.StatusPendingRollback, "Rollback to 2", time.Now()
	pending.Info, pending.Version = &info, 4
	if err := cfg.Releases.Create(&pending); err != nil {
		t.Fatal(err)
	}
	if err := journal.Put(engine.PendingHelmOp{Context: contextName, Namespace: helmNamespace, Release: name,
		Action: engine.HelmRollback, Revision: 2, FromRevision: 3, Started: time.Now().UTC(), StartedBy: engine.OriginGUI,
		State: engine.HelmOpRunning}); err != nil {
		t.Fatal(err)
	}

	// The next session offers it, with the release's state from the cache.
	k2 := newKindSession(ctx, t, kubeconfig, contextName, journal)
	defer k2.s.Close()
	var offered []engine.PendingHelmOpInfo
	if !testkit.WaitFor(ctx, func() bool {
		res, err := k2.dispatch(ctx, engine.OriginExternalAgent, "helm.pendingOperations", struct{}{})
		if err != nil {
			return false
		}
		b, _ := json.Marshal(res)
		offered = nil
		_ = json.Unmarshal(b, &offered)
		return len(offered) == 1 && offered[0].ReleaseStatus == "pending-rollback"
	}) {
		t.Fatalf("not offered: %+v", offered)
	}
	if o := offered[0]; o.Status != engine.HelmOpStatusInterrupted || o.ReleaseRevision != 4 || o.Revision != 2 {
		t.Errorf("offer %+v", o)
	}
	if !testkit.WaitFor(ctx, func() bool {
		k2.mu.Lock()
		defer k2.mu.Unlock()
		for _, ev := range k2.events {
			if ev.Type == engine.EventHelm && strings.Contains(ev.Message, "The rollback of release "+name) {
				return true
			}
		}
		return false
	}) {
		t.Error("the offer was not announced")
	}
	// Finish it: the rollback again, as recorded.
	if _, err := k2.dispatch(ctx, engine.OriginGUI, "helm.release.rollback", map[string]any{"ref": ref, "revision": offered[0].Revision}); err != nil {
		t.Fatalf("finishing the rollback: %v", err)
	}
	waitRelease(k2, 5, "deployed")
	if greeting(ctx, t, kc, name) != "v2" {
		t.Error("finishing the rollback did not restore revision 2's ConfigMap")
	}
	if list, _ := journal.List(); len(list) != 0 {
		t.Errorf("journal after finishing: %+v", list)
	}

	// Uninstall through the engine.
	if _, err := k2.dispatch(ctx, engine.OriginExternalAgent, "helm.release.uninstall", map[string]any{"ref": ref}); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	waitRelease(k2, 0, "")
	if _, err := kc.CoreV1().ConfigMaps(helmNamespace).Get(ctx, name+"-config", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("the release's ConfigMap after the uninstall: %v", err)
	}
	k2.mu.Lock()
	asked := len(k2.asked)
	k2.mu.Unlock()
	if asked != 2 {
		t.Errorf("%d confirmations in the second session, want 2 (rollback, uninstall)", asked)
	}
	for _, leak := range []string{"values-s3cr3t", "dmFsdWVzLXMzY3IzdA=="} {
		if strings.Contains(k.audit.String()+k2.audit.String(), leak) {
			t.Errorf("%q in the audit log", leak)
		}
	}
	st := conn.Counter().Snapshot()
	t.Logf("requests of the test's own SDK client: %v", st.ByVerbResource)
	t.Logf("requests of the second session (cache, SDK actions): %v", k2.s.Backend().Conn.Counter().Snapshot().ByVerbResource)
}

// greeting reads the test chart's ConfigMap.
func greeting(ctx context.Context, t *testing.T, kc kubernetes.Interface, release string) string {
	t.Helper()
	cm, err := kc.CoreV1().ConfigMaps(helmNamespace).Get(ctx, release+"-config", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return cm.Data["greeting"]
}
