package fyneui

import (
	"time"

	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// A context's client certificate in the cluster lists (D-75, the owner's F22): for a context whose
// user logs in with a client certificate, the time left or "expired". Cogpit's own: Lens's cluster
// list shows no certificate. Where it shows (the lead's decisions, open to revision):
//   - the navigator's section, the one list that chooses a cluster (D-57, ⌘P): a second line under
//     the cluster's name, "cert expires in 23 days", or just "expired" in the error colour once it
//     expired (the owner: "einfach nur 'expired' das reicht"), short enough for the sidebar's
//     width; its tooltip a line with the date and time in the user's format (D-31);
//   - the welcome page's cluster rows: a pill, "client certificate expires in 23 days", or
//     "expired" (the "certificate not checked" pill beside it is the server's certificate);
//   - the hotbar's tile: its tooltip's line, as the section's; the tile itself shows connections.
//
// Colours from the status tokens: muted while valid, the warning colour while expiring (14 days or
// the last fifth of its lifetime, kubeconfig.CertValidity.StateAt) or not valid yet, the error
// colour once expired. It never blocks, asks or warns before a connect: the owner, "ein
// Verbindungsaufbau soll dadurch nicht blockiert werden, auch wenn er fehlschlagen wird". The
// texts follow the clock: the shell's ten-second tick (shell.tick) computes them again from the
// dates, without reading the kubeconfigs or asking the cluster.

// certState is a certificate's state at now, its text after the subject ("expires in 23 days",
// "expired", "not valid yet"; kubeconfig.CertValidity.Describe but for an expired one, which the
// window names without since when: the owner) and its colour.
func certState(v *kubeconfig.CertValidity, now time.Time) (state, text string, color fyne.ThemeColorName) {
	state, text = v.StateAt(now), v.Describe(now)
	switch state {
	case kubeconfig.CertExpired:
		return state, "expired", colorStatusError
	case kubeconfig.CertNotYetValid, kubeconfig.CertExpiring:
		return state, text, colorStatusWarning
	}
	return state, text, colorMuted
}

// certLine is the navigator row's second line: "cert expires in 23 days", or "expired", and its
// colour; "" for a context without a client certificate.
func certLine(v *kubeconfig.CertValidity, now time.Time) (string, fyne.ThemeColorName) {
	if v == nil {
		return "", colorMuted
	}
	state, text, color := certState(v, now)
	if state == kubeconfig.CertExpired {
		return text, color
	}
	return "cert " + text, color
}

// certPill is the welcome page's pill: "client certificate expires in 23 days", or "expired", and
// its colour.
func certPill(v *kubeconfig.CertValidity, now time.Time) (string, fyne.ThemeColorName) {
	if v == nil {
		return "", colorMuted
	}
	state, text, color := certState(v, now)
	if state == kubeconfig.CertExpired {
		return text, color
	}
	return "client certificate " + text, color
}

// certTip is a tooltip's line about the client certificate, with its date and time in the user's
// format (D-31): "Client certificate expires in 23 days (valid until 09.11.2026 14:03)", "Client
// certificate expired (valid until …)", "Client certificate not valid yet (valid from …)"; ""
// without one.
func (sh *shell) certTip(v *kubeconfig.CertValidity) string {
	if v == nil {
		return ""
	}
	tf, now := sh.timeFormat(), sh.now()
	at := func(t time.Time) string { return tf.FormatDate(t) + " " + tf.FormatClock(t) }
	state, text, _ := certState(v, now)
	if state == kubeconfig.CertNotYetValid {
		return "Client certificate " + text + " (valid from " + at(v.NotBefore) + ")"
	}
	return "Client certificate " + text + " (valid until " + at(v.NotAfter) + ")"
}

// clientCert is a listed context's client certificate, nil for one without (or not listed).
func (sh *shell) clientCert(ctx string) *kubeconfig.CertValidity {
	if sh.side == nil || sh.side.sec == nil {
		return nil
	}
	c, ok := sh.side.sec.context(ctx)
	if !ok {
		return nil
	}
	return c.ClientCert
}

// certsTick shows the client certificates' texts as the clock moved on: the navigator's rows whose
// line changed, and the welcome page when one of its pills did (its render compares).
func (sh *shell) certsTick() {
	if sh.side != nil && sh.side.sec != nil {
		sh.side.sec.refreshCerts()
	}
	if w := sh.tabs.welcome; w != nil && w.shown {
		w.render()
	}
}
