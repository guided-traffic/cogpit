package fyneui

import (
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// proxyPassword is a proxy credential typed in the tests: the window shows it only while typed.
const proxyPassword = "gui-PROXY-pw-12"

// proxyEntry is the shown Proxy section's field.
func proxyEntry(sh *shell) *widget.Entry {
	c, ok := sh.settings.controls[proxySetting].(*fyne.Container)
	if !ok {
		return nil
	}
	return entryOf(c.Objects[0])
}

// A cluster's Settings › Proxy, between General and Terminal as captured: the placeholder; a
// proxy typed with a password is kept, and shown again with the password as •••; a refused value
// shows why without quoting it; an edit of the host keeps the password.
func TestClusterProxyField(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, w, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": "kind-dev"})
	q.until(t, "the page", sh.settings.visible)
	if labels := navLabels(sh.settings); !slices.Equal(labels, []string{"General", "Proxy", "Terminal", "Namespaces", "Metrics", "Node Shell", "Agents",
		"—", "integrations", "Metrics Server", "Prometheus", "Alertmanager", "Trivy Operator", "Kyverno"}) {
		t.Fatalf("sections %q", labels)
	}
	test.Tap(sh.settings.navItems[engine.SectionProxy])
	q.until(t, "the Proxy section", func() bool { return proxyEntry(sh) != nil })
	e := proxyEntry(sh)
	if e.PlaceHolder != "http://<address>:<port>" {
		t.Errorf("placeholder %q", e.PlaceHolder)
	}

	w.Canvas().Focus(e)
	test.Type(e, "http://alice:"+proxyPassword+"@proxy.example.com:3128")
	q.until(t, "the proxy kept", func() bool {
		cs, _ := s.ClusterSettingsOf("kind-dev")
		return cs.Proxy == "http://alice:"+proxyPassword+"@proxy.example.com:3128"
	})
	if p := sh.ui.ClusterSettings["kind-dev"].Proxy; strings.Contains(p, proxyPassword) {
		t.Errorf("the window's state holds the password: %q", p)
	}

	// Shown again: redacted.
	test.Tap(sh.settings.navItems[engine.SectionGeneral])
	q.until(t, "the General section", func() bool { return sh.settings.controls["name"] != nil })
	test.Tap(sh.settings.navItems[engine.SectionProxy])
	q.until(t, "the redacted proxy", func() bool {
		e = proxyEntry(sh)
		return e != nil && e.Text == "http://alice:•••@proxy.example.com:3128"
	})

	// Another host, the password still •••: kept.
	w.Canvas().Focus(e)
	e.SetText("http://alice:•••@proxy2.example.com:8080")
	q.until(t, "the host changed", func() bool {
		cs, _ := s.ClusterSettingsOf("kind-dev")
		return cs.Proxy == "http://alice:"+proxyPassword+"@proxy2.example.com:8080"
	})

	// A refusal names the problem, not the value.
	e.SetText("ftp://alice:" + proxyPassword + "@proxy")
	q.until(t, "the refusal", func() bool { return sh.settings.errs[proxySetting].Visible() })
	if text := sh.settings.errs[proxySetting].text; strings.Contains(text, proxyPassword) || !strings.Contains(text, "http://") {
		t.Errorf("refusal %q", text)
	}
	e.SetText("")
	q.until(t, "the proxy cleared", func() bool {
		cs, _ := s.ClusterSettingsOf("kind-dev")
		return cs.Proxy == "" && !sh.settings.errs[proxySetting].Visible()
	})
}

// The Proxy section, dark and light: the PNGs of the pull request (AGENTS.md rule 7).
func TestRenderClusterSettingsProxyDark(t *testing.T) {
	renderClusterSettingsProxy(t, ThemeDark, "cluster_settings_proxy_dark.png")
}

func TestRenderClusterSettingsProxyLight(t *testing.T) {
	renderClusterSettingsProxy(t, ThemeLight, "cluster_settings_proxy_light.png")
}

func renderClusterSettingsProxy(t *testing.T, mode ThemeMode, name string) {
	s, _ := settingsEngine(t)
	mustDispatch(t, s, "cluster.settings.set", map[string]any{"context": "kind-dev", "name": "Dev cluster", "iconColor": "violet",
		"proxy": "http://alice:" + proxyPassword + "@proxy.example.com:3128"})
	sh, w, q := newEngineShell(t, s, Options{Theme: mode})
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": "kind-dev", "section": "proxy"})
	q.until(t, "the Proxy section", func() bool {
		e := proxyEntry(sh)
		return sh.settings.visible() && sh.settings.view != nil && e != nil && e.Text == "http://alice:•••@proxy.example.com:3128"
	})
	q.drain()
	savePNG(t, w, name)
}

// Preferences › Proxy, second in the list: the proxy of the traffic that is no cluster's, typed,
// kept, and shown again with its password as •••.
func TestPreferencesProxyField(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, w, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "proxy"})
	q.until(t, "the Proxy section", func() bool { return sh.settings.controls[httpProxySetting] != nil })
	if labels := navLabels(sh.settings); !slices.Equal(labels, []string{"App", "Proxy", "Kubernetes", "Editor", "Terminal", "Tables"}) {
		t.Fatalf("sections %q", labels)
	}
	e := entryOf(sh.settings.controls[httpProxySetting])
	w.Canvas().Focus(sh.settings.controls[httpProxySetting].(fyne.Focusable))
	test.Type(e, "http://bob:"+proxyPassword+"@proxy.example.com:3128")
	q.until(t, "the proxy kept", func() bool {
		p, _ := s.Preferences()
		return p.HTTPProxy == "http://bob:"+proxyPassword+"@proxy.example.com:3128"
	})
	test.Tap(sh.settings.navItems[engine.SectionApp])
	q.until(t, "the App section", func() bool { return sh.settings.controls["theme"] != nil })
	test.Tap(sh.settings.navItems[engine.SectionProxy])
	q.until(t, "the redacted proxy", func() bool {
		e := entryOf(sh.settings.controls[httpProxySetting])
		return e != nil && e.Text == "http://bob:•••@proxy.example.com:3128"
	})
}

// Preferences › Proxy, dark and light: the PNGs of the pull request (AGENTS.md rule 7).
func TestRenderPreferencesProxyDark(t *testing.T) {
	renderPreferencesProxy(t, ThemeDark, "preferences_proxy_dark.png")
}

func TestRenderPreferencesProxyLight(t *testing.T) {
	renderPreferencesProxy(t, ThemeLight, "preferences_proxy_light.png")
}

func renderPreferencesProxy(t *testing.T, mode ThemeMode, name string) {
	s, _ := settingsEngine(t)
	mustDispatch(t, s, "preferences.set", map[string]any{"theme": string(mode), "httpProxy": "http://bob:" + proxyPassword + "@proxy.example.com:3128"})
	sh, w, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "proxy"})
	q.until(t, "the Proxy section", func() bool {
		e := entryOf(sh.settings.controls[httpProxySetting])
		return sh.settings.visible() && e != nil && e.Text == "http://bob:•••@proxy.example.com:3128"
	})
	q.drain()
	savePNG(t, w, name)
}
