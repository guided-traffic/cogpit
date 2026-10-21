// Command coverage merges Go coverage profiles into one report (docs/developer/releasing.md#ci):
// CI's unit, race and integration profiles become the combined coverage, the README's badge and
// the job summary. A block that several profiles hold counts once, as covered when any of them
// covered it, so the merged profile is in mode set.
//
//	go run ./hack/coverage -o .cache/coverage/combined.out -badge .github/badges/coverage.json \
//	    -summary summary.md .cache/coverage/*.out
//
// The badge is coloured as in the team's other repositories: >= 90 brightgreen, >= 80 green,
// >= 70 yellow, >= 60 orange, else red.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

func main() {
	out := flag.String("o", "", "write the merged profile here (mode set)")
	badge := flag.String("badge", "", "write the shields.io endpoint badge here")
	summary := flag.String("summary", "", "write a Markdown summary here: the total and a table per package")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "coverage: name at least one profile")
		os.Exit(2)
	}

	m := merged{}
	for _, p := range flag.Args() {
		if err := m.add(p); err != nil {
			fmt.Fprintln(os.Stderr, "coverage:", err)
			os.Exit(1)
		}
	}
	covered, total := m.total()
	pct := percent(covered, total)
	fmt.Printf("coverage: %.1f%% of %d statements (%d profiles)\n", pct, total, flag.NArg())

	if *out != "" {
		if err := write(*out, m.writeProfile); err != nil {
			fmt.Fprintln(os.Stderr, "coverage:", err)
			os.Exit(1)
		}
	}
	if *badge != "" {
		if err := write(*badge, func(w io.Writer) error { return writeBadge(w, pct) }); err != nil {
			fmt.Fprintln(os.Stderr, "coverage:", err)
			os.Exit(1)
		}
	}
	if *summary != "" {
		if err := write(*summary, func(w io.Writer) error { return m.writeSummary(w, flag.Args()) }); err != nil {
			fmt.Fprintln(os.Stderr, "coverage:", err)
			os.Exit(1)
		}
	}
}

// block is one profile block: its statements and whether any profile covered it.
type block struct {
	stmts   int
	covered bool
}

// merged holds the blocks of every profile, keyed by "file:start,end".
type merged map[string]block

// add reads one profile ("mode: …", then "file:sl.sc,el.ec stmts count" per line).
func (m merged) add(name string) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for line := 1; sc.Scan(); line++ {
		text := sc.Text()
		if line == 1 {
			if !strings.HasPrefix(text, "mode: ") {
				return fmt.Errorf("%s: not a coverage profile", name)
			}
			continue
		}
		fields := strings.Fields(text)
		if len(fields) != 3 {
			return fmt.Errorf("%s:%d: %q is not a profile line", name, line, text)
		}
		stmts, err1 := strconv.Atoi(fields[1])
		count, err2 := strconv.ParseInt(fields[2], 10, 64)
		if err1 != nil || err2 != nil {
			return fmt.Errorf("%s:%d: %q is not a profile line", name, line, text)
		}
		b := m[fields[0]]
		b.stmts = stmts
		b.covered = b.covered || count > 0
		m[fields[0]] = b
	}
	return sc.Err()
}

func (m merged) total() (covered, total int) {
	for _, b := range m {
		total += b.stmts
		if b.covered {
			covered += b.stmts
		}
	}
	return covered, total
}

func (m merged) writeProfile(w io.Writer) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	bw := bufio.NewWriter(w)
	fmt.Fprintln(bw, "mode: set")
	for _, k := range keys {
		n := 0
		if m[k].covered {
			n = 1
		}
		fmt.Fprintf(bw, "%s %d %d\n", k, m[k].stmts, n)
	}
	return bw.Flush()
}

// writeSummary writes the total and one row per package (the directory of the block's file).
func (m merged) writeSummary(w io.Writer, profiles []string) error {
	type sums struct{ covered, total int }
	pkgs := map[string]*sums{}
	for k, b := range m {
		file, _, _ := strings.Cut(k, ":")
		p := pkgs[path.Dir(file)]
		if p == nil {
			p = &sums{}
			pkgs[path.Dir(file)] = p
		}
		p.total += b.stmts
		if b.covered {
			p.covered += b.stmts
		}
	}
	names := make([]string, 0, len(pkgs))
	for n := range pkgs {
		names = append(names, n)
	}
	slices.Sort(names)
	covered, total := m.total()
	fmt.Fprintf(w, "## Combined coverage: %.1f%%\n\n", percent(covered, total))
	bases := make([]string, len(profiles))
	for i, p := range profiles {
		bases[i] = "`" + filepath.Base(p) + "`"
	}
	fmt.Fprintf(w, "%d of %d statements, merged from %s. A block counts as covered when any profile covered it.\n\n",
		covered, total, strings.Join(bases, ", "))
	fmt.Fprintln(w, "| Package | Coverage | Statements |")
	fmt.Fprintln(w, "|---|---:|---:|")
	for _, n := range names {
		p := pkgs[n]
		fmt.Fprintf(w, "| `%s` | %.1f%% | %d/%d |\n", n, percent(p.covered, p.total), p.covered, p.total)
	}
	return nil
}

func writeBadge(w io.Writer, pct float64) error {
	color := "red"
	switch {
	case pct >= 90:
		color = "brightgreen"
	case pct >= 80:
		color = "green"
	case pct >= 70:
		color = "yellow"
	case pct >= 60:
		color = "orange"
	}
	_, err := fmt.Fprintf(w, "{\n  \"schemaVersion\": 1,\n  \"label\": \"coverage\",\n  \"message\": \"%.1f%%\",\n  \"color\": \"%s\"\n}\n", pct, color)
	return err
}

func percent(covered, total int) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(covered) / float64(total)
}

// write creates name (and its directory) and fills it with fill.
func write(name string, fill func(io.Writer) error) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	if err := fill(f); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
