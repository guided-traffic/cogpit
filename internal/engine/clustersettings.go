package engine

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/util/validation"
)

// Cluster settings (G13, docs/parity/ui-parity.md › Cluster settings): what the user sets for one
// context in Cogpit, as Lens's cluster settings page holds it: the cluster's display name and
// identity (an icon colour from the theme's accent palette, or an uploaded image, and since D-80 an
// accent colour that marks it, shown by the light strip under the tabs (D-new), both the user's
// alone: accentcolor.go, lightstrip.go), and the working
// directory and default namespace of that cluster's terminals. They are kept per context id in
// ClusterSettingsFile under Cogpit's configuration directory, the images in ClusterIconsDir beside
// it, never in a kubeconfig and never in the cluster (D-2: no implicit kubeconfig writes). They
// are engine state (UIState.ClusterSettings) so that the hotbar, the welcome page and the
// navigator show the same identity, and agents see it; the identity itself (the name and the icon)
// only the user changes (H-125, clusteridentity.go), the rest agents change too. The Prometheus a cluster's
// metrics come from is a cluster setting too, kept apart in metrics.json (metricssettings.go).

// ClusterSettingsFile is the cluster settings' file under Cogpit's configuration directory.
const ClusterSettingsFile = "cluster-settings.json"

// Bounds of the cluster settings.
const (
	maxClusterName         = 128
	maxClusterSettings     = 1024 // contexts with settings; more are dropped on load and refused on change
	maxTerminalDir         = 4096
	clusterSettingsVersion = 1
	homeVariable           = "$HOME" // what the page shows when no working directory is set (captured)
)

// IconColors are the colours a cluster's icon may take by name: Solarized's eight accents, the
// theme's accent palette (D-22), drawn in each variant's legible shade (the GUI's theme tokens).
// Since 2026-10-15 (the owner's A93 of 2026-10-14 evening) any colour "#rrggbb" too, which the window draws in a shade
// of the same hue legible on each theme (IsCustomIconColor).
var IconColors = []string{"cyan", "blue", "violet", "magenta", "red", "orange", "yellow", "green"}

// IsCustomIconColor reports whether c is a colour picked freely: "#rrggbb", lower-case hex.
func IsCustomIconColor(c string) bool {
	if len(c) != 7 || c[0] != '#' {
		return false
	}
	for _, r := range c[1:] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// validIconColor reports whether c is one of IconColors or a custom colour.
func validIconColor(c string) bool { return slices.Contains(IconColors, c) || IsCustomIconColor(c) }

// ErrNoClusterSettings disables the cluster settings commands of a session that keeps none.
var ErrNoClusterSettings = errors.New("this Cogpit keeps no cluster settings")

// ClusterSettings are one context's settings. The zero value is a context's defaults: its own
// name, the initials badge in the accent colour, terminals in $HOME and kubectl's own namespace.
type ClusterSettings struct {
	// Name is the display name; empty: the context's name. Only the user sets or clears it, in
	// Cogpit's window (ErrClusterIdentityGUIOnly, H-125).
	Name string `json:"name,omitempty"`
	// IconColor is one of IconColors; empty: the accent. Only the user sets or clears it, as the
	// uploaded image (ErrClusterIdentityGUIOnly, H-125).
	IconColor string `json:"iconColor,omitempty"`
	// Icon is the uploaded image's file in ClusterIconsDir ("<sha256>.png" or "<sha256>.svg"),
	// which takes the colour's place unless IconTile; empty: none. Since D-119 a square PNG cut
	// from IconSource; an SVG or another shape only from before.
	Icon string `json:"icon,omitempty"`
	// IconSource is the image Icon was cut from, a PNG in ClusterIconsDir, and IconCrop where it was
	// cut (D-119, clustersettings_iconcut.go): the dialog opens again where it was left.
	IconSource string    `json:"iconSource,omitempty"`
	IconCrop   *IconCrop `json:"iconCrop,omitempty"`
	// IconTile shows the colour tile although an image is kept, so that the user can go back to
	// the image (D-119).
	IconTile bool `json:"iconTile,omitempty"`
	// IconLetters are the tile's letters, at most MaxIconLetters; empty: the name's initials. Only
	// the user sets them, as the icon (H-125).
	IconLetters string `json:"iconLetters,omitempty"`
	// AccentColor is the cluster's accent colour (D-80, accentcolor.go): one of IconColors or
	// "#rrggbb"; empty: none. The user's mark of which cluster this is (a production one in red),
	// shown by the light strip under the tabs (LightStrip); only the user sets or clears it, in
	// Cogpit's window (ErrAccentColorGUIOnly).
	AccentColor string `json:"accentColor,omitempty"`
	// LightStrip is how the strip under the tabs shows the accent colour while one of the cluster's
	// tabs is shown (D-new, lightstrip.go): "" static, or LightStripBreathing, LightStripScanner,
	// LightStripFlow. Without an accent colour the strip is off whatever this says; only the user
	// chooses it, in Cogpit's window (ErrLightStripGUIOnly).
	LightStrip string `json:"lightStrip,omitempty"`
	// TerminalDir is the working directory of the cluster's local terminals: an absolute path,
	// or one starting with $HOME or ~; empty: the home directory.
	TerminalDir string `json:"terminalDir,omitempty"`
	// TerminalNamespace is kubectl's namespace in the cluster's local terminals; empty: the
	// namespace filter's single namespace, else the context's own.
	TerminalNamespace string `json:"terminalNamespace,omitempty"`
	// HiddenMetrics are the metrics the window does not show for this cluster, and does not ask
	// the cluster for (hidemetrics.go); none: every metric shows.
	HiddenMetrics MetricSet `json:"hiddenMetrics,omitempty"`
	// MetricsSource is the Metrics source kept before D-76 ("prometheus", "metrics-server" or
	// "none"; "" Automatic), read from older files only: normalize moves it into IntegrationsOff,
	// whose two metrics switches are the source since (integrations.go, metricssource.go).
	MetricsSource string `json:"metricsSource,omitempty"`
	// IntegrationsOff are the integrations the user switched off for this cluster (integrations.go,
	// D-76); nil while the user made no choice for it: the window asks, and reads nothing of them
	// meanwhile but what its question ticks (D-82; the CLI's session: Preferences.IntegrationsOff).
	// Only the user switches one, on or off, or makes the choice (integrationsguionly.go, H-126,
	// H-133).
	IntegrationsOff *IntegrationSet `json:"integrationsOff,omitempty"`
	// Proxy is the HTTP(S) or SOCKS5 proxy of this cluster's API traffic, as the user typed it
	// (proxy.go), applied at the next connect in place of the kubeconfig's proxy-url; empty: the
	// kubeconfig's, else the environment's. It may carry a password: the UI state and every
	// result show it redacted (RedactProxyURL), only the settings file keeps it as typed.
	Proxy string `json:"proxy,omitempty"`
	// AccessibleNamespaces are the namespaces the user names for a cluster whose namespaces they
	// may not list (Lens's Accessible namespaces; accessiblens.go): sorted, without duplicates. The
	// namespace filter offers them, and a namespaced kind the user may not list in all namespaces is
	// watched in each of them; none: the namespace filter's, else the context's own.
	AccessibleNamespaces []string `json:"accessibleNamespaces,omitempty"`
	// AgentWrites is the user's allowance of agents' mutating commands in this cluster without
	// asking (D-2, allowance.go); only the user sets it, in Cogpit's window. Destructive commands
	// always ask. False, the default: every mutating command of an agent waits for the user.
	AgentWrites bool `json:"agentWrites,omitempty"`
	// NodeShellImage is the image of this cluster's node shells (nodeshell.go); empty:
	// DefaultNodeShellImage. Only the user sets one; agents may reset it.
	NodeShellImage string `json:"nodeShellImage,omitempty"`
	// NodeShellPullSecret names an image pull secret in kube-system for the node shells' image;
	// empty: none.
	NodeShellPullSecret string `json:"nodeShellPullSecret,omitempty"`
	// NodeShellMaxMinutes is how long a node shell's pod lives at most, in minutes
	// (nodeshell_duration.go); 0: DefaultNodeShellDeadline. Only the user sets it; agents may
	// reset it.
	NodeShellMaxMinutes int `json:"nodeShellMaxMinutes,omitempty"`
	// HoldBackgroundLogin is the page's "Renew the login while the cluster is not shown" switched
	// off (D-69 item 4, connwatch_lapse.go; kept as its negation so that the default, on, is the
	// zero value): while the cluster is connected but not shown, its credential plugin does not run,
	// and a login that expires meanwhile lapses until the cluster is shown. Agents and the CLI may
	// only set it (turn the renewal off); turning the renewal on again is the user's, in Cogpit's
	// window (ErrRenewLoginGUIOnly).
	HoldBackgroundLogin bool `json:"holdBackgroundLogin,omitempty"`
}

// IsZero reports whether the settings are all defaults.
func (c ClusterSettings) IsZero() bool { return c.Equal(ClusterSettings{}) }

// Equal reports whether two settings are the same; an empty list of accessible namespaces equals
// none.
func (c ClusterSettings) Equal(o ClusterSettings) bool {
	return c.Name == o.Name && c.IconColor == o.IconColor && c.Icon == o.Icon && c.IconSource == o.IconSource && equalCrop(c.IconCrop, o.IconCrop) &&
		c.IconTile == o.IconTile && c.IconLetters == o.IconLetters && c.AccentColor == o.AccentColor && c.LightStrip == o.LightStrip && c.TerminalDir == o.TerminalDir &&
		c.TerminalNamespace == o.TerminalNamespace && c.HiddenMetrics == o.HiddenMetrics && c.Proxy == o.Proxy && c.MetricsSource == o.MetricsSource &&
		equalIntegrations(c.IntegrationsOff, o.IntegrationsOff) &&
		slices.Equal(c.AccessibleNamespaces, o.AccessibleNamespaces) && c.AgentWrites == o.AgentWrites &&
		c.NodeShellImage == o.NodeShellImage && c.NodeShellPullSecret == o.NodeShellPullSecret && c.NodeShellMaxMinutes == o.NodeShellMaxMinutes &&
		c.HoldBackgroundLogin == o.HoldBackgroundLogin
}

// Check says why settings cannot be kept: the first field out of its bounds.
func (c ClusterSettings) Check() error {
	if err := checkClusterName(c.Name); err != nil {
		return err
	}
	if c.IconColor != "" && !validIconColor(c.IconColor) {
		return fmt.Errorf("icon colour %q: one of %s, or #rrggbb", c.IconColor, strings.Join(IconColors, ", "))
	}
	if c.Icon != "" && !validIconFile(c.Icon) {
		return fmt.Errorf("icon %q is not an image Cogpit keeps", c.Icon)
	}
	if c.IconSource != "" && !validIconFile(c.IconSource) {
		return fmt.Errorf("icon source %q is not an image Cogpit keeps", c.IconSource)
	}
	if c.IconCrop != nil {
		if err := c.IconCrop.Check(); err != nil {
			return err
		}
	}
	if err := CheckIconLetters(c.IconLetters); err != nil {
		return err
	}
	if c.AccentColor != "" && !validIconColor(c.AccentColor) {
		return fmt.Errorf("accent colour %q: one of %s, or #rrggbb", c.AccentColor, strings.Join(IconColors, ", "))
	}
	if err := checkLightStrip(c.LightStrip); err != nil { // lightstrip.go
		return err
	}
	if err := checkTerminalDir(c.TerminalDir); err != nil {
		return err
	}
	if c.TerminalNamespace != "" {
		if errs := validation.IsDNS1123Label(c.TerminalNamespace); len(errs) > 0 {
			return fmt.Errorf("namespace %q: %s", c.TerminalNamespace, strings.Join(errs, "; "))
		}
	}
	if c.HiddenMetrics&^HideAll != 0 {
		return errors.New("hidden metrics: a metric this Cogpit does not know")
	}
	if c.MetricsSource != "" { // read from older files only, and moved into IntegrationsOff (normalize)
		return errors.New("the metrics source is kept as the metrics integrations since D-76")
	}
	if c.IntegrationsOff != nil && *c.IntegrationsOff&^IntAll != 0 {
		return errors.New("integrations: one this Cogpit does not know")
	}
	if err := checkAccessibleNamespaces(c.AccessibleNamespaces); err != nil {
		return err
	}
	if err := CheckNodeShellImage(c.NodeShellImage); err != nil { // nodeshell.go
		return err
	}
	if err := CheckNodeShellPullSecret(c.NodeShellPullSecret); err != nil {
		return err
	}
	if err := CheckNodeShellMaxMinutes(c.NodeShellMaxMinutes); err != nil { // nodeshell_duration.go
		return err
	}
	return CheckProxyURL(c.Proxy)
}

// normalize drops every field of settings read from a file that Check would refuse.
func (c *ClusterSettings) normalize() {
	if checkClusterName(c.Name) != nil {
		c.Name = ""
	}
	c.Name = strings.TrimSpace(c.Name)
	if c.IconColor != "" && !validIconColor(c.IconColor) {
		c.IconColor = ""
	}
	if c.Icon != "" && !validIconFile(c.Icon) {
		c.Icon = ""
	}
	if c.IconSource != "" && !validIconFile(c.IconSource) {
		c.IconSource = ""
	}
	if c.IconCrop != nil && (c.IconSource == "" || c.IconCrop.Check() != nil) {
		c.IconCrop = nil
	}
	if CheckIconLetters(c.IconLetters) != nil {
		c.IconLetters = ""
	}
	if c.AccentColor != "" && !validIconColor(c.AccentColor) {
		c.AccentColor = ""
	}
	if checkLightStrip(c.LightStrip) != nil { // static kept as "", an unknown value dropped
		c.LightStrip = ""
	}
	if checkTerminalDir(c.TerminalDir) != nil {
		c.TerminalDir = ""
	}
	if c.TerminalNamespace != "" && len(validation.IsDNS1123Label(c.TerminalNamespace)) > 0 {
		c.TerminalNamespace = ""
	}
	c.HiddenMetrics &= HideAll
	if c.MetricsSource != "" { // the Metrics source kept before D-76 becomes the two metrics switches
		if CheckMetricsSource(c.MetricsSource) == nil && c.IntegrationsOff == nil {
			off := integrationsOffOfSource(c.MetricsSource)
			c.IntegrationsOff = &off
		}
		c.MetricsSource = ""
	}
	if c.IntegrationsOff != nil {
		off := *c.IntegrationsOff & IntAll
		c.IntegrationsOff = &off
	}
	if c.Proxy = strings.TrimSpace(c.Proxy); CheckProxyURL(c.Proxy) != nil {
		c.Proxy = ""
	}
	c.AccessibleNamespaces = keptAccessibleNamespaces(c.AccessibleNamespaces)
	if CheckNodeShellImage(c.NodeShellImage) != nil {
		c.NodeShellImage = ""
	}
	if CheckNodeShellPullSecret(c.NodeShellPullSecret) != nil {
		c.NodeShellPullSecret = ""
	}
	if CheckNodeShellMaxMinutes(c.NodeShellMaxMinutes) != nil {
		c.NodeShellMaxMinutes = 0
	}
}

func checkClusterName(name string) error {
	switch {
	case !utf8.ValidString(name):
		return errors.New("a cluster's name must be text")
	case utf8.RuneCountInString(name) > maxClusterName:
		return fmt.Errorf("a cluster's name has at most %d characters", maxClusterName)
	case strings.IndexFunc(name, unicode.IsControl) >= 0:
		return errors.New("a cluster's name is one line without control characters")
	}
	return nil
}

// checkTerminalDir refuses a working directory that is not an absolute path, $HOME or ~ (alone or
// followed by a path), on one line.
func checkTerminalDir(dir string) error {
	switch {
	case dir == "":
		return nil
	case len(dir) > maxTerminalDir || !utf8.ValidString(dir) || strings.IndexFunc(dir, unicode.IsControl) >= 0:
		return fmt.Errorf("the working directory is one line of at most %d bytes", maxTerminalDir)
	case dir == homeVariable || dir == "~" || strings.HasPrefix(dir, homeVariable+"/") || strings.HasPrefix(dir, "~/"):
		return nil
	case !filepath.IsAbs(dir):
		return fmt.Errorf("the working directory %q: an absolute path, or one starting with $HOME", dir)
	}
	return nil
}

// ExpandTerminalDir resolves a TerminalDir setting to a directory: $HOME and ~ become the user's
// home directory; empty stays empty (the terminal's default, the home directory).
func ExpandTerminalDir(dir string) (string, error) {
	for _, home := range []string{homeVariable, "~"} {
		if dir == home || strings.HasPrefix(dir, home+"/") {
			h, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("the working directory %s: no home directory: %w", dir, err)
			}
			return filepath.Join(h, strings.TrimPrefix(dir, home)), nil
		}
	}
	return dir, nil
}

// checkSettingsContext refuses a context id that cannot be a key of the settings file.
func checkSettingsContext(id string) error {
	if strings.TrimSpace(id) == "" || len(id) > maxHotbarContext || !utf8.ValidString(id) {
		return errors.New("no context named")
	}
	return nil
}

// ClusterSettingsStore keeps every context's settings in ClusterSettingsFile and their images in
// ClusterIconsDir, under one directory (the app's: <user config dir>/Cogpit). Safe for concurrent
// use; two Cogpit processes keep one copy each and the last change wins.
type ClusterSettingsStore struct {
	dir string // empty: kept in memory only (and no images)

	change     sync.Mutex // serialises ChangeClusterSettings from the write to the UI state
	mu         sync.Mutex
	by         map[string]ClusterSettings
	loaded     bool
	unreadable bool
}

// NewClusterSettingsStore returns the settings kept in dir; an empty dir keeps them in memory only.
func NewClusterSettingsStore(dir string) *ClusterSettingsStore {
	return &ClusterSettingsStore{dir: dir}
}

// Path returns the settings' file; empty when they are kept in memory only.
func (c *ClusterSettingsStore) Path() string {
	if c.dir == "" {
		return ""
	}
	return filepath.Join(c.dir, ClusterSettingsFile)
}

type clusterSettingsDoc struct {
	Version  int                        `json:"version"`
	Clusters map[string]ClusterSettings `json:"clusters"`
}

// load reads the file once. c.mu is held.
func (c *ClusterSettingsStore) load() error {
	if c.loaded {
		return nil
	}
	c.loaded = true
	c.by = map[string]ClusterSettings{}
	var doc clusterSettingsDoc
	if err := readPrefsDoc(c.Path(), &doc); err != nil {
		c.unreadable = true
		return fmt.Errorf("the cluster settings file %s is unreadable (%v); Cogpit starts without and moves the file to %s.unreadable at the first change", c.Path(), err, ClusterSettingsFile)
	}
	if doc.Version != 0 && doc.Version != clusterSettingsVersion {
		c.unreadable = true
		return fmt.Errorf("the cluster settings file %s has format version %d, this Cogpit reads %d; it starts without and moves the file to %s.unreadable at the first change", c.Path(), doc.Version, clusterSettingsVersion, ClusterSettingsFile)
	}
	for _, id := range slices.Sorted(maps.Keys(doc.Clusters)) {
		if len(c.by) == maxClusterSettings || checkSettingsContext(id) != nil {
			continue
		}
		cs := doc.Clusters[id]
		cs.normalize()
		if !cs.IsZero() {
			c.by[id] = cs
		}
	}
	return nil
}

// attach loads the settings into the session's UI state; nil-safe.
func (c *ClusterSettingsStore) attach(s *Session) {
	if c == nil {
		return
	}
	c.mu.Lock()
	err := c.load()
	st := maps.Clone(c.by)
	c.mu.Unlock()
	if err != nil {
		s.log.Warn("cluster settings", "err", err)
	}
	s.ui.ClusterSettings = shownClusterSettings(st) // proxies without their passwords (proxy.go)
}

// ClusterSettingsStore returns the session's cluster settings; nil when it keeps none.
func (s *Session) ClusterSettingsStore() *ClusterSettingsStore { return s.opts.ClusterSettings }

// ClusterSettingsOf returns a context's settings: its defaults when it has none.
func (s *Session) ClusterSettingsOf(context string) (ClusterSettings, error) {
	c := s.opts.ClusterSettings
	if c == nil {
		return ClusterSettings{}, ErrNoClusterSettings
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.load(); err != nil {
		s.log.Warn("cluster settings", "err", err)
	}
	return c.by[context], nil
}

// ChangeClusterSettings applies fn to a copy of a context's settings, checks the result, writes
// the file and shows it (UIState.ClusterSettings, EventUIState). If fn, the check or the write
// fails, nothing changes. An image no context uses any more is removed afterwards.
func (s *Session) ChangeClusterSettings(context string, fn func(*ClusterSettings) error) (ClusterSettings, error) {
	c := s.opts.ClusterSettings
	if c == nil {
		return ClusterSettings{}, ErrNoClusterSettings
	}
	if err := checkSettingsContext(context); err != nil {
		return ClusterSettings{}, err
	}
	c.change.Lock()
	defer c.change.Unlock()
	var dropped []string
	proxyChanged, holdChanged := false, false
	next, all, err := func() (ClusterSettings, map[string]ClusterSettings, error) {
		c.mu.Lock()
		defer c.mu.Unlock()
		if err := c.load(); err != nil {
			s.log.Warn("cluster settings", "err", err)
		}
		old := c.by[context]
		cs := old
		cs.AccessibleNamespaces = slices.Clone(old.AccessibleNamespaces) // fn may append to it
		if old.IntegrationsOff != nil {
			off := *old.IntegrationsOff // fn may change it
			cs.IntegrationsOff = &off
		}
		if err := fn(&cs); err != nil {
			return ClusterSettings{}, nil, err
		}
		cs.Name = strings.TrimSpace(cs.Name)
		cs.Proxy = strings.TrimSpace(cs.Proxy)
		cs.AccessibleNamespaces = sortedAccessibleNamespaces(cs.AccessibleNamespaces)
		if cs.Proxy == RedactProxyURL(old.Proxy) {
			cs.Proxy = old.Proxy // the redacted form handed back as it was shown: unchanged
		}
		if err := cs.Check(); err != nil {
			return ClusterSettings{}, nil, err
		}
		all := maps.Clone(c.by)
		if cs.IsZero() {
			delete(all, context)
		} else {
			if _, ok := all[context]; !ok && len(all) >= maxClusterSettings {
				return ClusterSettings{}, nil, fmt.Errorf("this Cogpit keeps the settings of at most %d clusters", maxClusterSettings)
			}
			all[context] = cs
		}
		if err := writePrefsDoc(c.Path(), &c.unreadable, clusterSettingsDoc{Version: clusterSettingsVersion, Clusters: all}); err != nil {
			return ClusterSettings{}, nil, fmt.Errorf("cluster settings: %w", err)
		}
		c.by = all
		proxyChanged = RedactProxyURL(old.Proxy) != RedactProxyURL(cs.Proxy)
		holdChanged = old.HoldBackgroundLogin != cs.HoldBackgroundLogin
		for _, name := range []string{old.Icon, old.IconSource} {
			if name != "" && name != cs.Icon && name != cs.IconSource && !iconInUse(all, name) {
				dropped = append(dropped, name)
			}
		}
		return cs, maps.Clone(all), nil
	}()
	if err != nil {
		return ClusterSettings{}, err
	}
	for _, name := range dropped {
		c.removeIcon(name)
	}
	shown := shownClusterSettings(all) // proxies without their passwords (proxy.go)
	s.UpdateUI(func(u *UIState) { u.ClusterSettings = shown })
	if proxyChanged {
		// A credential plugin's approval binds the proxy (A23): the context's approval state, which
		// the context lists show, may have changed with it.
		s.emit(Event{Type: EventContexts})
	}
	if holdChanged {
		s.applyLoginHolds() // a cluster that is not shown holds its login, or renews a lapsed one (connwatch_lapse.go)
	}
	return next, nil
}

// equalIntegrations reports whether two choices of integrations are the same; no choice equals
// only no choice.
func equalIntegrations(a, b *IntegrationSet) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// iconInUse reports whether a kept file is a context's icon or an icon's source.
func iconInUse(all map[string]ClusterSettings, icon string) bool {
	for _, cs := range all {
		if cs.Icon == icon || cs.IconSource == icon {
			return true
		}
	}
	return false
}

// equalCrop reports whether two cuts are the same; no cut equals only no cut.
func equalCrop(a, b *IconCrop) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// ClusterName is the name a context shows everywhere in the window (D-68): its display name, else
// the short name of its name without the file it comes from (ShortContextName of ContextLabel:
// "kubernetes-admin@prod-eu [dev.yaml]" → "prod-eu").
func ClusterName(context string, cs ClusterSettings) string {
	if cs.Name != "" {
		return cs.Name
	}
	return ShortContextName(ContextLabel(context))
}

// ClusterSettingsView is a context's settings as cluster.settings.get returns them: what is set,
// what the defaults mean, and where the context comes from.
type ClusterSettingsView struct {
	// Context is the context id, as cluster_contexts lists it.
	Context string `json:"context"`
	// Name is the name the cluster shows: the display name set, else ContextName's short name
	// (the part after its last "@", D-68).
	Name string `json:"name"`
	// ContextName is the context's name in its kubeconfig file, the default display name.
	ContextName string `json:"contextName"`
	// NameSet says whether the user set a display name.
	NameSet bool `json:"nameSet"`
	// IconColor is the icon's colour (one of IconColors); empty: the accent.
	IconColor string `json:"iconColor,omitempty"`
	// IconImage is the uploaded image's format, "png" or "svg"; empty without one.
	IconImage string `json:"iconImage,omitempty"`
	// IconTile says the colour tile shows although an image is kept (D-119).
	IconTile bool `json:"iconTile,omitempty"`
	// IconLetters are the letters the user set for the tile; empty: the name's initials.
	IconLetters string `json:"iconLetters,omitempty"`
	// IconCrop is where the image was cut from its source (D-119); nil without one.
	IconCrop *IconCrop `json:"iconCrop,omitempty"`
	// AccentColor is the cluster's accent colour (one of IconColors, or #rrggbb), the user's mark of
	// which cluster this is (D-80); empty: none. Only the user sets it, in Cogpit's window.
	AccentColor string `json:"accentColor,omitempty"`
	// LightStrip is how the strip under the tabs shows the accent colour while the cluster's tab is
	// shown (D-new): static, breathing, scanner or flow; without an accent colour the strip is off
	// (frosted glass) whatever it says. Only the user chooses it, in Cogpit's window.
	LightStrip string `json:"lightStrip"`
	// Kubeconfig is the kubeconfig file the context comes from; empty for the demo cluster.
	Kubeconfig string `json:"kubeconfig,omitempty"`
	// TerminalDir is the terminals' working directory as set; empty means $HOME.
	TerminalDir string `json:"terminalDir,omitempty"`
	// TerminalNamespace is kubectl's namespace in the terminals as set; empty: the namespace
	// filter's single namespace, else the context's own.
	TerminalNamespace string `json:"terminalNamespace,omitempty"`
	// HiddenMetrics are the metrics the window hides for this cluster and does not ask it for,
	// by name (cluster, nodes, pods, containers, cpu, memory, network, filesystem); empty: all
	// metrics are visible.
	HiddenMetrics []string `json:"hiddenMetrics"`
	// Integrations are the cluster's integrations (D-76): each one's switch and, while the cluster is
	// connected, what Cogpit found of it. IntegrationsDecided says whether the user chose them for
	// this cluster; until then nothing of them is read but what its question ticks (D-82).
	Integrations        []IntegrationStatus `json:"integrations"`
	IntegrationsDecided bool                `json:"integrationsDecided"`
	// MetricsSource is which providers the metrics come from: "auto" (both), "prometheus",
	// "metrics-server" or "none" (the two metrics integrations); MetricsInUse names the providers that answer now, while the
	// cluster is connected ("Prometheus (monitoring/prometheus-server) and Kubernetes Metrics
	// Server"), empty otherwise.
	MetricsSource string `json:"metricsSource"`
	MetricsInUse  string `json:"metricsInUse,omitempty"`
	// Proxy is the proxy of the cluster's API traffic, its password shown as •••; empty: none set
	// (the kubeconfig's proxy-url or the environment's apply).
	Proxy string `json:"proxy,omitempty"`
	// AccessibleNamespaces are the namespaces named for a user who may not list the cluster's
	// namespaces, sorted; empty: none (the namespace filter's, else the context's namespace).
	AccessibleNamespaces []string `json:"accessibleNamespaces"`
	// AgentWrites says whether the user allowed agents' mutating commands in this cluster without
	// asking (destructive commands always ask); only the user changes it, in Cogpit's window.
	AgentWrites bool `json:"agentWrites"`
	// NodeShellImage is the node shells' image as set; empty: NodeShellDefaultImage.
	NodeShellImage string `json:"nodeShellImage,omitempty"`
	// NodeShellDefaultImage is Cogpit's default image of a node shell, pinned by digest.
	NodeShellDefaultImage string `json:"nodeShellDefaultImage"`
	// NodeShellPullSecret is the image pull secret in kube-system as set; empty: none.
	NodeShellPullSecret string `json:"nodeShellPullSecret,omitempty"`
	// NodeShellMaxDuration is how long a node shell's pod lives at most, as set (90m, 2h,
	// 1h30m); empty: NodeShellDefaultMaxDuration.
	NodeShellMaxDuration string `json:"nodeShellMaxDuration,omitempty"`
	// NodeShellDefaultMaxDuration is Cogpit's default maximum duration of a node shell (2h).
	NodeShellDefaultMaxDuration string `json:"nodeShellDefaultMaxDuration"`
	// RenewLoginInBackground says whether the cluster's login is renewed while the cluster is
	// connected but not shown (D-69 item 4; on by default): off, its credential plugin does not run
	// then, and a login that expires lapses until the cluster is shown.
	RenewLoginInBackground bool `json:"renewLoginInBackground"`
	// Connected says whether the context is the connected cluster (its metrics settings apply).
	Connected bool `json:"connected"`
	// Kept says whether the settings outlive this run (false without a configuration directory).
	Kept bool `json:"kept"`
}

// ClusterSettingsView returns a context's settings with what they mean.
func (s *Session) ClusterSettingsView(context string) (ClusterSettingsView, error) {
	cs, err := s.ClusterSettingsOf(context)
	if err != nil {
		return ClusterSettingsView{}, err
	}
	v := ClusterSettingsView{Context: context, Name: ClusterName(context, cs), ContextName: ContextLabel(context),
		NameSet: cs.Name != "", IconColor: cs.IconColor, AccentColor: cs.AccentColor, LightStrip: LightStripOf(cs), TerminalDir: cs.TerminalDir,
		TerminalNamespace: cs.TerminalNamespace, HiddenMetrics: MetricNames(cs.HiddenMetrics),
		Proxy: RedactProxyURL(cs.Proxy), AccessibleNamespaces: append([]string{}, cs.AccessibleNamespaces...),
		AgentWrites: cs.AgentWrites, Kept: s.opts.ClusterSettings.Path() != "", RenewLoginInBackground: !cs.HoldBackgroundLogin,
		NodeShellImage: cs.NodeShellImage, NodeShellDefaultImage: DefaultNodeShellImage, NodeShellPullSecret: cs.NodeShellPullSecret,
		NodeShellDefaultMaxDuration: FormatNodeShellDeadline(DefaultNodeShellDeadline)}
	if cs.NodeShellMaxMinutes != 0 {
		v.NodeShellMaxDuration = FormatNodeShellDeadline(nodeShellDeadline(cs.NodeShellMaxMinutes))
	}
	if cs.Icon != "" {
		v.IconImage = strings.TrimPrefix(filepath.Ext(cs.Icon), ".")
		v.IconTile = cs.IconTile
		v.IconCrop = cs.IconCrop
	}
	v.IconLetters = cs.IconLetters
	if ctxs, err := s.Contexts(); err == nil {
		for _, c := range ctxs {
			if c.Name == context {
				v.Kubeconfig = c.File
				if c.KubeContext != "" {
					v.ContextName = c.KubeContext
					if cs.Name == "" {
						v.Name = ShortContextName(c.KubeContext)
					}
				}
			}
		}
	}
	u := s.UIState()
	v.Connected = u.connectionState(context).Live() // shown or not (D-69)
	v.MetricsSource = MetricsSourceName(string(sourceOfIntegrationsOff(integrationsOffOf(&u, context))))
	v.Integrations, v.IntegrationsDecided = s.IntegrationStatuses(context), u.IntegrationsDecided(context)
	if b := s.BackendOf(context); v.Connected && b != nil && context != "" {
		v.MetricsInUse = metricsInUse(b.Metrics)
	}
	return v, nil
}
