package fyneui

import (
	"fmt"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A failed connect's error page (D-114, the owner's AW17 of 2026-10-22, his choice (b)). After the
// shown cluster's connect failed (the restore's or a click's) the workspace shows this page,
// whether or not a tab is open: the welcome page shows only while no cluster is chosen. Before, the
// welcome page took the workspace and said the raw error chain in its status line (the restore's
// walk-cert-gone), and D-97's placeholder with Connect Again never showed.
//
// The page is the welcome page's look as the owner asked ("wie die Welcome seite mit oben dem Cogpit
// Icon, dem Hintergrund der Welcomeseite"): the logo on its glow at the top, the horizon behind; then
// plainly that it is an error page; the error's cause in a sentence, under a coral bracket naming the
// stage it failed in; the connect stage by stage, each with its result ("dem gescheiterten
// Verbindungsaufbau als Ablauf"); Connect Again (the welcome page's connect of the cluster, a
// credential plugin's review included) and Show Connection Updates (its connection log, the whole
// error chain line by line). Every text wraps to the page's column, inside the workspace's scroll,
// so that nothing draws over the navigator (AW18).
//
// What it shows is engine state (UIState.Connection and ConnectFailure, engine/connectfailure.go):
// agents read the same in ui_state.

// errorPageTagline says plainly that the page is an error page.
const errorPageTagline = "This is an error page: Cogpit could not connect to the cluster you chose. " +
	"Below is why, and every stage of the connect with its result."

// errorPage is the failed connect's page. Main goroutine.
type errorPage struct {
	sh   *shell
	root *fyne.Container // over the workspace and the dock while shown

	title   *textLine
	head    *bracketLabel // "error · <the stage it failed in>"
	cause   *paragraph
	steps   *bracketLabel // "the connect · n stages"
	plate   fyne.CanvasObject
	list    *fyne.Container // the stages' rows
	retry   *textButton
	connLog *textButton
	folders *textButton // Program Folders…: shown when the login did not find a program (D-120)
	when    *paragraph

	shown bool
	built string      // the signature the page was built for
	rows  []*stageRow // the stages' rows, for tests
}

func newErrorPage(sh *shell) *errorPage {
	p := &errorPage{sh: sh}
	p.title = newTextLine("", theme.ColorNameForeground, theme.SizeNameHeadingText, fyne.TextStyle{Bold: true})
	p.title.align = fyne.TextAlignCenter
	tagline := newCenteredText(errorPageTagline, colorMuted, theme.SizeNameText, welcomeColumn-120)
	head := container.NewVBox(container.NewCenter(newLogoGlow(welcomeLogoSize)), p.title, newSpace(theme.SizeNamePadding),
		container.NewCenter(tagline))
	p.head = newBracketLabel("error", colorStatusError)
	p.cause = newParagraph("", colorStatusError, welcomeColumn)
	p.steps = newBracketLabel("the connect", colorAccent)
	p.list = container.NewVBox()
	p.plate = plateOf(p.list)
	p.retry = newPlateButton(connectAgainLabel, theme.LoginIcon(), func() { sh.tabs.welcome.connect(sh.ui.Context) })
	p.retry.Importance = widget.HighImportance
	p.connLog = newPlateButton(strings.TrimSuffix(connectLogItem, "…"), theme.ListIcon(), func() { sh.kc.showConnectLog(sh.ui.Context) })
	// A credential plugin's program not found: where to add its folder (preferences_programs.go).
	p.folders = newPlateButton(programFoldersButton, theme.FolderOpenIcon(), func() {
		sh.dispatch("preferences.open", preferencesOpenParams{Section: engine.SectionKubernetes}, nil)
	})
	p.folders.Hide()
	p.when = newParagraph("", colorMuted, welcomeColumn)
	p.when.size = theme.SizeNameCaptionText
	column := container.NewVBox(
		head,
		newSpace(theme.SizeNameInnerPadding),
		p.head,
		p.cause,
		newSpace(theme.SizeNameInnerPadding),
		p.steps,
		p.plate,
		newSpace(theme.SizeNamePadding),
		container.NewHBox(p.retry, p.connLog, p.folders),
		p.when,
	)
	p.root = newWelcomeStage(box(welcomeLayout{}, column))
	p.root.Hide()
	return p
}

// programFoldersButton opens Preferences › Kubernetes, where the Program folders setting is.
const programFoldersButton = "Program Folders…"

// errorPageShown reports whether the workspace shows the error page: the chosen cluster's connect
// failed and the engine shows no welcome page (engine/welcome.go).
func errorPageShown(ui engine.UIState) bool {
	return ui.Connection.State == engine.ConnectError && ui.Context != "" && !ui.Welcome
}

// sync follows the UI state: shows or hides the page, and draws what it says. The tab bar calls it
// whenever the tabs, the connection or a cluster's identity change.
func (p *errorPage) sync() {
	show := errorPageShown(p.sh.ui)
	if show != p.shown {
		p.shown = show
		if show {
			p.sh.tip.hide()
			p.sh.win.Canvas().Unfocus() // no key may reach a table or a field the page hides
			p.root.Show()
		} else {
			p.root.Hide()
			p.built = ""
		}
	}
	if show {
		p.render()
	}
}

// render draws the page for the state: rebuilt only when what it says changed.
func (p *errorPage) render() {
	ui := p.sh.ui
	name := p.sh.identity(ui.Context).name
	var stages []engine.ConnectStage
	at := ""
	if f := ui.ConnectFailure; f != nil && f.Context == ui.Context {
		stages, at = f.Stages, p.sh.stagesAt(f.At)
	}
	sig := fmt.Sprintf("%s|%s|%s|%s|%v", ui.Context, name, ui.Connection.Error, at, stages)
	if sig == p.built {
		return
	}
	p.built = sig
	defer welcomeStageOf(p.root).relayout() // the scrim over the grid floor ends under the last line

	p.title.SetText("Could not connect to " + name)
	where := "error"
	p.folders.Hide()
	if st := failedStage(stages); st != nil {
		where = "error · " + strings.ToLower(stageLabel(st.Name))
		if st.MissingProgram != "" {
			p.folders.Show()
		}
	}
	p.head.SetText(where)
	cause := ui.Connection.Error
	if cause == "" {
		cause = "The connect failed without a reason."
	}
	p.cause.text = capitalize(cause)
	p.cause.Refresh()

	th := p.sh.app.Settings().Theme()
	width := float32(welcomeColumn) - 2*th.Size(theme.SizeNameInnerPadding)
	p.rows = p.rows[:0]
	objs := stageRows(stages, width)
	for i, o := range objs {
		p.rows = append(p.rows, o.(*stageRow))
		if i > 0 {
			objs[i] = container.NewVBox(newSpace(theme.SizeNamePadding), o)
		}
	}
	p.list.Objects = objs
	p.list.Refresh()
	if len(stages) == 0 {
		p.steps.Hide()
		p.plate.Hide()
	} else {
		p.steps.SetText(fmt.Sprintf("the connect · %d stages", len(stages)))
		p.steps.Show()
		p.plate.Show()
	}
	when := "The whole error, line by line, is in Connection Updates."
	if at != "" {
		when = "Tried " + at + ". " + when
	}
	p.when.text = when
	p.when.Refresh()
}

// connectFailureChanged reports whether a failed connect's page says something else.
func connectFailureChanged(a, b *engine.ConnectFailure) bool {
	if a == nil || b == nil {
		return a != b
	}
	return a.Context != b.Context || !a.At.Equal(b.At) || !slices.Equal(a.Stages, b.Stages)
}

// capitalize puts the first letter of a sentence in upper case.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}
