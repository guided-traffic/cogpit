package model

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TitleProperties is the title of the drawer's first section. Lens merges an object's metadata
// and its kind's state into one Properties section (capture, 2026-10-11); a kind profile whose
// Detail returns a first section with this title has its items appended to the generic metadata
// items (see [MergeProperties]).
const TitleProperties = "Properties"

// DetailSection is one titled block of the detail drawer: name/value items, or a table.
//
// The JSON shape is part of MCP's object_get result; fields are only ever added, and every added
// field is optional:
//
//	{"title": "Capacity", "group": "", "items": [...], "table": {"columns": [...], "rows": [[...]]}}
type DetailSection struct {
	Title string `json:"title"`
	// Group, when set, is a heading shown once above consecutive sections of the same group:
	// "Init Containers" and "Containers" above one section per container, titled by its name.
	Group string       `json:"group,omitempty"`
	Items []DetailItem `json:"items,omitempty"`
	// Table, when set, is the section's content as a table: a node's Capacity and Allocatable.
	Table *DetailTable `json:"table,omitempty"`
	// Grade, when set, is the image grade of the container the section is about (the Trivy
	// integration, D-113): a shield beside the section's title and a card under its items.
	Grade *GradeCard `json:"grade,omitempty"`
	// Count, when set, is the number the section's head shows in place of its items and rows: an
	// Ingress's rules, whose table has a row per path.
	Count int `json:"count,omitempty"`
}

// The groups of a pod's container sections, one section per container titled by its name.
const (
	GroupInitContainers      = "Init Containers"
	GroupContainers          = "Containers"
	GroupEphemeralContainers = "Ephemeral Containers"
)

// DetailTable is a small table in the drawer: column titles and rows of cell texts. A cell's text
// may hold several lines, separated by "\n" as in an item's value, shown one under the other: a
// role's rules list their resources and verbs a line each.
//
// Rows is always the plain text, as Value is for an item. Types and Cells, when set, say how the
// drawer draws a table whose cells name objects or carry a status (a node's pods, a binding's
// subjects, an endpoint slice's endpoints): Types holds each column's type, Cells the rows as table
// cells, row for row and column for column. A cell with a link is drawn as a link, one with a
// status in its colour, as the lists draw them, and a cell of a warning column as Lens's warning
// icon with the cell's text as its tooltip; a warning column's title is not shown. Build such a
// table with [CellTable].
//
// A sortable table ([SortableCellTable]: the Pods of a node's drawer) also carries its columns'
// IDs, and Sort says the order its rows are in: the engine sorts them as the drawer's sort says
// (UIState.DrawerSort, ui.sortDrawerTable), and the drawer sorts by a click on a column's title.
type DetailTable struct {
	Columns []string     `json:"columns"`
	Rows    [][]string   `json:"rows"`
	Types   []ColumnType `json:"types,omitempty"`
	Cells   [][]Cell     `json:"cells,omitempty"`
	// IDs are the columns' ids ("name", "cpu"), set on a sortable table: what a sort names a
	// column by.
	IDs []string `json:"ids,omitempty"`
	// Sortable marks a table the drawer sorts by its column titles.
	Sortable bool `json:"sortable,omitempty"`
	// Sort is the order the rows are in, set by the engine on a sortable table; nil: the order
	// the kind's profile gave them.
	Sort *TableSort `json:"sort,omitempty"`
	// Omitted counts the rows the engine left out of a long sortable table: it shows the first rows
	// in Sort's order and counts the rest here (a workload's Pods, D-40).
	Omitted int `json:"omitted,omitempty"`
	// LastUnder shows the last column's texts on a line of their own under their rows, across the
	// table's width and wrapped there, where a column would cut them off: a release revision's
	// description, which holds Helm's error message. Rows and Cells keep the column like any other.
	LastUnder bool `json:"lastUnder,omitempty"`
}

// TableSort is the order of a sortable drawer table's rows: by the column with the id Column,
// ascending unless Desc.
type TableSort struct {
	Column string `json:"column"`
	Desc   bool   `json:"desc,omitempty"`
}

// SortableCellTable is [CellTable] for a table the drawer sorts by a click on a column's title (a
// node's Pods): it carries the columns' ids. Number, age, time, warning and metric columns sort
// by Cell.Num, the others by the text; empty cells (a pod without a measurement) sort last.
func SortableCellTable(columns []Column, rows [][]Cell) *DetailTable {
	t := CellTable(columns, rows)
	t.Sortable = true
	t.IDs = make([]string, len(columns))
	for i, c := range columns {
		t.IDs[i] = c.ID
	}
	return t
}

// ColumnIndex returns the index of the column with the id, or -1 (a table without ids has none).
func (t *DetailTable) ColumnIndex(id string) int {
	if t == nil {
		return -1
	}
	for i, c := range t.IDs {
		if c == id {
			return i
		}
	}
	return -1
}

// Link returns the object the cell at row and col names, or nil for a plain cell, a table without
// cells, or one out of range.
func (t *DetailTable) Link(row, col int) *ObjectRef {
	if t == nil || row < 0 || row >= len(t.Cells) || col < 0 || col >= len(t.Cells[row]) {
		return nil
	}
	return t.Cells[row][col].Link
}

// CellTable returns a drawer table of cells under columns: the titles and types of the columns,
// the cells, and Rows with each cell's text ([CellText]). A row with fewer cells than columns is
// padded with empty cells. Age and until columns need a clock, which a kind's Detail does not
// have: render times as text, absolute ones as marks ([MarkTime], [MarkDate]).
func CellTable(columns []Column, rows [][]Cell) *DetailTable {
	t := &DetailTable{
		Columns: make([]string, len(columns)), Types: make([]ColumnType, len(columns)),
		Rows: make([][]string, 0, len(rows)), Cells: make([][]Cell, 0, len(rows)),
	}
	for i, c := range columns {
		t.Columns[i], t.Types[i] = c.Title, c.Type
	}
	for _, r := range rows {
		cells := make([]Cell, len(columns))
		copy(cells, r)
		texts := make([]string, len(columns))
		for i, c := range cells {
			texts[i] = CellText(columns[i], c, time.Time{})
		}
		t.Rows = append(t.Rows, texts)
		t.Cells = append(t.Cells, cells)
	}
	return t
}

// WriteAges returns sections with the texts of their tables' relative times (ColumnAge,
// ColumnUntil) written against now. A kind's Detail has no clock (KindProfile.Detail), so a table
// of cells built there holds such a time in its cells (Cell.Num) and CellTable's text is not yet
// the one to show; the engine writes it when it builds the drawer, as it writes the marks
// (TimeFormat.WriteSections): a workload's pods' Age. Sections without such a column are shared,
// the others copied; the input is not modified.
func WriteAges(sections []DetailSection, now time.Time) []DetailSection {
	var out []DetailSection
	for i, sec := range sections {
		t := sec.Table
		if t == nil || t.Cells == nil || !slices.ContainsFunc(t.Types, relativeTime) {
			continue
		}
		if out == nil {
			out = append([]DetailSection(nil), sections...)
		}
		nt := *t
		nt.Rows = make([][]string, len(t.Rows))
		for r, row := range t.Rows {
			row = slices.Clone(row)
			for c, typ := range t.Types {
				if relativeTime(typ) && c < len(row) && r < len(t.Cells) && c < len(t.Cells[r]) {
					row[c] = CellText(Column{Type: typ}, t.Cells[r][c], now)
				}
			}
			nt.Rows[r] = row
		}
		out[i].Table = &nt
	}
	if out == nil {
		return sections
	}
	return out
}

// relativeTime reports whether a column's cells are written relative to now.
func relativeTime(typ ColumnType) bool { return typ == ColumnAge || typ == ColumnUntil }

// DetailItem is one line of a detail section: a name and a value.
//
// Value is always the plain text, lines separated by "\n" for lists, so that agents and plain
// renderers need nothing else. The other fields say how the drawer shows it, as Lens does
// (capture, 2026-10-11):
//
//   - Status colours the value (a status pill).
//   - Link is the object the value names (a node, a controller, a service account); the drawer
//     renders the value as a link that goes to it (ui.navigate; an empty Name opens the kind's
//     list view).
//   - Chips, when set, are the value's entries shown as chips, each with its own status and link
//     (labels, pod IPs, conditions, secrets).
//   - Fold, when set, folds the list behind its count: the drawer shows "<n> <Fold>" ("6 Labels",
//     "1 Label" for one entry) and expands it on click. The entries are the chips, else the lines
//     of Value.
//   - Actions are buttons beside the value that run a command for it: "Forward…" beside a
//     container port. An action marked Link is the value itself: the value is a link that runs
//     it (a container port forwards at once and opens the browser).
//   - Private marks a value agents must not read (Helm's user-supplied values, which can hold
//     credentials): object_get gives agents [Withheld] in its place (WithholdPrivate), the user's
//     own window shows it. It is not part of the JSON.
type DetailItem struct {
	Name    string       `json:"name,omitempty"`
	Value   string       `json:"value"`
	Status  Status       `json:"status,omitempty"`
	Link    *ObjectRef   `json:"link,omitempty"`
	Chips   []Chip       `json:"chips,omitempty"`
	Fold    string       `json:"fold,omitempty"`
	Actions []ItemAction `json:"actions,omitempty"`
	// Pill draws the value as a neutral grey pill, carrying no status colour (a Helm release's
	// Status in Lens's drawer, captured 2026-10-12).
	Pill bool `json:"pill,omitempty"`
	// Wrap breaks a plain value onto lines within the value's width where it is cut off
	// otherwise: a release's description, which holds Helm's error message.
	Wrap    bool `json:"wrap,omitempty"`
	Private bool `json:"-"`
}

// Withheld is what agents read in place of a private item's value.
const Withheld = "<withheld: Cogpit shows this value only in its own window>"

// WithholdPrivate returns sections with every private item's value replaced by [Withheld], its
// chips, link and actions dropped: what an agent may see of a drawer. Sections without a private
// item are shared, the others copied; the input is not modified.
func WithholdPrivate(sections []DetailSection) []DetailSection {
	var out []DetailSection
	for i, sec := range sections {
		private := false
		for _, it := range sec.Items {
			private = private || it.Private
		}
		if !private {
			continue
		}
		if out == nil {
			out = append([]DetailSection(nil), sections...)
		}
		items := make([]DetailItem, len(sec.Items))
		for j, it := range sec.Items {
			if it.Private {
				it = DetailItem{Name: it.Name, Value: Withheld, Private: true}
			}
			items[j] = it
		}
		out[i].Items = items
	}
	if out == nil {
		return sections
	}
	return out
}

// ItemAction is a button beside a drawer item's value (architecture §4.4): it runs the command
// Command with Params, which name everything the command needs from the item (the pod, the
// container, the port). Agents see it in object_get and can dispatch the same command. The GUI
// runs it as the row menu runs a command (a schema-generated dialog when the command needs more
// parameters, prefilled with Params), unless the GUI registers a dialog of its own for the
// command (the port-forward dialog).
type ItemAction struct {
	Label   string          `json:"label"`
	Command string          `json:"command"`
	Params  json.RawMessage `json:"params,omitempty"`
	// Link makes the action the item's value itself instead of a button beside it: the drawer
	// renders the value as a link that runs the action; Label says what it does, for agents. At
	// most one action of an item is a link.
	Link bool `json:"link,omitempty"`
}

// Chip is one entry of a list value shown as a chip.
type Chip struct {
	Text   string     `json:"text"`
	Status Status     `json:"status,omitempty"`
	Link   *ObjectRef `json:"link,omitempty"`
}

// Entries returns how many entries a list value has: its chips, else its lines.
func (it DetailItem) Entries() int {
	if len(it.Chips) > 0 {
		return len(it.Chips)
	}
	if it.Value == "" {
		return 0
	}
	return strings.Count(it.Value, "\n") + 1
}

// ChipsItem returns an item whose value is a list of chips; Value is their texts, one per line.
// An empty list returns an item with an empty value, which detail builders drop.
func ChipsItem(name string, chips []Chip) DetailItem {
	texts := make([]string, len(chips))
	for i, c := range chips {
		texts[i] = c.Text
	}
	return DetailItem{Name: name, Value: strings.Join(texts, "\n"), Chips: chips}
}

// RefFunc links a reference (an owner reference) to the object it names, as [RefTo] does, or
// returns nil.
type RefFunc func(apiVersion, kind, namespace, name string) *ObjectRef

// PropertiesSection returns the generic part of the drawer's first section, the metadata every
// kind shows: Created, Name, Namespace (a link), Labels (folded chips), Annotations (folded) and
// Controlled By (a link for built-in owner kinds). Lens shows no UID (capture, 2026-10-11).
// Created is the age against now, then the time as a mark ([MarkTime]) that the engine writes in
// the user's zone and format (D-31): "5m ago (10.09.2026 17:41:01)".
func PropertiesSection(obj *unstructured.Unstructured, now time.Time) DetailSection {
	return PropertiesSectionLinked(obj, now, RefTo)
}

// PropertiesSectionLinked is PropertiesSection with the owners linked by link: the engine's
// drawer links custom kinds too, through their definitions (kinds.RefToWith).
func PropertiesSectionLinked(obj *unstructured.Unstructured, now time.Time, link RefFunc) DetailSection {
	if link == nil {
		link = RefTo
	}
	s := DetailSection{Title: TitleProperties}
	if ts := obj.GetCreationTimestamp(); !ts.IsZero() {
		s.Items = append(s.Items, DetailItem{
			Name:  "Created",
			Value: FormatAge(now.Sub(ts.Time)) + " ago (" + MarkTime(ts.Time) + ")",
		})
	}
	s.Items = append(s.Items, DetailItem{Name: "Name", Value: obj.GetName()})
	if ns := obj.GetNamespace(); ns != "" {
		s.Items = append(s.Items, DetailItem{Name: "Namespace", Value: ns, Link: &ObjectRef{Kind: "namespaces", Name: ns}})
	}
	if labels := SortedPairs(obj.GetLabels()); len(labels) > 0 {
		chips := make([]Chip, len(labels))
		for i, l := range labels {
			chips[i] = Chip{Text: l}
		}
		it := ChipsItem("Labels", chips)
		it.Fold = "Labels"
		s.Items = append(s.Items, it)
	}
	if ann := obj.GetAnnotations(); len(ann) > 0 {
		filtered := make(map[string]string, len(ann))
		for k, v := range ann {
			if k == "kubectl.kubernetes.io/last-applied-configuration" {
				continue // shown in the YAML; too long for the drawer, as in Lens
			}
			filtered[k] = v
		}
		if pairs := SortedPairs(filtered); len(pairs) > 0 {
			s.Items = append(s.Items, DetailItem{Name: "Annotations", Value: strings.Join(pairs, "\n"), Fold: "Annotations"})
		}
	}
	for _, o := range obj.GetOwnerReferences() {
		s.Items = append(s.Items, DetailItem{
			Name: "Controlled By", Value: o.Kind + " " + o.Name,
			Link: link(o.APIVersion, o.Kind, obj.GetNamespace(), o.Name),
		})
	}
	return s
}

// MergeProperties returns the drawer's sections: props first, with the items of the kind's own
// Properties section (a first section titled [TitleProperties]) appended, then the kind's other
// sections in their order.
func MergeProperties(props DetailSection, kind []DetailSection) []DetailSection {
	out := make([]DetailSection, 0, 1+len(kind))
	if len(kind) > 0 && kind[0].Title == TitleProperties {
		props.Items = append(props.Items, kind[0].Items...)
		kind = kind[1:]
	}
	out = append(out, props)
	return append(out, kind...)
}
