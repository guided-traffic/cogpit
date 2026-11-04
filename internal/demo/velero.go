//nolint:goconst // Object builders: Velero's field names and values read best as literals, as in a manifest.
package demo

import (
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The demo cluster's Velero (the Velero page, D-new): Velero's definitions, its server and node
// agents in the namespace velero, two storage locations (one unavailable), a snapshot location,
// five schedules with two weeks of their backups (nightly-all overdue while its last backup still
// runs, shop-volumes' latest partially failed, monitoring-config paused, hourly-db hourly), a
// manual backup, three restores, the file-system backup repositories (one whose maintenance failed)
// and PVCs, some in namespaces outside every schedule. All synthetic. The objects are static, the
// churn never changes them, and nothing in them comes from the random generator: the rest of the
// cluster stays as it was. Their times are relative to Now, as every other object's.

// veleroNS is the demo's Velero namespace, and veleroBackupKind the kind of its backups.
const (
	veleroNS         = "velero"
	veleroBackupKind = "Backup"
)

// veleroCRDs are the definitions of Velero's kinds the page reads.
var veleroCRDs = []struct{ plural, kind string }{
	{"schedules", "Schedule"}, {"backups", veleroBackupKind}, {"restores", "Restore"},
	{"backupstoragelocations", "BackupStorageLocation"}, {"volumesnapshotlocations", "VolumeSnapshotLocation"},
	{"backuprepositories", "BackupRepository"}, {"podvolumebackups", "PodVolumeBackup"},
}

// veleroSchedule is one of the demo's schedules: its cron, the template's scope, how its runs
// went (each run's phase by its distance from the newest: 0 the newest) and how long each takes.
type veleroSchedule struct {
	name, cron string
	every      time.Duration // the run's period, for the backups' times
	anchor     func(now time.Time) time.Time
	runs       int // backups kept
	took       time.Duration
	ttl        string
	paused     bool
	template   map[string]any
	phase      func(i int) string // the run's phase, i runs before the newest
	outcome    func(i int) map[string]any
}

// The runs' margins before now: a schedule's last run lies at least runMargin back, less than the
// page's grace for a run on its way, so that the next one is never seen overdue; nightly-all's due
// run lies dueMargin back, more than the grace, so that it always is.
const (
	runMargin = time.Minute
	dueMargin = 10 * time.Minute
)

// lastAt is the newest time at hour:minute UTC (each day, in Velero's zone) that lies more than
// margin before now.
func lastAt(now time.Time, hour, minute int, margin time.Duration) time.Time {
	now = now.UTC()
	t := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.UTC)
	for !t.Before(now.Add(-margin)) {
		t = t.Add(-24 * time.Hour)
	}
	return t
}

// outsideNamespaces are the namespaces the demo's two schedules of every namespace leave out:
// demo-ml and demo-media keep their PVCs outside every schedule, demo-data is only in hourly-db's,
// by a selector.
var outsideNamespaces = []any{"kube-system", "demo-ml", "demo-media", "demo-data"}

// demoSchedules are the demo's schedules.
func demoSchedules() []veleroSchedule {
	completed := func(int) string { return "Completed" }
	return []veleroSchedule{
		{
			name: "nightly-all", cron: "0 2 * * *", every: 24 * time.Hour, runs: 14, took: 58 * time.Minute, ttl: "720h0m0s",
			// The run due last night was not created: the one before still runs (Velero skips a due run
			// while a backup of the schedule is in progress).
			anchor:   func(now time.Time) time.Time { return lastAt(now, 2, 0, dueMargin).Add(-24 * time.Hour) },
			template: map[string]any{"excludedNamespaces": outsideNamespaces, "snapshotVolumes": true, "storageLocation": "default"},
			phase: func(i int) string {
				switch i {
				case 0:
					return "InProgress"
				case 3:
					return "PartiallyFailed"
				}
				return "Completed"
			},
			outcome: func(i int) map[string]any {
				if i == 3 {
					return map[string]any{"errors": int64(1), "warnings": int64(3)}
				}
				return nil
			},
		},
		{
			name: "shop-volumes", cron: "0 */6 * * *", every: 6 * time.Hour, runs: 28, took: 14 * time.Minute, ttl: "168h0m0s",
			anchor: func(now time.Time) time.Time {
				t := lastAt(now, 0, 0, runMargin)
				for t.Add(6 * time.Hour).Before(now.Add(-runMargin)) {
					t = t.Add(6 * time.Hour)
				}
				return t
			},
			template: map[string]any{"includedNamespaces": []any{"demo-shop"}, "defaultVolumesToFsBackup": true, "storageLocation": "default"},
			phase: func(i int) string {
				if i == 0 {
					return "PartiallyFailed"
				}
				return "Completed"
			},
			outcome: func(i int) map[string]any {
				if i == 0 {
					return map[string]any{"errors": int64(4), "warnings": int64(12), "volumeSnapshotsAttempted": int64(3),
						"volumeSnapshotsCompleted": int64(2)}
				}
				return nil
			},
		},
		{
			name: "hourly-db", cron: "5 * * * *", every: time.Hour, runs: 72, took: 3 * time.Minute, ttl: "72h0m0s",
			anchor: func(now time.Time) time.Time {
				now = now.UTC()
				t := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 5, 0, 0, time.UTC)
				for !t.Before(now.Add(-runMargin)) {
					t = t.Add(-time.Hour)
				}
				return t
			},
			template: map[string]any{"includedNamespaces": []any{"demo-data"},
				"labelSelector": map[string]any{"matchLabels": map[string]any{"app": "postgres"}}, "storageLocation": "default"},
			phase: completed,
		},
		{
			name: "weekly-full", cron: "0 3 * * 0", every: 7 * 24 * time.Hour, runs: 2, took: 107 * time.Minute, ttl: "2160h0m0s",
			anchor: func(now time.Time) time.Time {
				t := lastAt(now, 3, 0, runMargin)
				for t.Weekday() != time.Sunday {
					t = t.Add(-24 * time.Hour)
				}
				return t
			},
			template: map[string]any{"excludedNamespaces": outsideNamespaces, "storageLocation": "secondary"},
			phase:    completed,
		},
		{
			name: "monitoring-config", cron: "30 4 * * *", every: 24 * time.Hour, runs: 3, took: 2 * time.Minute, ttl: "336h0m0s", paused: true,
			anchor:   func(now time.Time) time.Time { return lastAt(now, 4, 30, runMargin).Add(-9 * 24 * time.Hour) },
			template: map[string]any{"includedNamespaces": []any{"demo-monitoring"}, "storageLocation": "default"},
			phase:    completed,
		},
	}
}

// buildVelero adds Velero's definitions and objects. Called with c.mu held.
func (c *Cluster) buildVelero(now time.Time) {
	for _, d := range veleroCRDs {
		c.put(kindCRDs, veleroCRD(d.plural, d.kind, now))
	}
	c.put(kindNamespaces, object("v1", "Namespace", map[string]any{
		"name": veleroNS, "uid": stableUID("namespace", veleroNS), "creationTimestamp": ts(now.Add(-60 * 24 * time.Hour)),
		"labels": map[string]any{"kubernetes.io/metadata.name": veleroNS},
	}, map[string]any{"spec": map[string]any{"finalizers": []any{"kubernetes"}}, "status": map[string]any{"phase": "Active"}}))
	c.put(kindDeployments, veleroServer(now))
	c.put(kindDaemonSets, veleroNodeAgent(now, c.opts.Nodes))
	c.buildVeleroStorage(now)
	var shopBackup string
	for _, s := range demoSchedules() {
		newest := s.anchor(now)
		c.put(kindVeleroSchedules, veleroScheduleObject(s, newest, now))
		for i := s.runs - 1; i >= 0; i-- {
			at := newest.Add(-time.Duration(i) * s.every)
			name := s.name + "-" + at.Format("20060102150405")
			if s.name == "shop-volumes" && i == 1 {
				shopBackup = name
			}
			if s.name == "shop-volumes" && i == 0 {
				c.buildVeleroPodVolumes(name, at)
			}
			var outcome map[string]any
			if s.outcome != nil {
				outcome = s.outcome(i)
			}
			c.put(kindVeleroBackups, veleroBackupObject(name, s.name, s.template, s.ttl, s.phase(i), at, s.took, outcome))
		}
	}
	c.put(kindVeleroBackups, veleroBackupObject("pre-upgrade-demo-auth", "", map[string]any{"includedNamespaces": []any{"demo-auth"},
		"storageLocation": "default"}, "720h0m0s", "Completed", now.Add(-5*24*time.Hour), 6*time.Minute, nil))
	for i, r := range []struct{ name, phase string }{
		{"restore-shop-cart", "Completed"}, {"restore-auth-users", "Completed"}, {"restore-shop-orders", "PartiallyFailed"},
	} {
		at := now.Add(-time.Duration(5-i) * 24 * time.Hour)
		c.put(kindVeleroRestores, veleroObject("Restore", r.name, at, nil, map[string]any{
			"backupName": shopBackup, "includedNamespaces": []any{"demo-shop"},
		}, map[string]any{"phase": r.phase, "startTimestamp": ts(at), "completionTimestamp": ts(at.Add(4 * time.Minute))}))
	}
	c.buildVeleroVolumes(now)
}

// buildVeleroStorage adds the storage and snapshot locations and the repositories.
func (c *Cluster) buildVeleroStorage(now time.Time) {
	c.put(kindVeleroLocations, veleroObject("BackupStorageLocation", "default", now.Add(-60*24*time.Hour), nil, map[string]any{
		"provider": "aws", "default": true, "accessMode": "ReadWrite",
		"objectStorage": map[string]any{"bucket": "demo-backups", "prefix": "cluster"},
		"config":        map[string]any{"region": "demo-1", "s3Url": "https://s3.example.com"},
	}, map[string]any{"phase": "Available", "lastValidationTime": ts(now.Add(-40 * time.Second)), "lastSyncedTime": ts(now.Add(-50 * time.Second))}))
	c.put(kindVeleroLocations, veleroObject("BackupStorageLocation", "secondary", now.Add(-40*24*time.Hour), nil, map[string]any{
		"provider": "aws", "accessMode": "ReadWrite",
		"objectStorage": map[string]any{"bucket": "demo-backups-offsite"},
		"config":        map[string]any{"region": "demo-2", "s3Url": "https://192.0.2.12:9000"},
	}, map[string]any{"phase": "Unavailable", "lastValidationTime": ts(now.Add(-30 * time.Second)), "lastSyncedTime": ts(now.Add(-3 * time.Hour)),
		"message": "rpc error: code = Unknown desc = dial tcp 192.0.2.12:9000: connect: connection refused"}))
	c.put(kindVeleroSnapshotLocations, veleroObject("VolumeSnapshotLocation", "default", now.Add(-60*24*time.Hour), nil,
		map[string]any{"provider": "aws", "config": map[string]any{"region": "demo-1"}}, map[string]any{"phase": "Available"}))
	for _, ns := range []string{"demo-shop", "demo-auth", "demo-search", "demo-identity", "demo-web", "demo-cache", "demo-queue", "demo-billing"} {
		status := map[string]any{"phase": "Ready", "lastMaintenanceTime": ts(now.Add(-26 * time.Hour)), "recentMaintenance": []any{
			map[string]any{"result": "Succeeded", "startTimestamp": ts(now.Add(-26 * time.Hour)), "completeTimestamp": ts(now.Add(-26*time.Hour + 3*time.Minute))},
		}}
		if ns == "demo-shop" {
			status["recentMaintenance"] = []any{
				map[string]any{"result": "Succeeded", "startTimestamp": ts(now.Add(-74 * time.Hour)), "completeTimestamp": ts(now.Add(-74*time.Hour + 3*time.Minute))},
				map[string]any{"result": "Failed", "startTimestamp": ts(now.Add(-50 * time.Hour)), "completeTimestamp": ts(now.Add(-48 * time.Hour)),
					"message": "repository maintenance failed: unable to acquire lock"},
			}
		}
		c.put(kindVeleroRepositories, veleroObject("BackupRepository", ns+"-default-kopia", now.Add(-30*24*time.Hour), nil, map[string]any{
			"volumeNamespace": ns, "backupStorageLocation": "default", "repositoryType": "kopia", "maintenanceFrequency": "1h0m0s",
		}, status))
	}
}

// demoPodVolume is one of the demo's pod volume backups.
type demoPodVolume struct {
	pod, volume string
	node        int // which worker it ran on
	phase, msg  string
}

// The reasons the demo's failed pod volume backups give, as Velero writes them: a pod that was
// gone when its volume's turn came (a job's short-lived pod), and a node agent without an answer.
const (
	pvbExposeError = "error to expose PVB: error to get pod volume path: error identifying unique volume path on host for volume %s " +
		"in pod %s: expected one matching path: /host_pods/%s/volumes/*/%s, got 0"
	pvbNotReady = "exposed PVB is not ready: no expose result is available for the current node"
)

// buildVeleroPodVolumes adds the pod volume backups of shop-volumes' newest backup, which
// partially failed: its four errors are the three volumes of short-lived pods that were gone and
// the one a node agent did not expose; two more completed.
func (c *Cluster) buildVeleroPodVolumes(backup string, at time.Time) {
	var workers []string
	for _, n := range c.w.nodes {
		if n.role == roleWorker {
			workers = append(workers, n.name)
		}
	}
	if len(workers) == 0 {
		workers = []string{"worker-1"}
	}
	for i, v := range []demoPodVolume{
		{pod: "nightly-export-29384011-x7k2p", volume: "scratch", node: 0, phase: "Failed", msg: pvbExposeError},
		{pod: "nightly-export-29384011-q9z4m", volume: "scratch", node: 1, phase: "Failed", msg: pvbExposeError},
		{pod: "cache-warmup-29384011-h2n8d", volume: "tmp-data", node: 0, phase: "Failed", msg: pvbExposeError},
		{pod: "orders-db-0", volume: "data", node: 2, phase: "Failed", msg: pvbNotReady},
		{pod: "cart-db-0", volume: "data", node: 1, phase: "Completed"},
		{pod: "catalog-cache-0", volume: "cache", node: 2, phase: "Completed"},
	} {
		podUID := stableUID("pod", "demo-shop", v.pod)
		msg := v.msg
		if msg == pvbExposeError {
			msg = fmt.Sprintf(pvbExposeError, v.volume, v.pod, podUID, v.volume)
		}
		start := at.Add(time.Duration(30+i*20) * time.Second)
		status := map[string]any{"phase": v.phase, "startTimestamp": ts(start), "completionTimestamp": ts(start.Add(40 * time.Second))}
		if msg != "" {
			status["message"] = msg
		}
		if v.phase == "Completed" {
			status["progress"] = map[string]any{"totalBytes": int64(1_342_177_280), "bytesDone": int64(1_342_177_280)}
			status["snapshotID"] = stableUID("snapshot", backup, v.pod)[:16]
		}
		name := backup + "-" + stableUID("podvolumebackup", backup, v.pod, v.volume)[:5]
		obj := veleroObject("PodVolumeBackup", name, at, map[string]any{
			"velero.io/backup-name": backup, "velero.io/backup-uid": stableUID(veleroBackupKind, backup),
		}, map[string]any{
			"backupStorageLocation": "default", "uploaderType": "kopia", "volume": v.volume, "node": workers[v.node%len(workers)],
			"pod":  map[string]any{"kind": "Pod", "namespace": "demo-shop", "name": v.pod, "uid": podUID},
			"tags": map[string]any{"backup": backup, "ns": "demo-shop", "pod": v.pod, "volume": v.volume},
		}, status)
		obj.Object["metadata"].(map[string]any)["ownerReferences"] = []any{
			map[string]any{"apiVersion": veleroAPI, "kind": veleroBackupKind, "name": backup, "uid": stableUID(veleroBackupKind, backup), "controller": true},
		}
		c.put(kindVeleroPodVolumeBackups, obj)
	}
}

// buildVeleroVolumes adds the PVCs of some namespaces: demo-ml and demo-media are outside every
// schedule, demo-media's managed by Flux, demo-data only in part (hourly-db's selector).
func (c *Cluster) buildVeleroVolumes(now time.Time) {
	for _, v := range []struct {
		ns, name, size string
		flux           bool
	}{
		{"demo-ml", "training-data", "200Gi", false}, {"demo-ml", "models", "50Gi", false}, {"demo-ml", "notebooks", "10Gi", false},
		{"demo-ml", "feature-store", "100Gi", false}, {"demo-media", "uploads", "20Gi", true},
		{"demo-data", "postgres-0", "40Gi", false}, {"demo-data", "postgres-1", "40Gi", false},
		{"demo-shop", "cart-db", "8Gi", false}, {"demo-shop", "orders-db", "8Gi", false},
		{"demo-search", "index-0", "30Gi", false},
	} {
		if c.namespace(v.ns) == nil {
			continue // a small demo cluster has fewer namespaces
		}
		labels := map[string]any{"app": v.name}
		if v.flux {
			labels["kustomize.toolkit.fluxcd.io/name"] = "media"
			labels["kustomize.toolkit.fluxcd.io/namespace"] = "flux-system"
		}
		uid := stableUID("persistentvolumeclaim", v.ns, v.name)
		c.put(kindPVCs, object("v1", "PersistentVolumeClaim", map[string]any{
			"name": v.name, "namespace": v.ns, "uid": uid, "creationTimestamp": ts(now.Add(-20 * 24 * time.Hour)), "labels": labels,
		}, map[string]any{
			"spec": map[string]any{"accessModes": []any{"ReadWriteOnce"}, "storageClassName": "standard", "volumeMode": "Filesystem",
				"resources": map[string]any{"requests": map[string]any{"storage": v.size}}, "volumeName": "pvc-" + uid},
			"status": map[string]any{"phase": "Bound", "accessModes": []any{"ReadWriteOnce"}, "capacity": map[string]any{"storage": v.size}},
		}))
	}
}

// veleroObject is an object of one of Velero's kinds in its namespace.
func veleroObject(kind, name string, created time.Time, labels map[string]any, spec, status map[string]any) *unstructured.Unstructured {
	meta := map[string]any{"name": name, "namespace": veleroNS, "uid": stableUID(kind, name), "creationTimestamp": ts(created)}
	if len(labels) > 0 {
		meta["labels"] = labels
	}
	fields := map[string]any{"spec": spec}
	if status != nil {
		fields["status"] = status
	}
	return object("velero.io/v1", kind, meta, fields)
}

// veleroScheduleObject is a schedule whose last backup was submitted at newest.
func veleroScheduleObject(s veleroSchedule, newest, now time.Time) *unstructured.Unstructured {
	tmpl := cloneMap(s.template)
	tmpl["ttl"] = s.ttl
	return veleroObject("Schedule", s.name, now.Add(-45*24*time.Hour), nil, map[string]any{
		"schedule": s.cron, "paused": s.paused, "useOwnerReferencesInBackup": false, "template": tmpl,
	}, map[string]any{"phase": "Enabled", "lastBackup": ts(newest.Add(12 * time.Second))})
}

// veleroBackupObject is a backup submitted at at that took took (unfinished while in progress),
// of a schedule ("" for one made by hand).
func veleroBackupObject(name, schedule string, tmpl map[string]any, ttl, phase string, at time.Time, took time.Duration,
	outcome map[string]any) *unstructured.Unstructured {
	spec := cloneMap(tmpl)
	spec["ttl"] = ttl
	location, _ := spec["storageLocation"].(string)
	labels := map[string]any{"velero.io/storage-location": location}
	if schedule != "" {
		labels["velero.io/schedule-name"] = schedule
	}
	start := at.Add(12 * time.Second)
	life, _ := time.ParseDuration(ttl)
	status := map[string]any{"phase": phase, "startTimestamp": ts(start), "expiration": ts(start.Add(life)), "formatVersion": "1.1.0",
		"version": int64(1)}
	if phase != "InProgress" {
		status["completionTimestamp"] = ts(start.Add(took))
		status["progress"] = map[string]any{"totalItems": int64(412), "itemsBackedUp": int64(412)}
	}
	for k, v := range outcome {
		status[k] = v
	}
	return veleroObject(veleroBackupKind, name, at, labels, spec, status)
}

// veleroServer is Velero's Deployment: its server image and the AWS plugin as an init container.
func veleroServer(now time.Time) *unstructured.Unstructured {
	labels := map[string]any{"app.kubernetes.io/name": "velero", "app.kubernetes.io/instance": "velero", "component": "velero"}
	return object("apps/v1", "Deployment", map[string]any{
		"name": "velero", "namespace": veleroNS, "uid": stableUID("deployment", veleroNS, "velero"),
		"creationTimestamp": ts(now.Add(-60 * 24 * time.Hour)), "labels": labels, "generation": int64(3),
	}, map[string]any{
		"spec": map[string]any{
			"replicas": int64(1),
			"selector": map[string]any{"matchLabels": map[string]any{"app.kubernetes.io/name": "velero"}},
			"template": map[string]any{
				"metadata": map[string]any{"labels": labels},
				"spec": map[string]any{
					"serviceAccountName": "velero-server",
					"initContainers": []any{map[string]any{"name": "velero-plugin-for-aws", "image": "velero/velero-plugin-for-aws:v1.13.0",
						"volumeMounts": []any{map[string]any{"name": "plugins", "mountPath": "/target"}}}},
					"containers": []any{map[string]any{"name": "velero", "image": "velero/velero:v1.17.1", "command": []any{"/velero"},
						"args": []any{"server", "--uploader-type=kopia", "--features="}}},
				},
			},
		},
		"status": map[string]any{"observedGeneration": int64(3), "replicas": int64(1), "readyReplicas": int64(1), "availableReplicas": int64(1),
			"updatedReplicas": int64(1), "conditions": []any{map[string]any{"type": "Available", "status": condTrue, "reason": "MinimumReplicasAvailable",
				"lastTransitionTime": ts(now.Add(-6 * 24 * time.Hour))}}},
	})
}

// veleroNodeAgent is Velero's node agent, on each of the n nodes, all ready.
func veleroNodeAgent(now time.Time, n int) *unstructured.Unstructured {
	labels := map[string]any{"name": "node-agent", "component": "velero"}
	nodes := int64(max(n, 1))
	return object("apps/v1", "DaemonSet", map[string]any{
		"name": "node-agent", "namespace": veleroNS, "uid": stableUID("daemonset", veleroNS, "node-agent"),
		"creationTimestamp": ts(now.Add(-60 * 24 * time.Hour)), "labels": labels, "generation": int64(2),
	}, map[string]any{
		"spec": map[string]any{
			"selector": map[string]any{"matchLabels": map[string]any{"name": "node-agent"}},
			"template": map[string]any{"metadata": map[string]any{"labels": labels}, "spec": map[string]any{
				"containers": []any{map[string]any{"name": "node-agent", "image": "velero/velero:v1.17.1", "args": []any{"node-agent", "server"}}},
			}},
			"updateStrategy": map[string]any{"type": "RollingUpdate"},
		},
		"status": map[string]any{"desiredNumberScheduled": nodes, "currentNumberScheduled": nodes, "numberReady": nodes,
			"updatedNumberScheduled": nodes, "numberAvailable": nodes, "observedGeneration": int64(2)},
	})
}

// veleroCRD is an established definition of one of Velero's kinds at velero.io/v1.
func veleroCRD(plural, kind string, now time.Time) *unstructured.Unstructured {
	name := plural + ".velero.io"
	return object("apiextensions.k8s.io/v1", "CustomResourceDefinition", map[string]any{
		"name": name, "uid": stableUID("customresourcedefinition", name), "creationTimestamp": ts(now.Add(-60 * 24 * time.Hour)),
		"labels": map[string]any{"component": "velero"},
	}, map[string]any{
		"spec": map[string]any{
			"group": "velero.io", "scope": "Namespaced",
			"names": map[string]any{"plural": plural, "singular": strings.ToLower(kind), "kind": kind, "listKind": kind + "List"},
			"versions": []any{map[string]any{"name": "v1", "served": true, "storage": true,
				"schema": map[string]any{"openAPIV3Schema": map[string]any{"type": "object", "x-kubernetes-preserve-unknown-fields": true}}}},
		},
		"status": map[string]any{"conditions": []any{
			map[string]any{"type": "NamesAccepted", "status": condTrue, "reason": "NoConflicts"},
			map[string]any{"type": "Established", "status": condTrue, "reason": "InitialNamesAccepted"},
		}},
	})
}

// cloneMap copies a map one level deep: the templates are shared by the schedules' backups.
func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}
