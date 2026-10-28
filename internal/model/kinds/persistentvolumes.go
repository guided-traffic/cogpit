package kinds

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

const (
	keyPersistentVolumes = "persistentvolumes"
	pvAvailable          = "Available"
	pvReleased           = "Released"
	pvFailed             = "Failed"
	idClaim              = "claim"
	idStorageClass       = "storageClass"
	titleStorageClass    = "Storage Class"
	titleAccessModes     = "Access Modes"
	idSource             = "source"
	// Volume source types the drawer describes in detail.
	sourceCSI      = "csi"
	sourceNFS      = "nfs"
	sourceHostPath = "hostPath"
)

// persistentVolumes has the columns of Lens's documentation (not captured): Name, Storage Class,
// Capacity, Claim, Age, Status. The kind is cluster-scoped. Storage Class links to the class,
// Claim ("namespace/name") to the claim. The status rule and the drawer are inferred.
var persistentVolumes = &model.KindProfile{
	Key:      keyPersistentVolumes,
	Version:  "v1",
	Resource: keyPersistentVolumes,
	Kind:     "PersistentVolume",
	Title:    "Persistent Volumes",
	Columns: []model.Column{
		nameColumn,
		{ID: idStorageClass, Title: titleStorageClass, Type: model.ColumnText},
		{ID: "capacity", Title: "Capacity", Type: model.ColumnNumber}, // sorted by bytes
		{ID: idClaim, Title: "Claim", Type: model.ColumnText},
		ageColumn,
		statusColumn,
	},
	Cells: pvCells,
	Fields: map[string]fieldFunc{
		idStorageClass: func(obj *unstructured.Unstructured) []string {
			return nonEmpty(pvcStorageClass(obj))
		},
		idClaim: func(obj *unstructured.Unstructured) []string {
			return nonEmpty(pvClaimText(obj))
		},
		"accessMode": func(obj *unstructured.Unstructured) []string {
			return model.NestedStrings(obj, "spec", "accessModes")
		},
		idReclaimPolicy: func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, "spec", "persistentVolumeReclaimPolicy"))
		},
		"volumeMode": func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, "spec", "volumeMode"))
		},
		// "source" is the volume's type, and for CSI volumes the driver: "csi", "csi.example.com".
		idSource: func(obj *unstructured.Unstructured) []string {
			typ, _ := pvSource(obj)
			if typ == "" {
				return nil
			}
			return appendUnique([]string{typ}, model.NestedString(obj, "spec", sourceCSI, "driver"))
		},
		idStatus: func(obj *unstructured.Unstructured) []string {
			text, _ := pvStatus(obj)
			return []string{text}
		},
	},
	Detail: pvDetail,
	Volume: pvVolume,
	// The pods that mount the volume's claim, named above its chart (engine/volumemetrics.go).
	DetailNeeds: []model.RelatedIndex{PodsByClaim},
}

// pvVolume is the volume and the claim it is bound to: the drawer shows the volume's usage, which
// the kubelet reports by the claim's name.
func pvVolume(obj *unstructured.Unstructured) (model.VolumeClaim, bool) {
	ns, name := pvClaim(obj)
	if ns == "" || name == "" || model.NestedString(obj, "status", "phase") != pvcBound {
		return model.VolumeClaim{}, false
	}
	return model.VolumeClaim{Namespace: ns, Claim: name, Volume: obj.GetName()}, true
}

func pvCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	text, token := pvStatus(obj)
	class := pvcStorageClass(obj)
	ns, name := pvClaim(obj)
	capacity := model.Cell{Text: "-"}
	if c := model.NestedIntOrString(obj, "spec", "capacity", resourceStorage); c != "" {
		capacity = model.Cell{Text: model.FormatByteQuantity(c), Num: quantityValue(c)}
	}
	return []model.Cell{
		nameCell(obj),
		{Text: class, Link: linkTo(keyStorageClasses, "", class)},
		capacity,
		{Text: pvClaimText(obj), Link: linkTo(persistentVolumeClaims.Key, ns, name)},
		model.AgeCell(created(obj)),
		{Text: text, Status: token},
	}, token
}

// pvStatus is status.phase (inferred): Bound and Available ok (a volume waiting to be claimed is
// healthy), Pending pending, Released a warning (its claim is gone and the volume keeps the data
// until someone reclaims it), Failed an error (automatic reclamation failed), and Terminating once
// deleted. A missing phase is shown as Unknown rather than guessed.
func pvStatus(obj any) (string, model.Status) {
	if deleting(obj) {
		return pvcTerminating, model.StatusTerminating
	}
	switch phase := model.NestedString(obj, "status", "phase"); phase {
	case pvcBound, pvAvailable:
		return phase, model.StatusOK
	case pvcPending:
		return phase, model.StatusPending
	case pvReleased:
		return phase, model.StatusWarning
	case pvFailed:
		return phase, model.StatusError
	case "":
		return textUnknown, model.StatusUnknown
	default:
		return phase, model.StatusUnknown
	}
}

// pvClaim is the claim spec.claimRef names: bound, or reserved for it.
func pvClaim(obj any) (namespace, name string) {
	return model.NestedString(obj, "spec", "claimRef", "namespace"), model.NestedString(obj, "spec", "claimRef", "name")
}

// pvClaimText renders the claim as kubectl does, "namespace/name".
func pvClaimText(obj any) string {
	ns, name := pvClaim(obj)
	switch {
	case name == "":
		return ""
	case ns == "":
		return name
	}
	return ns + "/" + name
}

// pvSourceTypes are the volume sources a persistent volume may have, in the order the drawer
// checks them; the first one set is the volume's.
var pvSourceTypes = []string{
	sourceCSI, "local", sourceHostPath, sourceNFS, "iscsi", "fc", "rbd", "cephfs", "awsElasticBlockStore",
	"gcePersistentDisk", "azureDisk", "azureFile", "cinder", "vsphereVolume", "portworxVolume",
	"flexVolume", "glusterfs", "quobyte", "scaleIO", "storageos", "photonPersistentDisk", "flocker",
}

// pvSource returns the volume's source type ("csi", "nfs") and a description of where its data
// lives: "csi.example.com, volume handle vol-0a1b", "nfs.example.com:/exports/share". CSI volume
// attributes and secret references are left out: they are long, and the drawer has no use for a
// Secret's name here.
func pvSource(obj any) (typ, text string) {
	spec := model.NestedMap(obj, "spec")
	for _, t := range pvSourceTypes {
		src := model.NestedMap(spec, t)
		if src == nil {
			continue
		}
		var parts []string
		add := func(label, field string) {
			if v := model.NestedIntOrString(src, field); v != "" {
				parts = append(parts, label+v)
			}
		}
		switch t {
		case sourceCSI:
			add("", "driver")
			add("volume handle ", "volumeHandle")
		case sourceNFS:
			if server, path := model.NestedString(src, "server"), model.NestedString(src, "path"); server != "" || path != "" {
				parts = append(parts, server+":"+path)
			}
		case "iscsi":
			add("", "targetPortal")
			add("", "iqn")
			add("LUN ", "lun")
		case "local", sourceHostPath:
			add("", "path")
			add("type ", "type")
		default:
			add("", "volumeID")
			add("", "pdName")
			add("", "diskName")
			add("", "volumePath")
			add("", "driver")
		}
		add("fs ", "fsType")
		if ro, _ := model.NestedBool(src, "readOnly"); ro {
			parts = append(parts, "read-only")
		}
		return t, strings.Join(parts, ", ")
	}
	return "", ""
}

// pvNodeAffinity renders spec.nodeAffinity.required: one line per node selector term, its
// expressions joined by ", ". A local volume can be used only on the nodes it names.
func pvNodeAffinity(obj any) []string {
	var out []string
	for _, term := range model.NestedSlice(obj, "spec", "nodeAffinity", "required", "nodeSelectorTerms") {
		var exprs []string
		for _, e := range model.NestedSlice(term, "matchExpressions") {
			key := model.NestedString(e, "key")
			if key == "" {
				continue
			}
			values := strings.Join(model.NestedStrings(e, "values"), ",")
			switch op := model.NestedString(e, "operator"); op {
			case "In":
				exprs = append(exprs, key+" in ("+values+")")
			case "NotIn":
				exprs = append(exprs, key+" notin ("+values+")")
			case opExists:
				exprs = append(exprs, key)
			case "DoesNotExist":
				exprs = append(exprs, "!"+key)
			default:
				exprs = append(exprs, key+" "+op+" ("+values+")")
			}
		}
		if len(exprs) > 0 {
			out = append(out, strings.Join(exprs, ", "))
		}
	}
	return out
}

// pvDetail is Cogpit's drawer for a volume (inferred, Lens's is not captured): its state, class,
// claim and source in Properties.
func pvDetail(obj *unstructured.Unstructured) []model.DetailSection {
	var d detailItems
	text, token := pvStatus(obj)
	d.addStatus(titleStatus, text, token)
	d.add("Reason", model.NestedString(obj, "status", "reason"))
	d.add("Message", model.NestedString(obj, "status", "message"))
	class := pvcStorageClass(obj)
	d.put(linkedItem(titleStorageClass, class, linkTo(keyStorageClasses, "", class)))
	d.add("Capacity", model.FormatByteQuantity(model.NestedIntOrString(obj, "spec", "capacity", resourceStorage)))
	d.add(titleAccessModes, strings.Join(model.NestedStrings(obj, "spec", "accessModes"), ", "))
	d.add(titleReclaim, model.NestedString(obj, "spec", "persistentVolumeReclaimPolicy"))
	d.add("Volume Mode", model.NestedString(obj, "spec", "volumeMode"))
	ns, name := pvClaim(obj)
	d.put(linkedItem("Claim", pvClaimText(obj), linkTo(persistentVolumeClaims.Key, ns, name)))
	if typ, src := pvSource(obj); typ != "" {
		if src != "" {
			typ += ": " + src
		}
		d.add("Source", typ)
	}
	d.add("Mount Options", strings.Join(model.NestedStrings(obj, "spec", "mountOptions"), "\n"))
	d.add("Node Affinity", strings.Join(pvNodeAffinity(obj), "\n"))
	d.add("Volume Attributes Class", model.NestedString(obj, "spec", "volumeAttributesClassName"))
	d.add("Phase Changed", timestamp(model.NestedTime(obj, "status", "lastPhaseTransitionTime")))
	return withSection(nil, model.TitleProperties, d)
}
