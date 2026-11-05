package model

import (
	"slices"
	"strconv"
	"strings"
	"time"
)

// ColumnType says how a column's cells sort and render.
type ColumnType string

// Column types.
const (
	ColumnText   ColumnType = "text"   // sorts by Cell.Text, case-insensitively
	ColumnNumber ColumnType = "number" // sorts by Cell.Num
	ColumnAge    ColumnType = "age"    // Cell.Num is a Unix timestamp in seconds; rendered as an age relative to now
	// ColumnMetric is a CPU or Memory usage column (KindProfile.Usage): Cell.Text is the formatted
	// usage ("0.012", "19.5 MiB", "7.5%"), Cell.Num its sort key. A cell without a Text has no
	// measurement (no metrics-server, or none for this object yet) and shows nothing, not "0".
	ColumnMetric ColumnType = "metric"
	// ColumnSignals is the Signals column right after Name (D-150): every mark on an object
	// in one cell, an icon per source side by side (Cell.Signals; the object's own state first,
	// then the integrations' in Cluster Settings' order: Trivy's image grade, Kyverno's policy
	// violations), each in its slot
	// of the column (Column.Slots) so that it keeps its place in a view. Text is the signals' texts
	// joined ([SignalsCell]) for search, CSV and agents; the column sorts by Num, the worst
	// signal's rank. An empty cell shows nothing.
	ColumnSignals ColumnType = "signals"
	// ColumnUntil is a time in the future: Cell.Num is a Unix timestamp in seconds, rendered as the
	// time left from now ([FormatUntil]): a Cron Job's Next execution.
	ColumnUntil ColumnType = "until"
	// ColumnTime is an absolute time: Cell.Num is a Unix timestamp in seconds, written as a date
	// and a time of day in the user's zone and format when the cell is drawn
	// ([TimeFormat.CellText], D-31): an event's Last Seen. Without a format it is written in ISO
	// 8601 in now's zone ([CellText]).
	ColumnTime ColumnType = "time"
	// ColumnIcon is a column of small images without a title, as Lens's Charts view draws a
	// chart's icon: Cell.Image is the image's URL, which the window fetches only while it shows the
	// cell and the user allows it (D-46), drawing a glyph of its own otherwise. Its cells have no
	// text, so that searching, sorting and copying ignore it.
	ColumnIcon ColumnType = "icon"
	// ColumnGrade is a column of image grades drawn as shields: Cell.Grade is the shield, Cell.Text
	// its readable form ("D · 2 Critical") for search, CSV and agents, Cell.Tip the counts behind
	// it, and the column sorts by Cell.Num ([GradeRank]). It has no title in the header and a fixed
	// width; Title names it for agents and the columns menu. A cell without a grade is empty. A
	// pod's worst image grade is a signal in the Signals column (D-113); no built-in view has a
	// grade column of its own.
	ColumnGrade ColumnType = "grade"
)

// What the pairs of a column of key=value pairs are (Column.Pairs).
const (
	PairsLabels   = "labels"
	PairsSelector = "selector"
)

// Column describes one table column of a kind.
type Column struct {
	ID     string     `json:"id"`              // stable id, lowerCamelCase: "name", "clusterIP"
	Title  string     `json:"title"`           // header text as Lens shows it: "Cluster IP"
	Type   ColumnType `json:"type"`            // sort and render behaviour
	Width  float32    `json:"width,omitempty"` // default width hint in points; 0 lets the UI decide
	Hidden bool       `json:"hidden,omitempty"`
	// Pairs, when set, says the column lists key=value pairs of one kind: PairsLabels, an object's
	// labels, or PairsSelector, a label selector. Preferences › Tables › Free width's exceptions
	// match it (D-153).
	Pairs string `json:"pairs,omitempty"`
	// Slots is a Signals column's number of icon places, one per source that applies to the view
	// (Signal.Slot); 0 for any other column.
	Slots int `json:"slots,omitempty"`
}

// Cell is one table cell. Text is what the user reads; Num is the sort key for number, age and
// Signals columns. Status colours the cell itself (a Status or Conditions column), independent of
// the row's status.
//
// Link, when set, is the object the cell names (a Namespace, Node, Controlled By or Involved
// Object cell), and the UI renders the cell as a link. Its Kind is a kind key; an empty Context
// means the row's context. A link to a namespace in a list's Namespace column narrows the open
// view's namespace filter to it (ui.setNamespace, owner 2026-10-11); other links go to the object
// (ui.navigate); a link with an empty Name opens the list view of its kind (ui.openView), as a
// definition's Resource cell does. Rows are shared and immutable, and so are their links.
//
// Parts, when set, colours the cell in runs, each in its own status colour, drawn in order with a
// space between them: a cordoned node's Conditions read "SchedulingDisabled Ready" with only
// "Ready" green. Text is then the parts' texts joined by single spaces, so that sorting, search and
// CellText work on the plain text, and Status is the cell's overall status. Parts is a pointer so
// that Cell stays comparable; like links, parts are shared and immutable. Build it with
// [PartsCell].
type Cell struct {
	Text   string      `json:"text,omitempty"`
	Num    int64       `json:"num,omitempty"`
	Status Status      `json:"status,omitempty"`
	Link   *ObjectRef  `json:"link,omitempty"`
	Parts  *[]CellPart `json:"parts,omitempty"`
	// Marks, when set, draws the cell as a row of small squares, one per mark in its status colour:
	// a pod's Containers cell, one square per container, init containers first (Lens; owner
	// 2026-10-12). Text stays the readable form ("1/2") for sorting, search, CSV and agents; each
	// mark's Text says what it stands for, and the cell's tooltip lists them. A pointer, so that
	// Cell stays comparable; shared and immutable like Parts.
	Marks *[]CellMark `json:"marks,omitempty"`
	// Image is the URL of the image a ColumnIcon cell draws (a chart's icon); empty: the column's
	// glyph.
	Image string `json:"image,omitempty"`
	// Bar, when set, draws the cell as a usage bar beside its Text: a node's CPU, Memory and Disk
	// as a share of what the node has (Lens's usage bars in the Nodes list). Text stays the
	// readable share ("7.5%") for sorting, search, CSV and agents. A pointer, so that Cell stays
	// comparable; shared and immutable like Parts.
	Bar *CellBar `json:"bar,omitempty"`
	// Tip is the cell's tooltip, a line or a few: a kubelet's upstream support and version skew in
	// the Nodes list (D-91). Agents read it beside the cell's text (view_rows, resources_list).
	Tip string `json:"tip,omitempty"`
	// Grade is the image grade a ColumnGrade cell draws as its shield; GradeNone draws nothing.
	Grade ImageGrade `json:"grade,omitempty"`
	// Signals, when set, are a Signals cell's icons, in slot order (ColumnSignals). A pointer, so
	// that Cell stays comparable; shared and immutable like Parts. Build it with [SignalsCell].
	Signals *[]Signal `json:"signals,omitempty"`
	// Web, when set, draws the cell as a list of web addresses separated by [WebSeparator], each
	// one a link that opens its URL in the browser on the user's click; an entry without a URL is
	// plain text (an Ingress's wildcard host). Text stays the entries' texts joined, for sorting,
	// search, CSV and agents, who read the URLs beside it. A pointer, so that Cell stays
	// comparable; shared and immutable like Parts. Build it with [WebCell].
	Web *[]WebLink `json:"web,omitempty"`
}

// WebLink is one entry of a cell drawn as web addresses: Text is what the cell shows, URL the
// absolute http or https URL a click opens; empty, the entry is no link.
type WebLink struct {
	Text string `json:"text"`
	URL  string `json:"url,omitempty"`
}

// WebSeparator stands between the entries of a cell of web addresses.
const WebSeparator = ", "

// WebCell returns a cell that lists web addresses; no entries return an empty cell.
func WebCell(links []WebLink) Cell {
	if len(links) == 0 {
		return Cell{}
	}
	texts := make([]string, len(links))
	for i, l := range links {
		texts[i] = l.Text
	}
	return Cell{Text: strings.Join(texts, WebSeparator), Web: &links}
}

// CellBar is a usage bar: Share is the part of the whole in use (0 to 1; more is drawn full),
// Status the bar's colour at the thresholds ([UsageBarStatus]: none below the warning share, then
// warning, then error), Detail the amounts behind the share ("0.30 of 4 cores"), the cell's
// tooltip.
type CellBar struct {
	Share  float64 `json:"share"`
	Status Status  `json:"status,omitempty"`
	Detail string  `json:"detail,omitempty"`
}

// CellMark is one square of a cell drawn in marks. Hollow draws it as an outline in its status
// colour instead of filled: an ephemeral container that has finished, which stays in the pod's
// status as long as the pod exists (D-102).
type CellMark struct {
	Text   string `json:"text"`
	Status Status `json:"status,omitempty"`
	Hollow bool   `json:"hollow,omitempty"`
}

// CellPart is one run of a cell's text in its own status colour; StatusNone draws it in the
// foreground colour.
type CellPart struct {
	Text   string `json:"text"`
	Status Status `json:"status,omitempty"`
}

// PartsCell returns a cell drawn in coloured parts, with status as the cell's overall status.
// Empty parts are dropped; a single part needs no parts and becomes a plain cell with that part's
// text.
func PartsCell(status Status, parts ...CellPart) Cell {
	kept := parts[:0:0]
	for _, p := range parts {
		if p.Text != "" {
			kept = append(kept, p)
		}
	}
	switch len(kept) {
	case 0:
		return Cell{Status: status}
	case 1:
		return Cell{Text: kept[0].Text, Status: status}
	}
	n := len(kept) - 1
	for _, p := range kept {
		n += len(p.Text)
	}
	var b strings.Builder
	b.Grow(n)
	for i, p := range kept {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(p.Text)
	}
	return Cell{Text: b.String(), Status: status, Parts: &kept}
}

// The Signals column's id and title, in every view that has one.
const (
	SignalsColumnID    = "signals"
	SignalsColumnTitle = "Signals"
)

// SignalSource is where a signal comes from; each source has its glyph and its slot.
type SignalSource string

// The signal sources, in their slots' order: the state's first, then the version check's (a
// node's), then the integrations' in the order of engine.Integrations, as the overlays give them
// their slots.
const (
	// SignalState is the object's own state (a pod's containers not ready, a node's pressure): a
	// triangle in the status's colour.
	SignalState SignalSource = "state"
	// SignalVersion is Cogpit's own check of a node's kubelet against its API server and upstream
	// support (D-91): an exclamation mark in the status's colour.
	SignalVersion SignalSource = "version"
	// SignalTrivy is the Trivy integration's worst image grade of a pod: the grade's shield, an N/A
	// one dimmed.
	SignalTrivy SignalSource = "trivy"
	// SignalKyverno is the Kyverno integration's policy violations: Kyverno's glyph.
	SignalKyverno SignalSource = "kyverno"
)

// Signal is one icon of a Signals cell: Text says what it marks ("Containers not ready: web",
// "Kyverno: 1 policy violation (require-team-label)", "D · 2 Critical"), Tip what is behind it,
// Status its colour (Grade a shield instead), Section the drawer section that tells more (a
// click opens the drawer there; empty: the drawer's top), Slot its place in the column, and Rank
// its sort key ([SignalRank]).
type Signal struct {
	Source  SignalSource `json:"source"`
	Slot    int          `json:"slot,omitempty"`
	Text    string       `json:"text"`
	Tip     string       `json:"tip,omitempty"`
	Status  Status       `json:"status,omitempty"`
	Grade   ImageGrade   `json:"grade,omitempty"`
	Section string       `json:"section,omitempty"`
	Rank    int64        `json:"rank,omitempty"`
}

// signalSeverityStep separates the severities in SignalRank: above any GradeRank.
const signalSeverityStep = 1_000_000_000

// SignalRank is a signal's sort key: its severity first (an error above a warning above anything
// else), then within a source its own order (a grade's GradeRank). A grade's severity: D and E an
// error, C a warning, S, A and B none above the rest, N/A the lowest.
func SignalRank(status Status, grade ImageGrade, fine int64) int64 {
	sev := int64(1)
	switch {
	case grade == GradeNA:
		sev = 0
	case grade >= GradeD, grade == GradeNone && status == StatusError:
		sev = 3
	case grade == GradeC, grade == GradeNone && status == StatusWarning:
		sev = 2
	}
	return sev*signalSeverityStep + fine
}

// SignalsCell returns a Signals cell of signals, sorted by slot: Text their texts joined by "; ",
// Tip their tooltips a line each (what agents read beside the text; the window shows each
// signal's own), Status the worst of them, Num the highest rank. No signals return an empty cell.
func SignalsCell(signals []Signal) Cell {
	if len(signals) == 0 {
		return Cell{}
	}
	sorted := slices.Clone(signals)
	slices.SortStableFunc(sorted, func(a, b Signal) int { return a.Slot - b.Slot })
	texts := make([]string, 0, len(sorted))
	var tips []string
	var c Cell
	for _, s := range sorted {
		if s.Text != "" {
			texts = append(texts, s.Text)
		}
		if s.Tip != "" {
			tips = append(tips, s.Tip)
		}
		if statusSeverity(s.Status) > statusSeverity(c.Status) {
			c.Status = s.Status
		}
		c.Num = max(c.Num, s.Rank)
	}
	c.Text, c.Tip = strings.Join(texts, "; "), strings.Join(tips, "\n")
	c.Signals = &sorted
	return c
}

// statusSeverity orders the statuses a Signals cell keeps the worst of.
func statusSeverity(s Status) int {
	switch s {
	case StatusError:
		return 3
	case StatusWarning:
		return 2
	case StatusNone:
		return 0
	}
	return 1
}

// Row is one object as a table row. Rows are immutable once built: snapshots share them.
type Row struct {
	Ref             ObjectRef `json:"ref"`
	UID             string    `json:"uid,omitempty"`
	ResourceVersion string    `json:"resourceVersion,omitempty"`
	Cells           []Cell    `json:"cells"`
	Status          Status    `json:"status,omitempty"`
}

// CellText returns the text a cell shows. Age cells are formatted relative to now, so a row built
// minutes ago still shows the right age; absolute times in ISO 8601 in now's zone (what the
// window shows is [TimeFormat.CellText]'s, in the user's format).
func CellText(col Column, cell Cell, now time.Time) string {
	switch col.Type {
	case ColumnAge:
		if cell.Num == 0 {
			return cell.Text
		}
		return FormatAge(now.Sub(time.Unix(cell.Num, 0)))
	case ColumnUntil:
		if cell.Num == 0 {
			return cell.Text
		}
		return FormatUntil(time.Unix(cell.Num, 0).Sub(now))
	case ColumnTime:
		if cell.Num == 0 {
			return cell.Text
		}
		return TimeFormat{Zone: now.Location()}.Format(time.Unix(cell.Num, 0))
	case ColumnNumber:
		if cell.Text == "" {
			return strconv.FormatInt(cell.Num, 10)
		}
	}
	return cell.Text
}

// FormatAge formats a duration the way Lens's Age column does: the largest unit, "42s", "5m",
// "3h", "12d", "2y".
func FormatAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d/time.Second)) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	case d < 365*24*time.Hour:
		return strconv.Itoa(int(d/(24*time.Hour))) + "d"
	default:
		return strconv.Itoa(int(d/(365*24*time.Hour))) + "y"
	}
}
