package fyneui

import (
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// clusterProxied gives the EKS context the proxy of Cogpit's Cluster Settings (its password as the
// engine shows it) and a proxy-url of the kubeconfig's own, which Cogpit's replaces (A23).
func clusterProxied(_, _, ctx map[string]any) {
	ctx["tls"] = map[string]any{
		"proxyURL":     "http://kube-proxy.example:3128",
		"clusterProxy": "http://alice:•••@proxy.corp.example:3128",
	}
}

// TestApprovalDialogShowsCogpitsProxy (A23, H-34): the dialog names the proxy Cogpit sets for the
// cluster, as the connection uses it, in the warning colour, and says that changing it asks again;
// the kubeconfig's own proxy-url is listed as not used. No password is shown.
func TestApprovalDialogShowsCogpitsProxy(t *testing.T) {
	_, w := openApproval(t, approvalFake(t, engine.ExecChanged, clusterProxied), ThemeDark)
	texts := overlayTexts(w)
	for _, want := range []string{
		"goes through the proxy http://alice:•••@proxy.corp.example:3128, set in Cogpit's Cluster Settings",
		"(Cluster Settings › Proxy; changing it asks again)",
		"Kubeconfig proxy", "http://kube-proxy.example:3128 (not used: Cogpit's proxy replaces it)",
		"The login program changed",
	} {
		if !hasText(texts, want) {
			t.Errorf("the dialog does not show %q; it shows %q", want, texts)
		}
	}
	warned := false
	for _, p := range walkParagraphs(w) {
		if p.color == colorStatusWarning && strings.Contains(p.text, "proxy.corp.example") {
			warned = true
		}
	}
	if !warned {
		t.Error("Cogpit's proxy is not a warning line")
	}
	if hasText(texts, "The connection goes through the proxy http://kube-proxy.example:3128") {
		t.Error("the dialog says the connection uses the kubeconfig's proxy, which Cogpit's replaces")
	}
}

func TestRenderExecApprovalClusterProxy(t *testing.T) {
	for _, tc := range []struct {
		mode ThemeMode
		name string
	}{{ThemeDark, "exec_approval_cluster_proxy_dark.png"}} {
		t.Run(tc.name, func(t *testing.T) {
			_, w := openApproval(t, approvalFake(t, engine.ExecChanged, clusterProxied), tc.mode)
			savePNG(t, w, tc.name)
		})
	}
}
