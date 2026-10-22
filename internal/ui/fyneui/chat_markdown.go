package fyneui

import (
	"image/color"
	"net/url"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// The agent chat's Markdown (chat.go): an answer is drawn as Markdown on the column's background,
// as the owner's design direction has it (plan/captures/chat): bold and italic text, headings,
// lists, links that open in the browser, inline code as small chips in a warm colour, code blocks,
// and GitHub tables as a grid with thin borders whose cells wrap. Fyne's Markdown parser reads the
// text (widget.NewRichTextFromMarkdown: goldmark with GitHub's tables), but its RichText draws
// neither code chips nor a table that wraps inside a narrow column, so the parsed segments become
// Cogpit's own blocks (mdBlock), drawn by the widgets below. Parsing is pure data and runs off the
// main goroutine (chat.go's reader); the widgets lay out on it.
//
// Each block is its own widget and keeps its layout until its content or its width changes: while
// an answer streams in, only its last block is laid out again (chat.go measures it).

// mdRun is a run of inline text in one style.
type mdRun struct {
	text   string
	bold   bool
	italic bool
	strike bool
	code   bool     // inline code: a chip
	link   *url.URL // a link: underlined in the link colour, opened on a click
}

// mdKind is a block's kind.
type mdKind uint8

const (
	mdKindPara    mdKind = iota
	mdKindHeading        // level 1 or 2; a level 3 heading is a bold paragraph, as Fyne's parser has it
	mdKindQuote
	mdKindCode
	mdKindList
	mdKindTable
	mdKindRule
)

// mdBlock is one block of an answer.
type mdBlock struct {
	kind  mdKind
	level int     // a heading's level
	runs  []mdRun // a paragraph's, heading's or quote's text
	code  string  // a code block's text
	list  *mdList
	table *mdTable
}

// mdList is a list: each item is blocks of its own (paragraphs, nested lists).
type mdList struct {
	ordered bool
	start   int
	items   [][]mdBlock
}

// mdTable is a GitHub table: the header's cells, the rows' cells, each a run list.
type mdTable struct {
	header [][]mdRun
	rows   [][][]mdRun
	align  []fyne.TextAlign
}

// parseChatMarkdown reads an answer's Markdown into blocks. Images are not loaded: an image is its
// alt text, as the Helm README shows it (helmchartdrawer.go), so that an answer cannot make the
// window fetch a URL.
func parseChatMarkdown(text string) []mdBlock {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return blocksFrom(widget.NewRichTextFromMarkdown(withoutImages(text)).Segments)
}

// plainBlocks is a user's message: its text as typed, one paragraph, its line breaks kept.
func plainBlocks(text string) []mdBlock {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return nil
	}
	return []mdBlock{{kind: mdKindPara, runs: []mdRun{{text: text}}}}
}

// blocksFrom turns Fyne's segments into blocks.
func blocksFrom(segs []widget.RichTextSegment) []mdBlock {
	var out []mdBlock
	var cur []mdRun
	kind, level := mdKindPara, 0
	flush := func() {
		if len(cur) > 0 && strings.TrimSpace(runsText(cur)) != "" {
			out = append(out, mdBlock{kind: kind, level: level, runs: cur})
		}
		cur, kind, level = nil, mdKindPara, 0
	}
	for _, s := range segs {
		switch t := s.(type) {
		case *widget.TextSegment:
			if !t.Style.Inline { // the end of a paragraph or heading
				if t.Text != "" {
					cur = append(cur, runOf(t))
				}
				flush()
				continue
			}
			switch t.Style.SizeName {
			case theme.SizeNameHeadingText:
				kind, level = mdKindHeading, 1
			case theme.SizeNameSubHeadingText:
				kind, level = mdKindHeading, 2
			}
			if t.Style.QuotingDepth > 0 && kind == mdKindPara {
				kind = mdKindQuote
			}
			cur = append(cur, runOf(t))
		case *widget.HyperlinkSegment:
			cur = append(cur, mdRun{text: t.Text, link: t.URL, bold: t.TextStyle.Bold, italic: t.TextStyle.Italic})
		case *widget.ImageSegment: // a reference-style image the README's filter leaves
			cur = append(cur, mdRun{text: imageText(t.Title)})
		case *widget.CheckBoxSegment:
			mark := "☐ "
			if t.Checked {
				mark = "☑ "
			}
			cur = append(cur, mdRun{text: mark + t.Text})
			flush()
		case *widget.ListSegment:
			flush()
			out = append(out, mdBlock{kind: mdKindList, list: listFrom(t)})
		case *widget.ParagraphSegment:
			flush()
			out = append(out, blocksFrom(t.Texts)...)
		case *widget.CodeBlockSegment:
			flush()
			out = append(out, mdBlock{kind: mdKindCode, code: t.Text})
		case *widget.TableSegment:
			flush()
			out = append(out, mdBlock{kind: mdKindTable, table: tableFrom(t)})
		case *widget.SeparatorSegment:
			flush()
			out = append(out, mdBlock{kind: mdKindRule})
		}
	}
	flush()
	return out
}

// imageText is an image as the answer shows it: its title or alt text, never the image.
func imageText(title string) string {
	if title = strings.TrimSpace(title); title != "" {
		return "(image: " + title + ")"
	}
	return "(an image)"
}

func runOf(t *widget.TextSegment) mdRun {
	st := t.Style.TextStyle
	return mdRun{text: t.Text, bold: st.Bold, italic: st.Italic, strike: st.Strikethrough, code: st.Monospace}
}

// runsFrom flattens a table cell's segments into runs.
func runsFrom(segs []widget.RichTextSegment) []mdRun {
	var out []mdRun
	for _, b := range blocksFrom(segs) {
		if len(out) > 0 {
			out = append(out, mdRun{text: " "})
		}
		out = append(out, b.runs...)
	}
	return out
}

func listFrom(l *widget.ListSegment) *mdList {
	out := &mdList{ordered: l.Ordered, start: l.StartNumber()}
	for _, it := range l.Items {
		switch t := it.(type) {
		case *widget.ListSegment: // a nested list belongs to the item before it
			nested := mdBlock{kind: mdKindList, list: listFrom(t)}
			if n := len(out.items); n > 0 {
				out.items[n-1] = append(out.items[n-1], nested)
			} else {
				out.items = append(out.items, []mdBlock{nested})
			}
		case *widget.ParagraphSegment:
			out.items = append(out.items, blocksFrom(t.Texts))
		default:
			out.items = append(out.items, blocksFrom([]widget.RichTextSegment{it}))
		}
	}
	return out
}

func tableFrom(t *widget.TableSegment) *mdTable {
	out := &mdTable{align: t.Alignments}
	for _, c := range t.Headers {
		out.header = append(out.header, runsFrom(c))
	}
	for _, r := range t.Rows {
		var row [][]mdRun
		for _, c := range r {
			row = append(row, runsFrom(c))
		}
		out.rows = append(out.rows, row)
	}
	return out
}

func runsText(runs []mdRun) string {
	var b strings.Builder
	for _, r := range runs {
		b.WriteString(r.text)
	}
	return b.String()
}

// key is the block's content as one string: a block whose key did not change keeps its widget.
func (b mdBlock) key() string {
	var s strings.Builder
	writeBlockKey(&s, b)
	return s.String()
}

func writeBlockKey(s *strings.Builder, b mdBlock) {
	s.WriteString(strconv.Itoa(int(b.kind)))
	s.WriteByte(':')
	s.WriteString(strconv.Itoa(b.level))
	s.WriteByte(':')
	writeRunsKey(s, b.runs)
	s.WriteString(b.code)
	if l := b.list; l != nil {
		s.WriteString(strconv.FormatBool(l.ordered))
		s.WriteString(strconv.Itoa(l.start))
		for _, it := range l.items {
			s.WriteString("\x01")
			for _, c := range it {
				writeBlockKey(s, c)
			}
		}
	}
	if t := b.table; t != nil {
		for _, c := range t.header {
			s.WriteString("\x02")
			writeRunsKey(s, c)
		}
		for _, r := range t.rows {
			s.WriteString("\x03")
			for _, c := range r {
				s.WriteString("\x02")
				writeRunsKey(s, c)
			}
		}
	}
}

func writeRunsKey(s *strings.Builder, runs []mdRun) {
	for _, r := range runs {
		flags := 0
		for i, on := range []bool{r.bold, r.italic, r.strike, r.code} {
			if on {
				flags |= 1 << i
			}
		}
		s.WriteByte(byte('a' + flags))
		if r.link != nil {
			s.WriteString(r.link.String())
		}
		s.WriteByte(0)
		s.WriteString(r.text)
		s.WriteByte(0)
	}
}

// stripThinking removes a model's reasoning from its answer text: the <think>…</think> blocks that
// local reasoning models (Qwen, DeepSeek) write before the answer. Reasoning is never shown as the
// answer (D-10); an unclosed block at the end means the model is still thinking.
func stripThinking(text string) (answer string, thinking bool) {
	for _, tag := range [][2]string{{"<think>", "</think>"}, {"<thinking>", "</thinking>"}} {
		for {
			i := strings.Index(text, tag[0])
			if i < 0 {
				break
			}
			j := strings.Index(text[i:], tag[1])
			if j < 0 {
				return strings.TrimSpace(text[:i]), true
			}
			text = text[:i] + text[i+j+len(tag[1]):]
		}
	}
	return strings.TrimLeft(text, "\n "), false
}

// --- the widgets ---

// widthAware is a widget that wraps its text to a width it is told before its minimum size is
// asked: a text's height depends on its width, which Fyne's layouts do not pass down.
type widthAware interface {
	setWidth(w float32)
}

// vstack stacks objects at its width, with a gap. It is a widget, not a container: refreshing it
// lays its objects out again without refreshing each of them (fyne.Container.Refresh does), so a
// streaming answer re-lays its last block only.
type vstack struct {
	widget.BaseWidget
	objs  []fyne.CanvasObject
	gap   fyne.ThemeSizeName // between two visible objects; empty: none
	gapX  float32            // a multiple of gap
	outer fyne.Widget        // the widget that embeds it, itself when it stands alone
}

// newVStack is a vstack on its own.
func newVStack(gap fyne.ThemeSizeName, objs ...fyne.CanvasObject) *vstack {
	v := makeVStack(gap, objs...)
	v.extend(v)
	return v
}

// makeVStack is a vstack for a widget that embeds it and then calls extend with itself: Fyne keys a
// widget's renderer by the outermost widget.
func makeVStack(gap fyne.ThemeSizeName, objs ...fyne.CanvasObject) *vstack {
	return &vstack{objs: objs, gap: gap, gapX: 1}
}

func (v *vstack) extend(outer fyne.Widget) {
	v.outer = outer
	v.ExtendBaseWidget(outer)
}

// set replaces the objects.
func (v *vstack) set(objs []fyne.CanvasObject) {
	v.objs = objs
	v.Refresh()
}

func (v *vstack) setWidth(w float32) {
	for _, o := range v.objs {
		if a, ok := o.(widthAware); ok {
			a.setWidth(w)
		}
	}
}

func (v *vstack) gapSize() float32 {
	if v.gap == "" {
		return 0
	}
	return v.Theme().Size(v.gap) * v.gapX
}

// CreateRenderer implements fyne.Widget.
func (v *vstack) CreateRenderer() fyne.WidgetRenderer { return &vstackRenderer{v: v} }

type vstackRenderer struct{ v *vstack }

func (r *vstackRenderer) Layout(size fyne.Size) {
	gap, y := r.v.gapSize(), float32(0)
	for _, o := range r.v.objs {
		if !o.Visible() {
			continue
		}
		h := o.MinSize().Height
		o.Move(fyne.NewPos(0, y))
		o.Resize(fyne.NewSize(size.Width, h))
		y += h + gap
	}
}

func (r *vstackRenderer) MinSize() fyne.Size {
	gap := r.v.gapSize()
	var w, h float32
	n := 0
	for _, o := range r.v.objs {
		if !o.Visible() {
			continue
		}
		ms := o.MinSize()
		w = max(w, ms.Width)
		h += ms.Height
		n++
	}
	if n > 1 {
		h += gap * float32(n-1)
	}
	return fyne.NewSize(w, h)
}

func (r *vstackRenderer) Refresh() {
	r.Layout(r.v.Size())
	canvas.Refresh(r.v.outer)
}

func (r *vstackRenderer) Objects() []fyne.CanvasObject { return r.v.objs }
func (r *vstackRenderer) Destroy()                     {}

// mdText draws blocks: an answer, a user's message, a list item's content. It keeps the widget of
// every block whose content did not change.
type mdText struct {
	*vstack
	color fyne.ThemeColorName
	hooks mdHooks
	keys  []string
	width float32
	// relaid counts the blocks laid out anew by setBlocks (the streaming test reads it).
	relaid int
}

func newMDText(color fyne.ThemeColorName, hooks mdHooks) *mdText {
	t := &mdText{vstack: makeVStack(theme.SizeNamePadding), color: color, hooks: hooks}
	t.gapX = 2
	t.extend(t)
	return t
}

// setBlocks shows blocks; the widgets of the blocks that did not change stay as they are.
func (t *mdText) setBlocks(blocks []mdBlock) {
	keys := make([]string, len(blocks))
	objs := make([]fyne.CanvasObject, len(blocks))
	changed := len(blocks) != len(t.objs)
	for i, b := range blocks {
		keys[i] = b.key()
		if i < len(t.keys) && t.keys[i] == keys[i] {
			objs[i] = t.objs[i]
			continue
		}
		changed = true
		if i < len(t.objs) {
			if f, ok := t.objs[i].(*mdFlow); ok && isFlow(b.kind) && f.kindOf == b.kind && f.level == b.level {
				f.setRuns(b.runs) // a growing paragraph keeps its widget
				objs[i] = f
				t.relaid++
				continue
			}
		}
		objs[i] = newBlockView(b, t.color, t.hooks)
		if a, ok := objs[i].(widthAware); ok {
			a.setWidth(t.width)
		}
		t.relaid++
	}
	t.keys = keys
	if changed {
		t.objs = objs
		t.Refresh()
	}
}

func (t *mdText) setWidth(w float32) {
	t.width = w
	t.vstack.setWidth(w)
}

func isFlow(k mdKind) bool { return k == mdKindPara || k == mdKindHeading || k == mdKindQuote }

// newBlockView makes the widget of one block.
func newBlockView(b mdBlock, color fyne.ThemeColorName, hooks mdHooks) fyne.CanvasObject {
	switch b.kind {
	case mdKindCode:
		c := newMDCode(b.code)
		if hooks.copy != nil {
			c.selectable(hooks.copy)
		}
		return c
	case mdKindList:
		return newMDList(b.list, color, hooks)
	case mdKindTable:
		return newMDTable(b.table, color, hooks)
	case mdKindRule:
		return newDivider(false)
	}
	f := newMDFlow(b.runs, color, hooks.link)
	if hooks.copy != nil {
		f.selectable(hooks.copy)
	}
	f.kindOf, f.level = b.kind, b.level
	switch {
	case b.kind == mdKindHeading && b.level == 1:
		f.size, f.bold = theme.SizeNameSubHeadingText, true
	case b.kind == mdKindHeading:
		f.bold = true
	case b.kind == mdKindQuote:
		f.italic, f.color = true, colorMuted
	}
	return f
}

// mdFlow is wrapped inline text: words, inline code chips, links. It lays its pieces out for the
// width it was told and again only when its runs or that width change.
type mdFlow struct {
	widget.BaseWidget
	runs     []mdRun
	kindOf   mdKind
	level    int
	size     fyne.ThemeSizeName
	bold     bool
	italic   bool
	color    fyne.ThemeColorName
	onLink   func(*url.URL)
	width    float32
	maxLines int // more lines are cut, the last one ending in "…"; 0: no limit

	pieces  []flowPiece
	height  float32
	laidW   float32
	laidOK  bool
	overURL bool     // the pointer is over a link
	sel     *selArea // the selection over the text (chat_select.go); nil: not selectable
}

// flowPiece is a piece of a laid-out flow: text in one style at a position.
type flowPiece struct {
	text  string
	x, y  float32
	w, h  float32
	style fyne.TextStyle
	size  float32
	code  bool
	link  *url.URL
	line  int       // the line it is on, from 0
	brk   flowBreak // what the text had where this piece starts a line (chat_select.go's copy)
	hard  int       // with flowHard: how many line breaks (a blank line is two)
}

// flowBreak is what a piece that starts a line follows in the text: the lines a width wrapped run
// on, a word's space carried by the line before.
type flowBreak uint8

const (
	flowWrap  flowBreak = iota // a wrap: the line before ends with its space, or a word broke
	flowHard                   // a line break of the text
	flowSpace                  // a wrap at a space the line start dropped
)

var (
	_ fyne.Tappable      = (*mdFlow)(nil)
	_ desktop.Cursorable = (*mdFlow)(nil)
	_ desktop.Hoverable  = (*mdFlow)(nil)
)

func newMDFlow(runs []mdRun, color fyne.ThemeColorName, onLink func(*url.URL)) *mdFlow {
	f := &mdFlow{runs: runs, size: theme.SizeNameText, color: color, onLink: onLink}
	f.ExtendBaseWidget(f)
	return f
}

// setRuns changes the text; the next layout places it.
func (f *mdFlow) setRuns(runs []mdRun) {
	f.runs = runs
	f.laidOK = false
	if f.sel != nil {
		f.sel.reset() // the offsets were into the old text
	}
	f.relayout()
	f.Refresh()
}

func (f *mdFlow) setWidth(w float32) {
	if w == f.width {
		return
	}
	f.width = w
	f.laidOK = false
}

// chipPad is the room left and right of an inline code chip's text.
const chipPad = 3

// relayout places the runs at the flow's width, if that is not done yet.
func (f *mdFlow) relayout() {
	if f.laidOK && f.laidW == f.width {
		return
	}
	th := f.Theme()
	f.pieces, f.height = layoutFlow(f.runs, f.width, th.Size(f.size), th.Size(theme.SizeNameLineSpacing), f.bold, f.italic, f.maxLines)
	f.laidW, f.laidOK = f.width, true
}

// layoutFlow places runs in lines no wider than width (a word wider than the line breaks at any
// character) and returns the pieces, a line's same-styled words joined, and the height.
func layoutFlow(runs []mdRun, width, size, spacing float32, bold, italic bool, maxLines int) ([]flowPiece, float32) {
	if width <= 0 {
		width = 1e6
	}
	codeSize := size - 1
	lineH := fyne.MeasureText("Mg", size, fyne.TextStyle{Bold: bold}).Height
	var out, line []flowPiece
	x, y, lines := float32(0), float32(0), 0
	full := false
	brk, hard := flowWrap, 0 // what the next placed piece follows, when it starts a line
	newline := func() {
		if full {
			line = nil
			return
		}
		lines++
		if maxLines > 0 && lines > maxLines {
			full = true
			line = nil
			ellipsizeLast(out, width)
			return
		}
		for i := range line {
			line[i].y = y + (lineH-line[i].h)/2
		}
		out = append(out, mergePieces(line)...)
		line = nil
		y += lineH + spacing
		x = 0
	}
	place := func(p flowPiece) {
		p.x, p.line = x, lines
		if len(line) == 0 {
			p.brk, p.hard = brk, hard
		}
		brk, hard = flowWrap, 0
		line = append(line, p)
		x += p.w
	}
	for _, r := range runs {
		style := fyne.TextStyle{Bold: r.bold || bold, Italic: r.italic || italic, Strikethrough: r.strike, Underline: r.link != nil}
		if r.code {
			cs := fyne.TextStyle{Monospace: true, Bold: bold}
			text := strings.ReplaceAll(r.text, "\n", " ")
			ms := fyne.MeasureText(text, codeSize, cs)
			w := ms.Width + 2*chipPad
			if x > 0 && x+w > width {
				newline()
			}
			if w <= width {
				place(flowPiece{text: text, w: w, h: ms.Height, style: cs, size: codeSize, code: true})
				continue
			}
			for i, part := range wrapChars(text, width-2*chipPad, codeSize, cs) {
				if i > 0 || x > 0 {
					newline()
				}
				pm := fyne.MeasureText(part, codeSize, cs)
				place(flowPiece{text: part, w: pm.Width + 2*chipPad, h: pm.Height, style: cs, size: codeSize, code: true})
			}
			continue
		}
		space := fyne.MeasureText(" ", size, style).Width
		for li, seg := range strings.Split(r.text, "\n") {
			if li > 0 {
				newline()
				brk = flowHard
				hard++
			}
			for _, word := range splitWords(seg) {
				trimmed := strings.TrimRight(word, " ")
				ms := fyne.MeasureText(word, size, style)
				tw := ms.Width - space*float32(len(word)-len(trimmed))
				if x > 0 && x+tw > width {
					newline()
					if trimmed == "" {
						brk = flowSpace
						continue // no space at a line's start
					}
				}
				if tw <= width {
					place(flowPiece{text: word, w: ms.Width, h: ms.Height, style: style, size: size, link: r.link})
					continue
				}
				for i, part := range wrapChars(trimmed, width, size, style) { // a URL, a long name
					if i > 0 {
						newline()
					}
					pm := fyne.MeasureText(part, size, style)
					place(flowPiece{text: part, w: pm.Width, h: pm.Height, style: style, size: size, link: r.link})
				}
				if len(trimmed) < len(word) {
					place(flowPiece{text: " ", w: space, h: ms.Height, style: style, size: size})
				}
			}
		}
	}
	if len(line) > 0 {
		newline()
	}
	if len(out) == 0 {
		return nil, 0
	}
	return out, max(y-spacing, lineH)
}

// splitWords splits text into words that keep the spaces after them; leading spaces are a word.
func splitWords(text string) []string {
	var out []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] != ' ' {
			continue
		}
		j := i
		for j < len(text) && text[j] == ' ' {
			j++
		}
		out = append(out, text[start:j])
		start, i = j, j-1
	}
	if start < len(text) {
		out = append(out, text[start:])
	}
	return out
}

// wrapChars breaks text into parts no wider than width at any character, each at least one.
func wrapChars(text string, width, size float32, style fyne.TextStyle) []string {
	runes := []rune(text)
	var out []string
	for len(runes) > 0 {
		if width <= 0 || fyne.MeasureText(string(runes), size, style).Width <= width {
			out = append(out, string(runes))
			break
		}
		lo, hi := 1, len(runes)-1
		for lo < hi {
			mid := (lo + hi + 1) / 2
			if fyne.MeasureText(string(runes[:mid]), size, style).Width <= width {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		out = append(out, string(runes[:lo]))
		runes = runes[lo:]
	}
	return out
}

// mergePieces joins neighbouring pieces of a line that look the same into one text.
func mergePieces(line []flowPiece) []flowPiece {
	var out []flowPiece
	for _, p := range line {
		if n := len(out); n > 0 {
			last := &out[n-1]
			if !p.code && !last.code && last.style == p.style && last.size == p.size && last.link == p.link && last.x+last.w >= p.x-0.5 {
				last.text += p.text
				last.w = p.x + p.w - last.x
				last.h = max(last.h, p.h)
				continue
			}
		}
		out = append(out, p)
	}
	return out
}

// ellipsizeLast ends the last laid-out line with "…": the line limit cut the text.
func ellipsizeLast(out []flowPiece, width float32) {
	if len(out) == 0 {
		return
	}
	p := &out[len(out)-1]
	p.text = ellipsize(strings.TrimRight(p.text, " ")+" …", max(width-p.x, 1), p.size, p.style)
	if !strings.HasSuffix(p.text, "…") {
		p.text += "…"
	}
}

// Tapped implements fyne.Tappable: a click on a link opens it.
func (f *mdFlow) Tapped(ev *fyne.PointEvent) {
	if u := f.linkAt(ev.Position); u != nil && f.onLink != nil {
		f.onLink(u)
	}
}

func (f *mdFlow) linkAt(pos fyne.Position) *url.URL {
	for _, p := range f.pieces {
		if p.link != nil && pos.X >= p.x && pos.X <= p.x+p.w && pos.Y >= p.y-2 && pos.Y <= p.y+p.h+2 {
			return p.link
		}
	}
	return nil
}

// Cursor implements desktop.Cursorable: the hand over a link.
func (f *mdFlow) Cursor() desktop.Cursor {
	if f.overURL {
		return desktop.PointerCursor
	}
	return desktop.DefaultCursor
}

// MouseIn implements desktop.Hoverable.
func (f *mdFlow) MouseIn(ev *desktop.MouseEvent) { f.overURL = f.linkAt(ev.Position) != nil }

// MouseMoved implements desktop.Hoverable.
func (f *mdFlow) MouseMoved(ev *desktop.MouseEvent) { f.overURL = f.linkAt(ev.Position) != nil }

// MouseOut implements desktop.Hoverable.
func (f *mdFlow) MouseOut() { f.overURL = false }

// MinSize is the height of the text at the width the flow was told.
func (f *mdFlow) MinSize() fyne.Size {
	f.ExtendBaseWidget(f)
	f.relayout()
	return fyne.NewSize(0, f.height)
}

// CreateRenderer implements fyne.Widget.
func (f *mdFlow) CreateRenderer() fyne.WidgetRenderer {
	r := &mdFlowRenderer{f: f}
	r.Refresh()
	return r
}

type mdFlowRenderer struct {
	f      *mdFlow
	objs   []fyne.CanvasObject
	built  []flowPiece // the pieces objs were made for
	chips  []*canvas.Rectangle
	hls    []*canvas.Rectangle // the selection's highlights, one per piece, over the chips and under the text
	texts  []*canvas.Text
	tokens []fyne.ThemeColorName // each text's colour token
}

func (r *mdFlowRenderer) Layout(size fyne.Size) {
	if size.Width > 0 && size.Width != r.f.width && r.f.width == 0 {
		r.f.width = size.Width // a flow nobody told its width takes the one it gets
	}
	r.f.relayout()
	if !samePieces(r.built, r.f.pieces) {
		r.build()
	}
	if r.f.sel != nil {
		r.f.sel.Move(fyne.Position{})
		r.f.sel.Resize(size)
		r.f.placeSelection(r.hls)
	}
}

func samePieces(a, b []flowPiece) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// build makes the canvas objects of the laid-out pieces: the chips, the selection's highlights,
// the texts and the selection's area, in that order.
func (r *mdFlowRenderer) build() {
	f := r.f
	th := f.Theme()
	r.objs, r.chips, r.hls, r.texts, r.tokens = r.objs[:0], r.chips[:0], r.hls[:0], r.texts[:0], r.tokens[:0]
	for _, p := range f.pieces {
		tok := f.color
		x := p.x
		switch {
		case p.code:
			chip := canvas.NewRectangle(themeColor(th, colorChip))
			chip.CornerRadius = 3
			chip.Move(fyne.NewPos(p.x, p.y-1))
			chip.Resize(fyne.NewSize(p.w, p.h+2))
			r.chips = append(r.chips, chip)
			tok, x = colorCode, p.x+chipPad
		case p.link != nil:
			tok = colorLink
		}
		t := canvas.NewText(p.text, themeColor(th, tok))
		t.TextSize, t.TextStyle = p.size, p.style
		t.Move(fyne.NewPos(x, p.y))
		t.Resize(fyne.NewSize(p.w, p.h))
		r.texts = append(r.texts, t)
		r.tokens = append(r.tokens, tok)
	}
	for _, c := range r.chips {
		r.objs = append(r.objs, c)
	}
	if f.sel != nil {
		for range f.pieces {
			hl := newSelHighlight()
			r.hls = append(r.hls, hl)
			r.objs = append(r.objs, hl)
		}
	}
	for _, t := range r.texts {
		r.objs = append(r.objs, t)
	}
	if f.sel != nil {
		r.objs = append(r.objs, f.sel)
	}
	r.built = f.pieces
}

func (r *mdFlowRenderer) MinSize() fyne.Size { return r.f.MinSize() }

func (r *mdFlowRenderer) Refresh() {
	r.f.relayout()
	if !samePieces(r.built, r.f.pieces) {
		r.build()
	} else { // a theme change: the colours only
		th := r.f.Theme()
		for i, t := range r.texts {
			t.Color = themeColor(th, r.tokens[i])
		}
		for _, c := range r.chips {
			c.FillColor = themeColor(th, colorChip)
		}
	}
	if r.f.sel != nil {
		r.f.placeSelection(r.hls)
	}
	canvas.Refresh(r.f)
}

func (r *mdFlowRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *mdFlowRenderer) Destroy()                     {}

// shownText is the flow's text as drawn, line by line (tests).
func (f *mdFlow) shownText() string {
	f.relayout()
	var b strings.Builder
	lastY := float32(-1)
	for _, p := range f.pieces {
		if lastY >= 0 && p.y > lastY+1 {
			b.WriteByte('\n')
		}
		lastY = p.y
		b.WriteString(p.text)
	}
	return b.String()
}

// mdCode is a code block: monospace lines on a chip-coloured surface, broken at the width.
type mdCode struct {
	widget.BaseWidget
	text  string
	width float32
	lines []string
	laidW float32
	color fyne.ThemeColorName
	words bool     // prose, not code (an error's text): wrap at word boundaries, break a word only when it is wider than a line
	cont  []bool   // a line runs on from the one before: the width broke a line of the text
	sel   *selArea // the selection over the text (chat_select.go); nil: not selectable
}

func newMDCode(text string) *mdCode {
	c := &mdCode{text: text, color: theme.ColorNameForeground}
	c.ExtendBaseWidget(c)
	return c
}

func (c *mdCode) setWidth(w float32) { c.width = w }

func (c *mdCode) pad() float32 { return c.Theme().Size(theme.SizeNameInnerPadding) }

func (c *mdCode) textSize() float32 { return c.Theme().Size(theme.SizeNameText) - 1 }

func (c *mdCode) relayout() {
	if c.lines != nil && c.laidW == c.width {
		return
	}
	st := fyne.TextStyle{Monospace: true}
	w := c.width - 2*c.pad()
	c.lines, c.cont = c.lines[:0], c.cont[:0]
	for _, l := range strings.Split(strings.ReplaceAll(c.text, "\t", "    "), "\n") {
		var parts []string
		switch {
		case c.width <= 0:
			parts = []string{l}
		case c.words:
			parts = wrapHard(l, w, c.textSize(), st)
		default:
			parts = wrapChars(l, w, c.textSize(), st)
		}
		if l == "" && c.width > 0 {
			parts = append(parts, "")
		}
		for i, part := range parts {
			c.lines = append(c.lines, part)
			c.cont = append(c.cont, i > 0)
		}
	}
	c.laidW = c.width
}

func (c *mdCode) lineHeight() float32 {
	return fyne.MeasureText("Mg", c.textSize(), fyne.TextStyle{Monospace: true}).Height + 1
}

// MinSize implements fyne.CanvasObject.
func (c *mdCode) MinSize() fyne.Size {
	c.ExtendBaseWidget(c)
	c.relayout()
	return fyne.NewSize(0, float32(len(c.lines))*c.lineHeight()+2*c.pad())
}

// CreateRenderer implements fyne.Widget.
func (c *mdCode) CreateRenderer() fyne.WidgetRenderer {
	r := &mdCodeRenderer{c: c, bg: canvas.NewRectangle(color.Transparent)}
	r.Refresh()
	return r
}

type mdCodeRenderer struct {
	c     *mdCode
	bg    *canvas.Rectangle
	hls   []*canvas.Rectangle // the selection's highlights, one per line
	texts []*canvas.Text
	objs  []fyne.CanvasObject
}

func (r *mdCodeRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	pad, lh := r.c.pad(), r.c.lineHeight()
	for i, t := range r.texts {
		t.Move(fyne.NewPos(pad, pad+float32(i)*lh))
	}
	if r.c.sel != nil {
		r.c.sel.Move(fyne.Position{})
		r.c.sel.Resize(size)
		r.c.placeSelection(r.hls)
	}
}

func (r *mdCodeRenderer) MinSize() fyne.Size { return r.c.MinSize() }

func (r *mdCodeRenderer) Refresh() {
	c := r.c
	c.relayout()
	th := c.Theme()
	r.bg.FillColor = themeColor(th, colorChip)
	if c.color == colorStatusError { // an error: its own colour at about 8 %, not a red box
		r.bg.FillColor = withAlpha(themeColor(th, colorStatusError), 0x14)
	}
	r.bg.CornerRadius = th.Size(theme.SizeNameInputRadius)
	fg := themeColor(th, c.color)
	if len(r.texts) != len(c.lines) {
		r.texts, r.hls = r.texts[:0], r.hls[:0]
		for range c.lines {
			r.texts = append(r.texts, canvas.NewText("", fg))
			if c.sel != nil {
				r.hls = append(r.hls, newSelHighlight())
			}
		}
	}
	r.objs = append(r.objs[:0], r.bg)
	for _, hl := range r.hls {
		r.objs = append(r.objs, hl)
	}
	for i, t := range r.texts {
		t.Text, t.Color, t.TextSize, t.TextStyle = c.lines[i], fg, c.textSize(), fyne.TextStyle{Monospace: true}
		r.objs = append(r.objs, t)
	}
	if c.sel != nil {
		r.objs = append(r.objs, c.sel)
	}
	r.Layout(c.Size())
	canvas.Refresh(c)
}

func (r *mdCodeRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *mdCodeRenderer) Destroy()                     {}

// mdListView is a list: each item's marker ("•", "1.") and its blocks, indented.
type mdListView struct {
	widget.BaseWidget
	markers []*textLine
	items   []*mdText
	objs    []fyne.CanvasObject
	indent  float32
}

func newMDList(l *mdList, color fyne.ThemeColorName, hooks mdHooks) *mdListView {
	v := &mdListView{}
	n := l.start
	widest := ""
	for _, it := range l.items {
		mark := "•"
		if l.ordered {
			mark = strconv.Itoa(n) + "."
			n++
		}
		if len(mark) > len(widest) {
			widest = mark
		}
		m := newTextLine(mark, color, theme.SizeNameText, fyne.TextStyle{Bold: l.ordered})
		m.align = fyne.TextAlignTrailing
		t := newMDText(color, hooks)
		t.gapX = 1
		t.setBlocks(it)
		v.markers = append(v.markers, m)
		v.items = append(v.items, t)
		v.objs = append(v.objs, m, t)
	}
	v.ExtendBaseWidget(v)
	v.indent = fyne.MeasureText(widest+"0", v.Theme().Size(theme.SizeNameText), fyne.TextStyle{Bold: true}).Width + v.Theme().Size(theme.SizeNamePadding)
	return v
}

func (v *mdListView) setWidth(w float32) {
	for _, it := range v.items {
		it.setWidth(w - v.indent)
	}
}

func (v *mdListView) gap() float32 { return v.Theme().Size(theme.SizeNamePadding) }

// MinSize implements fyne.CanvasObject.
func (v *mdListView) MinSize() fyne.Size {
	var h float32
	for i, it := range v.items {
		if i > 0 {
			h += v.gap()
		}
		h += max(it.MinSize().Height, v.markers[i].MinSize().Height)
	}
	return fyne.NewSize(0, h)
}

// CreateRenderer implements fyne.Widget.
func (v *mdListView) CreateRenderer() fyne.WidgetRenderer { return &mdListRenderer{v: v} }

type mdListRenderer struct{ v *mdListView }

func (r *mdListRenderer) Layout(size fyne.Size) {
	v := r.v
	pad := v.Theme().Size(theme.SizeNamePadding)
	y := float32(0)
	for i, it := range v.items {
		m := v.markers[i]
		h := it.MinSize().Height
		mh := m.MinSize().Height
		m.Move(fyne.NewPos(0, y))
		m.Resize(fyne.NewSize(v.indent-pad, mh))
		it.Move(fyne.NewPos(v.indent, y))
		it.Resize(fyne.NewSize(max(size.Width-v.indent, 0), h))
		y += max(h, mh) + v.gap()
	}
}

func (r *mdListRenderer) MinSize() fyne.Size           { return r.v.MinSize() }
func (r *mdListRenderer) Refresh()                     { r.Layout(r.v.Size()); canvas.Refresh(r.v) }
func (r *mdListRenderer) Objects() []fyne.CanvasObject { return r.v.objs }
func (r *mdListRenderer) Destroy()                     {}

// mdTableView is a GitHub table as a grid: thin borders, the header row bold on a header surface,
// each cell's text wrapping in its column. Columns are as wide as their text up to a fair share
// of the width: narrow columns keep their width, the wide ones share what is left and wrap.
type mdTableView struct {
	widget.BaseWidget
	cells  [][]*mdFlow // rows, the header first when there is one
	header bool
	align  []fyne.TextAlign
	width  float32

	cols    []float32
	rowsH   []float32
	headBg  *surface
	border  *canvas.Rectangle
	lines   []*canvas.Rectangle
	objs    []fyne.CanvasObject
	laidW   float32
	natural []float32 // each column's width with its cells on one line
	words   []float32 // each column's longest word, which it keeps whole when the table can
}

func newMDTable(t *mdTable, fg fyne.ThemeColorName, hooks mdHooks) *mdTableView {
	v := &mdTableView{align: t.align, header: len(t.header) > 0}
	cols := len(t.header)
	for _, r := range t.rows {
		cols = max(cols, len(r))
	}
	row := func(cells [][]mdRun, bold bool) []*mdFlow {
		out := make([]*mdFlow, cols)
		for c := range out {
			var runs []mdRun
			if c < len(cells) {
				runs = cells[c]
			}
			f := newMDFlow(runs, fg, hooks.link)
			f.bold = bold
			if hooks.copy != nil {
				f.selectable(hooks.copy)
			}
			out[c] = f
		}
		return out
	}
	if v.header {
		v.cells = append(v.cells, row(t.header, true))
	}
	for _, r := range t.rows {
		v.cells = append(v.cells, row(r, false))
	}
	v.headBg = newSurface(colorDrawerHeader)
	v.border = canvas.NewRectangle(color.Transparent)
	v.ExtendBaseWidget(v)
	return v
}

func (v *mdTableView) setWidth(w float32) {
	if w == v.width {
		return
	}
	v.width = w
	v.laidW = -1
}

func (v *mdTableView) cellPad() float32 { return v.Theme().Size(theme.SizeNameInnerPadding) }

// relayout sizes the columns for the width and the rows for their cells.
func (v *mdTableView) relayout() {
	if v.laidW == v.width && v.cols != nil {
		return
	}
	pad := v.cellPad()
	ncols := 0
	if len(v.cells) > 0 {
		ncols = len(v.cells[0])
	}
	if v.natural == nil {
		v.natural, v.words = make([]float32, ncols), make([]float32, ncols)
		size := v.Theme().Size(theme.SizeNameText)
		for _, row := range v.cells {
			for c, f := range row {
				pieces, _ := layoutFlow(f.runs, 0, size, 0, f.bold, false, 0)
				w := float32(0)
				for _, p := range pieces {
					w = max(w, p.x+p.w)
				}
				v.natural[c] = max(v.natural[c], w+2*pad+1)
				v.words[c] = max(v.words[c], longestWord(f.runs, size, f.bold)+2*pad+1)
			}
		}
	}
	avail := v.width - 1
	if v.width <= 0 {
		avail = 1e6
	}
	v.cols = keepWords(fairShare(v.natural, avail, 2*pad+24), v.words)
	v.rowsH = v.rowsH[:0]
	for _, row := range v.cells {
		h := float32(0)
		for c, f := range row {
			f.setWidth(v.cols[c] - 2*pad)
			h = max(h, f.MinSize().Height)
		}
		v.rowsH = append(v.rowsH, h+2*pad)
	}
	v.laidW = v.width
}

// fairShare gives each column its natural width while the columns fit; otherwise the narrow ones
// keep theirs and the others share the rest equally, never below least.
func fairShare(natural []float32, avail, least float32) []float32 {
	out := append([]float32(nil), natural...)
	total := float32(0)
	for _, w := range natural {
		total += w
	}
	if total <= avail || len(natural) == 0 {
		return out
	}
	open := len(natural)
	fixed := make([]bool, len(natural))
	left := avail
	for {
		share := left / float32(open)
		changed := false
		for i, w := range natural {
			if !fixed[i] && w <= share {
				fixed[i], out[i] = true, w
				left -= w
				open--
				changed = true
			}
		}
		if !changed || open == 0 {
			break
		}
	}
	if open > 0 {
		share := max(left/float32(open), least)
		for i := range natural {
			if !fixed[i] {
				out[i] = share
			}
		}
	}
	return out
}

// keepWords widens the columns narrower than their longest word with the room of the columns that
// have more than theirs, so that "CrashLoopBackOff" is not broken while a column of prose could wrap
// one more line instead.
func keepWords(cols, words []float32) []float32 {
	for c := range cols {
		need := words[c] - cols[c]
		if need <= 0 {
			continue
		}
		slack := float32(0)
		for o := range cols {
			if o != c {
				slack += max(cols[o]-max(words[o], 0), 0)
			}
		}
		take := min(need, slack)
		if take <= 0 {
			continue
		}
		for o := range cols {
			if s := max(cols[o]-words[o], 0); o != c && s > 0 {
				cols[o] -= take * s / slack
			}
		}
		cols[c] += take
	}
	return cols
}

// longestWord is the widest word of runs, an inline code chip counting whole.
func longestWord(runs []mdRun, size float32, bold bool) float32 {
	w := float32(0)
	for _, r := range runs {
		if r.code {
			w = max(w, fyne.MeasureText(r.text, size-1, fyne.TextStyle{Monospace: true, Bold: bold}).Width+2*chipPad)
			continue
		}
		st := fyne.TextStyle{Bold: r.bold || bold, Italic: r.italic}
		for _, word := range strings.Fields(r.text) {
			w = max(w, fyne.MeasureText(word, size, st).Width)
		}
	}
	return w
}

// MinSize implements fyne.CanvasObject.
func (v *mdTableView) MinSize() fyne.Size {
	v.ExtendBaseWidget(v)
	v.relayout()
	h := float32(1)
	for _, rh := range v.rowsH {
		h += rh
	}
	return fyne.NewSize(0, h)
}

func (v *mdTableView) tableWidth() float32 {
	w := float32(1)
	for _, c := range v.cols {
		w += c
	}
	return w
}

// CreateRenderer implements fyne.Widget.
func (v *mdTableView) CreateRenderer() fyne.WidgetRenderer {
	r := &mdTableRenderer{v: v}
	r.Refresh()
	return r
}

type mdTableRenderer struct{ v *mdTableView }

func (r *mdTableRenderer) Layout(fyne.Size) {
	v := r.v
	v.relayout()
	th := v.Theme()
	pad := v.cellPad()
	tw := v.tableWidth()
	line := themeColor(th, colorHairline)
	nLines := max(len(v.rowsH)-1, 0) + max(len(v.cols)-1, 0)
	for len(v.lines) < nLines {
		v.lines = append(v.lines, canvas.NewRectangle(line))
	}
	v.objs = v.objs[:0]
	if v.header && len(v.rowsH) > 0 {
		v.headBg.Move(fyne.NewPos(0, 0))
		v.headBg.Resize(fyne.NewSize(tw, v.rowsH[0]))
		v.objs = append(v.objs, v.headBg)
	}
	li := 0
	y := float32(0)
	for ri, row := range v.cells {
		x := float32(0)
		for c, f := range row {
			fw := v.cols[c] - 2*pad
			fh := f.MinSize().Height
			fx := x + pad
			if c < len(v.align) {
				used := float32(0)
				for _, p := range f.pieces {
					used = max(used, p.x+p.w)
				}
				switch v.align[c] {
				case fyne.TextAlignTrailing:
					fx += max(fw-used, 0)
				case fyne.TextAlignCenter:
					fx += max(fw-used, 0) / 2
				}
			}
			f.Move(fyne.NewPos(fx, y+pad))
			f.Resize(fyne.NewSize(fw, fh))
			v.objs = append(v.objs, f)
			x += v.cols[c]
		}
		y += v.rowsH[ri]
		if ri < len(v.rowsH)-1 {
			l := v.lines[li]
			l.FillColor = line
			l.Move(fyne.NewPos(0, y))
			l.Resize(fyne.NewSize(tw, 1))
			v.objs = append(v.objs, l)
			li++
		}
	}
	x := float32(0)
	for c := 0; c < len(v.cols)-1; c++ {
		x += v.cols[c]
		l := v.lines[li]
		l.FillColor = line
		l.Move(fyne.NewPos(x, 0))
		l.Resize(fyne.NewSize(1, y))
		v.objs = append(v.objs, l)
		li++
	}
	v.border.StrokeColor, v.border.StrokeWidth, v.border.FillColor = line, 1, color.Transparent
	v.border.CornerRadius = 2
	v.border.Move(fyne.NewPos(0, 0))
	v.border.Resize(fyne.NewSize(tw, y+1))
	v.objs = append(v.objs, v.border)
}

func (r *mdTableRenderer) MinSize() fyne.Size { return r.v.MinSize() }

func (r *mdTableRenderer) Refresh() {
	r.Layout(r.v.Size())
	canvas.Refresh(r.v)
}

func (r *mdTableRenderer) Objects() []fyne.CanvasObject { return r.v.objs }
func (r *mdTableRenderer) Destroy()                     {}
