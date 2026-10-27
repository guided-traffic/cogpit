package commands

import (
	"context"
	"slices"
	"strings"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Preferences (G13; engine/preferences.go): Cogpit's own settings, the Preferences page (⌘,).
// preferences.set changes only Cogpit's preferences file, never a cluster or a kubeconfig, so it
// is a UI command that agents run freely (D-2), except the local terminal's shell and its
// arguments, which name a program Cogpit runs and are refused for every origin but the GUI
// (engine.ErrShellGUIOnly), and the other fields each refusal below names (the default
// integrations for new clusters among them, on or off: engine.ErrIntegrationsGUIOnly, H-126, H-133).
// The open page is UI state (settings.close closes it).

// PreferencesResult is what preferences.get and preferences.set return.
type PreferencesResult struct {
	Preferences engine.Preferences `json:"preferences"`
	// Timezone is the zone absolute times are shown in now: the preference's, or "Local" for the
	// computer's own.
	Timezone string `json:"timezone"`
	// Kept says whether the preferences outlive this run (false without a configuration folder).
	Kept bool `json:"kept"`
	// KubectlDir is the directory for binaries in effect: the preference's, else Cogpit's own
	// folder; empty when there is none.
	KubectlDir string `json:"kubectlDir,omitempty"`
}

type preferencesSetParams struct {
	Theme             *string  `json:"theme,omitempty" jsonschema:"system (follow the computer's light or dark setting), dark or light; Cogpit's window switches at once unless it was started with --theme"`
	Timezone          *string  `json:"timezone,omitempty" jsonschema:"IANA time zone absolute times are shown in, such as Europe/Berlin or UTC; empty: the computer's own"`
	TableRowHeight    *int     `json:"tableRowHeight,omitempty" jsonschema:"a table row's height in points, 20 to 48; 0: Cogpit's default"`
	EditorFontSize    *int     `json:"editorFontSize,omitempty" jsonschema:"the YAML editor's text size in points, 8 to 32 (default 13)"`
	EditorLineNumbers *bool    `json:"editorLineNumbers,omitempty" jsonschema:"whether the YAML editor shows line numbers (default true)"`
	EditorTabSize     *int     `json:"editorTabSize,omitempty" jsonschema:"how many spaces Tab indents by in the YAML editor, 1 to 8 (default 2)"`
	TerminalFontSize  *int     `json:"terminalFontSize,omitempty" jsonschema:"the dock terminals' text size in points, 8 to 32 (default 13), for terminals opened afterwards"`
	TerminalShell     *string  `json:"terminalShell,omitempty" jsonschema:"the local terminal's shell, an absolute path; empty: the system's default ($SHELL, else /bin/zsh; on Windows pwsh, else Windows PowerShell). Only the user in Cogpit's window may set it: refused for agents"`
	TerminalShellArgs []string `json:"terminalShellArgs,omitempty" jsonschema:"the local terminal shell's arguments, one per entry. Only the user in Cogpit's window may set them: refused for agents"`
	HelmDriver        *string  `json:"helmDriver,omitempty" jsonschema:"where a new Helm install keeps its release's records (Lens's HELM_DRIVER): secret (Helm's default; empty means it too) or configmap; sql is not supported. Releases already installed keep theirs. Agents and the CLI may only set secret: configmap, whose records whoever may read ConfigMaps can read, is the user's choice, in Cogpit's window"`
	HTTPProxy         *string  `json:"httpProxy,omitempty" jsonschema:"the proxy of the traffic that is no cluster's (the Helm repositories' indexes and charts, the kubectl downloads): http://<address>:<port>, https:// or socks5://, optionally with user:password@; empty: the environment's HTTPS_PROXY. Agents and the CLI may only clear it (an empty string): setting one is the user's, in Cogpit's window"`
	// TerminalRightClickCopyPaste is GUI-only too (engine/termpaste.go).
	TerminalRightClickCopyPaste *bool `json:"terminalRightClickCopyPaste,omitempty" jsonschema:"whether a right click in the dock's terminals copies the selection, or pastes the clipboard when nothing is selected (default false). Only the user in Cogpit's window may set it: refused for agents"`
	// The kubectl preferences (engine/kubectl.go): the mirror, the directory and the path GUI-only.
	KubectlDownload *bool   `json:"kubectlDownload,omitempty" jsonschema:"whether a terminal whose kubectl is more than one minor version away from its cluster, or missing, offers the matching release for download (default true); the download itself is the user's click"`
	KubectlMirror   *string `json:"kubectlMirror,omitempty" jsonschema:"the https base URL kubectl is downloaded from; empty: https://dl.k8s.io/release. Only the user in Cogpit's window may set it: refused for agents"`
	KubectlDir      *string `json:"kubectlDir,omitempty" jsonschema:"the directory for downloaded kubectl releases, an absolute path; empty: Cogpit's own folder. Only the user in Cogpit's window may set it: refused for agents"`
	KubectlPath     *string `json:"kubectlPath,omitempty" jsonschema:"the kubectl the terminals run, an absolute path; empty: the first on PATH. Only the user in Cogpit's window may set it: refused for agents"`
	KubectlEnforce  *bool   `json:"kubectlEnforce,omitempty" jsonschema:"whether a terminal's shell (zsh, bash, fish) also defines a function kubectl that runs the kubectl Cogpit chose, so that a folder the profile puts first or an alias does not win (default false: kubectl put first on PATH after the profile ran). Only the user in Cogpit's window may set it: refused for agents"`
	// LoadChartIcons: agents and the CLI may only turn it off (engine/helmicons.go, D-46).
	LoadChartIcons *bool `json:"loadChartIcons,omitempty" jsonschema:"whether the Charts view and a chart's drawer fetch the icon a chart repository's index names, from whatever host serves it (default true); off: Cogpit's own glyph. Agents and the CLI may only turn it off"`
	// CheckForUpdates and UpdateChannel: the app's updater (engine/updates.go, D-4).
	CheckForUpdates *bool   `json:"checkForUpdates,omitempty" jsonschema:"whether Cogpit looks for a new release at start and every hour and offers it (default true). Agents and the CLI may only turn it on"`
	UpdateChannel   *string `json:"updateChannel,omitempty" jsonschema:"the update channel the app follows: stable, the only one until Cogpit publishes prereleases (empty means stable)"`
	// RestoreSession: agents and the CLI may only turn it off (engine/restore.go, D-69 item 8).
	RestoreSession *bool `json:"restoreSession,omitempty" jsonschema:"whether Cogpit, at its next start, restores the tabs of the last run and connects again to the clusters connected when it quit, the one shown then first, their credential plugins running as on any connect (default false: it starts without tabs and connections). Agents and the CLI may only turn it off"`
	// IntegrationDefaults: the integrations of clusters without a choice of their own (engine/integrations.go, D-76).
	IntegrationDefaults map[string]bool `json:"integrationDefaults,omitempty" jsonschema:"the default for new clusters: integrations on (true) or off (false), by name (metrics-server, prometheus, alertmanager, trivy, kyverno), what the window's question about a new cluster's integrations looks for when the user checks them (Check all), and what a session that does not ask (the CLI's) reads; the window reads nothing of a cluster without a choice before its question is answered. Those left out stay as they are (default: every one on); preferences.integrationsOff reads back the ones off. GUI only: refused to agents and the CLI, on (true) and off (false) alike: the default is what the user's Check all looks for, which is the user's, in Cogpit's window"`
	// DebugImage: the image Debug offers for a pod's debug container (engine/debugcontainer.go, D-102).
	DebugImage *string `json:"debugImage,omitempty" jsonschema:"the image Debug (pod_debug) offers for a pod's debug container, best pinned by digest (repository:tag@sha256:...); empty: Cogpit's default, nicolaka/netshoot:v0.16 pinned by digest. Agents and the CLI may only reset it (an empty string): setting one is the user's, in Cogpit's window"`
	// MixedEnvironments: which tabs the tab bar shows (engine/navscope.go TabsShown, GW11).
	MixedEnvironments *bool `json:"mixedEnvironments,omitempty" jsonschema:"whether the tab bar shows the open tabs of every cluster side by side (true), or only the shown cluster's, the one the status bar and the navigator name (false, the default); ui_state's tabs list every tab either way"`
	// ReduceMotion: whether the window slides (fyneui/navigator_slide.go).
	ReduceMotion *bool `json:"reduceMotion,omitempty" jsonschema:"whether the window switches at once where it would slide (true): between All Clusters and a cluster's tree, and from one cluster's tree to another's; false, the default, it slides"`
	// RevisionsDiff: how the Revisions tab reads a comparison (engine/helmrevisions.go).
	RevisionsDiff *string `json:"revisionsDiff,omitempty" jsonschema:"how a Helm release's Revisions tab reads a comparison, which revision is the diff's old side: comparedToShown (the default; empty means it too), olderToNewer, or shownToCompared"`
	// LightStripMainsOnly: whether the light strip animates on battery (fyneui/preferences_power.go).
	LightStripMainsOnly *bool `json:"lightStripMainsOnly,omitempty" jsonschema:"whether the light strip under the tabs animates only while the computer runs on its power adapter (true): on battery it shows its colour standing still; false, the default, it animates on battery too"`
	// ZoomLevel: the window's zoom (engine/zoom.go).
	ZoomLevel *int `json:"zoomLevel,omitempty" jsonschema:"the window's zoom level as VS Code counts it, -4 to 4: the window scales by 1.2 to its power (50%, 60%, 70%, 80%, 100%, 120%, 140%, 170%, 210%); 0, the default, is 100%; ui_zoom_in, ui_zoom_out and ui_zoom_reset step it"`
}

// shellChange reports whether the parameters change the local terminal's shell or arguments.
func (p preferencesSetParams) shellChange() bool {
	return p.TerminalShell != nil || p.TerminalShellArgs != nil
}

// kubectlChange reports whether the parameters change which kubectl the terminals run: its path,
// the directory for binaries or the mirror.
func (p preferencesSetParams) kubectlChange() bool {
	return p.KubectlMirror != nil || p.KubectlDir != nil || p.KubectlPath != nil || p.KubectlEnforce != nil
}

// refuse refuses what only the user may set, for every origin but the GUI.
func (p preferencesSetParams) refuse(ctx context.Context) error {
	if engine.OriginOf(ctx) == engine.OriginGUI {
		return nil
	}
	if p.shellChange() {
		return engine.ErrShellGUIOnly
	}
	if p.kubectlChange() {
		return engine.ErrKubectlGUIOnly
	}
	return nil
}

// refuseIntegrationDefaults refuses any change of the default integrations for new clusters, on or
// off, from every origin but the GUI: it is what the user's Check all looks for with the user's
// identity (H-126) and what it leaves out (H-133). The names are checked first.
func (p preferencesSetParams) refuseIntegrationDefaults(ctx context.Context) error {
	if len(p.IntegrationDefaults) == 0 {
		return nil
	}
	if _, _, err := engine.ParseIntegrationSwitches(p.IntegrationDefaults); err != nil {
		return err
	}
	return engine.RefuseIntegrationSwitches(ctx)
}

type preferencesOpenParams struct {
	Section string `json:"section,omitempty" jsonschema:"the section to show: app (theme, time zone), proxy, kubernetes (kubeconfig syncs, kubectl), editor, terminal, tables or agent (the built-in agent's LLM connections); omitted: app"`
}

// prefsResult is what the preferences commands return: the proxy's password as ••• (Shown).
func prefsResult(s *engine.Session, p engine.Preferences) PreferencesResult {
	return PreferencesResult{Preferences: p.Shown(), Timezone: s.Location().String(), Kept: s.PreferencesStore().Path() != "",
		KubectlDir: s.KubectlBinDir()}
}

const prefsWritesOnly = " Changes only Cogpit's own preferences (a file in Cogpit's configuration directory), never a cluster or a kubeconfig."

func registerPreferences(g *registrar) {
	add(g, engine.Spec[struct{}]{
		ID: "preferences.get", Label: "Show Preferences", Danger: engine.DangerRead,
		Description: "Returns Cogpit's preferences as the Preferences page (⌘,) shows them: the theme (system, dark, light), " +
			"the time zone absolute times are shown in (empty: the computer's) and the zone in effect, the tables' row " +
			"height, the YAML editor's font size, line numbers and tab size, the terminal's font size, the local " +
			"terminal's shell and arguments (empty: the system's default shell, $SHELL or on Windows PowerShell), whether a right click in a terminal copies and pastes, the " +
			"HTTP proxy of the traffic that is no cluster's (empty: the environment's; its password shown as •••), and " +
			"kubectl for terminals: downloads offered or not, the mirror, the directory for binaries (kubectlDir: the one " +
			"in effect), the path to kubectl, where new Helm installs keep their records (helmDriver, empty: " +
			"secret), whether chart icons are fetched (loadChartIcons), whether the app checks for updates automatically " +
			"(checkForUpdates) and on which channel (updateChannel), whether the next start restores the last session " +
			"(restoreSession: its tabs and connections), the integrations off for clusters without a choice of their own " +
			"(integrationsOff, the default for new clusters; absent: every one on), the image Debug offers for a pod's debug " +
			"container (debugImage; empty: Cogpit's default, nicolaka/netshoot:v0.16 pinned by digest), whether the tab bar shows " +
			"every cluster's tabs (mixedEnvironments), whether the window switches at once instead of sliding (reduceMotion), " +
			"how a Helm release's Revisions tab reads a comparison (revisionsDiff: comparedToShown, olderToNewer or shownToCompared; empty: comparedToShown) and " +
			"whether the light strip animates only on the power adapter (lightStripMainsOnly), the window's zoom level (zoomLevel: -4 to 4, 0 is 100%), " +
			"the folders Cogpit looks in for a credential plugin's programs after PATH (programFolders: preferences_addProgramFolder, preferences_removeProgramFolder); " +
			"kept says whether they outlive this run. " +
			"ui_state carries the same.",
		Enabled: func(s *engine.Session, _ struct{}) error { _, err := s.Preferences(); return err },
		Run: func(_ context.Context, s *engine.Session, _ struct{}) (any, error) {
			p, err := s.Preferences()
			if err != nil {
				return nil, err
			}
			return prefsResult(s, p), nil
		},
	})

	add(g, engine.Spec[preferencesSetParams]{
		ID: "preferences.set", Label: "Set Preferences", Danger: engine.DangerUI,
		Description: "Changes some of Cogpit's preferences, as the Preferences page does; the fields left out stay as they are. " +
			"The window follows at once: the theme, the time zone of the drawer's Created and the charts, the tables' row " +
			"height, the open editors; the HTTP proxy at the next download from a Helm repository or of kubectl. The " +
			"local terminal's shell and its arguments are refused for agents and the CLI: they name a program Cogpit " +
			"runs on this computer; so are the path to kubectl, the directory for binaries and the kubectl download " +
			"mirror, which decide what the terminals run as kubectl, and the terminals' right mouse button, which decides " +
			"whether a click pastes into a shell. An agent's HTTP proxy (it decides where Cogpit's downloads go) always " +
			"waits for the user's acceptance; the CLI may only clear it. Refused too: choosing the configmap Helm storage driver for new installs, which they may " +
			"only set back to secret, turning on chart icons, which they may only turn off, turning off the automatic " +
			"update checks, which they may only turn on, and turning on the restore of the last session (the next start would " +
			"connect clusters and run their credential plugins without a click), which they may only turn off, switching " +
			"a default integration for new clusters on or off (integrationDefaults), and setting the debug container's " +
			"image (debugImage), which they may only reset: it decides what the user's next Debug runs beside a pod's processes. The default " +
			"integrations for new clusters (integrationDefaults) apply at once to every cluster without a choice of its own." +
			prefsWritesOnly + " Returns the preferences.",
		Enabled: func(s *engine.Session, _ preferencesSetParams) error { _, err := s.Preferences(); return err },
		// An agent's proxy always waits for the user (A20, owner 2026-10-14 evening; the follow-up's
		// Preferences proxy belongs to no context, so no allowance could skip it: a night decision).
		AgentsAsk: func(_ *engine.Session, p preferencesSetParams) bool { return setsProxy(p.HTTPProxy) },
		Effects: func(_ *engine.Session, p preferencesSetParams) []engine.Effect {
			if !setsProxy(p.HTTPProxy) {
				return nil
			}
			return proxyEffects("Cogpit's downloads (Helm repositories, kubectl)", *p.HTTPProxy)
		},
		Refuse: func(ctx context.Context, _ *engine.Session, p preferencesSetParams) error {
			if err := p.refuse(ctx); err != nil {
				return err
			}
			if p.TerminalRightClickCopyPaste != nil && engine.OriginOf(ctx) != engine.OriginGUI {
				return engine.ErrRightClickGUIOnly
			}
			if err := engine.RefuseHelmDriver(ctx, p.HelmDriver); err != nil {
				return err
			}
			if err := engine.RefuseChartIcons(ctx, p.LoadChartIcons); err != nil {
				return err
			}
			if err := engine.RefuseUpdateChecks(ctx, p.CheckForUpdates); err != nil {
				return err
			}
			if err := engine.RefuseRestoreSession(ctx, p.RestoreSession); err != nil {
				return err
			}
			if err := p.refuseIntegrationDefaults(ctx); err != nil {
				return err
			}
			if err := engine.RefuseDebugImage(ctx, p.DebugImage); err != nil {
				return err
			}
			return refuseProxy(ctx, p.HTTPProxy)
		},
		Run: func(ctx context.Context, s *engine.Session, p preferencesSetParams) (any, error) {
			if err := p.refuse(ctx); err != nil {
				return nil, err
			}
			if p.TerminalRightClickCopyPaste != nil && engine.OriginOf(ctx) != engine.OriginGUI {
				return nil, engine.ErrRightClickGUIOnly
			}
			if err := engine.RefuseHelmDriver(ctx, p.HelmDriver); err != nil {
				return nil, err
			}
			if err := refuseProxy(ctx, p.HTTPProxy); err != nil {
				return nil, err
			}
			if err := engine.RefuseChartIcons(ctx, p.LoadChartIcons); err != nil {
				return nil, err
			}
			if err := engine.RefuseUpdateChecks(ctx, p.CheckForUpdates); err != nil {
				return nil, err
			}
			if err := engine.RefuseRestoreSession(ctx, p.RestoreSession); err != nil {
				return nil, err
			}
			if err := p.refuseIntegrationDefaults(ctx); err != nil {
				return nil, err
			}
			if err := engine.RefuseDebugImage(ctx, p.DebugImage); err != nil {
				return nil, err
			}
			var helmDriver string
			if p.HelmDriver != nil {
				d, err := engine.HelmDriverPreference(*p.HelmDriver)
				if err != nil {
					return nil, err
				}
				helmDriver = d
			}
			if p.TerminalShell != nil {
				if err := engine.CheckShell(strings.TrimSpace(*p.TerminalShell)); err != nil {
					return nil, err
				}
			}
			if p.KubectlPath != nil {
				if err := engine.CheckKubectlPath(strings.TrimSpace(*p.KubectlPath)); err != nil {
					return nil, err
				}
			}
			intOn, intOff, err := engine.ParseIntegrationSwitches(p.IntegrationDefaults)
			if err != nil {
				return nil, err
			}
			next, err := s.ChangePreferences(func(pr *engine.Preferences) error {
				pr.IntegrationsOff = (pr.IntegrationsOff &^ intOn) | intOff // the default for new clusters (D-76)
				setIf(&pr.Theme, p.Theme)
				if p.Timezone != nil {
					pr.Timezone = strings.TrimSpace(*p.Timezone)
				}
				setIf(&pr.TableRowHeight, p.TableRowHeight)
				setIf(&pr.EditorFontSize, p.EditorFontSize)
				setIf(&pr.EditorLineNumbers, p.EditorLineNumbers)
				setIf(&pr.EditorTabSize, p.EditorTabSize)
				setIf(&pr.TerminalFontSize, p.TerminalFontSize)
				if p.TerminalShell != nil {
					pr.TerminalShell = strings.TrimSpace(*p.TerminalShell)
				}
				if p.TerminalShellArgs != nil {
					pr.TerminalShellArgs = slices.Clone(p.TerminalShellArgs)
				}
				if p.HelmDriver != nil {
					pr.HelmDriver = helmDriver
				}
				if p.HTTPProxy != nil {
					next, err := engine.MergeProxyPassword(pr.HTTPProxy, *p.HTTPProxy) // "•••" keeps the password set before
					if err != nil {
						return err
					}
					pr.HTTPProxy = next
				}
				setIf(&pr.TerminalRightClickCopyPaste, p.TerminalRightClickCopyPaste)
				setIf(&pr.KubectlDownload, p.KubectlDownload)
				setIf(&pr.LoadChartIcons, p.LoadChartIcons)
				setIf(&pr.CheckForUpdates, p.CheckForUpdates)
				setIf(&pr.RestoreSession, p.RestoreSession)
				setIf(&pr.MixedEnvironments, p.MixedEnvironments)
				setIf(&pr.ReduceMotion, p.ReduceMotion)
				if p.RevisionsDiff != nil {
					pr.RevisionsDiff = strings.TrimSpace(*p.RevisionsDiff)
					if pr.RevisionsDiff == engine.RevisionsComparedToShown {
						pr.RevisionsDiff = "" // the default, as the file keeps it
					}
				}
				setIf(&pr.LightStripMainsOnly, p.LightStripMainsOnly)
				setIf(&pr.ZoomLevel, p.ZoomLevel)
				if p.UpdateChannel != nil {
					pr.UpdateChannel = strings.TrimSpace(*p.UpdateChannel)
					if pr.UpdateChannel == "" {
						pr.UpdateChannel = engine.UpdateChannelStable
					}
				}
				for _, f := range []struct{ dst, v *string }{{&pr.KubectlMirror, p.KubectlMirror}, {&pr.KubectlDir, p.KubectlDir}, {&pr.KubectlPath, p.KubectlPath}} {
					if f.v != nil {
						*f.dst = strings.TrimSpace(*f.v)
					}
				}
				if p.KubectlEnforce != nil {
					pr.KubectlEnforce = *p.KubectlEnforce
				}
				if p.DebugImage != nil {
					pr.DebugImage = strings.TrimSpace(*p.DebugImage)
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
			return prefsResult(s, next), nil
		},
	})

	add(g, engine.Spec[preferencesOpenParams]{
		ID: "preferences.open", Label: "Preferences", MenuPath: "Cogpit/Preferences", Shortcut: "Cmd+,", Danger: engine.DangerUI,
		Description: "Opens Cogpit's Preferences page in the window, as ⌘, (Ctrl+,) does, at a section: app (theme, time zone), " +
			"proxy (the HTTP proxy of the traffic that is no cluster's), kubernetes (kubeconfig syncs, kubectl for terminals, Helm's storage driver), " +
			"editor, terminal, tables or agent (the built-in agent's LLM connections, which only the user edits there). It shows as a card over the whole window until " +
			"closed (settings_close, Escape); ui_state reports it as settings. Returns the new UI state.",
		Enabled: func(s *engine.Session, p preferencesOpenParams) error {
			if _, err := s.Preferences(); err != nil {
				return err
			}
			_, err := engine.CheckSection(engine.SettingsPreferences, p.Section)
			return err
		},
		Run: func(_ context.Context, s *engine.Session, p preferencesOpenParams) (any, error) {
			section, err := engine.CheckSection(engine.SettingsPreferences, p.Section)
			if err != nil {
				return nil, err
			}
			return s.UpdateUI(func(u *engine.UIState) {
				u.Settings = &engine.SettingsPage{Page: engine.SettingsPreferences, Section: section}
				u.Palette = false
			}), nil
		},
	})

	add(g, engine.Spec[struct{}]{
		ID: "settings.close", Label: "Close Settings", Danger: engine.DangerUI,
		Description: "Closes the open settings page, Preferences or a cluster's Settings, as its close button or Escape does; " +
			"the cluster's views come back as they were. Returns the new UI state.",
		Run: func(_ context.Context, s *engine.Session, _ struct{}) (any, error) {
			return s.UpdateUI(func(u *engine.UIState) { u.Settings = nil }), nil
		},
	})
}

// setIf sets *dst to *v when v is given.
func setIf[T any](dst *T, v *T) {
	if v != nil {
		*dst = *v
	}
}
