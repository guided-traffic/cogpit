package testkit

import (
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Velero fixtures (the Velero page, D-179): its definitions as Velero names them, served at
// velero.io/v1 with a permissive schema (no Velero is installed), and objects of its kinds.

// VeleroNamespace is where the fixtures' Velero lives.
const VeleroNamespace = "velero"

// Velero's resources.
var (
	VeleroSchedulesGVR         = schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "schedules"}
	VeleroBackupsGVR           = schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "backups"}
	VeleroRestoresGVR          = schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "restores"}
	VeleroLocationsGVR         = schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "backupstoragelocations"}
	VeleroSnapshotLocationsGVR = schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "volumesnapshotlocations"}
	VeleroRepositoriesGVR      = schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "backuprepositories"}
	VeleroPodVolumeBackupsGVR  = schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "podvolumebackups"}
	VeleroDataUploadsGVR       = schema.GroupVersionResource{Group: "velero.io", Version: "v2alpha1", Resource: "datauploads"}
	DaemonSetsGVR              = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}
	StatefulSetsGVR            = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}
	PVCsGVR                    = schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}
)

// VeleroCRDs returns the definitions of Velero's Schedules, Backups, Restores, storage and
// snapshot locations, repositories, pod volume backups and (at v2alpha1) data uploads, established.
func VeleroCRDs() []*unstructured.Unstructured {
	return []*unstructured.Unstructured{
		CRD("velero.io", "schedules", "Schedule", true),
		CRD("velero.io", "backups", "Backup", true),
		CRD("velero.io", "restores", "Restore", true),
		CRD("velero.io", "backupstoragelocations", "BackupStorageLocation", true),
		CRD("velero.io", "volumesnapshotlocations", "VolumeSnapshotLocation", true),
		CRD("velero.io", "backuprepositories", "BackupRepository", true),
		CRD("velero.io", "podvolumebackups", "PodVolumeBackup", true),
		CRD("velero.io", "datauploads", "DataUpload", true, CRDVersion{Name: "v2alpha1", Served: true, Storage: true}),
	}
}

// VeleroObject builds an object of one of Velero's kinds in VeleroNamespace, created at created,
// with labels (nil: none) and its spec and status (nil: left out).
func VeleroObject(kind, name string, created time.Time, labels map[string]string, spec, status map[string]any) *unstructured.Unstructured {
	fields := map[string]any{"metadata": map[string]any{"creationTimestamp": created.UTC().Format(time.RFC3339)}}
	if spec != nil {
		fields["spec"] = spec
	}
	if status != nil {
		fields["status"] = status
	}
	obj := Object("velero.io/v1", kind, VeleroNamespace, name, fields)
	if len(labels) > 0 {
		obj.SetLabels(labels)
	}
	return obj
}

// VeleroTime writes a time as Velero's objects do.
func VeleroTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }
