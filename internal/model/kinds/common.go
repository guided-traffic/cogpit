package kinds

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// fieldFunc is the type of a searchable field (model.KindProfile.Fields).
type fieldFunc = func(obj *unstructured.Unstructured) []string

// Condition statuses as the API writes them, the text for a state nobody reported, and a boolean
// as annotations and table cells spell it.
const (
	condTrue       = "True"
	condFalse      = "False"
	textUnknown    = "Unknown"
	textTrue       = "true"
	titleResources = "Resources" // a section, column or table heading (RBAC rules, Flux, Argo CD)
)

// Ids that name a column or a searchable field of more than one kind. A column and a field that
// show the same thing share an id, so agents can use one name for both.
const (
	idStatus     = "status"
	idType       = "type"
	idSelector   = "selector"
	idExternalIP = "externalIP"
	titleStatus  = "Status"
)

// keyNamespaces is the kind key namespace cells link to.
const keyNamespaces = "namespaces"

// fieldSpec is the object's spec, where the local and virtual kinds keep their fields too.
const fieldSpec = "spec"

// Column ids, titles and searchable field names several kinds share.
const (
	idKind       = "kind"
	titleKind    = "Kind"
	titleTarget  = "Target"
	fieldService = "service"
	fieldPath    = "path"
)

// Columns several kinds share.
var (
	nameColumn       = model.Column{ID: "name", Title: "Name", Type: model.ColumnText}
	namespaceColumn  = model.Column{ID: "namespace", Title: "Namespace", Type: model.ColumnText}
	ageColumn        = model.Column{ID: "age", Title: "Age", Type: model.ColumnAge}
	statusColumn     = model.Column{ID: idStatus, Title: titleStatus, Type: model.ColumnText}
	typeColumn       = model.Column{ID: idType, Title: "Type", Type: model.ColumnText}
	conditionsColumn = model.Column{ID: "conditions", Title: "Conditions", Type: model.ColumnText}
	// warningColumn is Lens's warning-icon column; it comes right after Name in the views that
	// have one (Pods, Nodes, Cron Jobs, Pod Disruption Budgets, the RBAC views). Its title is not
	// shown in the header; it names the column for agents and resources.list.
	warningColumn = model.Column{ID: "warning", Title: "Warning", Type: model.ColumnWarning, Width: 28}
)

func nameCell(obj any) model.Cell {
	return model.Cell{Text: model.NestedString(obj, "metadata", "name")}
}

// namespaceCell is the Namespace cell, a link to the namespace: in a list it narrows the view's
// namespace filter to that namespace.
func namespaceCell(obj any) model.Cell {
	ns := model.NestedString(obj, "metadata", "namespace")
	return model.Cell{Text: ns, Link: linkTo(keyNamespaces, "", ns)}
}

// linkTo returns a link to an object of a kind key, or nil when the name is empty. Namespace is
// empty for cluster-scoped kinds.
func linkTo(kind, namespace, name string) *model.ObjectRef {
	if name == "" {
		return nil
	}
	return &model.ObjectRef{Kind: kind, Namespace: namespace, Name: name}
}

// warningCell is a cell of warningColumn: no warning when status is StatusNone, else the icon in
// that status's colour with the reason as its tooltip.
func warningCell(status model.Status, reason string) model.Cell {
	if status == model.StatusNone {
		return model.Cell{}
	}
	return model.Cell{Text: reason, Num: 1, Status: status}
}

func created(obj any) time.Time {
	return model.NestedTime(obj, "metadata", "creationTimestamp")
}

// deleting reports whether the object has a deletion timestamp. It reads the timestamp the same
// way model.BuildRow does, so a row's cells and its terminating status never disagree.
func deleting(obj any) bool {
	return !model.NestedTime(obj, "metadata", "deletionTimestamp").IsZero()
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// ratio renders "ready/total" with ready as the sort key: the Containers and Pods columns.
func ratio(part, whole int64) model.Cell {
	return model.Cell{Text: itoa(part) + "/" + itoa(whole), Num: part}
}

// nonNegative reads a count, treating a missing or negative value as zero.
func nonNegative(root any, fields ...string) int64 {
	n, ok := model.NestedInt64(root, fields...)
	if !ok || n < 0 {
		return 0
	}
	return n
}

// appendUnique appends the non-empty values that out does not hold yet. Lists here are short
// (IPs, images), so a linear scan beats a map.
func appendUnique(out []string, values ...string) []string {
	for _, v := range values {
		if v == "" || contains(out, v) {
			continue
		}
		out = append(out, v)
	}
	return out
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

// nonEmpty returns the value as a one-element list, or nil when it is empty: the shape of a
// searchable field with at most one value.
func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

// orDash renders an empty list the way the table shows "none".
func orDash(list []string) string {
	if len(list) == 0 {
		return "-"
	}
	return strings.Join(list, ", ")
}

// firstTime returns the first non-zero time.
func firstTime(times ...time.Time) time.Time {
	for _, t := range times {
		if !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

// timestamp renders an absolute time for the detail drawer, which has no "now" to render ages
// against: a mark the engine writes in the user's zone and format (model.MarkTime, D-31); "" for
// the zero time.
func timestamp(t time.Time) string { return model.MarkTime(t) }

// byName returns the element of a list of named objects (containers, statuses) with the given
// name, or nil.
func byName(list []any, name string) any {
	if name == "" {
		return nil
	}
	for _, e := range list {
		if model.NestedString(e, "name") == name {
			return e
		}
	}
	return nil
}

// condition is one entry of a status.conditions list.
type condition struct {
	Type, Status, Reason, Message string
}

// conditions reads a conditions list, skipping entries that are not objects or have no type.
func conditions(root any, fields ...string) []condition {
	list := model.NestedSlice(root, fields...)
	out := make([]condition, 0, len(list))
	for _, e := range list {
		c := condition{
			Type:    model.NestedString(e, "type"),
			Status:  model.NestedString(e, "status"),
			Reason:  model.NestedString(e, "reason"),
			Message: model.NestedString(e, "message"),
		}
		if c.Type != "" {
			out = append(out, c)
		}
	}
	return out
}

// conditionStatus returns the status of the condition of the given type in the list at the path,
// or "" when there is none. It allocates nothing, for row building.
func conditionStatus(root any, typ string, fields ...string) string {
	for _, e := range model.NestedSlice(root, fields...) {
		if model.NestedString(e, "type") == typ {
			return model.NestedString(e, "status")
		}
	}
	return ""
}

func findCondition(conds []condition, typ string) (condition, bool) {
	for _, c := range conds {
		if c.Type == typ {
			return c, true
		}
	}
	return condition{}, false
}

// text renders a condition for the drawer: "True", "False (Reason)", "True (Reason): message".
func (c condition) text() string {
	s := c.Status
	if s == "" {
		s = textUnknown
	}
	if c.Reason != "" {
		s += " (" + c.Reason + ")"
	}
	if c.Message != "" {
		s += ": " + c.Message
	}
	return s
}

// conditionToken colours a condition. For most conditions True is good; for negative ones
// (pressure, failure, remaining content) True is the problem, so the caller passes the token a
// True status earns.
func conditionToken(status string, whenTrue, whenFalse model.Status) model.Status {
	switch status {
	case condTrue:
		return whenTrue
	case condFalse:
		return whenFalse
	}
	return model.StatusUnknown
}

// conditionSection renders a conditions list as a drawer section, or reports false when there
// are none.
func conditionSection(conds []condition, token func(condition) model.Status) (model.DetailSection, bool) {
	if len(conds) == 0 {
		return model.DetailSection{}, false
	}
	s := model.DetailSection{Title: "Conditions", Items: make([]model.DetailItem, 0, len(conds))}
	for _, c := range conds {
		s.Items = append(s.Items, model.DetailItem{Name: c.Type, Value: c.text(), Status: token(c)})
	}
	return s, true
}

// labelSelector renders a metav1.LabelSelector in the label-selector syntax: matchLabels as
// "key=value", then matchExpressions as "key in (a,b)", "key notin (a)", "key" and "!key".
func labelSelector(root any, fields ...string) []string {
	sel := model.NestedMap(root, fields...)
	if sel == nil {
		return nil
	}
	out := model.SortedPairs(model.NestedStringMap(sel, "matchLabels"))
	for _, e := range model.NestedSlice(sel, "matchExpressions") {
		key := model.NestedString(e, "key")
		if key == "" {
			continue
		}
		values := strings.Join(model.NestedStrings(e, "values"), ",")
		switch op := model.NestedString(e, "operator"); op {
		case "In":
			out = append(out, key+" in ("+values+")")
		case "NotIn":
			out = append(out, key+" notin ("+values+")")
		case "Exists":
			out = append(out, key)
		case "DoesNotExist":
			out = append(out, "!"+key)
		default:
			out = append(out, key+" "+op+" ("+values+")")
		}
	}
	return out
}

// podSpecImages returns the images of every container of a pod spec (init containers first, then
// app and ephemeral containers), without duplicates.
func podSpecImages(spec any) []string {
	out := appendImages(nil, model.NestedSlice(spec, fieldInitContainers))
	out = appendImages(out, model.NestedSlice(spec, "containers"))
	return appendImages(out, model.NestedSlice(spec, "ephemeralContainers"))
}

func appendImages(out []string, containers []any) []string {
	for _, c := range containers {
		out = appendUnique(out, model.NestedString(c, "image"))
	}
	return out
}

// detailItems collects drawer items, dropping those without a value: a field the object does not
// set is left out rather than shown empty.
type detailItems []model.DetailItem

func (d *detailItems) add(name, value string) {
	d.addStatus(name, value, model.StatusNone)
}

func (d *detailItems) addStatus(name, value string, status model.Status) {
	if value == "" {
		return
	}
	*d = append(*d, model.DetailItem{Name: name, Value: value, Status: status})
}

// sortedKeys returns a map's keys in order, for drawer sections built from maps (capacity).
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// singleLine folds a multi-line message into one line for a table cell.
func singleLine(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	return strings.Join(strings.Fields(s), " ")
}
