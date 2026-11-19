package engine

import (
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"strings"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The search field's toggles (ui-parity › Tables, G5): match case ("Aa") and regular expression
// (".*"), captured from Lens Desktop without their rules. Cogpit's reading, inferred (Cogpit's
// defaults, not the owner's decision):
//   - both off: every whitespace-separated term must occur in one of the row's cells, ignoring
//     case, as the search always did;
//   - match case: the same terms, their case kept;
//   - regular expression: the whole text is one pattern in Go's RE2 syntax, taken as typed
//     (leading and trailing spaces are part of it), and a row matches when the pattern matches in
//     one of its cells; without match case it ignores case;
//   - a pattern that does not compile, or is longer than MaxFilterPattern bytes, filters nothing
//     out: the view shows every row while the search field says why (FilterError), so that a
//     pattern half typed does not empty the table;
//   - the toggles belong to the tab, like its search text, and stay when the view changes and the
//     text is cleared.
//
// RE2 matches in time linear in the text, so a pattern cannot make a view's rebuild hang; the
// bound on its length bounds the compile and the program, and regexp refuses repetitions above
// 1000 and programs too large on its own.

// FilterOptions are the search field's toggles, per tab (UIState.FilterOptions).
type FilterOptions struct {
	// MatchCase keeps the search's case: "Web" does not match "web".
	MatchCase bool `json:"matchCase"`
	// Regex takes the search text as one regular expression (RE2 syntax).
	Regex bool `json:"regex"`
}

// MaxFilterPattern bounds a regular expression's length in bytes (Cogpit's, as the editor's find).
const MaxFilterPattern = 1000

// RowMatcher decides which rows a view's search keeps. Its zero value keeps every row.
type RowMatcher struct {
	terms     []string       // the terms, lowered unless matchCase
	matchCase bool           // terms keep their case
	re        *regexp.Regexp // the pattern, in regex mode
}

// NewRowMatcher compiles the search text with its options. An invalid pattern returns the zero
// matcher, which keeps every row, and the reason.
func NewRowMatcher(text string, o FilterOptions) (RowMatcher, error) {
	if o.Regex {
		if text == "" {
			return RowMatcher{}, nil
		}
		re, err := compileFilter(text, o.MatchCase)
		if err != nil {
			return RowMatcher{}, err
		}
		return RowMatcher{re: re}, nil
	}
	if !o.MatchCase {
		text = strings.ToLower(text)
	}
	return RowMatcher{terms: strings.Fields(text), matchCase: o.MatchCase}, nil
}

// compileFilter compiles a search pattern: bounded, case-insensitive unless matchCase. The pattern
// is compiled as typed first, so that an error quotes the user's text and not the (?i) added to it.
func compileFilter(pattern string, matchCase bool) (*regexp.Regexp, error) {
	if len(pattern) > MaxFilterPattern {
		return nil, fmt.Errorf("the regular expression is longer than %d bytes", MaxFilterPattern)
	}
	re, err := regexp.Compile(pattern)
	if err == nil && !matchCase {
		re, err = regexp.Compile("(?i)" + pattern)
	}
	if err != nil {
		var se *syntax.Error
		if errors.As(err, &se) {
			return nil, fmt.Errorf("invalid regular expression: %s in `%s`", se.Code, se.Expr)
		}
		return nil, fmt.Errorf("invalid regular expression: %w", err)
	}
	return re, nil
}

// All reports whether the matcher keeps every row: no terms and no pattern.
func (m RowMatcher) All() bool { return m.re == nil && len(m.terms) == 0 }

// Match reports whether the matcher keeps row.
func (m RowMatcher) Match(row model.Row) bool {
	switch {
	case m.re != nil:
		for _, c := range row.Cells {
			if m.re.MatchString(c.Text) {
				return true
			}
		}
		return false
	case len(m.terms) == 0:
		return true
	}
	var cells []string // each cell lowered once, not once per term
	if !m.matchCase {
		cells = make([]string, len(row.Cells))
		for i, c := range row.Cells {
			cells[i] = strings.ToLower(c.Text)
		}
	}
	for _, term := range m.terms {
		found := false
		for i, c := range row.Cells {
			text := c.Text
			if cells != nil {
				text = cells[i]
			}
			if strings.Contains(text, term) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// FilterError says why a search text with its options filters nothing out: a regular expression
// that does not compile or is too long; "" when the search applies.
func FilterError(text string, o FilterOptions) string {
	if _, err := NewRowMatcher(text, o); err != nil {
		return err.Error()
	}
	return ""
}

// MatchesFilter reports whether a row matches a view's search text with its options; an invalid
// pattern matches every row (FilterError says why).
func MatchesFilter(row model.Row, text string, o FilterOptions) bool {
	m, _ := NewRowMatcher(text, o)
	return m.Match(row)
}

// matcher is the view's search as a RowMatcher, and why it filters nothing out ("" when it
// applies). Terms ignore surrounding spaces; a regular expression is taken as typed.
func (p viewParams) matcher() (RowMatcher, string) {
	text := p.filter
	if !p.filterOpts.Regex {
		text = strings.TrimSpace(text)
	}
	m, err := NewRowMatcher(text, p.filterOpts)
	if err != nil {
		return m, err.Error()
	}
	return m, ""
}
