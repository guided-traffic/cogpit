package commands_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
)

// linkConnector offers contexts as kubeconfig sources do (D-28), with their credential plugins'
// approval states, and records every connect.
type linkConnector struct {
	*fakeConnector
	mu       sync.Mutex
	connects []string
}

func (c *linkConnector) Contexts() ([]engine.ContextInfo, error) {
	return []engine.ContextInfo{
		{Name: "test", Source: "config", KubeContext: "test", Current: true},
		{Name: "dev [dev.yaml]", Source: "dev.yaml", KubeContext: "dev"},
		{Name: "eks [aws.yaml]", Source: "aws.yaml", KubeContext: "eks", Exec: engine.ExecRequired, ExecKind: "program"},
		{Name: "gke [gke.yaml]", Source: "gke.yaml", KubeContext: "gke", Exec: engine.ExecApproved, ExecKind: "program"},
		{Name: "tok [tok.yaml]", Source: "tok.yaml", KubeContext: "tok", Exec: engine.ExecChanged, ExecKind: "tokenFile"},
		{Name: "lab [lab.yaml]", Source: "lab.yaml", KubeContext: "lab", Exec: engine.ExecRequired, ExecKind: "insecure", Insecure: true},
		{Name: "dup [a.yaml]", Source: "a.yaml", KubeContext: "dup"},
		{Name: "dup [b.yaml]", Source: "b.yaml", KubeContext: "dup"},
		{Name: "broken [down.yaml]", Source: "down.yaml", KubeContext: "broken"},
		{Name: "merged"}, // a connector without sources: its plugins would run unasked
		{Name: engine.DemoContext, Demo: true},
	}, nil
}

func (c *linkConnector) Connect(ctx context.Context, name string) (*engine.Backend, error) {
	c.mu.Lock()
	c.connects = append(c.connects, name)
	c.mu.Unlock()
	if name == "broken [down.yaml]" {
		name = "broken"
	}
	return c.fakeConnector.Connect(ctx, name)
}

func (c *linkConnector) connected() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.connects)
}

// linkHarness is a session connected to "test" over linkConnector, with an audit log, the
// preferences page and the toasts it shows recorded.
type linkHarness struct {
	*harness
	conn   *linkConnector
	audit  *syncBuffer
	asked  atomic.Int32
	no     atomic.Bool // the user declines
	mu     sync.Mutex
	toasts []engine.Event
}

func newLinkHarness(t *testing.T) *linkHarness {
	t.Helper()
	src := seed()
	h := &linkHarness{audit: &syncBuffer{}}
	writer := &fakeWriter{}
	h.conn = &linkConnector{fakeConnector: &fakeConnector{src: src, writer: writer}}
	s := engine.New(engine.Options{Connector: h.conn, Audit: h.audit, SnapshotInterval: 10 * time.Millisecond,
		Preferences: engine.NewPreferencesStore(""),
		Confirmer: engine.ConfirmFunc(func(context.Context, engine.ConfirmRequest) (bool, error) {
			h.asked.Add(1)
			return !h.no.Load(), nil
		})})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	cancel := s.Subscribe(func(ev engine.Event) {
		if ev.Type == engine.EventToast {
			h.mu.Lock()
			h.toasts = append(h.toasts, ev)
			h.mu.Unlock()
		}
	})
	t.Cleanup(cancel)
	if err := s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	h.conn.mu.Lock()
	h.conn.connects = nil // the harness's own connect
	h.conn.mu.Unlock()
	h.harness = &harness{s: s, src: src, writer: writer}
	return h
}

func (h *linkHarness) lastToast(t *testing.T) engine.Event {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.toasts) == 0 {
		t.Fatal("no toast")
	}
	return h.toasts[len(h.toasts)-1]
}

// lastLinkAudit is the audit log's last line of a link.
func (h *linkHarness) lastLinkAudit(t *testing.T) engine.AuditEntry {
	t.Helper()
	var last *engine.AuditEntry
	for _, e := range h.audit.Lines() {
		if e.Command == engine.DeepLinkAuditCommand {
			last = &e
		}
	}
	if last == nil {
		t.Fatal("no audit line of a link")
	}
	return *last
}

// A link opens an object's drawer in the connected cluster: no connect, the view, the selection
// and the drawer as ui_navigate leaves them; a toast says what it opened and the audit log has the
// link with origin deeplink.
func TestDeepLinkOpensAnObjectsDrawer(t *testing.T) {
	h := newLinkHarness(t)
	res, err := h.s.OpenDeepLink(context.Background(), "cogpit://open?context=test&view=pods&namespace=shop&name=web-0")
	if err != nil {
		t.Fatal(err)
	}
	want := model.ObjectRef{Context: "test", Kind: "pods", Namespace: "shop", Name: "web-0"}
	u := h.s.UIState()
	if u.View != "pods" || u.Drawer == nil || *u.Drawer != want || len(u.Selected) != 0 {
		t.Errorf("view %q, drawer %v, selected %v", u.View, u.Drawer, u.Selected)
	}
	if res.Ref == nil || *res.Ref != want || !res.Found || res.Connected || res.Context != "test" {
		t.Errorf("result %+v", res)
	}
	if got := h.conn.connected(); len(got) != 0 {
		t.Errorf("connected %v; the context was connected already", got)
	}
	toast := h.lastToast(t)
	if toast.Level != engine.LevelInfo || toast.Message != res.Message || !strings.Contains(toast.Message, "shop/web-0") ||
		!strings.Contains(toast.Message, "on test") {
		t.Errorf("toast %+v, result's message %q", toast, res.Message)
	}
	e := h.lastLinkAudit(t)
	var params engine.DeepLink
	if err := json.Unmarshal(e.Params, &params); err != nil {
		t.Fatal(err)
	}
	if e.Origin != engine.OriginDeepLink || !e.OK || e.Context != "test" ||
		params != (engine.DeepLink{Context: "test", View: "pods", Namespace: "shop", Name: "web-0"}) {
		t.Errorf("audit %+v, params %+v", e, params)
	}
	if h.asked.Load() != 0 || len(h.writer.calls) != 0 {
		t.Errorf("asked %d, writes %v", h.asked.Load(), h.writer.calls)
	}
}

// A link to another context of the sources connects it (a bare kubeconfig context name one file
// alone defines names it), then opens the view; a namespace without a name sets the filter; a page
// opens like a view; a context the link names only connects; a link without a context applies to
// the connected cluster.
func TestDeepLinkConnectsAndNavigates(t *testing.T) {
	h := newLinkHarness(t)
	res, err := h.s.OpenDeepLink(context.Background(), "cogpit://open?context=dev&view=services&namespace=infra")
	if err != nil {
		t.Fatal(err)
	}
	u := h.s.UIState()
	if got := h.conn.connected(); !slices.Equal(got, []string{"dev [dev.yaml]"}) {
		t.Errorf("connected %v", got)
	}
	if u.Context != "dev [dev.yaml]" || u.Connection.State != engine.Connected || u.View != "services" ||
		!slices.Equal(u.Namespaces, []string{"infra"}) || !res.Connected || res.View != "services" {
		t.Errorf("state %q %s view %q namespaces %v; result %+v", u.Context, u.Connection.State, u.View, u.Namespaces, res)
	}

	if _, err := h.s.OpenDeepLink(context.Background(), "cogpit://open/?view=workloads/overview"); err != nil {
		t.Fatal(err)
	}
	if u := h.s.UIState(); u.View != "workloads/overview" || u.Context != "dev [dev.yaml]" {
		t.Errorf("the page: view %q on %q", u.View, u.Context)
	}
	if !strings.Contains(h.lastToast(t).Message, "Overview (workloads/overview)") {
		t.Errorf("toast %q names no page title", h.lastToast(t).Message)
	}

	// An approved credential plugin runs: the approval is the user's, given before.
	if _, err := h.s.OpenDeepLink(context.Background(), "COGPIT://OPEN?context=gke+%5Bgke.yaml%5D"); err != nil {
		t.Fatal(err)
	}
	if got := h.conn.connected(); !slices.Equal(got, []string{"dev [dev.yaml]", "gke [gke.yaml]"}) {
		t.Errorf("connected %v", got)
	}
	// Each connect asked once (A7); the link within the connected cluster did not.
	if h.asked.Load() != 2 || len(h.writer.calls) != 0 {
		t.Errorf("asked %d, want 2; writes %v", h.asked.Load(), h.writer.calls)
	}
}

// A link that would connect another cluster asks the user once, naming the context and what it
// opens (A7, owner 2026-10-14 evening, H-89); declined, nothing is connected and nothing moves, and
// the toast and the audit log say the user declined. Navigation within the connected cluster never
// asks.
func TestDeepLinkAsksBeforeItConnects(t *testing.T) {
	h := newLinkHarness(t)
	var req engine.ConfirmRequest
	h.s.SetConfirmer(engine.ConfirmFunc(func(_ context.Context, r engine.ConfirmRequest) (bool, error) {
		h.asked.Add(1)
		req = r
		return !h.no.Load(), nil
	}))
	if _, err := h.s.OpenDeepLink(context.Background(), "cogpit://open?view=services"); err != nil || h.asked.Load() != 0 {
		t.Fatalf("a link within the connected cluster: %v, asked %d", err, h.asked.Load())
	}
	h.no.Store(true)
	_, err := h.s.OpenDeepLink(context.Background(), "cogpit://open?context=dev&view=pods&namespace=shop&name=web-0")
	if !errors.Is(err, engine.ErrDeepLinkDeclined) || h.asked.Load() != 1 || len(h.conn.connected()) != 0 {
		t.Fatalf("declined: %v, asked %d, connected %v", err, h.asked.Load(), h.conn.connected())
	}
	if req.Command != engine.DeepLinkConnectCommand || req.Origin != engine.OriginDeepLink || req.Context != "dev [dev.yaml]" ||
		!slices.Equal(req.Effects, []engine.Effect{{Name: "Opens", Value: "pods → web-0 in namespace shop", Tone: engine.EffectNames},
			{Name: "Shown now", Value: "test, its tabs stay"}}) {
		t.Errorf("the question %+v", req)
	}
	if u := h.s.UIState(); u.Context != "test" || u.View != "services" {
		t.Errorf("a declined link moved the window: %q %q", u.Context, u.View)
	}
	if e := h.lastLinkAudit(t); e.OK || !strings.Contains(e.Error, "declined") {
		t.Errorf("audit %+v", e)
	}
	h.no.Store(false)
	if _, err := h.s.OpenDeepLink(context.Background(), "cogpit://open?context=dev"); err != nil || h.asked.Load() != 2 ||
		!slices.Equal(h.conn.connected(), []string{"dev [dev.yaml]"}) {
		t.Errorf("accepted: %v, asked %d, connected %v", err, h.asked.Load(), h.conn.connected())
	}
	// Without a window to ask in, a link that would connect is refused.
	h.s.SetConfirmer(nil)
	if _, err := h.s.OpenDeepLink(context.Background(), "cogpit://open?context=test"); !errors.Is(err, engine.ErrConfirmationUnavailable) {
		t.Errorf("headless: %v", err)
	}
}

// A link that opens while the Preferences page is open closes it, or the page would hide what it
// opened.
func TestDeepLinkClosesTheSettingsPage(t *testing.T) {
	h := newLinkHarness(t)
	h.must(t, engine.OriginGUI, "preferences.open", map[string]any{}, nil)
	if _, err := h.s.OpenDeepLink(context.Background(), "cogpit://open?view=deployments"); err != nil {
		t.Fatal(err)
	}
	if u := h.s.UIState(); u.Settings != nil || u.View != "deployments" {
		t.Errorf("settings %+v, view %q", u.Settings, u.View)
	}
}

// What a link is refused: a context whose login waits for the user's approval (a link never asks
// for it, and never connects it), one two files define, one that is no source's, an unknown one, no
// context without a connection, a connect that fails, a view the cluster does not serve, and every
// malformed link. Each says why in a warning toast and an audit line, writes nothing and asks
// nobody.
func TestDeepLinkRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, link string
		disconnect bool
		want       error
		says       string
		connects   []string
	}{
		{name: "plugin not approved", link: "cogpit://open?context=eks", want: engine.ErrDeepLinkNeedsApproval, says: "navigator's clusters (⌘P)"},
		{name: "token file changed", link: "cogpit://open?context=tok+%5Btok.yaml%5D&view=pods", want: engine.ErrDeepLinkNeedsApproval, says: "token file"},
		// A28: a context that skips the certificate check is asked once in the window, never by a link.
		{name: "unchecked certificate not allowed", link: "cogpit://open?context=lab", want: engine.ErrDeepLinkNeedsApproval, says: "connection without the certificate check"},
		{name: "two files", link: "cogpit://open?context=dup", want: engine.ErrDeepLinkContext, says: "2 kubeconfig files"},
		{name: "not a source", link: "cogpit://open?context=merged", want: engine.ErrDeepLinkContext, says: "not read from a kubeconfig source"},
		{name: "unknown", link: "cogpit://open?context=prod", want: engine.ErrDeepLinkContext, says: "prod"},
		{name: "not connected", link: "cogpit://open?view=pods", disconnect: true, says: "no cluster is connected"},
		{name: "connect fails", link: "cogpit://open?context=broken", says: "connection refused", connects: []string{"broken [down.yaml]"}},
		{name: "unknown view", link: "cogpit://open?view=frobnicators", says: "frobnicators"},
		{name: "a file", link: "cogpit://open?view=pods&file=%2Fetc%2Fpasswd", want: engine.ErrDeepLinkInvalid, says: "file"},
		{name: "another action", link: "cogpit://delete?context=test&view=pods&name=web-0", want: engine.ErrDeepLinkInvalid, says: "delete"},
		{name: "another scheme", link: "https://open?context=test", want: engine.ErrDeepLinkInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newLinkHarness(t)
			if tc.disconnect {
				h.must(t, engine.OriginGUI, "cluster.disconnect", map[string]any{}, nil)
			}
			before := h.s.UIState()
			_, err := h.s.OpenDeepLink(context.Background(), tc.link)
			if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) || !strings.Contains(err.Error(), tc.says) {
				t.Fatalf("err %v, want %v saying %q", err, tc.want, tc.says)
			}
			if got := h.conn.connected(); !slices.Equal(got, tc.connects) {
				t.Errorf("connected %v, want %v", got, tc.connects)
			}
			if tc.connects == nil {
				if u := h.s.UIState(); u.Context != before.Context || u.View != before.View || u.Connection != before.Connection {
					t.Errorf("the state moved: %q %q %+v", u.Context, u.View, u.Connection)
				}
			}
			toast := h.lastToast(t)
			t.Log(toast.Message)
			if toast.Level != engine.LevelWarning || !strings.HasPrefix(toast.Message, "Cogpit did not follow a link: ") {
				t.Errorf("toast %+v", toast)
			}
			if e := h.lastLinkAudit(t); e.OK || e.Origin != engine.OriginDeepLink || e.Error == "" {
				t.Errorf("audit %+v", e)
			}
			wantAsked := int32(0)
			if tc.connects != nil { // the user was asked before the connect that failed (A7)
				wantAsked = 1
			}
			if h.asked.Load() != wantAsked || len(h.writer.calls) != 0 {
				t.Errorf("asked %d, want %d; writes %v", h.asked.Load(), wantAsked, h.writer.calls)
			}
		})
	}
}

// A link that does not parse is quoted in the audit log only as escaped, bounded text.
func TestDeepLinkAuditQuotesABadLinkEscaped(t *testing.T) {
	h := newLinkHarness(t)
	link := "cogpit://open?view=pods&name=" + strings.Repeat("a", 300) + "%E2%80%AE"
	if _, err := h.s.OpenDeepLink(context.Background(), link); !errors.Is(err, engine.ErrDeepLinkInvalid) {
		t.Fatalf("err %v", err)
	}
	e := h.lastLinkAudit(t)
	var params map[string]string
	if err := json.Unmarshal(e.Params, &params); err != nil {
		t.Fatal(err)
	}
	if l := params["link"]; len(l) > 260 || !strings.HasPrefix(l, "cogpit://open?view=pods") || !strings.HasSuffix(l, "…") {
		t.Errorf("audit quotes %q", l)
	}
}

// The commands a link runs only navigate: each is registered, a UI or read command that never
// escalates, is not GUI-only and declares no verb but the cache's reads. Every other command of
// the registry is refused to origin deeplink before anything runs or anyone is asked, so no write
// is reachable from a link, whatever OpenDeepLink dispatches.
func TestDeepLinkCommandsOnlyNavigate(t *testing.T) {
	h := newLinkHarness(t)
	reg := h.s.Registry()
	for _, id := range engine.DeepLinkCommands {
		c, ok := reg.Get(id)
		if !ok {
			t.Errorf("%s is not registered", id)
			continue
		}
		if (c.Danger != engine.DangerUI && c.Danger != engine.DangerRead) || c.Escalates || c.GUIOnly {
			t.Errorf("%s: danger %s, escalates %v, GUI-only %v", id, c.Danger, c.Escalates, c.GUIOnly)
		}
		for _, v := range c.Verbs {
			if !slices.Contains([]string{"get", "list", "watch"}, v.Verb) {
				t.Errorf("%s declares the verb %s", id, v.Verb)
			}
		}
	}
	refused := 0
	for _, c := range reg.Commands() {
		if slices.Contains(engine.DeepLinkCommands, c.ID) {
			continue
		}
		_, err := h.s.Dispatch(context.Background(), engine.Invocation{ID: c.ID, Params: guiOnlyParams(c), Origin: engine.OriginDeepLink})
		if !errors.Is(err, engine.ErrDeepLinkRefused) {
			t.Errorf("%s with origin deeplink: %v, want refused", c.ID, err)
		}
		refused++
	}
	if refused < 100 || h.asked.Load() != 0 || len(h.writer.calls) != 0 {
		t.Errorf("refused %d commands; asked %d, writes %v", refused, h.asked.Load(), h.writer.calls)
	}
}
