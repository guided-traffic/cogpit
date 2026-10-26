package terminal

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"k8s.io/client-go/tools/clientcmd"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// localBinding is what a local terminal is bound to: the context entry for its own kubeconfig
// and the user's files that follow it on KUBECONFIG.
type localBinding struct {
	context kubeconfig.Context
	paths   []string // absolute, in precedence order
}

// resolveLocal finds the context's cluster and user names in the user's kubeconfig files with
// client-go's loading rules, the same merge kubectl in the terminal will do.
func resolveLocal(opts LocalOptions) (localBinding, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if len(opts.KubeconfigPaths) > 0 {
		rules = &clientcmd.ClientConfigLoadingRules{Precedence: opts.KubeconfigPaths}
	}
	// The default rules migrate ~/.kube/.kubeconfig to ~/.kube/config on load, which writes a
	// kubeconfig file. Cogpit writes no kubeconfig implicitly.
	rules.MigrationRules = nil
	// Load parses and merges; it runs no exec credential plugin.
	cfg, err := rules.Load()
	if err != nil {
		// SafeError: client-go's errors for some malformed files echo users entries, tokens included.
		return localBinding{}, fmt.Errorf("local terminal: reading kubeconfig: %w", kubeconfig.SafeError(err))
	}
	name := opts.Context
	if name == "" {
		name = cfg.CurrentContext
	}
	if name == "" {
		return localBinding{}, fmt.Errorf("local terminal: no context given and the kubeconfig files set no current-context")
	}
	c, ok := cfg.Contexts[name]
	if !ok || c == nil {
		return localBinding{}, fmt.Errorf("local terminal: context %q is not defined in the kubeconfig files", name)
	}
	ns := opts.Namespace
	if ns == "" {
		ns = c.Namespace
	}

	var paths []string
	for _, p := range rules.GetLoadingPrecedence() {
		if p == "" {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return localBinding{}, fmt.Errorf("local terminal: kubeconfig path %q: %w", p, err)
		}
		// KUBECONFIG is a list; a path that contains the separator cannot be put on it.
		if strings.ContainsRune(abs, os.PathListSeparator) {
			return localBinding{}, fmt.Errorf("local terminal: kubeconfig path %q contains %q and cannot be put on KUBECONFIG", abs, os.PathListSeparator)
		}
		paths = append(paths, abs)
	}
	return localBinding{
		context: kubeconfig.Context{Name: name, Cluster: c.Cluster, User: c.AuthInfo, Namespace: ns},
		paths:   paths,
	}, nil
}

// kubeconfigVar is the KUBECONFIG value: the terminal's own file first, then the user's files.
func (b localBinding) kubeconfigVar(own string) string {
	return strings.Join(append([]string{own}, b.paths...), string(os.PathListSeparator))
}

// writeTerminalConfig writes the terminal's own kubeconfig to a new owner-only temporary file and
// returns its path. The file holds names only, no credential.
func writeTerminalConfig(ctx kubeconfig.Context) (string, error) {
	data, err := kubeconfig.TerminalConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("local terminal: %w", err)
	}
	// CreateTemp creates the file with mode 0600 in the user's temporary directory (per user on
	// macOS); the explicit Chmod keeps that true whatever CreateTemp does in future. On Windows the
	// mode only keeps the file writable; the user's temporary directory (%TEMP%, in the profile)
	// grants the user, SYSTEM and Administrators access, which the file inherits.
	f, err := os.CreateTemp("", "cogpit-terminal-*.kubeconfig")
	if err != nil {
		return "", fmt.Errorf("local terminal: creating the terminal kubeconfig: %w", err)
	}
	path := f.Name()
	fail := func(err error) (string, error) {
		_ = f.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("local terminal: writing the terminal kubeconfig: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		return fail(err)
	}
	if _, err := f.Write(data); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("local terminal: writing the terminal kubeconfig: %w", err)
	}
	return path, nil
}

// localEnv is the shell's environment: Cogpit's own, the terminal type, opts.Env, the entries that
// put the chosen kubectl first (kubectl.go), and KUBECONFIG last. os/exec keeps the last value of
// a duplicated key, so opts.Env can override TERM but not KUBECONFIG.
func localEnv(extra []string, kubeconfigVar string, kubectl ...string) []string {
	env := baseEnv(extra)
	env = append(env, kubectl...)
	return append(env, "KUBECONFIG="+kubeconfigVar)
}

// baseEnv is the shell's environment before Cogpit's own entries: Cogpit's, the terminal type and
// opts.Env. FYNE_SCALE is given back as Cogpit was started with it: the window's zoom sets it in
// Cogpit's own environment for Fyne (fyneui/zoom.go, D-134), and a Fyne program started from the
// shell would otherwise open at Cogpit's zoom.
func baseEnv(extra []string) []string {
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		k, _, _ := strings.Cut(kv, "=")
		return strings.EqualFold(k, fyneScaleEnv) // Windows' names are any case
	})
	if startFyneScaleSet {
		env = append(env, fyneScaleEnv+"="+startFyneScale)
	}
	env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor")
	return append(env, extra...)
}

// fyneScaleEnv is Fyne's scale variable; startFyneScale its value when Cogpit started, read before
// main runs and the window's zoom changes it.
const fyneScaleEnv = "FYNE_SCALE"

var startFyneScale, startFyneScaleSet = os.LookupEnv(fyneScaleEnv)
