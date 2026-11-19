package engine

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/model"
)

// TestConfirmMessageNamesKinds: the confirmation's sentence names each object by its kind as the
// dialog's Objects row does ("Drain Node worker-1"), not by its kind key ("Drain nodes
// worker-1"); an object without a name in Kinds
// keeps its key.
func TestConfirmMessageNamesKinds(t *testing.T) {
	req := ConfirmRequest{Label: "Drain", Context: "prod", Objects: []model.ObjectRef{{Kind: "nodes", Name: "worker-1"}}, Kinds: []string{"Node"}}
	if got, want := req.Message(), "Drain Node worker-1 (context prod)?"; got != want {
		t.Errorf("message %q, want %q", got, want)
	}
	req = ConfirmRequest{Label: "Delete", Context: "prod", Namespaces: []string{"shop"},
		Objects: []model.ObjectRef{{Kind: "configmaps", Namespace: "shop", Name: "a"}, {Kind: "widgets.example.com", Namespace: "shop", Name: "b"}},
		Kinds:   []string{"Config Map", ""}}
	if got, want := req.Message(), "Delete 2 objects: Config Map a, widgets.example.com b in namespace shop (context prod)?"; got != want {
		t.Errorf("message %q, want %q", got, want)
	}
	for title, want := range map[string]string{
		"Pods": "Pod", "Ingress Classes": "Ingress Class", "Network Policies": "Network Policy", "Ingresses": "Ingress",
		"Role Bindings": "Role Binding", "Certificate": "Certificate", "Port Forwarding": "Port Forwarding",
	} {
		if got := singularTitle(title); got != want {
			t.Errorf("singularTitle(%q) = %q, want %q", title, got, want)
		}
	}
}
