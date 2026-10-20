package fyneui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// overlayTexts returns the text of every label, text line and paragraph in the window's top
// overlay (a dialog), in tree order.
func overlayTexts(w fyne.Window) []string {
	overlay := w.Canvas().Overlays().Top()
	if overlay == nil {
		return nil
	}
	var out []string
	var walk func(o fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if o == nil || !o.Visible() {
			return
		}
		switch x := o.(type) {
		case *textLine:
			out = append(out, x.text)
		case *paragraph:
			out = append(out, x.text)
		case *pill:
			out = append(out, x.text)
		case *widget.Label:
			out = append(out, x.Text)
		case *widget.RichText: // a framed dialog's title (dialogframe.go)
			out = append(out, x.String())
		case *bracketLabel:
			out = append(out, x.text)
		case *fyne.Container:
			for _, c := range x.Objects {
				walk(c)
			}
		case fyne.Widget:
			for _, c := range test.WidgetRenderer(x).Objects() {
				walk(c)
			}
		}
	}
	walk(overlay)
	return out
}

func hasText(texts []string, sub string) bool {
	return indexText(texts, sub) >= 0
}

// indexText is the position of the first text containing sub, or -1.
func indexText(texts []string, sub string) int {
	return slices.IndexFunc(texts, func(t string) bool { return strings.Contains(t, sub) })
}

// approvalFake is the kubeconfig fake with a pending group that tweak changes: the group, its shared
// plugin and its one context.
func approvalFake(t *testing.T, state string, tweak func(group, plugin, ctx map[string]any)) *fakeEngine {
	f, _ := kubeconfigFake(t, state)
	f.handlers["kubeconfig.pendingApprovals"] = func(context.Context, engine.Invocation) (any, error) {
		v := pendingFixture(state)
		g := group(v)
		tweak(g, g["plugin"].(map[string]any), g["contexts"].([]any)[0].(map[string]any))
		return v, nil
	}
	return f
}

func openApproval(t *testing.T, f *fakeEngine, mode ThemeMode) (*shell, fyne.Window) {
	t.Helper()
	sh, w, _ := openApprovalQueue(t, f, mode)
	return sh, w
}

func openApprovalQueue(t *testing.T, f *fakeEngine, mode ThemeMode) (*shell, fyne.Window, *mainQueue) {
	t.Helper()
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	selectAndConnect(t, sh, q, "prod-eks [eks.yaml]")
	q.until(t, "the approval dialog", func() bool { return sh.kc.approval != nil })
	return sh, w, q
}

// untrusted tweaks the EKS context as R-11 describes: the user's real server URL, a proxy of a
// third party, certificate checks off and a CA of the kubeconfig's own.
func untrusted(_, _, ctx map[string]any) {
	ctx["tls"] = map[string]any{
		"proxyURL": "http://proxy.example:3128", "insecure": true, "serverName": "sni.example",
		"caEmbedded": true, "caFile": "/Users/me/Downloads/ca.pem",
	}
	ctx["files"] = map[string]any{"caFileSHA256": strings.Repeat("ab", 32)}
}

// TestApprovalDialogShowsProxyAndInsecure (review R-11): the dialog shows how the credential
// travels, not only the server URL: an insecure, proxied or own-CA connection as warning lines.
func TestApprovalDialogShowsProxyAndInsecure(t *testing.T) {
	_, w := openApproval(t, approvalFake(t, engine.ExecRequired, untrusted), ThemeDark)
	texts := overlayTexts(w)
	for _, want := range []string{
		"proxy.example:3128", "Certificate checks are off", "own certificate authority", "sni.example",
		"/Users/me/Downloads/ca.pem", "abababababababab…", "can read the login credential",
	} {
		if !hasText(texts, want) {
			t.Errorf("the dialog does not show %q; it shows %q", want, texts)
		}
	}
	// The warnings are the warning colour, not plain rows.
	warned := 0
	for _, o := range walkParagraphs(w) {
		if o.color == colorStatusWarning && (strings.Contains(o.text, "Certificate checks are off") ||
			strings.Contains(o.text, "proxy.example") || strings.Contains(o.text, "own certificate authority")) {
			warned++
		}
	}
	if warned < 3 {
		t.Errorf("%d of 3 warning lines are in the warning colour", warned)
	}
}

// walkParagraphs returns the paragraphs of the top overlay.
func walkParagraphs(w fyne.Window) []*paragraph {
	var out []*paragraph
	var walk func(o fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if o == nil || !o.Visible() {
			return
		}
		switch x := o.(type) {
		case *paragraph:
			out = append(out, x)
		case *fyne.Container:
			for _, c := range x.Objects {
				walk(c)
			}
		case fyne.Widget:
			for _, c := range test.WidgetRenderer(x).Objects() {
				walk(c)
			}
		}
	}
	walk(w.Canvas().Overlays().Top())
	return out
}

// TestApprovalDialogPlainTLSAddsNothing: a connection with the system's trust and no proxy shows no
// extra lines.
func TestApprovalDialogPlainTLSAddsNothing(t *testing.T) {
	_, w := openApproval(t, approvalFake(t, engine.ExecRequired, func(_, _, _ map[string]any) {}), ThemeDark)
	texts := overlayTexts(w)
	for _, not := range []string{"Certificate check", "Proxy", "Trusted CA", "TLS server name", "certificate authority"} {
		if hasText(texts, not) {
			t.Errorf("a plain connection shows %q: %q", not, texts)
		}
	}
}

// TestApprovalDialogEscapesUntrustedText: proxy URL, CA path and token file path are kubeconfig
// text and go through DisplayText.
func TestApprovalDialogEscapesUntrustedText(t *testing.T) {
	f := approvalFake(t, engine.ExecRequired, func(_, _, ctx map[string]any) {
		ctx["tls"] = map[string]any{"proxyURL": "http://pro\u202exy.example:3128", "caFile": "/ca\n.pem", "serverName": "a\u200bb"}
		ctx["kubeContext"] = "pro\u202ed"
	})
	_, w := openApproval(t, f, ThemeDark)
	texts := overlayTexts(w)
	for _, t2 := range texts {
		if strings.ContainsAny(t2, "\u202e\u200b\n") {
			t.Errorf("unescaped control characters in %q", t2)
		}
	}
	if !hasText(texts, "pro"+`\u202e`+"xy.example") || !hasText(texts, `/ca\u000a.pem`) || !hasText(texts, `pro\u202ed`) {
		t.Errorf("the escapes are visible: %q", texts)
	}
}

// TestApprovalDialogShowsProgramDigest: a program inside the kubeconfig's folder is shown with the
// digest the approval binds.
func TestApprovalDialogShowsProgramDigest(t *testing.T) {
	f := approvalFake(t, engine.ExecRequired, func(_, plugin, _ map[string]any) {
		plugin["command"] = "/Users/me/Downloads/plug"
		plugin["files"] = map[string]any{"programSHA256": strings.Repeat("cd", 32)}
	})
	_, w := openApproval(t, f, ThemeDark)
	texts := overlayTexts(w)
	if !hasText(texts, "inside the kubeconfig's folder; content SHA-256 cdcdcdcdcdcdcdcd…") || !hasText(texts, "replacing the file asks again") {
		t.Errorf("%q", texts)
	}
}

// tokenFileTweak makes the plugin a token file alone, as D-17 asks for.
func tokenFileTweak(group, plugin, _ map[string]any) {
	group["kind"] = kubeconfig.LoginTokenFile
	delete(plugin, "command")
	delete(plugin, "args")
	delete(plugin, "envNames")
	plugin["tokenFile"] = "/Users/me/secrets/prod.token"
	group["found"] = ""
	group["riskyEnv"] = nil
}

// TestApprovalDialogAsksForATokenFile (D-17): the same dialog names the file and the server its
// content goes to; there is no program to show.
func TestApprovalDialogAsksForATokenFile(t *testing.T) {
	f := approvalFake(t, engine.ExecRequired, tokenFileTweak)
	_, w := openApproval(t, f, ThemeDark)
	texts := overlayTexts(w)
	for _, want := range []string{
		"Allow this file as the login token?", "/Users/me/secrets/prod.token", "ABCDEF.gr7.eu-central-1.eks.amazonaws.com",
		"Token file", "Credential for",
	} {
		if !hasText(texts, want) {
			t.Errorf("the dialog does not show %q; it shows %q", want, texts)
		}
	}
	for _, not := range []string{"Program", "Arguments", "Environment"} {
		if hasText(texts, not) {
			t.Errorf("a token file has no %q: %q", not, texts)
		}
	}
}

// bundled is a group of contexts of one file that run the same plugin: prod-eks (the one connected
// to), stage-eks that is proxied with certificate checks off, dev-eks with its own CA, and
// test-eks that changed since the user allowed it.
func bundled(group, _, ctx map[string]any) {
	stage := eksContext("stage-eks", engine.ExecRequired, "sha256:5747")
	stage["tls"] = map[string]any{"proxyURL": "http://proxy.example:3128", "insecure": true}
	dev := eksContext("dev-eks", engine.ExecRequired, "sha256:de11")
	dev["tls"] = map[string]any{"caEmbedded": true, "serverName": "dev.sni.example"}
	testCtx := eksContext("test-eks", engine.ExecChanged, "sha256:7e57")
	group["contexts"] = []any{ctx, stage, dev, testCtx}
}

// TestApprovalDialogListsEveryContextOfThePlugin (D-28, 2026-10-12): one dialog lists the contexts of
// the file that use the plugin, each with its server and, where they apply, the R-11 warnings for
// that context only.
func TestApprovalDialogListsEveryContextOfThePlugin(t *testing.T) {
	_, w := openApproval(t, approvalFake(t, engine.ExecRequired, bundled), ThemeDark)
	texts := overlayTexts(w)
	for _, want := range []string{
		"Allow this program to log in to 4 contexts?", "the 4 contexts below", "Allowing it allows it for every context listed",
		"prod-eks", "ABCDEF.gr7.eu-central-1.eks.amazonaws.com", "stage-eks", "https://stage-eks.gr7.eu-central-1.eks.amazonaws.com",
		"dev-eks", "https://dev-eks.gr7.eu-central-1.eks.amazonaws.com", "test-eks", "https://test-eks.gr7.eu-central-1.eks.amazonaws.com",
		"1 of 4, marked changed", "4 contexts",
	} {
		if !hasText(texts, want) {
			t.Errorf("the dialog does not show %q; it shows %q", want, texts)
		}
	}
	// Each context's warnings sit under its own name and above the next context's.
	order := []string{"prod-eks", "stage-eks", "Certificate checks are off", "proxy.example:3128", "dev-eks", "own certificate authority", "test-eks"}
	last := -1
	for _, s := range order {
		i := indexText(texts, s)
		if i < 0 {
			continue
		}
		if i <= last && s != "prod-eks" {
			t.Errorf("%q at %d is not after the previous entry (%d) in %q", s, i, last, texts)
		}
		last = max(last, i)
	}
	// The warnings are only for the contexts they apply to: prod-eks has none, so nothing about a
	// proxy or an insecure connection stands between its name and the next context's.
	iProd, iStage := indexText(texts, "prod-eks"), indexText(texts, "stage-eks")
	for _, s := range texts[iProd+1 : iStage] {
		if strings.Contains(s, "Certificate checks") || strings.Contains(s, "proxy") || strings.Contains(s, "certificate authority") {
			t.Errorf("a warning between prod-eks and stage-eks: %q", s)
		}
	}
	count := func(sub string) int {
		n := 0
		for _, s := range texts {
			if strings.Contains(s, sub) {
				n++
			}
		}
		return n
	}
	if count("Certificate checks are off") != 1 || count("own certificate authority") != 1 || count("The connection goes through the proxy") != 1 {
		t.Errorf("a warning is not shown once, for its context: %q", texts)
	}
	if !hasText(texts, "changed") {
		t.Errorf("the changed context is not marked: %q", texts)
	}
	// One program, shown once.
	if count("Program") != 1 || count("get-token") != 1 {
		t.Errorf("the shared plugin is not shown once: %q", texts)
	}
}

// TestApprovalDialogOneConfirmApprovesAll: one tap sends every listed context with its own
// fingerprint in one approveExec call, and connects the context the user chose, only.
func TestApprovalDialogOneConfirmApprovesAll(t *testing.T) {
	f, rec := kubeconfigFake(t, engine.ExecRequired)
	f.handlers["kubeconfig.pendingApprovals"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		rec.add(inv)
		v := pendingFixture(engine.ExecRequired)
		g := group(v)
		bundled(g, nil, g["contexts"].([]any)[0].(map[string]any))
		return v, nil
	}
	sh, _, q := newTestShell(t, f, Options{})
	selectAndConnect(t, sh, q, "prod-eks [eks.yaml]")
	q.until(t, "the approval dialog", func() bool { return sh.kc.approval != nil })
	if got := sh.kc.approval.allow.Text; got != "Allow All 4 and Connect" {
		t.Errorf("button %q", got)
	}
	if got := rec.params("kubeconfig.pendingApprovals"); got["context"] != "prod-eks [eks.yaml]" {
		t.Errorf("the dialog asked for %v", got)
	}
	if f.connects() != nil {
		t.Fatalf("connected before the plugin was allowed: %v", f.connects())
	}
	test.Tap(sh.kc.approval.allow)
	q.until(t, "the connection", func() bool { return len(f.connects()) == 1 })
	calls := 0
	for _, id := range rec.ids() {
		if id == "kubeconfig.approveExec" {
			calls++
		}
	}
	want := []approvalItem{
		{"prod-eks [eks.yaml]", "sha256:feed"}, {"stage-eks [eks.yaml]", "sha256:5747"},
		{"dev-eks [eks.yaml]", "sha256:de11"}, {"test-eks [eks.yaml]", "sha256:7e57"},
	}
	if got := rec.approvals(); calls != 1 || !slices.Equal(got, want) {
		t.Errorf("approveExec called %d times with %v, want once with %v", calls, got, want)
	}
	if got := f.connects(); !slices.Equal(got, []string{"prod-eks [eks.yaml]"}) {
		t.Errorf("connected %v", got)
	}
}

// TestApprovalDialogRefusedAsAWhole: when the engine refuses because one context changed, the dialog
// closes, the error that names the context is shown, and nothing connects.
func TestApprovalDialogRefusedAsAWhole(t *testing.T) {
	f, rec := kubeconfigFake(t, engine.ExecRequired)
	f.handlers["kubeconfig.pendingApprovals"] = func(context.Context, engine.Invocation) (any, error) {
		v := pendingFixture(engine.ExecRequired)
		g := group(v)
		bundled(g, nil, g["contexts"].([]any)[0].(map[string]any))
		return v, nil
	}
	f.handlers["kubeconfig.approveExec"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		rec.add(inv)
		return nil, &engine.ExecChangedError{Contexts: []string{"dev-eks [eks.yaml]"}}
	}
	sh, _, q := newTestShell(t, f, Options{})
	selectAndConnect(t, sh, q, "prod-eks [eks.yaml]")
	q.until(t, "the approval dialog", func() bool { return sh.kc.approval != nil })
	toasts := len(sh.toasts.Objects)
	test.Tap(sh.kc.approval.allow)
	q.until(t, "the dialog to close", func() bool { return sh.kc.approval == nil && len(sh.toasts.Objects) > toasts })
	q.drain()
	if len(f.connects()) != 0 {
		t.Errorf("connected: %v", f.connects())
	}
	if shown := walkToastTexts(sh); !hasText(shown, "dev-eks [eks.yaml]") {
		t.Errorf("the toast does not name the context: %q", shown)
	}
}

// walkToastTexts returns the texts of the shell's toasts.
func walkToastTexts(sh *shell) []string {
	var out []string
	var walk func(o fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		switch x := o.(type) {
		case *paragraph:
			out = append(out, x.text)
		case *textLine:
			out = append(out, x.text)
		case *fyne.Container:
			for _, c := range x.Objects {
				walk(c)
			}
		case fyne.Widget:
			for _, c := range test.WidgetRenderer(x).Objects() {
				walk(c)
			}
		}
	}
	for _, o := range sh.toasts.Objects {
		walk(o)
	}
	return out
}

// manyContexts is ten contexts of one plugin, as the owner described (a config with ten EKS contexts).
func manyContexts(group, _, ctx map[string]any) {
	list := []any{ctx}
	for i := 1; i < 10; i++ {
		c := eksContext(fmt.Sprintf("eks-%02d", i), engine.ExecRequired, fmt.Sprintf("sha256:%04d", i))
		if i == 3 {
			c["tls"] = map[string]any{"insecure": true}
		}
		if i == 6 {
			c["tls"] = map[string]any{"proxyURL": "http://proxy.example:3128"}
		}
		list = append(list, c)
	}
	group["contexts"] = list
}

// TestApprovalDialogScrollsALongListOfContexts: ten contexts do not grow the dialog past the window; it
// scrolls as a whole, buttons outside. One context fits and does not scroll.
func TestApprovalDialogScrollsALongListOfContexts(t *testing.T) {
	sh, w := openApproval(t, approvalFake(t, engine.ExecRequired, manyContexts), ThemeDark)
	if sh.kc.approval.scroll == nil {
		t.Fatal("ten contexts do not scroll")
	}
	if h := w.Canvas().Overlays().Top().MinSize().Height; h > 0.92*900 {
		t.Errorf("the dialog is %v high in a window of 900", h)
	}
	if !hasText(overlayTexts(w), "Allow this program to log in to 10 contexts?") || !hasText(overlayTexts(w), "eks-09") {
		t.Error("no title or no last context for ten contexts")
	}
	sh1, _ := openApproval(t, approvalFake(t, engine.ExecRequired, untrusted), ThemeDark)
	if sh1.kc.approval.scroll != nil {
		t.Error("one context scrolls")
	}
}

func TestRenderExecApprovalUntrustedDark(t *testing.T) {
	_, w := openApproval(t, approvalFake(t, engine.ExecRequired, untrusted), ThemeDark)
	savePNG(t, w, "exec_approval_untrusted_dark.png")
}

func TestRenderTokenFileApprovalLight(t *testing.T) {
	_, w := openApproval(t, approvalFake(t, engine.ExecRequired, tokenFileTweak), ThemeLight)
	savePNG(t, w, "token_file_approval_light.png")
}

func TestRenderExecApprovalBundledDark(t *testing.T) {
	_, w := openApproval(t, approvalFake(t, engine.ExecRequired, bundled), ThemeDark)
	savePNG(t, w, "exec_approval_bundled_dark.png")
}

func TestRenderExecApprovalTenContextsLight(t *testing.T) {
	_, w := openApproval(t, approvalFake(t, engine.ExecRequired, manyContexts), ThemeLight)
	savePNG(t, w, "exec_approval_ten_contexts_light.png")
}
