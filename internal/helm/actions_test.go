package helm

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"helm.sh/helm/v4/pkg/action"
	chartcommon "helm.sh/helm/v4/pkg/chart/common"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	kubefake "helm.sh/helm/v4/pkg/kube/fake"
	"helm.sh/helm/v4/pkg/release/common"
	rspb "helm.sh/helm/v4/pkg/release/v1"
	"helm.sh/helm/v4/pkg/storage"
	"helm.sh/helm/v4/pkg/storage/driver"
)

// sdkFixture is the Helm SDK on an in-memory release store and a kube client that only prints:
// the SDK's own rollback, uninstall and upgrade logic, without a cluster. The chart renders one
// ConfigMap from .Values.greeting and fails on .Values.boom.
func sdkFixture(t *testing.T, revisions ...common.Status) (*Actions, *storage.Storage) {
	t.Helper()
	store := storage.Init(driver.NewMemory())
	for i, st := range revisions {
		rls := &rspb.Release{
			Name: "web", Namespace: "apps", Version: i + 1,
			Info:     &rspb.Info{Status: st, FirstDeployed: t0, LastDeployed: t0},
			Chart:    fixtureChart(),
			Manifest: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: web\n",
			Config:   map[string]any{"rev": i + 1},
		}
		if err := store.Create(rls); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &action.Configuration{
		Releases:     store,
		KubeClient:   &kubefake.PrintingKubeClient{Out: io.Discard},
		Capabilities: chartcommon.DefaultCapabilities,
	}
	return &Actions{config: func(string, Driver, *slog.Logger) (*action.Configuration, error) { return cfg, nil }}, store
}

func fixtureChart() *chart.Chart {
	return &chart.Chart{
		Metadata: &chart.Metadata{Name: "web", Version: "1.0.0", APIVersion: "v2"},
		Values:   map[string]any{"greeting": "hello", "boom": false},
		Templates: []*chartcommon.File{{Name: "templates/cm.yaml", Data: []byte(
			"{{ if .Values.boom }}{{ fail (printf \"cannot use %s\" .Values.boom) }}{{ end }}" +
				"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: web\ndata:\n  greeting: {{ .Values.greeting | quote }}\n")}},
	}
}

func last(t *testing.T, store *storage.Storage) *rspb.Release {
	t.Helper()
	r, err := store.Last("web")
	if err != nil {
		t.Fatal(err)
	}
	rls, ok := r.(*rspb.Release)
	if !ok {
		t.Fatalf("release of type %T", r)
	}
	return rls
}

// Rollback makes a new revision from the chosen one, deployed, and supersedes the others; a
// revision the history lacks is an error that names it. A release a crash left pending-rollback
// can be rolled back again.
func TestRollbackThroughTheSDK(t *testing.T) {
	a, store := sdkFixture(t, common.StatusSuperseded, common.StatusDeployed)
	if err := a.Rollback("apps", "web", 1, 2); err != nil {
		t.Fatal(err)
	}
	rls := last(t, store)
	if rls.Version != 3 || rls.Info.Status != common.StatusDeployed || rls.Config["rev"] != 1 || rls.Info.RollbackRevision != 1 {
		t.Errorf("after rollback: revision %d status %s config %v", rls.Version, rls.Info.Status, rls.Config)
	}
	if err := a.Rollback("apps", "web", 9, 0); err == nil || !strings.Contains(err.Error(), "helm rollback apps/web to revision 9") {
		t.Errorf("rollback to a missing revision: %v", err)
	}

	crashed, store2 := sdkFixture(t, common.StatusSuperseded, common.StatusDeployed, common.StatusPendingRollback)
	if err := crashed.Rollback("apps", "web", 1, 0); err != nil {
		t.Fatalf("finishing a pending rollback: %v", err)
	}
	if rls := last(t, store2); rls.Version != 4 || rls.Info.Status != common.StatusDeployed {
		t.Errorf("after finishing: revision %d status %s", rls.Version, rls.Info.Status)
	}
}

// An upgrade or a rollback based on a revision the server no longer holds as its newest (the
// caller's cache lagged) is refused before the SDK changes anything (review of the fourth run,
// R-1).
func TestActionsRefuseWhenTheServerMoved(t *testing.T) {
	a, store := sdkFixture(t, common.StatusSuperseded, common.StatusDeployed)
	if _, err := a.Upgrade(context.Background(), "apps", "web", "", 1, map[string]any{"greeting": "stale"}); !errors.Is(err, ErrReleaseMoved) || !errors.Is(err, ErrUnchanged) ||
		!strings.Contains(err.Error(), "revision 2 on the server") {
		t.Errorf("upgrade based on revision 1 of 2: %v", err)
	}
	if err := a.Rollback("apps", "web", 1, 1); !errors.Is(err, ErrReleaseMoved) || !errors.Is(err, ErrUnchanged) {
		t.Errorf("rollback based on revision 1 of 2: %v", err)
	}
	if rls := last(t, store); rls.Version != 2 || rls.Info.Status != common.StatusDeployed {
		t.Errorf("the release changed: revision %d status %s", rls.Version, rls.Info.Status)
	}
	if _, err := a.Upgrade(context.Background(), "apps", "web", "", 1, map[string]any{"boom": "s3cr3t-value"}); errors.Is(err, ErrReleaseMoved) && strings.Contains(err.Error(), "s3cr3t") {
		t.Errorf("the refusal quotes a value: %v", err)
	}
}

// Uninstall removes the release and its history; one a crash left uninstalling is uninstalled
// again; a release that does not exist is an error.
func TestUninstallThroughTheSDK(t *testing.T) {
	for _, states := range [][]common.Status{
		{common.StatusSuperseded, common.StatusDeployed},
		{common.StatusDeployed, common.StatusUninstalling},
		{common.StatusPendingInstall}, // an install a crash interrupted: what the journal offers to uninstall
	} {
		a, store := sdkFixture(t, states...)
		if err := a.Uninstall("apps", "web"); err != nil {
			t.Fatalf("%v: %v", states, err)
		}
		if h, err := store.History("web"); err == nil && len(h) > 0 {
			t.Errorf("%v: %d revisions left", states, len(h))
		}
		if err := a.Uninstall("apps", "web"); err == nil {
			t.Error("uninstalling a release that is gone succeeded")
		}
	}
}

// Upgrade makes a deployed revision from the release's own chart with exactly the given values
// (the old ones are not reused, not even for empty values), supersedes the previous one, keeps ten
// revisions, and refuses another chart version.
func TestUpgradeThroughTheSDK(t *testing.T) {
	a, store := sdkFixture(t, common.StatusSuperseded, common.StatusDeployed)
	rev, err := a.Upgrade(context.Background(), "apps", "web", "1.0.0", 2, map[string]any{"greeting": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	rls := last(t, store)
	if rev != 3 || rls.Version != 3 || rls.Info.Status != common.StatusDeployed || rls.Config["greeting"] != "hi" || rls.Config["rev"] != nil {
		t.Errorf("after upgrade: returned %d, revision %d status %s config %v", rev, rls.Version, rls.Info.Status, rls.Config)
	}
	if !strings.Contains(rls.Manifest, `greeting: "hi"`) || rls.Chart.Metadata.Version != "1.0.0" {
		t.Errorf("manifest %q chart %v", rls.Manifest, rls.Chart.Metadata)
	}
	if prev, err := store.Get("web", 2); err != nil || prev.(*rspb.Release).Info.Status != common.StatusSuperseded {
		t.Errorf("revision 2 after the upgrade: %v", err)
	}
	if _, err := a.Upgrade(context.Background(), "apps", "web", "", 0, nil); err != nil {
		t.Fatal(err)
	}
	if rls := last(t, store); rls.Version != 4 || len(rls.Config) != 0 {
		t.Errorf("an upgrade with no values reused the old ones: %v", rls.Config)
	}
	if _, err := a.Upgrade(context.Background(), "apps", "web", "2.0.0", 0, nil); !errors.Is(err, ErrNoRepository) || !errors.Is(err, ErrUnchanged) {
		t.Errorf("another chart version: %v", err)
	}

	// A failure after the SDK made its revision leaves it failed, and is not ErrUnchanged.
	failing, store3 := sdkFixture(t, common.StatusDeployed)
	cfg, _ := failing.config("apps", DriverSecret, nil)
	cfg.KubeClient = &kubefake.FailingKubeClient{PrintingKubeClient: kubefake.PrintingKubeClient{Out: io.Discard}, UpdateError: errors.New("the server rejected it")}
	if _, err := failing.Upgrade(context.Background(), "apps", "web", "", 0, map[string]any{"greeting": "x"}); err == nil || errors.Is(err, ErrUnchanged) ||
		!strings.Contains(err.Error(), "rejected") {
		t.Errorf("failing update: %v", err)
	}
	if rls := last(t, store3); rls.Version != 2 || rls.Info.Status != common.StatusFailed {
		t.Errorf("after a failed update: revision %d status %s", rls.Version, rls.Info.Status)
	}

	many, store2 := sdkFixture(t, common.StatusSuperseded, common.StatusSuperseded, common.StatusSuperseded, common.StatusSuperseded,
		common.StatusSuperseded, common.StatusSuperseded, common.StatusSuperseded, common.StatusSuperseded, common.StatusSuperseded,
		common.StatusSuperseded, common.StatusDeployed)
	if _, err := many.Upgrade(context.Background(), "apps", "web", "", 0, map[string]any{"greeting": "x"}); err != nil {
		t.Fatal(err)
	}
	if h, _ := store2.History("web"); len(h) != MaxHistory {
		t.Errorf("%d revisions after an upgrade of 11, want %d", len(h), MaxHistory)
	}
}

// An upgrade refuses a release another action left pending (the SDK's own check); rolling it back
// to the revision before, which is how Cogpit finishes an interrupted upgrade, works.
func TestUpgradeRefusesAPendingRelease(t *testing.T) {
	a, store := sdkFixture(t, common.StatusDeployed, common.StatusPendingUpgrade)
	if _, err := a.Upgrade(context.Background(), "apps", "web", "", 0, map[string]any{"greeting": "x"}); err == nil ||
		!strings.Contains(err.Error(), "in progress") {
		t.Fatalf("upgrade of a pending release: %v", err)
	}
	if err := a.Rollback("apps", "web", 1, 0); err != nil {
		t.Fatalf("rolling back an interrupted upgrade: %v", err)
	}
	if rls := last(t, store); rls.Version != 3 || rls.Info.Status != common.StatusDeployed || rls.Config["rev"] != 1 {
		t.Errorf("after the rollback: revision %d status %s config %v", rls.Version, rls.Info.Status, rls.Config)
	}
}

// A value that reaches the SDK's error (a template's fail quoting it) is scrubbed from the error
// and from what the SDK logs; a panic's text is scrubbed too.
func TestUpgradeScrubsValuesFromErrors(t *testing.T) {
	a, store := sdkFixture(t, common.StatusDeployed)
	var logged bytes.Buffer
	cfg, _ := a.config("apps", DriverSecret, nil)
	a.config = func(_ string, _ Driver, l *slog.Logger) (*action.Configuration, error) {
		cfg.SetLogger(l.Handler())
		return cfg, nil
	}
	a.log = slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	_, err := a.Upgrade(context.Background(), "apps", "web", "", 0, map[string]any{"boom": "s3cr3t-token", "greeting": "fine"})
	if err == nil || strings.Contains(err.Error(), "s3cr3t-token") || !strings.Contains(err.Error(), "cannot use "+withheldValue) ||
		!errors.Is(err, ErrUnchanged) {
		t.Errorf("error %v", err)
	}
	if strings.Contains(logged.String(), "s3cr3t-token") {
		t.Errorf("the SDK logged a value: %s", logged.String())
	}
	if rls := last(t, store); rls.Version != 1 {
		t.Errorf("a failed render made revision %d", rls.Version)
	}
	panicky := &Actions{config: func(string, Driver, *slog.Logger) (*action.Configuration, error) { panic("boom with s3cr3t-token") }}
	if _, err := panicky.Upgrade(context.Background(), "apps", "web", "", 0, map[string]any{"x": "s3cr3t-token"}); err == nil ||
		strings.Contains(err.Error(), "s3cr3t-token") || !strings.Contains(err.Error(), "internal error") {
		t.Errorf("panic: %v", err)
	}
}

// A configuration that cannot be made, or an SDK that panics, is the action's error.
func TestActionsReportFailures(t *testing.T) {
	broken := &Actions{config: func(string, Driver, *slog.Logger) (*action.Configuration, error) { return nil, errors.New("no client") }}
	if err := broken.Rollback("apps", "web", 1, 0); err == nil || !strings.Contains(err.Error(), "no client") {
		t.Errorf("rollback: %v", err)
	}
	panicky := &Actions{config: func(string, Driver, *slog.Logger) (*action.Configuration, error) { return &action.Configuration{}, nil }}
	if err := panicky.Uninstall("apps", "web"); err == nil || !strings.Contains(err.Error(), "internal error") {
		t.Errorf("uninstall with an empty configuration: %v", err)
	}
	if _, err := panicky.Upgrade(context.Background(), "apps", "web", "", 0, nil); err == nil || !strings.Contains(err.Error(), "internal error") {
		t.Errorf("upgrade with an empty configuration: %v", err)
	}
	if err := NewActions(nil, nil).Uninstall("apps", "web"); err == nil {
		t.Error("an action without a connection succeeded")
	}
}
