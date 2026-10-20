package fyneui

import (
	"context"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// guiConfirmer is the engine's Confirmer for the desktop app: a dialog whose title names the
// command and whose rows name the context, the namespace, the objects (kind and name apart,
// objectnames.go), what the command does beyond them (its effects, a row each) and who asked.
// The engine calls Confirm on its own goroutines; the dialog is shown on the main goroutine and
// Confirm waits for the answer.
//
// One dialog is shown at a time, in the order the requests came: a second request (an agent's,
// while the user is about to confirm their own) would otherwise open on top, with its confirm
// button where the user's pointer already is. For the same reason the confirm button arms only
// armDelay after the dialog appears.
type guiConfirmer struct {
	sh       *shell
	slot     chan struct{}  // holds a token while a dialog is shown
	armDelay time.Duration  // how long the confirm button stays disabled
	current  *confirmDialog // the dialog on screen; main goroutine only
	answers  dialogAnswers  // the user's answer in a command's own dialog (helmrollback.go)
}

// defaultArmDelay keeps a click meant for the previous dialog, or a double click, from confirming.
const defaultArmDelay = 750 * time.Millisecond

func newGUIConfirmer(sh *shell) *guiConfirmer {
	return &guiConfirmer{sh: sh, slot: make(chan struct{}, 1), armDelay: defaultArmDelay}
}

var _ engine.Confirmer = (*guiConfirmer)(nil)

// confirmReviewHeight caps the height of a confirmation's review text; a longer one scrolls.
const confirmReviewHeight = 240

// reviewBlock shows a confirmation's review text (an agent's Helm values): monospace, selectable,
// in a scrolling box no taller than confirmReviewHeight, so that a long text neither pushes the
// buttons off the window nor is cut off unseen.
func reviewBlock(text string, width float32) fyne.CanvasObject {
	l := widget.NewLabelWithStyle(text, fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
	l.Selectable = true
	scroll := container.NewScroll(l)
	scroll.SetMinSize(fyne.NewSize(width, min(float32(confirmReviewHeight), l.MinSize().Height)))
	return container.NewStack(newSurface(theme.ColorNameInputBackground), scroll)
}

// confirmDialog is one shown confirmation.
type confirmDialog struct {
	d       *dialogFrame
	confirm *widget.Button
	cancel  *widget.Button
}

// Confirm implements engine.Confirmer. It returns the user's answer, or ctx's error when ctx ends
// first; the dialog is then taken down. It must not be called on the main goroutine.
func (c *guiConfirmer) Confirm(ctx context.Context, req engine.ConfirmRequest) (bool, error) {
	if ok, handled, err := c.takeDialogAnswer(req); handled {
		return ok, err // the user answered in the command's own dialog (Rollback)
	}
	select {
	case c.slot <- struct{}{}:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	defer func() { <-c.slot }()
	answer := make(chan bool, 1)
	var once sync.Once
	reply := func(ok bool) { once.Do(func() { answer <- ok }) }
	var dlg *confirmDialog
	c.sh.do(func() { dlg = c.show(req, reply) })
	select {
	case ok := <-answer:
		return ok, nil
	case <-ctx.Done():
		c.sh.do(func() {
			if dlg != nil {
				dlg.d.Hide()
			}
		})
		return false, ctx.Err()
	}
}

// show builds and shows the dialog; reply receives the answer once.
func (c *guiConfirmer) show(req engine.ConfirmRequest, reply func(bool)) *confirmDialog {
	th := c.sh.app.Settings().Theme()
	width := th.Size(sizeConfirmWidth)
	destructive := req.Danger == engine.DangerDestructive

	// The rows say what happens, in the place order Context › Namespace › Objects (the owner's
	// rule, WF16): no sentence above them repeats them. req.Message() stays the engine's one-line
	// text of the same request (the tests read it; no agent or log does).
	valueWidth := width - th.Size(sizeDrawerLabel) - 2*th.Size(theme.SizeNamePadding)
	rows := box(kvLayout{})
	addRow := func(name, value string) {
		v := newParagraph(value, theme.ColorNameForeground, valueWidth)
		v.hard = true
		rows.Objects = append(rows.Objects, newTextLine(name, colorMuted, theme.SizeNameText, fyne.TextStyle{}), v)
	}
	objects := make([]namedObject, 0, len(req.Objects))
	for i, o := range req.Objects {
		kind := singular(c.sh.kindTitle(o.Kind))
		if i < len(req.Kinds) && req.Kinds[i] != "" { // the engine's name, as Message says it
			kind = req.Kinds[i]
		}
		objects = append(objects, namedObject{kind: kind, name: o.Name})
	}
	// Every object, kind and name apart, grouped by kind, a long list scrolling (WF32, objectnames.go).
	placeRows(rows, req.Context, req.Namespaces, objects, valueWidth)
	// What the objects alone don't say, a row each in the quick view the save review has (WF33):
	// the keys a save removes, a drain's options, a rollback's revision.
	effectRows(rows, req.Effects, valueWidth)
	if len(req.Effects) == 0 && req.Detail != "" { // a request that carries only the text: never hidden
		addRow("Effect", req.Detail)
	}
	addRow("Requested by", originText(req.Origin))

	content := container.NewVBox(rows)
	if req.Warning != "" { // what the user must know before answering: a chart nothing authenticates (H-41, A14)
		content.Add(newSpace(theme.SizeNamePadding))
		content.Add(newParagraph(req.Warning, colorStatusWarning, width))
	}
	if req.Review != "" { // what the command will apply, as the request carries it: read before answering
		content.Add(newSpace(theme.SizeNamePadding))
		content.Add(reviewBlock(req.Review, width))
	}
	if hint := c.sh.allowanceHint(req); hint != "" { // Cluster Settings › Agents (clustersettings_agents.go)
		content.Add(newSpace(theme.SizeNamePadding))
		content.Add(newParagraph(hint, colorMuted, width))
	}

	label := req.Label
	if label == "" {
		label = "Confirm"
	}
	cd := &confirmDialog{}
	cd.confirm = widget.NewButton(label, nil)
	cd.confirm.Importance = widget.HighImportance
	if destructive {
		cd.confirm.Importance = widget.DangerImportance
		// No icon: the word, the red fill and the rim say it (design-language.md), and a glyph in the
		// button would be a fourth way of saying the same.
	}
	cd.cancel = widget.NewButton("Cancel", nil)
	// The card says who asks and what is at stake (design-language.md): a request an agent made carries
	// a violet "asked by · agent" over the title; a destructive one a rim in the error colour, which
	// outranks the agent's violet (the eyebrow still names the agent). The rim is never the only signal:
	// the button is red and says the verb, and "This cannot be undone." stays in words.
	cd.d = newDialogFrame(c.sh, dialogTitle(label+"?"))
	cd.d.setContent(content)
	if destructive && !req.Undoable { // only where something is lost (A101): in the footer, with the buttons
		cd.d.setNote("This cannot be undone.", colorStatusError)
	}
	if req.Origin.IsAgent() {
		cd.d.setEyebrow("asked by · agent", colorAgent)
	}
	switch {
	case destructive:
		cd.d.setRim(colorStatusError)
	case req.Origin.IsAgent():
		cd.d.setRim(colorAgent)
	}
	cd.d.SetButtons([]fyne.CanvasObject{cd.cancel, cd.confirm})
	cd.d.SetOnClosed(func() {
		reply(false) // closed any other way than Confirm: Escape, ctx ended
		if c.current == cd {
			c.current = nil
		}
	})
	cd.confirm.OnTapped = func() { reply(true); cd.d.Hide() }
	if c.armDelay > 0 {
		cd.confirm.Disable()
		time.AfterFunc(c.armDelay, func() { c.sh.do(cd.confirm.Enable) })
	}
	cd.cancel.OnTapped = func() { reply(false); cd.d.Hide() }
	c.current = cd
	cd.d.Show() // no button has the focus: Enter must not confirm a destructive command by accident
	return cd
}

// originText says who asked for a command, for the confirmation's "Requested by" row.
func originText(o engine.Origin) string {
	switch o {
	case engine.OriginGUI:
		return "You, in this window"
	case engine.OriginCLI:
		return "cogpit-cli"
	case engine.OriginExternalAgent:
		return "An external agent over MCP"
	case engine.OriginBuiltInAgent:
		return "Cogpit's built-in agent"
	case engine.OriginDeepLink:
		return "A cogpit:// link from a web page, mail or app"
	}
	return string(o)
}
