package fyneui

import (
	"image/color"
	"slices"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/terminal"
	"github.com/guided-traffic/cogpit/internal/ui/fyneui/yamlview"
)

// Preferences (G13, ⌘,; engine/preferences.go). The window applies UIState.Preferences whenever
// it changes, whoever changed it: the theme (unless --theme set one for the run) and the tables'
// row height through Cogpit's theme, the time zone in every absolute time the window writes
// (timeFormat; the engine writes the drawer's in it), the editor's font size, line numbers and tab size in
// every open editor tab, the terminal's font size in the terminals opened afterwards (a running
// terminal keeps the size its grid was laid out in). The page's sections: App (theme, time zone),
// Proxy (the proxy of the traffic that is no cluster's, clustersettings_proxy.go), Kubernetes (the
// kubeconfig syncs, Cogpit's existing dialogs; kubectl for terminals), Editor, Terminal, Tables.

// preferencesSectionLabels are the Preferences page's section titles (Cogpit's grouping, inferred).
var preferencesSectionLabels = map[string]string{
	engine.SectionApp: "App", engine.SectionProxy: proxySectionLabel, engine.SectionKubernetes: "Kubernetes", engine.SectionEditor: "Editor",
	engine.SectionTerminal: "Terminal", engine.SectionTables: "Tables",
}

// themeChoices are the theme's options as Lens names them (captured from its documentation).
var themeChoices = []struct{ label, value string }{
	{"Sync with computer", engine.ThemeSystem}, {"Dark", engine.ThemeDark}, {"Light", engine.ThemeLight},
}

// timezoneChoices are offered in the time zone field; any IANA name can be typed.
var timezoneChoices = []string{"UTC", "Europe/Berlin", "Europe/London", "Europe/Paris", "America/New_York",
	"America/Chicago", "America/Los_Angeles", "America/Sao_Paulo", "Asia/Tokyo", "Asia/Singapore", "Asia/Kolkata",
	"Australia/Sydney"}

// preferencesChanged applies the preferences in the UI state to the window. Main goroutine.
func (sh *shell) preferencesChanged() {
	p := sh.ui.Preferences
	if p == nil {
		return
	}
	mode := sh.opts.Theme // --theme wins for the run
	if mode == "" {
		mode = ThemeMode(p.Theme)
	}
	if cur, ok := sh.app.Settings().Theme().(*cogTheme); !ok || cur.mode != mode || cur.rowHeight != p.TableRowHeight {
		sh.app.Settings().SetTheme(newThemeFor(mode, p.TableRowHeight)) // Fyne refreshes every widget
	}
	var zone *time.Location
	if p.Timezone != "" {
		zone, _ = engine.LoadTimezone(p.Timezone)
	}
	if zone.String() != sh.zone.String() || (zone == nil) != (sh.zone == nil) {
		sh.zone = zone
		sh.drawer.request()        // the drawer's times, which the engine writes in the zone
		sh.drawer.metricsChanged() // the charts' time axes
		if sh.view != nil {
			sh.view.refreshAges() // an event's Last Seen
		}
		for _, c := range sh.dock.contents {
			if lt := c.logs; lt != nil && lt.view != nil {
				lt.view.SetView(viewOf(lt.tab, sh.timeFormat())) // the shown timestamps
			}
		}
	}
	for _, v := range sh.editorViews() {
		sh.applyEditorPrefs(v)
	}
	sh.applyTerminalPrefs() // the right mouse button in the open terminals (termpaste.go)
	// Load chart icons (helmcharticons.go).
	sh.chartIconsPrefChanged()
}

// loc is the time zone the window writes absolute times in: the Preferences' time zone, else the
// clock's (the computer's).
func (sh *shell) loc() *time.Location {
	if sh.zone != nil {
		return sh.zone
	}
	return sh.now().Location()
}

// timeFormat writes absolute times as the window shows them (D-31): the engine's layout, the
// computer's region format (UIState.TimeFormat), in the Preferences' time zone. Every time the
// window writes goes through it: tables, charts, notifications, the audit log, logs, banners.
func (sh *shell) timeFormat() model.TimeFormat { return sh.ui.TimeFormat.In(sh.loc()) }

// applyEditorPrefs gives an editor tab's YAML view the Preferences' font size, line numbers and
// tab size.
func (sh *shell) applyEditorPrefs(v *yamlview.View) {
	p := sh.ui.Preferences
	if p == nil || v == nil {
		return
	}
	v.SetTextSize(float32(p.EditorFontSize))
	v.SetLineNumbers(p.EditorLineNumbers)
	v.SetTabSize(p.EditorTabSize)
}

// editorViews are the YAML views of the open Edit, Create resource, Helm Upgrade and Helm Install
// tabs.
func (sh *shell) editorViews() []*yamlview.View {
	var out []*yamlview.View
	for _, c := range sh.dock.contents {
		switch e := c.editor.(type) {
		case *editorTab:
			out = append(out, e.pane.view)
		case *createTab:
			out = append(out, e.pane.view)
		case *helmUpgradeTab:
			out = append(out, e.pane.view)
		case *helmInstallTab:
			out = append(out, e.pane.view)
		}
	}
	return out
}

// terminalScope wraps a new terminal widget so that it draws at the Preferences' terminal font
// size: Cogpit's theme with that text size, its colours following the app's theme.
func (sh *shell) terminalScope(w fyne.CanvasObject) fyne.CanvasObject {
	p := sh.ui.Preferences
	if p == nil || p.TerminalFontSize == 0 || p.TerminalFontSize == textSize {
		return w
	}
	return container.NewThemeOverride(w, &textSizeTheme{app: sh.app, size: float32(p.TerminalFontSize)})
}

// textSizeTheme is the app's current theme with another text size.
type textSizeTheme struct {
	app  fyne.App
	size float32
}

func (t *textSizeTheme) cur() fyne.Theme { return t.app.Settings().Theme() }

func (t *textSizeTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	return t.cur().Color(n, v)
}
func (t *textSizeTheme) Font(s fyne.TextStyle) fyne.Resource     { return t.cur().Font(s) }
func (t *textSizeTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return t.cur().Icon(n) }
func (t *textSizeTheme) Size(n fyne.ThemeSizeName) float32 {
	if n == theme.SizeNameText {
		return t.size
	}
	return t.cur().Size(n)
}

// setPreference dispatches preferences.set for one setting and shows a refusal under it.
func (p *settingsPage) setPreference(name string, value any) {
	if p.syncing {
		return
	}
	p.sh.dispatchInOrder("preferences.set", map[string]any{name: value}, p.settled(name))
}

// preferencesSection builds one section of the Preferences page.
func (p *settingsPage) preferencesSection(section string) fyne.CanvasObject {
	switch section {
	case engine.SectionProxy:
		return p.preferencesProxySection() // clustersettings_proxy.go
	case engine.SectionKubernetes:
		return p.kubernetesSection()
	case engine.SectionEditor:
		return p.editorSection()
	case engine.SectionTerminal:
		return p.terminalSection()
	case engine.SectionTables:
		return p.tablesSection()
	case engine.SectionAgent:
		return p.agentSection() // preferences_agent.go
	}
	return p.appSection()
}

func (p *settingsPage) appSection() fyne.CanvasObject {
	labels := make([]string, len(themeChoices))
	for i, c := range themeChoices {
		labels[i] = c.label
	}
	themeSel := newSelectField(labels, func(label string) {
		for _, c := range themeChoices {
			if c.label == label {
				p.setPreference("theme", c.value)
			}
		}
	})
	themeHelp := "Dark and light are Cogpit's night and day stages; Sync with computer follows the system's appearance."
	if p.sh.opts.Theme != "" {
		themeHelp = "This run was started with --theme " + string(p.sh.opts.Theme) + "; the choice applies from the next start."
	}
	zone := newFieldSelectEntry(timezoneChoices) // Escape closes the card (D-116, D-118)
	name, _ := p.sh.now().Zone()
	zone.SetPlaceHolder("The computer's (" + name + ")")
	zone.OnChanged = func(string) {
		p.typed("timezone", func() { p.setPreference("timezone", strings.TrimSpace(zone.Text)) })
	}
	loginItem, syncLoginItem := p.loginItemSetting()  // nil where not offered (preferences_loginitem.go)
	updates, syncUpdates := p.updatesSettings()       // preferences_updates.go
	restore, syncRestore := p.restoreSessionSetting() // preferences_restore.go
	motion, syncMotion := p.reduceMotionSetting()     // preferences_motion.go
	p.sync = func() {
		pr := p.prefs()
		for _, c := range themeChoices {
			if c.value == pr.Theme {
				themeSel.SetSelected(c.label)
			}
		}
		syncUpdates()
		p.setEntry(zone, &zone.Entry, "timezone", pr.Timezone)
		syncRestore()
		syncMotion()
		syncLoginItem()
	}
	// Two real groups, one bracket each: what the app is and does, and how it updates.
	general := []fyne.CanvasObject{p.setting("theme", "Theme", themeHelp, themeSel),
		p.setting("timezone", "Locale timezone", "Absolute times: the drawer's Created, the charts' time axes and hover. Empty: the computer's.", zone),
		restore,
		motion,
	}
	if loginItem != nil {
		general = append(general, loginItem)
	}
	return container.NewVBox(
		p.settingsGroup("general · "+strconv.Itoa(len(general)), "", general...),
		p.settingsGroup("updates · "+strconv.Itoa(len(updates)), "", updates...))
}

func (p *settingsPage) kubernetesSection() fyne.CanvasObject {
	kc := p.sh.kc
	sources := newPlateButton("Kubeconfig Sources…", nil, kc.showSources)
	syncB := newPlateButton("Sync Files and Folders…", nil, kc.addKubeconfigs)
	helmDriver, syncHelmDriver := p.helmDriverSetting() // preferences_helm.go
	chartIcons, syncChartIcons := p.chartIconsSetting() // helmcharticons.go
	// The default for new clusters' integrations (preferences_integrations.go, D-76).
	integrations, syncIntegrations := p.newClusterIntegrationsSetting()
	debugImage, syncDebugImage := p.debugImageSetting() // preferences_debug.go (D-102)
	// Set before kubectlSettings, which chains p.sync.
	p.sync = func() { syncHelmDriver(); syncChartIcons(); syncIntegrations(); syncDebugImage() }
	programFolders := p.programFoldersSetting() // preferences_programs.go (D-120); chains p.sync
	return container.NewVBox(append([]fyne.CanvasObject{
		p.setting("kubeconfigSyncs", "Kubeconfig syncs", "The files and folders Cogpit reads kubeconfigs from; ~/.kube is synced by default.",
			container.NewHBox(sources, syncB)),
		programFolders,
		helmDriver,
		chartIcons,
		integrations,
		debugImage,
	}, p.kubectlSettings()...)...) // kubectl for terminals (kubectloffer_prefs.go)
}

// sizeChoices are the font sizes offered, with the current one if it is another.
func sizeChoices(cur int, sizes ...int) []string {
	if cur > 0 && !slices.Contains(sizes, cur) {
		sizes = append(slices.Clone(sizes), cur)
		slices.Sort(sizes)
	}
	out := make([]string, len(sizes))
	for i, s := range sizes {
		out[i] = strconv.Itoa(s)
	}
	return out
}

// intSelect is a select of numbers that sets a preference.
func (p *settingsPage) intSelect(name string, options []string) *selectField {
	return newSelectField(options, func(s string) {
		if n, err := strconv.Atoi(s); err == nil {
			p.setPreference(name, n)
		}
	})
}

var fontSizes = []int{10, 11, 12, 13, 14, 15, 16, 18, 20, 22, 24}

func (p *settingsPage) editorSection() fyne.CanvasObject {
	pr := p.prefs()
	font := p.intSelect("editorFontSize", sizeChoices(pr.EditorFontSize, fontSizes...))
	numbers := widget.NewCheck("Show line numbers", func(on bool) { p.setPreference("editorLineNumbers", on) })
	tab := p.intSelect("editorTabSize", sizeChoices(pr.EditorTabSize, 1, 2, 4, 8))
	p.sync = func() {
		pr := p.prefs()
		font.SetSelected(strconv.Itoa(pr.EditorFontSize))
		numbers.SetChecked(pr.EditorLineNumbers)
		tab.SetSelected(strconv.Itoa(pr.EditorTabSize))
	}
	return container.NewVBox(
		p.setting("editorFontSize", "Font size", "The YAML editor of the Edit, Create resource, Helm Upgrade and Helm Install tabs, in points.", font),
		p.setting("editorLineNumbers", "Line numbers", "", numbers),
		p.setting("editorTabSize", "Tab size", "Spaces Tab indents by; YAML indents with spaces only.", tab),
	)
}

func (p *settingsPage) terminalSection() fyne.CanvasObject {
	pr := p.prefs()
	shell := newFieldEntry()                      // Escape closes the card, the value typed set first (D-116, D-118)
	shell.SetPlaceHolder(terminal.DefaultShell()) // $SHELL on macOS and Linux, PowerShell's on Windows
	shell.OnChanged = func(string) {
		p.typed("terminalShell", func() { p.setPreference("terminalShell", strings.TrimSpace(shell.Text)) })
	}
	args := newFieldMultiLineEntry()
	args.SetPlaceHolder("one argument per line")
	args.SetMinRowsVisible(3)
	args.OnChanged = func(string) {
		p.typed("terminalShellArgs", func() { p.setPreference("terminalShellArgs", shellArgs(args.Text)) })
	}
	font := p.intSelect("terminalFontSize", sizeChoices(pr.TerminalFontSize, fontSizes...))
	rightClick, syncRightClick := p.rightClickSetting() // termpaste.go
	p.sync = func() {
		pr := p.prefs()
		p.setEntry(shell, &shell.Entry, "terminalShell", pr.TerminalShell)
		p.setEntry(args, &args.Entry, "terminalShellArgs", strings.Join(pr.TerminalShellArgs, "\n"))
		font.SetSelected(strconv.Itoa(pr.TerminalFontSize))
		syncRightClick()
	}
	return container.NewVBox(
		p.setting("terminalShell", "Shell path", "The local terminal's shell, an absolute path; pod shells are not affected.", shell),
		p.setting("terminalShellArgs", "Shell arguments", "Passed to the shell after its name, one per line.", args),
		p.setting("terminalFontSize", "Font size", "The dock's terminals opened from now on, in points.", font),
		p.setting("terminalRightClickCopyPaste", rightClickTitle, rightClickHelp, rightClick),
	)
}

// shellArgs splits the arguments field: one argument per line, blank lines dropped.
func shellArgs(text string) []string {
	out := []string{}
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimRight(l, "\r"); strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func (p *settingsPage) tablesSection() fyne.CanvasObject {
	def := "Default (" + strconv.Itoa(defaultRowHeight()) + ")"
	heights := []int{20, 22, 24, 26, 28, 30, 32, 36, 40, 44, 48}
	opts := append([]string{def}, sizeChoices(0, heights...)...)
	row := newSelectField(opts, func(s string) {
		if s == def {
			p.setPreference("tableRowHeight", 0)
		} else if n, err := strconv.Atoi(s); err == nil {
			p.setPreference("tableRowHeight", n)
		}
	})
	p.sync = func() {
		if h := p.prefs().TableRowHeight; h > 0 {
			row.SetSelected(strconv.Itoa(h))
		} else {
			row.SetSelected(def)
		}
	}
	return container.NewVBox(
		p.setting("tableRowHeight", "Row line height", "The height of a row in every table, in points.", row),
	)
}

// prefs are the preferences the page shows: the UI state's, else the defaults.
func (p *settingsPage) prefs() engine.Preferences {
	if pr := p.sh.ui.Preferences; pr != nil {
		return *pr
	}
	return engine.DefaultPreferences()
}
