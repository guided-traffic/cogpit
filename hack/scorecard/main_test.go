package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/testkit/scorecard"
)

// TestReadBudgetsFromTestingMd reads the real table: every budget the page and the generator name
// is in it, so a renamed heading or a reshaped row fails here and not on the next make scorecard.
func TestReadBudgetsFromTestingMd(t *testing.T) {
	budgets, err := readBudgets("../../docs/developer/testing.md")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]budget{}
	for _, b := range budgets {
		ids[b.id] = b
	}
	for _, b := range benchmarks {
		if _, ok := ids[b.budget]; !ok {
			t.Errorf("%s measures %s, which testing.md does not list", b.name, b.budget)
		}
	}
	for id := range notMeasured {
		if _, ok := ids[id]; !ok {
			t.Errorf("notMeasured names %s, which testing.md does not list", id)
		}
	}
	if p1 := ids["P1"]; !strings.Contains(p1.scenario, "first rows") || !strings.Contains(p1.target, "1 s") {
		t.Errorf("P1: %+v", p1)
	}
}

func TestReadBenchParsesSubBenchmarks(t *testing.T) {
	path := filepath.Join(t.TempDir(), benchFile)
	out := "goos: darwin\npkg: github.com/guided-traffic/cogpit/internal/engine\n" +
		"BenchmarkSnapshot10k/name-20  \t     346\t   3344388 ns/op\t 1450912 B/op\t      10 allocs/op\n" +
		"BenchmarkSortCommand10k-20                 \t     100\t  61473977 ns/op\n" +
		"BenchmarkSomethingElse-20 \t 1 \t 5 ns/op\nPASS\n"
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, err := readBench(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("%d lines: %+v", len(lines), lines)
	}
	snap, sort := lines[0], lines[1]
	if snap.Budget != "P4" || snap.Test != "BenchmarkSnapshot10k/name" || !strings.Contains(snap.Metric, "(name)") ||
		snap.Value < 3.34 || snap.Value > 3.35 || snap.Unit != "ms" || snap.Pass == nil || !*snap.Pass || snap.GOMAXPROCS != 20 ||
		snap.Package != "github.com/guided-traffic/cogpit/internal/engine" {
		t.Errorf("the snapshot: %+v", snap)
	}
	if sort.Budget != "P2" || sort.Pass == nil || *sort.Pass {
		t.Errorf("61 ms against 50 ms passed: %+v", sort)
	}
}

func line(budget, test string, value, limit float64, bound scorecard.Bound) scorecard.Line {
	l := scorecard.Line{Measurement: scorecard.Measurement{Budget: budget, Metric: test + " metric", Value: value, Unit: "ms", Limit: limit, Bound: bound},
		Test: test, Time: time.Date(2026, 10, 16, 1, 0, 0, 0, time.UTC)}
	if pass, ok := l.Within(); ok {
		l.Pass = &pass
	}
	return l
}

// TestRenderJudgesEachBudget: a budget passes when every bounded measurement does, fails with one
// over, a budget of context values only is no verdict, and one without measurements is listed with
// its reason; testing.md's links become their text.
func TestRenderJudgesEachBudget(t *testing.T) {
	budgets := []budget{
		{id: "P1", scenario: "Open a context ([D-1](decisions.md#d-1-ui-toolkit))", target: "≤ 1 s"},
		{id: "P2", scenario: "Sort", target: "≤ 50 ms"},
		{id: "P3", scenario: "Scroll", target: "every frame ≤ 16 ms"},
		{id: "P5", scenario: "Logs", target: "no frame over 16 ms"},
		{id: "P99", scenario: "Something new", target: "≤ 1"},
	}
	lines := []scorecard.Line{
		line("P1", "TestA", 100, 1000, scorecard.AtMost),
		line("P1", "TestA/sub", 1200, 1000, scorecard.AtMost),
		line("P2", "BenchmarkSort", 7, 50, scorecard.AtMost),
		line("P5", "TestLogs", 13, 0, scorecard.Context),
	}
	page, err := render(budgets, lines, machine{goos: "darwin", goarch: "arm64", cpus: 4, goVersion: "go1.27.2"}, lastDate(lines))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"| [P1](#p1) | Open a context (D-1) | ≤ 1 s | **fail** (1 of 2 over) | `TestA` |",
		"| [P2](#p2) | Sort | ≤ 50 ms | **pass** (1 of 1) | `BenchmarkSort` |",
		"| P3 | Scroll | every frame ≤ 16 ms | not measured | – |",
		"| [P5](#p5) | Logs | no frame over 16 ms | context only | `TestLogs` |",
		"| TestA/sub metric | 1200 ms | at most 1000 ms | **fail** | `TestA/sub` |",
		"- **P3** (Scroll): a real window's frames",
		"- **P99** (Something new): no test records it yet.",
		"**Measured 2026-10-16** on darwin/arm64 (4 CPUs) with go1.27.2,",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %q:\n%s", want, page)
		}
	}
	if _, err := render(budgets[:1], lines, machine{}, time.Now()); err == nil || !strings.Contains(err.Error(), `"P2"`) {
		t.Errorf("a budget testing.md does not list: %v", err)
	}
}

// TestCollectRefusesASilentTest: a budget test that recorded nothing fails the run by name.
func TestCollectRefusesASilentTest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "scorecard-1.jsonl"), []byte(`{"budget":"P1","metric":"m","value":1,"unit":"ms","limit":2,"bound":"at most","test":"TestBudgetFirstRows/1000"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := collect(dir)
	if err == nil || !strings.Contains(err.Error(), "TestLogsP5 recorded nothing") || strings.Contains(err.Error(), "TestBudgetFirstRows recorded") ||
		!strings.Contains(err.Error(), "BenchmarkSortChange10k reported no result") {
		t.Errorf("collect: %v", err)
	}
}

func TestCell(t *testing.T) {
	if got := cell("a [link](x.md) | b", 100); got != `a link \| b` {
		t.Errorf("cell = %q", got)
	}
	if got := cell("one two three `four five` six", 22); got != "one two three `four` …" {
		t.Errorf("cut = %q", got)
	}
}
