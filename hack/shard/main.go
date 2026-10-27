// Command shard splits one package's tests into stable shards for CI's sharded race jobs
// (docs/developer/releasing.md#ci). It reads the package's top-level tests, examples and fuzz
// targets from the test files the build would compile (go list with the given tags decides which
// files count), assigns each name to a shard by an FNV-1a hash of the name, and prints the -run
// pattern of one shard. A test added later lands in a shard without a change to the workflow, and a
// name keeps its shard as long as the number of shards stays the same.
//
// -run and -skip narrow the tests before the split, as go test's flags of those names select
// top-level tests, so the tests they leave keep their shards (the race jobs leave the render tests
// to a run without the race detector: make test-race-ui and test-norace-coverage).
//
// With -exec it runs the shards itself: it compiles the package's tests once and runs every shard
// as a go test run of its own, -jobs of them at once (make test-ui; docs/developer/releasing.md).
// With -norace it prints, a line per package, the -run pattern of the tests the race detector keeps
// from running, for the job that runs them without it (make test-norace-coverage).
//
//	go run ./hack/shard -pkg ./internal/ui/fyneui -tags race -shard 2 -of 6   # the -run pattern of shard 2
//	go run ./hack/shard -pkg ./internal/ui/fyneui -tags race -of 6 -summary   # the tests per shard
//	go run ./hack/shard ... -summary -durations race.json                     # and their time, from go test -json
//	go run ./hack/shard -pkg ./internal/ui/fyneui -of 8 -jobs 8 -exec         # run all eight shards, eight at once
//	go run ./hack/shard -pkg ./internal/ui/fyneui -run '^TestRender' -of 4 -exec  # only the render tests, in four
//	go run ./hack/shard -pkg ./... -norace                                    # per package, the tests -race keeps from running
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"hash/fnv"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func main() {
	pkg := flag.String("pkg", "", "the package, as go list takes it (./internal/ui/fyneui)")
	tags := flag.String("tags", "", "build tags, comma-separated, as go test gets them (race when the shards run with -race)")
	shard := flag.Int("shard", 0, "the shard to print the -run pattern of, 1 to -of")
	of := flag.Int("of", 0, "the number of shards")
	summary := flag.Bool("summary", false, "print the number of tests per shard instead of a pattern")
	durations := flag.String("durations", "", "with -summary: a go test -json file of the package, to sum each shard's time")
	norace := flag.Bool("norace", false, "print, a line per package of -pkg, the package and the -run pattern of its tests the race detector keeps from running")
	run := flag.Bool("exec", false, "build the test binary once and run every shard, -jobs at once")
	jobs := flag.Int("jobs", 0, "with -exec: how many shards run at once (0: as many as the CPUs and the memory allow)")
	race := flag.Bool("race", false, "with -exec: build with the race detector (and split the files of the race build)")
	timeout := flag.Duration("timeout", 30*time.Minute, "with -exec: each shard's -test.timeout")
	cover := flag.String("coverdir", "", "with -exec: build with coverage and write each shard's profile to <dir>/<name>-<n>.out")
	coverName := flag.String("covername", "shard", "with -coverdir: the profiles' name before -<n>.out")
	runRE := flag.String("run", "", "split only the tests whose name matches this regular expression, as go test -run selects a top-level test")
	skipRE := flag.String("skip", "", "leave out the tests whose name matches this regular expression, as go test -skip does a top-level test")
	flag.Parse()

	sel, err := newSelection(*runRE, *skipRE)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shard:", err)
		os.Exit(2)
	}
	if *norace {
		if sel.narrows() {
			fmt.Fprintln(os.Stderr, "shard: -norace takes neither -run nor -skip")
			os.Exit(2)
		}
		if *pkg == "" {
			fmt.Fprintln(os.Stderr, "shard: -norace needs -pkg (a package or a pattern such as ./...)")
			os.Exit(2)
		}
		kept, err := raceKept(*pkg)
		if err != nil {
			fmt.Fprintln(os.Stderr, "shard:", err)
			os.Exit(2)
		}
		n := 0
		for _, p := range kept {
			n += len(p.names)
			fmt.Printf("%s %s\n", p.importPath, pattern(p.names))
		}
		fmt.Fprintf(os.Stderr, "shard: %d tests in %d packages of %s run only without the race detector\n", n, len(kept), *pkg)
		return
	}
	if *run {
		if *pkg == "" || *of < 1 || *jobs < 0 {
			fmt.Fprintln(os.Stderr, "shard: -exec needs -pkg, -of >= 1 and -jobs >= 0")
			os.Exit(2)
		}
		if *race {
			*tags = joinTags(*tags, "race")
		}
		if *jobs == 0 {
			*jobs = defaultJobs(*race, totalMemory())
		}
		ok, err := execShards(execConfig{pkg: *pkg, tags: *tags, sel: sel, race: *race, of: *of, jobs: *jobs,
			timeout: *timeout, coverDir: *cover, coverName: *coverName}, os.Stdout)
		if err != nil {
			fmt.Fprintln(os.Stderr, "shard:", err)
			os.Exit(2)
		}
		if !ok {
			os.Exit(1)
		}
		return
	}

	if *pkg == "" || *of < 1 || (!*summary && (*shard < 1 || *shard > *of)) {
		fmt.Fprintln(os.Stderr, "shard: need -pkg, -of >= 1 and -shard 1..of (or -summary, -norace or -exec)")
		os.Exit(2)
	}
	names, err := testNames(*pkg, *tags)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shard:", err)
		os.Exit(2)
	}
	if names = sel.keep(names); len(names) == 0 {
		fmt.Fprintf(os.Stderr, "shard: %s has no tests%s\n", *pkg, sel)
		os.Exit(1)
	}
	shards := split(names, *of)
	if *summary {
		times := map[string]float64{}
		if *durations != "" {
			if times, err = readDurations(*durations); err != nil {
				fmt.Fprintln(os.Stderr, "shard:", err)
				os.Exit(2)
			}
		}
		printSummary(os.Stdout, shards, times)
		return
	}
	mine := shards[*shard-1]
	if len(mine) == 0 {
		// A pattern no test name matches: go test runs nothing and passes.
		fmt.Fprintf(os.Stderr, "shard: shard %d of %d is empty\n", *shard, *of)
		fmt.Println("^$")
		return
	}
	fmt.Fprintf(os.Stderr, "shard: %d of %d tests in shard %d of %d\n", len(mine), len(names), *shard, *of)
	fmt.Println(pattern(mine))
}

// split assigns every name to one of n shards by its hash; each shard keeps the names' order.
func split(names []string, n int) [][]string {
	shards := make([][]string, n)
	for _, name := range names {
		h := fnv.New32a()
		_, _ = h.Write([]byte(name))
		i := int(h.Sum32() % uint32(n))
		shards[i] = append(shards[i], name)
	}
	return shards
}

// pattern is the -run pattern that matches exactly the given top-level names (their subtests
// run with them).
func pattern(names []string) string {
	return "^(" + strings.Join(names, "|") + ")$"
}

// selection is what -run and -skip leave of a package's tests; nil expressions select all.
type selection struct{ run, skip *regexp.Regexp }

// newSelection compiles -run and -skip. They match top-level names only: a slash, which go test
// reads as a subtest's level, is refused.
func newSelection(run, skip string) (selection, error) {
	var s selection
	for _, f := range []struct {
		flag, expr string
		re         **regexp.Regexp
	}{{"-run", run, &s.run}, {"-skip", skip, &s.skip}} {
		if f.expr == "" {
			continue
		}
		if strings.Contains(f.expr, "/") {
			return s, fmt.Errorf("%s %q: the expression matches a top-level test's name, which has no slash", f.flag, f.expr)
		}
		re, err := regexp.Compile(f.expr)
		if err != nil {
			return s, fmt.Errorf("%s: %w", f.flag, err)
		}
		*f.re = re
	}
	return s, nil
}

// narrows reports whether -run or -skip was given, so that the shards' patterns must name their tests.
func (s selection) narrows() bool { return s.run != nil || s.skip != nil }

// keep is the names the selection leaves, in their order.
func (s selection) keep(names []string) []string {
	return slices.DeleteFunc(slices.Clone(names), func(n string) bool {
		return (s.run != nil && !s.run.MatchString(n)) || (s.skip != nil && s.skip.MatchString(n))
	})
}

// String names the selection for a message: empty, or " selected by -run … and -skip …".
func (s selection) String() string {
	var parts []string
	if s.run != nil {
		parts = append(parts, "-run "+s.run.String())
	}
	if s.skip != nil {
		parts = append(parts, "-skip "+s.skip.String())
	}
	if len(parts) == 0 {
		return ""
	}
	return " selected by " + strings.Join(parts, " and ")
}

// testNames lists the package's top-level Test, Example and Fuzz functions, sorted, from the test
// files go list reports for the given tags (TestMain is not a test).
func testNames(pkg, tags string) ([]string, error) {
	files, err := testFiles(pkg, tags)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, file := range files {
		for _, d := range file.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && isTopLevelTest(fn) {
				names = append(names, fn.Name.Name)
			}
		}
	}
	slices.Sort(names)
	return slices.Compact(names), nil
}

// testFiles parses the package's test files that go list reports for the given tags.
func testFiles(pkg, tags string) ([]*ast.File, error) {
	args := []string{"list", "-json"}
	if tags != "" {
		args = append(args, "-tags", tags)
	}
	out, err := exec.Command("go", append(args, pkg)...).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("go list %s: %v: %s", pkg, err, ee.Stderr)
		}
		return nil, fmt.Errorf("go list %s: %w", pkg, err)
	}
	var p struct {
		Dir          string
		TestGoFiles  []string
		XTestGoFiles []string
	}
	if err := json.Unmarshal(out, &p); err != nil {
		return nil, fmt.Errorf("go list %s: %w", pkg, err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, f := range append(p.TestGoFiles, p.XTestGoFiles...) {
		file, err := parser.ParseFile(fset, filepath.Join(p.Dir, f), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

// isTopLevelTest reports whether fn is a test, example or fuzz target the go tool runs.
func isTopLevelTest(fn *ast.FuncDecl) bool {
	if fn.Recv != nil || fn.Name.Name == "TestMain" {
		return false
	}
	name := fn.Name.Name
	return isTest(name, "Test") || isTest(name, "Example") || isTest(name, "Fuzz")
}

// isTest reports whether name is a test function name for prefix, by the go tool's rule: the
// prefix alone, or the prefix followed by a rune that is not a lower-case letter.
func isTest(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	if len(name) == len(prefix) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(name[len(prefix):])
	return !unicode.IsLower(r)
}

// readDurations sums the elapsed seconds of each top-level test in a go test -json file.
func readDurations(path string) (map[string]float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	times := map[string]float64{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var ev struct {
			Action  string
			Test    string
			Elapsed float64
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil || ev.Test == "" || strings.Contains(ev.Test, "/") {
			continue
		}
		if ev.Action == "pass" || ev.Action == "fail" || ev.Action == "skip" {
			times[ev.Test] = ev.Elapsed
		}
	}
	return times, sc.Err()
}

func printSummary(w io.Writer, shards [][]string, times map[string]float64) {
	for i, names := range shards {
		var sum, longest float64
		slowest := ""
		for _, n := range names {
			sum += times[n]
			if times[n] > longest {
				longest, slowest = times[n], n
			}
		}
		if len(times) == 0 {
			fmt.Fprintf(w, "shard %d: %d tests\n", i+1, len(names))
			continue
		}
		fmt.Fprintf(w, "shard %d: %d tests, %.0f s (longest %s, %.0f s)\n", i+1, len(names), sum, slowest, longest)
	}
}
