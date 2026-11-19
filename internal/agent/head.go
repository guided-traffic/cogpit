package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// A tool result above the connection's limit (engine.ChatLimits.MaxToolResultKiB) goes to the
// model as a head: the result's own JSON document, still valid, with its largest part shortened to
// what fits. The part is the largest array or string of the document, at the top level or one
// object level down ("rows", "issues", "lines", "events.rows", "yaml"): an array keeps its leading
// elements, cut at an element boundary, a string its leading lines, cut at a line boundary.
// Everything else of the document stays, in its order.
//
// After the document, never inside it, Cogpit's note says how many of how many elements or lines
// are shown, what Cogpit counted over all of them (totals) and how to read on: result_read with
// the name the raw result is held under, which is the tool call's id, and the next offset
// (rawresult.go). The model reads the note with every request until the result is cleared (D-167),
// so it is short.
//
// The head cuts between elements or lines: a first one that does not fit beside the rest of the
// document is left to result_read, whose page has more room, and shown cut only when it fits no
// page either. A document without such a part, or one that is above the limit without its parts, is
// cut at a byte position, with a note that says so.
//
// The notes repeat little of a result, since a result's text is the cluster's: a part's name when
// its members' names are short plain words, and in the totals a field's name when it is short and
// its values when they are few and short, within totalsRoom. What is more than a plain word stands
// as a JSON string.

// partDepth is how many object levels below the document an array or a string counts as a part.
const partDepth = 2

// Bounds of the note's totals: a field or column is counted when its name has at most
// maxTotalValue bytes and it has at most maxTotalValues distinct values, none longer than
// maxTotalValue bytes; at most maxTotalFields are named, in at most maxTotalsBytes and an eighth of
// the limit (totalsRoom). maxTotalValue bounds a part's member names too (partName).
const (
	maxTotalValues = 8
	maxTotalFields = 6
	maxTotalValue  = 32
	maxTotalsBytes = 480
)

// totalsRoom is the most bytes a head's totals take of a result's limit.
func totalsRoom(limit int) int { return min(limit/8, maxTotalsBytes) }

// maxLeftOut is the most parts a head leaves out whole beside the one it shortens.
const maxLeftOut = 8

// minCut is the least room in which an element or a line that fits no page whole is shown cut.
const minCut = 64

// pageNoteRoom is about what a page of result_read spends on its note, for the head's guess whether
// an element fits a page (fitsAPage).
const pageNoteRoom = 400

// rowsMember is the member of a row table (engine.RowTable) that holds its rows.
const rowsMember = "rows"

// part is an array or a string of a result's document.
type part struct {
	path       []string // the members' names from the document's root; none for the root itself
	start, end int      // the value's bytes in the document
	array      bool
	parent     []byte // the object the part is a member of; nil for the root
}

// scanParts appends the parts of the value data, which starts at byte base of the document and is
// reached by path: itself, the members of an object and those of its object members.
func scanParts(data []byte, base int, path []string, parent []byte, out []part) []part {
	if len(data) == 0 {
		return out
	}
	if data[0] == '[' || data[0] == '"' {
		return append(out, part{path: path, start: base, end: base + len(data), array: data[0] == '[', parent: parent})
	}
	if data[0] != '{' || len(path) >= partDepth {
		return out
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if _, err := dec.Token(); err != nil {
		return out
	}
	for dec.More() {
		tok, err := dec.Token()
		key, ok := tok.(string)
		if err != nil || !ok {
			return out
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return out
		}
		at := base + int(dec.InputOffset()) - len(raw)
		out = scanParts(raw, at, append(slices.Clip(path), key), data, out)
	}
	return out
}

// partsOf returns a document's parts, the largest first.
func partsOf(data []byte) []part {
	parts := scanParts(data, 0, nil, nil, nil)
	sort.SliceStable(parts, func(i, j int) bool { return parts[i].end-parts[i].start > parts[j].end-parts[j].start })
	return parts
}

// paged is a part with its elements or lines at hand.
type paged struct {
	part
	elems []json.RawMessage // an array's elements
	lines []string          // a string's lines
	// columns and same are a row table's columns and the cells its rows share (engine.RowTable),
	// for an array of rows beside them.
	columns []string
	same    json.RawMessage
}

// openPart reads a part's elements or lines out of the document.
func openPart(data []byte, pt part) (*paged, error) {
	p := &paged{part: pt}
	value := data[pt.start:pt.end]
	if !pt.array {
		var s string
		if err := json.Unmarshal(value, &s); err != nil {
			return nil, err
		}
		p.lines = strings.Split(strings.TrimSuffix(s, "\n"), "\n")
		return p, nil
	}
	if err := json.Unmarshal(value, &p.elems); err != nil {
		return nil, err
	}
	if len(pt.path) > 0 && pt.path[len(pt.path)-1] == rowsMember && len(p.elems) > 0 && p.elems[0][0] == '[' {
		var table struct {
			Same    json.RawMessage `json:"same"`
			Columns []string        `json:"columns"`
		}
		if json.Unmarshal(pt.parent, &table) == nil && len(table.Columns) > 0 {
			p.columns, p.same = table.Columns, table.Same
		}
	}
	return p, nil
}

// largestPart opens the largest part of a document; nil for a document without one.
func largestPart(data []byte) *paged {
	parts := partsOf(data)
	if len(parts) == 0 {
		return nil
	}
	p, err := openPart(data, parts[0])
	if err != nil {
		return nil
	}
	return p
}

// count is how many elements or lines the part has.
func (p *paged) count() int {
	if p.array {
		return len(p.elems)
	}
	return len(p.lines)
}

// unit names what the part is counted in.
func (p *paged) unit() string {
	if p.array {
		return "elements"
	}
	return "lines"
}

// one names a single element or line by its index.
func (p *paged) one(i int) string {
	if p.array {
		return "Element " + strconv.Itoa(i)
	}
	return "Line " + strconv.Itoa(i)
}

// of names the part for a note: ` of "events.rows"`, "" for a part without a name (partName).
func (p *paged) of() string {
	if name := partName(p.path); name != "" {
		return " of " + name
	}
	return ""
}

// partName is a part's name for a note: its members' names from the document's root,
// `"events.rows"`. "" for the document itself, and for a part under a member whose name is not a
// plain word of at most maxTotalValue bytes: such a name is the cluster's text (a Config Map's key,
// a values file's), and a note does not repeat it.
func partName(path []string) string {
	if len(path) == 0 {
		return ""
	}
	for _, member := range path {
		if member == "" || len(member) > maxTotalValue || !plainWord(member) {
			return ""
		}
	}
	return quoted(strings.Join(path, "."))
}

// fitsAPage reports whether element or line i fits a page of result_read whole, as far as a head
// can tell: a page has the limit less its note and the part's name.
func (p *paged) fitsAPage(i, limit int) bool {
	return len(p.encoded(i))+2 <= limit-pageNoteRoom-len(p.wrap(nil, false))
}

// span is one page of a part.
type span struct {
	json  []byte // the page's elements as an array, or its lines as a string
	shown int    // the elements or lines in it
	next  int    // the offset of the first one after the page
	left  int    // the ones from next on that the page's search would still return
	// cut is set when the page's one element or line fits no page whole and is shown cut (cutTo):
	// its index, the bytes shown and the bytes it has.
	cut            bool
	cutAt, cutSize int
	cutIndex       int
}

// span builds the page from offset on: at most limit elements or lines (0: as many as fit), with
// contains only those whose text contains it whatever the case, in at most budget bytes of JSON.
// With cut, a first one that does not fit is shown cut, so that reading goes on behind it; without,
// the page ends before it.
func (p *paged) span(offset, limit int, contains string, budget int, cut bool) span {
	match := p.matcher(contains)
	n := p.count()
	open, shut, sep := byte('['), byte(']'), ","
	if !p.array {
		open, shut, sep = '"', '"', `\n`
	}
	buf := []byte{open}
	sp := span{}
	i := max(offset, 0)
	for ; i < n && (limit <= 0 || sp.shown < limit); i++ {
		if !match(i) {
			continue
		}
		enc := p.encoded(i)
		lead := 0
		if sp.shown > 0 {
			lead = len(sep)
		}
		if len(buf)+lead+len(enc)+1 > budget {
			if room := budget - len(buf) - 1; cut && sp.shown == 0 && room >= minCut {
				piece, kept := p.cutTo(i, room)
				buf = append(buf, piece...)
				sp.shown, sp.cut, sp.cutIndex, sp.cutAt, sp.cutSize = 1, true, i, kept, p.size(i)
				i++
			}
			break
		}
		if sp.shown > 0 {
			buf = append(buf, sep...)
		}
		buf = append(buf, enc...)
		sp.shown++
	}
	sp.json = append(buf, shut)
	sp.next = min(i, n)
	for j := sp.next; j < n; j++ {
		if match(j) {
			sp.left++
		}
	}
	return sp
}

// matcher reports for an element or a line whether its text contains the words looked for,
// whatever the case; an element's text is its JSON.
func (p *paged) matcher(contains string) func(i int) bool {
	if contains == "" {
		return func(int) bool { return true }
	}
	needle := strings.ToLower(contains)
	if !p.array {
		return func(i int) bool { return strings.Contains(strings.ToLower(p.lines[i]), needle) }
	}
	// As the element's JSON writes it: a quote or a < is escaped there.
	escaped, _ := json.Marshal(contains)
	inJSON := bytes.ToLower(escaped[1 : len(escaped)-1])
	return func(i int) bool {
		text := bytes.ToLower(p.elems[i])
		return bytes.Contains(text, []byte(needle)) || bytes.Contains(text, inJSON)
	}
}

// encoded is element i as it stands in a page's array, or line i as it stands in a page's string.
func (p *paged) encoded(i int) []byte {
	if p.array {
		return p.elems[i]
	}
	enc := jsonString(p.lines[i])
	return enc[1 : len(enc)-1]
}

// size is the bytes of element i's JSON or of line i.
func (p *paged) size(i int) int {
	if p.array {
		return len(p.elems[i])
	}
	return len(p.lines[i])
}

// cutTo is the beginning of element or line i in at most room bytes of a page, and the bytes of it
// that are shown: a line cut at a character and ending in "…", an element as cutJSON cuts it, null
// for one that cannot be cut.
func (p *paged) cutTo(i, room int) ([]byte, int) {
	if p.array {
		if piece, kept := cutJSON(p.elems[i], room); piece != nil {
			return piece, kept
		}
		return []byte(jsonNull), 0
	}
	text := p.lines[i]
	for k := min(len(text), room); k > 0; {
		head := truncateUTF8(text, k)
		enc := jsonString(head + cutMark)
		enc = enc[1 : len(enc)-1] // a line stands inside the page's string
		if len(enc) <= room {
			return enc, len(head)
		}
		k = min(k*room/len(enc), k-1)
	}
	return nil, 0
}

// cutMark ends a text that is shown cut.
const cutMark = "…"

// cutJSON returns the beginning of the JSON value v as a JSON value of its own in at most room
// bytes, and how many of v's bytes it keeps: v's text up to a place where a value can end, a string
// it ends in closed after "…", the arrays and objects around it closed. The text stays as v has it,
// escaped once, so that what looks for a value in it (the Secret filter of what is kept) still
// finds it. nil when no such beginning fits: a number or a word longer than room.
func cutJSON(v []byte, room int) ([]byte, int) {
	var (
		closers []byte // what closes the open arrays and objects, the innermost last
		best    struct {
			at      int // v's bytes kept
			str     bool
			closers []byte
			ok      bool
		}
		inStr, key, esc, scalar bool
		hex                     int  // the digits left of a \u escape
		wantKey                 bool // the next string is a member's name
	)
	// keep notes that v can end before byte at: inside a string value when str is set.
	keep := func(at int, str bool) {
		n := at + len(closers)
		if str {
			n += len(cutMark) + 1
		}
		if n <= room {
			best.at, best.str, best.ok = at, str, true
			best.closers = append(best.closers[:0], closers...)
		}
	}
	for i := 0; i < len(v) && i <= room; i++ {
		c := v[i]
		if inStr {
			if !key && !esc && hex == 0 && utf8.RuneStart(c) {
				keep(i, true)
			}
			switch {
			case hex > 0:
				hex--
			case esc:
				esc = false
				if c == 'u' {
					hex = 4
				}
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
				if !key && len(closers) > 0 {
					keep(i+1, false)
				}
				key = false
			}
			continue
		}
		if scalar {
			if !strings.ContainsRune(",]} \t\r\n", rune(c)) {
				continue
			}
			scalar = false
			if len(closers) > 0 {
				keep(i, false)
			}
		}
		switch c {
		case ' ', '\t', '\r', '\n', ':':
		case '"':
			inStr, key, wantKey = true, wantKey, false
		case '{', '[':
			closers = append(closers, c+2) // '}' and ']' follow '{' and '[' by two
			wantKey = c == '{'
			keep(i+1, false)
		case '}', ']':
			closers = closers[:max(len(closers)-1, 0)]
			wantKey = false
			if len(closers) > 0 {
				keep(i+1, false)
			}
		case ',':
			wantKey = len(closers) > 0 && closers[len(closers)-1] == '}'
		default:
			scalar = true
		}
	}
	if !best.ok {
		return nil, 0
	}
	out := append([]byte(nil), v[:best.at]...)
	if best.str {
		out = append(out, cutMark+`"`...)
	}
	slices.Reverse(best.closers)
	return append(out, best.closers...), best.at
}

// wrap is a page as its own document: the page under the part's name, a row table's rows with its
// columns and shared cells when header is set; the page alone for a part that is the document.
func (p *paged) wrap(page []byte, header bool) []byte {
	if len(p.path) == 0 {
		return page
	}
	var b bytes.Buffer
	b.WriteByte('{')
	if header && p.columns != nil {
		if len(p.same) > 0 {
			b.WriteString(`"same":`)
			b.Write(p.same)
			b.WriteByte(',')
		}
		b.WriteString(`"columns":`)
		b.Write(jsonValue(p.columns))
		b.WriteByte(',')
	}
	b.Write(jsonString(p.path[len(p.path)-1]))
	b.WriteByte(':')
	b.Write(page)
	b.WriteByte('}')
	return b.Bytes()
}

// totals is what Cogpit counted over all of an array's elements, for the note: per string field
// of its objects, or per column of a row table, the count of each value, for the fields with few
// values that repeat ("level: ok 212, error 19; kind: Pod 229, Node 2"), the fields in their order
// for as long as they fit room bytes. "" when there is none. The names and the values are the
// result's own text, a cluster's included: noteWord writes them, and a field with a name longer
// than maxTotalValue bytes is not counted.
func (p *paged) totals(room int) string {
	if !p.array {
		return ""
	}
	var fields []*fieldCount
	byName := map[string]*fieldCount{}
	add := func(name, value string) {
		f := byName[name]
		if f == nil {
			f = &fieldCount{name: name, values: map[string]int{}, dropped: len(name) > maxTotalValue}
			byName[name] = f
			fields = append(fields, f)
		}
		f.add(value)
	}
	for _, e := range p.elems {
		if p.columns != nil {
			var row []string
			if json.Unmarshal(e, &row) != nil {
				return ""
			}
			for j, cell := range row[:min(len(row), len(p.columns))] {
				add(p.columns[j], cell)
			}
			continue
		}
		if !countFields(e, byName, add) {
			return ""
		}
	}
	const sep = "; "
	var out []string
	for _, f := range fields {
		if len(out) == maxTotalFields {
			break
		}
		if !f.counts() {
			continue
		}
		if sum := f.sum(len(p.elems)); len(sum)+len(sep) <= room {
			out = append(out, sum)
			room -= len(sum) + len(sep)
		}
	}
	return strings.Join(out, sep)
}

// countFields adds an element's string fields, in their order, to the count; false for an element
// that is no object. A field no longer counted (fieldCount.dropped) is not decoded.
func countFields(elem []byte, byName map[string]*fieldCount, add func(name, value string)) bool {
	if len(elem) == 0 || elem[0] != '{' {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(elem))
	if _, err := dec.Token(); err != nil {
		return false
	}
	for dec.More() {
		tok, err := dec.Token()
		key, ok := tok.(string)
		if err != nil || !ok {
			return false
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return false
		}
		if f := byName[key]; raw[0] != '"' || (f != nil && f.dropped) {
			continue
		}
		var s string
		if json.Unmarshal(raw, &s) == nil {
			add(key, s)
		}
	}
	return true
}

// fieldCount counts a field's values over an array's elements.
type fieldCount struct {
	name    string
	values  map[string]int
	n       int
	dropped bool // too many values, too long a one or too long a name: not a field to sum up
}

func (f *fieldCount) add(value string) {
	if f.dropped {
		return
	}
	if _, known := f.values[value]; !known && (len(f.values) == maxTotalValues || len(value) > maxTotalValue) {
		f.dropped, f.values = true, nil
		return
	}
	f.values[value]++
	f.n++
}

// counts reports whether the field is one the note sums up: few values, and they repeat.
func (f *fieldCount) counts() bool {
	return !f.dropped && len(f.values) > 0 && f.n >= 2*len(f.values)
}

// sum writes the field's counts over total elements, the most frequent value first, and how many
// elements lack the field: "level: ok 212, error 19", "status: warning 149, (none) 851".
func (f *fieldCount) sum(total int) string {
	values := make([]string, 0, len(f.values))
	for v := range f.values {
		values = append(values, v)
	}
	sort.Slice(values, func(i, j int) bool {
		if f.values[values[i]] != f.values[values[j]] {
			return f.values[values[i]] > f.values[values[j]]
		}
		return values[i] < values[j]
	})
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = noteWord(v) + " " + strconv.Itoa(f.values[v])
	}
	if f.n < total {
		parts = append(parts, "(none) "+strconv.Itoa(total-f.n))
	}
	return noteWord(f.name) + ": " + strings.Join(parts, ", ")
}

// noteWord writes a name or a value from a result into Cogpit's note: as it is when it is made of
// letters, digits and a few signs, else as a JSON string, so that a text from the cluster stays
// recognisable as one and cannot end the note.
func noteWord(s string) string {
	switch {
	case s == "":
		return "(empty)"
	case plainWord(s):
		return s
	}
	return quoted(s)
}

// plainWord reports whether s is made of letters, digits and the signs _ - . / : alone.
func plainWord(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', strings.ContainsRune("_-./:", r):
		default:
			return false
		}
	}
	return true
}

// jsonValue encodes v as the notes and the pages write JSON: <, > and & as they are.
func jsonValue(v any) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return []byte(`""`)
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}

// jsonString is s as a JSON string.
func jsonString(s string) []byte { return jsonValue(s) }

// quoted is s as a JSON string, for a note.
func quoted(s string) string { return string(jsonString(s)) }

// readOn is the call that reads a held result on, as the notes write it:
// result_read {"call":"call_3","offset":65}.
type readOn struct {
	Call     string `json:"call"`
	Offset   int    `json:"offset"`
	Limit    int    `json:"limit,omitempty"`
	Contains string `json:"contains,omitempty"`
}

func (r readOn) String() string { return toolRead + " " + string(jsonValue(r)) }

// The note after a head: how it begins, how it leads on to the call that reads on, and how it ends
// after that call.
const (
	headNoteStart = "[Cogpit: this shows the first "
	headReadOn    = " Read on with "
	headNoteEnd   = `; add "contains" to search all of it.]`
)

// headRef returns the name a tool result's raw result was held under, for a tool message that is a
// head, or a choice's entries (choose.go), whose note names result_read. Only Cogpit's own shape
// counts: the result's frame line, the document's one line, then the note's line, which ends in
// the call. An error's message and a document hold the cluster's text, which may write the same
// words: neither is read.
func headRef(content string) (string, bool) {
	frame, rest, ok := strings.Cut(content, "\n")
	if !ok || !strings.HasPrefix(frame, resultFrameStart) || !strings.HasSuffix(frame, resultFrameEnd) {
		return "", false
	}
	_, rest, ok = strings.Cut(rest, "\n") // the document
	if !ok {
		return "", false
	}
	note, _, _ := strings.Cut(rest, "\n") // a note about the connection may follow (D-58)
	i := strings.LastIndex(note, headReadOn+toolRead+" ")
	if i < 0 || (!strings.HasPrefix(note, headNoteStart) && !strings.HasPrefix(note, choiceNoteStart)) || !strings.HasSuffix(note, headNoteEnd) {
		return "", false
	}
	call := note[i+len(headReadOn) : len(note)-len(headNoteEnd)]
	var r readOn
	if json.Unmarshal([]byte(strings.TrimPrefix(call, toolRead+" ")), &r) != nil || r.Call == "" || len(r.Call) > maxRef ||
		call != (readOn{Call: r.Call, Offset: r.Offset}).String() {
		return "", false
	}
	return r.Call, true
}

// leftOut is a part a head leaves out whole to fit the limit.
type leftOut struct {
	part
	what string // `"sections" (12 elements)`
}

// headResult builds the head of a result above limit: the document with its largest part's
// leading elements or lines, a line break and Cogpit's note, together at most limit bytes. With
// held, the note names result_read for ref, the name the raw result is held under. It returns the
// head and whether the note names result_read; ok is false for a document that has no part, does
// not fit without its largest parts, or is not one line.
func headResult(data []byte, limit int, ref string, held bool) (head string, reads, ok bool) {
	parts := partsOf(data)
	if len(parts) == 0 || bytes.IndexByte(data, '\n') >= 0 {
		return "", false, false
	}
	// What is left when every part that may go is left out: if that does not fit, nothing is opened.
	least := len(data) - (parts[0].end - parts[0].start)
	for _, pt := range parts[1:min(len(parts), 1+maxLeftOut)] {
		least -= pt.end - pt.start - 2
	}
	if least+len(headNoteStart) > limit {
		return "", false, false
	}
	p, err := openPart(data, parts[0])
	if err != nil || p.count() == 0 {
		return "", false, false
	}
	n := p.count()
	totals := p.totals(totalsRoom(limit))
	// The note's room is measured with the widest numbers it can carry. When the rest of the
	// document and the note do not fit, the totals go first, then the parts after the largest are
	// left out, the largest of them first, for as long as leaving one out gains room.
	var out []leftOut
	widest := span{shown: n, next: n - 1, left: 1}
	rest := len(data) - (p.end - p.start)
	room := 0
	for next := parts[1:]; ; {
		room = limit - rest - 1 - len(headNote(p, widest, totals, ref, held, out, len(data)))
		if room >= 2 {
			break
		}
		if totals != "" {
			totals = ""
			continue
		}
		if len(next) == 0 || len(out) == maxLeftOut {
			return "", false, false
		}
		o := leaveOut(data, next[0])
		gain := o.end - o.start - 2
		if gain <= len(o.what)+2 {
			return "", false, false
		}
		out, next, rest = append(out, o), next[1:], rest-gain
	}
	sp := p.span(0, 0, "", room, false)
	if sp.shown == 0 && !p.fitsAPage(0, limit) {
		// The first element or line fits no page of result_read either: its beginning is shown here.
		widest.cut, widest.cutAt, widest.cutSize = true, p.size(0), p.size(0)
		sp = p.span(0, 0, "", room-len(cutClause(p, widest)), true)
	}
	reads = held && sp.next < n
	type patch struct {
		part
		with []byte
	}
	patches := []patch{{p.part, sp.json}}
	for _, o := range out {
		patches = append(patches, patch{o.part, emptyPart(o.part)})
	}
	sort.Slice(patches, func(i, j int) bool { return patches[i].start < patches[j].start })
	var b strings.Builder
	at := 0
	for _, pt := range patches {
		b.Write(data[at:pt.start])
		b.Write(pt.with)
		at = pt.end
	}
	b.Write(data[at:])
	b.WriteByte('\n')
	b.WriteString(headNote(p, sp, totals, ref, reads, out, len(data)))
	return b.String(), reads, true
}

// leaveOut names a part a head leaves out, with what it holds: `"sections" (12 elements)`, by its
// kind when it has no name a note repeats (partName).
func leaveOut(data []byte, pt part) leftOut {
	o := leftOut{part: pt, what: partName(pt.path)}
	if o.what == "" {
		o.what = "a text"
		if pt.array {
			o.what = "an array"
		}
	}
	if p, err := openPart(data, pt); err == nil {
		o.what += fmt.Sprintf(" (%d %s)", p.count(), p.unit())
	}
	return o
}

// emptyPart is what stands in a head for a part left out.
func emptyPart(pt part) []byte {
	if pt.array {
		return []byte("[]")
	}
	return []byte(`""`)
}

// headNote is Cogpit's note after a head: how much of the part is shown, the totals over all of
// it, what was left out, and how to read on; without reads, how to get less instead.
func headNote(p *paged, sp span, totals, ref string, reads bool, out []leftOut, size int) string {
	n := p.count()
	var b strings.Builder
	fmt.Fprintf(&b, headNoteStart+"%d of the %d %s%s.", sp.shown, n, p.unit(), p.of())
	b.WriteString(cutClause(p, sp))
	if totals != "" {
		fmt.Fprintf(&b, " Counted over all %d: %s.", n, totals)
	}
	if len(out) > 0 {
		names := make([]string, len(out))
		for i, o := range out {
			names[i] = o.what
		}
		b.WriteString(" Left out to fit: " + strings.Join(names, ", ") + "; ask the tool for less to get them.")
	}
	switch {
	case reads:
		b.WriteString(headReadOn + readOn{Call: ref, Offset: sp.next}.String() + headNoteEnd)
		return b.String()
	case sp.next < n:
		fmt.Fprintf(&b, " The rest of the %d bytes is not held: ask the tool for less, with a namespace, a filter or a limit.", size)
	}
	b.WriteByte(']')
	return b.String()
}

// cutClause says which element or line of a head or a page is shown cut. Nothing reads the rest of
// it: result_read's offsets count whole elements and lines.
func cutClause(p *paged, sp span) string {
	if !sp.cut {
		return ""
	}
	return fmt.Sprintf(" %s is cut: %d of its %d bytes, and result_read does not reach the rest of it.", p.one(sp.cutIndex), sp.cutAt, sp.cutSize)
}
