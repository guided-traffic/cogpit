package fyneui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The proxy settings (G13; engine/proxy.go): a cluster's Settings › Proxy, one field for the
// proxy of its API traffic (Lens's page, captured: one field with the placeholder
// "http://<address>:<port>"), and Preferences › Proxy, the proxy of the traffic that is no
// cluster's (Lens's Preferences have an HTTP proxy for it; the section's place and wording are
// Cogpit's, inferred). The field shows the engine's redacted form ("http://alice:•••@proxy:3128")
// and sets what is typed after a typing pause; a value that still shows the password as ••• keeps
// the one set before, so the host can be changed without typing it again. The engine refuses a
// proxy from anywhere but this window, and every refusal shows under the field without the value.

// proxySetting is the setting's name on both pages (its control and its refusal line).
const proxySetting = "proxy"

// proxySectionLabel is the Proxy section's title on both pages (Lens's, captured on the cluster's).
const proxySectionLabel = "Proxy"

// clusterProxySection builds a cluster's Settings › Proxy.
func (p *settingsPage) clusterProxySection() fyne.CanvasObject {
	entry := newFieldEntry() // Escape closes the card (D-116, D-118)
	entry.SetPlaceHolder(engine.ProxyPlaceholder)
	entry.OnChanged = func(string) {
		p.typed(proxySetting, func() { p.setCluster(proxySetting, strings.TrimSpace(entry.Text)) })
	}
	note := newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	note.truncate = true
	p.controls["proxyNote"] = note
	p.sync = func() {
		v := p.clusterView()
		p.setEntry(entry, &entry.Entry, proxySetting, v.Proxy)
		switch {
		case engine.IsDemoContext(p.shown.Context):
			entry.Disable()
			note.SetText("The demo cluster is generated in memory: it has no traffic to route.")
		case v.Connected:
			entry.Enable()
			note.SetText("Applies at the next connect, after Disconnect; the connection open now keeps its way to the server.")
		default:
			entry.Enable()
			note.SetText("Applies when Cogpit next connects to this cluster.")
		}
	}
	return container.NewVBox(
		p.setting(proxySetting, "HTTP proxy",
			"http://, https:// or socks5://, with user:password@ if asked; replaces the kubeconfig's proxy-url for Cogpit only.",
			container.NewVBox(entry, note)),
	)
}

// httpProxySetting is the Preferences' proxy setting's name (preferences.set's parameter).
const httpProxySetting = "httpProxy"

// preferencesProxySection builds Preferences › Proxy: the proxy of the traffic that is no
// cluster's, the Helm repositories' downloads. Lens's "Allow untrusted certificate authorities"
// beside it is not built (a question for the owner: it weakens TLS for those downloads).
func (p *settingsPage) preferencesProxySection() fyne.CanvasObject {
	entry := newFieldEntry() // Escape closes the card (D-116, D-118)
	entry.SetPlaceHolder("The environment's (HTTPS_PROXY)")
	entry.OnChanged = func(string) {
		p.typed(httpProxySetting, func() { p.setPreference(httpProxySetting, strings.TrimSpace(entry.Text)) })
	}
	p.sync = func() {
		p.setEntry(entry, &entry.Entry, httpProxySetting, p.prefs().HTTPProxy) // the UI state's: its password as •••
	}
	return container.NewVBox(
		p.setting(httpProxySetting, "HTTP proxy for non-cluster traffic",
			"Downloads from Helm repositories go through it: http://<address>:<port>, https:// or socks5://; clusters use their own.",
			entry),
	)
}
