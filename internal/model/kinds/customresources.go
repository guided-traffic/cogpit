package kinds

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/util/jsonpath"

	"github.com/guided-traffic/cogpit/internal/model"
)

// A custom resource definition is hostile input: these limits keep a definition with absurd
// printer columns from costing more than a sensible one would.
const (
	maxPrinterColumns = 50   // printer columns of one version that Cogpit shows; the rest are ignored
	maxJSONPathLength = 512  // longer printer column paths are not evaluated
	maxFanOut         = 2    // wildcards, unions, slices and recursive descents in one path
	maxCellText       = 1024 // bytes of a printer column cell; longer values are cut with "…"
)

// Printer column types as additionalPrinterColumns declares them.
const (
	pcInteger = "integer"
	pcNumber  = "number"
	pcBoolean = "boolean"
	pcDate    = "date"
	pcString  = "string"
)

// creationTimestampPath is the printer column path that is the object's age.
const creationTimestampPath = ".metadata.creationTimestamp"

// fieldName is the "name" field of a definition's versions and printer columns.
const fieldName = "name"

// missingNumber is the text of a number printer column whose value the object does not set. A
// number cell without text shows its sort key (model.CellText), so an empty cell would read "0";
// kubectl prints nothing there.
const missingNumber = "-"

// Errors of ParseCRD. Match them with errors.Is.
var (
	// ErrNotEstablished: the API server does not serve the definition's kind (yet): its
	// Established condition is not True.
	ErrNotEstablished = errors.New("the definition is not established")
	// ErrNoServedVersion: no version of the definition is served.
	ErrNoServedVersion = errors.New("the definition serves no version")
	// ErrInvalidDefinition: the definition lacks a usable group, plural or kind.
	ErrInvalidDefinition = errors.New("the definition has no valid group, plural or kind")
)

var (
	dnsLabel     = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	dnsSubdomain = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)+$`)
	kindName     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)
)

// CustomKind is what a custom resource definition says about the kind it defines, as far as its
// view needs it: the version Cogpit shows, the names, the scope and that version's printer
// columns. ParseCRD reads it from a definition; Profile builds the kind's generic profile.
type CustomKind struct {
	Group      string
	Version    string // the highest served version by Kubernetes version priority
	Plural     string
	Kind       string
	Namespaced bool
	// Columns are the chosen version's additionalPrinterColumns of every priority, in the
	// definition's order (at most maxPrinterColumns).
	Columns []PrinterColumn
	// Names are the definition's singular name and short names (spec.names), the names kubectl
	// and the palette's jump accept for the kind; at most maxShortNames short names, each a DNS
	// label, others left out.
	Names model.KindNames
}

// maxShortNames caps the short names read from a definition: hostile content must not grow what
// every keystroke of the palette's jump compares.
const maxShortNames = 16

// PrinterColumn is one additionalPrinterColumns entry of a definition's version.
type PrinterColumn struct {
	Name     string
	Type     string // integer, number, boolean, date or string; anything else renders as string
	JSONPath string
	Priority int64 // 0: shown; above 0: hidden, as kubectl shows them only with -o wide
}

// Key returns the kind key of a custom resource kind: "<plural>.<group>"
// (docs/developer/architecture.md, Kind keys).
func (k CustomKind) Key() string { return k.Plural + "." + k.Group }

// Equal reports whether two kinds build the same profile, so that a definition update that
// changes nothing Cogpit shows (its status, its schema) keeps the profile.
func (k CustomKind) Equal(o CustomKind) bool {
	return k.Group == o.Group && k.Version == o.Version && k.Plural == o.Plural && k.Kind == o.Kind &&
		k.Namespaced == o.Namespaced && slices.Equal(k.Columns, o.Columns) &&
		k.Names.Singular == o.Names.Singular && slices.Equal(k.Names.ShortNames, o.Names.ShortNames)
}

// ParseCRD reads an apiextensions.k8s.io/v1 CustomResourceDefinition. It fails with
// ErrNotEstablished while the API server does not serve the kind, ErrNoServedVersion when no
// version is served, and ErrInvalidDefinition when the names do not form a kind key; it never
// panics on hostile content.
func ParseCRD(crd *unstructured.Unstructured) (k CustomKind, err error) {
	defer func() {
		if r := recover(); r != nil {
			k, err = CustomKind{}, fmt.Errorf("%w: %v", ErrInvalidDefinition, r)
		}
	}()
	return parseCRD(crd)
}

// parseCRD is ParseCRD without the recover, so that tests see a panic.
func parseCRD(crd *unstructured.Unstructured) (k CustomKind, err error) {
	if crd == nil {
		return CustomKind{}, ErrInvalidDefinition
	}
	spec := model.NestedMap(crd, "spec")
	k = CustomKind{
		Group:      model.NestedString(spec, "group"),
		Plural:     model.NestedString(spec, "names", "plural"),
		Kind:       model.NestedString(spec, "names", "kind"),
		Namespaced: model.NestedString(spec, idScope) == "Namespaced",
	}
	// Kubernetes validates these, so a definition that breaks them did not come from an API
	// server; a group without a dot would collide with built-in kind keys.
	if len(k.Group) > 253 || !dnsSubdomain.MatchString(k.Group) ||
		len(k.Plural) > 63 || !dnsLabel.MatchString(k.Plural) ||
		len(k.Kind) > 63 || !kindName.MatchString(k.Kind) {
		return CustomKind{}, ErrInvalidDefinition
	}
	if conditionStatus(crd, "Established", "status", "conditions") != condTrue {
		return CustomKind{}, ErrNotEstablished
	}
	v := servedVersion(spec)
	if v == nil {
		return CustomKind{}, ErrNoServedVersion
	}
	k.Version = model.NestedString(v, fieldName)
	k.Names = crdNames(spec)
	for _, c := range model.NestedSlice(v, "additionalPrinterColumns") {
		if len(k.Columns) == maxPrinterColumns {
			break
		}
		pc := PrinterColumn{
			Name:     model.NestedString(c, fieldName),
			Type:     model.NestedString(c, "type"),
			JSONPath: strings.TrimSpace(model.NestedString(c, "jsonPath")),
		}
		pc.Priority, _ = model.NestedInt64(c, "priority")
		if strings.TrimSpace(pc.Name) == "" || pc.JSONPath == "" {
			continue
		}
		k.Columns = append(k.Columns, pc)
	}
	return k, nil
}

// crdNames reads a definition's singular name and short names, each kept only when it is a DNS
// label, as Kubernetes validates them.
func crdNames(spec map[string]any) model.KindNames {
	var n model.KindNames
	if s := model.NestedString(spec, "names", "singular"); len(s) <= 63 && dnsLabel.MatchString(s) {
		n.Singular = s
	}
	for _, sn := range model.NestedStrings(spec, "names", "shortNames") {
		if len(n.ShortNames) == maxShortNames {
			break
		}
		if len(sn) <= 63 && dnsLabel.MatchString(sn) {
			n.ShortNames = append(n.ShortNames, sn)
		}
	}
	return n
}

// servedVersion returns the served version of highest Kubernetes version priority (v2 before v1
// before v1beta2 before v1alpha1), as discovery orders a group's versions, or nil.
func servedVersion(spec map[string]any) any {
	var best any
	bestName := ""
	for _, v := range model.NestedSlice(spec, "versions") {
		name := model.NestedString(v, fieldName)
		if served, _ := model.NestedBool(v, "served"); !served || len(name) > 63 || !dnsLabel.MatchString(name) {
			continue
		}
		if best == nil || version.CompareKubeAwareVersionStrings(name, bestName) > 0 {
			best, bestName = v, name
		}
	}
	return best
}

// ServedVersionName returns the name of the version a definition's view uses (the highest served
// version by Kubernetes version priority), or "" when none is served. Unlike ParseCRD it does
// not need the definition to be established.
func ServedVersionName(crd any) string {
	return model.NestedString(servedVersion(model.NestedMap(crd, "spec")), fieldName)
}

// Profile builds the generic profile of a custom resource kind: titled by its singular Kind name,
// with the columns Name, Namespace (namespaced kinds), the printer columns, and Age, which a
// printer column of priority 0 on .metadata.creationTimestamp replaces. Printer columns of a
// higher priority are hidden columns: kubectl shows them only with -o wide. The row's status
// comes from a Ready condition; the drawer shows every printer column, then the conditions.
func (k CustomKind) Profile() *model.KindProfile {
	ids := map[string]bool{nameColumn.ID: true, namespaceColumn.ID: true}
	ownAge := true
	for _, c := range k.Columns {
		if c.Priority == 0 && c.JSONPath == creationTimestampPath {
			ownAge = false
		}
	}
	if ownAge {
		ids[ageColumn.ID] = true
	}

	columns := []model.Column{nameColumn}
	if k.Namespaced {
		columns = append(columns, namespaceColumn)
	}
	printer := make([]*printerColumn, 0, len(k.Columns))
	for _, c := range k.Columns {
		pc := compilePrinterColumn(c, uniqueID(ids, c.Name))
		printer = append(printer, pc)
		columns = append(columns, pc.column)
	}
	if ownAge {
		columns = append(columns, ageColumn)
	}

	fields := make(map[string]fieldFunc, len(printer))
	for _, pc := range printer {
		if pc.typ == pcDate {
			continue // a timestamp is no value to search by
		}
		fields[pc.column.ID] = func(obj *unstructured.Unstructured) []string {
			return nonEmpty(pc.text(obj))
		}
	}

	namespaced := k.Namespaced
	return &model.KindProfile{
		Key: k.Key(), Group: k.Group, Version: k.Version, Resource: k.Plural, Kind: k.Kind,
		Title: k.Kind, Namespaced: k.Namespaced,
		Columns: columns,
		Cells: func(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
			cells := make([]model.Cell, 0, len(columns))
			cells = append(cells, nameCell(obj))
			if namespaced {
				cells = append(cells, namespaceCell(obj))
			}
			for _, pc := range printer {
				cells = append(cells, pc.cell(obj))
			}
			if ownAge {
				cells = append(cells, model.AgeCell(created(obj)))
			}
			return cells, readyStatus(obj)
		},
		Fields: fields,
		Detail: func(obj *unstructured.Unstructured) []model.DetailSection {
			return customDetail(k.Kind, printer, obj)
		},
	}
}

// readyStatus is a custom resource's status token, from its Ready condition (the convention of
// Flux, cert-manager, Crossplane and others): True is ok, False an error, Unknown pending; without
// a Ready condition the row has no status. Inferred: Lens's custom resource views were not seen
// colouring rows.
func readyStatus(obj any) model.Status {
	switch conditionStatus(obj, "Ready", "status", "conditions") {
	case condTrue:
		return model.StatusOK
	case condFalse:
		return model.StatusError
	case textUnknown:
		return model.StatusPending
	}
	return model.StatusNone
}

// negativeConditions are condition types whose True status is the problem. Every other condition
// of a custom resource reads True as good (inferred: a generic kind's polarity is not declared).
var negativeConditions = []string{"Stalled", "Degraded", "Failed", "Failure", "Error", "Reconciling", "Progressing"}

// customDetail is the drawer of a custom resource: every printer column with a value, then the
// conditions.
func customDetail(kind string, printer []*printerColumn, obj *unstructured.Unstructured) []model.DetailSection {
	var d detailItems
	for _, pc := range printer {
		if pc.age {
			continue // the metadata section shows when the object was created
		}
		d.add(pc.name, pc.detailText(obj))
	}
	var sections []model.DetailSection
	if len(d) > 0 {
		sections = append(sections, model.DetailSection{Title: kind, Items: d})
	}
	if s, ok := conditionSection(conditions(obj, "status", "conditions"), func(c condition) model.Status {
		switch {
		case c.Type == "Reconciling" || c.Type == "Progressing":
			return conditionToken(c.Status, model.StatusPending, model.StatusOK)
		case slices.Contains(negativeConditions, c.Type):
			return conditionToken(c.Status, model.StatusError, model.StatusOK)
		}
		return conditionToken(c.Status, model.StatusOK, model.StatusWarning)
	}); ok {
		sections = append(sections, s)
	}
	return sections
}

// printerColumn is a printer column with its path parsed once per profile. A jsonpath.JSONPath
// keeps evaluation state, so evaluations of one column are serialised; rows are built
// concurrently by views, commands and agents.
type printerColumn struct {
	name   string
	typ    string
	column model.Column
	age    bool // the column is the object's creation time

	mu   sync.Mutex
	path *jsonpath.JSONPath // nil when the path is not evaluated: it does not parse or is not allowed
}

func compilePrinterColumn(c PrinterColumn, id string) *printerColumn {
	pc := &printerColumn{name: strings.TrimSpace(c.Name), typ: c.Type}
	ct := model.ColumnText
	switch c.Type {
	case pcInteger, pcNumber:
		ct = model.ColumnNumber
	case pcDate:
		ct = model.ColumnAge
	case pcBoolean, pcString:
	default:
		pc.typ = pcString
	}
	pc.column = model.Column{ID: id, Title: pc.name, Type: ct, Hidden: c.Priority != 0}
	pc.age = c.JSONPath == creationTimestampPath
	pc.path = compileJSONPath(c.JSONPath)
	return pc
}

// compileJSONPath parses a printer column's path the way the API server does ("{" + path + "}")
// and refuses what a simple path never needs: a path that leaves the braces, the range and end
// keywords (they change the parser's state), and more than maxFanOut wildcards, unions, slices or
// recursive descents (each multiplies the results). It returns nil for a refused path.
func compileJSONPath(path string) (j *jsonpath.JSONPath) {
	defer func() {
		if recover() != nil {
			j = nil
		}
	}()
	if path == "" || len(path) > maxJSONPathLength || !utf8.ValidString(path) {
		return nil
	}
	text := "{" + path + "}"
	parsed, err := jsonpath.Parse("column", text)
	if err != nil || parsed == nil || parsed.Root == nil || len(parsed.Root.Nodes) != 1 {
		return nil
	}
	list, ok := parsed.Root.Nodes[0].(*jsonpath.ListNode)
	if !ok {
		return nil
	}
	fanOut := 0
	if !simplePath(list, &fanOut) {
		return nil
	}
	j = jsonpath.New("column").AllowMissingKeys(true)
	if err := j.Parse(text); err != nil {
		return nil
	}
	return j
}

// simplePath walks a parsed path: no identifiers (range, end), and at most maxFanOut nodes that
// multiply the results.
func simplePath(list *jsonpath.ListNode, fanOut *int) bool {
	if list == nil {
		return true
	}
	for _, n := range list.Nodes {
		switch node := n.(type) {
		case *jsonpath.IdentifierNode:
			return false
		case *jsonpath.ListNode:
			if !simplePath(node, fanOut) {
				return false
			}
		case *jsonpath.FilterNode:
			if !simplePath(node.Left, fanOut) || !simplePath(node.Right, fanOut) {
				return false
			}
		case *jsonpath.UnionNode:
			*fanOut++
			for _, l := range node.Nodes {
				if !simplePath(l, fanOut) {
					return false
				}
			}
		case *jsonpath.WildcardNode, *jsonpath.RecursiveNode:
			*fanOut++
		case *jsonpath.ArrayNode:
			if !node.Params[0].Known || node.Params[1].Known || node.Params[2].Known {
				*fanOut++ // a slice, not a single index
			}
		}
		if *fanOut > maxFanOut {
			return false
		}
	}
	return true
}

// value returns the first value the path finds, as the API server's table conversion takes it,
// and whether there is one. A null is no value.
func (pc *printerColumn) value(obj *unstructured.Unstructured) (v any, ok bool) {
	if pc.path == nil || obj == nil || obj.Object == nil {
		return nil, false
	}
	pc.mu.Lock()
	defer pc.mu.Unlock()
	defer func() {
		if recover() != nil {
			v, ok = nil, false
		}
	}()
	results, err := pc.path.FindResults(obj.Object)
	if err != nil || len(results) == 0 || len(results[0]) == 0 || !results[0][0].IsValid() || !results[0][0].CanInterface() {
		return nil, false
	}
	v = results[0][0].Interface()
	return v, v != nil
}

// cell renders the column's cell the way kubectl prints the API server's table: integers and
// numbers as numbers (without exponent), booleans as true or false, dates as an age, everything
// else as text, maps and lists as JSON. A value of the wrong type for the column is no value.
func (pc *printerColumn) cell(obj *unstructured.Unstructured) model.Cell {
	v, ok := pc.value(obj)
	switch pc.typ {
	case pcInteger:
		if n, isInt := integerValue(v); ok && isInt {
			return model.Cell{Text: strconv.FormatInt(n, 10), Num: n}
		}
		return model.Cell{Text: missingNumber}
	case pcNumber:
		if f, isNum := numberValue(v); ok && isNum {
			return model.Cell{Text: formatFloat(f), Num: roundClamp(f)}
		}
		return model.Cell{Text: missingNumber}
	case pcBoolean:
		if b, isBool := v.(bool); ok && isBool {
			return model.Cell{Text: strconv.FormatBool(b)}
		}
		return model.Cell{}
	case pcDate:
		s, isString := v.(string)
		if !ok || !isString || s == "" || s == "null" {
			return model.Cell{}
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return model.Cell{Text: "<invalid>"} // what kubectl prints for a date it cannot parse
		}
		return model.AgeCell(t)
	}
	if !ok {
		return model.Cell{}
	}
	return model.Cell{Text: cutText(singleLine(textValue(v)))}
}

// text is the column's value as a searchable field: the cell's text, or "" without a value.
func (pc *printerColumn) text(obj *unstructured.Unstructured) string {
	c := pc.cell(obj)
	if c.Text == missingNumber && (pc.typ == pcInteger || pc.typ == pcNumber) {
		return ""
	}
	return c.Text
}

// detailText is the column's value in the drawer: dates as absolute times (the drawer has no
// clock to render an age against), text with its line breaks.
func (pc *printerColumn) detailText(obj *unstructured.Unstructured) string {
	switch pc.typ {
	case pcDate:
		c := pc.cell(obj)
		if c.Num != 0 {
			return timestamp(time.Unix(c.Num, 0))
		}
		return c.Text
	case pcString:
		if v, ok := pc.value(obj); ok {
			return cutText(textValue(v))
		}
		return ""
	}
	return pc.text(obj)
}

// integerValue converts a value the way the API server does for an integer column: integers as
// they are, floats truncated.
func integerValue(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || n < math.MinInt64 || n >= math.MaxInt64 {
			return 0, false
		}
		return int64(n), true
	case float32:
		return integerValue(float64(n))
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i, true
		}
		f, err := n.Float64()
		if err != nil {
			return 0, false
		}
		return integerValue(f)
	}
	return model.NestedInt64(map[string]any{"v": v}, "v")
}

// numberValue converts a value for a number column.
func numberValue(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	if i, ok := model.NestedInt64(map[string]any{"v": v}, "v"); ok {
		return float64(i), true
	}
	return 0, false
}

// formatFloat renders a number without an exponent: 2.5, 1000000, 0.000001.
func formatFloat(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// roundClamp is a number's sort key.
func roundClamp(f float64) int64 {
	switch {
	case math.IsNaN(f):
		return 0
	case f >= math.MaxInt64:
		return math.MaxInt64
	case f <= math.MinInt64:
		return math.MinInt64
	}
	return int64(math.Round(f))
}

// textValue renders any value as text, as kubectl prints a string column: strings as they are,
// numbers without exponent, maps and lists as JSON.
func textValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return formatFloat(t)
	case float32:
		return formatFloat(float64(t))
	case json.Number:
		return t.String()
	case map[string]any, []any:
		b, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		return string(b)
	}
	if n, ok := model.NestedInt64(map[string]any{"v": v}, "v"); ok {
		return strconv.FormatInt(n, 10)
	}
	return fmt.Sprint(v)
}

// cutText bounds a cell's text: a printer column may point at a whole subtree.
func cutText(s string) string {
	if len(s) <= maxCellText {
		return s
	}
	cut := maxCellText
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// uniqueID returns a lowerCamelCase column id for a printer column's name that is not taken yet,
// and takes it: "Last Transition" → "lastTransition", "READY" → "ready", "Cluster IP" →
// "clusterIP"; a taken id gets a number ("age2"), a name without letters or digits becomes
// "column".
func uniqueID(taken map[string]bool, name string) string {
	base := lowerCamelID(name)
	id := base
	for n := 2; taken[id]; n++ {
		id = base + strconv.Itoa(n)
	}
	taken[id] = true
	return id
}

func lowerCamelID(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool {
		return r > unicode.MaxASCII || (!unicode.IsLetter(r) && !unicode.IsDigit(r))
	})
	if len(words) == 0 {
		return "column"
	}
	allCaps := strings.ToUpper(name) == name
	var b strings.Builder
	for i, w := range words {
		switch {
		case i == 0 && (allCaps || isUpper(w)):
			b.WriteString(strings.ToLower(w))
		case i == 0:
			b.WriteString(strings.ToLower(w[:1]) + w[1:])
		case allCaps:
			b.WriteString(strings.ToUpper(w[:1]) + strings.ToLower(w[1:]))
		default:
			b.WriteString(strings.ToUpper(w[:1]) + w[1:])
		}
	}
	id := b.String()
	if id[0] < 'a' || id[0] > 'z' {
		id = "c" + id // ids start with a lowercase letter: "2FA" → "c2FA"
	}
	if len(id) > 63 {
		id = id[:63]
	}
	return id
}

// isUpper reports whether a word has no lowercase letter ("IP", "TLS").
func isUpper(w string) bool { return strings.ToUpper(w) == w }
