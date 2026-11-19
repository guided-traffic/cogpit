package fyneui

import (
	"strings"

	"fyne.io/fyne/v2"
)

// nameBreaks are the characters after which a name without spaces wraps: the separators of
// Kubernetes names, label keys and Config Map keys ("agent-not-ready-taint-key",
// "node.cilium.io/agent-not-ready", "MAX_CONNECTIONS").
const nameBreaks = "-._/"

// wrapName wraps a name to width (the owner's F3: a key without spaces ran under its value): at its
// spaces as wrapLines does; a word still wider than width after one of nameBreaks, as many of its
// parts on a line as fit; a part wider than width alone by characters (wrapHard). A name that fits
// is one line, as before.
func wrapName(text string, width, size float32, style fyne.TextStyle) []string {
	if width <= 0 {
		return []string{text}
	}
	var out []string
	for _, line := range wrapLines(text, width, size, style) {
		if fyne.MeasureText(line, size, style).Width <= width {
			out = append(out, line)
			continue
		}
		// wrapLines leaves a word wider than width on a line of its own.
		out = append(out, breakWord(line, width, size, style)...)
	}
	return out
}

// breakWord breaks a word wider than width after its nameBreaks, filling each line with as many
// parts as fit; a part wider than width breaks by characters and the next part goes on from its
// rest.
func breakWord(word string, width, size float32, style fyne.TextStyle) []string {
	fits := func(s string) bool { return fyne.MeasureText(s, size, style).Width <= width }
	var out []string
	line := ""
	for _, part := range splitAfter(word, nameBreaks) {
		if line != "" && !fits(line+part) {
			out = append(out, line)
			line = ""
		}
		if line == "" && !fits(part) {
			pieces := wrapHard(part, width, size, style)
			out = append(out, pieces[:len(pieces)-1]...)
			line = pieces[len(pieces)-1]
			continue
		}
		line += part
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}

// splitAfter splits s after each of the characters in seps: "a-b.c" → "a-", "b.", "c".
func splitAfter(s, seps string) []string {
	var out []string
	start := 0
	for i, r := range s {
		if strings.ContainsRune(seps, r) {
			out = append(out, s[start:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// newNameParagraph is a name wrapped to width by wrapName.
func newNameParagraph(text string, c fyne.ThemeColorName, width float32) *paragraph {
	p := newParagraph(text, c, width)
	p.names = true
	return p
}
