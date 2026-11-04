package engine

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The Velero page (velero.go): Velero's rules as its controllers apply them (the next run, the
// skip while a backup runs), the last runs with missed ones, the scopes and the namespaces outside
// them, Needs attention's order, the header from the Deployment and the DaemonSet, and the page's
// worker over the cache. Every name is made up.

var veleroNow = time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)

// veleroAt builds the page from a reading at now for a user in zone (nil: UTC).
func veleroAt(objs map[string][]*unstructured.Unstructured, now time.Time, zone *time.Location, namespaces ...string) *VeleroDashboardModel {
	return buildVeleroModel(&Backend{Context: "test"}, veleroReading(objs), namespaces, now, model.TimeFormat{Zone: zone})
}

// veleroReading is a reading of the given objects by kind key, every kind listed; without storage
// locations it has the default one, available.
func veleroReading(objs map[string][]*unstructured.Unstructured) *veleroBuild {
	if _, ok := objs[VeleroLocationsKey]; !ok {
		objs[VeleroLocationsKey] = []*unstructured.Unstructured{testkit.VeleroObject("BackupStorageLocation", "default", veleroNow, nil,
			map[string]any{"default": true}, map[string]any{"phase": "Available"})}
	}
	vb := &veleroBuild{served: true, synced: true, objs: objs, read: map[string]bool{}, kindOK: map[string]bool{}, kindErr: map[string]string{}}
	for _, k := range veleroPageKinds {
		vb.read[k], vb.kindOK[k] = true, true
	}
	return vb
}

func veleroModelOf(t *testing.T, objs map[string][]*unstructured.Unstructured, namespaces ...string) *VeleroDashboardModel {
	t.Helper()
	return veleroAt(objs, veleroNow, nil, namespaces...)
}

// vSchedule is a schedule with its cron and template; status fields as given (lastBackup,
// lastSkipped, phase; phase Enabled by default).
func vSchedule(name, cron string, tmpl map[string]any, paused bool, status map[string]any) *unstructured.Unstructured {
	if tmpl == nil {
		tmpl = map[string]any{}
	}
	st := map[string]any{"phase": "Enabled"}
	for k, v := range status {
		st[k] = v
	}
	return testkit.VeleroObject("Schedule", name, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), nil,
		map[string]any{"schedule": cron, "paused": paused, "template": tmpl}, st)
}

// vBackup is a backup of a schedule ("" for a manual one) named by its run, in a phase, started at
// at and finished after took (0: not finished).
func vBackup(schedule, name, phase string, at time.Time, took time.Duration, spec, status map[string]any) *unstructured.Unstructured {
	var labels map[string]string
	if schedule != "" {
		labels = map[string]string{veleroScheduleLabel: schedule}
	}
	st := map[string]any{"phase": phase, "startTimestamp": testkit.VeleroTime(at)}
	if took > 0 {
		st["completionTimestamp"] = testkit.VeleroTime(at.Add(took))
	}
	for k, v := range status {
		st[k] = v
	}
	if spec == nil {
		spec = map[string]any{}
	}
	return testkit.VeleroObject("Backup", name, at, labels, spec, st)
}

// runName is a scheduled backup's name: <schedule>-YYYYMMDDHHMMSS.
func runName(schedule string, at time.Time) string {
	return schedule + "-" + at.UTC().Format("20060102150405")
}

func TestVeleroCronWords(t *testing.T) {
	for _, c := range []struct{ cron, want string }{
		{"0 2 * * *", "daily 02:00"},
		{"30 4 * * *", "daily 04:30"},
		{"0 */6 * * *", "every 6 h"},
		{"15 0/4 * * *", "every 4 h at :15"},
		{"5 * * * *", "hourly :05"},
		{"*/15 * * * *", "every 15 min"},
		{"0 3 * * 0", "Sun 03:00"},
		{"0 3 * * SUN", "Sun 03:00"},
		{"30 1 * * 1-5", "Mon–Fri 01:30"},
		{"0 2,14 * * *", "daily 02:00, 14:00"},
		{"0 4 1 * *", "monthly, day 1 04:00"},
		{"@daily", "daily 00:00"},
		{"@every 6h", "every 6h"},
		{"CRON_TZ=Europe/Berlin 0 2 * * *", "daily 02:00"},
		{"TZ=UTC 0 2 * * *", "daily 02:00"},
		{"0 2 1-7 * 1", "0 2 1-7 * 1"}, // no words for it: as written
	} {
		if got := parseVeleroCron(c.cron, time.UTC).words; got != c.want {
			t.Errorf("%q: %q, want %q", c.cron, got, c.want)
		}
	}
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("no time zone data")
	}
	// A cron in another zone than the user's is named after its times; in the user's, it is not.
	at := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	if c := parseVeleroCron("CRON_TZ=Europe/Berlin 0 2 * * *", time.UTC); c.zoneNote(nil, at) != " Europe/Berlin" || c.zoneNote(berlin, at) != "" {
		t.Errorf("prefix zone: %q, %q", c.zoneNote(nil, at), c.zoneNote(berlin, at))
	}
	// Velero's server in another zone (its container's TZ): its crons without a prefix run there.
	if c := parseVeleroCron("0 2 * * *", berlin); c.zoneNote(time.UTC, at) != " Europe/Berlin" || c.zoneNote(berlin, at) != "" {
		t.Errorf("server zone: %q", c.zoneNote(time.UTC, at))
	}
}

// TestVeleroOverdue: the controller's rule. A schedule is overdue once cron.Next(max(lastBackup or
// creation, lastSkipped)) is more than veleroGrace past; the backup that keeps it from running is
// named; a skip, a pause or a time within the grace is not overdue.
func TestVeleroOverdue(t *testing.T) {
	last := time.Date(2026, 11, 2, 2, 0, 12, 0, time.UTC)
	running := vBackup("nightly", runName("nightly", last), "InProgress", last, 0, nil, nil)
	sched := func(paused bool, status map[string]any) *unstructured.Unstructured {
		st := map[string]any{"lastBackup": testkit.VeleroTime(last)}
		for k, v := range status {
			st[k] = v
		}
		return vSchedule("nightly", "0 2 * * *", nil, paused, st)
	}
	objs := func() map[string][]*unstructured.Unstructured {
		return map[string][]*unstructured.Unstructured{VeleroSchedulesKey: {sched(false, nil)}, VeleroBackupsKey: {running}}
	}
	m := veleroAt(objs(), veleroNow, nil)
	due := time.Date(2026, 11, 3, 2, 0, 0, 0, time.UTC)
	if len(m.Schedules) != 1 || m.Schedules[0].State != lblOverdue || m.Schedules[0].Status != model.StatusError {
		t.Fatalf("schedules %+v", m.Schedules)
	}
	if next := m.Schedules[0].NextRun; !next.Equal(time.Date(2026, 11, 4, 2, 0, 0, 0, time.UTC)) {
		t.Errorf("next run %v: the next time cron names, the due one not created", next)
	}
	want := "due 02:00, no backup created; " + running.GetName() + " still InProgress"
	if len(m.Attention) != 1 || m.Attention[0].Problem != lblOverdue || !m.Attention[0].Since.Equal(due) || m.Attention[0].Detail != want {
		t.Errorf("attention %+v, want %q", m.Attention, want)
	}
	if tiles := tileText(m); !strings.Contains(tiles, "Schedules 1: Overdue 1") || !strings.Contains(tiles, "Backups 1: In progress 1") {
		t.Errorf("tiles %s", tiles)
	}
	// A user in another zone reads the cron's times with its zone named: Velero runs it in UTC.
	if berlin, err := time.LoadLocation("Europe/Berlin"); err == nil {
		m := veleroAt(objs(), veleroNow, berlin)
		if m.Schedules[0].When != "daily 02:00 UTC" || !strings.HasPrefix(m.Attention[0].Detail, "due 02:00 UTC, no backup created") {
			t.Errorf("in Berlin: %q, %q", m.Schedules[0].When, m.Attention[0].Detail)
		}
	}

	// A skip after the last backup moves the next run past now; a paused schedule is due nowhere.
	for name, objs := range map[string]*unstructured.Unstructured{
		"skipped": sched(false, map[string]any{"lastSkipped": testkit.VeleroTime(time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC))}),
		"paused":  sched(true, nil),
	} {
		m := veleroModelOf(t, map[string][]*unstructured.Unstructured{VeleroSchedulesKey: {objs}, VeleroBackupsKey: {running}})
		if m.Schedules[0].State == lblOverdue || len(m.Attention) != 0 {
			t.Errorf("%s: %+v, attention %+v", name, m.Schedules[0], m.Attention)
		}
	}
	if m := veleroModelOf(t, map[string][]*unstructured.Unstructured{VeleroSchedulesKey: {sched(true, nil)}}); m.Schedules[0].State != lblPaused ||
		!m.Schedules[0].NextRun.IsZero() {
		t.Errorf("paused %+v", m.Schedules[0])
	}
	// Within the grace the run is on its way.
	m = veleroAt(map[string][]*unstructured.Unstructured{VeleroSchedulesKey: {sched(false, nil)}}, due.Add(3*time.Minute), nil)
	if m.Schedules[0].State != lblEnabled {
		t.Errorf("within the grace: %+v", m.Schedules[0])
	}
}

// TestVeleroLastRuns: the last seven runs, oldest first: each backup's phase, the runs cron expected
// between two backups that the later one absorbed, and the runs due since the last one.
func TestVeleroLastRuns(t *testing.T) {
	at := func(h int) time.Time { return time.Date(2026, 11, 3, h, 5, 10, 0, time.UTC) }
	b := func(h int, phase string) *unstructured.Unstructured {
		return vBackup("hourly", runName("hourly", at(h)), phase, at(h), 2*time.Minute, nil, nil)
	}
	backups := []*unstructured.Unstructured{
		b(3, phaseCompleted), b(4, phaseCompleted), b(7, phaseFailed), b(8, "InProgress"),
	}
	sched := vSchedule("hourly", "5 * * * *", nil, false, map[string]any{"lastBackup": testkit.VeleroTime(at(8))})
	m := veleroAt(map[string][]*unstructured.Unstructured{VeleroSchedulesKey: {sched}, VeleroBackupsKey: backups},
		time.Date(2026, 11, 3, 11, 0, 0, 0, time.UTC), nil)
	var got []string
	for _, r := range m.Schedules[0].Runs {
		got = append(got, r.State+"@"+r.At.Format("15:04"))
	}
	// 05:05 and 06:05 came due and the 07:05 run absorbed them; 09:05 and 10:05 are due and not
	// created while the 08:05 backup runs. The 03:05 run is the eighth from the end: left out.
	want := []string{"completed@04:05", "missed@05:05", "missed@06:05", "failed@07:05", "running@08:05", "missed@09:05", "missed@10:05"}
	if !slices.Equal(got, want) {
		t.Errorf("runs %v, want %v", got, want)
	}
	if r := m.Schedules[0]; r.State != lblOverdue || r.When != "hourly :05" || r.Duration != "2m" || !r.LastCompleted.Equal(at(4).Add(2*time.Minute)) {
		t.Errorf("schedule %+v", r)
	}
	// The words of a cron without a time of day are the same in every zone: no zone named.
	if berlin, err := time.LoadLocation("Europe/Berlin"); err == nil {
		m := veleroAt(map[string][]*unstructured.Unstructured{VeleroSchedulesKey: {sched}}, veleroNow, berlin)
		if m.Schedules[0].When != "hourly :05" {
			t.Errorf("in Berlin: %q", m.Schedules[0].When)
		}
	}
	// Overdue and the latest finished backup failed: two rows, the newest first.
	if len(m.Attention) != 2 || m.Attention[0].Problem != lblOverdue || m.Attention[1].Problem != lblFailed {
		t.Errorf("attention %+v", m.Attention)
	}
}

func TestVeleroGlobs(t *testing.T) {
	for _, c := range []struct {
		pattern, s string
		want       bool
	}{
		{"kube-*", "kube-system", true}, {"kube-*", "kubernetes", false}, {"*", "anything", true},
		{"data?", "data1", true}, {"data?", "data12", false}, {"[a-c]*", "billing", true}, {"[a-c]*", "shop", false},
		{"[!a-c]*", "shop", true}, {"{shop,cart}", "cart", true}, {"{shop,cart}", "carts", false},
		{"team-*-prod", "team-a-prod", true}, {"[abc", "a", false}, {"shop", "shop", true},
	} {
		if got := globMatch(c.pattern, c.s); got != c.want {
			t.Errorf("%q ~ %q: %v", c.pattern, c.s, got)
		}
	}
	sc := veleroScopeOf(map[string]any{"includedNamespaces": []any{"*"}, "excludedNamespaces": []any{"kube-*", "velero"}})
	if !sc.covers("shop") || sc.covers("kube-system") || sc.covers("velero") || sc.text() != "all namespaces − 2" {
		t.Errorf("scope %+v %q", sc, sc.text())
	}
	sel := veleroScopeOf(map[string]any{"includedNamespaces": []any{"database"}, "labelSelector": map[string]any{
		"matchLabels": map[string]any{"app": "postgres"}}})
	if !sel.partial() || sel.text() != "database · app=postgres" {
		t.Errorf("selector scope %q", sel.text())
	}
}

// TestVeleroOutside: Outside every schedule from the enabled schedules' scopes: excluded, a glob, a
// selector or a resource filter that leaves out data (partly; one that leaves out only jobs does not), a paused or failing schedule (no cover), the Namespace
// labelled out of backups (its objects in scope: partly), GitOps tools from the labels, those with
// PVCs first.
func TestVeleroOutside(t *testing.T) {
	scheds := []*unstructured.Unstructured{
		vSchedule("nightly-all", "0 2 * * *", map[string]any{"excludedNamespaces": []any{
			"kube-*", "analytics", "data?", "monitoring", "legacy", "keycloak", "frontend", "volumes", "batch", "vault"}}, false, nil),
		vSchedule("no-jobs", "0 1 * * *", map[string]any{"includedNamespaces": []any{"batch"},
			"excludedResources": []any{"jobs.batch", "events"}}, false, nil),
		vSchedule("no-secrets", "0 1 * * *", map[string]any{"includedNamespaces": []any{"vault"},
			"excludedNamespaceScopedResources": []any{"Secrets"}}, false, nil),
		vSchedule("hourly-db", "5 * * * *", map[string]any{"includedNamespaces": []any{"data?"},
			"labelSelector": map[string]any{"matchLabels": map[string]any{"app": "postgres"}}}, false, nil),
		vSchedule("pvcs-only", "0 3 * * *", map[string]any{"includedNamespaces": []any{"volumes"},
			"includedResources": []any{"persistentvolumeclaims"}}, false, nil),
		vSchedule("monitoring", "0 4 * * *", map[string]any{"includedNamespaces": []any{"monitoring"}}, true, nil),
		vSchedule("broken", "not a cron", map[string]any{"includedNamespaces": []any{"legacy"}}, false,
			map[string]any{"phase": "FailedValidation", "validationErrors": []any{"invalid schedule: expected 5 fields"}}),
	}
	ns := func(name string, labels map[string]string, terminating bool) *unstructured.Unstructured {
		o := testkit.Namespace(name)
		o.SetLabels(labels)
		if terminating {
			o.Object["status"] = map[string]any{"phase": "Terminating"}
		}
		return o
	}
	var namespaces []*unstructured.Unstructured
	for _, n := range []string{"shop", "analytics", "data1", "kube-system", "monitoring", "legacy", "keycloak", "frontend", "volumes", "batch", "vault"} {
		namespaces = append(namespaces, ns(n, nil, false))
	}
	namespaces = append(namespaces, ns("labelled", map[string]string{veleroExcludeLabel: "true"}, false), ns("going", nil, true))
	pvc := func(namespace, name string, labels map[string]string) *unstructured.Unstructured {
		o := testkit.Object("v1", "PersistentVolumeClaim", namespace, name, nil)
		o.SetLabels(labels)
		return o
	}
	deploy := func(namespace, name string, annotations map[string]string) *unstructured.Unstructured {
		o := testkit.Object("apps/v1", "Deployment", namespace, name, nil)
		o.SetAnnotations(annotations)
		return o
	}
	argo := map[string]string{argoTrackingAnno: "frontend:apps/Deployment:frontend/web"}
	m := veleroModelOf(t, map[string][]*unstructured.Unstructured{
		VeleroSchedulesKey: scheds,
		kindNamespaces:     namespaces,
		kindPVCs: {pvc("analytics", "a", nil), pvc("analytics", "b", nil), pvc("data1", "pg", nil), pvc("keycloak", "db", map[string]string{fluxKustomizeLabel: "keycloak"}),
			pvc("labelled", "x", nil)},
		logKindDeployments: {deploy("frontend", "web", argo), deploy("frontend", "api", argo), deploy("analytics", "spark", nil)},
	})
	var got []string
	for _, r := range m.Outside {
		got = append(got, r.Ref.Name+"|"+r.ManagedBy+"|"+r.Reason)
	}
	want := []string{
		"analytics||excluded from nightly-all, no other schedule names it",
		"keycloak|Flux CD|excluded from nightly-all, no other schedule names it · Git holds no volume data",
		"data1||partly in scope: hourly-db selects app=postgres only",
		"labelled||partly in scope: its Namespace object carries velero.io/exclude-from-backup; the rest is in scope of nightly-all",
		"kube-system||excluded from nightly-all, no other schedule names it",
		"legacy||in scope of broken only, which fails validation",
		"monitoring||in scope of monitoring only, which is paused",
		"frontend|Argo CD|excluded from nightly-all, no other schedule names it · no volumes; rebuilt from Git",
		"vault||partly in scope: no-secrets leaves out Secrets",
		"volumes||partly in scope: pvcs-only takes only persistentvolumeclaims",
	}
	if !slices.Equal(got, want) {
		t.Errorf("outside\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if r := m.Outside[0]; r.PVCs != 2 || r.Workloads != 1 || r.Partly {
		t.Errorf("analytics %+v", r)
	}
	// The namespace filter narrows the rows to its namespaces, and the schedules to those whose
	// scope takes one of them in.
	m = veleroModelOf(t, map[string][]*unstructured.Unstructured{VeleroSchedulesKey: scheds, kindNamespaces: namespaces}, "data1")
	var names []string
	for _, s := range m.Schedules {
		names = append(names, s.Ref.Name)
	}
	if len(m.Outside) != 1 || m.Outside[0].Ref.Name != "data1" || !slices.Equal(names, []string{"hourly-db"}) {
		t.Errorf("narrowed: outside %+v, schedules %v", m.Outside, names)
	}
}

// TestVeleroAttentionOrder: errors before warnings, each newest first, a row without a time last;
// a manual backup that failed and was done again since is gone, one that failed long ago too.
func TestVeleroAttentionOrder(t *testing.T) {
	ago := func(d time.Duration) time.Time { return veleroNow.Add(-d) }
	bsl := func(name, phase string, synced time.Time) *unstructured.Unstructured {
		return testkit.VeleroObject("BackupStorageLocation", name, ago(30*24*time.Hour), nil, map[string]any{"default": name == "default"},
			map[string]any{"phase": phase, "lastSyncedTime": testkit.VeleroTime(synced), "message": "rpc error: connection refused"})
	}
	spec := map[string]any{"includedNamespaces": []any{"shop"}}
	repo := testkit.VeleroObject("BackupRepository", "shop-default-kopia", ago(10*24*time.Hour), nil,
		map[string]any{"volumeNamespace": "shop"}, map[string]any{"phase": "Ready", "recentMaintenance": []any{
			map[string]any{"result": "Succeeded", "completeTimestamp": testkit.VeleroTime(ago(72 * time.Hour))},
			map[string]any{"result": "Failed", "completeTimestamp": testkit.VeleroTime(ago(48 * time.Hour)), "message": "unable to acquire lock"},
		}})
	m := veleroModelOf(t, map[string][]*unstructured.Unstructured{
		VeleroLocationsKey:    {bsl("default", "Available", ago(time.Minute)), bsl("secondary", "Unavailable", ago(3*time.Hour))},
		VeleroRepositoriesKey: {repo},
		VeleroBackupsKey: {
			vBackup("", "manual-old", phaseFailed, ago(10*24*time.Hour), time.Minute, spec, nil),     // past the week
			vBackup("", "manual-redone", phaseFailed, ago(5*time.Hour), time.Minute, spec, nil),      // done again after
			vBackup("", "manual-redone-2", phaseCompleted, ago(4*time.Hour), time.Minute, spec, nil), // the redo
			vBackup("", "manual-partial", phasePartiallyFailed, ago(time.Hour), time.Minute, map[string]any{}, // another scope
				map[string]any{"errors": int64(4), "warnings": int64(12), "volumeSnapshotsAttempted": int64(3), "volumeSnapshotsCompleted": int64(2)}),
		},
	})
	var got []string
	for _, a := range m.Attention {
		got = append(got, a.Problem+"|"+a.Object+"|"+a.Detail)
	}
	want := []string{
		"Location unavailable|BackupStorageLocation secondary|rpc error: connection refused",
		"Partially failed|Backup manual-partial|4 errors, 12 warnings · 2 of 3 volume snapshots",
		"Maintenance failed|BackupRepository shop-default-kopia|unable to acquire lock",
	}
	if !slices.Equal(got, want) {
		t.Errorf("attention\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !m.Attention[0].Since.Equal(ago(3 * time.Hour)) {
		t.Errorf("unavailable since %v: its last sync", m.Attention[0].Since)
	}
	// No location is the default and none is named as the server's default: a schedule that names
	// none fails its next backup.
	m = veleroModelOf(t, map[string][]*unstructured.Unstructured{
		VeleroLocationsKey: {bsl("primary", "Available", ago(time.Minute))},
		VeleroSchedulesKey: {vSchedule("nightly", "0 2 * * *", nil, false, map[string]any{"lastBackup": testkit.VeleroTime(ago(7 * time.Hour))})},
	})
	if len(m.Attention) != 1 || m.Attention[0].Problem != "No default location" || m.Attention[0].Status != model.StatusError ||
		m.Attention[0].Ref.Name != "" || !strings.HasSuffix(m.Attention[0].Detail, "naming none: nightly") {
		t.Errorf("no default %+v", m.Attention)
	}
}

// TestVeleroHeader: the version and plugins from Velero's Deployment, the server's and node agents'
// readiness, the server's default TTL from its arguments; node agents not ready need a check.
func TestVeleroHeader(t *testing.T) {
	dep := testkit.Object("apps/v1", "Deployment", "velero", "velero", map[string]any{
		"spec": map[string]any{"replicas": int64(1), "template": map[string]any{"spec": map[string]any{
			"initContainers": []any{
				map[string]any{"name": "velero-plugin-for-aws", "image": "velero/velero-plugin-for-aws:v1.13.0"},
				map[string]any{"name": "csi", "image": "registry.example.com/velero/velero-plugin-for-csi:v0.7.1"},
			},
			"containers": []any{map[string]any{"name": "velero", "image": "velero/velero:v1.17.1",
				"args": []any{"server", "--default-backup-ttl=168h", "--uploader-type=kopia"}}},
		}}},
		"status": map[string]any{"readyReplicas": int64(1)},
	})
	other := testkit.Object("apps/v1", "Deployment", "velero", "minio", map[string]any{"spec": map[string]any{"template": map[string]any{
		"spec": map[string]any{"containers": []any{map[string]any{"name": "minio", "image": "minio/minio:latest"}}}}}})
	ds := testkit.Object("apps/v1", "DaemonSet", "velero", "node-agent", map[string]any{
		"status": map[string]any{"numberReady": int64(5), "desiredNumberScheduled": int64(6)}})
	bsl := testkit.VeleroObject("BackupStorageLocation", "default", veleroNow, nil, map[string]any{"default": true},
		map[string]any{"phase": "Available"})
	m := veleroModelOf(t, map[string][]*unstructured.Unstructured{
		VeleroLocationsKey: {bsl}, logKindDeployments: {other, dep}, logKindDaemonSets: {ds},
		VeleroSchedulesKey: {vSchedule("weekly", "0 3 * * 0", nil, false, map[string]any{"lastBackup": testkit.VeleroTime(veleroNow.Add(-time.Hour))})},
	})
	h := m.Header
	if h.Version != "1.17.1" || h.Namespace != "velero" || h.Server == nil || h.Server.Ready != 1 || h.Server.Desired != 1 ||
		h.NodeAgents == nil || h.NodeAgents.Ready != 5 || h.NodeAgents.Desired != 6 || h.Server.Ref.Kind != logKindDeployments {
		t.Fatalf("header %+v", h)
	}
	if len(h.Plugins) != 2 || h.Plugins[0] != (VeleroPlugin{Name: "aws", Version: "1.13.0"}) || h.Plugins[1] != (VeleroPlugin{Name: "csi", Version: "0.7.1"}) {
		t.Errorf("plugins %+v", h.Plugins)
	}
	if m.Schedules[0].TTL != "7d" || m.Schedules[0].Location != "default" {
		t.Errorf("schedule %+v: the server's default TTL and the default location", m.Schedules[0])
	}
	if len(m.Attention) != 1 || m.Attention[0].Problem != "Node agents not ready" || m.Attention[0].Status != model.StatusWarning ||
		m.Attention[0].Detail != "not ready on 1 of 6 nodes" || m.Attention[0].Ref.Kind != logKindDaemonSets {
		t.Errorf("attention %+v", m.Attention)
	}
	if tiles := tileText(m); !strings.Contains(tiles, "Storage 1: Location available 1") {
		t.Errorf("tiles %s", tiles)
	}
}

// tileText writes the tiles as "Title count: Label n, …; …".
func tileText(m *VeleroDashboardModel) string {
	parts := make([]string, 0, len(m.Tiles))
	for _, t := range m.Tiles {
		lines := make([]string, 0, len(t.Lines))
		for _, l := range t.Lines {
			lines = append(lines, l.Label+" "+strconv.Itoa(l.Count))
		}
		parts = append(parts, t.Title+" "+strconv.Itoa(t.Count)+": "+strings.Join(lines, ", "))
	}
	return strings.Join(parts, "; ")
}

// veleroCoreProfiles are the built-in profiles the page reads that the test sessions lack.
func veleroCoreProfiles() []*model.KindProfile {
	var out []*model.KindProfile
	for _, p := range kinds.Builtin() {
		if p.Key == kindPVCs || p.Key == kindStatefulSets || p.Key == logKindDaemonSets {
			out = append(out, p)
		}
	}
	return out
}

// TestVeleroPage: the page's worker holds its kinds while the page is open and follows them, the
// namespace filter and the page's leaving; without Velero the page says so.
func TestVeleroPage(t *testing.T) {
	if title, ok := PageTitle(VeleroDashboard); !ok || title != "Velero" || !IsPage(VeleroDashboard) || !MixedKinds(VeleroDashboard) {
		t.Fatalf("page %q %v", title, ok)
	}
	src := testkit.NewMemorySource()
	src.Set(testkit.NamespacesGVR, testkit.Namespace("shop"))
	src.Set(testkit.NamespacesGVR, testkit.Namespace("velero"))
	for _, crd := range testkit.VeleroCRDs() {
		src.Set(crdsGVR, crd)
	}
	now := time.Now()
	src.Set(testkit.VeleroLocationsGVR, testkit.VeleroObject("BackupStorageLocation", "default", now, nil, map[string]any{"default": true},
		map[string]any{"phase": "Available"}))
	src.Set(testkit.VeleroSchedulesGVR, testkit.VeleroObject("Schedule", "shop", now.Add(-time.Hour), nil,
		map[string]any{"schedule": "0 2 * * *", "template": map[string]any{"includedNamespaces": []any{"shop"}}},
		map[string]any{"phase": "Enabled", "lastBackup": testkit.VeleroTime(now.Add(-time.Hour))}))
	src.Set(testkit.VeleroBackupsGVR, vBackup("shop", runName("shop", now.Add(-time.Hour)), phaseFailed, now.Add(-time.Hour), time.Minute,
		map[string]any{"includedNamespaces": []any{"shop"}}, map[string]any{"failureReason": "found a backup with status InProgress during the server starting"}))
	s := newCRDSession(t, src, veleroCoreProfiles()...)
	waitFor(t, "Velero's kinds", func() bool { return hasKind(s, VeleroBackupsKey) && hasKind(s, VeleroSchedulesKey) })

	// Read once, the page not open: from the cache.
	m, err := s.VeleroDashboard(context.Background(), "", nil)
	if err != nil || !m.Served || !m.Synced {
		t.Fatalf("model %+v %v", m, err)
	}
	if len(m.Attention) != 1 || m.Attention[0].Problem != lblFailed || !strings.Contains(m.Attention[0].Detail, "found a backup") {
		t.Errorf("attention %+v", m.Attention)
	}
	if len(m.Outside) != 1 || m.Outside[0].Ref.Name != "velero" {
		t.Errorf("outside %+v", m.Outside)
	}

	s.OpenTab(VeleroDashboard, false)
	waitTable(t, s, VeleroDashboard, func(t *TableSnapshot) bool { return t.Synced && t.Title == "Velero" && t.Error == "" })
	// The storage location goes away: the page follows.
	src.Set(testkit.VeleroLocationsGVR, testkit.VeleroObject("BackupStorageLocation", "default", now, nil, map[string]any{"default": true},
		map[string]any{"phase": "Unavailable", "message": "connection refused"}))
	waitFor(t, "the unavailable location", func() bool {
		m, err := s.VeleroDashboard(context.Background(), "", nil)
		return err == nil && len(m.Attention) == 2 && m.Attention[0].Problem == "Location unavailable"
	})
	// The namespace filter narrows the page.
	s.UpdateUI(func(u *UIState) { u.Namespaces = []string{"velero"} })
	waitFor(t, "the narrowed page", func() bool {
		m, _ := s.VeleroDashboard(context.Background(), "", nil)
		return m != nil && slices.Equal(m.Namespaces, []string{"velero"}) && len(m.Schedules) == 0 && len(m.Attention) == 1
	})
	// Leaving the page releases its handles.
	s.OpenTab("pods", false)
	waitFor(t, "the worker to stop", func() bool {
		s.state.mu.Lock()
		defer s.state.mu.Unlock()
		return s.state.velero == nil
	})
}

// TestVeleroPageWithoutVelero: a cluster without Velero's kinds: not served, and the page says so.
func TestVeleroPageWithoutVelero(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.PodsGVR, testkit.Pod("default", "web-0"))
	s := newCRDSession(t, src)
	waitFor(t, "the definitions to be listed", func() bool { set := s.Backend().customSet(); return set != nil && set.synced })
	m, err := s.VeleroDashboard(context.Background(), "", nil)
	if err != nil || m.Served || len(m.Tiles) != 0 {
		t.Fatalf("%+v %v", m, err)
	}
	s.OpenTab(VeleroDashboard, false)
	waitTable(t, s, VeleroDashboard, func(t *TableSnapshot) bool { return t.Error == veleroNotServed })
}

// vPVB is a pod volume backup of a backup, of a pod's volume on a node, in a phase with a message.
func vPVB(name, backup, phase, ns, pod, volume, node, msg string) *unstructured.Unstructured {
	return testkit.VeleroObject("PodVolumeBackup", name, veleroNow, map[string]string{veleroBackupLabel: backup},
		map[string]any{"pod": map[string]any{"kind": "Pod", "namespace": ns, "name": pod}, "volume": volume, "node": node},
		map[string]any{"phase": phase, "message": msg})
}

// exposeError is the message of a pod volume backup whose pod's volume was gone when it ran.
func exposeError(pod, volume, uid string) string {
	return "error to expose PVB: error to get pod volume path: error identifying unique volume path on host for volume " + volume +
		" in pod " + pod + ": expected one matching path: /host_pods/" + uid + "/volumes/*/" + volume + ", got 0"
}

func TestVeleroMaskVolumeMessage(t *testing.T) {
	a := maskVolumeMessage(exposeError("export-1", "scratch", "1111"), "shop", "export-1", "scratch")
	b := maskVolumeMessage(exposeError("export-2", "tmp", "2222"), "shop", "export-2", "tmp")
	if a != b || strings.Contains(a, "export") || strings.Contains(a, "/host_pods") {
		t.Errorf("masked\n%s\n%s", a, b)
	}
	if got := maskVolumeMessage("pod shop/web-0 (data) failed", "shop", "web-0", "data"); got != "pod <pod> <volume> failed" {
		t.Errorf("masked %q", got)
	}
	// Another reason stays apart.
	if c := maskVolumeMessage("exposed PVB is not ready: no expose result is available for the current node", "shop", "db-0", "data"); c == a {
		t.Error("two reasons masked alike")
	}
}

// TestVeleroFailedVolumes: the failed volume backups of the backups in Needs attention, grouped by
// namespace and masked message, most volumes first; the backup's Detail counts them and the errors
// they do not explain; a data upload counts as one; a completed one, another backup's, or one
// outside the namespace filter does not.
func TestVeleroFailedVolumes(t *testing.T) {
	at := time.Date(2026, 11, 3, 2, 0, 0, 0, time.UTC)
	latest := runName("nightly", at)
	older := runName("nightly", at.Add(-24*time.Hour))
	objs := func() map[string][]*unstructured.Unstructured {
		return map[string][]*unstructured.Unstructured{
			VeleroSchedulesKey: {vSchedule("nightly", "0 2 * * *", nil, false, map[string]any{"lastBackup": testkit.VeleroTime(at)})},
			VeleroBackupsKey: {
				vBackup("nightly", older, phaseCompleted, at.Add(-24*time.Hour), time.Hour, nil, nil),
				vBackup("nightly", latest, phasePartiallyFailed, at, time.Hour, nil, map[string]any{"errors": int64(49), "warnings": int64(2)}),
			},
			VeleroPodVolumeBackupsKey: {
				vPVB("p1", latest, phaseFailed, "shop", "export-b", "scratch", "node-2", exposeError("export-b", "scratch", "uid-b")),
				vPVB("p2", latest, phaseFailed, "shop", "export-a", "scratch", "node-1", exposeError("export-a", "scratch", "uid-a")),
				vPVB("p3", latest, phaseFailed, "shop", "warmup-c", "tmp", "node-1", exposeError("warmup-c", "tmp", "uid-c")),
				vPVB("p4", latest, phaseFailed, "shop", "orders-db-0", "data", "node-3",
					"exposed PVB is not ready: no expose result is available for the current node"),
				vPVB("p5", latest, phaseCompleted, "shop", "cart-db-0", "data", "node-1", ""),
				vPVB("p6", older, phaseFailed, "shop", "export-z", "scratch", "node-1", exposeError("export-z", "scratch", "uid-z")),
			},
			VeleroDataUploadsKey: {testkit.VeleroObject("DataUpload", "du1", veleroNow, map[string]string{veleroBackupLabel: latest},
				map[string]any{"sourceNamespace": "billing", "sourcePVC": "ledger"},
				map[string]any{"phase": phaseFailed, "node": "node-4", "message": "data path failed: snapshot not ready"})},
		}
	}
	m := veleroAt(objs(), veleroNow, nil)
	if len(m.Attention) != 1 || m.Attention[0].Detail != "49 errors, 2 warnings · 5 volume backups failed · 44 counted only" {
		t.Fatalf("attention %+v", m.Attention)
	}
	var got []string
	for _, g := range m.FailedVolumes {
		got = append(got, g.Namespace+"|"+strconv.Itoa(g.Volumes)+"|"+strconv.Itoa(g.Pods)+"|"+strings.Join(g.Nodes, ",")+"|"+g.Ref.Name)
	}
	want := []string{"shop|3|3|node-1,node-2|p2", "billing|1|0|node-4|du1", "shop|1|1|node-3|p4"}
	if !slices.Equal(got, want) {
		t.Errorf("groups %v, want %v", got, want)
	}
	g := m.FailedVolumes[0]
	if g.Message != exposeError("export-a", "scratch", "uid-a") || g.Ref.Kind != VeleroPodVolumeBackupsKey || len(g.Members) != 3 ||
		g.Members[0].Pod != "export-a" || g.Members[2].Pod != "warmup-c" || g.Members[2].Volume != "tmp" || g.Members[0].Backup != latest {
		t.Errorf("first group %+v", g)
	}
	if du := m.FailedVolumes[1].Members[0]; du.Ref.Kind != VeleroDataUploadsKey || du.Volume != "ledger" || du.Pod != "" || du.Node != "node-4" {
		t.Errorf("data upload %+v", du)
	}
	// The namespace filter narrows the groups; the Detail still counts the backup's own.
	m = veleroAt(objs(), veleroNow, nil, "billing")
	if len(m.FailedVolumes) != 1 || m.FailedVolumes[0].Namespace != "billing" {
		t.Errorf("narrowed %+v", m.FailedVolumes)
	}
	// As many errors as failed volume backups: nothing counted only.
	o := objs()
	o[VeleroBackupsKey][1] = vBackup("nightly", latest, phasePartiallyFailed, at, time.Hour, nil, map[string]any{"errors": int64(5)})
	if m := veleroAt(o, veleroNow, nil); m.Attention[0].Detail != "5 errors · 5 volume backups failed" {
		t.Errorf("detail %q", m.Attention[0].Detail)
	}
	// None failed: no groups, the Detail as before.
	o = objs()
	delete(o, VeleroPodVolumeBackupsKey)
	delete(o, VeleroDataUploadsKey)
	if m := veleroAt(o, veleroNow, nil); len(m.FailedVolumes) != 0 || m.Attention[0].Detail != "49 errors, 2 warnings" {
		t.Errorf("without failures: %+v, %q", m.FailedVolumes, m.Attention[0].Detail)
	}
}
