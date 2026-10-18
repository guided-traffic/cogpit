package fyneui

import (
	"errors"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The settings pages (G13): Preferences (⌘,) and a cluster's Settings (⌘⇧,). Lens shows both as
// pages (its cluster settings as a tab "Settings - <context>" with the cluster's icon and a page
// list on the left, captured 2026-10-11; its Preferences as a page, inferred). Cogpit does not open
// them as tabs of its tab bar (tabs.go): a settings page takes the place of the navigator, the tab
// bar, the views and the dock until it is closed (its × or Escape); the hotbar, the top bar and
// the status bar stay. Which page is open is
// UI state (UIState.Settings: preferences.open, cluster.settings.open, settings.close), so an
// agent sees it and can open one; the section list dispatches the open command with the section.
// Each section shows its settings under upper-case headings with a help line below each, as the
// captures show Lens's; a change is dispatched at once (a text field after a typing pause), and
// the engine's refusal shows under the field.

// settingsTyping is the typing pause after which a text field's value is set.
const settingsTyping = 600 * time.Millisecond

// settingsPage is the open settings page.
type settingsPage struct {
	sh *shell

	root   *fyne.Container // the page, in the window's centre while open
	title  *textLine
	badge  *clusterBadge // the cluster's icon beside the title; hidden on Preferences
	nav    *fyne.Container
	body   *fyne.Container
	closeB *widget.Button

	typing  time.Duration       // the typing pause before a text field's value is set (tests shorten it)
	shown   engine.SettingsPage // what the page shows; Page is empty while it is closed
	built   string              // the page, context and section the body's controls were built for
	view    *engine.ClusterSettingsView
	viewSeq int
	syncing bool // controls being set from the state: their callbacks do not dispatch

	// The shown section's controls, by setting name, for updates from the state and for tests.
	controls map[string]fyne.CanvasObject
	errs     map[string]*textLine
	timers   map[string]*time.Timer
	sync     func() // sets the shown section's controls from the state
	navItems map[string]*widget.Button

	// The window's dialogs, replaced by tests: an image file, a folder (the system's panels, else
	// Fyne's dialogs, filepicker_choosers.go), the file manager.
	pickIcon      func(done func(path string))
	pickFolder    func(done func(path string))
	openFolder    func(path string)
	prom          *promFields // the Metrics section's Prometheus fields (clustersettings_prometheus.go)
	iconMenuModel *fyne.Menu  // the icon's open "…" menu, for tests

	agentsDialog *agentsDialog // the open question before agents' changes are allowed (clustersettings_agents.go)
	agent        *agentPrefs   // Preferences › Agent while it is built (preferences_agent.go)

	// integrationRows are the Integrations section's rows while it is built (clustersettings_integrations.go).
	integrationRows []*integrationRow
}

func newSettingsPage(sh *shell) *settingsPage {
	p := &settingsPage{sh: sh, typing: settingsTyping, controls: map[string]fyne.CanvasObject{}, errs: map[string]*textLine{},
		timers: map[string]*time.Timer{}, navItems: map[string]*widget.Button{}}
	p.title = newTextLine("", theme.ColorNameForeground, theme.SizeNameHeadingText, fyne.TextStyle{Bold: true})
	p.title.truncate = true
	p.badge = newClusterBadge()
	p.badge.side = sizeHotbarTile
	p.closeB = widget.NewButtonWithIcon("", theme.CancelIcon(), func() { sh.dispatch("settings.close", struct{}{}, nil) })
	p.closeB.Importance = widget.LowImportance
	p.nav = container.NewVBox()
	p.body = container.NewStack()
	header := inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, container.NewBorder(nil, nil,
		container.NewHBox(container.NewCenter(p.badge), newHGap(theme.SizeNamePadding)), container.NewCenter(p.closeB),
		container.NewVBox(layout.NewSpacer(), p.title, layout.NewSpacer())))
	navPanel := sized(sizeSidebarWidth, "", newSurface(colorSidebar),
		container.NewBorder(nil, nil, nil, newDivider(true), inset(theme.SizeNamePadding, theme.SizeNameInnerPadding, container.NewVBox(p.nav))))
	content := container.NewBorder(container.NewVBox(header, newDivider(false)), nil, nil, nil,
		container.NewVScroll(inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding, box(columnLayout{}, p.body))))
	p.root = container.NewStack(newSurface(theme.ColorNameBackground), container.NewBorder(nil, nil, navPanel, nil, content))
	p.pickIcon = p.pickIconPanel
	p.pickFolder = p.pickFolderPanel
	p.openFolder = sh.openFolderWithFileManager
	return p
}

// visible reports whether a settings page is open.
func (p *settingsPage) visible() bool { return p.shown.Page != "" }

// stateChanged follows UIState.Settings: opens, closes or switches the page, then shows the state
// in its controls. Main goroutine.
func (p *settingsPage) stateChanged() {
	sh := p.sh
	st := sh.ui.Settings
	if st == nil {
		if p.visible() {
			p.stopTimers()
			p.shown, p.built, p.view = engine.SettingsPage{}, "", nil
			sh.center.Objects = []fyne.CanvasObject{sh.mainRow}
			sh.center.Refresh()
		}
		return
	}
	if !p.visible() {
		sh.tip.hide()
		sh.win.Canvas().Unfocus() // the keys must not go on to a field the page hides
		sh.center.Objects = []fyne.CanvasObject{p.root}
		sh.center.Refresh()
	}
	p.shown = *st
	if key := st.Page + "\x00" + st.Context + "\x00" + st.Section; key != p.built {
		if st.Page == engine.SettingsCluster && (p.view == nil || p.view.Context != st.Context) {
			p.view = nil
		}
		p.stopTimers()
		p.built = key
		p.build()
	}
	p.renderNav()
	p.identityChanged()
	if st.Page == engine.SettingsCluster {
		p.requestView()
	}
	p.syncControls()
}

// build makes the shown section's controls.
func (p *settingsPage) build() {
	clear(p.controls)
	clear(p.errs)
	p.sync, p.prom, p.integrationRows = nil, nil, nil
	if p.agent != nil && p.agent.form != nil {
		p.agent.form.close() // a connection test under way ends with its section
	}
	p.agent = nil
	var content fyne.CanvasObject
	if p.shown.Page == engine.SettingsCluster {
		content = p.clusterSection(p.shown.Section)
	} else {
		content = p.preferencesSection(p.shown.Section)
	}
	p.body.Objects = []fyne.CanvasObject{content}
	p.body.Refresh()
}

// renderNav shows the page's sections, the shown one highlighted.
func (p *settingsPage) renderNav() {
	sections, labels := engine.PreferencesSections, preferencesSectionLabels
	if p.shown.Page == engine.SettingsCluster {
		sections, labels = engine.ClusterSettingsSections, clusterSectionLabels
	}
	objs := make([]fyne.CanvasObject, 0, len(sections)+1)
	clear(p.navItems)
	for _, s := range sections {
		if s == engine.SectionAgent && !p.sh.chat.available() {
			continue // a window without the built-in agent has no connections to set (preferences_agent.go)
		}
		section := s
		b := widget.NewButton(labels[s], func() { p.openSection(section) })
		b.Alignment = widget.ButtonAlignLeading
		b.Importance = widget.LowImportance
		if s == p.shown.Section {
			b.Importance = widget.HighImportance
		}
		p.navItems[s] = b
		if p.shown.Page == engine.SettingsCluster && s == engine.SectionIntegrations {
			// The integrations under a divider, their heading a section of its own (D-87,
			// clustersettings_nav.go).
			objs = append(objs, navDivider(), newNavHeading(b, labels[s]))
			continue
		}
		objs = append(objs, b)
	}
	p.nav.Objects = objs
	p.nav.Refresh()
}

// openSection shows another section of the open page, through the engine.
func (p *settingsPage) openSection(section string) {
	if p.shown.Page == engine.SettingsCluster {
		p.sh.dispatch("cluster.settings.open", clusterSettingsOpenParams{Context: p.shown.Context, Section: section}, nil)
		return
	}
	p.sh.dispatch("preferences.open", preferencesOpenParams{Section: section}, nil)
}

// identityChanged shows the title: "Preferences", or "Settings - <name>" with the cluster's icon,
// as Lens's tab is titled (captured).
func (p *settingsPage) identityChanged() {
	if !p.visible() {
		return
	}
	if p.shown.Page != engine.SettingsCluster {
		p.title.SetText("Preferences")
		p.badge.Hide()
		return
	}
	id := p.sh.identity(p.shown.Context)
	p.title.SetText("Settings - " + id.name)
	p.badge.setIdentity(id)
	p.badge.Show()
	if b, ok := p.controls["iconPreview"].(*clusterBadge); ok {
		b.setIdentity(id)
	}
}

// syncControls sets the shown section's controls from the state.
func (p *settingsPage) syncControls() {
	if p.sync == nil {
		return
	}
	p.syncing = true
	defer func() { p.syncing = false }()
	p.sync()
}

func (p *settingsPage) stopTimers() {
	for k, t := range p.timers {
		t.Stop()
		delete(p.timers, k)
	}
}

// --- building blocks of a section ---

// heading is a section's upper-case heading, as Lens's pages have them (captured).
func heading(text string) fyne.CanvasObject {
	return newTextLine(strings.ToUpper(text), colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{Bold: true})
}

// setting is one setting: its heading, its control, a help line and the line that shows why the
// engine refused a value.
func (p *settingsPage) setting(name, title, help string, control fyne.CanvasObject) fyne.CanvasObject {
	errLine := newTextLine("", colorStatusError, theme.SizeNameCaptionText, fyne.TextStyle{})
	errLine.Hide()
	p.errs[name] = errLine
	p.controls[name] = control
	objs := []fyne.CanvasObject{heading(title), control}
	if help != "" {
		h := newTextLine(help, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
		h.truncate = true
		objs = append(objs, h)
	}
	objs = append(objs, errLine, newSpace(theme.SizeNameInnerPadding))
	return container.NewVBox(objs...)
}

// showResult shows a refusal under a setting, or clears it.
func (p *settingsPage) showResult(name string, err error) {
	l := p.errs[name]
	if l == nil {
		p.sh.showError(err)
		return
	}
	if err == nil {
		l.SetText("")
		l.Hide()
		return
	}
	var dis *engine.DisabledError
	if errors.As(err, &dis) {
		l.SetText(dis.Reason)
	} else {
		l.SetText(err.Error())
	}
	l.Show()
}

// typed sets a text field's value after a typing pause.
func (p *settingsPage) typed(name string, set func()) {
	if p.syncing {
		return
	}
	if t := p.timers[name]; t != nil {
		t.Stop()
	}
	built := p.built
	p.timers[name] = time.AfterFunc(p.typing, func() {
		p.sh.do(func() {
			delete(p.timers, name)
			if p.built == built { // the section is still the one typed in
				set()
			}
		})
	})
}

// setEntry shows a value in a text field the user is not typing in; focus is the field's
// focusable widget (a select entry's own).
func (p *settingsPage) setEntry(focus fyne.Focusable, e *widget.Entry, name, value string) {
	if p.timers[name] != nil || p.sh.win.Canvas().Focused() == focus || e.Text == value {
		return
	}
	e.SetText(value)
}

// columnLayout lays its objects out at most settingsColumn points wide, from the left: a page's
// fields do not stretch across a wide window.
type columnLayout struct{}

// settingsColumn is the widest a settings page's column grows.
const settingsColumn = 640

func (columnLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var ms fyne.Size
	for _, o := range objs {
		if o.Visible() {
			ms = ms.Max(o.MinSize())
		}
	}
	return ms
}

func (columnLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	w := min(size.Width, max(settingsColumn, 0))
	for _, o := range objs {
		o.Move(fyne.NewPos(0, 0))
		o.Resize(fyne.NewSize(max(w, o.MinSize().Width), size.Height))
	}
}

// Parameters of the settings commands, mirroring the engine's (commands/preferences.go,
// commands/clustersettings.go).
type (
	preferencesOpenParams struct {
		Section string `json:"section,omitempty"`
	}
	clusterSettingsOpenParams struct {
		Context string `json:"context,omitempty"`
		Section string `json:"section,omitempty"`
	}
	clusterSettingsContext struct {
		Context string `json:"context,omitempty"`
	}
	clusterIconParams struct {
		Context string `json:"context,omitempty"`
		Path    string `json:"path"`
	}
)
