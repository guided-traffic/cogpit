package commands

import (
	"context"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A Helm repository's user and password over plain http (credentials H-40, owner): such a
// repository downloads nothing until the user allowed it, once, in Cogpit's window; the allowance
// is bound to its name, URL and user name. helm.repo.list says which repositories wait and why; the
// allowance is GUI-only, so that no agent allows its own requests (D-2).

type allowHTTPParams struct {
	Repositories []engine.HelmHTTPAllowItem `json:"repositories" jsonschema:"the repositories to allow, each with the httpToken helm_repo_list returned for it: all of them or none are allowed, and the call is refused, naming them, if any changed since"`
}

func registerHelmHTTP(g *registrar) {
	add(g, engine.Spec[allowHTTPParams]{
		ID: "helm.repo.allowHTTPCredentials", Label: "Allow Credentials over HTTP", Danger: engine.DangerUI,
		GUIOnly: engine.ErrHelmHTTPGUIOnly,
		Description: "Records that the user lets Cogpit send the user name and password of the listed Helm repositories " +
			"over plain http, unencrypted, as helm does: a repository of Helm's repositories file whose URL is http:// and " +
			"that has credentials downloads nothing before (helm_repo_list's httpCredentials says required or changed, its " +
			"error why). The allowance is bound to the repository's name, URL and user name; a change of either asks " +
			"again, a changed password does not. Each repository's httpToken is checked against Helm's file as it is now: " +
			"if any changed, nothing is allowed. Only the user's own action in Cogpit's window runs it: agents and the CLI " +
			"are refused, whatever they pass, and should ask the user to allow it in the Charts view.",
		Enabled: func(s *engine.Session, _ allowHTTPParams) error { _, err := helmRepos(s); return err },
		Run: func(ctx context.Context, s *engine.Session, p allowHTTPParams) (any, error) {
			if engine.OriginOf(ctx) != engine.OriginGUI { // Dispatch refuses it first (GUIOnly); kept as the last guard
				return nil, engine.ErrHelmHTTPGUIOnly
			}
			h, err := helmRepos(s)
			if err != nil {
				return nil, err
			}
			return h.AllowHTTP(p.Repositories)
		},
	})
}
