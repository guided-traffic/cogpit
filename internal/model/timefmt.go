package model

import (
	"strings"
	"sync"
	"time"
	"unicode"
)

// Absolute times as the user reads them (D-31, owner 2026-10-13): every timestamp Cogpit shows is
// written in the Preferences' time zone (the computer's by default, summer and winter time
// included) and in the date and time format of the computer's region, "10.09.2026 17:41:01" in
// Germany, "9/10/2026 5:41:01 PM" in the US. The layout comes from the operating system once at
// start (internal/locale) and is kept so that a format preference can replace it later; the zone
// comes from the Preferences whenever they change. Ages ("5m", "160d") stay as they are; what
// Cogpit writes for machines (its files, the structured times of MCP results, an object's YAML)
// stays RFC 3339.

// The ISO 8601 layouts: the zero TimeLayout's, where the region's format is unknown.
const (
	DefaultDateLayout = "2006-01-02"
	DefaultTimeLayout = "15:04:05"
)

// TimeLayout is how absolute times are written: Go layouts (package time) of a date with day,
// month and four-digit year, "02.01.2006", and of a time of day with seconds, "15:04:05" or
// "3:04:05 PM". A time is written as its date, one space and its time of day. An empty field is
// the ISO 8601 default; the zero value writes "2006-01-02 15:04:05".
type TimeLayout struct {
	Date string `json:"date"`
	Time string `json:"time"`
}

// OrDefault returns the layout with ISO 8601's in place of an empty field.
func (l TimeLayout) OrDefault() TimeLayout {
	if l.Date == "" {
		l.Date = DefaultDateLayout
	}
	if l.Time == "" {
		l.Time = DefaultTimeLayout
	}
	return l
}

// In returns the layout in a time zone.
func (l TimeLayout) In(zone *time.Location) TimeFormat { return TimeFormat{Layout: l, Zone: zone} }

// DateTimeLayouts are the layouts a typed date with or without a time of day is read in: the
// date and time with seconds, without seconds, and the date alone.
func (l TimeLayout) DateTimeLayouts() []string {
	l = l.OrDefault()
	return []string{l.Date + " " + l.Time, l.Date + " " + derive(l.Time, deriveClock), l.Date}
}

// TimeOfDayLayouts are the layouts a typed time of day is read in: with seconds and without.
func (l TimeLayout) TimeOfDayLayouts() []string {
	l = l.OrDefault()
	return []string{l.Time, derive(l.Time, deriveClock)}
}

// TimeFormat writes absolute times for the user: a TimeLayout in a time zone (nil: UTC). The
// zero time is written as "".
type TimeFormat struct {
	Layout TimeLayout
	Zone   *time.Location
}

func (f TimeFormat) in(t time.Time) time.Time {
	if f.Zone == nil {
		return t.UTC()
	}
	return t.In(f.Zone)
}

func (f TimeFormat) write(t time.Time, layout string) string {
	if t.IsZero() {
		return ""
	}
	return f.in(t).Format(layout)
}

// Format writes a date and a time of day: "10.09.2026 17:41:01".
func (f TimeFormat) Format(t time.Time) string {
	l := f.Layout.OrDefault()
	return f.write(t, l.Date+" "+l.Time)
}

// FormatDate writes the date alone: "10.09.2026".
func (f TimeFormat) FormatDate(t time.Time) string { return f.write(t, f.Layout.OrDefault().Date) }

// FormatTime writes the time of day alone, with seconds: "17:41:01".
func (f TimeFormat) FormatTime(t time.Time) string { return f.write(t, f.Layout.OrDefault().Time) }

// FormatClock writes the time of day without seconds: "17:41", "5:41 PM" (a chart's axis).
func (f TimeFormat) FormatClock(t time.Time) string {
	return f.write(t, derive(f.Layout.OrDefault().Time, deriveClock))
}

// FormatDay writes the day and month without the year: "10.09.", "9/10" (a chart's axis).
func (f TimeFormat) FormatDay(t time.Time) string {
	return f.write(t, derive(f.Layout.OrDefault().Date, deriveDay))
}

// FormatPrecise writes a date and a time of day with milliseconds: "10.09.2026 17:41:01.123"
// (log lines, which several can share a second).
func (f TimeFormat) FormatPrecise(t time.Time) string {
	l := f.Layout.OrDefault()
	return f.write(t, l.Date+" "+derive(l.Time, derivePrecise))
}

// CellText is [CellText] with absolute times (ColumnTime) written in this format; ages and the
// other columns are written as CellText writes them.
func (f TimeFormat) CellText(col Column, cell Cell, now time.Time) string {
	if col.Type == ColumnTime && cell.Num != 0 {
		return f.Format(time.Unix(cell.Num, 0))
	}
	return CellText(col, cell, now)
}

// Marks. A kind's Detail has neither a clock nor the user's format (KindProfile.Detail takes the
// object alone), so it writes an absolute time as a mark, MarkTime(t), which the engine replaces
// with the time in the user's zone and format when it builds the drawer (WriteSections). A mark is
// the time in RFC 3339 between Unicode's interlinear annotation anchor and terminator, characters
// meant for text that is processed before it is shown; a mark that does not hold a time is left
// as it is.
const (
	markStart = '\uFFF9'
	markEnd   = '\uFFFB'
	markDate  = 'D' // after markStart: the date alone
)

// MarkTime returns the mark of an absolute time, written as a date and a time of day; "" for the
// zero time.
func MarkTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return string(markStart) + t.UTC().Format(time.RFC3339Nano) + string(markEnd)
}

// MarkDate returns the mark of a date, written as the date alone; "" for the zero time.
func MarkDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return string(markStart) + string(markDate) + t.UTC().Format(time.RFC3339Nano) + string(markEnd)
}

// HasMarks reports whether s holds a mark.
func HasMarks(s string) bool { return strings.ContainsRune(s, markStart) }

// WriteMarks returns s with every mark written in this format.
func (f TimeFormat) WriteMarks(s string) string {
	if !HasMarks(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for {
		i := strings.IndexRune(s, markStart)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		s = s[i+len(string(markStart)):]
		j := strings.IndexRune(s, markEnd)
		if j < 0 {
			b.WriteRune(markStart)
			continue
		}
		inner, date := s[:j], false
		if strings.HasPrefix(inner, string(markDate)) {
			inner, date = inner[1:], true
		}
		t, err := time.Parse(time.RFC3339Nano, inner)
		switch {
		case err != nil:
			b.WriteRune(markStart) // not a mark of Cogpit's: shown as it came
			continue
		case date:
			b.WriteString(f.FormatDate(t))
		default:
			b.WriteString(f.Format(t))
		}
		s = s[j+len(string(markEnd)):]
	}
}

// WriteSections returns sections with the marks in their items' values, their chips and their
// tables written in this format. Sections without a mark are shared, the others copied; the input
// is not modified.
func (f TimeFormat) WriteSections(sections []DetailSection) []DetailSection {
	var out []DetailSection
	for i, sec := range sections {
		if !sectionHasMarks(sec) {
			continue
		}
		if out == nil {
			out = append([]DetailSection(nil), sections...)
		}
		out[i] = f.writeSection(sec)
	}
	if out == nil {
		return sections
	}
	return out
}

func sectionHasMarks(sec DetailSection) bool {
	if g := sec.Grade; g != nil && HasMarks(g.Scanned) {
		return true
	}
	for _, it := range sec.Items {
		if HasMarks(it.Value) {
			return true
		}
		for _, c := range it.Chips {
			if HasMarks(c.Text) {
				return true
			}
		}
	}
	if t := sec.Table; t != nil {
		for _, r := range t.Rows {
			for _, c := range r {
				if HasMarks(c) {
					return true
				}
			}
		}
		for _, r := range t.Cells {
			for _, c := range r {
				if HasMarks(c.Text) {
					return true
				}
			}
		}
	}
	return false
}

func (f TimeFormat) writeSection(sec DetailSection) DetailSection {
	items := make([]DetailItem, len(sec.Items))
	for i, it := range sec.Items {
		it.Value = f.WriteMarks(it.Value)
		if len(it.Chips) > 0 {
			chips := make([]Chip, len(it.Chips))
			for j, c := range it.Chips {
				c.Text = f.WriteMarks(c.Text)
				chips[j] = c
			}
			it.Chips = chips
		}
		items[i] = it
	}
	if sec.Items != nil {
		sec.Items = items
	}
	if t := sec.Table; t != nil {
		nt := *t
		nt.Rows = make([][]string, len(t.Rows))
		for i, r := range t.Rows {
			row := make([]string, len(r))
			for j, c := range r {
				row[j] = f.WriteMarks(c)
			}
			nt.Rows[i] = row
		}
		if t.Cells != nil {
			nt.Cells = make([][]Cell, len(t.Cells))
			for i, r := range t.Cells {
				row := make([]Cell, len(r))
				for j, c := range r {
					c.Text = f.WriteMarks(c.Text)
					row[j] = c
				}
				nt.Cells[i] = row
			}
		}
		sec.Table = &nt
	}
	if g := sec.Grade; g != nil {
		ng := *g
		ng.Scanned = f.WriteMarks(g.Scanned)
		sec.Grade = &ng
	}
	return sec
}

// Layouts derived from the date and time layouts: the time of day without seconds (clockLayout),
// the date without the year (dayLayout), the time of day with milliseconds (preciseLayout). Each
// works on the layout's elements, so that a layout from any region (or, later, the user's own)
// derives the same way; a layout without the element is returned as it is.

var derivedLayouts sync.Map // derivedKey → string: a pure function's results, kept (a few layouts per run)

type deriveKind uint8

const (
	deriveClock deriveKind = iota
	deriveDay
	derivePrecise
)

type derivedKey struct {
	layout string
	kind   deriveKind
}

func derive(layout string, kind deriveKind) string {
	key := derivedKey{layout, kind}
	if v, ok := derivedLayouts.Load(key); ok {
		return v.(string)
	}
	var out string
	switch kind {
	case deriveClock:
		out = clockLayout(layout)
	case deriveDay:
		out = dayLayout(layout)
	default:
		out = preciseLayout(layout)
	}
	derivedLayouts.Store(key, out)
	return out
}

// clockLayout drops the seconds (and a fraction after them) from a time layout, with the
// separator before them when that is punctuation ("15:04:05" → "15:04", "3:04:05 PM" →
// "3:04 PM"), else with the unit after them ("15 h 04 min 05 s" → "15 h 04 min").
func clockLayout(layout string) string {
	ch := layoutChunks(layout)
	s := chunkIndex(ch, chunkSecond)
	if s < 0 {
		return layout
	}
	start, end := s, s+1
	if end < len(ch) && ch[end].kind == chunkFraction {
		end++
	}
	switch {
	case s > 0 && ch[s-1].kind == chunkLiteral && !hasLetter(ch[s-1].text):
		start = s - 1
	case end < len(ch) && ch[end].kind == chunkLiteral && hasLetter(ch[end].text):
		end++
	}
	return strings.TrimSpace(joinChunks(ch[:start]) + joinChunks(ch[end:]))
}

// dayLayout drops the year from a date layout. A year at the end goes with the separator before
// it, unless that is a full stop, which German and others write after the month ("02.01.2006" →
// "02.01.", "1/2/2006" → "1/2"); a year elsewhere goes with the separator after it ("2006-01-02"
// → "01-02").
func dayLayout(layout string) string {
	ch := layoutChunks(layout)
	y := chunkIndex(ch, chunkYear)
	if y < 0 {
		return layout
	}
	last := true
	for _, c := range ch[y+1:] {
		last = last && c.kind == chunkLiteral
	}
	start, end := y, y+1
	switch {
	case last:
		end = len(ch)
		if y > 0 && ch[y-1].kind == chunkLiteral && ch[y-1].text != "." {
			start = y - 1
		}
	case end < len(ch) && ch[end].kind == chunkLiteral:
		end++
	}
	return strings.TrimSpace(joinChunks(ch[:start]) + joinChunks(ch[end:]))
}

// preciseLayout adds milliseconds after the seconds of a time layout that has none.
func preciseLayout(layout string) string {
	ch := layoutChunks(layout)
	s := chunkIndex(ch, chunkSecond)
	if s < 0 || s+1 < len(ch) && ch[s+1].kind == chunkFraction {
		return layout
	}
	return joinChunks(ch[:s+1]) + ".000" + joinChunks(ch[s+1:])
}

func hasLetter(s string) bool { return strings.IndexFunc(s, unicode.IsLetter) >= 0 }

// The elements of a Go layout (package time's, the ones its documentation lists) and the literal
// text between them.

type chunkKind uint8

const (
	chunkLiteral chunkKind = iota
	chunkYear
	chunkMonth
	chunkDay
	chunkWeekday
	chunkHour
	chunkMinute
	chunkSecond
	chunkFraction
	chunkAMPM
	chunkZone
)

type layoutChunk struct {
	text string
	kind chunkKind
}

// layoutChunks splits a Go layout into its elements and the literal text between them, as package
// time reads it.
func layoutChunks(layout string) []layoutChunk {
	var out []layoutChunk
	lit := 0
	for i := 0; i < len(layout); {
		n, kind := layoutElement(layout[i:])
		if n == 0 {
			i++
			continue
		}
		if i > lit {
			out = append(out, layoutChunk{layout[lit:i], chunkLiteral})
		}
		out = append(out, layoutChunk{layout[i : i+n], kind})
		i += n
		lit = i
	}
	if lit < len(layout) {
		out = append(out, layoutChunk{layout[lit:], chunkLiteral})
	}
	return out
}

// layoutElement returns the length and kind of the layout element s starts with, 0 for literal
// text: package time's rules (its nextStdChunk).
func layoutElement(s string) (int, chunkKind) {
	has := func(p string) bool { return strings.HasPrefix(s, p) }
	lowerAt := func(i int) bool { return i < len(s) && s[i] >= 'a' && s[i] <= 'z' }
	switch s[0] {
	case 'J':
		if has("January") {
			return 7, chunkMonth
		}
		if has("Jan") && !lowerAt(3) {
			return 3, chunkMonth
		}
	case 'M':
		if has("Monday") {
			return 6, chunkWeekday
		}
		if has("Mon") && !lowerAt(3) {
			return 3, chunkWeekday
		}
		if has("MST") {
			return 3, chunkZone
		}
	case '0':
		if len(s) >= 2 && s[1] >= '1' && s[1] <= '6' {
			return 2, [...]chunkKind{chunkMonth, chunkDay, chunkHour, chunkMinute, chunkSecond, chunkYear}[s[1]-'1']
		}
		if has("002") {
			return 3, chunkDay
		}
	case '1':
		if has("15") {
			return 2, chunkHour
		}
		return 1, chunkMonth
	case '2':
		if has("2006") {
			return 4, chunkYear
		}
		return 1, chunkDay
	case '_':
		if has("__2") {
			return 3, chunkDay
		}
		if has("_2") && !has("_2006") {
			return 2, chunkDay
		}
	case '3':
		return 1, chunkHour
	case '4':
		return 1, chunkMinute
	case '5':
		return 1, chunkSecond
	case 'P':
		if has("PM") {
			return 2, chunkAMPM
		}
	case 'p':
		if has("pm") {
			return 2, chunkAMPM
		}
	case '-', 'Z':
		for _, z := range []string{"07:00:00", "070000", "07:00", "0700", "07"} {
			if strings.HasPrefix(s[1:], z) {
				return 1 + len(z), chunkZone
			}
		}
	case '.', ',':
		if len(s) >= 2 && (s[1] == '0' || s[1] == '9') {
			j := 1
			for j < len(s) && s[j] == s[1] {
				j++
			}
			if j >= len(s) || s[j] < '0' || s[j] > '9' {
				return j, chunkFraction
			}
		}
	}
	return 0, chunkLiteral
}

func chunkIndex(ch []layoutChunk, kind chunkKind) int {
	for i, c := range ch {
		if c.kind == kind {
			return i
		}
	}
	return -1
}

func joinChunks(ch []layoutChunk) string {
	var b strings.Builder
	for _, c := range ch {
		b.WriteString(c.text)
	}
	return b.String()
}

// LayoutElements returns the kinds of a Go layout's elements in order ("year", "month", "day",
// "weekday", "hour", "minute", "second", "fraction", "ampm", "zone"), literal text left out: what
// checks a layout converted from another notation (internal/locale).
func LayoutElements(layout string) []string {
	names := [...]string{chunkYear: "year", chunkMonth: "month", chunkDay: "day", chunkWeekday: "weekday",
		chunkHour: "hour", chunkMinute: "minute", chunkSecond: "second", chunkFraction: "fraction",
		chunkAMPM: "ampm", chunkZone: "zone"}
	var out []string
	for _, c := range layoutChunks(layout) {
		if c.kind != chunkLiteral {
			out = append(out, names[c.kind])
		}
	}
	return out
}
