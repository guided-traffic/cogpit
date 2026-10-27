package agent

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/testkit"
	"github.com/guided-traffic/cogpit/internal/testkit/fakellm"
)

// states lists the steps' states.
func states(steps []engine.ChatTestStep) string {
	var out []string
	for _, s := range steps {
		out = append(out, string(s.Layer)+"="+string(s.State))
	}
	return strings.Join(out, " ")
}

// TestConnectionTestLayers: each layer passes or fails on its own, a failure leaves the later ones
// not reached, progress is reported after every change, and no request carries cluster data.
func TestConnectionTestLayers(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{})
	conn := engine.ChatConnection{Name: "t", Kind: engine.ChatLMStudio, BaseURL: h.llm.Base, Model: "test-model"}

	t.Run("pass", func(t *testing.T) {
		h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{{Name: "get_time", Args: `{}`}}})
		var mu sync.Mutex
		var reports [][]engine.ChatTestStep
		steps := h.a.TestConnection(context.Background(), conn, nil, func(s []engine.ChatTestStep) {
			mu.Lock()
			reports = append(reports, s)
			mu.Unlock()
		})
		if got := states(steps); got != "dns=pass tcp=pass http=pass auth=pass models=pass tools=pass" {
			t.Fatalf("%s (%+v)", got, steps)
		}
		if !strings.HasPrefix(steps[2].Detail, "HTTP, no TLS; GET "+h.llm.Base+"/models at 127.0.0.1: HTTP 200") || !strings.Contains(steps[3].Detail, "answers without a key") ||
			!strings.Contains(steps[4].Detail, `1 model found; "test-model" among them`) || !slices.Equal(steps[4].Models, []string{"test-model"}) ||
			!strings.Contains(steps[5].Detail, "test-model called get_time") {
			t.Errorf("details %+v", steps)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(reports) != 13 || states(reports[0]) != "dns=pending tcp=pending http=pending auth=pending models=pending tools=pending" ||
			states(reports[1]) != "dns=running tcp=pending http=pending auth=pending models=pending tools=pending" {
			t.Errorf("%d reports, first %s", len(reports), states(reports[0]))
		}
	})

	t.Run("auth", func(t *testing.T) {
		h.llm.RequireKey("right-key-123")
		defer h.llm.RequireKey("")
		steps := h.a.TestConnection(context.Background(), conn, nil, nil)
		if got := states(steps); got != "dns=pass tcp=pass http=pass auth=fail models=notReached tools=notReached" || !strings.Contains(steps[2].Detail, "HTTP 401") ||
			!strings.Contains(steps[3].Detail, "the endpoint wants a key (HTTP 401 without one); none is given") {
			t.Errorf("%s %+v", got, steps)
		}
		wrong := "wrong-key-456"
		steps = h.a.TestConnection(context.Background(), conn, &wrong, nil)
		if got := states(steps); got != "dns=pass tcp=pass http=pass auth=fail models=notReached tools=notReached" || !strings.Contains(steps[3].Detail, "refused the API key") ||
			strings.Contains(steps[3].Detail, wrong) {
			t.Errorf("%s %+v", got, steps)
		}
		right := "right-key-123"
		h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{{Name: "get_time", Args: `{}`}}})
		if got := states(h.a.TestConnection(context.Background(), conn, &right, nil)); got != "dns=pass tcp=pass http=pass auth=pass models=pass tools=pass" {
			t.Errorf("with the right key: %s", got)
		}
	})

	t.Run("tools", func(t *testing.T) {
		h.llm.Script(fakellm.Reply{Text: []string{"It is noon."}})
		steps := h.a.TestConnection(context.Background(), conn, nil, nil)
		if got := states(steps); got != "dns=pass tcp=pass http=pass auth=pass models=pass tools=fail" || !strings.Contains(steps[5].Detail, "without a tool call") {
			t.Errorf("%s %+v", got, steps)
		}
	})

	t.Run("models path", func(t *testing.T) {
		wrongBase := conn
		wrongBase.BaseURL = h.llm.Root + "/openai" // no models list there
		steps := h.a.TestConnection(context.Background(), wrongBase, nil, nil)
		if got := states(steps); got != "dns=pass tcp=pass http=pass auth=fail models=notReached tools=notReached" || !strings.Contains(steps[3].Detail, "HTTP 404") {
			t.Errorf("%s %+v", got, steps)
		}
	})

	t.Run("tcp", func(t *testing.T) {
		addr := testkit.RefusedAddr
		closed := conn
		closed.BaseURL = "http://" + addr + "/v1"
		steps := h.a.TestConnection(context.Background(), closed, nil, nil)
		if got := states(steps); got != "dns=pass tcp=fail http=notReached auth=notReached models=notReached tools=notReached" || !strings.Contains(steps[1].Detail, addr) {
			t.Errorf("%s %+v", got, steps)
		}
	})

	for _, r := range h.llm.Requests() {
		for _, data := range []string{"shop", "web-0", secretValue, `"context"`} {
			if strings.Contains(string(r.Body), data) {
				t.Errorf("the connection test sent %q", data)
			}
		}
	}
}

// TestModelsAndEndpoints: the models list is read from each kind's path and sorted.
func TestModelsAndEndpoints(t *testing.T) {
	for _, tc := range []struct {
		kind               engine.ChatConnectionKind
		base, chat, models string
	}{
		{engine.ChatLMStudio, "http://h:1234/v1/", "http://h:1234/v1/chat/completions", "http://h:1234/v1/models"},
		{engine.ChatLiteLLM, "https://llm.example", "https://llm.example/chat/completions", "https://llm.example/models"},
		{engine.ChatLiteLLM, "https://llm.example/v1", "https://llm.example/v1/chat/completions", "https://llm.example/v1/models"},
		{engine.ChatOpenWebUI, "https://webui.example", "https://webui.example/api/chat/completions", "https://webui.example/api/models"},
		{engine.ChatOpenWebUI, "https://webui.example/api/", "https://webui.example/api/chat/completions", "https://webui.example/api/models"},
		{engine.ChatOpenAI, "https://api.example/v1", "https://api.example/v1/chat/completions", "https://api.example/v1/models"},
	} {
		ep, err := endpointFor(tc.kind, tc.base)
		if err != nil || ep.chat != tc.chat || ep.models != tc.models {
			t.Errorf("%s %s: %+v %v", tc.kind, tc.base, ep, err)
		}
	}
	for _, bad := range []string{"ftp://h/v1", "http://user:pw@h/v1", "http://h/v1?x=1", "h:1234", "", "http:///v1"} {
		if _, err := endpointFor(engine.ChatOpenAI, bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}

	h := newHarness(t, engine.ChatLimits{})
	h.llm.Models(0, "zeta", "alpha", "alpha")
	models, err := h.a.Models(context.Background(), h.conn.ID)
	if err != nil || strings.Join(models, ",") != "alpha,zeta" {
		t.Errorf("models %v %v", models, err)
	}
	if got, err := parseModels([]byte(`[{"name":"b"},{"id":"a"}]`)); err != nil || strings.Join(got, ",") != "a,b" {
		t.Errorf("a bare array: %v %v", got, err)
	}
}

// fakeResolver routes the DNS layer's lookups to names (an address list per name; a name not in it
// is not known) for the test, so that no test asks a real resolver. It returns the names asked.
func fakeResolver(t *testing.T, names map[string][]string) func() []string {
	t.Helper()
	var mu sync.Mutex
	var asked []string
	old := lookupNetIP
	lookupNetIP = func(_ context.Context, network, host string) ([]netip.Addr, error) {
		mu.Lock()
		asked = append(asked, host)
		mu.Unlock()
		if network != "ip" {
			t.Errorf("looked up %s as %q", host, network)
		}
		ips, ok := names[host]
		if !ok {
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
		var out []netip.Addr
		for _, ip := range ips {
			out = append(out, netip.MustParseAddr(ip))
		}
		return out, nil
	}
	t.Cleanup(func() { lookupNetIP = old })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(asked)
	}
}

// fakeDialer routes the TCP layer's dials: an address in refuse is refused, any other reaches the
// fake endpoint at to. It returns the addresses dialled, in order.
func fakeDialer(t *testing.T, to string, refuse ...string) func() []string {
	t.Helper()
	var mu sync.Mutex
	var dialled []string
	old := dialTCP
	dialTCP = func(ctx context.Context, network, addr string) (net.Conn, error) {
		mu.Lock()
		dialled = append(dialled, addr)
		mu.Unlock()
		if slices.Contains(refuse, addr) {
			return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("connect: connection refused")}
		}
		return (&net.Dialer{}).DialContext(ctx, network, to)
	}
	t.Cleanup(func() { dialTCP = old })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(dialled)
	}
}

// TestConnectionTestDNS (the owner's VW8, 2026-10-23): the DNS layer comes before TCP, resolves the
// endpoint's name and lists its addresses (IPv4 and IPv6, duplicates once, more than four counted),
// a name that is not known fails it; an IP address and localhost pass at once without a lookup; the
// TCP layer dials the addresses the DNS layer found, in its order, and says which one answered.
// With a proxy configured, DNS and TCP check the proxy (its password never shown) and the endpoint's
// name is left to the proxy. No real resolver or network is asked.
func TestConnectionTestDNS(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{})
	fake, err := url.Parse(h.llm.Root)
	if err != nil {
		t.Fatal(err)
	}
	asked := fakeResolver(t, map[string][]string{
		"llm.example.test":   {"203.0.113.7", "2001:db8::7", "203.0.113.7", "::ffff:203.0.113.8"},
		"wide.example.test":  {"192.0.2.1", "192.0.2.2", "192.0.2.3", "192.0.2.4", "192.0.2.5", "192.0.2.6"},
		"proxy.example.test": {"192.0.2.99"},
	})
	var proxy func(*http.Request) (*url.URL, error)
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, fake.Host)
	}
	tr.Proxy = func(r *http.Request) (*url.URL, error) {
		if proxy == nil {
			return nil, nil
		}
		return proxy(r)
	}
	a := New(Options{Session: h.s, Keys: NewMemoryKeyStore(), HTTPClient: &http.Client{Transport: tr, Timeout: 10 * time.Second}})
	test := func(base string) []engine.ChatTestStep {
		t.Helper()
		h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{{Name: "get_time", Args: `{}`}}})
		return a.TestConnection(context.Background(), engine.ChatConnection{Name: "dns", Kind: engine.ChatLMStudio, BaseURL: base, Model: "test-model"}, nil, nil)
	}

	t.Run("resolved, the second address answers", func(t *testing.T) {
		dialled := fakeDialer(t, fake.Host, "203.0.113.7:1234")
		steps := test("http://llm.example.test:1234/v1")
		if got := states(steps); got != "dns=pass tcp=pass http=pass auth=pass models=pass tools=pass" {
			t.Fatalf("%s %+v", got, steps)
		}
		if want := "llm.example.test: 203.0.113.7, 2001:db8::7, 203.0.113.8"; steps[0].Detail != want {
			t.Errorf("DNS: %q, want %q", steps[0].Detail, want)
		}
		if want := "llm.example.test:1234 answers at 2001:db8::7 (203.0.113.7 connection refused)"; steps[1].Detail != want {
			t.Errorf("TCP: %q, want %q", steps[1].Detail, want)
		}
		if got := strings.Join(dialled(), " "); got != "203.0.113.7:1234 [2001:db8::7]:1234" {
			t.Errorf("dialled %s", got)
		}
	})

	t.Run("every address refused", func(t *testing.T) {
		fakeDialer(t, fake.Host, "203.0.113.7:1234", "[2001:db8::7]:1234", "203.0.113.8:1234")
		steps := test("http://llm.example.test:1234/v1")
		want := "llm.example.test:1234: 203.0.113.7 connection refused; 2001:db8::7 connection refused; 203.0.113.8 connection refused"
		if got := states(steps); got != "dns=pass tcp=fail http=notReached auth=notReached models=notReached tools=notReached" || steps[1].Detail != want {
			t.Errorf("%s %q", got, steps[1].Detail)
		}
	})

	t.Run("more than four addresses", func(t *testing.T) {
		fakeDialer(t, fake.Host)
		steps := test("http://wide.example.test:1234/v1")
		if want := "wide.example.test: 192.0.2.1, 192.0.2.2, 192.0.2.3, 192.0.2.4 and 2 more"; steps[0].Detail != want {
			t.Errorf("DNS: %q", steps[0].Detail)
		}
	})

	t.Run("not known", func(t *testing.T) {
		dialled := fakeDialer(t, fake.Host)
		steps := test("https://nowhere.example.test/v1")
		if got := states(steps); got != "dns=fail tcp=notReached http=notReached auth=notReached models=notReached tools=notReached" ||
			steps[0].Detail != "nowhere.example.test: the name is not known" {
			t.Errorf("%s %q", got, steps[0].Detail)
		}
		if n := len(dialled()); n != 0 {
			t.Errorf("%d dials after a failed lookup", n)
		}
	})

	t.Run("an address and localhost need no lookup", func(t *testing.T) {
		before := len(asked())
		for base, want := range map[string][2]string{
			"http://192.0.2.10:1234/v1": {"192.0.2.10 is an address: nothing to resolve", "192.0.2.10:1234 answers"},
			"http://[fd00::3]:1234/v1":  {"fd00::3 is an address: nothing to resolve", "[fd00::3]:1234 answers"},
			"http://localhost:1234/v1":  {"localhost is this computer (127.0.0.1, ::1): nothing to resolve", "localhost:1234 answers at 127.0.0.1"},
			"http://LocalHost.:1234/v1": {"LocalHost. is this computer (127.0.0.1, ::1): nothing to resolve", "LocalHost.:1234 answers at 127.0.0.1"},
		} {
			fakeDialer(t, fake.Host)
			steps := test(base)
			if got := states(steps); got != "dns=pass tcp=pass http=pass auth=pass models=pass tools=pass" || steps[0].Detail != want[0] || steps[1].Detail != want[1] {
				t.Errorf("%s: %s %q %q", base, got, steps[0].Detail, steps[1].Detail)
			}
		}
		if got := asked()[before:]; len(got) != 0 {
			t.Errorf("looked up %v", got)
		}
	})

	t.Run("a proxy", func(t *testing.T) {
		proxy = func(*http.Request) (*url.URL, error) {
			return url.Parse("http://me:proxy-secret-pw@proxy.example.test:3128")
		}
		defer func() { proxy = nil }()
		before := len(asked())
		dialled := fakeDialer(t, fake.Host)
		steps := test("https://llm.example.test/v1")
		if got := states(steps)[:len("dns=pass tcp=pass")]; got != "dns=pass tcp=pass" {
			t.Errorf("%s %+v", states(steps), steps)
		}
		if want := "the proxy proxy.example.test: 192.0.2.99; the proxy resolves llm.example.test"; steps[0].Detail != want {
			t.Errorf("DNS: %q, want %q", steps[0].Detail, want)
		}
		if want := "the proxy proxy.example.test:3128 answers at 192.0.2.99"; steps[1].Detail != want {
			t.Errorf("TCP: %q, want %q", steps[1].Detail, want)
		}
		if got := asked()[before:]; strings.Join(got, " ") != "proxy.example.test" {
			t.Errorf("looked up %v, want only the proxy", got)
		}
		if got := dialled(); len(got) != 1 || got[0] != "192.0.2.99:3128" {
			t.Errorf("dialled %v", got)
		}
		for _, s := range steps {
			if strings.Contains(s.Detail, "proxy-secret-pw") || strings.Contains(s.Detail, "me:") {
				t.Errorf("a step shows the proxy's credentials: %q", s.Detail)
			}
		}

		proxy = func(*http.Request) (*url.URL, error) {
			return nil, errors.New(`invalid proxy address "http://me:proxy-secret-pw@%zz": bad`)
		}
		steps = test("https://llm.example.test/v1")
		if got := states(steps); got != "dns=fail tcp=notReached http=notReached auth=notReached models=notReached tools=notReached" ||
			!strings.Contains(steps[0].Detail, "not a valid URL") || strings.Contains(steps[0].Detail, "proxy-secret-pw") {
			t.Errorf("%s %q", got, steps[0].Detail)
		}
	})

	t.Run("a resolver that does not answer", func(t *testing.T) {
		old, oldBound := lookupNetIP, testDNSTimeout
		lookupNetIP = func(ctx context.Context, _, _ string) ([]netip.Addr, error) {
			<-ctx.Done()
			return nil, &net.DNSError{Err: "i/o timeout", IsTimeout: true}
		}
		testDNSTimeout = 50 * time.Millisecond
		defer func() { lookupNetIP, testDNSTimeout = old, oldBound }()
		steps := test("https://slow.example.test/v1")
		if got := states(steps); got != "dns=fail tcp=notReached http=notReached auth=notReached models=notReached tools=notReached" ||
			steps[0].Detail != "no answer within 50ms" {
			t.Errorf("%s %q", got, steps[0].Detail)
		}
		lookupNetIP = func(context.Context, string, string) ([]netip.Addr, error) {
			return nil, &net.DNSError{Err: "i/o timeout", Name: "slow.example.test", IsTimeout: true}
		}
		if steps := test("https://slow.example.test/v1"); steps[0].Detail != "slow.example.test: the resolver did not answer" {
			t.Errorf("a resolver's timeout: %q", steps[0].Detail)
		}
	})
}

// TestConnectionTestAddressShare: each address of the TCP layer gets an equal share of the time left,
// at least two seconds while that much is left, as Go's dialer shares it. The bubble's clock
// (testing/synctest) stands still while the test runs, so the share is exact.
func TestConnectionTestAddressShare(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, tc := range []struct {
			bound time.Duration
			left  int
			want  time.Duration
		}{
			{5 * time.Second, 1, 5 * time.Second},
			{5 * time.Second, 2, 2500 * time.Millisecond},
			{5 * time.Second, 4, 2 * time.Second},
			{time.Second, 3, time.Second},
		} {
			ctx, cancel := context.WithTimeout(context.Background(), tc.bound)
			dctx, dcancel := addrContext(ctx, tc.left)
			deadline, _ := dctx.Deadline()
			if got := time.Until(deadline); got != tc.want {
				t.Errorf("%s for %d addresses: %s each, want %s", tc.bound, tc.left, got, tc.want)
			}
			dcancel()
			cancel()
		}
	})
}

// TestConnectionTestAuthAndModels (the owner's request of 2026-10-25): Auth asks without the key
// first, so it says whether the endpoint wants one; Models counts the list, names the chosen model
// and hands the list to the form; an empty list passes with a warning.
func TestConnectionTestAuthAndModels(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{})
	conn := engine.ChatConnection{Name: "t", Kind: engine.ChatLMStudio, BaseURL: h.llm.Base, Model: "qwen"}
	run := func(key *string) []engine.ChatTestStep {
		h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{{Name: "get_time", Args: `{}`}}})
		return h.a.TestConnection(context.Background(), conn, key, nil)
	}
	key := "right-key-123"

	h.llm.Models(0, "llama", "qwen")
	steps := run(&key)
	if got := states(steps); got != "dns=pass tcp=pass http=pass auth=pass models=pass tools=pass" ||
		steps[3].Detail != "the endpoint answers without a key; the key given is accepted too" ||
		steps[4].Detail != `2 models found; "qwen" among them` || !slices.Equal(steps[4].Models, []string{"llama", "qwen"}) {
		t.Errorf("no key needed: %s %+v", got, steps)
	}

	h.llm.RequireKey(key)
	steps = run(&key)
	if got := states(steps); got != "dns=pass tcp=pass http=pass auth=pass models=pass tools=pass" ||
		steps[3].Detail != "the endpoint wants a key (HTTP 401 without one); the key given is accepted" {
		t.Errorf("a key needed: %s %+v", got, steps)
	}
	h.llm.RequireKey("")

	conn.Model = "mistral"
	steps = run(nil)
	if steps[4].Detail != `2 models found; "mistral" is not among them` || steps[4].Warning != "" {
		t.Errorf("a model not listed: %+v", steps[4])
	}
	conn.Model = ""
	h.llm.Models(0)
	steps = run(nil)
	if steps[4].State != engine.ChatTestPass || steps[4].Detail != "0 models found; none chosen yet" ||
		steps[4].Warning != noModelsWarning || len(steps[4].Models) != 0 {
		t.Errorf("no models: %+v", steps[4])
	}
	if steps[5].State != engine.ChatTestFail || steps[5].Detail != "no model chosen" {
		t.Errorf("tools without a model: %+v", steps[5])
	}
}

// TestModelsOfTheFormsValues (the owner's request of 2026-10-25, a colleague's empty list): the
// models of an unsaved connection, with the key typed, or the stored one of a saved connection
// when none is typed; a key the endpoint wants and none given fails, its key never in the error.
func TestModelsOfTheFormsValues(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{})
	h.llm.Models(0, "llama", "qwen")
	key := "right-key-123"
	h.llm.RequireKey(key)
	unsaved := engine.ChatConnection{Name: "new", Kind: engine.ChatLMStudio, BaseURL: h.llm.Base}
	if got, err := h.a.ModelsOf(context.Background(), unsaved, &key); err != nil || !slices.Equal(got, []string{"llama", "qwen"}) {
		t.Fatalf("typed key: %v, %v", got, err)
	}
	if _, err := h.a.ModelsOf(context.Background(), unsaved, nil); err == nil {
		t.Error("no key: listed")
	}
	wrong := "wrong-key-456"
	if _, err := h.a.ModelsOf(context.Background(), unsaved, &wrong); err == nil || strings.Contains(err.Error(), wrong) {
		t.Errorf("a wrong key: %v", err)
	}
	saved, err := h.a.SaveConnection(unsaved, &key)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := h.a.ModelsOf(context.Background(), saved, nil); err != nil || len(got) != 2 {
		t.Errorf("the stored key: %v, %v", got, err)
	}
}

// asAddr is a connection that reports another remote address: the address of the name the test's
// client stands for, while it reaches a server on this computer.
type asAddr struct {
	net.Conn
	remote net.Addr
}

func (c asAddr) RemoteAddr() net.Addr { return c.remote }

// TestConnectionTestEveryAddress (the owner's case of 2026-10-25): a name with three addresses, one
// of them a proxy with no server behind it that answers 503 with an HTML page. When the chat's
// client reaches that one the HTTP layer fails, names the URL, the address and the page's text,
// and lists the other addresses' answers; when the chat's client reaches a good one the layer passes
// with a warning naming the broken address.
func TestConnectionTestEveryAddress(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{})
	good, err := url.Parse(h.llm.Root)
	if err != nil {
		t.Fatal(err)
	}
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "<html><body><h1>503 Service Unavailable</h1>\nNo server is available to handle this request.\n</body></html>\n")
	}))
	t.Cleanup(broken.Close)
	bad, err := url.Parse(broken.URL)
	if err != nil {
		t.Fatal(err)
	}
	fakeResolver(t, map[string][]string{"llm.three.example": {"192.0.2.11", "192.0.2.12", "192.0.2.13"}})
	route := func(addr string) string {
		if strings.HasPrefix(addr, "192.0.2.11:") {
			return bad.Host
		}
		return good.Host
	}
	old := dialTCP
	dialTCP = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, route(addr))
	}
	t.Cleanup(func() { dialTCP = old })
	chatsAddress := "192.0.2.11:1234"
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		c, err := (&net.Dialer{}).DialContext(ctx, network, route(chatsAddress))
		if err != nil {
			return nil, err
		}
		return asAddr{Conn: c, remote: net.TCPAddrFromAddrPort(netip.MustParseAddrPort(chatsAddress))}, nil
	}
	a := New(Options{Session: h.s, Keys: NewMemoryKeyStore(), HTTPClient: &http.Client{Transport: tr, Timeout: 10 * time.Second}})
	conn := engine.ChatConnection{Name: "three", Kind: engine.ChatLiteLLM, BaseURL: "http://llm.three.example:1234/v1", Model: "test-model"}

	steps := a.TestConnection(context.Background(), conn, nil, nil)
	want := `GET http://llm.three.example:1234/v1/models at 192.0.2.11: HTTP 503 Service Unavailable, the API does not answer there ` +
		`(an HTML page, from a proxy or load balancer in front of the API: "No server is available to handle this request."); ` +
		`the name's other addresses: 192.0.2.12 HTTP 200, 192.0.2.13 HTTP 200. The chat's requests go to 192.0.2.11, ` +
		`the first in Go's order of the name's addresses`
	if got := states(steps); got != "dns=pass tcp=pass http=fail auth=notReached models=notReached tools=notReached" || steps[2].Detail != want {
		t.Fatalf("the chat's address broken: %s\n%q\nwant\n%q", got, steps[2].Detail, want)
	}

	chatsAddress = "192.0.2.12:1234"
	tr.CloseIdleConnections()
	h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{{Name: "get_time", Args: `{}`}}})
	steps = a.TestConnection(context.Background(), conn, nil, nil)
	if got := states(steps); got != "dns=pass tcp=pass http=pass auth=pass models=pass tools=pass" ||
		!strings.Contains(steps[2].Detail, "GET http://llm.three.example:1234/v1/models at 192.0.2.12: HTTP 200; the name's other addresses: 192.0.2.11 HTTP 503 (") ||
		!strings.Contains(steps[2].Warning, "Not every address of the name answers as the API: 192.0.2.11 HTTP 503") ||
		!strings.Contains(steps[2].Warning, "the chat's own go to 192.0.2.12.") {
		t.Fatalf("another address broken: %s %+v", got, steps[2])
	}
}

// TestConnectionTestShowsTheExchange (the owner, 2026-10-26: "WELCHER ENDPUNKT WURDE WIE
// AUFGERUFEN???" after a Tool use step that said only "the endpoint answered HTTP 404 Not Found:
// Not Found"): a failed layer names the method and URL, and carries its last request and answer in
// curl -v's form, the key never in it; a passed layer carries none.
func TestConnectionTestShowsTheExchange(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{})
	conn := engine.ChatConnection{Name: "t", Kind: engine.ChatLMStudio, BaseURL: h.llm.Base, Model: "test-model"}
	chatURL := h.llm.Base + "/chat/completions"

	h.llm.Script(fakellm.Reply{Status: http.StatusNotFound, Body: `{"detail":"Not Found"}`})
	steps := h.a.TestConnection(context.Background(), conn, nil, nil)
	if got := states(steps); got != "dns=pass tcp=pass http=pass auth=pass models=pass tools=fail" {
		t.Fatalf("%s %+v", got, steps)
	}
	for _, s := range steps[:5] {
		if s.Exchange != "" {
			t.Errorf("%s passed with an exchange: %q", s.Layer, s.Exchange)
		}
	}
	tools := steps[5]
	if want := "POST " + chatURL + ": the endpoint answered HTTP 404 Not Found: Not Found. The models list answered at " +
		h.llm.Base + "/models"; !strings.HasPrefix(tools.Detail, want) || !strings.Contains(tools.Detail, `does not know the model "test-model"`) {
		t.Errorf("detail %q, want it to start with %q", tools.Detail, want)
	}
	for _, want := range []string{
		"* connected to 127.0.0.1:",
		"> POST " + chatURL + "\n",
		"> Content-Type: application/json\n",
		"> Accept: text/event-stream, application/json\n",
		"* no Authorization field: no key went\n",
		`"model":"test-model"`,
		`"name":"get_time"`,
		"< HTTP/1.1 404 Not Found\n",
		"< Content-Type: application/json\n",
		"<\n< {\"detail\":\"Not Found\"}\n",
		" to the answer's header",
	} {
		if !strings.Contains(tools.Exchange, want) {
			t.Errorf("the exchange lacks %q:\n%s", want, tools.Exchange)
		}
	}

	key := "sk-right-key-123"
	h.llm.RequireKey(key)
	defer h.llm.RequireKey("")
	wrong := "sk-wrong-key-456"
	steps = h.a.TestConnection(context.Background(), conn, &wrong, nil)
	auth := steps[3]
	if got := states(steps); got != "dns=pass tcp=pass http=pass auth=fail models=notReached tools=notReached" ||
		!strings.Contains(auth.Detail, "GET "+h.llm.Base+"/models: the endpoint refused the API key (HTTP 401 Unauthorized)") {
		t.Errorf("%s %q", got, auth.Detail)
	}
	for _, want := range []string{"> GET " + h.llm.Base + "/models\n", "> Authorization: Bearer •••\n", "< HTTP/1.1 401 Unauthorized\n",
		"* the body not shown: a refusal may quote a part of the key"} {
		if !strings.Contains(auth.Exchange, want) {
			t.Errorf("the auth exchange lacks %q:\n%s", want, auth.Exchange)
		}
	}
	for _, s := range steps {
		if strings.Contains(s.Exchange, wrong) || strings.Contains(s.Detail, wrong) || strings.Contains(s.Exchange, key) {
			t.Errorf("%s shows a key: %q %q", s.Layer, s.Detail, s.Exchange)
		}
	}

	h.llm.RequireKey("")
	h.llm.Script(fakellm.Reply{Text: []string{"It is noon, " + key}})
	steps = h.a.TestConnection(context.Background(), conn, &key, nil)
	tools = steps[5]
	if !strings.Contains(tools.Exchange, "< HTTP/1.1 200 OK\n") || !strings.Contains(tools.Exchange, "It is noon, •••") ||
		strings.Contains(tools.Exchange, key) {
		t.Errorf("an answer without a tool call: %s", tools.Exchange)
	}
}

// TestExchangeText: a body cut at its bound says so, control characters are replaced, a Set-Cookie
// is withheld, and a request that got no answer says why.
func TestExchangeText(t *testing.T) {
	x := &exchange{method: http.MethodGet, url: "https://llm.example/models", addr: "203.0.113.7:443",
		sent: [][]string{{"Host", "llm.example"}, {"Authorization", "Bearer sk-1"}}, proto: "HTTP/1.1", status: "200 OK",
		header: http.Header{"Set-Cookie": {"session=abc"}, "X-Echo": {"sk-1"}}, more: true, took: 3 * time.Millisecond}
	x.answer.WriteString("line\x01one\r\nline two\n")
	got := x.text("sk-1")
	want := strings.Join([]string{
		"* connected to 203.0.113.7:443",
		"> GET https://llm.example/models",
		"> Host: llm.example",
		"> Authorization: Bearer •••",
		"< HTTP/1.1 200 OK",
		"< Set-Cookie: (withheld)",
		"< X-Echo: •••",
		"<",
		"< line�one",
		"< line two",
		"< … (cut after 2048 bytes)",
		"* 3 ms to the answer's header",
	}, "\n")
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	failed := &exchange{method: http.MethodPost, url: "http://h:1/chat/completions", err: errors.New("dial tcp: connection refused"),
		took: 2 * time.Second}
	if got := failed.text(""); !strings.HasSuffix(got, "* no answer after 2.0 s: dial tcp: connection refused") {
		t.Errorf("no answer: %q", got)
	}
}
