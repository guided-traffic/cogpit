package kinds_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utiljson "k8s.io/apimachinery/pkg/util/json"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

var update = flag.Bool("update", false, "rewrite the golden files from the current output")

// now is the fixed time the goldens render ages against.
var now = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

// testCase is one recorded object of testdata/<kind>/<name>.yaml.
type testCase struct {
	name string
	path string // the YAML file
	json []byte // the object as JSON, for fuzz seeds
	obj  *unstructured.Unstructured
}

// loadCases reads every recorded object of a kind. Objects decode the way the cache decodes them:
// integers as int64, other numbers as float64.
func loadCases(t testing.TB, key string) []testCase {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", key, "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatalf("no testdata for %s", key)
	}
	cases := make([]testCase, 0, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		data, err := utilyaml.ToJSON(raw)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		var m map[string]any
		if err := utiljson.Unmarshal(data, &m); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		cases = append(cases, testCase{
			name: strings.TrimSuffix(filepath.Base(path), ".yaml"),
			path: path,
			json: data,
			obj:  &unstructured.Unstructured{Object: m},
		})
	}
	return cases
}

type goldenCell struct {
	Column string           `json:"column"`
	Text   string           `json:"text"` // model.CellText at the fixed now
	Num    int64            `json:"num,omitempty"`
	Status model.Status     `json:"status,omitempty"`
	Link   *model.ObjectRef `json:"link,omitempty"`
	Parts  []model.CellPart `json:"parts,omitempty"`
	Web    []model.WebLink  `json:"web,omitempty"`
}

type golden struct {
	Ref    model.ObjectRef       `json:"ref"`
	Status model.Status          `json:"status"`
	Cells  []goldenCell          `json:"cells"`
	Fields map[string][]string   `json:"fields"`
	Detail []model.DetailSection `json:"detail"`
}

// render builds a case's golden. It calls the profile's functions directly, not only through
// model.BuildRow and model.FieldValues, because those recover from panics and would hide one.
func render(t *testing.T, p *model.KindProfile, obj *unstructured.Unstructured) golden {
	t.Helper()
	cells, status := cellsOf(p, obj)
	if len(cells) != len(p.Columns) {
		t.Fatalf("Cells returned %d cells for %d columns", len(cells), len(p.Columns))
	}
	row := model.BuildRow(p, "test", obj)
	if !slices.EqualFunc(row.Cells, cells, sameCell) {
		t.Fatalf("BuildRow cells differ from Cells: %+v vs %+v", row.Cells, cells)
	}
	if obj.GetDeletionTimestamp() == nil && row.Status != status {
		t.Fatalf("BuildRow status %q, Cells status %q", row.Status, status)
	}
	g := golden{Ref: row.Ref, Status: row.Status, Fields: map[string][]string{}}
	for i, c := range row.Cells {
		g.Cells = append(g.Cells, goldenCell{
			Column: p.Columns[i].ID,
			Text:   model.CellText(p.Columns[i], c, now),
			Num:    c.Num,
			Status: c.Status,
			Link:   c.Link,
			Parts:  partsOf(c),
			Web:    webOf(c),
		})
	}
	for name, f := range p.Fields {
		direct := f(obj)
		values := model.FieldValues(p, name, obj)
		if !slices.Equal(direct, values) {
			t.Fatalf("field %s: FieldValues %q, direct %q", name, values, direct)
		}
		g.Fields[name] = nonNil(values)
	}
	for _, name := range []string{"name", "namespace", "label"} {
		values := model.FieldValues(p, name, obj)
		sort.Strings(values) // labels come from a map
		g.Fields[name] = nonNil(values)
	}
	g.Detail = detailOf(p, obj)
	return g
}

// cellsOf calls the profile's cells function directly: CellsWith without related objects when
// the profile reads them, else Cells.
func cellsOf(p *model.KindProfile, obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	if p.CellsWith != nil {
		return p.CellsWith(obj, model.NoRelated)
	}
	return p.Cells(obj)
}

// detailOf calls the profile's drawer sections directly, without related objects, and writes
// their absolute times (marks) as the engine does, in ISO 8601 in UTC (D-31).
func detailOf(p *model.KindProfile, obj *unstructured.Unstructured) []model.DetailSection {
	return model.TimeLayout{}.In(time.UTC).WriteSections(p.DetailSections(obj, model.NoRelated))
}

// sameCell compares cells by value, links, parts and web links included.
func sameCell(a, b model.Cell) bool {
	if a.Text != b.Text || a.Num != b.Num || a.Status != b.Status || (a.Link == nil) != (b.Link == nil) {
		return false
	}
	return (a.Link == nil || *a.Link == *b.Link) && slices.Equal(partsOf(a), partsOf(b)) && slices.Equal(webOf(a), webOf(b))
}

// webOf returns a cell's web links, nil when it has none.
func webOf(c model.Cell) []model.WebLink {
	if c.Web == nil {
		return nil
	}
	return *c.Web
}

// partsOf returns a cell's coloured parts, nil when it has none.
func partsOf(c model.Cell) []model.CellPart {
	if c.Parts == nil {
		return nil
	}
	return *c.Parts
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func encode(t *testing.T, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // keep "<none>" and "→" readable in the goldens
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestGolden renders every recorded object and compares row, status, fields and detail with its
// golden file. Run with -update to rewrite the goldens, then review them by reading the diff.
func TestGolden(t *testing.T) {
	for _, p := range kinds.Builtin() {
		for _, c := range loadCases(t, p.Key) {
			t.Run(p.Key+"/"+c.name, func(t *testing.T) {
				got := encode(t, render(t, p, c.obj))
				path := strings.TrimSuffix(c.path, ".yaml") + ".golden.json"
				if *update {
					if err := os.WriteFile(path, got, 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("%v (run go test ./internal/model/... -update)", err)
				}
				if !bytes.Equal(got, want) {
					t.Errorf("%s differs from the golden file:\n%s", path, lineDiff(string(want), string(got)))
				}
			})
		}
	}
}

// lineDiff shows the first lines where two texts differ, enough to find the change.
func lineDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	var b strings.Builder
	shown := 0
	for i := 0; i < len(w) || i < len(g); i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl == gl {
			continue
		}
		b.WriteString("line " + strconv.Itoa(i+1) + ":\n  want: " + wl + "\n  got:  " + gl + "\n")
		if shown++; shown == 10 {
			break
		}
	}
	return b.String()
}

// TestTestdataComplete checks that every kind has the baseline cases and that goldens and
// recorded objects match one to one.
func TestTestdataComplete(t *testing.T) {
	for _, p := range kinds.Builtin() {
		for _, base := range []string{"healthy", "failing", "terminating", "minimal"} {
			if _, err := os.Stat(filepath.Join("testdata", p.Key, base+".yaml")); err != nil {
				t.Errorf("%s: missing baseline case: %v", p.Key, err)
			}
		}
		goldens, _ := filepath.Glob(filepath.Join("testdata", p.Key, "*.golden.json"))
		for _, g := range goldens {
			if _, err := os.Stat(strings.TrimSuffix(g, ".golden.json") + ".yaml"); err != nil {
				t.Errorf("orphan golden %s", g)
			}
		}
	}
}

var lowerCamel = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)

// TestBuiltinProfiles checks what every built-in profile must have. It names no kinds: a new
// profile file is checked by adding it to Builtin. The sidebar's order is the navigator's
// (internal/ui/fyneui/sidebar.go navLayout), not Builtin's.
func TestBuiltinProfiles(t *testing.T) {
	keys := map[string]bool{}
	for _, p := range kinds.Builtin() {
		if keys[p.Key] {
			t.Errorf("%s: registered twice", p.Key)
		}
		keys[p.Key] = true
		// Built-in kinds keep their bare plural as the key; only kinds from custom resource
		// definitions take "<plural>.<group>" (architecture.md, Kind keys).
		if p.Key != p.Resource || strings.Contains(p.Key, ".") {
			t.Errorf("%s: key must be the bare plural resource %q", p.Key, p.Resource)
		}
		if p.Title == "" || p.Version == "" || p.Kind == "" {
			t.Errorf("%s: title %q, version %q, kind %q", p.Key, p.Title, p.Version, p.Kind)
		}
		if p.Cells == nil || p.Detail == nil {
			t.Errorf("%s: Cells or Detail missing", p.Key)
		}
		if len(p.Columns) == 0 || p.Columns[0].ID != "name" {
			t.Errorf("%s: the first column must be the name", p.Key)
		}
		seen := map[string]bool{}
		for i, c := range p.Columns {
			if !lowerCamel.MatchString(c.ID) || seen[c.ID] || c.Title == "" {
				t.Errorf("%s: bad or duplicate column %+v", p.Key, c)
			}
			if c.Type == model.ColumnWarning && i != 1 {
				t.Errorf("%s: the warning column must come right after Name", p.Key)
			}
			seen[c.ID] = true
		}
		// A cluster-scoped kind may still show Lens's empty Namespace column (Ingress Classes,
		// captured 2026-10-11); a namespaced kind must show its namespace.
		if p.Namespaced && !seen["namespace"] {
			t.Errorf("%s: namespaced but no namespace column", p.Key)
		}
		for name := range p.Fields {
			if !lowerCamel.MatchString(name) {
				t.Errorf("%s: field %q is not lowerCamelCase", p.Key, name)
			}
		}
		if got, ok := kinds.ByKey(p.Key); !ok || got != p {
			t.Errorf("ByKey(%q) = %v, %v", p.Key, got, ok)
		}
	}
	for _, key := range []string{"nodes", "pods", "deployments", "services", "namespaces", "events"} {
		if !keys[key] {
			t.Errorf("Builtin lacks %s", key)
		}
	}
	if p, _ := kinds.ByKey("deployments"); p.GVR().Group != "apps" {
		t.Errorf("deployments group = %q", p.GVR().Group)
	}
	if _, ok := kinds.ByKey("widgets"); ok {
		t.Error("ByKey found an unknown kind")
	}
}

// TestStatusTexts states the status semantics outright, so that a golden regenerated by mistake
// cannot quietly change them: the Status (or Conditions) column text and token, and the row's.
func TestStatusTexts(t *testing.T) {
	tests := []statusText{
		{"pods", "healthy", "status", "Running", model.StatusOK, model.StatusOK},
		{"pods", "failing", "status", "CrashLoopBackOff", model.StatusError, model.StatusError},
		{"pods", "terminating", "status", "Terminating", model.StatusTerminating, model.StatusTerminating},
		{"pods", "minimal", "status", "Unknown", model.StatusUnknown, model.StatusUnknown},
		{"pods", "imagepullbackoff", "status", "ImagePullBackOff", model.StatusError, model.StatusError},
		{"pods", "init-progress", "status", "Init:1/2", model.StatusPending, model.StatusPending},
		{"pods", "init-crashloop", "status", "Init:CrashLoopBackOff", model.StatusError, model.StatusError},
		{"pods", "completed-job", "status", "Completed", model.StatusSucceeded, model.StatusSucceeded},
		{"pods", "evicted", "status", "Evicted", model.StatusError, model.StatusError},
		{"pods", "oomkilled", "status", "OOMKilled", model.StatusError, model.StatusError},
		{"pods", "not-ready", "status", "Running", model.StatusWarning, model.StatusWarning},
		{"pods", "sidecar", "status", "Running", model.StatusOK, model.StatusOK},
		{"pods", "unschedulable", "status", "Pending", model.StatusPending, model.StatusPending},
		{"pods", "container-creating", "status", "ContainerCreating", model.StatusPending, model.StatusPending},
		{"pods", "create-config-error", "status", "CreateContainerConfigError", model.StatusError, model.StatusError},
		{"pods", "sidecar", "containers", "2/2", model.StatusNone, model.StatusOK},
		{"pods", "not-ready", "restarts", "2", model.StatusNone, model.StatusWarning},
		{"pods", "init-crashloop", "restarts", "4", model.StatusNone, model.StatusError},
		{"pods", "completed-job", "controlledBy", "Job", model.StatusNone, model.StatusSucceeded},

		{"deployments", "healthy", "conditions", "Available Progressing", model.StatusOK, model.StatusOK},
		{"deployments", "failing", "conditions", "", model.StatusError, model.StatusWarning},
		{"deployments", "rolling-out", "conditions", "Progressing", model.StatusPending, model.StatusPending},
		{"deployments", "degraded", "conditions", "Available Progressing", model.StatusOK, model.StatusWarning},
		{"deployments", "replica-failure", "conditions", "Available Progressing ReplicaFailure", model.StatusError, model.StatusPending},
		{"deployments", "paused", "pods", "2/3", model.StatusNone, model.StatusWarning},
		{"deployments", "scaled-to-zero", "pods", "0/0", model.StatusNone, model.StatusNone},
		{"deployments", "minimal", "replicas", "1", model.StatusNone, model.StatusPending},
		{"deployments", "terminating", "pods", "1/1", model.StatusNone, model.StatusTerminating},

		{"services", "healthy", "status", "Active", model.StatusOK, model.StatusOK},
		{"services", "failing", "status", "Pending", model.StatusPending, model.StatusPending},
		{"services", "failing", "ports", "443:30443/TCP", model.StatusNone, model.StatusPending},
		{"services", "failing", "externalIP", "-", model.StatusNone, model.StatusPending},
		{"services", "nodeport", "ports", "53:30053/UDP, 53:30054/TCP", model.StatusNone, model.StatusOK},
		{"services", "loadbalancer", "externalIP", "203.0.113.10, storefront.lb.example.net", model.StatusNone, model.StatusOK},
		{"services", "minimal", "type", "ClusterIP", model.StatusNone, model.StatusOK},
		{"services", "terminating", "status", "Active", model.StatusOK, model.StatusTerminating},

		{"namespaces", "healthy", "status", "Active", model.StatusOK, model.StatusOK},
		{"namespaces", "terminating", "status", "Terminating", model.StatusTerminating, model.StatusTerminating},
		{"namespaces", "failing", "status", "Terminating", model.StatusTerminating, model.StatusTerminating},
		{"namespaces", "minimal", "status", "Unknown", model.StatusUnknown, model.StatusUnknown},

		{"nodes", "healthy", "conditions", "Ready", model.StatusOK, model.StatusOK},
		{"nodes", "failing", "conditions", "NotReady DiskPressure MemoryPressure", model.StatusError, model.StatusError},
		// A cordoned Ready node reads "SchedulingDisabled Ready" and is not a warning (capture,
		// 2026-10-11); before, Cogpit showed "Ready SchedulingDisabled" in the warning colour.
		{"nodes", "cordoned", "conditions", "SchedulingDisabled Ready", model.StatusOK, model.StatusOK},
		{"nodes", "cordoned", "roles", "worker", model.StatusNone, model.StatusOK},
		{"nodes", "unknown", "conditions", "Unknown", model.StatusUnknown, model.StatusUnknown},
		{"nodes", "minimal", "roles", "<none>", model.StatusNone, model.StatusUnknown},
		{"nodes", "failing", "taints", "3", model.StatusNone, model.StatusError},

		// As captured from Lens: the Type cell is plain, a warning's message is red.
		{"events", "healthy", "type", "Normal", model.StatusNone, model.StatusNone},
		{"events", "failing", "type", "Warning", model.StatusNone, model.StatusWarning},
		{"events", "failing", "message", "Back-off restarting failed container worker in pod worker-6f8b9c7d4-q9m2z_payments (exit code 1)", model.StatusError, model.StatusWarning},
		{"events", "healthy", "message", `Container image "registry.example.com/shop/web:1.4.2" already present on machine`, model.StatusNone, model.StatusNone},
		{"events", "failing", "count", "42", model.StatusNone, model.StatusWarning},
		{"events", "warning-series", "count", "17", model.StatusNone, model.StatusWarning},
		// Last Seen is a timestamp, as captured from Lens; written in the user's format when drawn (D-31), ISO 8601 here.
		{"events", "warning-series", "lastSeen", "2026-10-11 11:58:30", model.StatusNone, model.StatusWarning},
		{"events", "warning-series", "age", "3h", model.StatusNone, model.StatusWarning}, // eventTime, whole seconds
		{"events", "new-style", "source", "default-scheduler, default-scheduler-cp-1", model.StatusNone, model.StatusNone},
		{"events", "new-style", "count", "1", model.StatusNone, model.StatusNone},
		{"events", "events-api-shape", "involvedObject", "Deployment: search", model.StatusNone, model.StatusNone},
		{"events", "minimal", "lastSeen", "2026-10-11 11:55:00", model.StatusNone, model.StatusNone},
	}
	checkStatusTexts(t, tests)
}

// statusText is one assertion of checkStatusTexts: the text and status of one cell of one recorded
// object (testdata/<kind>/<name>.yaml), and the row's status.
type statusText struct {
	kind, name, column, text string
	cell, row                model.Status
}

// checkStatusTexts asserts cell texts and statuses outright. A kind's own test file calls it with
// its cases, so that adding a kind touches no shared test table.
func checkStatusTexts(t *testing.T, tests []statusText) {
	t.Helper()
	objects := map[string]*unstructured.Unstructured{}
	for _, tt := range tests {
		if _, done := objects[tt.kind+"/"]; done {
			continue
		}
		objects[tt.kind+"/"] = nil
		for _, c := range loadCases(t, tt.kind) {
			objects[tt.kind+"/"+c.name] = c.obj
		}
	}
	for _, tt := range tests {
		t.Run(tt.kind+"/"+tt.name+"/"+tt.column, func(t *testing.T) {
			p, ok := kinds.ByKey(tt.kind)
			if !ok {
				t.Fatalf("no built-in profile %s", tt.kind)
			}
			obj, ok := objects[tt.kind+"/"+tt.name]
			if !ok || obj == nil {
				t.Fatalf("no testdata %s/%s", tt.kind, tt.name)
			}
			i := p.ColumnIndex(tt.column)
			if i < 0 {
				t.Fatalf("no column %q", tt.column)
			}
			row := model.BuildRow(p, "test", obj)
			cell := row.Cells[i]
			if text := model.CellText(p.Columns[i], cell, now); text != tt.text || cell.Status != tt.cell || row.Status != tt.row {
				t.Errorf("got %q (cell %q, row %q), want %q (cell %q, row %q)",
					text, cell.Status, row.Status, tt.text, tt.cell, tt.row)
			}
		})
	}
}
