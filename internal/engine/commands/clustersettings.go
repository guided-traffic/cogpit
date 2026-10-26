package commands

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Cluster settings (G13; engine/clustersettings.go): what the user sets for one context in Cogpit,
// the Cluster Settings page (⌘⇧,): its display name and icon (a colour of the theme's accent
// palette, or an uploaded image), and its terminals' working directory and kubectl namespace.
// Every command changes only Cogpit's own cluster settings file (and its icons folder), never a
// cluster or a kubeconfig, so they are UI commands that agents run freely (D-2), except what marks
// which cluster this is, the user's alone, in the window: the display name and the icon (H-125, the
// owner's decision of 2026-10-19: cluster.settings.set's name and iconColor refused for agents and
// the CLI, set or cleared, engine.ErrClusterIdentityGUIOnly; cluster.settings.resetIcon GUI-only;
// the icon upload GUI-only, it also reads a file on this computer, engine.ErrIconGUIOnly) and the
// accent colour (accentcolor.go, D-80). The Prometheus a cluster's metrics come from stays
// cluster.setPrometheus. Its integrations (D-76) only the user switches, on or off (H-126, the
// owner's decision of 2026-10-19; H-133, his decision of 2026-10-20; engine/integrationsguionly.go):
// agents and the CLI read them; where an integration is read from when it is not found by name
// (cluster.setPrometheus) stays the user's (A16).

type clusterSettingsParams struct {
	Context string `json:"context,omitempty" jsonschema:"context id as cluster_contexts lists it (a bare context name works when one file only defines it); empty or omitted: the connected context"`
}

type clusterSettingsSetParams struct {
	Context              string   `json:"context,omitempty" jsonschema:"context id as cluster_contexts lists it (a bare context name works when one file only defines it); empty or omitted: the connected context"`
	Name                 *string  `json:"name,omitempty" jsonschema:"the cluster's display name in the hotbar, the navigator and the welcome page, one line of at most 128 characters; empty: the context's name. Only the user in Cogpit's window may set or clear it: refused for agents and the CLI"`
	IconColor            *string  `json:"iconColor,omitempty" jsonschema:"the colour tile's colour, one of cyan, blue, violet, magenta, red, orange, yellow, green (the theme's accents) or any colour as #rrggbb (drawn in a shade of its hue legible on each theme); set, it shows the tile instead of an image (the image is kept); empty: the accent. Only the user in Cogpit's window may set or clear it: refused for agents and the CLI"`
	IconLetters          *string  `json:"iconLetters,omitempty" jsonschema:"the colour tile's letters, at most 5 characters without spaces, drawn smaller to fit; empty: the initials of the cluster's name. Only the user in Cogpit's window may set or clear them: refused for agents and the CLI"`
	IconTile             *bool    `json:"iconTile,omitempty" jsonschema:"true: the colour tile shows although an image is kept (Internal icon); false: the kept image shows (Custom icon). Only the user in Cogpit's window may switch it: refused for agents and the CLI"`
	TerminalDir          *string  `json:"terminalDir,omitempty" jsonschema:"the working directory of the cluster's local terminals: an absolute path, or one starting with $HOME or ~; empty: $HOME"`
	TerminalNamespace    *string  `json:"terminalNamespace,omitempty" jsonschema:"kubectl's namespace in the cluster's local terminals; empty: the namespace filter's single namespace, else the context's own"`
	Proxy                *string  `json:"proxy,omitempty" jsonschema:"the HTTP(S) or SOCKS5 proxy of this cluster's API traffic (http://<address>:<port>, https:// or socks5://, optionally with user:password@), applied at the next connect in place of the kubeconfig's proxy-url; empty: none. Agents and the CLI may only clear it (an empty string): setting one is the user's, in Cogpit's window"`
	HiddenMetrics        []string `json:"hiddenMetrics,omitempty" jsonschema:"the metrics the window hides for this cluster, and does not ask the cluster for: places (cluster: the Overview's charts; nodes, pods: their lists' CPU and Memory columns and their drawers' charts; containers: a pod's containers' charts) and measures (cpu, memory, network, filesystem), or all; it replaces the list set before, and an empty list shows every metric again. metrics_get still answers"`
	MetricsSource        *string  `json:"metricsSource,omitempty" jsonschema:"which providers the cluster's metrics come from, as Lens's Metrics source: auto (both: Prometheus's history and the Metrics Server's current values, the default), prometheus (Prometheus alone), metrics-server (the Metrics Server alone: no history, network, filesystem or disk) or none (no metrics: nothing is asked); applied to the connected cluster at once. It sets the metrics-server and prometheus integrations. GUI only: refused to agents and the CLI, none included (it switches the metrics integrations, which is the user's, in Cogpit's window)"`
	AccessibleNamespaces []string `json:"accessibleNamespaces,omitempty" jsonschema:"the namespaces the user may read in, for a user who may not list the cluster's namespaces (Lens: Accessible namespaces), at most 100 namespace names; it replaces the list set before, and an empty list removes them. It only narrows what Cogpit reads: the namespace filter offers them, and a kind the user may not list in all namespaces is watched in each of them instead"`
	// RenewLoginInBackground: agents and the CLI may only turn it off (engine/connwatch_lapse.go, D-69 item 4).
	RenewLoginInBackground *bool `json:"renewLoginInBackground,omitempty" jsonschema:"whether the cluster's login is renewed while the cluster is connected but not shown, its credential plugin running (a browser login included) when the login expires (default true). Off: the plugin does not run then, and an expired login lapses (connections' loginLapsed) until the cluster is shown or logged in again. Agents and the CLI may only turn it off"`
	// StreamInitialLists (D-new; engine/initiallists.go): agents and the CLI switch it both ways, as
	// it changes only how Cogpit reads what it reads anyway.
	StreamInitialLists *bool `json:"streamInitialLists,omitempty" jsonschema:"whether the cluster's reads stream their initial lists (WatchList) while the streams end in time (true: Automatic, the default; a stream that stays silent for 10 s before its end makes the connection list, and Cogpit switches the streams off for the cluster when a list then answered at once) or always list and then watch, as Lens and kubectl read (false). Off applies to the connected cluster at once; Automatic from its next connect"`
	// Integrations (D-76; engine/integrations.go): agents and the CLI switch none, on or off (H-126,
	// H-133, engine/integrationsguionly.go).
	Integrations map[string]bool `json:"integrations,omitempty" jsonschema:"integrations switched on (true) or off (false) for this cluster, by name: metrics-server, prometheus, alertmanager, trivy, kyverno, as {alertmanager: false}; those left out stay as they apply now (for a cluster without a choice of its own: off, but for what the window's question about it ticks), and the result is the cluster's own choice (the window's question about it ends). Off: Cogpit sends the cluster nothing for it and shows nothing of it; on: Cogpit reads what the cluster runs, with the user's identity, and never writes to it. Applied to the connected cluster at once. Not together with metricsSource for metrics-server or prometheus. GUI only: refused to agents and the CLI, on (true) and off (false) alike: switching one on reads with the user's identity, switching one off takes its alerts, ratings or policy marks out of the window; both are the user's, in Cogpit's window"`
}

type clusterIconParams struct {
	Context string `json:"context,omitempty" jsonschema:"context id as cluster_contexts lists it; empty or omitted: the connected context"`
	Path    string `json:"path" jsonschema:"the image file the user picked: .png, .jpeg or .svg, at most 1 MiB"`
	iconCutParams
}

// iconCutParams are where a cluster's icon is cut from its image (D-119); omitted: the middle, the
// image just filling the square.
type iconCutParams struct {
	Zoom *float64 `json:"zoom,omitempty" jsonschema:"the zoom, from 1 (the image's shorter side fills the icon's square) to 8; omitted: 1"`
	X    *float64 `json:"x,omitempty" jsonschema:"the image's point at the square's centre, as a fraction of its width (0 to 1); omitted: 0.5. Moved inward where the square would leave the image"`
	Y    *float64 `json:"y,omitempty" jsonschema:"the image's point at the square's centre, as a fraction of its height (0 to 1); omitted: 0.5"`
}

// crop is the cut the parameters name, the default for what they leave out.
func (p iconCutParams) crop() engine.IconCrop {
	c := engine.DefaultIconCrop
	if p.Zoom != nil {
		c.Zoom = *p.Zoom
	}
	if p.X != nil {
		c.X = *p.X
	}
	if p.Y != nil {
		c.Y = *p.Y
	}
	return c
}

type clusterIconCutParams struct {
	Context string `json:"context,omitempty" jsonschema:"context id as cluster_contexts lists it; empty or omitted: the connected context"`
	iconCutParams
}

type clusterSettingsOpenParams struct {
	Context string `json:"context,omitempty" jsonschema:"context id as cluster_contexts lists it; empty or omitted: the connected context"`
	Section string `json:"section,omitempty" jsonschema:"the section to show: general (name, icon, kubeconfig), proxy, terminal, namespaces (the accessible namespaces), metrics (hidden metrics, and the state of the two integrations it reads), nodeShell (the node shells' image and pull secret), agents (the allowance of agents' changes, which only the user sets), integrations (each integration's switch and what Cogpit found of it, Ask again), or an integration's own section, named as the integration is: metrics-server, prometheus (its address and query format), alertmanager (its address), trivy or kyverno; omitted: general"`
}

const clusterSettingsOnly = " Changes only Cogpit's own cluster settings (a file in Cogpit's configuration directory), never the cluster or its kubeconfig."

// settingsContext is the context a cluster settings command names: the connected one when none
// is given, and a bare name resolved as cluster.connect resolves it; the store must exist.
func settingsContext(s *engine.Session, name string) (string, error) {
	if s.ClusterSettingsStore() == nil {
		return "", engine.ErrNoClusterSettings
	}
	return hotbarContext(s, name)
}

// settingsView is what a cluster settings change returns: the context's settings now.
func settingsView(s *engine.Session, id string) (any, error) {
	return s.ClusterSettingsView(id)
}

// changeSettings resolves the context, checks that a source offers it and changes its settings.
func changeSettings(s *engine.Session, name string, fn func(*engine.ClusterSettings) error) (any, error) {
	id, err := settingsContext(s, name)
	if err != nil {
		return nil, err
	}
	if err := offered(s, id); err != nil {
		return nil, err
	}
	if _, err := s.ChangeClusterSettings(id, fn); err != nil {
		return nil, err
	}
	return settingsView(s, id)
}

// refuse refuses what of a cluster settings change only the user may make, in Cogpit's window,
// for every other origin, before anyone is asked (Dispatch) and again before it runs: the display
// name and the icon colour (H-125), the renewal of a background login turned on (D-69), any switch
// of an integration, on or off (H-126, H-133), and a proxy from the CLI (refuseProxy).
func (p clusterSettingsSetParams) refuse(ctx context.Context) error {
	if err := engine.RefuseClusterIdentity(ctx, p.Name, p.IconColor, p.IconLetters, p.IconTile); err != nil {
		return err
	}
	if err := engine.RefuseRenewLogin(ctx, p.RenewLoginInBackground); err != nil {
		return err
	}
	if err := p.refuseIntegrations(ctx); err != nil {
		return err
	}
	return refuseProxy(ctx, p.Proxy)
}

// refuseIntegrations refuses any switch of the cluster's integrations, on or off, by name or by a
// Metrics source, for every origin but the window (H-126, H-133). The names and the source are
// checked first, so that a misspelt one says which are known.
func (p clusterSettingsSetParams) refuseIntegrations(ctx context.Context) error {
	if len(p.Integrations) == 0 && p.MetricsSource == nil {
		return nil
	}
	if _, _, err := engine.ParseIntegrationSwitches(p.Integrations); err != nil {
		return err
	}
	if p.MetricsSource != nil {
		if _, err := engine.ParseMetricsSource(*p.MetricsSource); err != nil {
			return err
		}
	}
	return engine.RefuseIntegrationSwitches(ctx)
}

// refuseIconReset refuses resetting a cluster's icon from anywhere but the window (Dispatch refuses
// it first, the command being GUI-only; Run checks again).
func refuseIconReset(ctx context.Context) error {
	if engine.OriginOf(ctx) != engine.OriginGUI {
		return engine.ErrClusterIdentityGUIOnly
	}
	return nil
}

func registerClusterSettings(g *registrar) {
	add(g, engine.Spec[clusterSettingsParams]{
		ID: "cluster.settings.get", Label: "Show Cluster Settings", Danger: engine.DangerRead,
		Description: "Returns a context's settings in Cogpit, as its Settings page shows them: the name it shows (the display " +
			"name set, else the context's name), its icon (iconColor, the tile's iconLetters, an uploaded image's format, " +
			"iconTile while the tile shows although an image is kept, the image's iconCrop), its accent colour (accentColor: the " +
			"user's mark of which cluster it is, a production one in red, say; only the user sets it) and how the light strip under " +
			"the tabs shows it (lightStrip: static, breathing, scanner or flow; frosted glass without an accent colour; only the " +
			"user chooses it), the kubeconfig file it comes " +
			"from, its terminals' working directory and kubectl namespace (empty: $HOME and the filter's or the " +
			"context's namespace), the metrics the window hides (hiddenMetrics; empty: all visible), the metrics source (metricsSource: " +
			"auto, prometheus, metrics-server or none) and, while connected, the providers answering (metricsInUse), the integrations " +
			"(integrations: each one's name, label, whether it is on, its state — found, configured, manual (none found by name: " +
			"its address is to be set, Prometheus's with cluster_setPrometheus), missing, forbidden, unavailable, checking, off or " +
			"notConnected — where it was found or set, why not, and what " +
			"it adds) and whether the user chose them for this cluster (integrationsDecided; false: the default for new " +
			"clusters applies and the window asks, ui_state's integrationsQuestion), the accessible " +
			"namespaces named for a user who may not list namespaces (accessibleNamespaces), the node shells' image, pull " +
			"secret and maximum duration (nodeShellImage, empty: nodeShellDefaultImage; nodeShellPullSecret; " +
			"nodeShellMaxDuration, empty: nodeShellDefaultMaxDuration), whether it is " +
			"connected (shown or not), whether the user allowed agents' changes without asking (agentWrites), whether its login " +
			"is renewed while it is not shown (renewLoginInBackground), and whether the settings are kept. The Prometheus setting is cluster_prometheus.",
		Enabled: func(s *engine.Session, p clusterSettingsParams) error {
			_, err := settingsContext(s, p.Context)
			return err
		},
		Run: func(_ context.Context, s *engine.Session, p clusterSettingsParams) (any, error) {
			id, err := settingsContext(s, p.Context)
			if err != nil {
				return nil, err
			}
			return settingsView(s, id)
		},
	})

	add(g, engine.Spec[clusterSettingsSetParams]{
		ID: "cluster.settings.set", Label: "Set Cluster Settings", Danger: engine.DangerUI,
		Description: "Changes some of a context's settings in Cogpit, as its Settings page does; the fields left out stay as " +
			"they are, an empty value restores the default. The display name and the icon colour show at once in the " +
			"hotbar, the navigator and the welcome page; the terminal's working directory and namespace apply to the " +
			"cluster's local terminals opened afterwards; hidden metrics leave the lists' columns, the drawers' charts " +
			"and the Overview at once, and nothing is polled for them; the metrics source turns a provider or both off " +
			"for the cluster at once; integrations switch on or off for the cluster at once (an integration off is " +
			"asked nothing and shows nothing) and become its own choice; a proxy applies at the next connect; accessible " +
			"namespaces (a list that replaces the last) only narrow what Cogpit reads: the namespace filter offers them, " +
			"and a kind the user may not list in all namespaces is watched in each of them, at once; renewLoginInBackground " +
			"off holds the credential plugin while the cluster is not shown (its login then lapses instead of being renewed), " +
			"and only the user may turn it on again; streamInitialLists off makes the connected cluster list at once instead of " +
			"streaming its initial lists, back on (Automatic) from its next connect. Refused for agents and the CLI: the display name and the icon colour, set " +
			"or cleared (only the user changes what marks which cluster this is), and switching an integration on or off " +
			"(integrations and metricsSource: only the user chooses what Cogpit reads with their identity and what " +
			"the window stops showing). An agent's proxy " +
			"always waits for the user's acceptance, also where the user allowed agent writes; the CLI may clear the proxy " +
			"but not set one; the proxy read back has its password shown as •••." +
			clusterSettingsOnly + " Returns the settings.",
		Enabled: func(s *engine.Session, p clusterSettingsSetParams) error {
			_, err := settingsContext(s, p.Context)
			return err
		},
		Refuse: func(ctx context.Context, _ *engine.Session, p clusterSettingsSetParams) error {
			return p.refuse(ctx)
		},
		// A proxy decides where the user's cluster traffic goes, credentials in it: an agent's always
		// waits for the user (A20, owner 2026-10-14 evening).
		AgentsAsk: func(_ *engine.Session, p clusterSettingsSetParams) bool { return setsProxy(p.Proxy) },
		Effects: func(s *engine.Session, p clusterSettingsSetParams) []engine.Effect {
			if !setsProxy(p.Proxy) {
				return nil
			}
			ctx, _ := settingsContext(s, p.Context)
			return proxyEffects("the API traffic of "+ctx+", from its next connect", *p.Proxy)
		},
		Run: func(ctx context.Context, s *engine.Session, p clusterSettingsSetParams) (any, error) {
			if err := p.refuse(ctx); err != nil {
				return nil, err
			}
			var source string
			if p.MetricsSource != nil {
				var err error
				if source, err = engine.ParseMetricsSource(*p.MetricsSource); err != nil {
					return nil, err
				}
			}
			intOn, intOff, err := engine.ParseIntegrationSwitches(p.Integrations)
			if err != nil {
				return nil, err
			}
			if p.MetricsSource != nil && (intOn|intOff)&engine.IntMetrics != 0 {
				return nil, errors.New("metricsSource and integrations both set the metrics providers: give one of them")
			}
			var hidden engine.MetricSet
			if p.HiddenMetrics != nil {
				var err error
				if hidden, err = engine.ParseMetricSet(p.HiddenMetrics); err != nil {
					return nil, err
				}
			}
			// What applies to a cluster without a choice of its own: nothing read but what its question
			// ticks in the window's session (D-82), the default for new clusters in one that does not ask.
			var baseOff engine.IntegrationSet
			if id, err := settingsContext(s, p.Context); err == nil {
				baseOff = s.IntegrationsOffNow(id)
			}
			return changeSettings(s, p.Context, func(cs *engine.ClusterSettings) error {
				if p.HiddenMetrics != nil {
					cs.HiddenMetrics = hidden
				}
				if p.MetricsSource != nil {
					cs.SetMetricsSource(baseOff, source) // the two metrics integrations (D-76)
				}
				if p.Integrations != nil {
					cs.SetIntegrations(baseOff, intOn, intOff) // the cluster's own choice from now on (D-76)
				}
				if p.AccessibleNamespaces != nil {
					cs.AccessibleNamespaces = slices.Clone(p.AccessibleNamespaces) // the engine trims, sorts and checks them
				}
				setIf(&cs.Name, p.Name)
				if p.IconColor != nil {
					cs.IconColor = strings.ToLower(strings.TrimSpace(*p.IconColor))
					if cs.IconColor != "" {
						cs.IconTile = cs.Icon != "" // a colour shows the tile (Lens: Pick Icon Color); the image is kept (D-119)
					}
				}
				if p.IconLetters != nil {
					cs.IconLetters = strings.TrimSpace(*p.IconLetters)
				}
				if p.IconTile != nil {
					if !*p.IconTile && cs.Icon == "" {
						return errors.New("no image is kept to show: cut one in the Custom icon's dialog first")
					}
					cs.IconTile = *p.IconTile && cs.Icon != ""
				}
				if p.TerminalDir != nil {
					cs.TerminalDir = strings.TrimSpace(*p.TerminalDir)
				}
				switch {
				case p.StreamInitialLists == nil:
				case *p.StreamInitialLists:
					cs.StreamsOff = nil
				case cs.StreamsOff == nil || cs.StreamsOff.By != engine.StreamsOffByUser:
					cs.StreamsOff = &engine.StreamsOff{By: engine.StreamsOffByUser, At: time.Now()}
				}
				if p.TerminalNamespace != nil {
					cs.TerminalNamespace = strings.TrimSpace(*p.TerminalNamespace)
				}
				if p.Proxy != nil {
					next, err := engine.MergeProxyPassword(cs.Proxy, *p.Proxy) // "•••" keeps the password set before
					if err != nil {
						return err
					}
					cs.Proxy = next
				}
				if p.RenewLoginInBackground != nil {
					cs.HoldBackgroundLogin = !*p.RenewLoginInBackground
				}
				return nil
			})
		},
	})

	add(g, engine.Spec[clusterIconParams]{
		ID: "cluster.settings.uploadIcon", Label: "Upload Icon", Danger: engine.DangerUI,
		GUIOnly: engine.ErrIconGUIOnly,
		Description: "Makes a context's icon a square cut from an image file the user picked (a PNG, a JPEG or an SVG without " +
			"scripts or outside references, at most 1 MiB): the image is kept, at most 1,024 pixels a side, as the icon's source, " +
			"and the icon is cut from it at zoom, x and y (the middle, filled, when omitted) as a PNG of 256 pixels a side; it shows " +
			"instead of the colour tile, which stays for when the user goes back to it. Only the user, in Cogpit's window, can " +
			"upload: refused for agents and the CLI, as every change of a cluster's icon is." + clusterSettingsOnly + " Returns the settings.",
		Enabled: func(s *engine.Session, p clusterIconParams) error {
			_, err := settingsContext(s, p.Context)
			return err
		},
		Refuse: func(ctx context.Context, _ *engine.Session, _ clusterIconParams) error {
			if engine.OriginOf(ctx) != engine.OriginGUI {
				return engine.ErrIconGUIOnly
			}
			return nil
		},
		Run: func(ctx context.Context, s *engine.Session, p clusterIconParams) (any, error) {
			if engine.OriginOf(ctx) != engine.OriginGUI {
				return nil, engine.ErrIconGUIOnly
			}
			id, err := settingsContext(s, p.Context)
			if err != nil {
				return nil, err
			}
			if err := offered(s, id); err != nil {
				return nil, err
			}
			if _, err := s.SetClusterIconCut(id, p.Path, p.crop()); err != nil {
				return nil, err
			}
			return settingsView(s, id)
		},
	})

	add(g, engine.Spec[clusterIconCutParams]{
		ID: "cluster.settings.cutIcon", Label: "Cut Icon", Danger: engine.DangerUI,
		GUIOnly: engine.ErrClusterIdentityGUIOnly, // H-125: the icon is the user's mark of the cluster
		Description: "Cuts a context's icon again from the image it was cut from before (D-119): at zoom (1: the image's " +
			"shorter side fills the square, up to 8) and x and y (the image's point at the square's centre, as fractions of its " +
			"width and height), moved inward where the square would leave the image; the icon shows instead of the colour tile. " +
			"Only the user, in Cogpit's window, can cut it: refused for agents and the CLI, as every change of a cluster's icon " +
			"is." + clusterSettingsOnly + " Returns the settings.",
		Enabled: func(s *engine.Session, p clusterIconCutParams) error {
			_, err := settingsContext(s, p.Context)
			return err
		},
		Refuse: func(ctx context.Context, _ *engine.Session, _ clusterIconCutParams) error {
			return refuseIconReset(ctx)
		},
		Run: func(ctx context.Context, s *engine.Session, p clusterIconCutParams) (any, error) {
			if err := refuseIconReset(ctx); err != nil {
				return nil, err
			}
			id, err := settingsContext(s, p.Context)
			if err != nil {
				return nil, err
			}
			if err := offered(s, id); err != nil {
				return nil, err
			}
			if _, err := s.SetClusterIconCut(id, "", p.crop()); err != nil {
				return nil, err
			}
			return settingsView(s, id)
		},
	})

	add(g, engine.Spec[clusterSettingsParams]{
		ID: "cluster.settings.resetIcon", Label: "Reset Icon", Danger: engine.DangerUI,
		GUIOnly: engine.ErrClusterIdentityGUIOnly, // H-125: the icon is the user's mark of the cluster
		Description: "Gives a context its default icon back: its initials in the accent colour, without an uploaded image, " +
			"its source, an icon colour or letters of the user's. Only the user, in Cogpit's window, can reset it: refused for agents and the CLI, so that no " +
			"agent takes away the icon the user marked a cluster with." + clusterSettingsOnly + " Returns the settings.",
		Enabled: func(s *engine.Session, p clusterSettingsParams) error {
			_, err := settingsContext(s, p.Context)
			return err
		},
		Refuse: func(ctx context.Context, _ *engine.Session, _ clusterSettingsParams) error {
			return refuseIconReset(ctx)
		},
		Run: func(ctx context.Context, s *engine.Session, p clusterSettingsParams) (any, error) {
			if err := refuseIconReset(ctx); err != nil {
				return nil, err
			}
			return changeSettings(s, p.Context, func(cs *engine.ClusterSettings) error {
				cs.Icon, cs.IconSource, cs.IconCrop, cs.IconColor, cs.IconLetters, cs.IconTile = "", "", nil, "", "", false
				return nil
			})
		},
	})

	add(g, engine.Spec[clusterSettingsOpenParams]{
		ID: "cluster.settings.open", Label: "Cluster Settings", MenuPath: "Cluster/Settings", Shortcut: "Cmd+Shift+,", Danger: engine.DangerUI,
		Description: "Opens a context's Settings page in Cogpit's window, as ⌘⇧, (Ctrl+Shift+,) does for the shown cluster, " +
			"at a section: general (name, icon, kubeconfig), proxy, terminal, namespaces (the accessible namespaces), metrics (hidden metrics, " +
			"and the state of the two integrations it reads), nodeShell, agents, integrations (every integration's switch and state, Ask again), " +
			"or an integration's own, named as the integration is: metrics-server, prometheus (its address and query format), " +
			"alertmanager (its address), trivy or kyverno. It shows as a card over the whole window " +
			"until closed (settings_close, Escape); ui_state reports it as settings. Returns the new UI state.",
		Enabled: func(s *engine.Session, p clusterSettingsOpenParams) error {
			if _, err := settingsContext(s, p.Context); err != nil {
				return err
			}
			_, err := engine.CheckSection(engine.SettingsCluster, p.Section)
			return err
		},
		Run: func(_ context.Context, s *engine.Session, p clusterSettingsOpenParams) (any, error) {
			id, err := settingsContext(s, p.Context)
			if err != nil {
				return nil, err
			}
			if err := offered(s, id); err != nil {
				return nil, err
			}
			section, err := engine.CheckSection(engine.SettingsCluster, p.Section)
			if err != nil {
				return nil, err
			}
			return s.UpdateUI(func(u *engine.UIState) {
				u.Settings = &engine.SettingsPage{Page: engine.SettingsCluster, Context: id, Section: section}
				u.Palette = false
			}), nil
		},
	})
}
