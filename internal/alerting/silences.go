package alerting

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Silences (D-166): Alertmanager's own mutes, which Cogpit lists, creates and expires on the user's
// request. A silence is a set of label matchers and a time range; an alert all of whose labels
// match is not notified while the silence is active. Cogpit keeps no silence of its own: what the
// Alertmanager answers is what the silences page shows.

// silencesPath lists the silences (GET) and creates one (POST); silencePath expires one (DELETE
// /api/v2/silence/<id>). Alertmanager API v2.
const (
	silencesPath = "/api/v2/silences"
	silencePath  = "/api/v2/silence/"
)

// SilenceState is a silence's state as Alertmanager answers it.
type SilenceState string

// The states of a silence.
const (
	SilenceActive  SilenceState = "active"  // it mutes the alerts it matches now
	SilencePending SilenceState = "pending" // it starts later
	SilenceExpired SilenceState = "expired" // it ended, or was expired; kept for Alertmanager's retention
)

// Rank orders the states as the silences page lists them: active, pending, expired.
func (s SilenceState) Rank() int {
	switch s {
	case SilenceActive:
		return 0
	case SilencePending:
		return 1
	}
	return 2
}

// Matcher is one of a silence's label matchers: Name Op Value, Op one of =, !=, =~ and !~ (IsRegex
// and IsEqual in Alertmanager's terms). A regular expression is anchored at both ends, as
// Alertmanager anchors it, and a label an alert lacks has the empty value.
type Matcher struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	IsRegex bool   `json:"isRegex"`
	IsEqual bool   `json:"isEqual"`
}

// The operators of a matcher.
const (
	OpEqual     = "="
	OpNotEqual  = "!="
	OpRegex     = "=~"
	OpNotRegex  = "!~"
	maxMatchers = 64      // per silence
	maxComment  = 4 << 10 // a comment's bytes
	maxAuthor   = 256     // createdBy's bytes
	maxName     = 256     // a matcher's label name
	maxSilences = 5000    // kept of one answer
)

// Ops are the operators in the order a form offers them.
var Ops = []string{OpEqual, OpNotEqual, OpRegex, OpNotRegex}

// NewMatcher makes a matcher from its operator ("=", "!=", "=~", "!~"; "" is "=").
func NewMatcher(name, op, value string) (Matcher, error) {
	m := Matcher{Name: strings.TrimSpace(name), Value: value}
	switch op {
	case OpEqual, "":
		m.IsEqual = true
	case OpNotEqual:
	case OpRegex:
		m.IsEqual, m.IsRegex = true, true
	case OpNotRegex:
		m.IsRegex = true
	default:
		return Matcher{}, fmt.Errorf("matcher %s: operator %q: one of =, !=, =~, !~", m.Name, op)
	}
	return m, m.Validate()
}

// Op is the matcher's operator.
func (m Matcher) Op() string {
	switch {
	case m.IsRegex && m.IsEqual:
		return OpRegex
	case m.IsRegex:
		return OpNotRegex
	case m.IsEqual:
		return OpEqual
	}
	return OpNotEqual
}

// String is the matcher as Alertmanager's and karma's filters write it: name="value", the value
// quoted.
func (m Matcher) String() string {
	return m.Name + m.Op() + fmt.Sprintf("%q", m.Value)
}

// Validate reports a matcher Alertmanager would refuse: no label name, a name too long or not
// UTF-8, a regular expression that does not compile.
func (m Matcher) Validate() error {
	switch {
	case m.Name == "":
		return errors.New("a matcher needs a label name")
	case len(m.Name) > maxName || !utf8.ValidString(m.Name) || strings.ContainsAny(m.Name, "\n\r\t\"{}=!~,"):
		return fmt.Errorf("matcher %.40q: not a label name", m.Name)
	case len(m.Value) > maxLabel || !utf8.ValidString(m.Value):
		return fmt.Errorf("matcher %s: the value is longer than %d bytes or not UTF-8", m.Name, maxLabel)
	}
	if m.IsRegex {
		if _, err := regexp.Compile("^(?:" + m.Value + ")$"); err != nil {
			return fmt.Errorf("matcher %s: %w", m.Name, err)
		}
	}
	return nil
}

// Matches reports whether labels satisfy the matcher. A matcher that does not validate matches
// nothing.
func (m Matcher) Matches(labels map[string]string) bool {
	v := labels[m.Name]
	var hit bool
	if m.IsRegex {
		re, err := regexp.Compile("^(?:" + m.Value + ")$")
		if err != nil {
			return false
		}
		hit = re.MatchString(v)
	} else {
		hit = v == m.Value
	}
	return hit == m.IsEqual
}

// MatchesAll reports whether labels satisfy every matcher: whether a silence of them mutes an alert
// of these labels. No matchers match nothing.
func MatchesAll(ms []Matcher, labels map[string]string) bool {
	if len(ms) == 0 {
		return false
	}
	for _, m := range ms {
		if !m.Matches(labels) {
			return false
		}
	}
	return true
}

// Silence is one of the Alertmanager's silences, as Cogpit keeps it (bounded: maxMatchers,
// maxComment, maxAuthor).
type Silence struct {
	ID        string       `json:"id"`
	State     SilenceState `json:"state"`
	Matchers  []Matcher    `json:"matchers"`
	StartsAt  time.Time    `json:"startsAt"`
	EndsAt    time.Time    `json:"endsAt"`
	UpdatedAt time.Time    `json:"updatedAt,omitzero"`
	CreatedBy string       `json:"createdBy"`
	Comment   string       `json:"comment"`
}

// wireSilence is Alertmanager's API v2 gettableSilence.
type wireSilence struct {
	ID       string    `json:"id"`
	Matchers []Matcher `json:"matchers"`
	StartsAt time.Time `json:"startsAt"`
	EndsAt   time.Time `json:"endsAt"`
	Updated  time.Time `json:"updatedAt"`
	By       string    `json:"createdBy"`
	Comment  string    `json:"comment"`
	Status   *struct {
		State string `json:"state"`
	} `json:"status"`
}

// UnmarshalJSON reads a matcher, isEqual true where an Alertmanager older than 0.22 leaves it out.
func (m *Matcher) UnmarshalJSON(b []byte) error {
	var w struct {
		Name    string `json:"name"`
		Value   string `json:"value"`
		IsRegex bool   `json:"isRegex"`
		IsEqual *bool  `json:"isEqual"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*m = Matcher{Name: w.Name, Value: w.Value, IsRegex: w.IsRegex, IsEqual: w.IsEqual == nil || *w.IsEqual}
	return nil
}

// decodeSilences reads the answer of GET /api/v2/silences: a JSON array of gettable silences,
// sorted as the silences page lists them (SortSilences), at most maxSilences (the newest kept).
func decodeSilences(body []byte) ([]Silence, error) {
	var wire []wireSilence
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, errNotAlertmanager
	}
	out := make([]Silence, 0, min(len(wire), maxSilences))
	for _, w := range wire {
		if w.ID == "" || w.Status == nil {
			return nil, errNotAlertmanager
		}
		s := Silence{ID: clip(w.ID, 64), State: SilenceState(w.Status.State), StartsAt: w.StartsAt, EndsAt: w.EndsAt,
			UpdatedAt: w.Updated, CreatedBy: clip(w.By, maxAuthor), Comment: clip(w.Comment, maxComment)}
		for i, m := range w.Matchers {
			if i == maxMatchers {
				break
			}
			m.Name, m.Value = clip(m.Name, maxName), clip(m.Value, maxLabel)
			s.Matchers = append(s.Matchers, m)
		}
		out = append(out, s)
	}
	SortSilences(out)
	if len(out) > maxSilences {
		out = out[:maxSilences] // the expired ones that ended longest ago go
	}
	return out, nil
}

// SortSilences orders silences as the silences page lists them: active ones first, the one that
// ends soonest first; then pending ones, the one that starts soonest first; then expired ones, the
// one that ended last first; then by id.
func SortSilences(ss []Silence) {
	slices.SortStableFunc(ss, func(a, b Silence) int {
		if a.State.Rank() != b.State.Rank() {
			return a.State.Rank() - b.State.Rank()
		}
		var c int
		switch a.State {
		case SilenceActive:
			c = a.EndsAt.Compare(b.EndsAt)
		case SilencePending:
			c = a.StartsAt.Compare(b.StartsAt)
		default:
			c = b.EndsAt.Compare(a.EndsAt)
		}
		if c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
}

// NewSilence is a silence to create: its matchers, when it starts and ends, who creates it and
// why. Alertmanager requires all of them.
type NewSilence struct {
	Matchers  []Matcher `json:"matchers"`
	StartsAt  time.Time `json:"startsAt"`
	EndsAt    time.Time `json:"endsAt"`
	CreatedBy string    `json:"createdBy"`
	Comment   string    `json:"comment"`
}

// Validate reports a silence Alertmanager would refuse, or one that mutes every alert: no matcher,
// a matcher that does not validate, only matchers that match the empty value (Alertmanager's own
// rule: such a silence would match alerts without the label too), an end not after its start or
// not in the future, no author, no comment.
func (n NewSilence) Validate(now time.Time) error {
	if len(n.Matchers) == 0 {
		return errors.New("a silence needs at least one matcher")
	}
	if len(n.Matchers) > maxMatchers {
		return fmt.Errorf("a silence has at most %d matchers", maxMatchers)
	}
	empty := true
	for _, m := range n.Matchers {
		if err := m.Validate(); err != nil {
			return err
		}
		if !m.Matches(map[string]string{}) {
			empty = false
		}
	}
	switch {
	case empty:
		return errors.New("at least one matcher must not match an empty value: a silence of these matchers would mute alerts that lack the labels")
	case !n.EndsAt.After(n.StartsAt):
		return errors.New("the silence must end after it starts")
	case !n.EndsAt.After(now):
		return errors.New("the silence must end in the future")
	case strings.TrimSpace(n.CreatedBy) == "":
		return errors.New("a silence needs its author (createdBy)")
	case len(n.CreatedBy) > maxAuthor:
		return fmt.Errorf("the author is longer than %d bytes", maxAuthor)
	case strings.TrimSpace(n.Comment) == "":
		return errors.New("a silence needs a comment: why it mutes the alerts")
	case len(n.Comment) > maxComment:
		return fmt.Errorf("the comment is longer than %d bytes", maxComment)
	}
	return nil
}

// silenceAnswer is the answer of POST /api/v2/silences.
type silenceAnswer struct {
	ID string `json:"silenceID"`
}

// validSilenceID reports an id Alertmanager may have given: a UUID's characters, no path.
var validSilenceID = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// ValidSilenceID reports whether id can name a silence in a request path.
func ValidSilenceID(id string) bool { return validSilenceID.MatchString(id) }
