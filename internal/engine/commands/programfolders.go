package commands

import (
	"context"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Program folders (D-120; engine/programfolders.go): Preferences › Kubernetes › Program folders,
// where Cogpit looks for a credential plugin's programs after PATH and its built-in folders. Adding
// one is GUI-only (engine.ErrProgramFoldersGUIOnly): Dispatch refuses agents and the CLI before
// anything is read or written. Removing one is a UI command anyone runs: it only makes fewer
// programs found. Both change only Cogpit's preferences file and its own PATH, never a cluster or a
// kubeconfig. preferences.get and ui_state show the folders (programFolders).

type programFolderParams struct {
	Path string `json:"path" jsonschema:"the folder, an absolute path, as preferences_get's programFolders lists it"`
}

const programFoldersEffect = " Takes effect at once for every program Cogpit starts afterwards (a credential plugin's next login, a " +
	"terminal opened afterwards); programs that run already keep theirs." + prefsWritesOnly + " Returns the preferences."

func registerProgramFolders(g *registrar) {
	add(g, engine.Spec[programFolderParams]{
		ID: "preferences.addProgramFolder", Label: "Add Program Folder", Danger: engine.DangerUI,
		GUIOnly: engine.ErrProgramFoldersGUIOnly,
		Description: "Adds a folder Cogpit looks in for the programs a kubeconfig's credential plugin runs (kubelogin, kubectl's " +
			"plugins such as krew's kubectl-oidc_login), as Preferences › Kubernetes › Program folders does: appended to Cogpit's " +
			"PATH after its own folders and Cogpit's built-in ones (Homebrew's, krew's, go install's and others), so it never " +
			"shadows a program found there. Refused: a folder that does not exist, is not a folder, or that every user of the " +
			"computer may write to. Only the user, in Cogpit's window, can add one: refused for agents and the CLI, since a " +
			"folder an agent added could hold a program of its choice that the user's next login runs." + programFoldersEffect,
		Enabled: func(s *engine.Session, _ programFolderParams) error { _, err := s.Preferences(); return err },
		Run: func(ctx context.Context, s *engine.Session, p programFolderParams) (any, error) {
			if engine.OriginOf(ctx) != engine.OriginGUI { // Dispatch refuses first; Run checks again
				return nil, engine.ErrProgramFoldersGUIOnly
			}
			next, err := s.AddProgramFolder(p.Path)
			if err != nil {
				return nil, err
			}
			return prefsResult(s, next), nil
		},
	})

	add(g, engine.Spec[programFolderParams]{
		ID: "preferences.removeProgramFolder", Label: "Remove Program Folder", Danger: engine.DangerUI,
		Description: "Removes a folder from the folders Cogpit looks in for a credential plugin's programs (preferences_get's " +
			"programFolders), as its remove button in Preferences › Kubernetes › Program folders does; a login whose plugin " +
			"was found only there fails with \"not found\" from then on." + programFoldersEffect,
		Enabled: func(s *engine.Session, _ programFolderParams) error { _, err := s.Preferences(); return err },
		Run: func(_ context.Context, s *engine.Session, p programFolderParams) (any, error) {
			next, err := s.RemoveProgramFolder(p.Path)
			if err != nil {
				return nil, err
			}
			return prefsResult(s, next), nil
		},
	})
}
