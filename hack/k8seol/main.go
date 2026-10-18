// Command k8seol refreshes the Kubernetes release table that internal/model/k8seol embeds
// (releases.json; docs/developer/decisions.md D-91): every minor's end of life, and the release
// day and maintenance mode of the maintained ones, from the Kubernetes project's own files,
// kubernetes/website's data/releases/schedule.yaml (the maintained minors) and
// data/releases/eol.yaml (the ended ones), which schedule-builder keeps (CC BY 4.0,
// ATTRIBUTION.md).
//
// It downloads both, checks them (the dates parse, the minors follow one another, the ends of
// life in order, each 12 to 18 months after its release, k8seol.Table.Validate) and checks the new
// table against the committed one: no minor lost, no newest minor older, no known minor's end of
// life moved by more than 120 days. It prints what changed, then writes the file.
//
// With -fallback (the release pipeline's, .github/workflows/build.yml) a failed download or check
// keeps the committed table, says why and exits 0, so the build goes on with the table of the last
// commit (a newer minor gets the estimate); without it the command fails. The file is data: dates
// that shift a warning, never code.
//
//	make k8s-eol   # go run ./hack/k8seol -o internal/model/k8seol/releases.json
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/guided-traffic/cogpit/internal/model/k8seol"
)

// The upstream files, on the branch kubernetes.io is built from.
const (
	scheduleURL = "https://raw.githubusercontent.com/kubernetes/website/main/data/releases/schedule.yaml"
	eolURL      = "https://raw.githubusercontent.com/kubernetes/website/main/data/releases/eol.yaml"
	source      = "kubernetes/website data/releases/schedule.yaml and eol.yaml (CC BY 4.0)"
	// maxFile bounds a download: both files are a few kilobytes.
	maxFile = 1 << 20
	// maxMove is how far a known minor's end of life may move against the committed table: upstream
	// moved them by weeks (1.31 by 14 days, 1.22 by 41); a move of months is a broken or altered
	// file, which keeps the committed table.
	maxMove = 120 * 24 * time.Hour
)

func main() {
	out := flag.String("o", "internal/model/k8seol/releases.json", "the file to write; its table is the one checked against")
	fallback := flag.Bool("fallback", false, "on a failed download or check keep the file, say why and exit 0 (the release pipeline)")
	sched := flag.String("schedule", scheduleURL, "schedule.yaml's URL")
	ended := flag.String("eol", eolURL, "eol.yaml's URL")
	timeout := flag.Duration("timeout", 30*time.Second, "the bound of each download")
	flag.Parse()
	client := &http.Client{Timeout: *timeout}
	err := run(context.Background(), client, *out, *sched, *ended, time.Now().UTC(), os.Stdout)
	switch {
	case err == nil:
	case *fallback && keepable(*out):
		fmt.Fprintf(os.Stderr, "k8seol: %v; keeping %s\n", err, *out)
		if os.Getenv("GITHUB_ACTIONS") == "true" {
			fmt.Printf("::warning title=Kubernetes release table::%s; the release keeps the committed table\n", oneLine(err.Error()))
		}
	default:
		fmt.Fprintln(os.Stderr, "k8seol:", err)
		os.Exit(1)
	}
}

// keepable reports whether the committed table is one to build with.
func keepable(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	_, err = k8seol.ParseTable(data)
	return err == nil
}

func oneLine(s string) string { return strings.ReplaceAll(s, "\n", " ") }

// run downloads both files, builds and checks the table, checks it against the committed one at
// out and writes it there.
func run(ctx context.Context, client *http.Client, out, schedURL, eolURL string, now time.Time, log io.Writer) error {
	sched, err := fetch(ctx, client, schedURL)
	if err != nil {
		return err
	}
	ended, err := fetch(ctx, client, eolURL)
	if err != nil {
		return err
	}
	t, err := build(sched, ended, now)
	if err != nil {
		return err
	}
	var old *k8seol.Table
	if data, err := os.ReadFile(out); err == nil {
		if old, err = k8seol.ParseTable(data); err != nil {
			return fmt.Errorf("the committed table %s: %w", out, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	changes, err := compare(old, t)
	if err != nil {
		return err
	}
	data := encode(t)
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return err
	}
	newest := t.Newest()
	fmt.Fprintf(log, "wrote %s: %d minors, %s to %s (%s ends %s), fetched %s\n", out, len(t.Minors), t.Minors[0].Minor,
		newest.Minor, newest.Minor, newest.EndOfLife, t.Fetched)
	for _, c := range changes {
		fmt.Fprintln(log, "  "+c)
	}
	return nil
}

// fetch downloads one file, bounded.
func fetch(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFile+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	if len(data) > maxFile {
		return nil, fmt.Errorf("download %s: larger than %d bytes", url, maxFile)
	}
	return data, nil
}

// The upstream files' fields Cogpit reads.
type scheduleFile struct {
	Schedules []struct {
		Release                  string `json:"release"`
		ReleaseDate              string `json:"releaseDate"`
		MaintenanceModeStartDate string `json:"maintenanceModeStartDate"`
		EndOfLifeDate            string `json:"endOfLifeDate"`
	} `json:"schedules"`
}

type eolFile struct {
	Branches []struct {
		Release           string `json:"release"`
		EndOfLifeDate     string `json:"endOfLifeDate"`
		FinalPatchRelease string `json:"finalPatchRelease"`
	} `json:"branches"`
}

// build merges the two files into a table and checks it. A minor in both (upstream moving it from
// one file to the other) takes eol.yaml's end of life and final patch and keeps schedule.yaml's
// release day and maintenance mode.
func build(sched, ended []byte, now time.Time) (*k8seol.Table, error) {
	var sf scheduleFile
	if err := yaml.Unmarshal(sched, &sf); err != nil {
		return nil, fmt.Errorf("schedule.yaml: %w", err)
	}
	var ef eolFile
	if err := yaml.Unmarshal(ended, &ef); err != nil {
		return nil, fmt.Errorf("eol.yaml: %w", err)
	}
	if len(sf.Schedules) == 0 || len(ef.Branches) == 0 {
		return nil, fmt.Errorf("schedule.yaml lists %d minors and eol.yaml %d; want both", len(sf.Schedules), len(ef.Branches))
	}
	by := map[string]*k8seol.Minor{}
	for _, s := range sf.Schedules {
		if by[s.Release] != nil {
			return nil, fmt.Errorf("schedule.yaml lists %s twice", s.Release)
		}
		by[s.Release] = &k8seol.Minor{Minor: s.Release, Released: s.ReleaseDate, MaintenanceFrom: s.MaintenanceModeStartDate, EndOfLife: s.EndOfLifeDate}
	}
	seen := map[string]bool{}
	for _, b := range ef.Branches {
		if seen[b.Release] {
			return nil, fmt.Errorf("eol.yaml lists %s twice", b.Release)
		}
		seen[b.Release] = true
		m := by[b.Release]
		if m == nil {
			m = &k8seol.Minor{Minor: b.Release}
			by[b.Release] = m
		}
		m.EndOfLife, m.FinalPatch = b.EndOfLifeDate, b.FinalPatchRelease
	}
	t := &k8seol.Table{Source: source, Fetched: now.Format(k8seol.DateLayout)}
	for _, m := range by {
		t.Minors = append(t.Minors, *m)
	}
	slices.SortFunc(t.Minors, func(a, b k8seol.Minor) int {
		va, _ := k8seol.ParseVersion(a.Minor)
		vb, _ := k8seol.ParseVersion(b.Minor)
		return va.Minor - vb.Minor
	})
	if err := t.Validate(); err != nil {
		return nil, err
	}
	return t, nil
}

// compare checks the new table against the committed one (nil: none yet) and returns what
// changed: the minors added, the days that moved.
func compare(old, t *k8seol.Table) ([]string, error) {
	if old == nil {
		return []string{"no committed table: every minor is new"}, nil
	}
	var changes []string
	for _, o := range old.Minors {
		v, _ := k8seol.ParseVersion(o.Minor)
		n, ok := t.Lookup(v)
		if !ok {
			return nil, fmt.Errorf("minor %s is in the committed table but not in upstream's files", o.Minor)
		}
		oe, _ := k8seol.ParseDate(o.EndOfLife)
		ne, _ := k8seol.ParseDate(n.EndOfLife)
		if d := ne.Sub(oe); d > maxMove || d < -maxMove {
			return nil, fmt.Errorf("%s's end of life moves from %s to %s, more than %d days", o.Minor, o.EndOfLife, n.EndOfLife, int(maxMove.Hours()/24))
		}
		if o != n {
			changes = append(changes, fmt.Sprintf("%s: %+v -> %+v", o.Minor, o, n))
		}
	}
	for _, n := range t.Minors {
		v, _ := k8seol.ParseVersion(n.Minor)
		if _, ok := old.Lookup(v); !ok {
			changes = append(changes, fmt.Sprintf("%s added: %+v", n.Minor, n))
		}
	}
	if len(changes) == 0 {
		changes = append(changes, "no minor changed since the committed table (fetched "+old.Fetched+")")
	}
	return changes, nil
}

// encode writes the table as JSON, one minor a line, so that a change is one line of the diff.
func encode(t *k8seol.Table) []byte {
	var b bytes.Buffer
	b.WriteString("{\n")
	fmt.Fprintf(&b, "  \"source\": %s,\n", quote(t.Source))
	fmt.Fprintf(&b, "  \"fetched\": %s,\n", quote(t.Fetched))
	b.WriteString("  \"minors\": [\n")
	for i, m := range t.Minors {
		b.WriteString("    {\"minor\": " + quote(m.Minor))
		for _, f := range []struct{ name, value string }{
			{"released", m.Released}, {"maintenanceFrom", m.MaintenanceFrom}, {"endOfLife", m.EndOfLife}, {"finalPatch", m.FinalPatch},
		} {
			if f.value != "" || f.name == "endOfLife" {
				b.WriteString(", " + quote(f.name) + ": " + quote(f.value))
			}
		}
		b.WriteString("}")
		if i < len(t.Minors)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("  ]\n}\n")
	return b.Bytes()
}

// quote writes a JSON string.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20:
			fmt.Fprintf(&b, "\\u%04x", r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
