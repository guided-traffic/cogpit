package model

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// KindProfile makes a Kubernetes kind look the way Lens shows it: its columns, its status rule,
// the fields agents can search by, and the sections of its detail drawer. Built-in profiles live
// in package kinds, one file per kind.
//
// Every function field must tolerate any unstructured content without panicking: missing fields,
// wrong types, null. Return empty cells instead.
type KindProfile struct {
	Key        string // kind key: the plural resource name, "pods"
	Group      string // API group, "" for the core group
	Version    string // preferred version, "v1"
	Resource   string // plural resource, "pods"
	Kind       string // Kubernetes Kind, "Pod"
	Title      string // view title as Lens shows it, "Pods"
	Namespaced bool

	// FallbackVersions are older versions of the kind, in order of preference, that a cluster
	// which does not serve Version may serve instead: Validating Admission Policies are v1 from
	// Kubernetes 1.30, v1beta1 or v1alpha1 before. The connector keeps the profile at the first
	// version the cluster serves (a copy with that Version), so the kind key stays the same
	// (architecture §4.5). The kind's fields that Cells and Detail read must mean the same in
	// every listed version.
	FallbackVersions []string

	// Columns in Lens's order. The first column is the name.
	Columns []Column
	// States are the columns whose cells are a row's states for the views' status filter (D-34,
	// states.go); nil: those of DefaultStateColumns the kind has.
	States []StateColumn

	// Cells returns one cell per column, in the order of Columns, and the row's status token.
	Cells func(obj *unstructured.Unstructured) ([]Cell, Status)

	// Fields are the searchable fields agents use with resources_find, beyond the generic "name",
	// "namespace" and "label" fields that every kind has (see FieldValues). Each returns the
	// object's values for that field: "clusterIP" → ["10.96.100.100"].
	Fields map[string]func(obj *unstructured.Unstructured) []string

	// Detail returns the kind's own drawer sections (containers, conditions, ports). The engine
	// adds the generic metadata section, the events and the YAML itself; for a virtual kind
	// (Derive) it adds neither metadata nor events, whose objects are Cogpit's own and are no
	// event's involved object: its Detail returns the whole drawer, Properties first.
	Detail func(obj *unstructured.Unstructured) []DetailSection
	// DetailTitle, when set, names the kind in the drawer's title ("<DetailTitle>: <name>") in
	// place of Kind: Lens titles a Helm release's drawer "Release: <name>" (captured 2026-10-12).
	DetailTitle string

	// What the cache holds for the kind (architecture §5). The engine builds every cache scope of
	// the kind from these, so a view, the drawer, Lookup and agents' reads share one informer.

	// MetadataOnly makes the cache hold only the objects' metadata: the objects Cells and Detail
	// see have apiVersion meta.k8s.io/v1, kind PartialObjectMetadata and metadata, nothing else
	// (Secrets, H-8). Data the cache does not hold comes from a direct read the drawer makes.
	MetadataOnly bool
	// LabelSelector narrows the kind's watch on the server: "owner=helm".
	LabelSelector string

	// Derive makes the kind virtual: its objects are not the cached objects of GVR (with
	// LabelSelector and MetadataOnly) but what Derive makes of them: Helm releases, one per release,
	// from their release Secrets. It gets every cached object of the scope (or of one namespace) and
	// returns the kind's objects, each with a name, a namespace if Namespaced, and a UID and
	// resourceVersion that change when the object does (the row cache keys on them). It must be
	// pure, must not modify its input, and must tolerate any content without panicking; the engine
	// calls it on every snapshot, so it memoizes expensive work itself. Commands that write API
	// objects (object.edit, object.delete) do not apply to virtual kinds.
	Derive func(objs []*unstructured.Unstructured) []*unstructured.Unstructured
	// Local marks a kind whose objects Cogpit makes itself, served from the backend's local store
	// and never from the API server: the port forwards. Group and Version name Cogpit's own API
	// (LocalGroup, "v1"). Commands that write API objects do not apply to local kinds.
	Local bool

	// Needs are the indexes over other kinds' cached objects that CellsWith and DetailWith read
	// (a PVC's pods, a node's pods). While a view or the drawer of this kind is open, the engine
	// holds a cache handle on each index's kind and rebuilds the rows when those objects change.
	Needs []RelatedIndex
	// DetailNeeds are indexes that only DetailWith reads, not the rows (a workload's pods, D-40).
	// While the drawer of this kind is open the engine holds a handle on their kinds and wakes the
	// drawer when the objects it looked up change; a view of the kind holds none, so its rows are
	// not rebuilt when those objects change. DetailWith reads Needs and DetailNeeds alike.
	DetailNeeds []RelatedIndex
	// CellsWith replaces Cells when set: the same, with the related objects of Needs. rel is never
	// nil; its lookups return nothing while an index's kind is still listing or not served.
	CellsWith func(obj *unstructured.Unstructured, rel Related) ([]Cell, Status)
	// DetailWith replaces Detail when set: the same, with the related objects of Needs.
	DetailWith func(obj *unstructured.Unstructured, rel Related) []DetailSection

	// Usage, when set, says the kind has CPU and Memory columns (ids [ColumnCPU] and
	// [ColumnMemory], type [ColumnMetric]) that show the object's usage from the Metrics API. Cells
	// leaves them empty; while the view is open the engine polls the metrics (architecture §9) and
	// fills them through UsageCells.
	Usage UsageKind
	// UsageCells returns the CPU and Memory cells of an object that has a measurement. It must
	// tolerate any content without panicking.
	UsageCells func(obj *unstructured.Unstructured, u Usage) (cpu, memory Cell)
	// DiskCell, when set, says the kind has a Disk column (id [ColumnDisk], type [ColumnMetric])
	// that shows a node's root filesystem as node-exporter reports it through Prometheus (the
	// Metrics Server measures no disk; the kubelet's summary API is not used). Cells leaves it
	// empty; while the view is open and Prometheus is found the engine polls it and fills it
	// through DiskCell. It must tolerate any content without panicking.
	DiskCell func(obj *unstructured.Unstructured, d NodeDisk) Cell
	// Volume, when set, says the kind's drawer shows the usage of a persistent volume: its chart
	// over time and a usage bar, from the kubelet's volume stats through Prometheus (architecture
	// §9). It returns the claim and the volume of an object that is bound, false while it is not.
	// It must tolerate any content without panicking.
	Volume func(obj *unstructured.Unstructured) (VolumeClaim, bool)
}

// LocalGroup is the API group of the local kinds, the objects Cogpit makes itself (port forwards).
// No Kubernetes API server serves it.
const LocalGroup = "cogpit.io"

// APIObject reports whether the kind's objects are objects of the cluster's API that commands may
// write: false for virtual (Derive) and local kinds.
func (p *KindProfile) APIObject() bool { return p.Derive == nil && !p.Local }

// GVR returns the profile's group, version and resource.
func (p *KindProfile) GVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: p.Group, Version: p.Version, Resource: p.Resource}
}

// ColumnIndex returns the index of the column with the given id, or -1.
func (p *KindProfile) ColumnIndex(id string) int {
	for i, c := range p.Columns {
		if c.ID == id {
			return i
		}
	}
	return -1
}

// FieldNames returns every field name resources_find accepts for this kind, generic ones first.
func (p *KindProfile) FieldNames() []string {
	names := make([]string, 0, 3+len(p.Fields))
	names = append(names, "name", "namespace", "label")
	for k := range p.Fields {
		names = append(names, k)
	}
	return names
}

// BuildRow turns an object into a table row for the given kubeconfig context. It never panics:
// a profile whose Cells function panics yields a row with the name and an unknown status. A
// profile with CellsWith sees no related objects; the engine's views use BuildRowWith.
func BuildRow(p *KindProfile, context string, obj *unstructured.Unstructured) (row Row) {
	return BuildRowWith(p, context, obj, nil)
}

// BuildRowWith is BuildRow with the related objects of the profile's Needs; nil means none.
func BuildRowWith(p *KindProfile, context string, obj *unstructured.Unstructured, rel Related) (row Row) {
	row = Row{
		Ref: ObjectRef{
			Context:   context,
			Kind:      p.Key,
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		},
		UID:             string(obj.GetUID()),
		ResourceVersion: obj.GetResourceVersion(),
	}
	defer func() {
		if r := recover(); r != nil {
			cells := make([]Cell, len(p.Columns))
			if len(cells) > 0 {
				cells[0] = Cell{Text: obj.GetName()}
			}
			row.Cells, row.Status = cells, StatusUnknown
		}
	}()
	switch {
	case p.CellsWith != nil:
		row.Cells, row.Status = p.CellsWith(obj, relatedOrNone(rel))
	case p.Cells != nil:
		row.Cells, row.Status = p.Cells(obj)
	default:
		row.Cells = make([]Cell, len(p.Columns))
		if len(row.Cells) > 0 {
			row.Cells[0] = Cell{Text: obj.GetName()}
		}
		return row
	}
	if len(row.Cells) != len(p.Columns) {
		fixed := make([]Cell, len(p.Columns))
		copy(fixed, row.Cells)
		row.Cells = fixed
	}
	if obj.GetDeletionTimestamp() != nil {
		row.Status = StatusTerminating
	}
	return row
}

// FieldValues returns the object's values for a searchable field. The generic fields are "name",
// "namespace" and "label" (each label as "key=value"); the rest come from the profile. An unknown
// field returns nil.
func FieldValues(p *KindProfile, field string, obj *unstructured.Unstructured) []string {
	switch field {
	case "name":
		return []string{obj.GetName()}
	case "namespace":
		return []string{obj.GetNamespace()}
	case "label", "labels":
		labels := obj.GetLabels()
		out := make([]string, 0, len(labels))
		for k, v := range labels {
			out = append(out, k+"="+v)
		}
		return out
	}
	if f, ok := p.Fields[field]; ok && f != nil {
		return safeStrings(f, obj)
	}
	return nil
}

func safeStrings(f func(*unstructured.Unstructured) []string, obj *unstructured.Unstructured) (out []string) {
	defer func() {
		if recover() != nil {
			out = nil
		}
	}()
	return f(obj)
}

// MatchesFilter reports whether a row matches the search text of a view: every whitespace-
// separated term must occur, case-insensitively, in one of the row's cells.
func MatchesFilter(row Row, filter string) bool {
	terms := strings.Fields(strings.ToLower(filter))
	if len(terms) == 0 {
		return true
	}
	for _, term := range terms {
		found := false
		for _, c := range row.Cells {
			if strings.Contains(strings.ToLower(c.Text), term) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
