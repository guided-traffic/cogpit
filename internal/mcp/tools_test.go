package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

func TestToolsMirrorRegistry(t *testing.T) {
	f := newFixture(t, nil, Options{})
	cs := f.connect(t)

	tools := map[string]*sdk.Tool{}
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		tools[tool.Name] = tool
	}
	cmds := f.session.Registry().Commands()
	if len(tools) != len(cmds) {
		t.Fatalf("%d tools for %d commands", len(tools), len(cmds))
	}
	for id, name := range map[string]string{
		"ui.state": "ui_state", "resources.find": "resources_find", "ui.navigate": "ui_navigate",
		"pods.delete": "pods_delete", "deployments.scale": "deployments_scale",
	} {
		if c, _ := f.session.Registry().Get(id); c == nil || tools[name] == nil {
			t.Errorf("command %s: no tool %s", id, name)
		}
	}

	for _, cmd := range cmds {
		tool := tools[cmd.ToolName()]
		if tool == nil {
			t.Errorf("command %s has no tool", cmd.ID)
			continue
		}
		if tool.Title != cmd.Label {
			t.Errorf("%s: title %q, want %q", tool.Name, tool.Title, cmd.Label)
		}
		if !strings.HasPrefix(tool.Description, cmd.Description) || !strings.Contains(tool.Description, "Danger level: "+string(cmd.Danger)+".") {
			t.Errorf("%s: description %q lacks the command's description or danger level", tool.Name, tool.Description)
		}
		asks := (cmd.Danger == engine.DangerMutating || cmd.Danger == engine.DangerDestructive) && !cmd.GUIOnly
		if asks != strings.Contains(tool.Description, "confirm it in Cogpit's window") {
			t.Errorf("%s: description %q; confirmation note expected: %v", tool.Name, tool.Description, asks)
		}
		if cmd.GUIOnly != strings.Contains(tool.Description, "GUI only: ") {
			t.Errorf("%s: description %q; GUI-only note expected: %v", tool.Name, tool.Description, cmd.GUIOnly)
		}

		a := tool.Annotations
		if a == nil {
			t.Errorf("%s: no annotations", tool.Name)
			continue
		}
		harmless := cmd.Danger == engine.DangerRead || cmd.Danger == engine.DangerUI
		if a.ReadOnlyHint != harmless || a.IdempotentHint != harmless {
			t.Errorf("%s (%s): readOnly %v idempotent %v, want %v", tool.Name, cmd.Danger, a.ReadOnlyHint, a.IdempotentHint, harmless)
		}
		if a.DestructiveHint == nil || *a.DestructiveHint != (cmd.Danger == engine.DangerDestructive) {
			t.Errorf("%s (%s): destructiveHint %v", tool.Name, cmd.Danger, a.DestructiveHint)
		}
		if a.OpenWorldHint == nil || *a.OpenWorldHint {
			t.Errorf("%s: openWorldHint %v, want explicit false", tool.Name, a.OpenWorldHint)
		}
		if a.Title != cmd.Label {
			t.Errorf("%s: annotations title %q", tool.Name, a.Title)
		}

		var want any
		data, err := json.Marshal(cmd.Schema)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(tool.InputSchema, want) {
			t.Errorf("%s: input schema %v, want %v", tool.Name, tool.InputSchema, want)
		}
	}

	instr := cs.InitializeResult().Instructions
	for _, hint := range []string{"ui_state", "resources_find", "ui_navigate"} {
		if !strings.Contains(instr, hint) {
			t.Errorf("instructions do not mention %s: %q", hint, instr)
		}
	}
}

func TestDestructiveAsksTheUser(t *testing.T) {
	var mu sync.Mutex
	var asked []engine.ConfirmRequest
	f := newFixture(t, engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, req)
		return true, nil
	}), Options{})
	cs := f.connect(t)

	res := call(t, cs, "pods_delete", map[string]any{"ref": podRef})
	if res.IsError || f.deleted.Load() != 1 {
		t.Fatalf("pods_delete: %s (deleted %d)", text(res), f.deleted.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(asked) != 1 {
		t.Fatalf("asked %d times", len(asked))
	}
	req := asked[0]
	if req.Origin != engine.OriginExternalAgent || req.Command != "pods.delete" || req.Danger != engine.DangerDestructive ||
		!slices.Equal(req.Objects, []model.ObjectRef{podRef}) || !slices.Equal(req.Namespaces, []string{"default"}) {
		t.Errorf("confirm request %+v", req)
	}
	// A write gets a "requested" line before it runs and a "done" line with the outcome.
	entries := f.auditEntries(t)
	if len(entries) != 2 || entries[0].Phase != "requested" || entries[1].Phase != "done" ||
		entries[1].Origin != engine.OriginExternalAgent || entries[1].Command != "pods.delete" || !entries[1].OK {
		t.Errorf("audit %+v", entries)
	}
}

func TestRefusedCallsAreToolErrors(t *testing.T) {
	declined := engine.ConfirmFunc(func(context.Context, engine.ConfirmRequest) (bool, error) { return false, nil })
	cases := []struct {
		name      string
		confirmer engine.Confirmer
		tool      string
		args      any
		code      string
		says      string
	}{
		{"declined destructive", declined, "pods_delete", map[string]any{"ref": podRef}, codeDeclined, "The user declined"},
		{"declined mutating", declined, "deployments_scale", map[string]any{"ref": podRef, "replicas": 0}, codeDeclined, "The user declined"},
		{"headless destructive", nil, "pods_delete", map[string]any{"ref": podRef}, codeConfirmationUnavailable, "no window"},
		{"headless mutating", nil, "deployments_scale", map[string]any{"ref": podRef, "replicas": 0}, codeConfirmationUnavailable, "no window"},
		{"disabled", nil, "probe_disabled", nil, codeDisabled, "not available right now: no cluster connected"},
		{"gui only", engine.ConfirmFunc(func(context.Context, engine.ConfirmRequest) (bool, error) { return true, nil }),
			"probe_guiOnly", nil, codeGUIOnly, "runs only on the user's own action in Cogpit's window"},
		{"wrong type", nil, "resources_find", map[string]any{"kind": 5, "field": "clusterIP", "value": showcaseIP}, codeInvalidParams, "input schema"},
		{"missing field", nil, "resources_find", map[string]any{"kind": "services"}, codeInvalidParams, "input schema"},
		{"unknown field", nil, "resources_find", map[string]any{"kind": "services", "field": "clusterIP", "value": "x", "extra": true}, codeInvalidParams, "input schema"},
		{"missing ref", nil, "ui_navigate", map[string]any{}, codeInvalidParams, `"ref"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, c.confirmer, Options{})
			cs := f.connect(t)
			res := call(t, cs, c.tool, c.args)
			if !res.IsError || errorCode(res) != c.code || !strings.Contains(text(res), c.says) {
				t.Fatalf("isError %v, code %q, text %q; want code %q mentioning %q", res.IsError, errorCode(res), text(res), c.code, c.says)
			}
			if f.deleted.Load() != 0 || f.scaled.Load() != 0 || f.guiRan.Load() != 0 {
				t.Error("a refused command ran")
			}
			entries := f.auditEntries(t)
			if len(entries) != 1 || entries[0].OK || entries[0].Origin != engine.OriginExternalAgent || entries[0].Error == "" {
				t.Errorf("audit %+v", entries)
			}
		})
	}
}

func TestResultEncoding(t *testing.T) {
	f := newFixture(t, nil, Options{})
	cs := f.connect(t)
	res := call(t, cs, "probe_list", nil)
	if res.IsError || text(res) != `["a","b"]` || res.StructuredContent != nil {
		t.Errorf("array result: isError %v, text %q, structured %v", res.IsError, text(res), res.StructuredContent)
	}
	res = call(t, cs, "ui_state", nil)
	if _, ok := res.StructuredContent.(map[string]any); res.IsError || !ok || !json.Valid([]byte(text(res))) {
		t.Errorf("object result: isError %v, text %q, structured %T", res.IsError, text(res), res.StructuredContent)
	}
}

// blockingConfirmer waits for its context and reports what it saw: the stand-in for a dialog the
// user has not answered yet.
type blockingConfirmer struct {
	entered chan struct{}
	ended   chan error
}

func newBlockingConfirmer() *blockingConfirmer {
	return &blockingConfirmer{entered: make(chan struct{}, 1), ended: make(chan error, 1)}
}

func (c *blockingConfirmer) Confirm(ctx context.Context, _ engine.ConfirmRequest) (bool, error) {
	c.entered <- struct{}{}
	<-ctx.Done()
	c.ended <- ctx.Err()
	return false, ctx.Err()
}

func (c *blockingConfirmer) waitEntered(t testing.TB) {
	t.Helper()
	select {
	case <-c.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the confirmation was never asked")
	}
}

func (c *blockingConfirmer) waitEnded(t testing.TB) {
	t.Helper()
	select {
	case _ = <-c.ended:
	case <-time.After(10 * time.Second):
		t.Fatal("the pending confirmation did not see the call end")
	}
}

func TestCancelEndsPendingConfirmation(t *testing.T) {
	confirm := newBlockingConfirmer()
	f := newFixture(t, confirm, Options{})
	cs := f.connect(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		_, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "pods_delete", Arguments: map[string]any{"ref": podRef}})
		errc <- err
	}()
	confirm.waitEntered(t)
	cancel()
	confirm.waitEnded(t)
	if err := <-errc; err == nil {
		t.Error("cancelled call returned no error")
	}
	if f.deleted.Load() != 0 {
		t.Error("the command ran after its call was cancelled")
	}
}

func TestCloseEndsPendingConfirmation(t *testing.T) {
	confirm := newBlockingConfirmer()
	f := newFixture(t, confirm, Options{})
	cs := f.connect(t)

	resc := make(chan *sdk.CallToolResult, 1)
	go func() {
		res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "pods_delete", Arguments: map[string]any{"ref": podRef}})
		if err != nil {
			t.Errorf("call: %v", err)
		}
		resc <- res
	}()
	confirm.waitEntered(t)
	if err := f.server.Close(); err != nil {
		t.Fatal(err)
	}
	confirm.waitEnded(t)
	if res := <-resc; res == nil || !res.IsError || errorCode(res) != codeCancelled {
		t.Errorf("result after Close: %+v", res)
	}
	if f.deleted.Load() != 0 {
		t.Error("the command ran after the server closed")
	}
}

func TestInputSchemaNormalisesRoot(t *testing.T) {
	type params struct {
		Name string `json:"name"`
	}
	f := newFixture(t, nil, Options{})
	if err := engine.Register(f.session.Registry(), engine.Spec[*params]{
		ID: "probe.pointer", Label: "Pointer Probe", Danger: engine.DangerRead,
		Run: func(context.Context, *engine.Session, *params) (any, error) { return map[string]int{"n": 1}, nil },
	}); err != nil {
		t.Fatal(err)
	}
	if err := engine.Register(f.session.Registry(), engine.Spec[string]{
		ID: "probe.scalar", Label: "Scalar Probe", Danger: engine.DangerRead,
		Run: func(context.Context, *engine.Session, string) (any, error) { return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}
	cs := f.connect(t)
	var names []string
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, tool.Name)
		if tool.Name == "probe_pointer" {
			if m, _ := tool.InputSchema.(map[string]any); m["type"] != "object" {
				t.Errorf("pointer params schema %v", tool.InputSchema)
			}
		}
	}
	if !slices.Contains(names, "probe_pointer") || slices.Contains(names, "probe_scalar") {
		t.Errorf("tools %v: want probe_pointer, and no probe_scalar (its params are not an object)", names)
	}
	if !strings.Contains(f.logs.String(), "probe.scalar") {
		t.Error("the left-out command was not logged")
	}
}
