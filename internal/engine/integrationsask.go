package engine

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// The question of a new cluster's integrations (D-76, the owner 2026-10-17: "Wenn man einen Cluster
// neu hinzufügt für den noch keine Einstellung getroffen wurde, soll der Nutzer gefragt werden,
// welche Integrationen er aktivieren möchte"; since D-82 with a pre-choice before anything is looked
// for, integrationsconsent.go). While the cluster the window shows is connected and the user made no
// choice of integrations for it (ClusterSettings.IntegrationsOff unset), the UI state carries the
// question (UIState.IntegrationsQuestion, its step and its ticks), so that agents see it in ui_state
// as the window shows it: a dialog over the workspace that opens on the pre-choice, then each
// integration's switch and what Cogpit found of it, the checkbox "Save as the default for new
// clusters", and Apply and Later. Nothing waits for it: the connect and the views go on behind it,
// and until it is answered nothing of the cluster's integrations is read but what the question
// looks for (D-82). Answered (cluster.integrations.answer), the choice is the cluster's; put off
// (cluster.integrations.later), it is not asked again this run and asked again at the next start's
// connect, until answered, and nothing is read meanwhile.
//
// The demo cluster is never asked (its metrics are its own, it runs nothing to find), nor is a
// session that keeps no cluster settings (nothing could keep the answer: the CLI's); both keep the
// default for new clusters.
//
// While the question or the cluster's Settings › Integrations shows a connected cluster, its metrics
// providers that are on (for the question: ticked) and that nothing looked for yet are detected once
// (metrics.Hub.DetectUnknown, bounded as D-71 bounds detection), so that both say "found" right
// after the connect or the tick instead of "checking" until a drawer opens. Nothing is polled for it.

// ErrNoIntegrationsQuestion disables cluster.integrations.later for a context the window does not
// ask about.
var ErrNoIntegrationsQuestion = errors.New("the window asks nothing about this cluster's integrations")

// syncIntegrationsQuestion derives IntegrationsQuestion from the shown cluster, its connection and
// its settings, and keeps the question's step and ticks with it; asks is false in a session that
// keeps no cluster settings, later holds the contexts put off this run. UpdateUI runs it after every
// change.
func (u *UIState) syncIntegrationsQuestion(asks bool, later map[string]bool) {
	u.IntegrationsAskFirst = asks // a session that asks reads nothing of an undecided cluster's integrations (D-82)
	prev, ctx := u.IntegrationsQuestion, u.Context
	if !asks || ctx == "" || !u.Connection.State.Live() || u.Connection.Demo || IsDemoContext(ctx) ||
		u.IntegrationsDecided(ctx) || later[ctx] {
		ctx = ""
	}
	u.IntegrationsQuestion = ctx
	u.syncIntegrationsStep(prev) // integrationsconsent.go
}

// ParseIntegrationSwitches reads integration switches by name ({"alertmanager": false,
// "prometheus": true}): the integrations switched on and those switched off. An unknown name is an
// error that lists the known ones.
func ParseIntegrationSwitches(switches map[string]bool) (on, off IntegrationSet, err error) {
	for _, name := range slices.Sorted(maps.Keys(switches)) {
		set, err := ParseIntegrationSet([]string{name})
		if err != nil {
			return 0, 0, err
		}
		if switches[name] {
			on |= set
		} else {
			off |= set
		}
	}
	if on&off != 0 {
		return 0, 0, fmt.Errorf("integrations: %s named twice, on and off", strings.Join(IntegrationNames(on&off), ", "))
	}
	return on, off, nil
}

// AnswerIntegrations answers the question of a context's integrations (D-76): starting from what
// applies now (IntegrationsOffNow: its choice; else what its question ticks, nothing while it does
// not ask, D-82; else, in a session that does not ask, the default for new clusters), the
// integrations in on are switched on and those in off off, and the result is the cluster's own
// choice; with saveAsDefault it is also the default for new clusters (Preferences.IntegrationsOff).
// It changes only Cogpit's own settings files; the connected cluster follows at once (the
// integrations' followers).
func (s *Session) AnswerIntegrations(context string, on, off IntegrationSet, saveAsDefault bool) (ClusterSettings, error) {
	base := s.IntegrationsOffNow(context)
	cs, err := s.ChangeClusterSettings(context, func(cs *ClusterSettings) error {
		cs.SetIntegrations(base, on, off)
		return nil
	})
	if err != nil || !saveAsDefault {
		return cs, err
	}
	chosen := *cs.IntegrationsOff & IntAll
	if _, err := s.ChangePreferences(func(p *Preferences) error {
		p.IntegrationsOff = chosen
		return nil
	}); err != nil {
		return cs, fmt.Errorf("the integrations of %s are kept, the default for new clusters is not: %w", context, err)
	}
	return cs, nil
}

// PutOffIntegrationsQuestion puts off the question of a context's integrations for this run
// (Later): it is asked again at the next start's connect, until answered. Meanwhile nothing of its
// integrations is read (D-82); the user can choose in the cluster's Settings › Integrations, or ask
// again there (AskIntegrationsAgain).
func (s *Session) PutOffIntegrationsQuestion(context string) error {
	if s.UIState().IntegrationsQuestion != context || context == "" {
		return ErrNoIntegrationsQuestion
	}
	s.UpdateUI(func(*UIState) {
		if s.integrationsLater == nil {
			s.integrationsLater = map[string]bool{}
		}
		s.integrationsLater[context] = true // UpdateUI holds s.mu
	})
	return nil
}

func init() { integrationFollowers = append(integrationFollowers, detectShownIntegrations) }

// detectShownIntegrations runs the detection of a connected cluster's metrics providers that are on
// and that nothing looked for yet, once, while the question or its Settings show their state
// (integrationsShown; a follower: it starts the detection in the hub's background and returns at
// once). When it ended, the window and agents hear of it (EventIntegrations, EventMetrics).
func detectShownIntegrations(st *state, b *Backend, now UIState) {
	if b == nil || b.Metrics == nil || now.connectionState(b.Context) != Connected || !integrationsShown(&now, b.Context) {
		return
	}
	detectUnknownWhileShown(st.s, b)
}

// detectUnknownWhileShown starts the hub's detection of the providers nothing looked for; when it
// ended and the cluster is still shown so, it starts the next, for a provider ticked in the question
// while the first ran (D-82). It ends when nothing is left to look for: a provider is looked for
// once, one the timeout cut short not again before the retry TTL, and a closed hub (the backend
// left or was replaced) starts none. done takes no lock but the UI state's.
func detectUnknownWhileShown(s *Session, b *Backend) {
	ctx := b.Context
	b.Metrics.DetectUnknown(func() {
		s.integrationsChanged(ctx)
		s.emit(Event{Type: EventMetrics})
		if u := s.UIState(); u.connectionState(ctx) == Connected && integrationsShown(&u, ctx) {
			detectUnknownWhileShown(s, b)
		}
	})
}

// integrationsShown reports whether the question or the cluster's Settings shows the state of a
// context's metrics integrations: Integrations, the Metrics Server's or Prometheus's own section,
// or Metrics, which names both (D-87).
func integrationsShown(u *UIState, context string) bool {
	if u.IntegrationsQuestion == context {
		return true
	}
	p := u.Settings
	if p == nil || p.Page != SettingsCluster || p.Context != context {
		return false
	}
	switch p.Section {
	case SectionIntegrations, SectionMetrics, IntegrationSection(IntegrationMetricsServer), IntegrationSection(IntegrationPrometheus):
		return true
	}
	return false
}
