//go:build kind

package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestKindCLIContextsAndGet lists the kind cluster's context through the kubeconfig `make
// test-kind` names (passed with --kubeconfig, never ~/.kube) and reads kube-system's pods and the
// namespaces through the headless engine. It only reads: the kubeconfig stays byte-identical and
// nothing is written to the cluster.
func TestKindCLIContextsAndGet(t *testing.T) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	before, err := os.ReadFile(kubeconfig)
	if err != nil {
		t.Fatal(err)
	}
	te := newTestEnv(t)
	id := contextName + " [kubeconfig]"

	if code := te.run(t, "contexts", "--kubeconfig", kubeconfig); code != exitOK {
		t.Fatalf("contexts: exit code %d; stderr: %s", code, te.errOut)
	}
	if !strings.Contains(te.out.String(), id) {
		t.Fatalf("contexts lacks %q:\n%s", id, te.out)
	}

	pods := te.listJSON(t, "pods", "-n", "kube-system", "--kubeconfig", kubeconfig, "--context", contextName)
	if pods.Total == 0 {
		t.Fatal("no pods in kube-system")
	}
	etcd := false
	for i := range pods.Rows {
		ref := pods.RefAt(i)
		if ref.Namespace != "kube-system" || ref.Context != id {
			t.Errorf("row %+v", ref)
		}
		etcd = etcd || strings.HasPrefix(ref.Name, "etcd-")
	}
	if !etcd {
		t.Errorf("no etcd pod among %d kube-system pods", pods.Total)
	}

	te.reset()
	if code := te.run(t, "get", "namespaces", "--kubeconfig", kubeconfig, "--context", id); code != exitOK {
		t.Fatalf("get namespaces: exit code %d; stderr: %s", code, te.errOut)
	}
	if out := te.out.String(); !strings.HasPrefix(out, "NAME") || !strings.Contains(out, "\nkube-system ") {
		t.Errorf("namespaces:\n%s", out)
	}
	t.Logf("kind: %d pods in kube-system; namespaces:\n%s", pods.Total, te.out)

	// A custom resource kind resolves in a fresh session: get waits for the definitions' first
	// list before it looks the key up. A definition's name is its kind key.
	crds := te.listJSON(t, "customresourcedefinitions", "--kubeconfig", kubeconfig, "--context", id)
	if crds.Total == 0 {
		t.Log("the kind cluster defines no custom resources; the custom kind is not read")
	} else {
		key := crds.RefAt(0).Name
		custom := te.listJSON(t, key, "--kubeconfig", kubeconfig, "--context", id)
		if custom.Kind != key {
			t.Errorf("get %s read %q", key, custom.Kind)
		}
		t.Logf("custom kind %s: %d objects", key, custom.Total)
	}

	after, err := os.ReadFile(kubeconfig)
	if err != nil || !bytes.Equal(before, after) {
		t.Errorf("the kind kubeconfig changed (%v)", err)
	}
}
