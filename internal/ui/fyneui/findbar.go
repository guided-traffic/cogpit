package fyneui

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/ui/fyneui/yamlview"
)

// The editor's find widget, as Lens's Helm Upgrade tab opens one at the editor's bottom (captured
// 2026-10-12): a search field with match case, whole word and regular expression, a replace field,
// previous and next, and "1 of 1". Cogpit's reading of the details the capture does not show: a
// match lies within one line, a regular expression applies to each line on its own (so ^ and $ are
// a line's ends), a replacement in a regular expression may use $1 and ${name}, the matches are
// capped, and the match is the editor's selection (no highlight of the others).

// findOptions are the find widget's three switches.
type findOptions struct {
	matchCase, wholeWord, regex bool
}

// findMatch is one match: a 0-based line and the rune columns [start, end) of the match in it.
type findMatch struct{ line, start, end int }

// maxFindMatches caps the matches one search lists; the editor holds values, not logs.
const maxFindMatches = 5000

// maxFindQuery is the longest search text taken.
const maxFindQuery = 1000

// compile turns the search text into a regular expression per the options.
func (o findOptions) compile(query string) (*regexp.Regexp, error) {
	if len(query) > maxFindQuery {
		return nil, fmt.Errorf("the search text is longer than %d bytes", maxFindQuery)
	}
	p := query
	if !o.regex {
		p = regexp.QuoteMeta(query)
	}
	if o.wholeWord {
		p = `\b(?:` + p + `)\b`
	}
	if !o.matchCase {
		p = `(?i)` + p
	}
	return regexp.Compile(p)
}

// findMatches lists the non-empty matches of the search text in text, in document order, at most
// maxFindMatches.
func findMatches(text, query string, o findOptions) ([]findMatch, error) {
	if query == "" {
		return nil, nil
	}
	re, err := o.compile(query)
	if err != nil {
		return nil, err
	}
	var out []findMatch
	for i, line := range strings.Split(text, "\n") {
		for _, loc := range re.FindAllStringIndex(line, -1) {
			if loc[0] == loc[1] {
				continue
			}
			out = append(out, findMatch{i, utf8.RuneCountInString(line[:loc[0]]), utf8.RuneCountInString(line[:loc[1]])})
			if len(out) >= maxFindMatches {
				return out, nil
			}
		}
	}
	return out, nil
}

// replaceAllIn replaces every match in text and returns the new text and how many were replaced.
func replaceAllIn(text, query, repl string, o findOptions) (string, int, error) {
	if query == "" {
		return text, 0, nil
	}
	re, err := o.compile(query)
	if err != nil {
		return text, 0, err
	}
	lines := strings.Split(text, "\n")
	n := 0
	for i, line := range lines {
		for _, loc := range re.FindAllStringIndex(line, -1) {
			if loc[0] != loc[1] {
				n++
			}
		}
		if o.regex {
			lines[i] = re.ReplaceAllString(line, repl)
		} else {
			lines[i] = re.ReplaceAllLiteralString(line, repl)
		}
	}
	return strings.Join(lines, "\n"), n, nil
}

// findBar is the find widget of one editor.
type findBar struct {
	sh   *shell
	view *yamlview.View

	find          *searchEntry // F1–F12 are the hotbar's (A64): a search field, not the editor's text
	replace       *widget.Entry
	opts          findOptions
	toggles       [3]*tipButton // match case, whole word, regular expression
	count         *textLine
	prev, next    *tipButton
	replaceBtn    *widget.Button
	replaceAllBtn *widget.Button
	root          fyne.CanvasObject

	matches []findMatch
	cur     int    // index into matches of the selected one; -1 when none is
	text    string // the text matches was computed from
}

func newFindBar(sh *shell, view *yamlview.View) *findBar {
	b := &findBar{sh: sh, view: view, cur: -1}
	view.OnFind = func() { b.open() } // ⌘F in the editor (findkeys.go)
	b.find = newSearchEntry(sh)
	b.find.SetPlaceHolder("Find")
	b.find.OnChanged = func(string) { b.refresh(true) }
	b.find.OnSubmitted = func(string) { b.step(1) }
	b.replace = widget.NewEntry()
	b.replace.SetPlaceHolder("Replace")
	b.replace.OnSubmitted = func(string) { b.replaceOne() }

	flags := []struct {
		label, tip string
		set        func(bool)
	}{
		{"Aa", "Match case", func(on bool) { b.opts.matchCase = on }},
		{"ab", "Whole word", func(on bool) { b.opts.wholeWord = on }},
		{".*", "Regular expression", func(on bool) { b.opts.regex = on }},
	}
	toggles := make([]fyne.CanvasObject, len(flags), len(flags)+4) // the row's other widgets follow
	for i, f := range flags {
		t := newTipButton(sh, nil, f.tip, nil)
		t.Text = f.label
		on := false
		t.OnTapped = func() {
			on = !on
			f.set(on)
			if on {
				t.Importance = widget.HighImportance
			} else {
				t.Importance = widget.LowImportance
			}
			t.Refresh()
			b.refresh(true)
		}
		b.toggles[i] = t
		toggles[i] = t
	}
	b.count = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	b.prev = newTipButton(sh, theme.MoveUpIcon(), "Previous match", func() { b.step(-1) })
	b.next = newTipButton(sh, theme.MoveDownIcon(), "Next match", func() { b.step(1) })
	closeBtn := newTipButton(sh, theme.CancelIcon(), "Close", b.close)
	b.replaceBtn = widget.NewButton("Replace", b.replaceOne)
	b.replaceAllBtn = widget.NewButton("Replace all", b.replaceEvery)

	findRow := container.NewBorder(nil, nil, nil,
		container.NewHBox(append(append(toggles, container.NewCenter(sized(sizeFindCountWidth, "", b.count))), b.prev, b.next, closeBtn)...), b.find)
	replaceRow := container.NewBorder(nil, nil, nil, container.NewHBox(b.replaceBtn, b.replaceAllBtn), b.replace)
	b.root = container.NewVBox(newDivider(false),
		inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, container.NewVBox(findRow, replaceRow)))
	b.root.Hide()
	return b
}

// open shows the widget and puts the cursor in the search field. A single line the user selected in
// the editor becomes the search text.
func (b *findBar) open() {
	if sel := b.view.SelectedText(); sel != "" && !strings.Contains(sel, "\n") && sel != b.find.Text {
		b.find.SetText(sel)
	}
	b.root.Show()
	if c := fyne.CurrentApp().Driver().CanvasForObject(b.find); c != nil {
		c.Focus(b.find)
	}
	b.refresh(true)
}

// close hides the widget and gives the editor the focus back.
func (b *findBar) close() {
	b.root.Hide()
	if c := fyne.CurrentApp().Driver().CanvasForObject(b.view); c != nil {
		c.Focus(b.view)
	}
}

// visible reports whether the widget is shown.
func (b *findBar) visible() bool { return b.root.Visible() }

// refresh lists the matches again; with select set it selects the first one at or after the caret.
func (b *findBar) refresh(selectFirst bool) {
	b.text = b.view.Text()
	ms, err := findMatches(b.text, b.find.Text, b.opts)
	b.matches, b.cur = ms, -1
	switch {
	case err != nil:
		b.count.SetText("Invalid")
		b.count.SetColor(colorStatusError)
		b.matches = nil
		return
	case b.find.Text == "":
		b.count.SetText("")
		return
	case len(ms) == 0:
		b.count.SetText("No results")
		b.count.SetColor(colorStatusWarning)
		return
	}
	b.count.SetColor(colorMuted)
	if selectFirst {
		line, col := b.view.CursorPosition()
		i := 0
		for j, m := range ms {
			if m.line > line || (m.line == line && m.start >= col) {
				i = j
				break
			}
		}
		b.show(i)
		return
	}
	b.count.SetText(fmt.Sprintf("%d found", len(ms)))
}

// show selects match i in the editor and says which one it is.
func (b *findBar) show(i int) {
	m := b.matches[i]
	b.cur = i
	b.view.SelectRange(m.line, m.start, m.line, m.end)
	n := fmt.Sprint(len(b.matches))
	if len(b.matches) >= maxFindMatches {
		n += "+"
	}
	b.count.SetText(fmt.Sprintf("%d of %s", i+1, n))
}

// step moves to the next (1) or previous (-1) match, wrapping. When the text changed since the
// matches were listed they are listed again and the step starts from the caret.
func (b *findBar) step(dir int) {
	if b.view.Text() != b.text || b.cur < 0 {
		b.refresh(false)
		if len(b.matches) == 0 {
			return
		}
		line, col := b.view.CursorPosition()
		i := -1
		if dir > 0 {
			for j, m := range b.matches {
				if m.line > line || (m.line == line && m.start >= col) {
					i = j
					break
				}
			}
			if i < 0 {
				i = 0
			}
		} else {
			for j := len(b.matches) - 1; j >= 0; j-- {
				if m := b.matches[j]; m.line < line || (m.line == line && m.start < col) {
					i = j
					break
				}
			}
			if i < 0 {
				i = len(b.matches) - 1
			}
		}
		b.show(i)
		return
	}
	b.show((b.cur + dir + len(b.matches)) % len(b.matches))
}

// selectedIsCurrent reports whether the editor's selection is the match the widget selected.
func (b *findBar) selectedIsCurrent() bool {
	if b.cur < 0 || b.cur >= len(b.matches) || b.view.Text() != b.text {
		return false
	}
	m := b.matches[b.cur]
	lines := strings.Split(b.text, "\n")
	r := []rune(lines[m.line])
	return b.view.SelectedText() == string(r[m.start:m.end])
}

// replaceOne replaces the selected match, then selects the next one; with no match selected it
// selects one first.
func (b *findBar) replaceOne() {
	if !b.selectedIsCurrent() {
		b.step(1)
		return
	}
	repl := b.replace.Text
	if b.opts.regex {
		if re, err := b.opts.compile(b.find.Text); err == nil {
			if sel := b.view.SelectedText(); re.MatchString(sel) {
				repl = re.ReplaceAllString(sel, repl)
			}
		}
	}
	b.view.ReplaceSelection(repl)
	b.refresh(true)
}

// replaceEvery replaces every match and says how many.
func (b *findBar) replaceEvery() {
	text := b.view.Text()
	out, n, err := replaceAllIn(text, b.find.Text, b.replace.Text, b.opts)
	if err != nil || n == 0 || out == text {
		b.refresh(false)
		return
	}
	b.view.SelectAll()
	b.view.ReplaceSelection(out)
	b.view.SetCursorPosition(0, 0)
	b.refresh(false)
	b.count.SetText(fmt.Sprintf("%d replaced", n))
}
