package fyneui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/model"
)

// ThemeMode picks Cogpit's colour palette.
type ThemeMode string

// Theme modes. The zero value means ThemeDark: Cogpit starts dark, as Lens does.
const (
	ThemeDark   ThemeMode = "dark"
	ThemeLight  ThemeMode = "light"
	ThemeSystem ThemeMode = "system" // follow the operating system's light or dark setting
)

// Cogpit's own colour tokens, beyond Fyne's. Widgets look them up through the theme so that no
// widget decides a colour itself (docs/developer/conventions.md, UI).
const (
	colorTopBar      fyne.ThemeColorName = "cogpit.topBar"
	colorStatusBar   fyne.ThemeColorName = "cogpit.statusBar"
	colorSidebar     fyne.ThemeColorName = "cogpit.sidebar"
	colorRowHover    fyne.ThemeColorName = "cogpit.rowHover"
	colorRowSelected fyne.ThemeColorName = "cogpit.rowSelected"
	// colorContainerRunning is a running container's square in a pod's Containers cell: the
	// Running green Cogpit had before the Solarized theme, which the owner wants there (2026-10-12).
	colorContainerRunning fyne.ThemeColorName = "cogpit.containerRunning"
	colorRowSelectedText  fyne.ThemeColorName = "cogpit.rowSelectedText" // text on a selected row
	colorDrawer           fyne.ThemeColorName = "cogpit.drawer"
	colorDrawerHeader     fyne.ThemeColorName = "cogpit.drawerHeader"
	colorDock             fyne.ThemeColorName = "cogpit.dock"
	colorDockTab          fyne.ThemeColorName = "cogpit.dockTab"
	colorDockTabText      fyne.ThemeColorName = "cogpit.dockTabText" // the active tab's title
	colorDockTabDim       fyne.ThemeColorName = "cogpit.dockTabDim"  // an inactive tab's title
	colorMuted            fyne.ThemeColorName = "cogpit.muted"
	colorAccent           fyne.ThemeColorName = "cogpit.accent"
	colorChartSecond      fyne.ThemeColorName = "cogpit.chartSecond" // a chart's second series (Transmit, Write); the first is the accent
	colorChip             fyne.ThemeColorName = "cogpit.chip"
	colorToast            fyne.ThemeColorName = "cogpit.toast"     // a toast's surface: stronger than the window
	colorToastText        fyne.ThemeColorName = "cogpit.toastText" // the text on it
	colorLink             fyne.ThemeColorName = "cogpit.link"      // a cell or drawer value that names an object
	colorTooltip          fyne.ThemeColorName = "cogpit.tooltip"   // a tooltip's background
	// colorCode is inline code's text on its chip in the agent chat's answers (chat_markdown.go): a
	// warm yellow, as the owner's design direction has it (plan/captures/chat), 4.5:1 on the chip.
	colorCode fyne.ThemeColorName = "cogpit.code"
	// colorToastSelection is selected text on a toast: the theme's selection is too close to the
	// dark toast's surface to be seen (D-99).
	colorToastSelection fyne.ThemeColorName = "cogpit.toastSelection"
)

// Status colour tokens, one per model.Status.
const (
	colorStatusOK          fyne.ThemeColorName = "cogpit.status.ok"
	colorStatusSucceeded   fyne.ThemeColorName = "cogpit.status.succeeded"
	colorStatusPending     fyne.ThemeColorName = "cogpit.status.pending"
	colorStatusWarning     fyne.ThemeColorName = "cogpit.status.warning"
	colorStatusError       fyne.ThemeColorName = "cogpit.status.error"
	colorStatusTerminating fyne.ThemeColorName = "cogpit.status.terminating"
	colorStatusUnknown     fyne.ThemeColorName = "cogpit.status.unknown"
	colorStatusStopped     fyne.ThemeColorName = "cogpit.status.stopped"
	colorStatusEphemeral   fyne.ThemeColorName = "cogpit.status.ephemeral"
)

// Cogpit's own size tokens. Fyne's sizes (text, padding) are set for density in [cogTheme.Size].
const (
	sizeSidebarWidth   fyne.ThemeSizeName = "cogpit.sidebarWidth"   // the navigator's width
	sizeHotbarWidth    fyne.ThemeSizeName = "cogpit.hotbarWidth"    // the hotbar's width (hotbar.go)
	sizeHotbarTile     fyne.ThemeSizeName = "cogpit.hotbarTile"     // a hotbar tile's square
	sizeNavBadge       fyne.ThemeSizeName = "cogpit.navBadge"       // the cluster's badge at the navigator's root (navigator.go)
	sizeBarHeight      fyne.ThemeSizeName = "cogpit.barHeight"      // the top bar's height
	sizeStatusHeight   fyne.ThemeSizeName = "cogpit.statusHeight"   // the status bar's height
	sizeStatusText     fyne.ThemeSizeName = "cogpit.statusText"     // the status bar's text
	sizeCellPadding    fyne.ThemeSizeName = "cogpit.cellPadding"    // horizontal padding inside a table cell
	sizeRowPadding     fyne.ThemeSizeName = "cogpit.rowPadding"     // vertical padding inside a table row
	sizeDrawerLabel    fyne.ThemeSizeName = "cogpit.drawerLabel"    // the name column of the drawer's details
	sizeChipRadius     fyne.ThemeSizeName = "cogpit.chipRadius"     // corner radius of chips and pills
	sizeSortIcon       fyne.ThemeSizeName = "cogpit.sortIcon"       // the sort arrow in a column header
	sizeDotDiameter    fyne.ThemeSizeName = "cogpit.dot"            // connection and status dots
	sizeToastWidth     fyne.ThemeSizeName = "cogpit.toastWidth"     // the width of a toast
	sizeFilterWidth    fyne.ThemeSizeName = "cogpit.filterWidth"    // the namespace filter button
	sizeSearchWidth    fyne.ThemeSizeName = "cogpit.searchWidth"    // the search entry
	sizeFindCountWidth fyne.ThemeSizeName = "cogpit.findCountWidth" // the find widget's "1 of 1"
	sizePickerWidth    fyne.ThemeSizeName = "cogpit.pickerWidth"    // the palette's and dialogs' width (the context picker's until D-57)
	sizePickerHeight   fyne.ThemeSizeName = "cogpit.pickerHeight"   // the palette's and dialogs' height
	sizeConfirmWidth   fyne.ThemeSizeName = "cogpit.confirmWidth"   // the confirmation dialog
	sizePopupMaxList   fyne.ThemeSizeName = "cogpit.popupMaxList"   // the tallest list in a popup
	sizeDockTabMaxLen  fyne.ThemeSizeName = "cogpit.dockTabMaxLen"  // the widest dock tab title
	sizeMenuColumn     fyne.ThemeSizeName = "cogpit.menuColumn"     // the ⋮ column at each row's end
	sizeCellIcon       fyne.ThemeSizeName = "cogpit.cellIcon"       // an icon in a table cell (the warning triangle, ⋮)
	sizeTooltipWidth   fyne.ThemeSizeName = "cogpit.tooltipWidth"   // the widest tooltip; longer text wraps
	sizeTooltipOffset  fyne.ThemeSizeName = "cogpit.tooltipOffset"  // a tooltip's distance from the pointer
	sizeDialogWidth    fyne.ThemeSizeName = "cogpit.dialogWidth"    // a command's parameter dialog
	sizeAboutWidth     fyne.ThemeSizeName = "cogpit.aboutWidth"     // About Cogpit's column (about.go)
	sizeHealthBar      fyne.ThemeSizeName = "cogpit.healthBar"      // a health bar's height (Workloads Overview)
	sizeLogo           fyne.ThemeSizeName = "cogpit.logo"           // the header's mark (D-24: 20 pixels)
)

// palette is one variant's colours: Solarized Dark and Solarized Light as VS Code's built-in themes
// colour them (D-22), copied here once; the comments name the VS Code key a value comes from. Status
// hues are spread so that no two tokens are confused at a glance.
type palette struct {
	fyne   map[fyne.ThemeColorName]color.Color
	status map[model.Status]color.Color
}

func hex(rgb uint32) color.NRGBA {
	return color.NRGBA{R: uint8(rgb >> 16), G: uint8(rgb >> 8), B: uint8(rgb), A: 0xff}
}

func alpha(c color.NRGBA, a uint8) color.NRGBA { c.A = a; return c }

// solCyan is Solarized's cyan, Cogpit's accent in the dark theme. Status tokens are Solarized's
// accents too, lightened (dark) or darkened (light) just far enough to read as text, at 3:1 on the
// view, sidebar, hover and header surfaces and 2.5:1 on a selected row (TestPaletteContrast).
// Credited in ATTRIBUTION.md.
const solCyan = 0x2aa198

var darkPalette = palette{
	fyne: map[fyne.ThemeColorName]color.Color{
		theme.ColorNameBackground:          hex(0x002b36), // editor.background
		theme.ColorNameButton:              hex(0x004052), // tab.inactiveBackground
		theme.ColorNameDisabledButton:      hex(0x00212b),
		theme.ColorNameDisabled:            hex(0x586e75), // base01
		theme.ColorNameError:               hex(0xe66d6b),
		theme.ColorNameFocus:               alpha(hex(solCyan), 0x99), // focusBorder
		theme.ColorNameForeground:          hex(0x839496),             // editor.foreground
		theme.ColorNameForegroundOnError:   hex(0x002b36),
		theme.ColorNameForegroundOnPrimary: hex(0x002b36),
		theme.ColorNameForegroundOnSuccess: hex(0x002b36),
		theme.ColorNameForegroundOnWarning: hex(0x002b36),
		theme.ColorNameHeaderBackground:    hex(0x003847), // activityBar.background
		theme.ColorNameHover:               alpha(hex(0xffffff), 0x10),
		theme.ColorNameHyperlink:           hex(solCyan),
		theme.ColorNameInputBackground:     hex(0x003847), // input.background
		theme.ColorNameInputBorder:         hex(0x005a6f),
		theme.ColorNameMenuBackground:      hex(0x00212b), // dropdown.background
		theme.ColorNameOverlayBackground:   hex(0x00212b), // editorWidget.background
		theme.ColorNamePlaceHolder:         hex(0x718588),
		theme.ColorNamePressed:             alpha(hex(0xffffff), 0x1c),
		theme.ColorNamePrimary:             hex(solCyan), // button.background
		theme.ColorNameScrollBar:           alpha(hex(0xffffff), 0x40),
		theme.ColorNameScrollBarBackground: alpha(hex(0x000000), 0x00),
		theme.ColorNameSelection:           hex(0x274642), // editor.selectionBackground
		theme.ColorNameSeparator:           hex(0x004052),
		theme.ColorNameShadow:              alpha(hex(0x000000), 0x66),
		theme.ColorNameSuccess:             hex(0x899c08),
		theme.ColorNameWarning:             hex(0xd97a53),

		colorTopBar:           hex(0x002c39), // titleBar.activeBackground
		colorStatusBar:        hex(0x00212b), // statusBar.background
		colorSidebar:          hex(0x00212b), // sideBar.background
		colorRowHover:         hex(0x003a4a), // list.hoverBackground #004454aa over the background
		colorRowSelected:      hex(0x005a6f), // list.activeSelectionBackground
		colorContainerRunning: hex(0x3dd68c), // the earlier Running green (owner, 2026-10-12)
		colorRowSelectedText:  hex(0xeee8d5), // base2: the foreground is too faint on the selection
		colorDrawer:           hex(0x00212b),
		colorDrawerHeader:     hex(0x003847),
		colorDock:             hex(0x004052), // tab.inactiveBackground, the strip behind the tabs
		colorDockTab:          hex(0x002b37), // tab.activeBackground
		colorDockTabText:      hex(0xd6dbdb), // tab.activeForeground
		colorDockTabDim:       hex(0x93a1a1), // base1
		colorMuted:            hex(0x718588),
		colorAccent:           hex(solCyan),
		colorChartSecond:      hex(0x898dd0), // violet #6c71c4, lightened to 4:1 on the plot
		colorChip:             hex(0x004052),
		colorToast:            hex(0x0b4556), // lighter than the window and the drawer, so a toast stands out
		colorToastText:        hex(0xeee8d5), // base2
		colorToastSelection:   hex(0x1d6870), // 1.6:1 against the toast, 5.2:1 under its text
		colorLink:             hex(solCyan),
		colorTooltip:          hex(0x003847), // editorHoverWidget.background #004052 is 3.6:1 under the text
		colorCode:             hex(0xd9a73a), // yellow #b58900 lightened: 5.1:1 on the chip
	},
	status: map[model.Status]color.Color{
		model.StatusOK:          hex(0x899c08), // green #859900
		model.StatusSucceeded:   hex(0x4099d7), // blue #268bd2
		model.StatusPending:     hex(0xb78d08), // yellow #b58900
		model.StatusWarning:     hex(0xd97a53), // orange #cb4b16
		model.StatusError:       hex(0xe66d6b), // red #dc322f
		model.StatusTerminating: hex(0x898dd0), // violet #6c71c4
		model.StatusUnknown:     hex(0x84959c), // base00 #657b83
		model.StatusStopped:     hex(0x93a1a1), // base1: a stopped container's square, legible on a selected row too
		model.StatusEphemeral:   hex(0x4099d7), // blue #268bd2, as Succeeded: an ephemeral container's square (D-102)
	},
}

var lightPalette = palette{
	fyne: map[fyne.ThemeColorName]color.Color{
		theme.ColorNameBackground:          hex(0xfdf6e3), // editor.background
		theme.ColorNameButton:              hex(0xddd6c1), // activityBar.background
		theme.ColorNameDisabledButton:      hex(0xeee8d5),
		theme.ColorNameDisabled:            hex(0x93a1a1), // base1
		theme.ColorNameError:               hex(0xdc322f),
		theme.ColorNameFocus:               alpha(hex(0x1f7f78), 0x99),
		theme.ColorNameForeground:          hex(0x586e75), // sideBarTitle.foreground; editor.foreground #657b83 is 4.1:1 on the background
		theme.ColorNameForegroundOnError:   hex(0xfdf6e3),
		theme.ColorNameForegroundOnPrimary: hex(0xfdf6e3),
		theme.ColorNameForegroundOnSuccess: hex(0xfdf6e3),
		theme.ColorNameForegroundOnWarning: hex(0xfdf6e3),
		theme.ColorNameHeaderBackground:    hex(0xddd6c1), // activityBar.background
		theme.ColorNameHover:               alpha(hex(0x000000), 0x0c),
		theme.ColorNameHyperlink:           hex(0x1b6761),
		theme.ColorNameInputBackground:     hex(0xddd6c1), // input.background
		theme.ColorNameInputBorder:         hex(0xd3af86), // dropdown.border
		theme.ColorNameMenuBackground:      hex(0xeee8d5), // dropdown.background
		theme.ColorNameOverlayBackground:   hex(0xeee8d5), // editorWidget.background
		theme.ColorNamePlaceHolder:         hex(0x657b83),
		theme.ColorNamePressed:             alpha(hex(0x000000), 0x17),
		theme.ColorNamePrimary:             hex(0x1f7f78), // cyan, darkened to carry the base3 text
		theme.ColorNameScrollBar:           alpha(hex(0x000000), 0x40),
		theme.ColorNameScrollBarBackground: alpha(hex(0x000000), 0x00),
		theme.ColorNameSelection:           hex(0xdfca88), // list.activeSelectionBackground
		theme.ColorNameSeparator:           hex(0xddd6c1),
		theme.ColorNameShadow:              alpha(hex(0x000000), 0x2e),
		theme.ColorNameSuccess:             hex(0x6e7f00),
		theme.ColorNameWarning:             hex(0xcb4b16),

		colorTopBar:           hex(0xeee8d5), // titleBar.activeBackground
		colorStatusBar:        hex(0xeee8d5), // statusBar.background
		colorSidebar:          hex(0xeee8d5), // sideBar.background
		colorRowHover:         hex(0xf5eacb), // list.hoverBackground #dfca8844 over the background
		colorRowSelected:      hex(0xdfca88), // list.activeSelectionBackground
		colorContainerRunning: hex(0x177f4a), // the earlier Running green #1f9d5c, darkened to read on a selected row: 3.1:1 on the selection, 4.1:1 on the navigator (owner, 2026-10-16; #1a8f53 before, 2.5:1 on the selection)
		colorRowSelectedText:  hex(0x073642), // base02
		colorDrawer:           hex(0xeee8d5),
		colorDrawerHeader:     hex(0xddd6c1),
		colorDock:             hex(0xddd6c1), // the strip behind the tabs (tab.inactiveBackground #d3cbb7 is 3.3:1 under its text)
		colorDockTab:          hex(0xfdf6e3), // tab.activeBackground
		colorDockTabText:      hex(0x586e75),
		colorDockTabDim:       hex(0x586e75), // tab.inactiveForeground
		colorMuted:            hex(0x657b83),
		colorAccent:           hex(0x1f7f78),
		colorChartSecond:      hex(0x5a5fb5), // violet #6c71c4, darkened to 3.9:1 on the plot
		colorChip:             hex(0xddd6c1),
		colorToast:            hex(0xffffff), // lighter than the window, with the level's border and bar
		colorToastText:        hex(0x073642), // base02
		colorToastSelection:   hex(0xdfca88), // the selection, list.activeSelectionBackground
		colorLink:             hex(0x1b6761),
		colorTooltip:          hex(0xeee8d5), // editorHoverWidget.background #ccc4b0 is 3.1:1 under the text
		colorCode:             hex(0x8f4a0a), // orange #cb4b16 darkened: 4.6:1 on the chip
	},
	status: map[model.Status]color.Color{
		model.StatusOK:          hex(0x6e7f00), // green #859900
		model.StatusSucceeded:   hex(0x237ebf), // blue #268bd2
		model.StatusPending:     hex(0x987300), // yellow #b58900
		model.StatusWarning:     hex(0xcb4b16), // orange
		model.StatusError:       hex(0xdc322f), // red
		model.StatusTerminating: hex(0x6c71c4), // violet
		model.StatusUnknown:     hex(0x657b83), // base00
		model.StatusStopped:     hex(0x586e75), // base01: a stopped container's square, legible on a selected row too
		model.StatusEphemeral:   hex(0x237ebf), // blue #268bd2, as Succeeded: an ephemeral container's square (D-102)
	},
}

// identityColorName is the token of a cluster's icon colour (Cluster settings › General › Pick
// Icon Color): one per accent of the palette, engine.IconColors.
func identityColorName(color string) fyne.ThemeColorName {
	return fyne.ThemeColorName("cogpit.identity." + color)
}

// identityColors are the icon colours per variant: Solarized's eight accents in the shades the
// status tokens use, so that a cluster's initials read at 3:1 on the hotbar, the navigator and the
// welcome page's list (TestIdentityColorsContrast); cyan is each variant's accent.
var identityColors = map[fyne.ThemeVariant]map[string]uint32{
	theme.VariantDark: {"cyan": solCyan, "blue": 0x4099d7, "violet": 0x898dd0, "magenta": 0xe0619c,
		"red": 0xe66d6b, "orange": 0xd97a53, "yellow": 0xb78d08, "green": 0x899c08},
	theme.VariantLight: {"cyan": 0x1f7f78, "blue": 0x237ebf, "violet": 0x6c71c4, "magenta": 0xc4307a,
		"red": 0xdc322f, "orange": 0xcb4b16, "yellow": 0x987300, "green": 0x6e7f00},
}

// init files each palette's status and identity colours under their theme names, so that Color
// is one lookup.
func init() {
	for _, p := range []*palette{&darkPalette, &lightPalette} {
		for s, name := range statusColorNames {
			p.fyne[name] = p.status[s]
		}
	}
	for v, colors := range identityColors {
		for name, rgb := range colors {
			paletteFor(v).fyne[identityColorName(name)] = hex(rgb)
		}
	}
}

var statusColorNames = map[model.Status]fyne.ThemeColorName{
	model.StatusOK:          colorStatusOK,
	model.StatusSucceeded:   colorStatusSucceeded,
	model.StatusPending:     colorStatusPending,
	model.StatusWarning:     colorStatusWarning,
	model.StatusError:       colorStatusError,
	model.StatusTerminating: colorStatusTerminating,
	model.StatusUnknown:     colorStatusUnknown,
	model.StatusStopped:     colorStatusStopped,
	model.StatusEphemeral:   colorStatusEphemeral,
}

// statusColorName returns the theme colour name of a status token; StatusNone and unknown tokens
// map to the foreground, so that an unexpected token never hides text.
func statusColorName(s model.Status) fyne.ThemeColorName {
	if n, ok := statusColorNames[s]; ok {
		return n
	}
	return theme.ColorNameForeground
}

// StatusColor returns the colour of a status token in the given variant: ok green, succeeded teal,
// pending amber, warning orange, error red, terminating violet, unknown grey. StatusNone returns
// the variant's foreground colour.
func StatusColor(s model.Status, v fyne.ThemeVariant) color.Color {
	p := paletteFor(v)
	if c, ok := p.status[s]; ok {
		return c
	}
	return p.fyne[theme.ColorNameForeground]
}

func paletteFor(v fyne.ThemeVariant) *palette {
	if v == theme.VariantLight {
		return &lightPalette
	}
	return &darkPalette
}

// cogTheme is Cogpit's fyne.Theme: its own palettes and a denser size set on top of Fyne's fonts
// and icons.
type cogTheme struct {
	mode ThemeMode
	base fyne.Theme
	// rowHeight is the Preferences' table row height in points (0: the default); rowPadding is
	// the padding above and below a row's text that gives it.
	rowHeight  int
	rowPadding float32
}

var _ fyne.Theme = (*cogTheme)(nil)

// newTheme returns the theme for a mode; the zero mode is dark.
func newTheme(mode ThemeMode) *cogTheme {
	if mode == "" {
		mode = ThemeDark
	}
	return &cogTheme{mode: mode, base: theme.DefaultTheme()}
}

// newThemeFor returns the theme for a mode with the Preferences' table row height (0: the
// default). It measures text, so an app must exist.
func newThemeFor(mode ThemeMode, rowHeight int) *cogTheme {
	t := newTheme(mode)
	if rowHeight > 0 {
		t.rowHeight = rowHeight
		t.rowPadding = max(1, (float32(rowHeight)-rowTextHeight())/2)
	}
	return t
}

// defaultRowPadding is the padding above and below a table row's text without a preference.
const defaultRowPadding = 6

// rowTextHeight is the height of a table row's text, the views' text size.
func rowTextHeight() float32 {
	return fyne.MeasureText("Mg", textSize, fyne.TextStyle{}).Height
}

// defaultRowHeight is a table row's height without a preference, rounded to whole points.
func defaultRowHeight() int { return int(rowTextHeight() + 2*defaultRowPadding + 0.5) }

// textSize is the views' text size, a step smaller than Fyne's default (Size).
const textSize = 13

// variant resolves the mode against the variant Fyne asks for: only ThemeSystem follows it.
func (t *cogTheme) variant(v fyne.ThemeVariant) fyne.ThemeVariant {
	switch t.mode {
	case ThemeLight:
		return theme.VariantLight
	case ThemeSystem:
		return v
	}
	return theme.VariantDark
}

// Color implements fyne.Theme.
func (t *cogTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	v = t.variant(v)
	if c, ok := paletteFor(v).fyne[n]; ok {
		return c
	}
	if c, ok := customIdentityColor(n, v); ok { // a freely picked cluster colour (A93, identitycolor.go)
		return c
	}
	return t.base.Color(n, v)
}

// Font implements fyne.Theme.
func (t *cogTheme) Font(s fyne.TextStyle) fyne.Resource { return t.base.Font(s) }

// iconChevronDown is Cogpit's own open-branch chevron, matching Fyne's closed-branch chevron;
// Fyne's tree draws an arrow (MoveDown) there, which reads as "move", not "expanded".
var iconChevronDown = fyne.NewStaticResource("cogpit-chevron-down.svg", []byte(
	`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">`+
		`<path d="M7.41 8.59 12 13.17l4.59-4.58L18 10l-6 6-6-6z"/></svg>`))

// Icon implements fyne.Theme.
func (t *cogTheme) Icon(n fyne.ThemeIconName) fyne.Resource {
	if n == theme.IconNameMoveDown {
		return theme.NewThemedResource(iconChevronDown)
	}
	return t.base.Icon(n)
}

// Size implements fyne.Theme. Text is a step smaller than Fyne's default and paddings are tighter:
// a Kubernetes IDE shows many rows, and Lens is compact too.
func (t *cogTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameText:
		return textSize
	case theme.SizeNameCaptionText:
		return 11
	case theme.SizeNameSubHeadingText:
		return 16
	case theme.SizeNameHeadingText:
		return 20
	case theme.SizeNamePadding:
		return 4
	case theme.SizeNameInnerPadding:
		return 6
	case theme.SizeNameLineSpacing:
		return 3
	case theme.SizeNameInlineIcon:
		return 16
	case theme.SizeNameScrollBar:
		return 10
	case theme.SizeNameScrollBarSmall:
		return 3
	case theme.SizeNameInputRadius, theme.SizeNameButtonRadius:
		return 5
	case theme.SizeNameSelectionRadius:
		return 4
	case theme.SizeNameDialogRadius:
		return 10
	case theme.SizeNameSplitThickness:
		return 4

	case sizeSidebarWidth:
		return 216
	case sizeHotbarWidth:
		return 64
	case sizeHotbarTile:
		return 38
	case sizeNavBadge:
		return 28
	case sizeBarHeight:
		return 40
	case sizeStatusHeight:
		// VS Code's status bar as the owner sees it beside Cogpit (2026-10-14: 26 was too flat).
		return 30
	case sizeStatusText:
		// The text size of the views, as VS Code's status bar reads (owner, 2026-10-12: the
		// caption size, 11, was too small).
		return 13
	case sizeCellPadding:
		return 10
	case sizeRowPadding:
		if t.rowPadding > 0 {
			return t.rowPadding
		}
		return defaultRowPadding
	case sizeDrawerLabel:
		return 128
	case sizeChipRadius:
		return 9
	case sizeSortIcon:
		return 14
	case sizeDotDiameter:
		return 8
	case sizeToastWidth:
		return 400
	case sizeFilterWidth:
		return 190
	case sizeSearchWidth:
		return 240
	case sizeFindCountWidth:
		return 80
	case sizePickerWidth:
		return 600
	case sizePickerHeight:
		return 460
	case sizeConfirmWidth:
		return 500
	case sizePopupMaxList:
		return 320
	case sizeDockTabMaxLen:
		return 220
	case sizeMenuColumn:
		return 30
	case sizeCellIcon:
		return 14
	case sizeTooltipWidth:
		return 440
	case sizeTooltipOffset:
		return 14
	case sizeDialogWidth:
		return 420
	case sizeAboutWidth:
		return 300
	case sizeHealthBar:
		return 6
	case sizeLogo:
		return 20
	}
	return t.base.Size(n)
}
