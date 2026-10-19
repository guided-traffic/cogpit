package fyneui

import (
	"errors"
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/ui/fyneui/yamlview"
)

// editorPushDelay is the typing pause after which an editor tab's text goes to the engine, where
// editor_get shows it to agents.
const editorPushDelay = 400 * time.Millisecond

// Parameters of the editor commands, mirroring the engine's schemas.
type (
	editorTextParams struct {
		ID   string `json:"id"`
		YAML string `json:"yaml"`
	}
	editorDiffParams struct {
		ID      string `json:"id"`
		YAML    string `json:"yaml"`
		Context int    `json:"context"`
	}
	createParams struct {
		YAML string `json:"yaml"`
		// Context is the Create tab's cluster: it creates there, whichever cluster the window shows
		// (D-69).
		Context string `json:"context,omitempty"`
	}
	createTabParams struct {
		Template string `json:"template,omitempty"`
	}
)

// editorPane is the YAML editor of an Edit or Create resource tab: yamlview made editable, its
// text loaded from the engine (editor.get) and sent back after a typing pause (editor.setText),
// so that the engine, and agents through it, see what the user typed. A text an agent sets
// replaces the pane's unless the user has typed since the last send.
type editorPane struct {
	sh   *shell
	id   string
	view *yamlview.View

	pushed uint64 // the newest text revision the pane has sent or loaded
	dirty  bool   // typed since the last send
	timer  *time.Timer
	closed bool
	onEdit func() // the user typed (editortab_revert.go); nil: nothing to tell
}

func newEditorPane(sh *shell, id string) *editorPane {
	p := &editorPane{sh: sh, id: id, view: yamlview.New()}
	p.view.SetReadOnly(false)
	sh.applyEditorPrefs(p.view) // font size, line numbers, tab size (preferences.go)
	p.view.OnChanged = p.changed
	return p
}

// load fills the pane from the engine, then calls done with the tab's state.
func (p *editorPane) load(done func(engine.EditorState, error)) {
	p.sh.dispatch("editor.get", dockParams{ID: p.id}, func(res any, err error) {
		if p.closed {
			return
		}
		var st engine.EditorState
		if err == nil {
			err = decode(res, &st)
		}
		if err == nil {
			p.view.SetText(st.Text)
			p.pushed, p.dirty = st.Revision, false
		}
		done(st, err)
	})
}

func (p *editorPane) changed() {
	p.dirty = true
	if p.onEdit != nil {
		p.onEdit()
	}
	if p.timer != nil {
		p.timer.Stop()
	}
	p.timer = time.AfterFunc(editorPushDelay, func() { p.sh.do(p.push) })
}

// push sends the text to the engine if the user typed since the last send.
func (p *editorPane) push() {
	if p.closed || !p.dirty {
		return
	}
	p.dirty = false
	p.sh.dispatch("editor.setText", editorTextParams{ID: p.id, YAML: p.view.Text()}, func(res any, err error) {
		var tr struct {
			Revision uint64 `json:"revision"`
		}
		if err == nil && decode(res, &tr) == nil && tr.Revision > p.pushed {
			p.pushed = tr.Revision
		}
	})
}

// replace shows a text the engine now holds (an applied object as stored, a reverted one), keeping
// the scroll position and the caret.
func (p *editorPane) replace(text string, rev uint64) {
	p.view.UpdateText(text)
	if rev > p.pushed {
		p.pushed = rev
	}
	p.dirty = false
}

// remote follows an EventEditor for the tab: a newer text from elsewhere (an agent) replaces the
// pane's, unless the user has typed since the last send; the user's typing wins and is sent next.
func (p *editorPane) remote(rev uint64) {
	if p.closed || p.dirty || rev <= p.pushed {
		return
	}
	p.sh.dispatch("editor.get", dockParams{ID: p.id}, func(res any, err error) {
		var st engine.EditorState
		if p.closed || p.dirty || err != nil || decode(res, &st) != nil || st.Revision <= p.pushed {
			return
		}
		p.replace(st.Text, st.Revision)
	})
}

func (p *editorPane) close() {
	p.closed = true
	if p.timer != nil {
		p.timer.Stop()
	}
}

// editorTab is an Edit dock tab: the object's YAML to edit and Save, then a review of the
// changes against the object as the cache holds it, checked by a server-side dry run, and Apply.
// A conflict (the object changed since the edit's base version) brings the review back with the
// changes against the newer version: nothing is overwritten without a second Apply.
type editorTab struct {
	sh   *shell
	tab  engine.DockTab
	pane *editorPane
	diff *diffView

	body     *fyne.Container // the editor, or the review
	review   fyne.CanvasObject
	banner   *textLine
	status   *textLine
	editBtns *fyne.Container
	saveBtn  *widget.Button
	saveExit *widget.Button
	backBtn  *widget.Button
	applyBtn *widget.Button
	revBtns  *fyne.Container
	root     fyne.CanvasObject
	// Revert to original, in the editor's and the review's footer, and its undo
	// (editortab_revert.go).
	revertBtn, reviewRevert, undoBtn *widget.Button

	reviewing  bool
	reviewText string // the text under review
	closeAfter bool
	busy       bool
}

func newEditorTab(sh *shell, t engine.DockTab) *editorTab {
	e := &editorTab{sh: sh, tab: t, pane: newEditorPane(sh, t.ID), diff: newDiffView()}
	e.banner = newTextLine("", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})
	e.banner.truncate = true
	e.status = newTextLine("Loading…", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	e.status.truncate = true

	e.saveBtn = widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() { e.save(false) })
	e.saveBtn.Importance = widget.HighImportance
	e.saveExit = widget.NewButton("Save & Close", func() { e.save(true) })
	e.backBtn = widget.NewButtonWithIcon("Back to Editor", theme.NavigateBackIcon(), e.backToEditor)
	e.applyBtn = widget.NewButtonWithIcon("Apply", theme.ConfirmIcon(), e.apply)
	e.applyBtn.Importance = widget.HighImportance
	e.newRevertButtons()
	e.editBtns = container.NewHBox(e.undoBtn, e.revertBtn, e.saveExit, e.saveBtn)
	e.revBtns = container.NewHBox(e.reviewRevert, e.backBtn, e.applyBtn)
	e.revBtns.Hide()

	e.review = container.NewBorder(
		container.NewVBox(inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, e.banner), newDivider(false)),
		nil, nil, nil, e.diff.list)
	e.body = container.NewStack(e.pane.view)
	footer := container.NewStack(newSurface(colorDock), inset(theme.SizeNameInnerPadding, theme.SizeNamePadding,
		container.NewBorder(nil, nil, nil, container.NewStack(e.editBtns, e.revBtns), e.status)))
	e.root = container.NewBorder(nil, container.NewVBox(newDivider(false), footer), nil, nil,
		container.NewStack(newSurface(theme.ColorNameInputBackground), e.body))
	e.setBusy(true)
	e.pane.load(func(st engine.EditorState, err error) {
		e.setBusy(false)
		if err != nil {
			e.setStatus("Could not open the editor: "+err.Error(), colorStatusError)
			return
		}
		e.showBase(st)
	})
	return e
}

// showBase says which version the edit is based on, and whether the cluster has moved on.
func (e *editorTab) showBase(st engine.EditorState) {
	switch {
	case st.Missing:
		e.setStatus(e.tab.Title+" no longer exists on the cluster.", colorStatusWarning)
	case st.Stale:
		e.setStatus(e.tab.Title+" changed on the cluster since this text was loaded; Save compares with its current state.", colorStatusWarning)
	default:
		e.setStatus(e.tab.Title+" · loaded at "+e.sh.timeFormat().FormatTime(e.sh.now()), colorMuted)
	}
}

func (e *editorTab) setStatus(text string, c fyne.ThemeColorName) {
	e.status.SetText(text)
	e.status.SetColor(c)
}

func (e *editorTab) setBusy(b bool) {
	e.busy = b
	for _, btn := range []*widget.Button{e.saveBtn, e.saveExit, e.backBtn} {
		if b {
			btn.Disable()
		} else {
			btn.Enable()
		}
	}
	if b {
		e.applyBtn.Disable()
	}
}

// save starts the review: the diff against the object as the cache holds it, then the dry run.
func (e *editorTab) save(closeAfter bool) {
	if e.busy {
		return
	}
	e.closeAfter = closeAfter
	e.showUndo(false)
	text := e.pane.view.Text()
	e.pane.push()
	e.setBusy(true)
	e.setStatus("Comparing with the object in the cluster…", colorMuted)
	e.sh.dispatch("editor.diff", editorDiffParams{ID: e.tab.ID, YAML: text, Context: engine.DefaultDiffContext}, func(res any, err error) {
		e.setBusy(false)
		var d engine.EditDiff
		if err == nil {
			err = decode(res, &d)
		}
		if err != nil {
			e.setStatus(errText(err), colorStatusError)
			return
		}
		if !d.Changed {
			e.setStatus("No changes to save.", colorMuted)
			if closeAfter {
				e.sh.dispatch("dock.close", dockParams{ID: e.tab.ID}, nil)
			}
			return
		}
		banner := fmt.Sprintf("Changes to %s: %d added, %d removed", e.tab.Title, d.Added, d.Removed)
		bannerColor := theme.ColorNameForeground
		if d.Stale {
			banner = e.tab.Title + " changed on the cluster since this text was loaded: these changes are against its current state and revert the other change"
			bannerColor = colorStatusWarning
		}
		e.showReview(text, d.Lines, banner, bannerColor)
		e.dryRun(text)
	})
}

func (e *editorTab) showReview(text string, lines []engine.DiffLine, banner string, c fyne.ThemeColorName) {
	e.reviewing, e.reviewText = true, text
	e.diff.set(lines)
	e.banner.SetText(banner)
	e.banner.SetColor(c)
	e.body.Objects = []fyne.CanvasObject{e.review}
	e.body.Refresh()
	e.editBtns.Hide()
	e.revBtns.Show()
}

func (e *editorTab) backToEditor() {
	e.reviewing = false
	e.body.Objects = []fyne.CanvasObject{e.pane.view}
	e.body.Refresh()
	e.revBtns.Hide()
	e.editBtns.Show()
	e.setStatus(e.tab.Title, colorMuted)
}

// dryRun asks the server to validate the text without storing it; Apply is offered when it does.
func (e *editorTab) dryRun(text string) {
	e.setBusy(true)
	e.setStatus("Checking with the server (dry run)…", colorMuted)
	e.sh.dispatch("object.dryRun", editorTextParams{ID: e.tab.ID, YAML: text}, func(_ any, err error) {
		e.setBusy(false)
		if !e.reviewing || text != e.reviewText {
			return
		}
		var ce *engine.ConflictError
		switch {
		case errors.As(err, &ce):
			e.showConflict(text, ce)
		case err != nil:
			e.setStatus("The server refuses the change: "+errText(err), colorStatusError)
		default:
			e.setStatus("The server accepts the change (dry run). Apply writes it to the cluster.", colorStatusOK)
			e.applyBtn.Enable()
		}
	})
}

// showConflict shows the changes against the version the cluster holds now and asks again.
func (e *editorTab) showConflict(text string, ce *engine.ConflictError) {
	if ce.CurrentResourceVersion == "" {
		e.setStatus(e.tab.Title+" changed on the cluster and Cogpit has not seen the change yet; nothing was applied. Save again in a moment.", colorStatusWarning)
		return
	}
	what := "changed on the cluster"
	if ce.Recreated {
		what = "was deleted and created again"
	}
	e.showReview(text, ce.Diff,
		e.tab.Title+" "+what+" since this text was loaded: nothing was applied. These are your changes against its current state.",
		colorStatusWarning)
	if ce.Rebased {
		e.setStatus("Apply again to overwrite the cluster's change with your text.", colorStatusWarning)
	} else {
		// A dry run does not move the edit's base: Apply reports the conflict once more and then
		// bases the edit on the new version, so that overwriting it always takes an apply.
		e.setStatus("Apply to base your edit on the cluster's current state; applying after that overwrites it.", colorStatusWarning)
	}
	e.applyBtn.Enable()
}

// apply writes the reviewed text with the edit's base version as precondition.
func (e *editorTab) apply() {
	if e.busy || !e.reviewing {
		return
	}
	text := e.reviewText
	e.setBusy(true)
	e.setStatus("Applying…", colorMuted)
	e.sh.dispatch("object.apply", editorTextParams{ID: e.tab.ID, YAML: text}, func(res any, err error) {
		e.setBusy(false)
		var ce *engine.ConflictError
		switch {
		case errors.As(err, &ce):
			e.showConflict(text, ce)
			return
		case errors.Is(err, engine.ErrDeclined):
			e.setStatus("Not applied.", colorMuted)
			e.applyBtn.Enable()
			return
		case err != nil:
			e.setStatus("Not applied: "+errText(err), colorStatusError)
			e.applyBtn.Enable()
			return
		}
		var ar engine.ApplyResult
		if decode(res, &ar) == nil && ar.Text != "" && !e.pane.dirty {
			e.pane.replace(ar.Text, ar.Revision)
		}
		if e.closeAfter {
			e.sh.dispatch("dock.close", dockParams{ID: e.tab.ID}, nil)
			return
		}
		e.backToEditor()
		e.setStatus("Saved "+e.tab.Title+" at "+e.sh.timeFormat().FormatTime(time.Now()), colorStatusOK)
	})
}

func (e *editorTab) remote(rev uint64) { e.pane.remote(rev) }

func (e *editorTab) close() { e.pane.close() }

// errText is an error as one line for a status bar: a disabled command's reason without the
// command's name.
func errText(err error) string {
	var dis *engine.DisabledError
	if errors.As(err, &dis) {
		return dis.Reason
	}
	return err.Error()
}
