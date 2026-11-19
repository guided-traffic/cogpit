package kubeconfig

import (
	"strings"
	"testing"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// TestIdentityIsWhatTheLoginDoesNotWhereItIsUsed (D-28): contexts of one file whose logins run the
// same program the same way share an identity, whatever their context, user, server and trust;
// everything that changes what runs, or which token file is read, changes it.
func TestIdentityIsWhatTheLoginDoesNotWhereItIsUsed(t *testing.T) {
	base := func() (*clientcmdapi.Cluster, *clientcmdapi.ExecConfig) {
		return &clientcmdapi.Cluster{Server: "https://a.example", CertificateAuthorityData: []byte("ca")},
			&clientcmdapi.ExecConfig{
				APIVersion: "client.authentication.k8s.io/v1", Command: "aws", Args: []string{"a", "b"},
				Env: []clientcmdapi.ExecEnvVar{{Name: "K", Value: "v"}}, InteractiveMode: clientcmdapi.NeverExecInteractiveMode,
			}
	}
	cl, ec := base()
	ref := NewExecPlugin("/f", "ctx", "u", cl, ec).Identity()
	if !strings.HasPrefix(ref, "login:") || NewExecPlugin("/f", "ctx", "u", cl, ec).Identity() != ref {
		t.Fatalf("identity %q is not deterministic", ref)
	}
	type change func(ctx, user *string, cl *clientcmdapi.Cluster, ec *clientcmdapi.ExecConfig)
	tests := []struct {
		name   string
		change change
		same   bool
	}{
		{"context", func(c, _ *string, _ *clientcmdapi.Cluster, _ *clientcmdapi.ExecConfig) { *c = "other" }, true},
		{"user", func(_, u *string, _ *clientcmdapi.Cluster, _ *clientcmdapi.ExecConfig) { *u = "other" }, true},
		{"server", func(_, _ *string, cl *clientcmdapi.Cluster, _ *clientcmdapi.ExecConfig) {
			cl.Server = "https://b.example"
		}, true},
		{"CA", func(_, _ *string, cl *clientcmdapi.Cluster, _ *clientcmdapi.ExecConfig) {
			cl.CertificateAuthorityData = nil
		}, true},
		{"insecure", func(_, _ *string, cl *clientcmdapi.Cluster, _ *clientcmdapi.ExecConfig) {
			cl.InsecureSkipTLSVerify = true
		}, true},
		{"proxy", func(_, _ *string, cl *clientcmdapi.Cluster, _ *clientcmdapi.ExecConfig) {
			cl.ProxyURL = "http://p.example"
		}, true},
		{"install hint", func(_, _ *string, _ *clientcmdapi.Cluster, ec *clientcmdapi.ExecConfig) { ec.InstallHint = "brew" }, true},
		{"command", func(_, _ *string, _ *clientcmdapi.Cluster, ec *clientcmdapi.ExecConfig) { ec.Command = "./aws" }, false},
		{"an argument", func(_, _ *string, _ *clientcmdapi.Cluster, ec *clientcmdapi.ExecConfig) { ec.Args[1] = "c" }, false},
		{"an env value", func(_, _ *string, _ *clientcmdapi.Cluster, ec *clientcmdapi.ExecConfig) { ec.Env[0].Value = "w" }, false},
		{"an env name", func(_, _ *string, _ *clientcmdapi.Cluster, ec *clientcmdapi.ExecConfig) { ec.Env[0].Name = "PATH" }, false},
		{"api version", func(_, _ *string, _ *clientcmdapi.Cluster, ec *clientcmdapi.ExecConfig) {
			ec.APIVersion = "client.authentication.k8s.io/v1beta1"
		}, false},
		{"interactive mode", func(_, _ *string, _ *clientcmdapi.Cluster, ec *clientcmdapi.ExecConfig) {
			ec.InteractiveMode = clientcmdapi.AlwaysExecInteractiveMode
		}, false},
		{"cluster info", func(_, _ *string, _ *clientcmdapi.Cluster, ec *clientcmdapi.ExecConfig) { ec.ProvideClusterInfo = true }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, user := "ctx", "u"
			cl, ec := base()
			tt.change(&ctx, &user, cl, ec)
			if got := NewExecPlugin("/f", ctx, user, cl, ec).Identity() == ref; got != tt.same {
				t.Errorf("same identity = %v, want %v", got, tt.same)
			}
		})
	}

	t.Run("program content", func(t *testing.T) {
		cl, ec := base()
		p := NewExecPlugin("/f", "ctx", "u", cl, ec)
		p.SetFiles(FileFacts{ProgramSHA256: strings.Repeat("ab", 32)})
		if p.Identity() == ref {
			t.Error("the program's content binding is not part of the identity")
		}
	})
	t.Run("token files", func(t *testing.T) {
		tf := func(path string, inFolder bool) *ExecPlugin {
			p := NewLogin("/f", "ctx", "u", &clientcmdapi.Cluster{Server: "https://a.example"}, nil, path)
			p.SetFiles(FileFacts{TokenInFolder: inFolder})
			return p
		}
		a, b := tf("/secrets/a.token", false), tf("/secrets/a.token", false)
		b.Context, b.Server = "other", "https://b.example"
		if a.Identity() == "" || a.Identity() != b.Identity() || a.Identity() == tf("/secrets/b.token", false).Identity() {
			t.Errorf("token file identities: %q %q", a.Identity(), b.Identity())
		}
		if tf("/f-dir/a.token", true).Identity() != "" || (*ExecPlugin)(nil).Identity() != "" {
			t.Error("a login that needs no approval has an identity")
		}
	})
}
