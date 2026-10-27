package model

import (
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
	// ColumnWarning is Lens's warning-icon column right after Name. A cell with a Status shows an
	// icon in that status's colour and its Text (the reason) as the icon's tooltip; an empty cell
	// shows nothing. Num is 1 when the row has a warning, so that the column sorts warnings
	// together; [CellText] returns the reason.
	ColumnWarning ColumnType = "warning"
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
	// ColumnGrade is a column of image grades drawn as shields (a pod's worst image grade, the Trivy
	// integration, D-113): Cell.Grade is the shield, Cell.Text its readable form ("D · 2
	// Critical") for search, CSV and agents, Cell.Tip the counts behind it, and the column sorts by
	// Cell.Num ([GradeRank]). Like the warning column it has no title in the header and a fixed
	// width; Title names it for agents and the columns menu. A cell without a grade is empty.
	ColumnGrade ColumnType = "grade"
)

// Column describes one table column of a kind.
type Column struct {
	ID     string     `json:"id"`              // stable id, lowerCamelCase: "name", "clusterIP"
	Title  string     `json:"title"`           // header text as Lens shows it: "Cluster IP"
	Type   ColumnType `json:"type"`            // sort and render behaviour
	Width  float32    `json:"width,omitempty"` // default width hint in points; 0 lets the UI decide
	Hidden bool       `json:"hidden,omitempty"`
}

// Cell is one table cell. Text is what the user reads; Num is the sort key for number, age and
// warning columns. Status colours the cell itself (a Status or Conditions column), independent of
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
