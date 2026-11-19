package engine

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// Asking once per file and plugin (D-28, owner's decision).

// bundleCtx is one context of bundleKubeconfig: its name, the server of its cluster and the
// arguments of its user's exec plugin (command "aws"), or a token file when tokenFile is set.
type bundleCtx struct {
	name, server string
	args         []string
	tokenFile    string
}

func eks(name string, args ...string) bundleCtx {
	return bundleCtx{name: name, server: "https://" + name + ".eks.example:443", args: args}
}

// bundleKubeconfig is one file with a cluster, a user and a context per entry.
func bundleKubeconfig(ctxs ...bundleCtx) string {
	var clusters, users, contexts strings.Builder
	for _, c := range ctxs {
		clusters.WriteString("- name: " + c.name + "\n  cluster: {server: \"" + c.server + "\"}\n")
		users.WriteString("- name: " + c.name + "\n  user:\n")
		if c.tokenFile != "" {
			users.WriteString("    tokenFile: " + c.tokenFile + "\n")
		} else {
			users.WriteString("    exec:\n      apiVersion: client.authentication.k8s.io/v1\n      interactiveMode: Never\n" +
				"      command: aws\n      args: [\"" + strings.Join(c.args, `", "`) + "\"]\n" +
				"      env:\n      - {name: SECRET_ENV, value: " + sourceSecret + "}\n")
		}
		contexts.WriteString("- name: " + c.name + "\n  context: {cluster: " + c.name + ", user: " + c.name + "}\n")
	}
	return "apiVersion: v1\nkind: Config\nclusters:\n" + clusters.String() + "users:\n" + users.String() + "contexts:\n" + contexts.String()
}

// bundleSession is a session over one added file written by the test.
type bundleSession struct {
	s    *Session
	k    *KubeconfigSources
	file string
}

func newBundleSession(t *testing.T, ctxs ...bundleCtx) *bundleSession {
	t.Helper()
	file := filepath.Join(t.TempDir(), "config")
	writeUserFile(t, file, bundleKubeconfig(ctxs...))
	k := NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"))
	if _, _, err := k.AddFile(file); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Connector: &ClusterConnector{Sources: k}})
	t.Cleanup(s.Close)
	return &bundleSession{s: s, k: k, file: file}
}

func (b *bundleSession) rewrite(t *testing.T, ctxs ...bundleCtx) {
	t.Helper()
	writeUserFile(t, b.file, bundleKubeconfig(ctxs...))
}

func (b *bundleSession) pending(t *testing.T, contextID string) []ExecApprovalGroup {
	t.Helper()
	groups, err := b.s.PendingExecApprovals(contextID)
	if err != nil {
		t.Fatal(err)
	}
	return groups
}

func kubeContexts(g ExecApprovalGroup) []string {
	var out []string
	for _, c := range g.Contexts {
		out = append(out, c.KubeContext)
	}
	return out
}

func itemsOf(g ExecApprovalGroup) []ExecApprovalItem {
	var out []ExecApprovalItem
	for _, c := range g.Contexts {
		out = append(out, ExecApprovalItem{Context: c.Context, Fingerprint: c.Fingerprint})
	}
	return out
}

func execStates(t *testing.T, b *bundleSession) map[string]string {
	t.Helper()
	infos, err := b.k.Contexts()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, i := range infos {
		out[i.KubeContext] = i.Exec
	}
	return out
}

var getToken = []string{"eks", "get-token", "--region", "eu-central-1"}

// TestPendingApprovalsGroupContextsByFileAndPlugin: three contexts that run the same plugin are one
// group listing each with its server, a fourth with another plugin is a group of its own; the shared
// plugin carries nothing that is specific to one context.
func TestPendingApprovalsGroupContextsByFileAndPlugin(t *testing.T) {
	b := newBundleSession(t, eks("c-eks", getToken...), eks("a-eks", getToken...), eks("b-eks", getToken...), eks("other", "sso", "login"))
	groups := b.pending(t, "")
	if len(groups) != 2 {
		t.Fatalf("%d groups, want 2: %+v", len(groups), groups)
	}
	g := groups[0]
	if got, want := kubeContexts(g), []string{"a-eks", "b-eks", "c-eks"}; !slices.Equal(got, want) {
		t.Errorf("contexts %v, want %v (sorted by name)", got, want)
	}
	if g.Kind != kubeconfig.LoginProgram || g.Source != "config" || g.File != b.file || g.Plugin.Command != "aws" || !slices.Equal(g.Plugin.Args, getToken) {
		t.Errorf("group %+v, plugin %+v", g, g.Plugin)
	}
	if g.Plugin.Context != "" || g.Plugin.User != "" || g.Plugin.Server != "" || g.Plugin.TLS != (kubeconfig.TLSView{}) {
		t.Errorf("the shared plugin carries what is specific to a context: %+v", g.Plugin)
	}
	fps := map[string]bool{}
	for _, c := range g.Contexts {
		if c.Server != "https://"+c.KubeContext+".eks.example:443" || c.State != ExecRequired || c.Context != c.KubeContext+" [config]" {
			t.Errorf("context %+v", c)
		}
		fps[c.Fingerprint] = true
	}
	if len(fps) != 3 {
		t.Errorf("the contexts share a fingerprint (it binds the context and the server): %v", fps)
	}
	if o := groups[1]; !slices.Equal(kubeContexts(o), []string{"other"}) || !slices.Equal(o.Plugin.Args, []string{"sso", "login"}) {
		t.Errorf("second group %+v", o)
	}

	// With a context: its group, siblings included; an unknown id is an error.
	one := b.pending(t, "b-eks [config]")
	if len(one) != 1 || !slices.Equal(kubeContexts(one[0]), []string{"a-eks", "b-eks", "c-eks"}) {
		t.Errorf("group of b-eks: %+v", one)
	}
	if _, err := b.s.PendingExecApprovals("nope [config]"); err == nil {
		t.Error("an unknown context was listed")
	}
}

// TestPendingApprovalsSplitByFileAndByWhatRuns: the same plugin in two files is two groups; a plugin
// that differs in its arguments is another group even in one file.
func TestPendingApprovalsSplitByFileAndByWhatRuns(t *testing.T) {
	su := newSessionSetup(t)
	writeUserFile(t, su.a, bundleKubeconfig(eks("x1", getToken...), eks("x2", getToken...), eks("x3", append(slices.Clone(getToken), "--role", "admin")...)))
	writeUserFile(t, su.b, bundleKubeconfig(eks("y1", getToken...)))
	s := New(Options{Connector: &ClusterConnector{Sources: su.open()}})
	defer s.Close()
	groups, err := s.PendingExecApprovals("")
	if err != nil {
		t.Fatal(err)
	}
	var shape []string
	for _, g := range groups {
		shape = append(shape, g.Source+":"+strings.Join(kubeContexts(g), "+"))
	}
	if want := []string{"a.yaml:x1+x2", "a.yaml:x3", "b.yaml:y1"}; !slices.Equal(shape, want) {
		t.Errorf("groups %v, want %v", shape, want)
	}
}

// TestApprovingAGroupApprovesExactlyItsContexts: one call allows the three contexts of the group, each
// stored with its own fingerprint; the context with the other plugin still asks; the group is gone
// from the listing; a restart remembers it.
func TestApprovingAGroupApprovesExactlyItsContexts(t *testing.T) {
	b := newBundleSession(t, eks("a-eks", getToken...), eks("b-eks", getToken...), eks("c-eks", getToken...), eks("other", "sso", "login"))
	g := b.pending(t, "")[0]
	approved, err := b.s.ApproveExecAll(itemsOf(g))
	if err != nil || len(approved) != 3 {
		t.Fatalf("ApproveExecAll: %+v, %v", approved, err)
	}
	for _, a := range approved {
		if a.State != ExecApproved || a.Plugin == nil {
			t.Errorf("approved %+v", a)
		}
	}
	want := map[string]string{"a-eks": ExecApproved, "b-eks": ExecApproved, "c-eks": ExecApproved, "other": ExecRequired}
	for k, v := range want {
		if got := execStates(t, b)[k]; got != v {
			t.Errorf("%s is %q, want %q", k, got, v)
		}
	}
	stored, err := b.k.approvals()
	if err != nil || len(stored) != 3 {
		t.Fatalf("stored approvals %+v, %v", stored, err)
	}
	for _, a := range stored {
		var fp string
		for _, c := range g.Contexts {
			if c.KubeContext == a.Context {
				fp = c.Fingerprint
			}
		}
		if a.File != b.file || fp == "" || a.Fingerprint != fp || a.Command != "aws" {
			t.Errorf("stored %+v, the dialog listed %q", a, fp)
		}
	}
	rest := b.pending(t, "")
	if len(rest) != 1 || !slices.Equal(kubeContexts(rest[0]), []string{"other"}) {
		t.Errorf("still pending: %+v", rest)
	}
	if got := b.pending(t, "a-eks [config]"); len(got) != 0 {
		t.Errorf("an allowed context is still listed: %+v", got)
	}
	s2 := New(Options{Connector: &ClusterConnector{Sources: NewKubeconfigSources(b.k.Dir())}})
	defer s2.Close()
	if info, err := s2.ExecPlugin("c-eks [config]"); err != nil || info.State != ExecApproved {
		t.Errorf("after a restart: %+v, %v", info, err)
	}
}

// TestApprovalIsRefusedAsAWholeWhenOneContextChanged: the server of one context changes between the
// listing and the approval. Nothing is approved, and the error names that context and no other.
func TestApprovalIsRefusedAsAWholeWhenOneContextChanged(t *testing.T) {
	b := newBundleSession(t, eks("a-eks", getToken...), eks("b-eks", getToken...), eks("c-eks", getToken...))
	g := b.pending(t, "")[0]
	moved := eks("b-eks", getToken...)
	moved.server = "https://evil.example:443"
	b.rewrite(t, eks("a-eks", getToken...), moved, eks("c-eks", getToken...))

	_, err := b.s.ApproveExecAll(itemsOf(g))
	var changed *ExecChangedError
	if !errors.Is(err, ErrExecChangedSinceShown) || !errors.As(err, &changed) {
		t.Fatalf("err = %v, want ErrExecChangedSinceShown", err)
	}
	if !slices.Equal(changed.Contexts, []string{"b-eks [config]"}) || !strings.Contains(err.Error(), "b-eks [config]") ||
		strings.Contains(err.Error(), "a-eks") || strings.Contains(err.Error(), "c-eks") {
		t.Errorf("the error names %v: %v", changed.Contexts, err)
	}
	if stored, _ := b.k.approvals(); len(stored) != 0 {
		t.Errorf("a refused approval stored %+v", stored)
	}
	// Listed again, the moved context still bundles with the others (the identity does not bind the
	// server), now with its new server and fingerprint.
	again := b.pending(t, "")
	if len(again) != 1 || len(again[0].Contexts) != 3 {
		t.Fatalf("after the change: %+v", again)
	}
	if _, err := b.s.ApproveExecAll(itemsOf(again[0])); err != nil {
		t.Errorf("approving what is listed now: %v", err)
	}
}

// TestApprovalRefusesContextsThatLeftAndDuplicatesAndNothing: a context that left the file is named like
// a changed one, a context listed twice or an empty list is refused.
func TestApprovalRefusesContextsThatLeftAndDuplicatesAndNothing(t *testing.T) {
	b := newBundleSession(t, eks("a-eks", getToken...), eks("b-eks", getToken...))
	g := b.pending(t, "")[0]
	b.rewrite(t, eks("a-eks", getToken...))
	var changed *ExecChangedError
	if _, err := b.s.ApproveExecAll(itemsOf(g)); !errors.As(err, &changed) || !slices.Equal(changed.Contexts, []string{"b-eks [config]"}) {
		t.Errorf("a context that left: %v", err)
	}
	items := itemsOf(g)
	if _, err := b.s.ApproveExecAll(append(items, items[0])); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Errorf("a duplicate: %v", err)
	}
	if _, err := b.s.ApproveExecAll(nil); err == nil {
		t.Error("an empty approval succeeded")
	}
	if stored, _ := b.k.approvals(); len(stored) != 0 {
		t.Errorf("refused approvals stored %+v", stored)
	}
}

// TestAContextAddedLaterAsksOnItsOwn: after the group was allowed, a context with the same plugin that
// appears in the file asks alone; two new ones ask together; the old approvals stay.
func TestAContextAddedLaterAsksOnItsOwn(t *testing.T) {
	b := newBundleSession(t, eks("a-eks", getToken...), eks("b-eks", getToken...), eks("c-eks", getToken...))
	if _, err := b.s.ApproveExecAll(itemsOf(b.pending(t, "")[0])); err != nil {
		t.Fatal(err)
	}
	old := []bundleCtx{eks("a-eks", getToken...), eks("b-eks", getToken...), eks("c-eks", getToken...)}

	b.rewrite(t, append(slices.Clone(old), eks("d-eks", getToken...))...)
	got := b.pending(t, "")
	if len(got) != 1 || !slices.Equal(kubeContexts(got[0]), []string{"d-eks"}) {
		t.Fatalf("a new context: %+v", got)
	}
	if one := b.pending(t, "d-eks [config]"); len(one) != 1 || len(one[0].Contexts) != 1 {
		t.Errorf("the group of the new context lists %+v", one)
	}
	for _, name := range []string{"a-eks", "b-eks", "c-eks"} {
		if st := execStates(t, b)[name]; st != ExecApproved {
			t.Errorf("%s is %q after another context was added", name, st)
		}
	}

	b.rewrite(t, append(slices.Clone(old), eks("d-eks", getToken...), eks("e-eks", getToken...))...)
	got = b.pending(t, "")
	if len(got) != 1 || !slices.Equal(kubeContexts(got[0]), []string{"d-eks", "e-eks"}) {
		t.Fatalf("two new contexts: %+v", got)
	}
}

// TestAChangedPluginAsksAgainForTheWholeGroup: the plugin of all three contexts changes; they are
// "changed" and ask together; one that changed alone splits off.
func TestAChangedPluginAsksAgainForTheWholeGroup(t *testing.T) {
	b := newBundleSession(t, eks("a-eks", getToken...), eks("b-eks", getToken...), eks("c-eks", getToken...))
	if _, err := b.s.ApproveExecAll(itemsOf(b.pending(t, "")[0])); err != nil {
		t.Fatal(err)
	}
	newArgs := append(slices.Clone(getToken), "--role", "admin")
	b.rewrite(t, eks("a-eks", newArgs...), eks("b-eks", newArgs...), eks("c-eks", getToken...))
	got := b.pending(t, "")
	if len(got) != 1 || !slices.Equal(kubeContexts(got[0]), []string{"a-eks", "b-eks"}) {
		t.Fatalf("groups %+v", got)
	}
	for _, c := range got[0].Contexts {
		if c.State != ExecChanged {
			t.Errorf("%s is %q, want changed", c.KubeContext, c.State)
		}
	}
	if st := execStates(t, b)["c-eks"]; st != ExecApproved {
		t.Errorf("the untouched context is %q", st)
	}
}

// TestTokenFileApprovalsBundleToo (D-17): contexts that name the same token file outside the folder
// ask once, listing each server; another token file is another group.
func TestTokenFileApprovalsBundleToo(t *testing.T) {
	outside := t.TempDir()
	tok1, tok2 := filepath.Join(outside, "one.token"), filepath.Join(outside, "two.token")
	writeUserFile(t, tok1, "secret-one\n")
	writeUserFile(t, tok2, "secret-two\n")
	tf := func(name, token string) bundleCtx {
		return bundleCtx{name: name, server: "https://" + name + ".example:6443", tokenFile: token}
	}
	b := newBundleSession(t, tf("s1", tok1), tf("s2", tok1), tf("s3", tok2))
	groups := b.pending(t, "")
	if len(groups) != 2 {
		t.Fatalf("groups %+v", groups)
	}
	g := groups[0]
	if g.Kind != kubeconfig.LoginTokenFile || g.Plugin.TokenFile != tok1 || !slices.Equal(kubeContexts(g), []string{"s1", "s2"}) {
		t.Errorf("group %+v", g)
	}
	if g.Contexts[0].Server != "https://s1.example:6443" || g.Contexts[1].Server != "https://s2.example:6443" {
		t.Errorf("servers %+v", g.Contexts)
	}
	if _, err := b.s.ApproveExecAll(itemsOf(g)); err != nil {
		t.Fatal(err)
	}
	if got := b.pending(t, ""); len(got) != 1 || !slices.Equal(kubeContexts(got[0]), []string{"s3"}) {
		t.Errorf("after the first group: %+v", got)
	}
}

// TestPendingApprovalsHoldNoSecrets: neither the listing nor the approval's result carries an
// environment value.
func TestPendingApprovalsHoldNoSecrets(t *testing.T) {
	b := newBundleSession(t, eks("a-eks", getToken...))
	groups := b.pending(t, "")
	approved, err := b.s.ApproveExecAll(itemsOf(groups[0]))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []any{groups, approved} {
		if out := mustMarshal(t, v); strings.Contains(out, sourceSecret) {
			t.Errorf("an environment value is in %s", out)
		}
	}
	if !slices.Equal(groups[0].Plugin.EnvNames, []string{"SECRET_ENV"}) {
		t.Errorf("env names %v", groups[0].Plugin.EnvNames)
	}
}

func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
