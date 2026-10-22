package cluster

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// A connect's stages (D-114): kubeconfig, a credential plugin's login, DNS, TCP, TLS and the API
// server's answer, each with its result, from Connect's own configuration and client-go's own
// first request. No test reaches a network: the servers listen on loopback, and a lookup goes to
// a resolver of the test's own (fakeDNS) over an in-memory pipe.

// stagesConnect connects path's current context with opts' dial, and returns the stages its trace
// reported, the error's stages and the error.
func stagesConnect(t *testing.T, path string, dial func(context.Context, string, string) (net.Conn, error)) (traced []Stage, err error) {
	t.Helper()
	log := &stepLog{}
	c, err := Connect(context.Background(), Options{KubeconfigPaths: []string{path}, Trace: log.add, ConnectTimeout: 5 * time.Second, dial: dial})
	if c != nil {
		t.Cleanup(c.Close)
	}
	st, ok := log.find(StepStages)
	if !ok {
		t.Fatalf("no stages reported:\n%s", log.text())
	}
	if err != nil {
		errStages, cause := StagesOf(err)
		if len(errStages) != len(st.Stages) || cause == "" {
			t.Errorf("the error's stages %+v, cause %q; traced %+v", errStages, cause, st.Stages)
		}
	}
	return st.Stages, err
}

// wantStages checks the stages' names and results in order, and that each text holds its part.
func wantStages(t *testing.T, got []Stage, want ...[3]string) {
	t.Helper()
	for _, g := range got {
		t.Logf("%-10s %-10s %s (%s)", g.Name, g.Result, g.Text, g.Took)
	}
	if len(got) != len(want) {
		t.Fatalf("stages %+v, want %d", got, len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Name != w[0] || g.Result != w[1] || !strings.Contains(g.Text, w[2]) {
			t.Errorf("stage %d: %s %s %q, want %s %s …%q…", i, g.Name, g.Result, g.Text, w[0], w[1], w[2])
		}
		if g.Result == StageNotReached && g.Text != "" {
			t.Errorf("stage %s not reached says %q", g.Name, g.Text)
		}
		if strings.Contains(g.Text, secretToken) {
			t.Errorf("stage %s quotes the token: %q", g.Name, g.Text)
		}
	}
}

// fakeDNS answers A queries for its names with their addresses, and everything else with no
// answer, as the server a PreferGo resolver's Dial reaches: an in-memory pipe, no socket.
type fakeDNS struct {
	names map[string]net.IP // "api.cogpit.test." → 127.0.0.1
	asked atomic.Int32
}

// dial is a dialer whose lookups go to the fake server; a nil fakeDNS fails every lookup.
func (f *fakeDNS) dial() func(context.Context, string, string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 5 * time.Second, Resolver: &net.Resolver{PreferGo: true,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			if f == nil {
				return nil, &net.OpError{Op: "dial", Net: "udp", Err: os.ErrPermission} // no DNS in tests
			}
			client, server := net.Pipe()
			go f.serve(server)
			return client, nil
		}}}
	return d.DialContext
}

// serve answers the queries on one stream: each a two-byte length and the message (the resolver
// frames a stream connection so).
func (f *fakeDNS) serve(c net.Conn) {
	defer func() { _ = c.Close() }()
	for {
		var n [2]byte
		if _, err := io.ReadFull(c, n[:]); err != nil {
			return
		}
		q := make([]byte, int(n[0])<<8|int(n[1]))
		if _, err := io.ReadFull(c, q); err != nil {
			return
		}
		resp := f.answer(q)
		if resp == nil {
			return
		}
		if _, err := c.Write(append([]byte{byte(len(resp) >> 8), byte(len(resp))}, resp...)); err != nil {
			return
		}
	}
}

// answer builds the response to query q: its id and question, and an A record for a known name.
func (f *fakeDNS) answer(q []byte) []byte {
	if len(q) < 12 {
		return nil
	}
	f.asked.Add(1)
	i := 12
	var name strings.Builder
	for i < len(q) && q[i] != 0 {
		l := int(q[i])
		if i+1+l > len(q) {
			return nil
		}
		name.Write(q[i+1 : i+1+l])
		name.WriteByte('.')
		i += l + 1
	}
	end := i + 5 // the root label, the type and the class
	if end > len(q) {
		return nil
	}
	ip := f.names[strings.ToLower(name.String())].To4()
	a := q[i+1] == 0 && q[i+2] == 1 && ip != nil
	resp := append([]byte{q[0], q[1], 0x81, 0x80, 0, 1, 0, 0, 0, 0, 0, 0}, q[12:end]...) // a response, recursion available, no error
	if a {
		resp[7] = 1
		resp = append(resp, 0xc0, 12, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4)
		resp = append(resp, ip...)
	}
	return resp
}

// A connect that works passes every stage: the kubeconfig with its context and file, DNS skipped
// for a server named by its address, TCP to that address and port, TLS checked against the
// kubeconfig's authority, and the API server's answer with its version.
func TestConnectStagesOfAConnectThatWorks(t *testing.T) {
	ca := testkit.NewTestCA(t, "cogpit stages CA")
	srv := testkit.NewFakeAPIServerWithCert(testkit.FakeAPIOptions{Token: secretToken}, ca.Issue(t, "127.0.0.1"))
	defer srv.Close()
	path := writeTLSKubeconfig(t, tlsCluster{server: srv.URL, caData: ca.PEM()})
	got, err := stagesConnect(t, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	addr := strings.TrimPrefix(srv.URL, "https://")
	wantStages(t, got,
		[3]string{StageKubeconfig, StageOK, `context "tls" in ` + path + ": server " + srv.URL + ", login by a bearer token"},
		[3]string{StageDNS, StageSkipped, "127.0.0.1 is an address: no lookup"},
		[3]string{StageTCP, StageOK, addr + " reached"},
		[3]string{StageTLS, StageOK, "with 127.0.0.1, its certificate checked against the kubeconfig's certificate authority"},
		[3]string{StageAPI, StageOK, "GET /version answered: Kubernetes v"})
}

// The server's name is looked up: the stage names the addresses it gave, and TCP the one dialled.
func TestConnectStagesNameTheAddressesOfTheLookup(t *testing.T) {
	ca := testkit.NewTestCA(t, "cogpit stages CA")
	srv := testkit.NewFakeAPIServerWithCert(testkit.FakeAPIOptions{Token: secretToken}, ca.Issue(t, "api.cogpit.test"))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "https://"))
	dns := &fakeDNS{names: map[string]net.IP{"api.cogpit.test.": net.IPv4(127, 0, 0, 1)}}
	path := writeTLSKubeconfig(t, tlsCluster{server: "https://api.cogpit.test:" + port, caData: ca.PEM()})
	got, err := stagesConnect(t, path, dns.dial())
	if err != nil {
		t.Fatal(err)
	}
	if dns.asked.Load() == 0 {
		t.Fatal("the lookup did not reach the test's resolver")
	}
	wantStages(t, got,
		[3]string{StageKubeconfig, StageOK, "server https://api.cogpit.test:" + port},
		[3]string{StageDNS, StageOK, "api.cogpit.test: 127.0.0.1"},
		[3]string{StageTCP, StageOK, "127.0.0.1:" + port + " reached"},
		[3]string{StageTLS, StageOK, "with api.cogpit.test"},
		[3]string{StageAPI, StageOK, "Kubernetes v"})
}

// Each failure is the stage it happened in, with its cause in a sentence; the error's Cause says
// it; every stage after it is not reached.
func TestConnectStagesOfAFailure(t *testing.T) {
	t.Run("client certificate gone", func(t *testing.T) {
		dir := t.TempDir()
		cert, key := filepath.Join(dir, "walk-cert", "client.crt"), filepath.Join(dir, "walk-cert", "client.key")
		kc := "apiVersion: v1\nkind: Config\ncurrent-context: walk-cert-gone\nclusters:\n- name: c\n  cluster: {server: \"https://127.0.0.1:1\"}\n" +
			"users:\n- name: u\n  user:\n    client-certificate: " + cert + "\n    client-key: " + key + "\n" +
			"contexts:\n- name: walk-cert-gone\n  context: {cluster: c, user: u}\n"
		got, err := stagesConnect(t, writeFile(t, "kubeconfig", kc), nil)
		if err == nil {
			t.Fatal("connected without a client certificate")
		}
		want := "the client certificate file " + cert + " does not exist; the client key file " + key + " does not exist"
		wantStages(t, got,
			[3]string{StageKubeconfig, StageFailed, want},
			[3]string{StageDNS, StageNotReached, ""}, [3]string{StageTCP, StageNotReached, ""},
			[3]string{StageTLS, StageNotReached, ""}, [3]string{StageAPI, StageNotReached, ""})
		if _, cause := StagesOf(err); cause != want {
			t.Errorf("the cause %q", cause)
		}
		if !strings.Contains(err.Error(), "invalid kubeconfig: invalid configuration: ") || !strings.Contains(err.Error(), "unable to read client-cert") {
			t.Errorf("the error lost its chain: %v", err)
		}
	})
	t.Run("lookup fails", func(t *testing.T) {
		path := writeFile(t, "kubeconfig", kubeconfigFor("x", "https://api.cogpit.invalid:6443", ""))
		got, err := stagesConnect(t, path, (*fakeDNS)(nil).dial())
		if err == nil {
			t.Fatal("connected")
		}
		wantStages(t, got,
			[3]string{StageKubeconfig, StageOK, `context "x"`},
			[3]string{StageDNS, StageFailed, "api.cogpit.invalid cannot be resolved"},
			[3]string{StageTCP, StageNotReached, ""}, [3]string{StageTLS, StageNotReached, ""}, [3]string{StageAPI, StageNotReached, ""})
	})
	t.Run("port closed", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := l.Addr().String()
		_ = l.Close()
		got, err := stagesConnect(t, writeFile(t, "kubeconfig", kubeconfigFor("x", "https://"+addr, "")), nil)
		if err == nil {
			t.Fatal("connected")
		}
		wantStages(t, got,
			[3]string{StageKubeconfig, StageOK, ""},
			[3]string{StageDNS, StageSkipped, "127.0.0.1 is an address"},
			[3]string{StageTCP, StageFailed, addr + " refused the connection: nothing listens there, or a firewall refuses it"},
			[3]string{StageTLS, StageNotReached, ""}, [3]string{StageAPI, StageNotReached, ""})
	})
	t.Run("unknown authority", func(t *testing.T) {
		ca := testkit.NewTestCA(t, "untrusted CA")
		srv := testkit.NewFakeAPIServerWithCert(testkit.FakeAPIOptions{Token: secretToken}, ca.Issue(t, "127.0.0.1"))
		defer srv.Close()
		other := testkit.NewTestCA(t, "the kubeconfig's CA")
		got, err := stagesConnect(t, writeTLSKubeconfig(t, tlsCluster{server: srv.URL, caData: other.PEM()}), nil)
		if err == nil {
			t.Fatal("connected")
		}
		wantStages(t, got,
			[3]string{StageKubeconfig, StageOK, ""}, [3]string{StageDNS, StageSkipped, ""},
			[3]string{StageTCP, StageOK, "reached"},
			[3]string{StageTLS, StageFailed, "the server's certificate is signed by an authority this context does not trust"},
			[3]string{StageAPI, StageNotReached, ""})
	})
	t.Run("login rejected", func(t *testing.T) {
		ca := testkit.NewTestCA(t, "cogpit stages CA")
		srv := testkit.NewFakeAPIServerWithCert(testkit.FakeAPIOptions{Token: "another-token-entirely"}, ca.Issue(t, "127.0.0.1"))
		defer srv.Close()
		got, err := stagesConnect(t, writeTLSKubeconfig(t, tlsCluster{server: srv.URL, caData: ca.PEM()}), nil)
		if err == nil {
			t.Fatal("connected")
		}
		wantStages(t, got,
			[3]string{StageKubeconfig, StageOK, ""}, [3]string{StageDNS, StageSkipped, ""}, [3]string{StageTCP, StageOK, ""},
			[3]string{StageTLS, StageOK, "TLS 1.3"},
			[3]string{StageAPI, StageFailed, "the API server rejected the login (401 Unauthorized)"})
	})
}

// Behind a proxy the dial goes to the proxy: TCP names it, DNS says the proxy resolves the API
// server's name, and a tunnel the proxy refuses fails TCP, not TLS.
func TestConnectStagesBehindAProxy(t *testing.T) {
	ca := testkit.NewTestCA(t, "cogpit stages CA")
	srv := testkit.NewFakeAPIServerWithCert(testkit.FakeAPIOptions{Token: secretToken}, ca.Issue(t, "127.0.0.1"))
	defer srv.Close()
	proxyHost := func(p *testkit.ForwardProxy) string { return p.URL.Host }
	t.Run("open", func(t *testing.T) {
		p := testkit.NewForwardProxy(t, "", "")
		got, err := stagesConnect(t, writeTLSKubeconfig(t, tlsCluster{server: srv.URL, caData: ca.PEM(), proxyURL: p.URL.String()}), nil)
		if err != nil {
			t.Fatal(err)
		}
		wantStages(t, got,
			[3]string{StageKubeconfig, StageOK, ""},
			[3]string{StageDNS, StageSkipped, "the proxy " + proxyHost(p) + " is an address: no lookup"},
			[3]string{StageTCP, StageOK, "the proxy " + p.URL.String() + " at " + proxyHost(p) + " reached"},
			[3]string{StageTLS, StageOK, "with 127.0.0.1"},
			[3]string{StageAPI, StageOK, "Kubernetes v"})
	})
	t.Run("refused", func(t *testing.T) {
		p := testkit.NewForwardProxy(t, "proxyuser", "proxy-password-123456")
		got, err := stagesConnect(t, writeTLSKubeconfig(t, tlsCluster{server: srv.URL, caData: ca.PEM(),
			proxyURL: p.URLWithUser("proxyuser", "wrong-password-987654").String()}), nil)
		if err == nil {
			t.Fatal("connected through a proxy that refused")
		}
		wantStages(t, got,
			[3]string{StageKubeconfig, StageOK, ""}, [3]string{StageDNS, StageSkipped, ""},
			[3]string{StageTCP, StageFailed, "reached; it did not open the tunnel to " + strings.TrimPrefix(srv.URL, "https://")},
			[3]string{StageTLS, StageNotReached, ""}, [3]string{StageAPI, StageNotReached, ""})
		for _, st := range got {
			if strings.Contains(st.Text, "wrong-password-987654") {
				t.Errorf("stage %s quotes the proxy's password: %q", st.Name, st.Text)
			}
		}
	})
}

// A context configured wrong before Connect runs has the kubeconfig stage failed with its cause and
// every network stage not reached.
func TestConfigStages(t *testing.T) {
	got := ConfigStages("no such context")
	wantStages(t, got,
		[3]string{StageKubeconfig, StageFailed, "no such context"},
		[3]string{StageDNS, StageNotReached, ""}, [3]string{StageTCP, StageNotReached, ""},
		[3]string{StageTLS, StageNotReached, ""}, [3]string{StageAPI, StageNotReached, ""})
	if s, c := StagesOf(&ConnectError{Problem: "x", Stages: got, Cause: "no such context"}); len(s) != 5 || c != "no such context" {
		t.Errorf("StagesOf: %+v %q", s, c)
	}
	if s, c := StagesOf(os.ErrClosed); s != nil || c != "" {
		t.Errorf("StagesOf a plain error: %+v %q", s, c)
	}
}
