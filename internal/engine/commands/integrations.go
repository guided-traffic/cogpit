package commands

import (
	"context"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The question of a new cluster's integrations (D-76; engine/integrationsask.go): the window asks,
// for a connected cluster the user made no choice of integrations for, which ones Cogpit reads;
// ui_state carries it as integrationsQuestion, with its step and its ticks. Since D-82
// (engine/integrationsconsent.go) it opens on a pre-choice and nothing is looked for before:
// cluster.integrations.step moves it (Check all, Choose, Don't use, back to the pre-choice),
// cluster.integrations.lookFor ticks the rows (a ticked integration is looked for at once),
// cluster.integrations.answer answers it (the choice becomes the cluster's, and with saveAsDefault
// the default for new clusters too), cluster.integrations.later puts it off for this run, and
// cluster.integrations.ask asks it again. All change only Cogpit's own settings and what it reads,
// so they are UI commands. The question asks the user's consent to what Cogpit looks for with the
// user's identity, so only the user answers it, in Cogpit's window (H-126, the owner's decision of
// 2026-10-19: "Nein"; engine/integrationsguionly.go): step, lookFor and answer are GUI-only, as is
// switching an integration on in cluster.settings.set and preferences.set. Later and Ask again stay
// open to agents and the CLI: both make Cogpit read less, never more (the lead's decision,
// 2026-10-19).

// refuseQuestion refuses the question's steps, ticks and answer from anywhere but the window
// (Dispatch refuses them first, the commands being GUI-only; Run checks again).
func refuseQuestion(ctx context.Context) error {
	if engine.OriginOf(ctx) != engine.OriginGUI {
		return engine.ErrIntegrationsGUIOnly
	}
	return nil
}

type integrationsAnswerParams struct {
	Context       string          `json:"context,omitempty" jsonschema:"context id as cluster_contexts lists it (a bare context name works when one file only defines it); empty or omitted: the connected context, which is the one the window asks about (ui_state's integrationsQuestion)"`
	Integrations  map[string]bool `json:"integrations,omitempty" jsonschema:"the integrations switched on (true) or off (false), by name: metrics-server, prometheus, alertmanager, trivy, kyverno; those left out stay as they apply now: as the question ticks them (ui_state's integrationsLookFor), none for a cluster the window does not ask about"`
	SaveAsDefault bool            `json:"saveAsDefault,omitempty" jsonschema:"also keep the answer as the default for new clusters (the question's checkbox Save as the default for new clusters), which the question's Check all looks for"`
}

type integrationsLaterParams struct {
	Context string `json:"context,omitempty" jsonschema:"context id as ui_state's integrationsQuestion names it; empty or omitted: the connected context"`
}

type integrationsStepParams struct {
	Context string `json:"context,omitempty" jsonschema:"context id as ui_state's integrationsQuestion names it; empty or omitted: the connected context"`
	Step    string `json:"step" jsonschema:"one of preChoice, checkAll, choose, dontUse: the question's page: preChoice the three choices (nothing looked for); checkAll the rows with every integration the default for new clusters keeps on ticked and looked for at once; choose the rows with none ticked; dontUse the warning of what goes without integrations"`
}

type integrationsLookForParams struct {
	Context      string          `json:"context,omitempty" jsonschema:"context id as ui_state's integrationsQuestion names it; empty or omitted: the connected context"`
	Integrations map[string]bool `json:"integrations" jsonschema:"the rows ticked (true: looked for at once) or unticked (false: asked nothing more), by name: metrics-server, prometheus, alertmanager, trivy, kyverno; those left out stay as they are"`
}

type integrationsAskParams struct {
	Context string `json:"context,omitempty" jsonschema:"context id as cluster_contexts lists it (a bare context name works when one file only defines it); empty or omitted: the connected context"`
}

// askedAbout resolves a question command's context and says why the window does not ask about it.
func askedAbout(s *engine.Session, name string) error {
	id, err := settingsContext(s, name)
	if err != nil {
		return err
	}
	if s.UIState().IntegrationsQuestion != id {
		return engine.ErrNoIntegrationsQuestion
	}
	return nil
}

func registerIntegrations(g *registrar) {
	add(g, engine.Spec[integrationsAnswerParams]{
		ID: "cluster.integrations.answer", Label: "Choose Integrations", Danger: engine.DangerUI,
		GUIOnly: engine.ErrIntegrationsGUIOnly, // H-126: the consent is the user's
		Description: "Answers the window's question about a cluster's integrations (ui_state's integrationsQuestion), as its " +
			"Apply button does (and the warning's Yes of Don't use integrations, with every one off): starting from what " +
			"applies now (the question's ticks; none for a cluster the window does not ask about), the integrations named " +
			"are switched on or off and the result is the cluster's own choice; with saveAsDefault it is also the default " +
			"for new clusters. Works for a cluster not asked about too. Off, an integration is asked nothing and shows " +
			"nothing; on, Cogpit reads what the cluster runs with the user's identity and never writes to it; " +
			"cluster_settings_get lists what Cogpit found of each. Answering consents to these reads, so only the user, in " +
			"Cogpit's window, answers: refused for agents and the CLI." + clusterSettingsOnly + " Returns the cluster's settings.",
		Enabled: func(s *engine.Session, p integrationsAnswerParams) error {
			_, err := settingsContext(s, p.Context)
			return err
		},
		Refuse: func(ctx context.Context, _ *engine.Session, _ integrationsAnswerParams) error {
			return refuseQuestion(ctx)
		},
		Run: func(ctx context.Context, s *engine.Session, p integrationsAnswerParams) (any, error) {
			if err := refuseQuestion(ctx); err != nil {
				return nil, err
			}
			on, off, err := engine.ParseIntegrationSwitches(p.Integrations)
			if err != nil {
				return nil, err
			}
			id, err := settingsContext(s, p.Context)
			if err != nil {
				return nil, err
			}
			if err := offered(s, id); err != nil {
				return nil, err
			}
			if _, err := s.AnswerIntegrations(id, on, off, p.SaveAsDefault); err != nil {
				return nil, err
			}
			return settingsView(s, id)
		},
	})

	add(g, engine.Spec[integrationsLaterParams]{
		ID: "cluster.integrations.later", Label: "Ask Later", Danger: engine.DangerUI,
		Description: "Puts off the window's question about a cluster's integrations, as its Later button does: it is not " +
			"asked again this run, and asked again at the next start's connect until it is answered. Meanwhile nothing of " +
			"the cluster's integrations is read; the user chooses them in its Settings › Integrations, cluster_integrations_ask " +
			"asks again. Changes nothing but the window. Returns the new UI state.",
		Enabled: func(s *engine.Session, p integrationsLaterParams) error {
			return askedAbout(s, p.Context)
		},
		Run: func(_ context.Context, s *engine.Session, p integrationsLaterParams) (any, error) {
			id, err := settingsContext(s, p.Context)
			if err != nil {
				return nil, err
			}
			if err := s.PutOffIntegrationsQuestion(id); err != nil {
				return nil, err
			}
			return s.UIState(), nil
		},
	})

	add(g, engine.Spec[integrationsStepParams]{
		ID: "cluster.integrations.step", Label: "Integrations Question Step", Danger: engine.DangerUI,
		GUIOnly: engine.ErrIntegrationsGUIOnly, // H-126: Check all looks for the integrations with the user's identity
		Description: "Moves the window's question about a cluster's integrations (ui_state's integrationsQuestion and " +
			"integrationsStep) to a page, as its buttons do: preChoice, the three choices, where nothing is looked for (the " +
			"rows' Back, the warning's No); checkAll, Check all integrations: the rows, every integration the default for " +
			"new clusters keeps on ticked and looked for at once, with the user's identity; choose, Choose integrations: the " +
			"rows, none ticked, each looked for once ticked (cluster_integrations_lookFor); dontUse, Don't use integrations: " +
			"the warning that the cluster then shows no metrics, alerts, image ratings or policy marks (its resources still " +
			"show), whose Yes is cluster_integrations_answer with every one off. Changes nothing but the window and what is " +
			"looked for. Only the user, in Cogpit's window, moves it: refused for agents and the CLI. Returns the new UI state.",
		Enabled: func(s *engine.Session, p integrationsStepParams) error {
			return askedAbout(s, p.Context)
		},
		Refuse: func(ctx context.Context, _ *engine.Session, _ integrationsStepParams) error {
			return refuseQuestion(ctx)
		},
		Run: func(ctx context.Context, s *engine.Session, p integrationsStepParams) (any, error) {
			if err := refuseQuestion(ctx); err != nil {
				return nil, err
			}
			step, err := engine.ParseIntegrationsStep(p.Step)
			if err != nil {
				return nil, err
			}
			id, err := settingsContext(s, p.Context)
			if err != nil {
				return nil, err
			}
			if err := s.StepIntegrationsQuestion(id, step); err != nil {
				return nil, err
			}
			return s.UIState(), nil
		},
	})

	add(g, engine.Spec[integrationsLookForParams]{
		ID: "cluster.integrations.lookFor", Label: "Look for Integrations", Danger: engine.DangerUI,
		GUIOnly: engine.ErrIntegrationsGUIOnly, // H-126: a tick looks for the integration with the user's identity
		Description: "Ticks or unticks rows of the window's question about a cluster's integrations (ui_state's " +
			"integrationsLookFor), as its checkboxes do, after Check all or Choose: a ticked integration is looked for at " +
			"once with the user's identity and read as one switched on until the question ends; an unticked one is asked " +
			"nothing more. cluster_settings_get lists what Cogpit found of each; cluster_integrations_answer applies the " +
			"ticks. Changes nothing but the window and what is looked for. Only the user, in Cogpit's window, ticks them: " +
			"refused for agents and the CLI. Returns the new UI state.",
		Enabled: func(s *engine.Session, p integrationsLookForParams) error {
			id, err := settingsContext(s, p.Context)
			if err != nil {
				return err
			}
			u := s.UIState()
			return u.IntegrationsQuestionRows(id)
		},
		Refuse: func(ctx context.Context, _ *engine.Session, _ integrationsLookForParams) error {
			return refuseQuestion(ctx)
		},
		Run: func(ctx context.Context, s *engine.Session, p integrationsLookForParams) (any, error) {
			if err := refuseQuestion(ctx); err != nil {
				return nil, err
			}
			on, off, err := engine.ParseIntegrationSwitches(p.Integrations)
			if err != nil {
				return nil, err
			}
			id, err := settingsContext(s, p.Context)
			if err != nil {
				return nil, err
			}
			if err := s.LookForIntegrations(id, on, off); err != nil {
				return nil, err
			}
			return s.UIState(), nil
		},
	})

	add(g, engine.Spec[integrationsAskParams]{
		ID: "cluster.integrations.ask", Label: "Ask About Integrations Again", Danger: engine.DangerUI,
		Description: "Asks the question about a cluster's integrations again, as Cluster Settings › Integrations' Ask again " +
			"does: the cluster's choice of integrations is forgotten (and a Later of this run), nothing of its integrations " +
			"is read from then on, and the window asks again from the pre-choice while the cluster is shown and connected " +
			"(ui_state's integrationsQuestion). Not for the demo cluster, nor while the window asks about the cluster." +
			clusterSettingsOnly + " Returns the cluster's settings.",
		Enabled: func(s *engine.Session, p integrationsAskParams) error {
			id, err := settingsContext(s, p.Context)
			if err != nil {
				return err
			}
			return s.CanAskIntegrationsAgain(id)
		},
		Run: func(_ context.Context, s *engine.Session, p integrationsAskParams) (any, error) {
			id, err := settingsContext(s, p.Context)
			if err != nil {
				return nil, err
			}
			if err := offered(s, id); err != nil {
				return nil, err
			}
			if err := s.AskIntegrationsAgain(id); err != nil {
				return nil, err
			}
			return settingsView(s, id)
		},
	})
}
