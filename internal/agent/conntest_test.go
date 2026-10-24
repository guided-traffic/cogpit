package agent

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
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
		if got := states(steps); got != "dns=pass tcp=pass http=pass auth=pass tools=pass" {
			t.Fatalf("%s (%+v)", got, steps)
		}
		if !strings.Contains(steps[2].Detail, "HTTP 200, plain http") || !strings.Contains(steps[3].Detail, "1 models, no key needed") ||
			!strings.Contains(steps[4].Detail, "test-model called get_time") {
			t.Errorf("details %+v", steps)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(reports) != 11 || states(reports[0]) != "dns=pending tcp=pending http=pending auth=pending tools=pending" ||
			states(reports[1]) != "dns=running tcp=pending http=pending auth=pending tools=pending" {
			t.Errorf("%d reports, first %s", len(reports), states(reports[0]))
		}
	})

	t.Run("auth", func(t *testing.T) {
		h.llm.RequireKey("right-key-123")
		defer h.llm.RequireKey("")
		steps := h.a.TestConnection(context.Background(), conn, nil, nil)
		if got := states(steps); got != "dns=pass tcp=pass http=pass auth=fail tools=notReached" || !strings.Contains(steps[2].Detail, "HTTP 401") ||
			!strings.Contains(steps[3].Detail, "wants an API key") {
			t.Errorf("%s %+v", got, steps)
		}
		wrong := "wrong-key-456"
		steps = h.a.TestConnection(context.Background(), conn, &wrong, nil)
		if got := states(steps); got != "dns=pass tcp=pass http=pass auth=fail tools=notReached" || !strings.Contains(steps[3].Detail, "refused the API key") ||
			strings.Contains(steps[3].Detail, wrong) {
			t.Errorf("%s %+v", got, steps)
		}
		right := "right-key-123"
		h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{{Name: "get_time", Args: `{}`}}})
		if got := states(h.a.TestConnection(context.Background(), conn, &right, nil)); got != "dns=pass tcp=pass http=pass auth=pass tools=pass" {
			t.Errorf("with the right key: %s", got)
		}
	})

	t.Run("tools", func(t *testing.T) {
		h.llm.Script(fakellm.Reply{Text: []string{"It is noon."}})
		steps := h.a.TestConnection(context.Background(), conn, nil, nil)
		if got := states(steps); got != "dns=pass tcp=pass http=pass auth=pass tools=fail" || !strings.Contains(steps[4].Detail, "without a tool call") {
			t.Errorf("%s %+v", got, steps)
		}
	})

	t.Run("models path", func(t *testing.T) {
		wrongBase := conn
		wrongBase.BaseURL = h.llm.Root + "/openai" // no models list there
		steps := h.a.TestConnection(context.Background(), wrongBase, nil, nil)
		if got := states(steps); got != "dns=pass tcp=pass http=pass auth=fail tools=notReached" || !strings.Contains(steps[3].Detail, "HTTP 404") {
			t.Errorf("%s %+v", got, steps)
		}
	})

	t.Run("tcp", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := l.Addr().String()
		_ = l.Close()
		closed := conn
		closed.BaseURL = "http://" + addr + "/v1"
		steps := h.a.TestConnection(context.Background(), closed, nil, nil)
		if got := states(steps); got != "dns=pass tcp=fail http=notReached auth=notReached tools=notReached" || !strings.Contains(steps[1].Detail, addr) {
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
		if got := states(steps); got != "dns=pass tcp=pass http=pass auth=pass tools=pass" {
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
		if got := states(steps); got != "dns=pass tcp=fail http=notReached auth=notReached tools=notReached" || steps[1].Detail != want {
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
		if got := states(steps); got != "dns=fail tcp=notReached http=notReached auth=notReached tools=notReached" ||
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
			if got := states(steps); got != "dns=pass tcp=pass http=pass auth=pass tools=pass" || steps[0].Detail != want[0] || steps[1].Detail != want[1] {
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
		if got := states(steps); got != "dns=fail tcp=notReached http=notReached auth=notReached tools=notReached" ||
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
		if got := states(steps); got != "dns=fail tcp=notReached http=notReached auth=notReached tools=notReached" ||
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
// at least two seconds while that much is left, as Go's dialer shares it.
func TestConnectionTestAddressShare(t *testing.T) {
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
		if got := time.Until(deadline); got > tc.want || got < tc.want-200*time.Millisecond {
			t.Errorf("%s for %d addresses: %s each, want %s", tc.bound, tc.left, got, tc.want)
		}
		dcancel()
		cancel()
	}
}
