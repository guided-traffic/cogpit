package fyneui

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// fakeMCP is an MCPControl that records its calls and hands out a new command on every start.
// Its tokens are fixed fakes ("token-<n>", or fakeToken's when set), never a real one.
type fakeMCP struct {
	mu       sync.Mutex
	calls    []string
	runs     int
	startErr error
	stdio    string // the stdio registration an app bundle with cogpit-cli offers; "" for HTTP
	// token, when set, is the token of every run, and Regenerate hands out regenToken; taken is the
	// port a start reports taken.
	token, regenToken string
	taken             int
	regenErr          error
	on                bool
}

func (m *fakeMCP) endpoint() MCPEndpoint {
	url := fmt.Sprintf("http://127.0.0.1:%d/mcp", 40000+m.runs)
	token := m.token
	if token == "" {
		token = fmt.Sprintf("token-%d", m.runs)
	}
	ep := MCPEndpoint{URL: url, Token: token,
		HTTPCommand: fmt.Sprintf(`claude mcp add cogpit %s --header "Authorization: Bearer %s"`, url, token)}
	ep.Command = ep.HTTPCommand
	if m.stdio != "" {
		ep.Command, ep.Stdio = m.stdio, true
	}
	return ep
}

func (m *fakeMCP) Start(fromUser bool) (MCPEndpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, fmt.Sprintf("start(user=%v)", fromUser))
	if m.startErr != nil {
		return MCPEndpoint{}, m.startErr
	}
	m.runs++
	m.on = true
	ep := m.endpoint()
	ep.PortTaken = m.taken
	return ep, nil
}

func (m *fakeMCP) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "stop")
	m.on = false
	return nil
}

func (m *fakeMCP) Regenerate() (MCPEndpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "regenerate")
	if m.regenErr != nil {
		return MCPEndpoint{}, m.regenErr
	}
	if m.regenToken != "" {
		m.token = m.regenToken
	}
	if !m.on {
		return MCPEndpoint{}, nil
	}
	return m.endpoint(), nil
}

func (m *fakeMCP) log() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return strings.Join(m.calls, ",")
}

// mcpShell is a window whose user switched the server on in an earlier run (the preference
// mcp.enabled stored true): the server is opt-in, so a window with no stored choice starts it off
// (TestMCPIsOptIn).
func mcpShell(t *testing.T, ctl *fakeMCP) (*shell, fyne.Window, *mainQueue) {
	t.Helper()
	sh, w, q := newTestShell(t, connectedFake(t, 20), Options{MCP: ctl})
	sh.app.Preferences().SetBool(prefMCPEnabled, true)
	sh.startMCP()
	settle(w)
	return sh, w, q
}

// TestMCPItemSwitchesTheServer: clicking the status bar item stops the server and starts it again;
// the item, its tooltip and the Help menu follow, and the command copied after a restart is the
// one the control gave for it.
func TestMCPItemSwitchesTheServer(t *testing.T) {
	ctl := &fakeMCP{}
	sh, w, q := mcpShell(t, ctl)
	item := sh.status.mcp
	if item == nil {
		t.Fatal("no MCP item with an MCP control")
	}
	if ctl.log() != "start(user=false)" || !sh.mcp.on || !item.on {
		t.Fatalf("after launch: calls %s, on %v", ctl.log(), sh.mcp.on)
	}
	if !strings.Contains(item.tip, "External MCP server") || !strings.Contains(item.tip, "http://127.0.0.1:40001/mcp") ||
		!strings.Contains(item.tip, "click to stop") {
		t.Errorf("tooltip while on: %q", item.tip)
	}

	test.Tap(item)
	q.until(t, "the server off", func() bool { return !sh.mcp.on && !sh.mcp.busy })
	if ctl.log() != "start(user=false),stop" {
		t.Errorf("calls %s, want a stop", ctl.log())
	}
	if !strings.Contains(item.tip, "off until you switch it on — click to start") || item.on {
		t.Errorf("after stop: on %v, tooltip %q", item.on, item.tip)
	}
	sh.mcpMenu = fyne.NewMenuItem("x", nil) // the menu item exists only after mainMenu
	sh.refreshMCP()
	if !sh.mcpMenu.Disabled {
		t.Error("Help › Copy MCP command is enabled while the server is off")
	}
	sh.app.Clipboard().SetContent("")
	sh.copyMCPCommand()
	settle(w)
	if got := sh.app.Clipboard().Content(); got != "" {
		t.Errorf("a command was copied while the server is off: %q", got)
	}
	if sh.app.Preferences().BoolWithFallback(prefMCPEnabled, true) {
		t.Error("the off state was not stored")
	}

	test.Tap(item)
	q.until(t, "the server on", func() bool { return sh.mcp.on && !sh.mcp.busy })
	if ctl.log() != "start(user=false),stop,start(user=true)" {
		t.Errorf("calls %s, want a start by the user", ctl.log())
	}
	if !strings.Contains(item.tip, "40002") {
		t.Errorf("after restart: tooltip %q", item.tip)
	}
	sh.copyMCPCommand()
	if got := sh.app.Clipboard().Content(); !strings.Contains(got, "token-2") || strings.Contains(got, "token-1") {
		t.Errorf("copied %q, want the new command", got)
	}
	if !sh.app.Preferences().BoolWithFallback(prefMCPEnabled, true) {
		t.Error("the on state was not stored")
	}
}

// TestMCPCopyNamesWhatTheCommandIs: Help › Copy MCP command copies the command and opens the MCP
// details, which say that the HTTP registration keeps working across restarts until the token is
// regenerated, and that the bundled cogpit-cli's stdio registration is run once and follows by
// itself (F6).
func TestMCPCopyNamesWhatTheCommandIs(t *testing.T) {
	for _, tc := range []struct {
		name, stdio, says string
	}{
		{"http", "", "keeps working across restarts of Cogpit; run it again after you regenerate the token"},
		{"stdio", "claude mcp add --scope user cogpit -- /Applications/Cogpit.app/Contents/MacOS/cogpit-cli mcp", "starts the cogpit-cli inside Cogpit.app"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sh, w, _ := mcpShell(t, &fakeMCP{stdio: tc.stdio})
			sh.copyMCPCommand()
			settle(w)
			if got := sh.app.Clipboard().Content(); got != sh.mcp.command || (tc.stdio != "" && got != tc.stdio) {
				t.Errorf("copied %q", got)
			}
			texts := strings.Join(overlayTexts(w), "\n")
			if !strings.Contains(texts, tc.says) {
				t.Errorf("dialog %q, want %q", texts, tc.says)
			}
		})
	}
}

// TestMCPOffIsRemembered: a server the user switched off stays off at the next launch.
func TestMCPOffIsRemembered(t *testing.T) {
	ctl := &fakeMCP{}
	sh, _, _ := newTestShell(t, connectedFake(t, 20), Options{MCP: ctl})
	sh.app.Preferences().SetBool(prefMCPEnabled, false)
	sh.startMCP()
	if ctl.log() != "" || sh.mcp.on || sh.status.mcp.on {
		t.Errorf("a stored off was ignored: calls %q, on %v", ctl.log(), sh.mcp.on)
	}
}

// TestMCPIsOptIn (D-49, the owner's decision of 2026-10-21): a launch without any stored choice, a
// new installation, does not start the server; the item shows it off and says why, nothing is
// stored, and the user's first click starts it and is remembered.
func TestMCPIsOptIn(t *testing.T) {
	ctl := &fakeMCP{}
	sh, _, q := newTestShell(t, connectedFake(t, 20), Options{MCP: ctl})
	sh.startMCP()
	if ctl.log() != "" || sh.mcp.on || sh.status.mcp.on {
		t.Fatalf("a new installation started the server: calls %q, on %v", ctl.log(), sh.mcp.on)
	}
	if tip := sh.status.mcp.tip; !strings.Contains(tip, "off until you switch it on") || strings.Contains(tip, "last start failed") {
		t.Errorf("the tooltip of an opt-in server that was never switched on: %q", tip)
	}
	if _, chosen := sh.legacyMCP(); chosen {
		t.Error("launching a new installation stored a choice")
	}
	sh.openMCPDetails()
	if texts := detailsTexts(t, sh); !strings.Contains(texts, "starts only when you switch it on") || !strings.Contains(texts, "Switch on") {
		t.Errorf("the details of an opt-in server that is off:\n%s", texts)
	}
	sh.closeMCPDetails()

	test.Tap(sh.status.mcp)
	q.until(t, "the server on", func() bool { return sh.mcp.on && !sh.mcp.busy })
	if ctl.log() != "start(user=true)" {
		t.Errorf("calls %s, want one start by the user", ctl.log())
	}
	if on, chosen := sh.legacyMCP(); !on || !chosen {
		t.Errorf("the user's switch-on was not stored: on %v, chosen %v", on, chosen)
	}
}

// TestMCPLegacyPreference: the preference every installation before D-49 kept. Only a stored
// choice counts: true stays on, false stays off, and an absent one is off (no store, as in a window
// whose control keeps no switch file).
func TestMCPLegacyPreference(t *testing.T) {
	for _, tc := range []struct {
		name   string
		pref   *bool
		starts bool
	}{
		{"stored true", ptr(true), true},
		{"stored false", ptr(false), false},
		{"never chosen", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctl := &fakeMCP{}
			sh, _, _ := newTestShell(t, connectedFake(t, 20), Options{MCP: ctl})
			if tc.pref != nil {
				sh.app.Preferences().SetBool(prefMCPEnabled, *tc.pref)
			}
			sh.startMCP()
			if started := ctl.log() == "start(user=false)"; started != tc.starts || sh.mcp.on != tc.starts {
				t.Errorf("calls %q, on %v, want a start: %v", ctl.log(), sh.mcp.on, tc.starts)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

// storingMCP is a fakeMCP that keeps the switch where cogpit-cli reads it (MCPSwitchStore, D-49).
type storingMCP struct {
	fakeMCP
	stored, set bool
	writes      []bool
}

func (m *storingMCP) StoredMCP() (on, set bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stored, m.set
}

func (m *storingMCP) StoreMCP(on bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stored, m.set, m.writes = on, true, append(m.writes, on)
	return nil
}

func (m *storingMCP) written() []bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]bool(nil), m.writes...)
}

// TestMCPSwitchStore (D-49, A15): the switch store decides the launch over the window's
// preference; a store never set takes the preference's explicit choice once (a switch set before
// D-49), and takes nothing when the user never chose, so a new installation is off and unset; every
// click that worked is stored there too, for cogpit-cli mcp.
func TestMCPSwitchStore(t *testing.T) {
	off := &storingMCP{stored: false, set: true}
	sh, _, _ := newTestShell(t, connectedFake(t, 20), Options{MCP: off})
	sh.app.Preferences().SetBool(prefMCPEnabled, true) // the store wins over an older preference
	sh.startMCP()
	if off.log() != "" || sh.mcp.on {
		t.Errorf("a stored off was ignored: calls %q", off.log())
	}

	migrate := &storingMCP{}
	sh, _, _ = newTestShell(t, connectedFake(t, 20), Options{MCP: migrate})
	sh.app.Preferences().SetBool(prefMCPEnabled, false)
	sh.startMCP()
	if migrate.log() != "" || sh.mcp.on || len(migrate.written()) != 1 || migrate.written()[0] {
		t.Errorf("the preference's off was not taken and copied: calls %q, writes %v", migrate.log(), migrate.written())
	}

	on := &storingMCP{stored: true, set: true}
	sh, w, q := newTestShell(t, connectedFake(t, 20), Options{MCP: on})
	sh.startMCP()
	settle(w)
	q.until(t, "the server on", func() bool { return sh.mcp.on })
	test.Tap(sh.status.mcp)
	q.until(t, "the switch stored off", func() bool { got := on.written(); return len(got) == 1 && !got[0] })
	if sh.mcp.on {
		t.Error("the server is still on after the click")
	}
}

// TestMCPSwitchStoreIsOptIn (D-49, the owner's decision of 2026-10-21): a new installation, with
// neither a stored switch nor the old preference, starts off and writes nothing; a stored "on" stays
// on; an old installation whose preference was stored true stays on, and its choice is copied into
// the store once, where cogpit-cli reads it.
func TestMCPSwitchStoreIsOptIn(t *testing.T) {
	t.Run("a new installation is off", func(t *testing.T) {
		fresh := &storingMCP{}
		sh, _, _ := newTestShell(t, connectedFake(t, 20), Options{MCP: fresh})
		sh.startMCP()
		if fresh.log() != "" || sh.mcp.on || sh.status.mcp.on {
			t.Errorf("a new installation started the server: calls %q", fresh.log())
		}
		if got := fresh.written(); len(got) != 0 {
			t.Errorf("a launch without a choice stored one: %v", got)
		}
	})
	t.Run("a stored on stays on", func(t *testing.T) {
		on := &storingMCP{stored: true, set: true}
		sh, _, _ := newTestShell(t, connectedFake(t, 20), Options{MCP: on})
		sh.startMCP()
		if on.log() != "start(user=false)" || !sh.mcp.on || len(on.written()) != 0 {
			t.Errorf("a stored on: calls %q, on %v, writes %v", on.log(), sh.mcp.on, on.written())
		}
	})
	t.Run("a stored legacy preference true stays on", func(t *testing.T) {
		legacy := &storingMCP{}
		sh, _, _ := newTestShell(t, connectedFake(t, 20), Options{MCP: legacy})
		sh.app.Preferences().SetBool(prefMCPEnabled, true)
		sh.startMCP()
		if legacy.log() != "start(user=false)" || !sh.mcp.on {
			t.Errorf("the preference's on was ignored: calls %q, on %v", legacy.log(), sh.mcp.on)
		}
		if got := legacy.written(); len(got) != 1 || !got[0] {
			t.Errorf("the preference's on was not copied into the store: %v", got)
		}
	})
}

// TestMCPStartFailureShowsOff: a server that cannot start leaves the item off with the reason, and
// the stored choice alone decides the next launch.
func TestMCPStartFailureShowsOff(t *testing.T) {
	ctl := &fakeMCP{startErr: errors.New("address in use")}
	sh, _, _ := newTestShell(t, connectedFake(t, 20), Options{MCP: ctl})
	sh.app.Preferences().SetBool(prefMCPEnabled, true) // switched on in an earlier run
	sh.startMCP()
	if sh.mcp.on || !strings.Contains(sh.status.mcp.tip, "address in use") {
		t.Errorf("on %v, tooltip %q", sh.mcp.on, sh.status.mcp.tip)
	}
	if !sh.app.Preferences().BoolWithFallback(prefMCPEnabled, true) {
		t.Error("a failed start changed the stored choice")
	}
}

// TestNoMCPControlShowsNoItem: --no-mcp leaves no item, no gear and no menu entry, and the link
// ends the bar.
func TestNoMCPControlShowsNoItem(t *testing.T) {
	sh, w, _ := newTestShell(t, connectedFake(t, 20), Options{})
	settle(w)
	if sh.status.mcp != nil || sh.hasMCP() {
		t.Error("an MCP item without a server")
	}
	sh.openMCPDetails()
	if sh.mcp.details != nil {
		t.Error("MCP details without a server")
	}
	statusSizes(sh, 2000)
	if sh.status.layout.sep.Visible() {
		t.Error("a divider after the link with nothing right of it")
	}
}

// TestMCPSwitchIsNoCommand: the switch is a GUI control, not an engine command, so no MCP tool
// can start the server (D-2, agents and MCP).
func TestMCPSwitchIsNoCommand(t *testing.T) {
	r := engine.NewRegistry()
	if err := commands.RegisterAll(r); err != nil {
		t.Fatal(err)
	}
	for _, c := range r.Commands() {
		text := strings.ToLower(c.ID + " " + c.Label + " " + c.Description)
		if strings.Contains(text, "mcp") {
			t.Errorf("command %q mentions the MCP server: %q", c.ID, text)
		}
	}
}

// TestMCPSwitchKeepsTheLinkLeftAndWidths: the item keeps its width on and off, so that the link and
// the status items do not move when the user clicks it; the link still gives way first.
func TestMCPSwitchKeepsTheLinkLeftAndWidths(t *testing.T) {
	ctl := &fakeMCP{}
	sh, w, q := mcpShell(t, ctl)
	q.until(t, "the diagnostics", func() bool { return sh.diag.total > 0 })
	settle(w)
	s := sh.status
	th := fyne.CurrentApp().Settings().Theme()
	width := s.layout.left.MinSize().Width + s.layout.right.MinSize().Width + 2*th.Size(theme.SizeNameInnerPadding) + 400
	link, _, linkW, _ := statusSizes(sh, width)
	onW := s.layout.right.Size().Width
	test.Tap(s.mcp)
	q.until(t, "off", func() bool { return !sh.mcp.on && !sh.mcp.busy })
	settle(w)
	link2, _, linkW2, _ := statusSizes(sh, width)
	if s.layout.right.Size().Width != onW || link != link2 || linkW != linkW2 {
		t.Errorf("the layout moved: item %v→%v, link %v→%v, link width %v→%v", onW, s.layout.right.Size().Width, link, link2, linkW, linkW2)
	}
}
