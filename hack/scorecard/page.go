package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/guided-traffic/cogpit/internal/testkit/scorecard"
)

// budget is a row of testing.md's table of performance budgets.
type budget struct {
	id, scenario, target string
}

const budgetsHeading = "## Performance budgets"

// readBudgets reads the budgets' ids, scenarios and proposed targets from testing.md's table.
func readBudgets(name string) ([]budget, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []budget
	in := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24) // the rows are long lines
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "## ") {
			in = strings.HasPrefix(line, budgetsHeading)
			continue
		}
		if !in || !strings.HasPrefix(line, "| P") {
			continue
		}
		cells := strings.Split(strings.TrimSuffix(strings.TrimPrefix(line, "| "), " |"), " | ")
		if len(cells) < 3 {
			return nil, fmt.Errorf("%s: a budget row with %d cells: %.60s", name, len(cells), line)
		}
		out = append(out, budget{id: cells[0], scenario: cells[1], target: cells[2]})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no budget rows under %q", name, budgetsHeading)
	}
	return out, nil
}

// machine describes where the measurements ran; never the host's name.
type machine struct {
	goos, goarch, cpu, osVersion, goVersion string
	cpus                                    int
}

func describeMachine() machine {
	m := machine{goos: runtime.GOOS, goarch: runtime.GOARCH, cpus: runtime.NumCPU(), goVersion: runtime.Version()}
	switch runtime.GOOS {
	case "darwin":
		m.cpu = command("sysctl", "-n", "machdep.cpu.brand_string")
		if v := command("sw_vers", "-productVersion"); v != "" {
			m.osVersion = "macOS " + v
		}
	case "linux":
		m.cpu = fileField("/proc/cpuinfo", "model name", ":")
		m.osVersion = strings.Trim(fileField("/etc/os-release", "PRETTY_NAME", "="), `"`)
	}
	return m
}

func command(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// fileField returns the value of the first line of a file that starts with key, after sep.
func fileField(name, key, sep string) string {
	raw, err := os.ReadFile(name)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, key) {
			if _, v, ok := strings.Cut(line, sep); ok {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

func (m machine) String() string {
	var about []string
	for _, s := range []string{m.cpu, fmt.Sprintf("%d CPUs", m.cpus), m.osVersion} {
		if s != "" {
			about = append(about, s)
		}
	}
	return fmt.Sprintf("%s/%s (%s) with %s", m.goos, m.goarch, strings.Join(about, ", "), m.goVersion)
}

// render writes the page.
func render(budgets []budget, lines []scorecard.Line, m machine, date time.Time) (string, error) {
	byBudget := map[string][]scorecard.Line{}
	known := map[string]bool{}
	for _, b := range budgets {
		known[b.id] = true
	}
	for _, l := range lines {
		if !known[l.Budget] {
			return "", fmt.Errorf("%s records budget %q, which testing.md's table does not list", l.Test, l.Budget)
		}
		byBudget[l.Budget] = append(byBudget[l.Budget], l)
	}
	for _, ls := range byBudget {
		sort.SliceStable(ls, func(i, j int) bool { return topLevel(ls[i].Test) < topLevel(ls[j].Test) })
	}

	var b strings.Builder
	b.WriteString("# Scorecard\n\n")
	b.WriteString("> **Generated** by `make scorecard` ([`hack/scorecard`](../../hack/scorecard/main.go)) from what the budget\n")
	b.WriteString("> tests and benchmarks measured; do not edit. Not run in CI ([D-15](../developer/decisions.md#d-15-progress-generators)):\n")
	b.WriteString("> run it on the machine named below and commit the page. The budgets and how each is measured:\n")
	b.WriteString("> [testing › Performance budgets](../developer/testing.md#performance-budgets); the work list:\n")
	b.WriteString("> [G15](gaps.md#g15-performance-and-api-load-at-scale).\n\n")
	fmt.Fprintf(&b, "**Measured %s** on %s,\n", date.Format("2006-01-02"), m)
	b.WriteString("the race detector off. A budget passes when every bounded measurement of it is within its limit;\n")
	b.WriteString("a measurement marked context explains the others and has no limit. The limits are testing.md's\n")
	b.WriteString("proposed targets, not yet validated against Lens Desktop on the same machine, and the tests\n")
	b.WriteString("measure Cogpit's share: against fakes on the loopback, offscreen, without a real cluster's round\n")
	b.WriteString("trips or a GPU frame.\n\n")

	b.WriteString("## Summary\n\n| Budget | Scenario | Proposed target | Result | Measured by |\n|---|---|---|---|---|\n")
	for _, bud := range budgets {
		ls := byBudget[bud.id]
		id := bud.id
		if len(ls) > 0 { // only a measured budget has a section of its own below
			id = fmt.Sprintf("[%s](#%s)", bud.id, strings.ToLower(bud.id))
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", id, cell(bud.scenario, 110), cell(bud.target, 110), verdict(ls), measuredBy(ls))
	}

	b.WriteString("\n## Measurements\n")
	for _, bud := range budgets {
		ls := byBudget[bud.id]
		if len(ls) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n### %s\n\n%s\n\n", bud.id, cell(bud.scenario, 400))
		b.WriteString("| Measurement | Value | Limit | Result | Test |\n|---|---:|---|---|---|\n")
		for _, l := range ls {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | `%s` |\n", cell(l.Metric, 200), quantity(l.Value, l.Unit),
				limit(l), result(l), l.Test)
		}
		hows := howsOf(ls)
		if len(hows) > 0 {
			b.WriteString("\n")
			for _, h := range hows {
				fmt.Fprintf(&b, "- %s\n", h)
			}
		}
	}

	b.WriteString("\n## Not measured by make scorecard\n\n")
	all := true
	for _, bud := range budgets {
		if len(byBudget[bud.id]) == 0 {
			fmt.Fprintf(&b, "- **%s** (%s): %s.\n", bud.id, cell(bud.scenario, 80), reasonNotMeasured(bud.id))
			all = false
		}
	}
	if all {
		b.WriteString("Every budget has a measurement.\n")
	}
	b.WriteString("\nThe cluster tier's measurements (kind, Prometheus, the Metrics Server) are not part of a run: they need\n")
	b.WriteString("`make kind-up`; testing.md records them by hand.\n")
	return b.String(), nil
}

func reasonNotMeasured(id string) string {
	if r, ok := notMeasured[id]; ok {
		return r
	}
	return "no test records it yet"
}

// verdict is a budget's result in the summary.
func verdict(ls []scorecard.Line) string {
	if len(ls) == 0 {
		return "not measured"
	}
	bounded, over := 0, 0
	for _, l := range ls {
		if l.Pass == nil {
			continue
		}
		bounded++
		if !*l.Pass {
			over++
		}
	}
	switch {
	case bounded == 0:
		return "context only"
	case over == 0:
		return fmt.Sprintf("**pass** (%d of %d)", bounded, bounded)
	default:
		return fmt.Sprintf("**fail** (%d of %d over)", over, bounded)
	}
}

func measuredBy(ls []scorecard.Line) string {
	seen := map[string]bool{}
	var names []string
	for _, l := range ls {
		if t := topLevel(l.Test); !seen[t] {
			seen[t] = true
			names = append(names, "`"+t+"`")
		}
	}
	if len(names) == 0 {
		return "–"
	}
	return strings.Join(names, ", ")
}

func limit(l scorecard.Line) string {
	if l.Bound == scorecard.Context {
		return "context"
	}
	return fmt.Sprintf("%s %s", l.Bound, quantity(l.Limit, l.Unit))
}

// quantity writes a value with its unit, "1 request" in the singular.
func quantity(v float64, unit string) string {
	if v == 1 && unit == "requests" {
		unit = "request"
	}
	return scorecard.Format(v) + " " + unit
}

func result(l scorecard.Line) string {
	switch {
	case l.Pass == nil:
		return "–"
	case *l.Pass:
		return "pass"
	default:
		return "**fail**"
	}
}

// howsOf lists how a budget's measurements were taken, once per test and text.
func howsOf(ls []scorecard.Line) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range ls {
		if l.How == "" {
			continue
		}
		h := fmt.Sprintf("`%s`: %s.", topLevel(l.Test), strings.TrimSuffix(l.How, "."))
		if strings.HasPrefix(l.How, "time per operation") {
			h = fmt.Sprintf("`%s`: the benchmark's time per operation.", topLevel(l.Test))
		}
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}

var mdLink = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)

// cell makes testing.md's text fit a table cell of this page: links as their text (they are
// relative to docs/developer), pipes escaped, cut at a word after width characters.
func cell(s string, width int) string {
	s = mdLink.ReplaceAllString(s, "$1")
	s = strings.ReplaceAll(s, "|", `\|`)
	if r := []rune(s); len(r) > width {
		cut := string(r[:width])
		if i := strings.LastIndexByte(cut, ' '); i > width/2 {
			cut = cut[:i]
		}
		s = strings.TrimRight(cut, " ,;:(") + " …"
		if strings.Count(s, "`")%2 == 1 {
			s = strings.Replace(s, " …", "` …", 1)
		}
	}
	return s
}
