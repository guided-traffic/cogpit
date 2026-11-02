package engine

import (
	"errors"
	"fmt"
	"strings"
)

// Consent before looking (D-82, the owner's WF5 of 2026-10-17, verbatim in decisions.md: a short
// pre-choice, since the "Suchen" run with the user's privileges). Every integration is looked for
// with the user's identity: the Metrics Server by a discovery request, Prometheus and Alertmanager
// through the API server's service proxy, Trivy's and Kyverno's reports by listing them. So in a
// session that keeps cluster settings (the
// window's) a cluster the user made no choice for reads nothing of any integration
// (integrationsOffOf): not while its question is put off with Later, not while it is connected in
// the background, not before the question is answered. Only what the question looks for is read,
// and only while it asks.
//
// The question opens on a pre-choice (IntegrationsPreChoice), which looks for nothing: "Use my
// default integrations" answers at once with the default for new clusters (the owner's QW4: no
// second page; cluster.integrations.answer), "Choose integrations" (IntegrationsChoose) shows the
// rows and looks for none until the user ticks one (cluster.integrations.lookFor), and "Don't use
// integrations" (IntegrationsDontUse) shows a last warning of what goes; its Yes answers with every
// integration off, its No goes back to the pre-choice. The rows lead back to it too. What the
// question ticks (UIState.IntegrationsLookFor) is read as an integration switched on is, behind the
// dialog: its looking is its reading. Apply makes the ticks the cluster's choice
// (cluster.integrations.answer).
//
// "Ask again" (cluster.integrations.ask, Cluster Settings › Integrations) forgets a cluster's
// choice, and a Later of this run, so that the window asks again from the pre-choice while the
// cluster is shown and connected; meanwhile nothing of it is read.
//
// A session that keeps no cluster settings (the CLI's) asks nothing and keeps the default for new
// clusters as before (H-120); the demo cluster, never asked, keeps it too: it is generated, and
// nothing of it is looked for in a cluster.

// IntegrationsStep is the page the question of a cluster's integrations shows (D-82).
type IntegrationsStep string

// The question's steps.
const (
	// IntegrationsPreChoice: the three choices; nothing is looked for.
	IntegrationsPreChoice IntegrationsStep = "preChoice"
	// IntegrationsChoose: the rows, none ticked; an integration is looked for once ticked.
	IntegrationsChoose IntegrationsStep = "choose"
	// IntegrationsDontUse: the warning of what goes without integrations, over the pre-choice; Yes
	// answers with every integration off, No goes back to the pre-choice. Nothing is looked for.
	IntegrationsDontUse IntegrationsStep = "dontUse"
)

// IntegrationsSteps are the steps in the order the question offers them.
var IntegrationsSteps = []IntegrationsStep{IntegrationsPreChoice, IntegrationsChoose, IntegrationsDontUse}

// ParseIntegrationsStep reads a step's name.
func ParseIntegrationsStep(name string) (IntegrationsStep, error) {
	for _, st := range IntegrationsSteps {
		if strings.EqualFold(strings.TrimSpace(name), string(st)) {
			return st, nil
		}
	}
	names := make([]string, len(IntegrationsSteps))
	for i, st := range IntegrationsSteps {
		names[i] = string(st)
	}
	return "", fmt.Errorf("step %q: one of %s", name, strings.Join(names, ", "))
}

// ErrIntegrationsNotRows disables cluster.integrations.lookFor while the question shows no rows.
var ErrIntegrationsNotRows = errors.New("the question shows no integrations to tick: choose Choose integrations first (cluster.integrations.step)")

// ErrIntegrationsAskedNow disables cluster.integrations.ask while the window asks about the cluster.
var ErrIntegrationsAskedNow = errors.New("the window asks about this cluster's integrations now")

// ErrDemoNotAsked disables cluster.integrations.ask for the demo cluster.
var ErrDemoNotAsked = errors.New("the demo cluster is never asked: its metrics are its own and nothing of it is looked for in a cluster")

// syncIntegrationsStep keeps the question's step and ticks in step with the question: a question
// that ends, or turns to another cluster, starts again at the pre-choice with nothing ticked.
func (u *UIState) syncIntegrationsStep(prev string) {
	if u.IntegrationsQuestion == "" {
		u.IntegrationsStep, u.IntegrationsLookFor = "", 0
		return
	}
	if u.IntegrationsQuestion != prev || u.IntegrationsStep == "" {
		u.IntegrationsStep, u.IntegrationsLookFor = IntegrationsPreChoice, 0
	}
}

// demoCluster reports whether a context is the demo cluster, or connected as one (Connection.Demo).
func (u *UIState) demoCluster(context string) bool {
	if IsDemoContext(context) || (context != "" && context == u.Context && u.Connection.Demo) {
		return true
	}
	i := u.connectionIndex(context, 0)
	return i >= 0 && u.Connections[i].Connection.Demo
}

// IntegrationsOffNow returns the integrations off for a context now: its choice, else (a session
// that asks) every one but those its question ticks, else the default for new clusters. Every
// change of its switches starts from it.
func (s *Session) IntegrationsOffNow(context string) IntegrationSet {
	u := s.UIState()
	return integrationsOffOf(&u, context)
}

// StepIntegrationsQuestion moves the question of a context to a step (D-82): the pre-choice and
// the warning look for nothing, Choose for nothing until ticked. It changes nothing but the window
// and what is looked for: every step starts with nothing ticked.
func (s *Session) StepIntegrationsQuestion(context string, step IntegrationsStep) error {
	if _, err := ParseIntegrationsStep(string(step)); err != nil {
		return err
	}
	if s.UIState().IntegrationsQuestion != context || context == "" {
		return ErrNoIntegrationsQuestion
	}
	s.UpdateUI(func(u *UIState) {
		if u.IntegrationsQuestion == context { // still asked: the answer may have come meanwhile
			u.IntegrationsStep, u.IntegrationsLookFor = step, 0
		}
	})
	return nil
}

// IntegrationsQuestionRows reports whether the question of a context shows its rows (Choose), where
// integrations are ticked; the error says why not.
func (u *UIState) IntegrationsQuestionRows(context string) error {
	if context == "" || u.IntegrationsQuestion != context {
		return ErrNoIntegrationsQuestion
	}
	if u.IntegrationsStep != IntegrationsChoose {
		return ErrIntegrationsNotRows
	}
	return nil
}

// LookForIntegrations ticks integrations in the question's rows (on) and unticks others (off): a
// ticked one is looked for at once, and read as an integration switched on is until the question
// ends; an unticked one is asked nothing more. Apply (AnswerIntegrations) makes the ticks the
// cluster's choice.
func (s *Session) LookForIntegrations(context string, on, off IntegrationSet) error {
	u := s.UIState()
	if err := u.IntegrationsQuestionRows(context); err != nil {
		return err
	}
	s.UpdateUI(func(u *UIState) {
		if u.IntegrationsQuestionRows(context) == nil {
			u.IntegrationsLookFor = (u.IntegrationsLookFor | on) &^ off & IntAll
		}
	})
	return nil
}

// CanAskIntegrationsAgain says why the question of a context's integrations cannot be asked again:
// a session without cluster settings, the demo cluster, a cluster the window asks about now; nil
// when it can.
func (s *Session) CanAskIntegrationsAgain(context string) error {
	if s.opts.ClusterSettings == nil {
		return ErrNoClusterSettings
	}
	u := s.UIState()
	switch {
	case u.demoCluster(context):
		return ErrDemoNotAsked
	case context != "" && u.IntegrationsQuestion == context:
		return ErrIntegrationsAskedNow
	}
	return nil
}

// AskIntegrationsAgain forgets a context's choice of integrations and a Later of this run, so that
// the window asks again from the pre-choice while the cluster is shown and connected (D-82, the
// owner's WF4: "Gibt es eine Möglichkeit den Dialog nochmal angezeigt zu bekommen?"). Until it is
// answered, nothing of the cluster's integrations is read. A cluster with neither is asked when it
// is shown and connected anyway: nothing changes for it. It changes only Cogpit's own settings.
func (s *Session) AskIntegrationsAgain(context string) error {
	if err := s.CanAskIntegrationsAgain(context); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.integrationsLater, context)
	s.mu.Unlock()
	_, err := s.ChangeClusterSettings(context, func(cs *ClusterSettings) error {
		cs.IntegrationsOff = nil
		return nil
	})
	return err
}
