package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The showcase: "show me the Service with ClusterIP 10.96.100.100".
const showcaseIP = "10.96.100.100"

var (
	webRef = model.ObjectRef{Kind: "services", Namespace: "default", Name: "web"}
	podRef = model.ObjectRef{Kind: "pods", Namespace: "default", Name: "web-0"}
)

type findParams struct {
	Kind  string `json:"kind" jsonschema:"kind key, e.g. services"`
	Field string `json:"field" jsonschema:"field to match, e.g. clusterIP"`
	Value string `json:"value" jsonschema:"value to match"`
}

type findResult struct {
	Refs []model.ObjectRef `json:"refs"`
}

type refParams struct {
	Ref model.ObjectRef `json:"ref" jsonschema:"the object"`
}

type scaleParams struct {
	Ref      model.ObjectRef `json:"ref" jsonschema:"the deployment"`
	Replicas int             `json:"replicas" jsonschema:"desired replicas"`
}

// fixture is an engine session with fake commands, including fakes with the ids of the real
// ones (ui.state, resources.find, ui.navigate), and an MCP server for it.
type fixture struct {
	session *engine.Session
	server  *Server
	audit   *syncBuffer
	logs    *syncBuffer
	deleted atomic.Int32
	scaled  atomic.Int32
	guiRan  atomic.Int32
}

// newFixture builds the fixture; confirmer nil means headless. Logs of the session and the
// server go to f.logs at debug level.
func newFixture(t testing.TB, confirmer engine.Confirmer, opts Options) *fixture {
	t.Helper()
	f := &fixture{audit: &syncBuffer{}, logs: &syncBuffer{}}
	logger := slog.New(slog.NewTextHandler(f.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	f.session = engine.New(engine.Options{Confirmer: confirmer, Audit: f.audit, Logger: logger})
	t.Cleanup(f.session.Close)
	f.register(t)
	if opts.TokenFile == "" {
		opts.TokenFile = filepath.Join(t.TempDir(), "Cogpit", "mcp-token")
	}
	if opts.Logger == nil {
		opts.Logger = logger
	}
	srv, err := New(f.session, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := srv.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	f.server = srv
	return f
}

func (f *fixture) register(t testing.TB) {
	t.Helper()
	reg := f.session.Registry()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(engine.Register(reg, engine.Spec[struct{}]{
		ID: "ui.state", Label: "UI State", Danger: engine.DangerRead,
		Description: "Returns what Cogpit's window shows: context, view, filter, selection, drawer.",
		Run:         func(_ context.Context, s *engine.Session, _ struct{}) (any, error) { return s.UIState(), nil },
	}))
	must(engine.Register(reg, engine.Spec[findParams]{
		ID: "resources.find", Label: "Find Resources", Danger: engine.DangerRead,
		Description: "Finds objects of a kind whose field has a value, from the cache.",
		Run: func(_ context.Context, _ *engine.Session, p findParams) (any, error) {
			out := findResult{Refs: []model.ObjectRef{}}
			if p.Kind == "services" && p.Field == "clusterIP" && p.Value == showcaseIP {
				out.Refs = append(out.Refs, webRef)
			}
			return out, nil
		},
	}))
	must(engine.Register(reg, engine.Spec[refParams]{
		ID: "ui.navigate", Label: "Navigate to Object", Danger: engine.DangerUI,
		Description: "Opens the object's view, selects it and opens its drawer.",
		Run: func(_ context.Context, s *engine.Session, p refParams) (any, error) {
			return s.UpdateUI(func(u *engine.UIState) {
				ref := p.Ref
				u.View = ref.Kind
				u.Selected = []model.ObjectRef{ref}
				u.Drawer = &ref
			}), nil
		},
	}))
	must(engine.Register(reg, engine.Spec[struct{}]{
		ID: "probe.list", Label: "List Probe", Danger: engine.DangerRead,
		Description: "Returns a JSON array.",
		Run:         func(context.Context, *engine.Session, struct{}) (any, error) { return []string{"a", "b"}, nil },
	}))
	must(engine.Register(reg, engine.Spec[struct{}]{
		ID: "probe.disabled", Label: "Disabled Probe", Danger: engine.DangerRead,
		Description: "Never enabled.",
		Enabled:     func(*engine.Session, struct{}) error { return errors.New("no cluster connected") },
		Run:         func(context.Context, *engine.Session, struct{}) (any, error) { return nil, nil },
	}))
	must(engine.Register(reg, engine.Spec[struct{}]{
		ID: "probe.guiOnly", Label: "GUI-only Probe", Danger: engine.DangerUI,
		Description: "Runs only on the user's own action in the window.",
		GUIOnly:     errors.New("only the user, in Cogpit's own window, can do this"),
		Run: func(context.Context, *engine.Session, struct{}) (any, error) {
			f.guiRan.Add(1)
			return nil, nil
		},
	}))
	must(engine.Register(reg, engine.Spec[refParams]{
		ID: "pods.delete", Label: "Delete Pod", Danger: engine.DangerDestructive,
		Description: "Deletes the pod.",
		Verbs:       []engine.Verb{{Verb: "delete", Resource: "pods"}},
		Targets:     func(_ *engine.Session, p refParams) ([]model.ObjectRef, error) { return []model.ObjectRef{p.Ref}, nil },
		Run: func(context.Context, *engine.Session, refParams) (any, error) {
			f.deleted.Add(1)
			return map[string]bool{"deleted": true}, nil
		},
	}))
	must(engine.Register(reg, engine.Spec[scaleParams]{
		ID: "deployments.scale", Label: "Scale Deployment", Danger: engine.DangerMutating,
		Description: "Sets the deployment's replicas.",
		Verbs:       []engine.Verb{{Verb: "patch", Group: "apps", Resource: "deployments", Subresource: "scale"}},
		Targets: func(_ *engine.Session, p scaleParams) ([]model.ObjectRef, error) {
			return []model.ObjectRef{p.Ref}, nil
		},
		Run: func(context.Context, *engine.Session, scaleParams) (any, error) {
			f.scaled.Add(1)
			return map[string]bool{"scaled": true}, nil
		},
	}))
}

// connect serves the tools over in-memory transports and returns a connected SDK client.
func (f *fixture) connect(t testing.TB) *sdk.ClientSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	serverT, clientT := sdk.NewInMemoryTransports()
	served := make(chan error, 1)
	go func() { served <- f.server.ServeTransport(ctx, serverT) }()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cs.Close()
		cancel()
		_ = <-served
	})
	return cs
}

// connectHTTP connects an SDK client to the started server through the streamable HTTP
// transport, with an http.Client that adds the bearer token.
func connectHTTP(t testing.TB, url, token, protocolVersion string) *sdk.ClientSession {
	t.Helper()
	tr := &http.Transport{}
	t.Cleanup(tr.CloseIdleConnections)
	transport := &sdk.StreamableClientTransport{
		Endpoint:   url,
		HTTPClient: &http.Client{Transport: bearer{token: token, base: tr}},
		MaxRetries: -1,
	}
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).
		Connect(context.Background(), transport, &sdk.ClientSessionOptions{ProtocolVersion: protocolVersion})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// bearer adds the Authorization header, as Claude Code does with --header.
type bearer struct {
	token string
	base  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(r)
}

func call(t testing.TB, cs *sdk.ClientSession, name string, args any) *sdk.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return res
}

// text returns the result's text content.
func text(res *sdk.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// errorCode returns the error code of a failed call's structured content.
func errorCode(res *sdk.CallToolResult) string {
	m, ok := res.StructuredContent.(map[string]any)
	if !ok {
		return ""
	}
	code, _ := m["error"].(string)
	return code
}

// decode re-encodes a client-side structured result into v.
func decode(t testing.TB, structured any, v any) {
	t.Helper()
	data, err := json.Marshal(structured)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
}

func (f *fixture) auditEntries(t testing.TB) []engine.AuditEntry {
	t.Helper()
	var out []engine.AuditEntry
	sc := bufio.NewScanner(strings.NewReader(f.audit.String()))
	for sc.Scan() {
		var e engine.AuditEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("audit line %q: %v", sc.Text(), err)
		}
		out = append(out, e)
	}
	return out
}

// syncBuffer is a bytes.Buffer safe for concurrent writers.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
