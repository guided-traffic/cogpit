package fyneui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/terminal"
)

// fixedNow is the clock of every test, so that ages in the PNGs do not change between runs.
var fixedNow = time.Date(2026, 10, 11, 14, 30, 0, 0, time.UTC)

// newTestEngine is engine.New with the tests' clock, so that a time the window shows (an object's
// age, "23d ago") is the same in every run and a render changes only when the window does (the
// owner, 2026-10-25: renders were updated for a day's turn of an age alone). The clock starts at
// fixedNow and runs, so timers and deadlines go on as with time.Now; a test's own Now stays. A
// demo cluster's objects are dated from fixedNow too.
func newTestEngine(o engine.Options) *engine.Session {
	if o.Now == nil {
		start := time.Now()
		o.Now = func() time.Time { return fixedNow.Add(time.Since(start)) }
	}
	if c, ok := o.Connector.(*engine.ClusterConnector); ok && c.IncludeDemo && c.DemoOptions.Now.IsZero() {
		c.DemoOptions.Now = fixedNow
	}
	return engine.New(o)
}

// fakeEngine is an Engine with canned data. Dispatch changes its UI state the way the real
// engine's commands do and emits events on the dispatching goroutine, as the real one does.
type fakeEngine struct {
	mu         sync.Mutex
	ui         engine.UIState
	dockShown  bool // UIState opened the dock for the tabs a test put in the state (SyncDock)
	tables     map[string]*engine.TableSnapshot
	kindsGate  chan struct{} // set: kinds.list answers once it is closed (a cluster's kinds read late)
	details    map[string]*engine.DetailModel
	metrics    map[string]*engine.ObjectMetrics
	subs       map[int]func(engine.Event)
	nextSub    int
	calls      []engine.Invocation
	reg        *engine.Registry
	contexts   []engine.ContextInfo
	diag       engine.Diagnostics
	confirmer  engine.Confirmer
	kinds      []kindInfo
	namespaces []string
	// terminatingNS are the namespaces resources.list answers as Terminating.
	terminatingNS map[string]bool
	// nsErr, when set, is resources.list's answer for the namespaces, as for a cluster whose cache
	// never read them and cannot now (its login expired, its connection interrupted).
	nsErr    error
	disabled map[string]string // command id → the reason Enabled returns
	opened   map[string]int    // dock tab id → times its stream was opened
	// quietTerminals: OpenTerminal's sessions print nothing (quietReader).
	quietTerminals bool
	// terminalCallsOnly: terminal.open is recorded but opens no dock tab, so that no terminal
	// session runs (two ending at once print their end notices together, which the test driver's
	// synchronous fyne.Do turns into a data race on the shared text shaper).
	terminalCallsOnly bool
	closed            map[string]int // dock tab id → times its stream was closed
	// handlers answer further commands by id, so that an area's tests add its commands in its
	// own file (editor_test.go).
	handlers map[string]func(context.Context, engine.Invocation) (any, error)
	// chat is the built-in agent's chat (chat_fake_test.go); nil: none, as the CLI's engine.
	chat engine.ChatAgent
}

var _ Engine = (*fakeEngine)(nil)

func newFakeEngine(t testing.TB) *fakeEngine {
	f := &fakeEngine{
		tables: map[string]*engine.TableSnapshot{}, details: map[string]*engine.DetailModel{},
		subs: map[int]func(engine.Event){}, reg: engine.NewRegistry(), disabled: map[string]string{},
		opened: map[string]int{}, closed: map[string]int{},
		handlers: map[string]func(context.Context, engine.Invocation) (any, error){},
	}
	f.ui = engine.UIState{Connection: engine.Connection{State: engine.Disconnected}, Sort: engine.SortSpec{Column: "name"}}
	enabled := func(id string) func(*engine.Session, containerParams) error {
		return func(*engine.Session, containerParams) error { return f.reason(id) }
	}
	run := func(context.Context, *engine.Session, containerParams) (any, error) { return nil, nil }
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(engine.Register(f.reg, engine.Spec[containerParams]{ID: "pod.logs", Label: "Logs", Kinds: []string{"pods"}, Order: 500, Danger: engine.DangerRead, Enabled: enabled("pod.logs"), Run: run}))
	must(engine.Register(f.reg, engine.Spec[containerParams]{ID: "pod.shell", Label: "Shell", Kinds: []string{"pods"}, Order: 200, Danger: engine.DangerRead, Enabled: enabled("pod.shell"), Run: run}))
	targets := func(_ *engine.Session, p refParams) ([]model.ObjectRef, error) { return []model.ObjectRef{p.Ref}, nil }
	runRef := func(context.Context, *engine.Session, refParams) (any, error) { return nil, nil }
	must(engine.Register(f.reg, engine.Spec[refParams]{ID: "object.delete", Label: "Delete", Order: engine.OrderDelete, Danger: engine.DangerDestructive,
		Enabled: func(_ *engine.Session, p refParams) error {
			if p.Ref.Kind == kindPortForwards { // as the engine: a local kind is no API object
				return fmt.Errorf("%w: a local kind", engine.ErrNotApplicable)
			}
			return f.reason("object.delete")
		}, Targets: targets, Run: runRef}))
	must(engine.Register(f.reg, engine.Spec[refParams]{ID: "deployment.restart", Label: "Restart", Kinds: []string{"deployments"}, Order: 200, Danger: engine.DangerMutating,
		Enabled: func(*engine.Session, refParams) error { return f.reason("deployment.restart") }, Targets: targets, Run: runRef}))
	must(engine.Register(f.reg, engine.Spec[struct{}]{ID: "terminal.open", Label: "Terminal", Danger: engine.DangerUI,
		Enabled: func(*engine.Session, struct{}) error { return f.reason("terminal.open") },
		Run:     func(context.Context, *engine.Session, struct{}) (any, error) { return nil, nil }}))
	f.fakeNavigatorHandlers()
	return f
}

func (f *fakeEngine) reason(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r := f.disabled[id]; r != "" {
		if r == notApplicable {
			return engine.ErrNotApplicable
		}
		return errors.New(r)
	}
	return nil
}

// notApplicable, as a reason in fakeEngine.disabled, makes the command not apply at all (hidden
// from menus), as the engine's object.delete does for a virtual kind.
const notApplicable = "\x00not applicable"

func (f *fakeEngine) UIState() engine.UIState {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.dockShown { // the tabs a test put in the state before the window read it were opened: shown
		f.ui.SyncDock(engine.UIState{})
		f.dockShown = true
	}
	return f.ui.Clone()
}

func (f *fakeEngine) Table(view string) *engine.TableSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tables[view]
}

func (f *fakeEngine) Detail(ref model.ObjectRef) (*engine.DetailModel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if d, ok := f.details[ref.String()]; ok {
		return d, nil
	}
	return &engine.DetailModel{Ref: ref, Title: singular(strings.ToUpper(ref.Kind[:1])+ref.Kind[1:]) + ": " + ref.Name}, nil
}

func (f *fakeEngine) ObjectMetrics(ref model.ObjectRef) (*engine.ObjectMetrics, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m := f.metrics[ref.String()]; m != nil {
		return m, nil
	}
	// As the engine answers for a cluster without a metrics provider, the common case.
	return &engine.ObjectMetrics{Ref: ref, Status: metrics.StatusNotServed, Message: "this cluster has no metrics provider"}, nil
}

func (f *fakeEngine) Subscribe(fn func(engine.Event)) func() {
	f.mu.Lock()
	id := f.nextSub
	f.nextSub++
	f.subs[id] = fn
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		delete(f.subs, id)
		f.mu.Unlock()
	}
}

func (f *fakeEngine) Diagnostics() engine.Diagnostics {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.diag
}

func (f *fakeEngine) Registry() *engine.Registry { return f.reg }

func (f *fakeEngine) Chat() engine.ChatAgent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.chat
}

func (f *fakeEngine) Contexts() ([]engine.ContextInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.contexts), nil
}

// fakeStream is a log stream or terminal session that records being closed.
type fakeStream struct {
	io.Reader
	f    *fakeEngine
	id   string
	done chan struct{}
}

func (s *fakeStream) Write(p []byte) (int, error) { return len(p), nil }
func (s *fakeStream) Resize(uint16, uint16) error { return nil }
func (s *fakeStream) Done() <-chan struct{}       { return s.done }
func (s *fakeStream) Close() error {
	if q, ok := s.Reader.(*quietReader); ok {
		q.close()
	}
	s.f.mu.Lock()
	s.f.closed[s.id]++
	s.f.mu.Unlock()
	return nil
}

func (f *fakeEngine) OpenLogs(_ context.Context, tabID string) (io.ReadCloser, error) {
	f.mu.Lock()
	f.opened[tabID]++
	f.mu.Unlock()
	return &fakeStream{Reader: strings.NewReader("2026-10-11T14:29:58Z listening on :8080\n"), f: f, id: tabID, done: make(chan struct{})}, nil
}

func (f *fakeEngine) OpenTerminal(_ context.Context, tabID string) (terminal.Session, error) {
	f.mu.Lock()
	f.opened[tabID]++
	quiet := f.quietTerminals
	f.mu.Unlock()
	var r io.Reader = strings.NewReader("$ ")
	if quiet {
		r = &quietReader{stop: make(chan struct{})}
	}
	return &fakeStream{Reader: r, f: f, id: tabID, done: make(chan struct{})}, nil
}

// quietReader is a terminal session that prints nothing until it is closed. Fyne's test driver
// runs fyne.Do on the calling goroutine, so a session's output is written into the terminal's grid
// on its reader goroutine while the test refreshes the widget on another: a data race of the test
// driver only (the desktop driver runs fyne.Do on the main goroutine). Tests that refresh open
// terminals set fakeEngine.quietTerminals.
type quietReader struct {
	stop chan struct{}
	once sync.Once
}

func (r *quietReader) Read([]byte) (int, error) { <-r.stop; return 0, io.EOF }
func (r *quietReader) close()                   { r.once.Do(func() { close(r.stop) }) }

func (f *fakeEngine) SetConfirmer(c engine.Confirmer) {
	f.mu.Lock()
	f.confirmer = c
	f.mu.Unlock()
}

// emit delivers an event to every subscriber on the calling goroutine.
func (f *fakeEngine) emit(ev engine.Event) {
	f.mu.Lock()
	fns := make([]func(engine.Event), 0, len(f.subs))
	for _, fn := range f.subs {
		fns = append(fns, fn)
	}
	f.mu.Unlock()
	for _, fn := range fns {
		fn(ev)
	}
}

// update changes the UI state and emits it, as Session.UpdateUI does.
func (f *fakeEngine) update(fn func(*engine.UIState)) {
	f.mu.Lock()
	old := f.ui.Clone()
	fn(&f.ui)
	syncFakeNavigator(&f.ui, old)
	// The welcome page or a failed connect's page, as the engine's UpdateUI derives them, where the
	// connection changed: the fake keeps no tabs as the engine does (a connected fake shows a view
	// without one), so that elsewhere a test's own Welcome stands.
	if f.ui.Connection != old.Connection || f.ui.Context != old.Context {
		f.ui.SyncWelcome()
	}
	f.ui.SyncDock(old) // a new or activated tab opens the dock, as the engine's UpdateUI does
	f.ui.Revision++
	st := f.ui.Clone()
	f.mu.Unlock()
	f.emit(engine.Event{Type: engine.EventUIState, UIState: &st})
}

// syncFakeNavigator keeps the navigator's state and the palette's text in step as the engine's
// syncNavigator does (engine/navigator.go): a closed palette has no text; a new level clears the
// search and puts the keyboard on its root, or at the section on the cluster it came from; a view
// opened at a cluster on the view's row.
func syncFakeNavigator(u *engine.UIState, old engine.UIState) {
	if !u.Palette {
		u.PaletteText, u.PaletteTextOrigin = "", ""
	}
	if u.Scope() != old.Scope() {
		u.Navigator.Search, u.Navigator.Row = "", engine.NavRowRoot
		if from := old.ScopeCluster(); u.ScopeCluster() == "" && from != "" {
			u.Navigator.Row = engine.NavRowContext(from)
		}
		return
	}
	if u.View != "" && u.View != old.View && u.ScopeCluster() != "" {
		u.Navigator.Row = u.View
	}
}

// fakeNavigatorHandlers answer the navigator's and the palette's state commands as the engine does
// (engine/navigator.go, layout.go, palettetext.go), so that the window's reports and ⌘P
// (findcluster.go) change the fake's state; ui.setNavigatorScope is fakeNavScope's.
func (f *fakeEngine) fakeNavigatorHandlers() {
	decode := func(inv engine.Invocation, p any) error { return json.Unmarshal(normalize(inv.Params), p) }
	f.handlers["ui.toggleNavigator"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p layoutToggleParams
		if err := decode(inv, &p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) {
			shown := u.NavigatorHidden // turned the other way round without open
			if p.Open != nil {
				shown = *p.Open
			}
			u.NavigatorHidden = !shown
		})
		return nil, nil
	}
	f.handlers["ui.setNavigatorSearch"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p navSearchParams
		if err := decode(inv, &p); err != nil {
			return nil, err
		}
		if err := engine.CheckNavigatorSearch(p.Search); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) { u.Navigator.Search = p.Search })
		return nil, nil
	}
	f.handlers["ui.setNavigatorRow"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p navRowParams
		if err := decode(inv, &p); err != nil {
			return nil, err
		}
		var err error
		f.update(func(u *engine.UIState) {
			if err = engine.CheckNavigatorRow(u.Scope(), p.Row); err == nil {
				u.Navigator.Row = p.Row
			}
		})
		return nil, err
	}
	f.handlers["ui.setNavigatorGroup"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p navGroupParams
		if err := decode(inv, &p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) {
			i := slices.Index(u.Navigator.Collapsed, p.Group)
			switch {
			case p.Collapsed && i < 0:
				u.Navigator.Collapsed = append(slices.Clone(u.Navigator.Collapsed), p.Group)
			case !p.Collapsed && i >= 0:
				u.Navigator.Collapsed = slices.Delete(slices.Clone(u.Navigator.Collapsed), i, i+1)
			}
		})
		return nil, nil
	}
	f.handlers["ui.palette.setText"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p paletteTextParams
		if err := decode(inv, &p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) {
			if inv.Origin == engine.OriginGUI && !u.Palette {
				return // the window's report of a palette closed meanwhile
			}
			u.Palette, u.PaletteText, u.PaletteTextOrigin = true, p.Text, inv.Origin
			if p.Text == "" {
				u.PaletteTextOrigin = ""
			}
		})
		return nil, nil
	}
}

// setNavScope answers ui.setNavigatorScope as the engine does in the cases the tests use
// (engine/navscope_test.go tests the engine's own): into another cluster it connects at once.
// fakeNavScope installs it; without it the fake does not know the command, and ← on a tree's root
// stays where it is.
func (f *fakeEngine) setNavScope(_ context.Context, inv engine.Invocation) (any, error) {
	var p navScopeParams
	if err := json.Unmarshal(normalize(inv.Params), &p); err != nil {
		return nil, err
	}
	id, err := engine.ParseNavScope(p.Scope)
	if err != nil {
		return nil, err
	}
	f.update(func(u *engine.UIState) {
		if id != "" && id != u.Context { // another cluster: connected, as the engine connects it
			u.Context, u.Connection, u.View = id, engine.Connection{State: engine.Connected}, kindPods
		}
		u.NavigatorScope = p.Scope
	})
	return nil, nil
}

// setTable replaces a view's snapshot and emits EventTable.
func (f *fakeEngine) setTable(view string, snap *engine.TableSnapshot) {
	f.mu.Lock()
	f.tables[view] = snap
	f.mu.Unlock()
	f.emit(engine.Event{Type: engine.EventTable, View: view})
}

// callsOf returns the recorded invocations of a command. A call is recorded as its dispatch
// begins, before its handler and state change run, so that a call waiting for the test (a
// confirmation, a gate) is seen and a check that nothing was dispatched sees one in flight. A
// test that waits for a call's effect waits for that effect (the state, the handler's own
// record), never for the call's record alone.
func (f *fakeEngine) callsOf(id string) []engine.Invocation {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []engine.Invocation
	for _, c := range f.calls {
		if c.ID == id {
			out = append(out, c)
		}
	}
	return out
}

// allCalls returns every recorded call: a copy, since the shell's goroutines record more while the
// test reads.
func (f *fakeEngine) allCalls() []engine.Invocation {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// windowReports are the commands the window sends on its own after a pause (navReport): the
// navigator's row and search and the palette's text, whenever they settle. None is a test's
// effect.
var windowReports = []string{"ui.setNavigatorRow", "ui.setNavigatorSearch", "ui.palette.setText"}

// mark is the place in the recorded calls that callsSince counts from.
func (f *fakeEngine) mark() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// callsSince returns the calls recorded after mark, without the window's own reports.
func (f *fakeEngine) callsSince(mark int) []engine.Invocation {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []engine.Invocation
	for _, c := range f.calls[mark:] {
		if !slices.Contains(windowReports, c.ID) {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeEngine) Dispatch(ctx context.Context, inv engine.Invocation) (any, error) {
	f.mu.Lock()
	f.calls = append(f.calls, inv)
	f.mu.Unlock()
	params := func(p any) error { return json.Unmarshal(normalize(inv.Params), p) }
	switch inv.ID {
	case "ui.openView":
		var p openViewParams
		if err := params(&p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) { u.View, u.Selected, u.Drawer = p.View, nil, nil })
	case "ui.select":
		var p selectModeParams // the modes and all as the engine applies them (tablecheck.go)
		if err := params(&p); err != nil {
			return nil, err
		}
		mode, err := engine.ParseSelectMode(p.Mode)
		if err != nil {
			return nil, err
		}
		refs := p.Refs
		if p.All {
			refs = nil
			f.mu.Lock()
			if s := f.tables[f.ui.View]; s != nil {
				for _, r := range s.Rows {
					refs = append(refs, r.Ref)
				}
			}
			f.mu.Unlock()
		}
		f.update(func(u *engine.UIState) {
			u.Selected = engine.ApplySelection(u.Selected, refs, mode)
			if (mode == engine.SelectReplace || mode == engine.SelectOpen) && !p.NoDrawer && len(refs) > 0 { // open: a plain click (A73)
				r := refs[0]
				u.Drawer = &r
			}
		})
	case "ui.navigate":
		var p refParams
		if err := params(&p); err != nil {
			return nil, err
		}
		if f.unknownKind(p.Ref.Kind) {
			return nil, fmt.Errorf("%w: %s", engine.ErrUnknownKind, p.Ref.Kind)
		}
		f.update(func(u *engine.UIState) { r := p.Ref; u.View, u.Selected, u.Drawer = r.Kind, []model.ObjectRef{r}, &r })
	case "ui.sort":
		var p sortParams
		if err := params(&p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) { u.Sort = engine.SortSpec{Column: p.Column, Desc: p.Desc} })
		f.mu.Lock()
		view := f.ui.View
		if s := f.tables[view]; s != nil {
			c := *s
			c.Sort = engine.SortSpec{Column: p.Column, Desc: p.Desc}
			c.Revision++
			f.tables[view] = &c
		}
		f.mu.Unlock()
		f.emit(engine.Event{Type: engine.EventTable, View: view})
	case "ui.setFilter":
		var p filterParams
		if err := params(&p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) {
			u.Filter = p.Filter
			if p.MatchCase != nil {
				u.FilterOptions.MatchCase = *p.MatchCase
			}
			if p.Regex != nil {
				u.FilterOptions.Regex = *p.Regex
			}
		})
	case "ui.setNamespace":
		var p namespacesParams
		if err := params(&p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) { u.Namespaces = p.Namespaces })
	case "ui.closeDrawer":
		f.update(func(u *engine.UIState) { u.Drawer = nil })
	case "cluster.connect":
		f.mu.Lock()
		h := f.handlers[inv.ID] // a test's own answer, a refusal
		f.mu.Unlock()
		if h != nil {
			return h(ctx, inv)
		}
		var p contextParams
		if err := params(&p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) {
			u.Context = p.Context
			u.Connection = engine.Connection{State: engine.Connected, Demo: p.Context == "demo"}
			u.View = "pods"
		})
	case "kinds.list":
		f.mu.Lock()
		gate := f.kindsGate
		f.mu.Unlock()
		if gate != nil {
			<-gate
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		return slices.Clone(f.kinds), nil
	case "resources.list": // the namespaces, a page at a time as the engine answers (limit 100 by default, at most 1000)
		var p listParams
		if err := params(&p); err != nil {
			return nil, err
		}
		limit := p.Limit
		switch {
		case limit == 0:
			limit = 100
		case limit > 1000:
			limit = 1000
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.nsErr != nil {
			return nil, f.nsErr
		}
		rows := make([]map[string]any, 0, len(f.namespaces))
		for _, n := range f.namespaces {
			status := model.StatusOK
			if f.terminatingNS[n] {
				status = model.StatusTerminating
			}
			rows = append(rows, map[string]any{"ref": model.ObjectRef{Kind: "namespaces", Name: n}, "status": status, "cells": []string{n}})
		}
		page := rows[min(p.Offset, len(rows)):]
		page = page[:min(limit, len(page))]
		return map[string]any{"rows": page, "total": len(rows), "returned": len(page)}, nil
	case "pod.logs", "pod.shell":
		var p containerParams
		if err := params(&p); err != nil {
			return nil, err
		}
		typ, title := engine.DockLogs, "Logs: "
		if inv.ID == "pod.shell" {
			typ, title = engine.DockShell, "Shell: "
		}
		var tab engine.DockTab
		f.update(func(u *engine.UIState) {
			id := fmt.Sprintf("%s-%d", typ, len(u.Dock)+1)
			r := p.Ref
			tab = engine.DockTab{ID: id, Type: typ, Title: title + p.Ref.Name, Ref: &r, Container: p.Container}
			u.Dock = append(u.Dock, tab)
			u.ActiveDock = id
		})
		return commands.DockResult{Tab: tab, Created: true}, nil // as the engine answers (commands/dock.go)
	case "terminal.open":
		f.mu.Lock()
		callsOnly := f.terminalCallsOnly
		f.mu.Unlock()
		if callsOnly {
			return nil, nil
		}
		var tab engine.DockTab
		f.update(func(u *engine.UIState) {
			id := fmt.Sprintf("terminal-%d", len(u.Dock)+1)
			tab = engine.DockTab{ID: id, Type: engine.DockTerminal, Title: "Terminal: " + u.Context}
			u.Dock = append(u.Dock, tab)
			u.ActiveDock = id
		})
		return commands.DockResult{Tab: tab, Created: true}, nil // as the engine answers (commands/dock.go)
	case "dock.close":
		var p dockParams
		if err := params(&p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) {
			u.Dock = slices.DeleteFunc(u.Dock, func(t engine.DockTab) bool { return t.ID == p.ID })
			if u.ActiveDock == p.ID {
				u.ActiveDock = ""
				if len(u.Dock) > 0 {
					u.ActiveDock = u.Dock[len(u.Dock)-1].ID
				}
			}
		})
	case "dock.activate":
		var p dockParams
		if err := params(&p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) { u.ShowDockTab(p.ID) }) // the active tab too: the dock opens (AW9)
	case "object.delete":
		var p refParams
		if err := params(&p); err != nil {
			return nil, err
		}
		f.mu.Lock()
		c, uiCtx := f.confirmer, f.ui.Context
		f.mu.Unlock()
		if c == nil {
			return nil, engine.ErrConfirmationUnavailable
		}
		ok, err := c.Confirm(ctx, engine.ConfirmRequest{Command: inv.ID, Label: "Delete", Danger: engine.DangerDestructive,
			Origin: inv.Origin, Context: uiCtx, Namespaces: []string{p.Ref.Namespace}, Objects: []model.ObjectRef{p.Ref}})
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, engine.ErrDeclined
		}
	default:
		f.mu.Lock()
		h := f.handlers[inv.ID]
		f.mu.Unlock()
		if h != nil {
			return h(ctx, inv)
		}
		// Commands a test registered itself (row-menu probes) succeed; anything else is unknown.
		if _, ok := f.reg.Get(inv.ID); !ok {
			return nil, fmt.Errorf("%w: %q", engine.ErrUnknownCommand, inv.ID)
		}
	}
	return nil, nil
}

// unknownKind reports a kind the fake cluster does not serve, as the engine refuses it.
func (f *fakeEngine) unknownKind(kind string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.kinds) == 0 {
		return false
	}
	for _, k := range f.kinds {
		if k.Kind == kind {
			return false
		}
	}
	return true
}

func normalize(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	return raw
}

// mainQueue stands in for Fyne's main goroutine in tests: the shell posts functions with do, and
// the test goroutine runs them with drain. Fyne's test driver runs fyne.Do synchronously on the
// calling goroutine, which would let engine goroutines touch widgets; the queue keeps every widget
// update on the test goroutine, so the race detector sees any update that bypasses sh.do.
type mainQueue struct {
	mu  sync.Mutex
	fns []func()
}

func (q *mainQueue) do(fn func()) {
	q.mu.Lock()
	q.fns = append(q.fns, fn)
	q.mu.Unlock()
}

// drain runs queued functions until the queue is empty.
func (q *mainQueue) drain() {
	for {
		q.mu.Lock()
		if len(q.fns) == 0 {
			q.mu.Unlock()
			return
		}
		fn := q.fns[0]
		q.fns = q.fns[1:]
		q.mu.Unlock()
		fn()
	}
}

// until drains the queue until cond holds, failing the test after fifteen seconds: five were too few
// for a race shard on CI's shared runner under load (2026-10-20 night, TestCreateTabKeepsSecretsOffDisk
// waiting for its tab); a condition that holds returns at once.
func (q *mainQueue) until(t testing.TB, what string, cond func() bool) {
	t.Helper()
	q.untilShows(t, what, cond, nil)
}

// untilShows is until with what the window shows in the failure (shows, read at the timeout), so
// that a wait that fails on CI says how far the window got.
func (q *mainQueue) untilShows(t testing.TB, what string, cond func() bool, shows func() string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		q.drain()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			if shows != nil {
				t.Fatalf("timed out waiting for %s; shown: %s", what, shows())
			}
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// fireTimer runs an armed time.AfterFunc's function now, as if its time had come, and reports
// whether one was armed. A test arms the timer with a long duration, so that a loaded runner cannot
// let it run before the step that must see it pending, and ends it here.
func fireTimer(t *time.Timer) bool {
	if t == nil || !t.Stop() {
		return false
	}
	t.Reset(0)
	return true
}

// hoverTip moves the pointer onto at and waits for a tooltip to show there. Anything that hides the
// pending tooltip on the way (a refresh that rebuilds the cell under the pointer) cancels it, and
// only the pointer's next move asks again: the pointer leaves for off and comes back whenever none
// is pending.
func hoverTip(t testing.TB, sh *shell, w fyne.Window, q *mainQueue, what string, at, off fyne.Position) string {
	t.Helper()
	test.MoveMouse(w.Canvas(), at)
	q.until(t, what, func() bool {
		if sh.tip.visibleText() != "" {
			return true
		}
		if sh.tip.owner == nil {
			test.MoveMouse(w.Canvas(), off)
			test.MoveMouse(w.Canvas(), at)
		}
		return false
	})
	return sh.tip.visibleText()
}

// newTestShell starts a shell on f in a 1440×900 offscreen window.
func newTestShell(t testing.TB, f *fakeEngine, opts Options) (*shell, fyne.Window, *mainQueue) {
	t.Helper()
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(opts.Theme))
	w := test.NewTempWindow(t, nil)
	w.SetPadded(false)
	q := &mainQueue{}
	sh := newShell(f, a, w, opts, q.do)
	sh.now = func() time.Time { return fixedNow }
	sh.kc.startDir = t.TempDir                      // no test lists the developer's home directory or ~/.kube
	sh.systemReduce = func() bool { return false }  // the computer's Reduce motion stays out of the tests
	sh.systemBattery = func() bool { return false } // and its power source
	sh.confirmer.armDelay = 0                       // tests tap at once; TestConfirmationsOneAtATimeAndArmed covers the delay
	f.SetConfirmer(sh.confirmer)
	w.SetContent(sh.root)
	w.Resize(fyne.NewSize(1440, 900))
	sh.start()
	t.Cleanup(func() {
		sh.stop()
		q.drain()
	})
	return sh, w, q
}

// The fixture: a demo-like cluster with 10,000 pods.

var fixtureNamespaces = []string{
	"argo", "cert-manager", "checkout", "data-pipeline", "default", "identity", "ingress-nginx",
	"inventory", "kube-system", "logging", "ml-serving", "monitoring", "notifications", "orders",
	"payments", "platform", "qa", "search", "staging", "storefront",
}

var fixtureApps = []string{
	"checkout-api", "payments-worker", "web-frontend", "search-indexer", "cart-service",
	"auth-gateway", "inventory-sync", "notification-dispatcher", "recommendation-engine",
	"metrics-collector", "image-resizer", "order-processor", "fraud-detector", "session-cache",
	"ledger-writer", "catalog-api", "pricing-engine", "shipping-quotes",
}

func fixtureKinds() []kindInfo {
	return []kindInfo{
		{Kind: "nodes", Title: "Nodes", APIKind: "Node", Version: "v1"},
		{Kind: "pods", Title: "Pods", APIKind: "Pod", Version: "v1", Namespaced: true},
		{Kind: "deployments", Title: "Deployments", APIKind: "Deployment", Group: "apps", Version: "v1", Namespaced: true},
		{Kind: "services", Title: "Services", APIKind: "Service", Version: "v1", Namespaced: true},
		{Kind: "namespaces", Title: "Namespaces", APIKind: "Namespace", Version: "v1"},
		{Kind: "events", Title: "Events", APIKind: "Event", Version: "v1", Namespaced: true},
	}
}

var podColumns = []model.Column{
	{ID: "name", Title: "Name", Type: model.ColumnText},
	{ID: "namespace", Title: "Namespace", Type: model.ColumnText},
	{ID: "containers", Title: "Containers", Type: model.ColumnNumber},
	{ID: "restarts", Title: "Restarts", Type: model.ColumnNumber},
	{ID: "controlledBy", Title: "Controlled By", Type: model.ColumnText},
	{ID: "node", Title: "Node", Type: model.ColumnText},
	{ID: "qos", Title: "QoS", Type: model.ColumnText},
	{ID: "age", Title: "Age", Type: model.ColumnAge},
	{ID: "status", Title: "Status", Type: model.ColumnText},
}

// fixturePods returns n pods, sorted by name, deterministic for a seed.
func fixturePods(n int, seed int64) *engine.TableSnapshot {
	rng := rand.New(rand.NewSource(seed))
	const alnum = "bcdfghjklmnpqrstvwxz2456789"
	randStr := func(k int) string {
		b := make([]byte, k)
		for i := range b {
			b[i] = alnum[rng.Intn(len(alnum))]
		}
		return string(b)
	}
	hashes := map[string]string{}
	rows := make([]model.Row, 0, n)
	for i := 0; i < n; i++ {
		app := fixtureApps[rng.Intn(len(fixtureApps))]
		ns := fixtureNamespaces[rng.Intn(len(fixtureNamespaces))]
		key := app + "/" + ns
		if hashes[key] == "" {
			hashes[key] = randStr(10)
		}
		name := app + "-" + hashes[key] + "-" + randStr(5)
		owner := "ReplicaSet " + app + "-" + hashes[key]
		if ns == "kube-system" {
			app, owner = "kube-proxy", "DaemonSet kube-proxy"
			name = "kube-proxy-" + randStr(5)
		}
		status, token := "Running", model.StatusOK
		switch r := rng.Intn(100); {
		case r < 3:
			status, token = "CrashLoopBackOff", model.StatusError
		case r < 6:
			status, token = "Pending", model.StatusPending
		case r < 8:
			status, token = "Completed", model.StatusSucceeded
			owner = "Job " + app + "-29384" + randStr(2)
		case r < 9:
			status, token = "Terminating", model.StatusTerminating
		case r < 10:
			status, token = "ContainerCreating", model.StatusPending
		}
		containers := 1 + rng.Intn(2)
		ready := containers
		if token != model.StatusOK {
			ready = rng.Intn(containers + 1)
		}
		restarts := int64(0)
		if rng.Intn(5) == 0 {
			restarts = int64(rng.Intn(12))
		}
		if token == model.StatusError {
			restarts = int64(20 + rng.Intn(400))
		}
		qos := []string{"Burstable", "Burstable", "Burstable", "Guaranteed", "Guaranteed", "BestEffort"}[rng.Intn(6)]
		node := fmt.Sprintf("worker-%c-%02d", 'a'+rune(rng.Intn(3)), 1+rng.Intn(17))
		created := fixedNow.Add(-time.Duration(rng.Int63n(int64(45 * 24 * time.Hour))))
		ref := model.ObjectRef{Context: "demo", Kind: "pods", Namespace: ns, Name: name}
		rows = append(rows, model.Row{
			Ref: ref, Status: token,
			Cells: []model.Cell{
				{Text: name}, {Text: ns},
				{Text: fmt.Sprintf("%d/%d", ready, containers), Num: int64(ready)},
				{Num: restarts},
				{Text: owner}, {Text: node}, {Text: qos},
				{Num: created.Unix()},
				{Text: status, Status: token},
			},
		})
	}
	slices.SortFunc(rows, func(a, b model.Row) int { return strings.Compare(a.Ref.Name, b.Ref.Name) })
	return &engine.TableSnapshot{
		View: "pods", Context: "demo", Title: "Pods", Namespaced: true, Columns: podColumns, Rows: rows,
		Sort: engine.SortSpec{Column: "name"}, Total: n, Revision: 1, Synced: true,
	}
}

// fixturePodDetail is the drawer model of a pod.
func fixturePodDetail(row model.Row) *engine.DetailModel {
	ref := row.Ref
	app := ref.Name
	if i := strings.LastIndex(app, "-"); i > 0 {
		app = app[:i]
	}
	created := time.Unix(row.Cells[7].Num, 0).UTC()
	return &engine.DetailModel{
		Ref: ref, Title: "Pod: " + ref.Name, Status: model.StatusOK,
		Containers: []string{"api", "envoy"},
		// The drawer model's shape (engine.DetailModel): Properties first, no UID, folded lists,
		// links; the containers under their group.
		Sections: []model.DetailSection{
			{Title: model.TitleProperties, Items: []model.DetailItem{
				{Name: "Created", Value: model.FormatAge(fixedNow.Sub(created)) + " ago (" + model.TimeFormat{}.Format(created) + ")"}, // as the engine writes it without a layout (D-31)
				{Name: "Name", Value: ref.Name},
				{Name: "Namespace", Value: ref.Namespace, Link: &model.ObjectRef{Kind: "namespaces", Name: ref.Namespace}},
				{Name: "Labels", Fold: "Labels",
					Value: "app=checkout-api\npod-template-hash=" + strings.TrimPrefix(app, "checkout-api-") + "\ntier=backend",
					Chips: []model.Chip{{Text: "app=checkout-api"}, {Text: "pod-template-hash=" + strings.TrimPrefix(app, "checkout-api-")}, {Text: "tier=backend"}}},
				{Name: "Annotations", Value: "prometheus.io/port=9102\nprometheus.io/scrape=true", Fold: "Annotations"},
				{Name: "Controlled By", Value: row.Cells[4].Text},
				{Name: "Status", Value: "Running", Status: model.StatusOK},
				{Name: "Node", Value: row.Cells[5].Text, Link: &model.ObjectRef{Kind: "nodes", Name: row.Cells[5].Text}},
				{Name: "Pod IP", Value: "10.244.7.31"},
				{Name: "Service Account", Value: "checkout-api", Link: &model.ObjectRef{Kind: "serviceaccounts", Namespace: ref.Namespace, Name: "checkout-api"}},
				{Name: "QoS Class", Value: row.Cells[6].Text},
				{Name: "Conditions", Value: "Initialized\nReady\nContainersReady\nPodScheduled", Chips: []model.Chip{
					{Text: "Initialized", Status: model.StatusOK}, {Text: "Ready", Status: model.StatusOK},
					{Text: "ContainersReady", Status: model.StatusOK}, {Text: "PodScheduled", Status: model.StatusOK}}},
			}},
			{Title: "api", Group: "Containers", Items: []model.DetailItem{
				{Name: "Status", Value: "Running, ready", Status: model.StatusOK},
				{Name: "Image", Value: "ghcr.io/example/checkout-api:2.14.1"},
				{Name: "Ports", Value: "http: 8080/TCP\nmetrics: 9102/TCP"},
			}},
			{Title: "envoy", Group: "Containers", Items: []model.DetailItem{
				{Name: "Status", Value: "Running, ready", Status: model.StatusOK},
				{Name: "Image", Value: "envoyproxy/envoy:v1.31.2"},
			}},
		},
		Events: []engine.EventItem{
			{Type: "Warning", Reason: "Unhealthy", Message: "Readiness probe failed: HTTP probe failed with statuscode: 503", Source: "kubelet", Count: 3, LastSeen: fixedNow.Add(-4 * time.Minute).Unix(), Status: model.StatusWarning},
			{Type: "Normal", Reason: "Started", Message: "Started container api", Source: "kubelet", Count: 1, LastSeen: fixedNow.Add(-26 * time.Hour).Unix()},
			{Type: "Normal", Reason: "Pulled", Message: "Container image \"ghcr.io/example/checkout-api:2.14.1\" already present on machine", Source: "kubelet", Count: 1, LastSeen: fixedNow.Add(-26 * time.Hour).Unix()},
			{Type: "Normal", Reason: "Scheduled", Message: "Successfully assigned " + ref.Namespace + "/" + ref.Name + " to " + row.Cells[5].Text, Source: "default-scheduler", Count: 1, LastSeen: fixedNow.Add(-26 * time.Hour).Unix()},
		},
		YAML: "apiVersion: v1\nkind: Pod\nmetadata:\n  name: " + ref.Name + "\n  namespace: " + ref.Namespace + "\n  labels:\n    app: checkout-api\nspec:\n  containers:\n  - name: api\n    image: ghcr.io/example/checkout-api:2.14.1\n  - name: envoy\n    image: envoyproxy/envoy:v1.31.2\nstatus:\n  phase: Running\n",
	}
}

func fixtureDiagnostics() engine.Diagnostics {
	d := engine.Diagnostics{Context: "demo", Demo: true, Snapshots: 5321,
		DemoStats: map[string]any{"objects": 10870, "eventsSent": 1234567, "eventsPerSec": 1000.0}}
	d.Requests.Total = 1284
	d.Requests.ByVerb = map[string]int64{"list": 12, "watch": 9, "get": 3}
	// Six informers; the UI only counts them, and may not import package cache to name the type.
	d.Informers = slices.Grow(d.Informers, 6)[:6]
	return d
}

func fixtureContexts() []engine.ContextInfo {
	return []engine.ContextInfo{
		{Name: "demo", Demo: true},
		{Name: "kind-cogpit", Cluster: "kind-cogpit", User: "kind-cogpit", Server: "https://127.0.0.1:52341", Current: true},
		{Name: "prod-eu-central", Cluster: "prod-eu-central", Server: "https://api.prod-eu-central.example.com:6443", Namespace: "platform"},
		{Name: "staging-us-east", Cluster: "staging-us-east", Server: "https://api.staging-us-east.example.com:6443"},
		{Name: "dev-hfi", Cluster: "minikube", Server: "https://192.168.49.2:8443", Namespace: "default"},
	}
}

// connectedFake is a fake connected to the demo cluster, showing pods.
func connectedFake(t testing.TB, pods int) *fakeEngine {
	f := newFakeEngine(t)
	f.kinds = fixtureKinds()
	f.namespaces = fixtureNamespaces
	f.contexts = fixtureContexts()
	f.diag = fixtureDiagnostics()
	f.ui.Context = "demo"
	f.ui.Connection = engine.Connection{State: engine.Connected, Demo: true}
	f.ui.View = "pods"
	f.ui.Revision = 1
	f.tables["pods"] = fixturePods(pods, 7)
	return f
}

// skipTerminalUnderRace skips a test whose terminals render while the test changes the window:
// Fyne's test driver runs fyne.Do on the calling goroutine, so a terminal's reader goroutine
// shapes text through Fyne's shared font cache while the test (or the next one) does too, a data
// race of the test driver only (the desktop driver runs fyne.Do on the main goroutine). These
// tests run without the race detector (make test-unit).
func skipTerminalUnderRace(t *testing.T) {
	t.Helper()
	if raceEnabled {
		t.Skip("terminal rendering races Fyne's test driver under -race (fake_test.go skipTerminalUnderRace)")
	}
}
