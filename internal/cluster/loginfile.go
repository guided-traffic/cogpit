package cluster

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// The login a connection was made with, as its kubeconfig file held it (D-81, the owner's F28). A
// connection keeps the credentials it read at its connect: a client certificate or a token in the
// kubeconfig (client-certificate-data, client-key-data, token) is copied into the connection's TLS
// and transport configuration once, and nothing reads the file again. client-go re-reads only a
// certificate and key that are both files (every 5 minutes) and a token file (every minute). So a
// certificate renewed in the file reached the connection only through a new connect, and Log in
// again, which renewed the login on the same connection, could not use it.
//
// Connect therefore notes a fingerprint of the context's login as the file holds it: the user
// entry's credentials, the cluster entry it is sent to, and the size and modification time of the
// files they name. LoginInFile reads the file again and reports whether its login differs: the
// engine then connects anew (Reconnect) to use it. The fingerprint is an HMAC under a key made for
// this process, so that it says nothing about a credential even where it is kept; it never leaves
// the process.

// fingerprintKey keys the logins' fingerprints, for this process only.
var fingerprintKey = func() []byte {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // crypto/rand does not fail on the platforms Go supports
	return b
}()

// loginEntry is what a context's login is made of, as its kubeconfig holds it.
type loginEntry struct {
	Server, TLSServerName, ProxyURL, CAFile                 string
	Insecure                                                bool
	CAData, CertData, KeyData                               []byte
	CertFile, KeyFile, Token, TokenFile, Username, Password string
	Impersonate, ImpersonateUID                             string
	ImpersonateGroups                                       []string
	ImpersonateExtra                                        map[string][]string
	ExecCommand, ExecAPIVersion, ExecInteractive            string
	ExecArgs                                                []string
	ExecEnv                                                 []clientcmdapi.ExecEnvVar
	ExecClusterInfo                                         bool
	AuthProvider                                            string
	AuthProviderConfig                                      map[string]string
	Files                                                   []fileStamp
}

// fileStamp is a file the login names, as stat sees it: a renewed certificate file changes it.
type fileStamp struct {
	Path    string
	Size    int64
	ModTime int64
	Missing bool
}

// loginFingerprint is the fingerprint of a context's login in raw; "" for a context it does not
// define.
func loginFingerprint(raw clientcmdapi.Config, kctx *clientcmdapi.Context) string {
	if kctx == nil {
		return ""
	}
	var e loginEntry
	if c := raw.Clusters[kctx.Cluster]; c != nil {
		e.Server, e.TLSServerName, e.ProxyURL, e.CAFile = c.Server, c.TLSServerName, c.ProxyURL, c.CertificateAuthority
		e.Insecure, e.CAData = c.InsecureSkipTLSVerify, c.CertificateAuthorityData
	}
	if u := raw.AuthInfos[kctx.AuthInfo]; u != nil {
		e.CertData, e.KeyData, e.CertFile, e.KeyFile = u.ClientCertificateData, u.ClientKeyData, u.ClientCertificate, u.ClientKey
		e.Token, e.TokenFile, e.Username, e.Password = u.Token, u.TokenFile, u.Username, u.Password
		e.Impersonate, e.ImpersonateUID, e.ImpersonateGroups, e.ImpersonateExtra = u.Impersonate, u.ImpersonateUID, u.ImpersonateGroups, u.ImpersonateUserExtra
		if x := u.Exec; x != nil {
			e.ExecCommand, e.ExecArgs, e.ExecEnv, e.ExecAPIVersion = x.Command, x.Args, x.Env, x.APIVersion
			e.ExecClusterInfo, e.ExecInteractive = x.ProvideClusterInfo, string(x.InteractiveMode)
		}
		if p := u.AuthProvider; p != nil {
			e.AuthProvider, e.AuthProviderConfig = p.Name, p.Config
		}
	}
	for _, p := range []string{e.CAFile, e.CertFile, e.KeyFile, e.TokenFile} {
		if p != "" {
			e.Files = append(e.Files, stampOf(p))
		}
	}
	data, err := json.Marshal(e) // #nosec G117 -- the HMAC's input below, never written or sent
	if err != nil {
		return ""
	}
	mac := hmac.New(sha256.New, fingerprintKey)
	_, _ = mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

// stampOf stats a file the login names, without opening it; a file in a place macOS protects is
// not looked at (clientcert.go, D-72): its path alone counts.
func stampOf(path string) fileStamp {
	if !filepath.IsAbs(path) || !readableUnasked(path) {
		return fileStamp{Path: path}
	}
	fi, err := os.Stat(path)
	if err != nil {
		return fileStamp{Path: path, Missing: true}
	}
	return fileStamp{Path: path, Size: fi.Size(), ModTime: fi.ModTime().UnixNano()}
}

// errNoLoginFile is LoginInFile's error for a connection made from no kubeconfig file (a test's).
var errNoLoginFile = errors.New("the connection was not made from a kubeconfig file")

// LoginInFile reads the connection's kubeconfig files again, as Connect read them, and returns the
// fingerprint of the context's login there and whether it differs from the one the connection was
// made with: a renewed certificate or token, another login program, another server (D-81). It
// reads only the kubeconfig and stats the files it names; a file that is missing or does not parse
// is an error (whose text names no credential), and changed is false then.
func (c *Conn) LoginInFile() (fingerprint string, changed bool, err error) {
	if c == nil || c.login == "" || len(c.paths) == 0 {
		return "", false, errNoLoginFile
	}
	rules := loadingRules(c.paths)
	usable, problems := checkFiles(rules.GetLoadingPrecedence(), true)
	if len(problems) > 0 {
		return "", false, errors.Join(problems...)
	}
	rules.Precedence = usable
	raw, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{CurrentContext: c.contextName}).RawConfig()
	if err != nil {
		return "", false, kubeconfig.SafeError(err)
	}
	kctx := raw.Contexts[c.contextName]
	if kctx == nil {
		return "", false, errors.New("the kubeconfig no longer defines the context " + kubeconfig.DisplayText(c.contextName, 200))
	}
	fp := loginFingerprint(raw, kctx)
	return fp, fp != c.login, nil
}

// CredentialPlugin reports whether the connection's login runs a credential plugin (the user entry's
// exec): a program that renews the login on the connection itself (D-58's Log in again). Without
// one, what the connection logs in with is what the kubeconfig held at the connect (D-81).
func (c *Conn) CredentialPlugin() bool { return c != nil && c.plugin }
