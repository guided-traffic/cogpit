package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

func (te *testEnv) reset() {
	for _, b := range []*syncBuffer{te.out, te.errOut} {
		b.mu.Lock()
		b.buf.Reset()
		b.mu.Unlock()
	}
}

// listJSON runs get with -o json and decodes resources_list's result.
func (te *testEnv) listJSON(t *testing.T, args ...string) commands.ListResult {
	t.Helper()
	te.reset()
	if code := te.run(t, append(append([]string{"get"}, args...), "-o", "json")...); code != exitOK {
		t.Fatalf("get %v: exit code %d; stderr: %s", args, code, te.errOut)
	}
	var res commands.ListResult
	if err := json.Unmarshal([]byte(te.out.String()), &res); err != nil {
		t.Fatalf("not resources_list's result: %v\n%s", err, te.out)
	}
	return res
}

func TestGetListsTheDemoClustersPods(t *testing.T) {
	te := newTestEnv(t)
	if code := te.run(t, "get", "pods", "--context", "demo"); code != exitOK {
		t.Fatalf("exit code %d; stderr: %s", code, te.errOut)
	}
	lines := strings.Split(strings.TrimRight(te.out.String(), "\n"), "\n")
	header := strings.Fields(lines[0])
	if len(header) < 3 || header[0] != "NAME" || header[1] != "NAMESPACE" {
		t.Errorf("header %q", lines[0])
	}
	// The metrics columns stay empty without a metrics view, and are left out.
	if strings.Contains(lines[0], "CPU") || strings.Contains(lines[0], "MEMORY") {
		t.Errorf("empty metrics columns in the header %q", lines[0])
	}

	all := te.listJSON(t, "pods", "--context", "demo")
	if all.Kind != "pods" || all.Total == 0 || all.Returned != all.Total {
		t.Fatalf("pods: kind %q, total %d, returned %d", all.Kind, all.Total, all.Returned)
	}
	if got := len(lines) - 1; got != all.Total {
		t.Errorf("%d rows in the table, %d in resources_list's result", got, all.Total)
	}
	ns := all.RefAt(0).Namespace
	// Flags after the arguments, a Kubernetes kind for the key, one namespace.
	inNS := te.listJSON(t, "Pod", "--context=demo", "-n", ns)
	if inNS.Total == 0 || inNS.Total >= all.Total && len(namespaces(all)) > 1 {
		t.Errorf("-n %s: %d of %d pods", ns, inNS.Total, all.Total)
	}
	for i := range inNS.Rows {
		if ref := inNS.RefAt(i); ref.Namespace != ns || ref.Context != "demo" {
			t.Errorf("row %+v outside -n %s", ref, ns)
		}
	}
	te.reset()
	if code := te.run(t, "get", "pods", "--context", "demo", "-o", "yaml", "-n", ns); code != exitOK ||
		!strings.Contains(te.out.String(), "kind: pods") || !strings.Contains(te.out.String(), "rows:") {
		t.Errorf("yaml: exit %d\n%s", code, te.out)
	}
}

func namespaces(r commands.ListResult) []string {
	var out []string
	for i := range r.Rows {
		if ns := r.RefAt(i).Namespace; !slices.Contains(out, ns) {
			out = append(out, ns)
		}
	}
	return out
}

func TestGetOneObject(t *testing.T) {
	te := newTestEnv(t)
	pod := te.listJSON(t, "pods", "--context", "demo").RefAt(0)

	te.reset()
	if code := te.run(t, "get", "pods", pod.Name, "-n", pod.Namespace, "--context", "demo", "-o", "yaml"); code != exitOK {
		t.Fatalf("yaml: exit code %d; stderr: %s", code, te.errOut)
	}
	if y := te.out.String(); !strings.Contains(y, "kind: Pod") || !strings.Contains(y, "name: "+pod.Name) || strings.Contains(y, "managedFields") {
		t.Errorf("not the pod's manifest:\n%s", y)
	}

	te.reset()
	if code := te.run(t, "get", "pods", pod.Name, "-n", pod.Namespace, "--context", "demo", "-o", "json"); code != exitOK {
		t.Fatalf("json: exit code %d; stderr: %s", code, te.errOut)
	}
	var obj struct {
		Kind     string `json:"kind"`
		Metadata struct {
			Name, Namespace string
		} `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(te.out.String()), &obj); err != nil || obj.Kind != "Pod" || obj.Metadata.Name != pod.Name {
		t.Errorf("json manifest %+v, %v\n%s", obj, err, te.out)
	}

	te.reset()
	if code := te.run(t, "get", "pods", pod.Name, "-n", pod.Namespace, "--context", "demo"); code != exitOK {
		t.Fatalf("table: exit code %d; stderr: %s", code, te.errOut)
	}
	if lines := strings.Split(strings.TrimSpace(te.out.String()), "\n"); len(lines) != 2 || !strings.HasPrefix(lines[1], pod.Name+" ") {
		t.Errorf("want the header and the pod's row:\n%s", te.out)
	}

	for _, format := range []string{"table", "yaml"} {
		te.reset()
		if code := te.run(t, "get", "pods", "no-such-pod", "-n", pod.Namespace, "--context", "demo", "-o", format); code != exitError {
			t.Errorf("%s: a missing pod exits %d", format, code)
		}
		if !strings.Contains(te.errOut.String(), `Pods "no-such-pod" not found in namespace "`+pod.Namespace+`"`) {
			t.Errorf("%s: stderr %s", format, te.errOut)
		}
	}
}

func TestGetErrors(t *testing.T) {
	cases := []struct {
		args   []string
		code   int
		errOut string
	}{
		{[]string{"get"}, exitUsage, "a kind is required"},
		{[]string{"get", "pods"}, exitUsage, "--context is required"},
		{[]string{"get", "pods", "a", "b", "--context", "demo"}, exitUsage, "at most one name"},
		{[]string{"get", "pods", "a", "-n", "x,y", "--context", "demo"}, exitUsage, "one namespace"},
		{[]string{"get", "pods", "--context", "demo", "-o", "wide"}, exitUsage, "unknown output format"},
		{[]string{"get", "gadgets", "--context", "demo"}, exitError, `unknown kind "gadgets"; this cluster serves: `},
		{[]string{"get", "pods", "--context", "nowhere"}, exitError, "connect nowhere"},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			te := newTestEnv(t)
			if code := te.run(t, c.args...); code != c.code {
				t.Errorf("exit code %d, want %d; stderr: %s", code, c.code, te.errOut)
			}
			if !strings.Contains(te.errOut.String(), c.errOut) {
				t.Errorf("stderr lacks %q:\n%s", c.errOut, te.errOut)
			}
		})
	}
}

// TestGetKeepsSecretsSecret reads Secrets from a fake API server through a kubeconfig named with
// --kubeconfig: the table names the keys, the manifest redacts the values, and neither the values
// nor the kubeconfig's token reach stdout or stderr.
func TestGetKeepsSecretsSecret(t *testing.T) {
	const token, value = "fake-TOKEN-0123456789", "pw-PLAINTEXT-42"
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{
		Token: token,
		Resources: append(slices.Clone(testkit.DefaultFakeResources),
			testkit.FakeResource{GVR: testkit.SecretsGVR, Kind: "Secret", Namespaced: true}),
	})
	defer srv.Close()
	srv.Set(testkit.SecretsGVR, testkit.Secret("team", "db-creds", nil, map[string]string{"password": value}))
	srv.Set(testkit.PodsGVR, testkit.Pod("team", "web-0"))
	file := srv.WriteKubeconfig(t, "fake", "team")

	te := newTestEnv(t)
	leaks := func(what string) {
		t.Helper()
		all := te.out.String() + te.errOut.String()
		for _, secret := range []string{value, base64.StdEncoding.EncodeToString([]byte(value)), token} {
			if strings.Contains(all, secret) {
				t.Errorf("%s: output carries a secret:\n%s", what, all)
			}
		}
	}

	if code := te.run(t, "get", "secrets", "--kubeconfig", file, "--context", "fake"); code != exitOK {
		t.Fatalf("table: exit code %d; stderr: %s", code, te.errOut)
	}
	if out := te.out.String(); !strings.Contains(out, "db-creds") || !strings.Contains(out, "password") {
		t.Errorf("table:\n%s", out)
	}
	leaks("table")

	te.reset()
	if code := te.run(t, "get", "secrets", "db-creds", "--kubeconfig", file, "--context", "fake", "-o", "yaml"); code != exitOK {
		t.Fatalf("yaml: exit code %d; stderr: %s", code, te.errOut)
	}
	if out := te.out.String(); !strings.Contains(out, "kind: Secret") || !strings.Contains(out, "password:") {
		t.Errorf("yaml (the context's namespace team):\n%s", out)
	}
	leaks("yaml")

	te.reset()
	if code := te.run(t, "get", "secrets", "db-creds", "-n", "team", "--kubeconfig", file, "--context", "fake [kubeconfig]", "-o", "json"); code != exitOK {
		t.Fatalf("json: exit code %d; stderr: %s", code, te.errOut)
	}
	leaks("json")

	te.reset()
	got := te.listJSON(t, "pods", "--kubeconfig", file, "--context", "fake")
	if got.Total != 1 || got.RefAt(0).Name != "web-0" || got.RefAt(0).Context != "fake [kubeconfig]" {
		t.Errorf("pods through the source context: %+v", got)
	}
	leaks("pods")
}

// TestGetWaitsForCustomKinds: a fresh session knows a custom resource kind only once the
// definitions are listed; get waits for that instead of calling the kind unknown.
func TestGetWaitsForCustomKinds(t *testing.T) {
	widgets := schema.GroupVersionResource{Group: "acme.example.com", Version: "v1", Resource: "widgets"}
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{
		Resources: append(slices.Clone(testkit.DefaultFakeResources),
			testkit.FakeResource{GVR: testkit.CRDsGVR, Kind: "CustomResourceDefinition"},
			testkit.FakeResource{GVR: widgets, Kind: "Widget", Namespaced: true}),
	})
	defer srv.Close()
	srv.Set(testkit.CRDsGVR, testkit.CRD("acme.example.com", "widgets", "Widget", true))
	srv.Set(widgets, testkit.Object("acme.example.com/v1", "Widget", "team", "w1", nil))
	file := srv.WriteKubeconfig(t, "fake", "team")

	te := newTestEnv(t)
	for _, kind := range []string{"widgets.acme.example.com", "Widget"} {
		got := te.listJSON(t, kind, "--kubeconfig", file, "--context", "fake")
		if got.Kind != "widgets.acme.example.com" || got.Total != 1 || got.RefAt(0).Name != "w1" {
			t.Errorf("get %s: %+v", kind, got)
		}
	}
}

// TestGetRunsNoPluginTheUserHasNotAllowed: a context whose login program the user has not allowed
// in Cogpit's window is refused before the program runs.
func TestGetRunsNoPluginTheUserHasNotAllowed(t *testing.T) {
	te := newTestEnv(t)
	marker := filepath.Join(t.TempDir(), "plugin-ran")
	te.writeKube(t, "plugins.yaml", pluginKubeconfig(t, marker))
	if code := te.run(t, "get", "pods", "--context", "login"); code != exitError {
		t.Fatalf("exit code %d, want %d", code, exitError)
	}
	if errOut := te.errOut.String(); !strings.Contains(errOut, "which you have not allowed yet") || strings.Contains(errOut, "arg-SECRET") {
		t.Errorf("stderr: %s", errOut)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("the credential plugin ran: %v", err)
	}
	if _, err := os.Stat(filepath.Join(te.dirs.Config, "exec-approvals.json")); !os.IsNotExist(err) {
		t.Errorf("the CLI wrote approvals: %v", err)
	}
}
