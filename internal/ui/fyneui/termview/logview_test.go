package termview

import (
	"bytes"
	"errors"
	"io"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/model"
)

// stampRE finds a line's RFC 3339 timestamp.
var stampRE = regexp.MustCompile(`\d{4}-\d\d-\d\dT[0-9:.]+Z`)

// localStamps is a line as a view with timestamps shows it: its timestamp written in the view's
// format with milliseconds, ISO 8601 in UTC without one (D-31).
func localStamps(line string) string {
	return stampRE.ReplaceAllStringFunc(line, func(s string) string {
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return s
		}
		return model.TimeFormat{}.FormatPrecise(t)
	})
}

// mergedLogs are lines of a controlling resource's logs with timestamps, as the engine streams
// them: "<pod>/<container> <timestamp> <message>", and one of Cogpit's own.
var mergedLogs = []string{
	"web-7d9f-a1/app 2026-10-11T12:00:01.204118340Z starting order-service version=1.42.0",
	"web-7d9f-b2/app 2026-10-11T12:00:01.311902118Z starting order-service version=1.42.0",
	"web-7d9f-a1/proxy 2026-10-11T12:00:01.402551730Z envoy ready; upstream app:8080 healthy",
	"web-7d9f-a1/app 2026-10-11T12:00:09.023772001Z POST /api/v1/orders 201 18.2ms id=ord_8f2k1",
	"web-7d9f-b2/app 2026-10-11T12:00:12.774310925Z ERROR payment provider unreachable: dial tcp 10.96.14.2:443: i/o timeout",
	"web-7d9f-b2/app 2026-10-11T12:00:12.775006113Z retrying in 2s attempt=1",
	"[cogpit] web-7d9f-c3/app: container \"app\" in pod \"web-7d9f-c3\" is waiting to start: ContainerCreating",
	"web-7d9f-b2/app 2026-10-11T12:00:14.801233417Z payment provider reachable again after 2.03s; queued 4 orders for capture and released the connection pool's idle sockets",
	"web-7d9f-a1/app 2026-10-11T12:00:19.023109876Z error budget burn rate 0.4 (ok)",
}

// showMerged lays out a log view holding lines, with the view set.
func showMerged(t *testing.T, lines []string, v LogView, size fyne.Size) (*Logs, fyne.Window) {
	t.Helper()
	pr, pw := io.Pipe()
	l, w := showLogs(t, pr, size)
	waitApplied := appliedWaiter(t, l)
	go func() {
		_, _ = io.WriteString(pw, strings.Join(lines, "\n")+"\n")
	}()
	waitApplied(len(lines))
	t.Cleanup(func() { _ = pw.Close() })
	l.SetView(v)
	return l, w
}

func rowTexts(l *Logs) []string {
	out := make([]string, 0, len(l.rows))
	for i := range l.rows {
		var b strings.Builder
		for _, r := range l.rowRuns(i) {
			b.WriteString(r.text)
		}
		out = append(out, b.String())
	}
	return out
}

func TestLogViewShownSpans(t *testing.T) {
	line := "web-1/app 2026-10-11T12:00:01.204Z hello world"
	for _, tc := range []struct {
		v      LogView
		text   string
		spans  shownSpans
		prefix string
	}{
		{LogView{}, line, shownSpans{}, ""},
		{LogView{Prefixed: true, Names: true}, line, shownSpans{nameEnd: 9}, "web-1/app"},
		{LogView{Prefixed: true}, "2026-10-11T12:00:01.204Z hello world", shownSpans{}, ""},
		// A shown timestamp is written in the view's format with milliseconds (D-31): ISO 8601 in
		// UTC without one, the user's zone and format with one.
		{LogView{Prefixed: true, Timestamps: true}, "2026-10-11 12:00:01.204 hello world", shownSpans{tsStart: 0, tsEnd: 23}, ""},
		{LogView{Prefixed: true, Names: true, Timestamps: true}, "web-1/app 2026-10-11 12:00:01.204 hello world", shownSpans{nameEnd: 9, tsStart: 10, tsEnd: 33}, "web-1/app"},
		{LogView{Prefixed: true, Names: true, Timestamps: true, Time: model.TimeLayout{Date: "1/2/2006", Time: "3:04:05 PM"}.In(time.FixedZone("EDT", -4*60*60))},
			"web-1/app 10/11/2026 8:00:01.204 AM hello world", shownSpans{nameEnd: 9, tsStart: 10, tsEnd: 35}, "web-1/app"},
	} {
		text, sp := tc.v.shown(line)
		if text != tc.text || sp != tc.spans {
			t.Errorf("%+v: %q %+v, want %q %+v", tc.v, text, sp, tc.text, tc.spans)
		}
	}
	// A token shaped like a timestamp that is none shows as it came.
	if text, sp := (LogView{Timestamps: true}).shown("2026-10-11T99:00:01.204Z odd"); text != "2026-10-11T99:00:01.204Z odd" || sp.tsEnd != 24 {
		t.Errorf("an odd stamp: %q %+v", text, sp)
	}
	// A line of Cogpit's own has no timestamp; a prefix alone is a whole line.
	if _, sp := (LogView{Prefixed: true, Names: true, Timestamps: true}).shown("[cogpit] no pods yet"); sp.tsEnd != 0 || sp.nameEnd != 8 {
		t.Errorf("notice spans %+v", sp)
	}
	if text, sp := (LogView{Prefixed: true, Names: true}).shown("web-1/app"); text != "web-1/app" || sp.nameEnd != 9 {
		t.Errorf("prefix only: %q %+v", text, sp)
	}
	if text, _ := (LogView{Prefixed: true}).shown("web-1/app"); text != "" {
		t.Errorf("prefix only, hidden: %q", text)
	}
}

func TestIndexFoldAndWrap(t *testing.T) {
	for _, tc := range []struct {
		s, sub string
		want   int
	}{
		{"Payment ERROR here", "error", 8}, {"abc", "", 0}, {"abc", "abcd", -1}, {"GRÜSSE grüße", "grüße", 8}, {"x", "y", -1},
	} {
		if got := indexFold(tc.s, tc.sub); got != tc.want {
			t.Errorf("indexFold(%q, %q) = %d, want %d", tc.s, tc.sub, got, tc.want)
		}
	}
	var rows []string
	text := "the quick brown fox jumps over the lazy dog"
	wrapLine(text, 12, func(from, to int) { rows = append(rows, text[from:to]) })
	if want := []string{"the quick ", "brown fox ", "jumps over ", "the lazy dog"}; !slices.Equal(rows, want) {
		t.Errorf("wrapped %q, want %q", rows, want)
	}
	rows = rows[:0]
	text = strings.Repeat("x", 25) + " ünï"
	wrapLine(text, 10, func(from, to int) { rows = append(rows, text[from:to]) })
	if want := []string{"xxxxxxxxxx", "xxxxxxxxxx", "xxxxx ünï"}; !slices.Equal(rows, want) {
		t.Errorf("wrapped %q, want %q", rows, want)
	}
	rows = rows[:0]
	wrapLine("short", 10, func(from, to int) { rows = append(rows, "short"[from:to]) })
	if !slices.Equal(rows, []string{"short"}) {
		t.Errorf("short line %q", rows)
	}
}

// TestRunsColourNamesTimestampsAndMatches: a row's runs are the name in the accent colour, the
// timestamp muted, the message, and the matches of the search highlighted, case-insensitively.
func TestRunsColourNamesTimestampsAndMatches(t *testing.T) {
	v := LogView{Prefixed: true, Names: true, Timestamps: true, Search: "ERROR"}
	text, sp := v.shown("web-1/app 2026-10-11T12:00:01.204Z error: an Error")
	runs := runsOf(text, sp, 0, len(text), v.Search, nil)
	type r struct {
		text  string
		color fyne.ThemeColorName
		mark  bool
	}
	var got []r
	for _, x := range runs {
		got = append(got, r(x))
	}
	want := []r{
		{"web-1/app", theme.ColorNamePrimary, false}, {" ", theme.ColorNameForeground, false},
		{"2026-10-11 12:00:01.204", theme.ColorNamePlaceHolder, false}, {" ", theme.ColorNameForeground, false},
		{"error", theme.ColorNameForeground, true}, {": an ", theme.ColorNameForeground, false},
		{"Error", theme.ColorNameForeground, true},
	}
	if !slices.Equal(got, want) {
		t.Errorf("runs\n%+v\nwant\n%+v", got, want)
	}
	// A match across a wrapped row's start is marked on both sides.
	runs = runsOf("abcdef", shownSpans{}, 3, 6, "cde", nil)
	if len(runs) != 2 || runs[0].text != "de" || !runs[0].mark || runs[1].mark {
		t.Errorf("runs across the row start %+v", runs)
	}
}

// TestLogsViewOptions: names hidden and shown, wrap at the view's width, a filter that keeps only
// matching lines (and says how many), and the lines a download saves.
func TestLogsViewOptions(t *testing.T) {
	l, _ := showMerged(t, mergedLogs, LogView{Prefixed: true, Names: true, Timestamps: true}, fyne.NewSize(600, 300))
	if got := rowTexts(l); len(got) != len(mergedLogs) || got[0] != localStamps(mergedLogs[0]) {
		t.Fatalf("rows %q", got)
	}
	l.SetView(LogView{Prefixed: true, Timestamps: true})
	if got := rowTexts(l)[0]; got != "2026-10-11 12:00:01.204 starting order-service version=1.42.0" {
		t.Errorf("names hidden: %q", got)
	}

	l.SetView(LogView{Prefixed: true, Names: true, Timestamps: true, Search: "error", Filter: true})
	if got := rowTexts(l); len(got) != 2 || !strings.Contains(got[0], "ERROR payment") || !strings.Contains(got[1], "error budget") {
		t.Errorf("filtered rows %q", got)
	}
	if st := l.status.Text; !strings.HasPrefix(st, "2 of 9 lines") {
		t.Errorf("status %q", st)
	}

	l.SetView(LogView{Prefixed: true, Names: true, Timestamps: true, Wrap: true})
	if l.cols < 20 {
		t.Fatalf("cols %d", l.cols)
	}
	got := rowTexts(l)
	if len(got) <= len(mergedLogs) {
		t.Errorf("wrapping at %d characters made %d rows of %d lines", l.cols, len(got), len(mergedLogs))
	}
	for _, r := range got {
		if n := len([]rune(r)); n > l.cols {
			t.Errorf("a row of %d characters at %d columns: %q", n, l.cols, r)
		}
	}
	if strings.Join(got, "") != localStamps(strings.Join(mergedLogs, "")) {
		t.Error("the wrapped rows lose text")
	}

	// A download holds every line as shown: without names when they are hidden, unfiltered.
	l.SetView(LogView{Prefixed: true, Timestamps: true, Search: "nothing matches", Filter: true})
	lines := l.ShownLines()
	if len(lines) != len(mergedLogs) || lines[0] != "2026-10-11 12:00:01.204 starting order-service version=1.42.0" {
		t.Errorf("shown lines %q", lines)
	}
	var buf bytes.Buffer
	if err := WriteLines(&buf, lines[:2]); err != nil || buf.String() != lines[0]+"\n"+lines[1]+"\n" {
		t.Errorf("written %q, %v", buf.String(), err)
	}
	if err := WriteLines(failingWriter{}, lines); err == nil {
		t.Error("a failing writer's error was lost")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// TestLogsWrapFollowsTheWidth: wrapped rows follow the view's width.
func TestLogsWrapFollowsTheWidth(t *testing.T) {
	l, w := showMerged(t, mergedLogs, LogView{Wrap: true}, fyne.NewSize(900, 300))
	wide := len(l.rows)
	w.Resize(fyne.NewSize(450, 300))
	l.Refresh()
	if narrow := len(l.rows); narrow <= wide {
		t.Errorf("%d rows at 900 px, %d at 450 px", wide, narrow)
	}
}

// TestLogsSlashFocusesSearch: "/" typed while the list has the focus calls the view's handler;
// other keys go to the list.
func TestLogsSlashFocusesSearch(t *testing.T) {
	l, w := showMerged(t, mergedLogs[:3], LogView{}, fyne.NewSize(600, 300))
	slashes := 0
	l.SetOnSlash(func() { slashes++ })
	w.Canvas().Focus(l.list)
	test.Type(l.list, "a/b")
	if slashes != 1 {
		t.Errorf("slash handled %d times", slashes)
	}
}

// TestLogsFilterAppliesToNewLines: lines that arrive while a filter is on show only when they
// match.
func TestLogsFilterAppliesToNewLines(t *testing.T) {
	pr, pw := io.Pipe()
	l, _ := showLogs(t, pr, fyne.NewSize(600, 300))
	waitApplied := appliedWaiter(t, l)
	l.SetView(LogView{Search: "warn", Filter: true})
	go func() { _, _ = io.WriteString(pw, "info one\nWARN two\ninfo three\nwarning four\n") }()
	waitApplied(4)
	if got := rowTexts(l); !slices.Equal(got, []string{"WARN two", "warning four"}) {
		t.Errorf("rows %q", got)
	}
	_ = pw.Close()
	select {
	case <-l.flushed:
	case <-time.After(5 * time.Second):
		t.Fatal("no end")
	}
	if st := l.status.Text; st != "2 of 4 lines · stream ended" {
		t.Errorf("status %q", st)
	}
}

func TestRenderMergedLogs(t *testing.T) {
	l, w := showMerged(t, mergedLogs, LogView{Prefixed: true, Names: true, Timestamps: true, Search: "error"}, fyne.NewSize(900, 260))
	l.Refresh()
	writePNG(t, w, "logs_merged.png")
	l.SetView(LogView{Prefixed: true, Names: true, Timestamps: true, Wrap: true, Search: "error"})
	l.Refresh()
	writePNG(t, w, "logs_merged_wrap.png")
}

// TestLogViewSelectsAGroupsSource (A81): an application's lines start with
// "<workload>/<pod>/<container>"; the view narrows them by workload, pod and container, a
// workload's "<pod>/<container>" lines by pod and container, and Cogpit's own lines always show.
func TestLogViewSelectsAGroupsSource(t *testing.T) {
	front := "front/front-7c-a/app 2026-10-15T00:00:00Z hello"
	back := "back/back-5d-b/app 2026-10-15T00:00:00Z hi"
	pod := "web-1-a/app 2026-10-15T00:00:00Z plain"
	own := "[cogpit] StatefulSet db left Kustomization apps; its logs end here"
	for _, tc := range []struct {
		v    LogView
		want []bool // front, back, pod, own
	}{
		{LogView{Prefixed: true}, []bool{true, true, true, true}},
		{LogView{Prefixed: true, Workload: "back"}, []bool{false, true, false, true}},
		{LogView{Prefixed: true, Workload: "front", Container: "app"}, []bool{true, false, false, true}},
		{LogView{Prefixed: true, Pod: "back-5d-b"}, []bool{false, true, false, true}},
		{LogView{Prefixed: true, Pod: "web-1-a"}, []bool{false, false, true, true}},
	} {
		for i, line := range []string{front, back, pod, own} {
			if got := tc.v.selects(line); got != tc.want[i] {
				t.Errorf("%+v selects %q: %v", tc.v, line, got)
			}
		}
	}
	if src, _, ok := (LogView{Prefixed: true}).source(front); !ok || src != "front/front-7c-a/app" {
		t.Errorf("source %q %v", src, ok)
	}
}

// A line of Cogpit's own is drawn in the warning colour, apart from the container's (WF35): a pod
// log's marker, a merged log's notice in the name's place, and not a container's line that merely
// mentions it.
func TestCogpitsOwnLinesStandApart(t *testing.T) {
	for _, c := range []struct {
		view   LogView
		line   string
		notice bool
	}{
		{LogView{}, "[cogpit] ── no connection 11:00:05 – 11:10:07 CEST (10 min 2 s) · …", true},
		{LogView{Prefixed: true, Names: true}, "[cogpit] web-7d9f-a1/app: ── no connection 11:00:05 – 11:10:07 CEST (10 min 2 s) · …", true},
		{LogView{Timestamps: true}, "2026-10-19T09:00:05Z hello [cogpit]", false},
	} {
		text, sp := c.view.shown(c.line)
		runs := runsOf(text, sp, 0, len(text), "", nil)
		last := runs[len(runs)-1].color
		if got := last == theme.ColorNameWarning; got != c.notice {
			t.Errorf("%q: notice colour %v, want %v (runs %+v)", c.line, got, c.notice, runs)
		}
	}
}
