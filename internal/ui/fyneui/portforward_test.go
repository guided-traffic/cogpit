package fyneui

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// Port forwarding in the GUI against the fake engine: the forward dialog from a pod's port, the
// port's link, the Port Forwarding view and its row menu. The forward commands are probes with
// the engine's parameter shapes; their handlers answer as the engine does.

type probeForward struct {
	Ref           model.ObjectRef `json:"ref"`
	Container     string          `json:"container,omitempty"`
	Port          uint16          `json:"port"`
	LocalPort     uint16          `json:"localPort,omitempty"`
	HTTPS         bool            `json:"https,omitempty"`
	OpenInBrowser bool            `json:"openInBrowser,omitempty"`
}

type probeForwardEdit struct {
	Ref           model.ObjectRef `json:"ref"`
	LocalPort     *uint16         `json:"localPort,omitempty"`
	HTTPS         *bool           `json:"https,omitempty"`
	OpenInBrowser *bool           `json:"openInBrowser,omitempty"`
}

// browserLog records the URLs the shell opens instead of opening them.
type browserLog struct {
	mu   sync.Mutex
	urls []string
}

func recordBrowser(t testing.TB) *browserLog {
	b := &browserLog{}
	old := openBrowser
	openBrowser = func(_ fyne.App, u *url.URL) error {
		b.mu.Lock()
		b.urls = append(b.urls, u.String())
		b.mu.Unlock()
		return nil
	}
	t.Cleanup(func() { openBrowser = old })
	return b
}

func (b *browserLog) opened() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.urls)
}

const forwardURLFixture = "http://127.0.0.1:54321"

// registerForwardProbes adds the forward commands to the fake, at the places the engine gives
// them, and answers them with a forward on 127.0.0.1:54321.
func registerForwardProbes(t testing.TB, f *fakeEngine) {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	targets := func(_ *engine.Session, p probeForward) ([]model.ObjectRef, error) {
		return []model.ObjectRef{p.Ref}, nil
	}
	run := func(context.Context, *engine.Session, probeForward) (any, error) { return nil, nil }
	for _, id := range []string{"pod.portForward", "service.portForward"} {
		must(engine.Register(f.reg, engine.Spec[probeForward]{ID: id, Label: "Forward", Danger: engine.DangerMutating, Targets: targets, Run: run}))
	}
	ref := func(context.Context, *engine.Session, probeRef) (any, error) { return nil, nil }
	for _, c := range []struct {
		id, label string
		order     int
	}{{"portforward.open", "Open", 100}, {"portforward.stop", "Stop", 300}, {"portforward.remove", "Delete", engine.OrderDelete}} {
		must(engine.Register(f.reg, engine.Spec[probeRef]{ID: c.id, Label: c.label, Kinds: []string{kindPortForwards}, Order: c.order, Danger: engine.DangerUI, Run: ref}))
	}
	must(engine.Register(f.reg, engine.Spec[probeForwardEdit]{ID: "portforward.edit", Label: "Edit", Kinds: []string{kindPortForwards}, Order: 200,
		Danger: engine.DangerMutating,
		Targets: func(_ *engine.Session, p probeForwardEdit) ([]model.ObjectRef, error) {
			return []model.ObjectRef{p.Ref}, nil
		},
		Prefill: func(_ *engine.Session, p probeForwardEdit) (probeForwardEdit, error) {
			port, https, open := uint16(9443), true, false // the forward's settings
			p.LocalPort, p.HTTPS, p.OpenInBrowser = &port, &https, &open
			return p, nil
		},
		Run: func(context.Context, *engine.Session, probeForwardEdit) (any, error) { return nil, nil }}))

	answer := func(_ context.Context, inv engine.Invocation) (any, error) {
		var p struct {
			OpenInBrowser *bool `json:"openInBrowser"`
		}
		_ = json.Unmarshal(inv.Params, &p)
		return map[string]any{"url": forwardURLFixture, "spec": map[string]any{"openInBrowser": p.OpenInBrowser != nil && *p.OpenInBrowser}}, nil
	}
	for _, id := range []string{"pod.portForward", "service.portForward", "portforward.edit", "portforward.open"} {
		f.handlers[id] = answer
	}
}

// forwardsProfile is the local kind's profile.
func forwardsProfile(t testing.TB) *model.KindProfile {
	t.Helper()
	for _, p := range kinds.Local() {
		if p.Key == kindPortForwards {
			return p
		}
	}
	t.Fatal("no local profile portforwards")
	return nil
}

// fixtureForwardObjects are forwards in each state, as the engine writes them.
func fixtureForwardObjects() []*unstructured.Unstructured {
	obj := func(name, ns, kind, target string, port, local int64, protocol, phase, pod, msg string, podPort int64) *unstructured.Unstructured {
		status := map[string]any{"phase": phase}
		if msg != "" {
			status["message"] = msg
		}
		if phase != "Stopped" && phase != "Failed" {
			status["localPort"], status["address"] = local, "127.0.0.1:"+strconv.FormatInt(local, 10)
			status["url"] = protocol + "://127.0.0.1:" + strconv.FormatInt(local, 10)
		}
		if pod != "" {
			status["pod"], status["podPort"] = pod, podPort
		}
		return &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "cogpit.io/v1", "kind": "PortForward",
			"metadata": map[string]any{"name": name, "namespace": ns, "uid": "uid-pf-" + name, "creationTimestamp": ts(time.Hour)},
			"spec":     map[string]any{"kind": kind, "name": target, "port": port, "localPort": int64(0), "protocol": protocol, "openInBrowser": true},
			"status":   status,
		}}
	}
	return []*unstructured.Unstructured{
		obj("pod-checkout-api-7d9f8c6b5-bkmqx-8080", "checkout", "Pod", "checkout-api-7d9f8c6b5-bkmqx", 8080, 54321, "http", "Active", "checkout-api-7d9f8c6b5-bkmqx", "", 8080),
		obj("service-storefront-443", "storefront", "Service", "storefront", 443, 9443, "https", "Active", "storefront-5c4b8d7f9-lmqrx", "", 8443),
		obj("service-payments-api-80", "payments", "Service", "payments-api", 80, 61002, "http", "Reconnecting", "payments-api-6f8b9c7d4-ctnsy",
			"lost the connection to pod payments-api-6f8b9c7d4-ctnsy", 8080),
		obj("pod-ledger-writer-0-5432", "orders", "Pod", "ledger-writer-0", 5432, 15432, "http", "Waiting", "", "pod ledger-writer-0 is pending", 0),
		obj("pod-search-indexer-9d8c7b6a5-dfnrz-9200", "search", "Pod", "search-indexer-9d8c7b6a5-dfnrz", 9200, 0, "http", "Stopped", "", "", 0),
	}
}

// forwardsFake is kindsFake with the forward probes, the Port Forwarding kind and its view.
func forwardsFake(t testing.TB, view string) *fakeEngine {
	f := kindsFake(t, view)
	registerForwardProbes(t, f)
	p := forwardsProfile(t)
	f.kinds = append(f.kinds, kindInfo{Kind: p.Key, Title: p.Title, APIKind: p.Kind, Group: p.Group, Version: p.Version, Namespaced: true, Columns: p.Columns, Local: p.Local})
	rows := make([]model.Row, 0, 5)
	for _, o := range fixtureForwardObjects() {
		rows = append(rows, model.BuildRow(p, "demo", o))
		ref := model.ObjectRef{Context: "demo", Kind: p.Key, Namespace: o.GetNamespace(), Name: o.GetName()}
		m, err := engine.BuildDetail(p, ref, o, nil, fixedNow)
		if err != nil {
			t.Fatal(err)
		}
		f.details[ref.String()] = m
	}
	slices.SortFunc(rows, func(a, b model.Row) int { return strings.Compare(a.Cells[0].Text, b.Cells[0].Text) })
	f.tables[p.Key] = &engine.TableSnapshot{View: p.Key, Context: "demo", Title: p.Title, Namespaced: true, Columns: p.Columns, Rows: rows,
		Sort: engine.SortSpec{Column: "name"}, Total: len(rows), Revision: 1, Synced: true}
	return f
}

// healthyPodRef is a running fixture pod with two containers, each with an http port.
func healthyPodRef(t testing.TB) model.ObjectRef {
	t.Helper()
	for _, p := range fixturePodSpecs() {
		if p.state == podOK && len(p.containers) == 2 {
			return model.ObjectRef{Context: "demo", Kind: "pods", Namespace: p.ns, Name: p.name}
		}
	}
	t.Fatal("no healthy fixture pod with two containers")
	return model.ObjectRef{}
}

// openPodDrawer opens the drawer of ref and waits for its port actions.
func openPodDrawer(t *testing.T, f *fakeEngine, ref model.ObjectRef, mode ThemeMode) (*shell, fyne.Window, *mainQueue) {
	t.Helper()
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the drawer's ports", func() bool {
		return len(sh.kinds) > 0 && sh.drawer.last != nil && len(forwardButtons(sh)) > 0 && sh.diag.total > 0
	})
	settle(w)
	return sh, w, q
}

func forwardButtons(sh *shell) []*textButton {
	var out []*textButton
	for _, b := range findAll[*textButton](sh.drawer.details) {
		if b.Text == "Forward…" {
			out = append(out, b)
		}
	}
	return out
}

// scrollDrawerToObject scrolls the drawer's details so that o is in view.
func scrollDrawerToObject(sh *shell, w fyne.Window, o fyne.CanvasObject) {
	scrolls := findAll[*container.Scroll](sh.drawer.root)
	if len(scrolls) == 0 {
		return
	}
	s := scrolls[0]
	d := fyne.CurrentApp().Driver()
	y := d.AbsolutePositionForObject(o).Y - d.AbsolutePositionForObject(s.Content).Y
	s.Offset = fyne.NewPos(0, fyne.Max(0, y-s.Size().Height/3))
	s.Refresh()
	settle(w)
}

// dialogTexts returns the texts of the top overlay (the open dialog).
func dialogTexts(w fyne.Window) []string {
	top := w.Canvas().Overlays().Top()
	if top == nil {
		return nil
	}
	var out []string
	for _, t := range findAll[*canvas.Text](top) {
		if t.Text != "" {
			out = append(out, t.Text)
		}
	}
	for _, c := range checksIn(top) {
		out = append(out, "["+c.Text+"]")
	}
	return out
}

// Forward… beside a container port opens the captured dialog; Start forwards with the settings
// and opens the browser on the forward's URL.
func TestForwardDialogFromPodDrawer(t *testing.T) {
	browser := recordBrowser(t)
	f := forwardsFake(t, "pods")
	ref := healthyPodRef(t)
	sh, w, q := openPodDrawer(t, f, ref, ThemeDark)
	buttons := forwardButtons(sh)
	if len(buttons) != 2 {
		t.Fatalf("%d Forward… buttons, want one per container port (2)", len(buttons))
	}
	test.Tap(buttons[0])
	q.until(t, "the dialog", func() bool { return w.Canvas().Overlays().Top() != nil })
	texts := strings.Join(dialogTexts(w), "|")
	for _, want := range []string{"Port Forwarding for " + ref.Name, "Local port", "Start", "Cancel", "[https]", "[Open in Browser]"} {
		if !strings.Contains(texts, want) {
			t.Errorf("the dialog has no %q: %s", want, texts)
		}
	}
	top := w.Canvas().Overlays().Top()
	entries := entriesIn(top)
	checks := checksIn(top)
	if len(entries) != 1 || entries[0].PlaceHolder != "Random" || entries[0].Text != "" || len(checks) != 2 || checks[0].Checked || !checks[1].Checked {
		t.Fatalf("the dialog's fields: %d entries, checks %v", len(entries), checks)
	}
	var start *widget.Button
	for _, b := range findAll[*widget.Button](top) {
		if b.Text == "Start" {
			start = b
		}
	}
	test.Type(entries[0], "70000")
	entries[0].FocusLost() // Fyne reports a field's error once it loses focus
	if !start.Disabled() {
		t.Error("Start is enabled for local port 70000")
	}
	entries[0].SetText("")
	test.Type(entries[0], "8080")
	entries[0].FocusLost()
	if start.Disabled() {
		t.Fatal("Start is disabled for local port 8080")
	}
	test.Tap(start)
	q.until(t, "the forward and the browser", func() bool { return len(f.callsOf("pod.portForward")) == 1 && len(browser.opened()) == 1 })
	var p probeForward
	if err := json.Unmarshal(f.callsOf("pod.portForward")[0].Params, &p); err != nil {
		t.Fatal(err)
	}
	if p.Ref.Name != ref.Name || p.Container != "app" || p.Port != 8080 || p.LocalPort != 8080 || p.HTTPS || !p.OpenInBrowser {
		t.Errorf("dispatched %+v", p)
	}
	if got := browser.opened(); got[0] != forwardURLFixture {
		t.Errorf("opened %v", got)
	}
	if f.callsOf("pod.portForward")[0].Origin != engine.OriginGUI {
		t.Error("not dispatched from the GUI")
	}
}

// A container port's link forwards to a free port at once and opens the browser.
func TestForwardPortLink(t *testing.T) {
	browser := recordBrowser(t)
	f := forwardsFake(t, "pods")
	sh, _, q := openPodDrawer(t, f, healthyPodRef(t), ThemeDark)
	var link *linkText
	for _, l := range findAll[*linkText](sh.drawer.details) {
		if strings.HasPrefix(l.text, "http: ") {
			link = l
			break
		}
	}
	if link == nil {
		t.Fatal("the port is not a link")
	}
	test.Tap(link)
	q.until(t, "the forward and the browser", func() bool { return len(f.callsOf("pod.portForward")) == 1 && len(browser.opened()) == 1 })
	var p probeForward
	_ = json.Unmarshal(f.callsOf("pod.portForward")[0].Params, &p)
	if p.LocalPort != 0 || !p.OpenInBrowser || p.Port == 0 {
		t.Errorf("dispatched %+v", p)
	}
}

// forwardedPodFake is forwardsFake whose healthy pod has a forward of its first port (8080, the
// app container's) on 127.0.0.1:54321, in phase, as the session's drawer model shows it.
func forwardedPodFake(t *testing.T, phase string) (*fakeEngine, model.ObjectRef) {
	t.Helper()
	f := forwardsFake(t, "pods")
	ref := healthyPodRef(t)
	fwd := fixtureForwardObjects()[0].DeepCopy()
	fwd.SetNamespace(ref.Namespace)
	fwd.SetName("pod-" + ref.Name + "-8080")
	_ = unstructured.SetNestedField(fwd.Object, ref.Name, "spec", "name")
	_ = unstructured.SetNestedField(fwd.Object, phase, "status", "phase")
	pods := fixturePodObjects()
	var pod *unstructured.Unstructured
	for _, o := range pods {
		if o.GetNamespace() == ref.Namespace && o.GetName() == ref.Name {
			pod = o
		}
	}
	p, _ := kinds.ByKey("pods")
	m, err := engine.BuildDetailWith(p, ref, pod, fixtureEventObjects(), fixedNow, fixtureRelated{"pods": pods, kindPortForwards: {fwd}})
	if err != nil {
		t.Fatal(err)
	}
	f.details[ref.String()] = m
	return f, ref
}

// A forwarded port shows its forward: the local address, selectable and copyable, and a stop icon
// in place of Forward…; the port opens the forward in the browser instead of forwarding again, and
// the icon stops it. The other container's port, not forwarded, keeps its Forward….
func TestForwardedPortInPodDrawer(t *testing.T) {
	browser := recordBrowser(t)
	f, ref := forwardedPodFake(t, "Active")
	sh, _, q := openPodDrawer(t, f, ref, ThemeDark)
	if n := len(forwardButtons(sh)); n != 1 {
		t.Errorf("%d Forward… buttons, want the unforwarded port's only", n)
	}
	var address *selLine
	for _, l := range findAll[*selLine](sh.drawer.details) {
		if l.line.text == "127.0.0.1:54321" {
			address = l
		}
	}
	if address == nil {
		t.Fatal("the forwarded port shows no local address")
	}
	if address.hook == nil || address.hook.text != "127.0.0.1:54321" {
		t.Errorf("the address's copy hook %+v", address.hook)
	}
	var stop *tipButton
	for _, b := range findAll[*tipButton](sh.drawer.details) {
		if b.tip == "Stop forwarding" {
			stop = b
		}
	}
	if stop == nil {
		t.Fatal("the forwarded port has no stop icon")
	}
	// The address stands between the port and the icon, whole.
	d := fyne.CurrentApp().Driver()
	ax, sx := d.AbsolutePositionForObject(address).X, d.AbsolutePositionForObject(stop).X
	if want := fyne.MeasureText("127.0.0.1:54321", theme.TextSize(), fyne.TextStyle{}).Width; ax >= sx || address.Size().Width < want {
		t.Errorf("address at x %v width %v (text %v), stop at x %v", ax, address.Size().Width, want, sx)
	}

	var link *linkText
	for _, l := range findAll[*linkText](sh.drawer.details) {
		if l.text == "http: 8080/TCP" {
			link = l
		}
	}
	if link == nil {
		t.Fatal("the forwarded port is not a link")
	}
	test.Tap(link)
	q.until(t, "the forward opened", func() bool { return len(f.callsOf("portforward.open")) == 1 && len(browser.opened()) == 1 })
	test.Tap(stop)
	q.until(t, "the forward stopped", func() bool { return len(f.callsOf("portforward.stop")) == 1 })
	for _, id := range []string{"portforward.open", "portforward.stop"} {
		var p probeRef
		if err := json.Unmarshal(f.callsOf(id)[0].Params, &p); err != nil || p.Ref.Kind != kindPortForwards || p.Ref.Name != "pod-"+ref.Name+"-8080" {
			t.Errorf("%s dispatched %s: %v", id, f.callsOf(id)[0].Params, err)
		}
	}
	if n := len(f.callsOf("pod.portForward")); n != 0 {
		t.Errorf("the forwarded port forwarded again (%d)", n)
	}
}

// The Port Forwarding view is in the sidebar's Network group after Gateway API, and its row menu
// is Lens's: Open, Edit, Stop, Delete. Open opens the browser; Edit opens the forward dialog with
// the forward's settings.
func TestPortForwardingView(t *testing.T) {
	browser := recordBrowser(t)
	f := forwardsFake(t, kindPortForwards)
	sh, w, q := settledShell(t, f, kindPortForwards)
	network := sh.side.children[groupPrefix+"network"]
	if len(network) == 0 || network[len(network)-1] != kindPortForwards {
		t.Fatalf("network group %v: Port Forwarding is not last", network)
	}
	row := rowOf(t, f, kindPortForwards, "pod-checkout-api-7d9f8c6b5-bkmqx-8080")
	m := openMenu(t, sh, q, row)
	if got := strings.Join(labels(m), ", "); got != "Show Details, Open, Edit, Stop, Delete" {
		t.Errorf("row menu %s", got)
	}
	sh.view.menu.Hide()
	item(t, m, "Open").Action()
	q.until(t, "the browser", func() bool { return len(browser.opened()) == 1 })

	m = openMenu(t, sh, q, row)
	sh.view.menu.Hide()
	item(t, m, "Edit").Action()
	q.until(t, "the dialog", func() bool { return w.Canvas().Overlays().Top() != nil })
	top := w.Canvas().Overlays().Top()
	entries, checks := entriesIn(top), checksIn(top)
	if len(entries) != 1 || entries[0].Text != "9443" || len(checks) != 2 || !checks[0].Checked || checks[1].Checked {
		t.Fatalf("Edit's dialog does not start from the forward's settings: entries %v checks %v", entries, checks)
	}
	if texts := strings.Join(dialogTexts(w), "|"); !strings.Contains(texts, "Port Forwarding for checkout-api-7d9f8c6b5-bkmqx") {
		t.Errorf("Edit's title: %s", texts)
	}
	for _, b := range findAll[*widget.Button](top) {
		if b.Text == "Start" {
			test.Tap(b)
		}
	}
	q.until(t, "portforward.edit", func() bool { return len(f.callsOf("portforward.edit")) == 1 })
	var p probeForwardEdit
	_ = json.Unmarshal(f.callsOf("portforward.edit")[0].Params, &p)
	if p.LocalPort == nil || *p.LocalPort != 9443 || p.HTTPS == nil || !*p.HTTPS || p.OpenInBrowser == nil || *p.OpenInBrowser {
		t.Errorf("edit dispatched %+v", p)
	}
	if len(browser.opened()) != 1 {
		t.Error("Edit opened the browser although the forward does not ask for it")
	}
}

// Only a forward's own address is opened in the browser.
func TestOpenForwardURLOnlyLocal(t *testing.T) {
	browser := recordBrowser(t)
	f := forwardsFake(t, "pods")
	sh, _, _ := newTestShell(t, f, Options{})
	for _, u := range []string{"https://example.com/", "file:///etc/passwd", "http://localhost.example.com:80/", "javascript:alert(1)", "http://localhost:54321"} {
		sh.openForwardURL(u)
	}
	sh.openForwardURL("https://127.0.0.1:9443")
	if got := browser.opened(); len(got) != 1 || got[0] != "https://127.0.0.1:9443" {
		t.Errorf("opened %v", got)
	}
}

func TestParseLocalPort(t *testing.T) {
	for in, want := range map[string]int{"": 0, " 8080 ": 8080, "65535": 65535, "0": -1, "65536": -1, "http": -1, "-1": -1} {
		got, err := parseLocalPort(in)
		if (want < 0) != (err != nil) || (want >= 0 && int(got) != want) {
			t.Errorf("parseLocalPort(%q) = %d, %v", in, got, err)
		}
	}
}

// renderForwardDialog opens the forward dialog from a pod's port, over the dimmed window.
func renderForwardDialog(t *testing.T, mode ThemeMode, name string) {
	f := forwardsFake(t, "pods")
	sh, w, q := openPodDrawer(t, f, healthyPodRef(t), mode)
	test.Tap(forwardButtons(sh)[0])
	q.until(t, "the dialog", func() bool { return w.Canvas().Overlays().Top() != nil })
	settle(w)
	savePNG(t, w, name)
}

func TestRenderForwardDialogDark(t *testing.T) {
	renderForwardDialog(t, ThemeDark, "forward_dialog_dark.png")
}

// renderPodDrawerPorts shows a pod's containers in the drawer: each port a link with Forward…
// beside it.
func renderPodDrawerPorts(t *testing.T, mode ThemeMode, name string) {
	f := forwardsFake(t, "pods")
	sh, w, _ := openPodDrawer(t, f, healthyPodRef(t), mode)
	scrollDrawerToObject(sh, w, forwardButtons(sh)[0])
	savePNG(t, w, name)
}

func TestRenderPodDrawerPortsDark(t *testing.T) {
	renderPodDrawerPorts(t, ThemeDark, "pod_drawer_ports_dark.png")
}

// renderPodDrawerForwarded shows a forwarded port in the pod drawer: the port, its local address
// and the stop icon, the other container's port with Forward…; the address in phase's colour.
func renderPodDrawerForwarded(t *testing.T, mode ThemeMode, phase, name string) {
	f, ref := forwardedPodFake(t, phase)
	sh, w, _ := openPodDrawer(t, f, ref, mode)
	scrollDrawerToObject(sh, w, forwardButtons(sh)[0])
	savePNG(t, w, name)
}

func TestRenderPodDrawerForwardedDark(t *testing.T) {
	renderPodDrawerForwarded(t, ThemeDark, "Active", "pod_drawer_forwarded_dark.png")
}
func TestRenderPodDrawerForwardReconnectingDark(t *testing.T) {
	renderPodDrawerForwarded(t, ThemeDark, "Reconnecting", "pod_drawer_forward_reconnecting_dark.png")
}

// renderPortForwardingView shows the view with forwards in each state, and with drawer the drawer
// of a service's forward.
func renderPortForwardingView(t *testing.T, mode ThemeMode, drawer bool, name string) {
	f := forwardsFake(t, kindPortForwards)
	if drawer {
		ref := model.ObjectRef{Context: "demo", Kind: kindPortForwards, Namespace: "storefront", Name: "service-storefront-443"}
		f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	}
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the view", func() bool {
		return len(sh.kinds) > 0 && sh.view.snap != nil && sh.view.snap.View == kindPortForwards && sh.diag.total > 0 &&
			(!drawer || sh.drawer.last != nil && len(sh.drawer.actions.Objects) > 0)
	})
	settle(w)
	savePNG(t, w, name)
}

func TestRenderPortForwardingViewDark(t *testing.T) {
	renderPortForwardingView(t, ThemeDark, false, "portforwards_view_dark.png")
}
func TestRenderPortForwardDrawerDark(t *testing.T) {
	renderPortForwardingView(t, ThemeDark, true, "portforward_drawer_dark.png")
}
