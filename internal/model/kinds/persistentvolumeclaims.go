package kinds

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

const (
	pvcBound   = "Bound"
	pvcPending = "Pending"
	pvcLost    = "Lost"
	// pvcTerminating is the Status text of a claim with a deletion timestamp.
	pvcTerminating = "Terminating"
	// storageClassAnnotation is the class annotation that predates spec.storageClassName.
	storageClassAnnotation = "volume.beta.kubernetes.io/storage-class"
	resourceStorage        = "storage"
)

// persistentVolumeClaims has the columns of Lens's documentation (not captured). The Pods column
// lists the pods that mount the claim, from the related-objects index PodsByClaim (architecture
// §4.4); Storage Class links to the class.
var persistentVolumeClaims = &model.KindProfile{
	Key:        "persistentvolumeclaims",
	Version:    "v1",
	Resource:   "persistentvolumeclaims",
	Kind:       "PersistentVolumeClaim",
	Title:      "Persistent Volume Claims",
	Namespaced: true,
	Columns: []model.Column{
		nameColumn,
		namespaceColumn,
		{ID: "storageClass", Title: "Storage Class", Type: model.ColumnText},
		{ID: "size", Title: "Size", Type: model.ColumnNumber},              // sorted by bytes
		{ID: idPods, Title: titlePods, Type: model.ColumnText, Width: 220}, // pod names, not the workloads' "3/3"
		ageColumn,
		statusColumn,
	},
	Needs:     []model.RelatedIndex{PodsByClaim},
	Cells:     pvcCells,
	CellsWith: pvcCellsWith,
	Fields: map[string]fieldFunc{
		"storageClass": func(obj *unstructured.Unstructured) []string {
			return nonEmpty(pvcStorageClass(obj))
		},
		"volumeName": func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, "spec", "volumeName"))
		},
		"accessMode": func(obj *unstructured.Unstructured) []string {
			return model.NestedStrings(obj, "spec", "accessModes")
		},
		"volumeMode": func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, "spec", "volumeMode"))
		},
		idStatus: func(obj *unstructured.Unstructured) []string {
			text, _ := pvcStatus(obj)
			return []string{text}
		},
	},
	Detail:     pvcDetail,
	DetailWith: pvcDetailWith,
	Volume:     pvcVolume,
}

// pvcVolume is the claim and its volume while it is bound: the drawer shows the volume's usage, which
// the kubelet reports by the claim's name.
func pvcVolume(obj *unstructured.Unstructured) (model.VolumeClaim, bool) {
	vol := model.NestedString(obj, "spec", "volumeName")
	if vol == "" || model.NestedString(obj, "status", "phase") != pvcBound {
		return model.VolumeClaim{}, false
	}
	return model.VolumeClaim{Namespace: obj.GetNamespace(), Claim: obj.GetName(), Volume: vol}, true
}

// pvcCells are the claim's cells without the related objects: the Pods column stays empty.
func pvcCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	return pvcCellsWith(obj, model.NoRelated)
}

func pvcCellsWith(obj *unstructured.Unstructured, rel model.Related) ([]model.Cell, model.Status) {
	text, token := pvcStatus(obj)
	class := pvcStorageClass(obj)
	return []model.Cell{
		nameCell(obj),
		namespaceCell(obj),
		{Text: class, Link: linkTo(keyStorageClasses, "", class)},
		pvcSize(obj),
		pvcPodsCell(claimPods(obj, rel)),
		model.AgeCell(created(obj)),
		{Text: text, Status: token},
	}, token
}

// claimPods returns the pods that mount the claim, sorted by name: the related objects of
// PodsByClaim. It is empty while the pods are still listing.
func claimPods(obj *unstructured.Unstructured, rel model.Related) []*unstructured.Unstructured {
	pods := append([]*unstructured.Unstructured(nil), rel.Objects(PodsByClaim, ClaimKey(obj.GetNamespace(), obj.GetName()))...)
	sortByName(pods)
	return pods
}

// sortByName sorts related objects by name, then namespace: the index returns them in no
// particular order.
func sortByName(objs []*unstructured.Unstructured) {
	slices.SortFunc(objs, func(a, b *unstructured.Unstructured) int {
		return cmp.Or(strings.Compare(a.GetName(), b.GetName()), strings.Compare(a.GetNamespace(), b.GetNamespace()))
	})
}

// pvcPodsCell is the Pods cell: the names of the pods that mount the claim, comma-separated. A
// cell holds one link: the claim of a single pod (the usual ReadWriteOnce claim) links to it; the
// names of several pods stay text, and the drawer links each.
func pvcPodsCell(pods []*unstructured.Unstructured) model.Cell {
	names := make([]string, len(pods))
	for i, p := range pods {
		names[i] = p.GetName()
	}
	c := model.Cell{Text: strings.Join(names, ", ")}
	if len(pods) == 1 {
		c.Link = linkTo(podsResource, pods[0].GetNamespace(), pods[0].GetName())
	}
	return c
}

// pvcStatus is status.phase: Bound ok, Pending pending, Lost error (the volume is gone), and
// Terminating once deleted, as kubectl prints it: a claim that a pod still uses stays in that
// state (pvc-protection), which is worth seeing. The API server sets a phase on every claim, so a
// missing one is shown as Unknown rather than guessed.
func pvcStatus(obj any) (string, model.Status) {
	if deleting(obj) {
		return pvcTerminating, model.StatusTerminating
	}
	switch phase := model.NestedString(obj, "status", "phase"); phase {
	case pvcBound:
		return phase, model.StatusOK
	case pvcPending:
		return phase, model.StatusPending
	case pvcLost:
		return phase, model.StatusError
	case "":
		return textUnknown, model.StatusUnknown
	default:
		return phase, model.StatusUnknown
	}
}

// pvcStorageClass is spec.storageClassName, else the older annotation when the field is not set.
// An explicit "" means no class: the claim binds only to volumes without one.
func pvcStorageClass(obj any) string {
	if _, set := model.NestedValue(obj, "spec", "storageClassName"); set {
		return model.NestedString(obj, "spec", "storageClassName")
	}
	return model.NestedString(obj, "metadata", "annotations", storageClassAnnotation)
}

// pvcSize is the claim's capacity once bound (status.capacity), else what it requests, in binary
// units (D-32). Num is the size in bytes, so the column sorts by size.
func pvcSize(obj any) model.Cell {
	text := model.NestedIntOrString(obj, "status", "capacity", resourceStorage)
	if text == "" {
		text = model.NestedIntOrString(obj, "spec", "resources", "requests", resourceStorage)
	}
	if text == "" {
		return model.Cell{Text: "-"}
	}
	return model.Cell{Text: model.FormatByteQuantity(text), Num: quantityValue(text)}
}

// quantityValue returns a quantity's value as a sort key, or 0 when it does not parse. It skips
// decimal exponents beyond ±30, which no size uses and which resource.ParseQuantity spends
// unbounded time on (it rounds a tiny value at full precision); objects from the API server are
// canonical, but the cache's content is treated as hostile.
func quantityValue(s string) int64 {
	if s == "" || len(s) > 64 {
		return 0
	}
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		if rest := s[i+1:]; rest != "" && rest != "i" { // "E" and "Ei" are suffixes
			exp, err := strconv.Atoi(rest)
			if err != nil || exp < -30 || exp > 30 {
				return 0
			}
		}
	}
	q, err := resource.ParseQuantity(s)
	if err != nil {
		return 0
	}
	return q.Value()
}

// pvcDetail is the claim's drawer without the related objects: no Pods item.
func pvcDetail(obj *unstructured.Unstructured) []model.DetailSection {
	return pvcDetailWith(obj, model.NoRelated)
}

// pvcDetailWith is the claim's drawer (inferred, Lens's is not captured). The class, the volume and
// each pod that mounts the claim link to their objects.
func pvcDetailWith(obj *unstructured.Unstructured, rel model.Related) []model.DetailSection {
	var d detailItems
	text, token := pvcStatus(obj)
	d.addStatus(titleStatus, text, token)
	class := pvcStorageClass(obj)
	if _, set := model.NestedValue(obj, "spec", "storageClassName"); set && class == "" {
		d.add(titleStorageClass, "none (binds only to volumes without a class)")
	} else {
		d.put(linkedItem(titleStorageClass, class, linkTo(keyStorageClasses, "", class)))
	}
	volume := model.NestedString(obj, "spec", "volumeName")
	d.put(linkedItem("Volume", volume, linkTo(keyPersistentVolumes, "", volume)))
	pods := claimPods(obj, rel)
	chips := make([]model.Chip, len(pods))
	for i, p := range pods {
		chips[i] = model.Chip{Text: p.GetName(), Link: linkTo(podsResource, p.GetNamespace(), p.GetName())}
	}
	d.put(model.ChipsItem(titlePods, chips))
	d.add("Access Modes", strings.Join(model.NestedStrings(obj, "spec", "accessModes"), ", "))
	d.add("Volume Mode", model.NestedString(obj, "spec", "volumeMode"))
	size := func(fields ...string) string {
		return model.FormatByteQuantity(model.NestedIntOrString(obj, fields...))
	}
	d.add("Requested", size("spec", "resources", "requests", resourceStorage))
	d.add("Limit", size("spec", "resources", "limits", resourceStorage))
	d.add("Capacity", size("status", "capacity", resourceStorage))
	d.add("Allocated", size("status", "allocatedResources", resourceStorage))
	resize := model.NestedString(obj, "status", "allocatedResourceStatuses", resourceStorage)
	d.addStatus("Resize Status", resize, resizeToken(resize))
	d.add("Volume Attributes Class", model.NestedString(obj, "spec", "volumeAttributesClassName"))
	d.add("Data Source", pvcDataSource(obj))
	d.add("Selector", strings.Join(labelSelector(obj, "spec", "selector"), "\n"))
	sections := withSection(nil, "Persistent Volume Claim", d)

	if cs, ok := conditionSection(conditions(obj, "status", "conditions"), pvcConditionToken); ok {
		sections = append(sections, cs)
	}
	return sections
}

// pvcDataSource renders the claim's data source (dataSourceRef, else dataSource) as "Kind/name",
// with its namespace when it is in another one: "VolumeSnapshot/nightly, namespace backups".
func pvcDataSource(obj any) string {
	src := model.NestedMap(obj, "spec", "dataSourceRef")
	if src == nil {
		src = model.NestedMap(obj, "spec", "dataSource")
	}
	kind, name := model.NestedString(src, "kind"), model.NestedString(src, "name")
	if name == "" {
		return ""
	}
	s := name
	if kind != "" {
		s = kind + "/" + name
	}
	if ns := model.NestedString(src, "namespace"); ns != "" {
		s += ", namespace " + ns
	}
	return s
}

// resizeToken colours status.allocatedResourceStatuses: a resize in progress or waiting for the
// node is pending, an infeasible one an error.
func resizeToken(s string) model.Status {
	switch {
	case s == "":
		return model.StatusNone
	case strings.HasSuffix(s, "Infeasible"):
		return model.StatusError
	default:
		return model.StatusPending
	}
}

// pvcConditionToken colours the claim's conditions. All of them report a change in flight
// (Resizing, FileSystemResizePending, ModifyingVolume) or its failure (ControllerResizeError,
// NodeResizeError, ModifyVolumeError) when True.
func pvcConditionToken(c condition) model.Status {
	whenTrue := model.StatusPending
	if strings.HasSuffix(c.Type, "Error") {
		whenTrue = model.StatusError
	}
	return conditionToken(c.Status, whenTrue, model.StatusNone)
}
