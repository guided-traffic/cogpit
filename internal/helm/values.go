package helm

import (
	"bytes"
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"

	chartcommon "helm.sh/helm/v4/pkg/chart/common"
	chartutil "helm.sh/helm/v4/pkg/chart/common/util"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// A release's values: the user-supplied values the release was installed or upgraded with, and
// all values (the chart's defaults with the user-supplied ones over them), as helm get values
// [--all] prints them. Values can hold credentials (H-8): they are shown in the user's own window,
// never to agents, and never in logs, errors, toasts, audit lines or the Helm journal. A value
// that reaches an error text through the Helm SDK (a schema check quoting it, a template's fail)
// is replaced by ScrubValues.

// ErrNoValuesPayload marks a release record (a Secret or a ConfigMap) whose payload could not be
// read.
var ErrNoValuesPayload = errors.New("the release record could not be read")

// Values is a release's values as YAML, from its newest revision's record (a Secret or a
// ConfigMap).
type Values struct {
	YAML string `json:"yaml"`
	// All is true when YAML holds every value, the chart's defaults included; false for the
	// user-supplied values only.
	All bool `json:"all"`
	// Revision, Chart and ChartVersion are the revision the values are of and its chart.
	Revision     int    `json:"revision"`
	Chart        string `json:"chart"`
	ChartVersion string `json:"chartVersion"`
}

// ReleaseValues decodes a release record (a Secret or a ConfigMap, from the cache) and returns its
// values as YAML: the user-supplied values, or with all set every value, the chart's defaults
// coalesced as Helm renders them. The payload is untrusted input: a decoder that fails or panics
// is an error that carries no content of the record.
func ReleaseValues(record *unstructured.Unstructured, all bool) (v Values, err error) {
	defer func() {
		if r := recover(); r != nil {
			v, err = Values{}, ErrNoValuesPayload
		}
	}()
	rls, err := DecodeRecord(record)
	if err != nil {
		return Values{}, ErrNoValuesPayload
	}
	v = Values{All: all, Revision: rls.Version}
	if rls.Chart != nil && rls.Chart.Metadata != nil {
		v.Chart, v.ChartVersion = rls.Chart.Metadata.Name, rls.Chart.Metadata.Version
	}
	vals := rls.Config
	if all && rls.Chart != nil {
		if vals, err = chartutil.CoalesceValues(rls.Chart, rls.Config); err != nil {
			return Values{}, errors.New("the release's values could not be merged with its chart's defaults")
		}
	}
	if len(vals) == 0 {
		return v, nil
	}
	y, err := yaml.Marshal(vals)
	if err != nil {
		return Values{}, errors.New("the release's values could not be written as YAML")
	}
	v.YAML = string(y)
	return v, nil
}

// lineNumber finds the line a YAML parser names in its error.
var lineNumber = regexp.MustCompile(`line (\d+)`)

// ParseValues parses values YAML as helm upgrade -f reads a values file (chart/common.ReadValues):
// a mapping, numbers as Helm reads them. Empty text is no values. The error names at most the line,
// never a value: some YAML errors quote what they could not convert.
func ParseValues(text string) (map[string]any, error) {
	if strings.TrimSpace(text) == "" {
		return map[string]any{}, nil
	}
	vals, err := chartcommon.ReadValues([]byte(text))
	if err != nil {
		msg := "the values are not valid YAML (a mapping of keys to values)"
		if m := lineNumber.FindStringSubmatch(err.Error()); m != nil {
			msg += ", see line " + m[1]
		}
		return nil, errors.New(msg)
	}
	return vals, nil
}

// minScrubbed is the shortest value ScrubValues replaces: shorter ones ("on", "443") would
// garble every message while hiding nothing a reader could use.
const minScrubbed = 4

// maxErrorText bounds an action's error text.
const maxErrorText = 2000

// withheldValue replaces a value in an error text.
const withheldValue = "<value withheld>"

// ScrubValues returns text with every string and number of values that is at least minScrubbed
// characters long replaced by a placeholder, also in its base64 form (charts put values into
// Secrets with b64enc, and the API server can quote those) and in the escaped forms error texts
// quote strings in (Go's %q and %+q, JSON with and without HTML escaping); longer values first, so
// that a value containing another is replaced whole. Text is then cut to maxErrorText. Values a
// template transformed otherwise (upper, sha256sum) are not found: a known limit.
func ScrubValues(text string, values map[string]any) string {
	return ScrubValuesExcept(text, values, nil)
}

// ScrubValuesExcept is ScrubValues keeping the strings of public: names the reader sees anyway
// (a release's name, its namespace, its chart's name and version), which a chart's values often
// repeat. A value equal to one of them is not replaced, and a shorter value inside one of them is
// not replaced there; a longer value that contains one is still replaced whole.
func ScrubValuesExcept(text string, values map[string]any, public []string) string {
	type candidate struct {
		s    string
		keep int // the index of a public string, or -1 for a value
	}
	var cands []candidate
	for i, p := range public {
		if len(p) >= minScrubbed { // a shorter one cannot hold a value that is replaced
			cands = append(cands, candidate{p, i})
		}
	}
	for _, s := range scalars(values, nil) {
		if slices.Contains(public, s) {
			continue
		}
		cands = append(cands, candidate{s, -1}, candidate{base64.StdEncoding.EncodeToString([]byte(s)), -1})
		for _, q := range quotedForms(s) {
			if q != s && len(q) >= minScrubbed {
				cands = append(cands, candidate{q, -1})
			}
		}
	}
	// Longest first; at equal length a public string before a value of the same text.
	slices.SortStableFunc(cands, func(a, b candidate) int {
		return cmp.Or(cmp.Compare(len(b.s), len(a.s)), cmp.Compare(b.keep, a.keep))
	})
	cands = slices.CompactFunc(cands, func(a, b candidate) bool { return a.s == b.s })
	kept := map[string]string{}
	for _, c := range cands {
		if c.keep < 0 {
			text = strings.ReplaceAll(text, c.s, withheldValue)
			continue
		}
		// A public string is held behind a mark no value of minScrubbed characters matches, and put
		// back after the shorter values are replaced.
		mark := "\x00\x01" + strconv.Itoa(c.keep) + "\x01\x00"
		if strings.Contains(text, c.s) {
			kept[mark] = c.s
			text = strings.ReplaceAll(text, c.s, mark)
		}
	}
	for mark, s := range kept {
		text = strings.ReplaceAll(text, mark, s)
	}
	if len(text) > maxErrorText {
		text = text[:maxErrorText] + "… (cut)"
	}
	return text
}

// quotedForms are s as quoting escapes it, without the quotes: Go's %q and %+q (strconv), and
// JSON, as Go's encoder writes it (<, > and & as \u003c…) and as other encoders do.
func quotedForms(s string) []string {
	unquote := func(q string) string { return q[1 : len(q)-1] }
	out := []string{unquote(strconv.Quote(s)), unquote(strconv.QuoteToASCII(s))}
	if b, err := json.Marshal(s); err == nil {
		out = append(out, unquote(string(b)))
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if enc.Encode(s) == nil {
		out = append(out, unquote(strings.TrimSuffix(buf.String(), "\n")))
	}
	return out
}

// scalars collects the string and number leaves of values that are at least minScrubbed long;
// a float also as a template renders it (%v: 1e+06), not only as its digits.
func scalars(v any, out []string) []string {
	switch x := v.(type) {
	case map[string]any:
		for _, e := range x {
			out = scalars(e, out)
		}
	case chartcommon.Values:
		for _, e := range x {
			out = scalars(e, out)
		}
	case []any:
		for _, e := range x {
			out = scalars(e, out)
		}
	case string:
		for _, line := range strings.Split(x, "\n") { // a multi-line value is quoted line by line too
			if s := strings.TrimSpace(line); len(s) >= minScrubbed {
				out = append(out, s)
			}
		}
		if len(x) >= minScrubbed {
			out = append(out, x)
		}
	case float64:
		for _, s := range []string{strconv.FormatFloat(x, 'f', -1, 64), strconv.FormatFloat(x, 'g', -1, 64)} {
			if len(s) >= minScrubbed {
				out = append(out, s)
			}
		}
	case int64:
		if s := strconv.FormatInt(x, 10); len(s) >= minScrubbed {
			out = append(out, s)
		}
	case int:
		if s := strconv.Itoa(x); len(s) >= minScrubbed {
			out = append(out, s)
		}
	}
	return out
}

// scrubError is an action error whose text has the values scrubbed; it keeps no reference to the
// original error, whose text may quote them, only to the sentinels it matched, which quote none.
type scrubError struct {
	msg       string
	sentinels []error
}

func (e *scrubError) Error() string   { return e.msg }
func (e *scrubError) Unwrap() []error { return e.sentinels }

// scrubSentinels are the errors a scrubbed error still matches with errors.Is.
var scrubSentinels = []error{ErrUnchanged, ErrNoRepository, ErrReleaseMoved, context.Canceled, context.DeadlineExceeded}

// Scrubbed returns err with its text scrubbed of values (ScrubValues), or nil; it still matches
// ErrUnchanged, ErrNoRepository, ErrReleaseMoved and the context errors with errors.Is, and
// nothing else.
func Scrubbed(err error, values map[string]any) error {
	if err == nil {
		return nil
	}
	out := &scrubError{msg: ScrubValues(err.Error(), values)}
	for _, s := range scrubSentinels {
		if errors.Is(err, s) {
			out.sentinels = append(out.sentinels, s)
		}
	}
	return out
}

// coalesceWarning finds the Helm SDK's warnings that quote a value. Its value coalescing
// (pkg/chart/common/util, run by every upgrade and by ReleaseValues with all) writes them with the
// standard library's log package, which Cogpit's logger receives (slog.SetDefault): "warning:
// destination for x is a table. Ignoring non-table value (hunter2)".
var coalesceWarning = regexp.MustCompile(`(?s)^(warning: (?:cannot overwrite table with non table for|destination for) .*?) \(.*\)$`)

// RedactSDKLog wraps the process's log handler so that the Helm SDK's coalescing warnings keep
// the key and drop the value they quote. Install it before slog.SetDefault.
func RedactSDKLog(next slog.Handler) slog.Handler { return sdkLogFilter{next: next} }

type sdkLogFilter struct{ next slog.Handler }

func (h sdkLogFilter) Enabled(ctx context.Context, l slog.Level) bool { return h.next.Enabled(ctx, l) }

func (h sdkLogFilter) Handle(ctx context.Context, r slog.Record) error {
	if m := coalesceWarning.FindStringSubmatch(r.Message); m != nil {
		out := slog.NewRecord(r.Time, r.Level, m[1]+" ("+withheldValue+")", r.PC)
		r.Attrs(func(a slog.Attr) bool { out.AddAttrs(a); return true })
		r = out
	}
	return h.next.Handle(ctx, r)
}

func (h sdkLogFilter) WithAttrs(attrs []slog.Attr) slog.Handler {
	return sdkLogFilter{next: h.next.WithAttrs(attrs)}
}

func (h sdkLogFilter) WithGroup(name string) slog.Handler {
	return sdkLogFilter{next: h.next.WithGroup(name)}
}

// scrubHandler is the Helm SDK's logger during an upgrade: it scrubs the values from every record's
// message and attributes before the next handler sees them (the SDK logs a failed upgrade's error,
// which may quote a value).
type scrubHandler struct {
	next   slog.Handler
	values map[string]any
}

func (h scrubHandler) Enabled(ctx context.Context, l slog.Level) bool { return h.next.Enabled(ctx, l) }

func (h scrubHandler) Handle(ctx context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, ScrubValues(r.Message, h.values), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.scrubAttr(a))
		return true
	})
	return h.next.Handle(ctx, out)
}

func (h scrubHandler) scrubAttr(a slog.Attr) slog.Attr {
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		attrs := v.Group()
		out := make([]any, 0, len(attrs))
		for _, g := range attrs {
			out = append(out, h.scrubAttr(g))
		}
		return slog.Group(a.Key, out...)
	}
	return slog.String(a.Key, ScrubValues(fmt.Sprint(v.Any()), h.values))
}

func (h scrubHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	scrubbedAttrs := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		scrubbedAttrs[i] = h.scrubAttr(a)
	}
	return scrubHandler{next: h.next.WithAttrs(scrubbedAttrs), values: h.values}
}

func (h scrubHandler) WithGroup(name string) slog.Handler {
	return scrubHandler{next: h.next.WithGroup(name), values: h.values}
}
