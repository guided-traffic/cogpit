package commands

import (
	"context"
	"fmt"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Helm chart repositories (G11, D-29, architecture §8): the repositories Helm's own
// repositories.yaml lists. They are added and removed with the helm CLI only (helm repo add, helm
// repo remove; owner): Cogpit writes nothing into Helm's file or folders, so there is no command
// that adds or removes one. The list reads them (name, URL without a user or password, whether
// credentials are present, never a credential), and an update downloads their indexes into Cogpit's
// own cache: both run for every origin and change no cluster.

type repoUpdateParams struct {
	Names []string `json:"names,omitempty" jsonschema:"the repositories to update, by their names in Helm's repositories file; empty: every repository Cogpit uses"`
}

// helmRepos returns the session's repositories, or why there are none.
func helmRepos(s *engine.Session) (*engine.HelmRepos, error) {
	if h := s.HelmRepos(); h != nil {
		return h, nil
	}
	return nil, engine.ErrNoHelmRepos
}

func registerHelmRepos(g *registrar) {
	add(g, engine.Spec[struct{}]{
		ID: "helm.repo.list", Label: "Helm Repositories", Danger: engine.DangerRead,
		Description: "Lists the Helm chart repositories of Helm's own repositories.yaml (the helm CLI adds and removes " +
			"them; Cogpit only reads the file, within 5 seconds of a change), in its order: name, URL without a user or " +
			"password, whether credentials are present (never a credential), TLS facts (CA file, client certificate, " +
			"insecure, plain http), how many charts the index lists, when it was downloaded, the last error or why " +
			"Cogpit does not use an entry; with the file's path and status (ok, missing, error: the last good list " +
			"kept). Their charts are the helmcharts kind (resources_list with kind helmcharts).",
		Enabled: func(s *engine.Session, _ struct{}) error { _, err := helmRepos(s); return err },
		Run: func(_ context.Context, s *engine.Session, _ struct{}) (any, error) {
			h, err := helmRepos(s)
			if err != nil {
				return nil, err
			}
			return h.List()
		},
	})

	add(g, engine.Spec[repoUpdateParams]{
		ID: "helm.repo.update", Label: "Update Helm Repositories", Danger: engine.DangerUI,
		Description: "Downloads the index of every Helm repository of Helm's repositories.yaml (or of those named) again " +
			"into Cogpit's own cache, one after the other, as helm repo update does, with the credentials Helm's file " +
			"holds; nothing is written into Helm's file or folders. A repository whose download fails keeps the index " +
			"it had and reports the error. Changes no cluster. While the Charts view or a Helm Install tab is open, " +
			"Cogpit does this itself for an index older than 10 minutes and for a repository new in Helm's file.",
		Enabled: func(s *engine.Session, _ repoUpdateParams) error { _, err := helmRepos(s); return err },
		Run: func(ctx context.Context, s *engine.Session, p repoUpdateParams) (any, error) {
			h, err := helmRepos(s)
			if err != nil {
				return nil, err
			}
			infos, err := h.Update(ctx, p.Names)
			if err != nil {
				if infos == nil {
					return nil, err
				}
				userToast(s, engine.LevelWarning, "Not every Helm repository was updated: "+err.Error())
				return infos, nil
			}
			switch len(infos) {
			case 0:
				userToast(s, engine.LevelInfo, "No Helm repository to update: add repositories with helm repo add")
			case 1:
				userToast(s, engine.LevelInfo, "Helm repository "+infos[0].Name+" updated")
			default:
				userToast(s, engine.LevelInfo, fmt.Sprintf("%d Helm repositories updated", len(infos)))
			}
			return infos, nil
		},
	})
}
