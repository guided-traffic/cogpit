package termview

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/testkit/scorecard"
)

// TestMain shortens the log flusher's interval: most tests wait for a flush or two, and none
// depends on the app's interval but TestLogsP5, which sets it for its own run.
func TestMain(m *testing.M) {
	logFlushEvery = 5 * time.Millisecond
	os.Exit(m.Run())
}

// showLogs lays a Logs view for stream out in an offscreen window of the given size.
func showLogs(t *testing.T, stream io.ReadCloser, size fyne.Size) (*Logs, fyne.Window) {
	t.Helper()
	newTestApp(t)
	l := NewLogs(stream)
	w := test.NewTempWindow(t, widget.NewLabel(""))
	w.Resize(size)
	w.SetContent(l)
	t.Cleanup(func() { _ = l.Close() })
	return l, w
}

func waitFlushed(t *testing.T, l *Logs, timeout time.Duration) {
	t.Helper()
	select {
	case <-l.flushed:
	case <-time.After(timeout):
		t.Fatalf("log view did not publish the end of the stream within %v", timeout)
	}
}

func TestCleanLogLine(t *testing.T) {
	cases := map[string]string{
		"plain line\n":                             "plain line",
		"crlf line\r\n":                            "crlf line",
		"\x1b[32mINFO\x1b[0m started":              "INFO started",
		"\x1b]0;title\x07after":                    "after",
		"\x1b]52;c;QUFB\x1b\\after":                "after",
		"\x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\": "link",
		"a\tb":                   "a       b",
		"12345678\tx":            "12345678        x",
		"nul\x00bell\x07del\x7f": "nulbelldel",
		"c1 \u009b31m":           "c1 31m",
		"bad \xff utf8":          "bad � utf8",
		"grüße 集群":               "grüße 集群",
		"cut \x1b[3":             "cut ",
		"esc at end \x1b":        "esc at end ",
	}
	for in, want := range cases {
		if got := cleanLogLine([]byte(in)); got != want {
			t.Errorf("cleanLogLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLineRingBounds(t *testing.T) {
	r := newLineRing(3, 10)
	for _, s := range []string{"a", "b", "c", "d"} {
		r.push(s)
	}
	if r.len() != 3 || r.at(0) != "b" || r.at(2) != "d" || r.evicted != 1 {
		t.Errorf("ring = %d lines [%q..%q], evicted %d", r.len(), r.at(0), r.at(2), r.evicted)
	}
	r.push("0123456789") // the byte bound evicts the rest
	if r.len() != 1 || r.at(0) != "0123456789" || r.bytes != 10 {
		t.Errorf("after a large line: %d lines, %d bytes", r.len(), r.bytes)
	}
	if got := r.drain(nil); !slices.Equal(got, []string{"0123456789"}) || r.len() != 0 || r.bytes != 0 {
		t.Errorf("drain = %v, ring %d lines %d bytes", got, r.len(), r.bytes)
	}
	if r.at(-1) != "" || r.at(5) != "" {
		t.Error("out-of-range at must be empty")
	}
}

func TestLogsShowsLinesAndEnd(t *testing.T) {
	pr, pw := io.Pipe()
	l, _ := showLogs(t, pr, fyne.NewSize(600, 300))
	go func() {
		_, _ = io.WriteString(pw, "first\n\x1b[31msecond\x1b[0m\r\n")
		_, _ = io.WriteString(pw, strings.Repeat("x", logMaxLineBytes+100)+"\n")
		_, _ = io.WriteString(pw, "no newline at the end")
		_ = pw.Close()
	}()
	waitFlushed(t, l, 5*time.Second)
	if l.lines.len() != 4 {
		t.Fatalf("lines = %d, want 4", l.lines.len())
	}
	if l.lines.at(0) != "first" || l.lines.at(1) != "second" || l.lines.at(3) != "no newline at the end" {
		t.Errorf("lines = %q, %q, …, %q", l.lines.at(0), l.lines.at(1), l.lines.at(3))
	}
	if long := l.lines.at(2); len(long) != logMaxLineBytes+len(truncatedMark) || !strings.HasSuffix(long, truncatedMark) {
		t.Errorf("long line kept %d bytes, want it cut at %d", len(long), logMaxLineBytes)
	}
	if got := l.status.Text; got != "4 lines · stream ended" {
		t.Errorf("status = %q", got)
	}
}

func TestLogsFollowOffStaysPut(t *testing.T) {
	pr, pw := io.Pipe()
	l, _ := showLogs(t, pr, fyne.NewSize(600, 300))
	waitApplied := appliedWaiter(t, l)
	write := func(from, to int) {
		var b strings.Builder
		for i := from; i < to; i++ {
			fmt.Fprintf(&b, "line %d\n", i)
		}
		if _, err := io.WriteString(pw, b.String()); err != nil {
			t.Fatal(err)
		}
	}
	write(0, 200)
	waitApplied(200)
	if off := l.list.GetScrollOffset(); off <= 0 {
		t.Fatalf("following: offset %v, want scrolled to the bottom", off)
	}

	l.followCheck.SetChecked(false)
	l.list.ScrollToOffset(100)
	write(200, 400) // no eviction yet: the view keeps its offset
	waitApplied(400)
	if off := l.list.GetScrollOffset(); off != 100 {
		t.Errorf("not following: offset %v, want 100", off)
	}

	l.followCheck.SetChecked(true)
	before := l.list.GetScrollOffset()
	write(400, 450)
	waitApplied(450)
	if off := l.list.GetScrollOffset(); off <= before {
		t.Errorf("following again: offset %v, want past %v", off, before)
	}
	_ = pw.Close()
}

func TestLogsFollowOffKeepsLinesInViewWhenOldOnesAreDropped(t *testing.T) {
	pr, pw := io.Pipe()
	l, _ := showLogs(t, pr, fyne.NewSize(600, 300))
	waitApplied := appliedWaiter(t, l)
	write := func(from, to int) {
		var b strings.Builder
		for i := from; i < to; i++ {
			fmt.Fprintf(&b, "line %d\n", i)
		}
		if _, err := io.WriteString(pw, b.String()); err != nil {
			t.Fatal(err)
		}
	}
	write(0, logScrollback)
	waitApplied(logScrollback)
	l.followCheck.SetChecked(false)
	stride := l.stride()
	l.list.ScrollToOffset(5000 * stride) // "line 5000" at the top
	write(logScrollback, logScrollback+1000)
	waitApplied(logScrollback + 1000)
	// 1,000 lines were dropped at the front, so "line 5000" is now item 4000.
	if off, want := l.list.GetScrollOffset(), 4000*stride; off < want-stride || off > want+stride {
		t.Errorf("offset %v, want about %v (line 5000 stays at the top)", off, want)
	}
	if got := l.lines.at(4000); got != "line 5000" {
		t.Errorf("item 4000 = %q", got)
	}
	_ = pw.Close()
}

// appliedWaiter hooks into l's flushes and returns a function that waits until at least want
// lines have been applied. Set it before writing to the stream.
func appliedWaiter(t *testing.T, l *Logs) func(want int) {
	t.Helper()
	applied := make(chan int, 1024)
	total := 0
	l.onFlush = func(_ time.Duration, n int) {
		total += n
		applied <- total
	}
	return func(want int) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case got := <-applied:
				if got >= want {
					return
				}
			case <-deadline:
				t.Fatalf("fewer than %d lines applied within 5s", want)
			}
		}
	}
}

// TestLogsP5 feeds 10,000 lines per second, for three seconds in make scorecard, and measures the
// time each batch spends on the main goroutine (the test driver runs fyne.Do on the caller, so the
// flusher's apply stands in for the main goroutine) and the heap once the scrollback is full.
func TestLogsP5(t *testing.T) {
	if testing.Short() {
		t.Skip("P5 runs for seconds")
	}
	if raceEnabled {
		t.Skip("P5 is measured without the race detector (Unit Tests (no race), make scorecard)")
	}
	const (
		rate = 10_000 // lines per second
		tick = 10 * time.Millisecond
	)
	duration := 3 * time.Second
	if os.Getenv(scorecard.DirEnv) == "" {
		duration = 1200 * time.Millisecond // still more lines than the scrollback keeps
	}
	defer func(d time.Duration) { logFlushEvery = d }(logFlushEvery)
	logFlushEvery = logFlushPeriod // the app's batches
	pr, pw := io.Pipe()
	l, _ := showLogs(t, pr, fyne.NewSize(1200, 800))
	var batches []time.Duration
	var batchLines []int
	l.onFlush = func(d time.Duration, n int) {
		batches = append(batches, d)
		batchLines = append(batchLines, n)
	}

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	total := int(rate * duration / time.Second)
	perTick := int(rate * tick / time.Second)
	start := time.Now()
	go func() {
		ticker := time.NewTicker(tick)
		defer ticker.Stop()
		var b strings.Builder
		for i := 0; i < total; {
			<-ticker.C
			b.Reset()
			for j := 0; j < perTick && i < total; j, i = j+1, i+1 {
				fmt.Fprintf(&b, "2026-10-11T12:00:%02d.%06dZ \x1b[32mINFO\x1b[0m request id=%08d method=GET path=/api/v1/orders/%d status=200 duration=%dms\n",
					i/rate%60, i%1_000_000, i, i%9973, i%250)
			}
			if _, err := io.WriteString(pw, b.String()); err != nil {
				return
			}
		}
		_ = pw.Close()
	}()
	waitFlushed(t, l, duration+10*time.Second)
	elapsed := time.Since(start)

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(l)

	if l.lines.len() != logScrollback {
		t.Errorf("kept %d lines, want the scrollback limit %d", l.lines.len(), logScrollback)
	}
	if want := fmt.Sprintf("request id=%08d", total-1); !strings.Contains(l.lines.at(logScrollback-1), want) {
		t.Errorf("last line = %q, want %s", l.lines.at(logScrollback-1), want)
	}
	if len(batches) == 0 {
		t.Fatal("no batches")
	}
	sorted := slices.Clone(batches)
	slices.Sort(sorted)
	pct := func(p float64) time.Duration { return sorted[int(p*float64(len(sorted)-1))] }
	over16 := 0
	for _, d := range batches {
		if d > 16*time.Millisecond {
			over16++
		}
	}
	heap := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("P5: %d lines in %v (%.0f lines/s), %d batches, %.0f lines/batch; main goroutine per batch: p50 %v, p99 %v, max %v, over 16 ms: %d; heap growth %.1f MiB for %d kept lines",
		total, elapsed.Round(time.Millisecond), float64(total)/elapsed.Seconds(), len(batches),
		float64(total)/float64(len(batches)), pct(0.5), pct(0.99), sorted[len(sorted)-1], over16,
		float64(heap)/(1<<20), l.lines.len())
	how := "offscreen: Fyne's test driver, the flusher's apply on the caller standing in for the main goroutine; no GPU frame"
	scorecard.Record(t, scorecard.Measurement{Budget: "P5", Metric: "the slowest log batch on the main goroutine at 10,000 lines/s",
		Value: scorecard.Millis(sorted[len(sorted)-1]), Unit: "ms", Limit: 16, How: how})
	scorecard.Record(t, scorecard.Measurement{Budget: "P5", Metric: "lines per second fed and shown",
		Value: float64(total) / elapsed.Seconds(), Unit: "lines/s", Bound: scorecard.Context, How: how})
	scorecard.Record(t, scorecard.Measurement{Budget: "P5", Metric: fmt.Sprintf("heap growth with %d kept lines, the scrollback's limit", l.lines.len()),
		Value: float64(heap) / (1 << 20), Unit: "MiB", Bound: scorecard.Context, How: how})

	// A generous bound on a loaded runner: make scorecard judges the budget (16 ms); this catches a
	// flusher that blocks the main goroutine for good.
	if maxD := sorted[len(sorted)-1]; maxD > 500*time.Millisecond {
		t.Errorf("a batch took %v on the main goroutine", maxD)
	}
	if heap > 64<<20 {
		t.Errorf("heap grew by %d MiB; the scrollback should bound it", heap>>20)
	}
}

func TestShownTextCutsOnRuneBoundary(t *testing.T) {
	s := strings.Repeat("集", logMaxShownBytes) // 3 bytes each
	got := shownText(s)
	if !strings.HasSuffix(got, truncatedMark) || len(got) > logMaxShownBytes+len(truncatedMark) {
		t.Errorf("shownText kept %d bytes", len(got))
	}
	if body := strings.TrimSuffix(got, truncatedMark); strings.Count(body, "集")*3 != len(body) {
		t.Error("shownText split a rune")
	}
	if shownText("short") != "short" {
		t.Error("shownText changed a short line")
	}
}

// demoLogs is a container log as the screenshot shows it: colours and an OSC title (stripped),
// a tab, a JSON line.
var demoLogs = []string{
	"2026-10-11T12:00:01.204Z \x1b[32mINFO\x1b[0m  starting order-service version=1.42.0 commit=9f3c2e1",
	"2026-10-11T12:00:01.311Z \x1b[32mINFO\x1b[0m  connected to postgres host=orders-db.shop.svc:5432 pool=20",
	"2026-10-11T12:00:01.312Z \x1b[32mINFO\x1b[0m  listening on :8080",
	"2026-10-11T12:00:04.877Z \x1b[36mDEBUG\x1b[0m GET /healthz 200 0.4ms",
	"2026-10-11T12:00:09.023Z \x1b[32mINFO\x1b[0m  POST /api/v1/orders 201 18.2ms\tid=ord_8f2k1",
	"2026-10-11T12:00:09.511Z \x1b[33mWARN\x1b[0m  slow query took=812ms query=\"SELECT * FROM orders WHERE customer_id = $1\"",
	"\x1b]0;pwned\x07{\"level\":\"info\",\"ts\":\"2026-10-11T12:00:10.002Z\",\"msg\":\"cache warmed\",\"entries\":10432}",
	"2026-10-11T12:00:12.774Z \x1b[31mERROR\x1b[0m payment provider unreachable: dial tcp 10.96.14.2:443: i/o timeout",
	"2026-10-11T12:00:12.775Z \x1b[32mINFO\x1b[0m  retrying in 2s attempt=1",
	"2026-10-11T12:00:14.801Z \x1b[32mINFO\x1b[0m  payment provider reachable again",
	"2026-10-11T12:00:19.023Z \x1b[32mINFO\x1b[0m  POST /api/v1/orders 201 16.9ms\tid=ord_8f2k2",
}

func TestLogsScreenshot(t *testing.T) {
	pr, pw := io.Pipe()
	l, w := showLogs(t, pr, fyne.NewSize(760, 330))
	go func() {
		for _, line := range demoLogs {
			_, _ = io.WriteString(pw, line+"\n")
		}
		_ = pw.Close()
	}()
	waitFlushed(t, l, 5*time.Second)
	if got := l.lines.at(6); strings.ContainsAny(got, "\x1b\x07") || strings.Contains(got, "pwned") {
		t.Errorf("escape sequences survived: %q", got)
	}
	// The test canvas, unlike the desktop driver, does not re-lay out a container whose child's
	// minimum size grew (the status label).
	l.Refresh()
	writePNG(t, w, "logs.png")
}

// TestLogsStampedAndSelection: a stamped view hides the server's timestamps unless asked to show
// them, before or after the names; a selection shows the lines of one pod or of one container's
// name, Cogpit's own lines always, counts them in the status and downloads them alone.
func TestLogsStampedAndSelection(t *testing.T) {
	l, _ := showMerged(t, mergedLogs, LogView{Prefixed: true, Names: true, Stamped: true}, fyne.NewSize(900, 300))
	if got := rowTexts(l); got[0] != "web-7d9f-a1/app starting order-service version=1.42.0" || got[6] != mergedLogs[6] {
		t.Errorf("stamped rows without timestamps %q", got)
	}
	l.SetView(LogView{Prefixed: true, Stamped: true})
	if got := rowTexts(l)[0]; got != "starting order-service version=1.42.0" {
		t.Errorf("without names and timestamps %q", got)
	}
	l.SetView(LogView{Prefixed: true, Names: true, Stamped: true, Timestamps: true})
	if got := rowTexts(l)[0]; got != localStamps(mergedLogs[0]) {
		t.Errorf("with names and timestamps %q", got)
	}

	l.SetView(LogView{Prefixed: true, Names: true, Stamped: true, Pod: "web-7d9f-a1"})
	got := rowTexts(l)
	if len(got) != 5 || !strings.HasPrefix(got[1], "web-7d9f-a1/proxy ") || !strings.HasPrefix(got[3], "[cogpit] ") {
		t.Errorf("one pod's rows %q", got)
	}
	if st := l.StatusText(); !strings.HasPrefix(st, "5 of 9 lines") {
		t.Errorf("status %q", st)
	}
	if lines := l.DownloadLines(); len(lines) != 5 {
		t.Errorf("a download of one pod holds %d lines", len(lines))
	}
	l.SetView(LogView{Prefixed: true, Names: true, Stamped: true, Container: "proxy"})
	if got := rowTexts(l); len(got) != 2 || !strings.HasPrefix(got[0], "web-7d9f-a1/proxy ") {
		t.Errorf("the proxies' rows %q", got)
	}
	l.SetView(LogView{Prefixed: true, Names: true, Stamped: true, Pod: "web-7d9f-b2", Container: "app", Search: "payment", Filter: true})
	if got := rowTexts(l); len(got) != 2 {
		t.Errorf("one pod's container, filtered: %q", got)
	}
	// A84: a download saves what the view shows, its filter applied; a search alone hides nothing.
	if got, rows := l.DownloadLines(), rowTexts(l); !slices.Equal(got, rows) || len(l.ShownLines()) != 5 {
		t.Errorf("a filtered download %q, the rows %q, held %d", got, rows, len(l.ShownLines()))
	}
	l.SetView(LogView{Prefixed: true, Names: true, Stamped: true, Pod: "web-7d9f-b2", Container: "app", Search: "payment"})
	if got := l.DownloadLines(); len(got) != 5 {
		t.Errorf("a download with a search and no filter %q", got)
	}
}
