package commands

import (
	"context"
	"strings"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// refuseProxy refuses a proxy set by the CLI or a link (engine.ErrProxyGUIOnly, engine/proxy.go):
// they may clear a proxy (an empty value), not set one. The user's window sets one; an agent's
// proxy waits for the user's acceptance whatever the allowance of agent writes (setsProxy, A20,
// owner 2026-10-14 evening). A proxy left out passes.
func refuseProxy(ctx context.Context, proxy *string) error {
	if o := engine.OriginOf(ctx); setsProxy(proxy) && o != engine.OriginGUI && !o.IsAgent() {
		return engine.ErrProxyGUIOnly
	}
	return nil
}

// setsProxy reports whether a settings change sets a proxy (not only clears it or leaves it).
func setsProxy(proxy *string) bool { return proxy != nil && strings.TrimSpace(*proxy) != "" }

// proxyEffects are the confirmation's rows for an agent's proxy: the proxy, its password never
// shown, what it carries and what it sees.
func proxyEffects(what, proxy string) []engine.Effect {
	return []engine.Effect{
		{Name: "Proxy", Value: engine.RedactProxyURL(proxy), Tone: engine.EffectNames},
		{Name: "For", Value: what},
		{Name: "Proxy sees", Value: "the hosts Cogpit talks to; the credentials too wherever TLS is not checked", Tone: engine.EffectWarning},
	}
}
