package fyneui

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// The credential plugin approval dialog (trust-boundaries H-1, D-28): one dialog per kubeconfig file
// and identical plugin. It lists every context of the file that uses the plugin and is not allowed
// yet, each with the server its credential goes to and how that server is reached and trusted, and
// one click allows them all. The engine stores one approval per context, each with its own
// fingerprint, and checks every one against the file as it is now. A context that waits only because
// its cluster skips the certificate check (kind insecure, A28) gets the same dialog for itself
// alone, without a program: the question is whether to connect without the check.

// Result types of kubeconfig.pendingApprovals and the parameters of kubeconfig.approveExec,
// mirroring the engine's JSON.
type (
	// approvalContextView is engine.ExecApprovalContext.
	approvalContextView struct {
		Context     string               `json:"context"`
		KubeContext string               `json:"kubeContext"`
		State       string               `json:"state"`
		Server      string               `json:"server"`
		Fingerprint string               `json:"fingerprint"`
		TLS         kubeconfig.TLSView   `json:"tls"`
		Files       kubeconfig.FileFacts `json:"files"`
	}
	// approvalGroupView is engine.ExecApprovalGroup.
	approvalGroupView struct {
		Source string `json:"source"`
		File   string `json:"file"`
		Kind   string `json:"kind"`
		Plugin struct {
			APIVersion string               `json:"apiVersion"`
			Command    string               `json:"command"`
			Args       []string             `json:"args"`
			EnvNames   []string             `json:"envNames"`
			TokenFile  string               `json:"tokenFile"`
			Files      kubeconfig.FileFacts `json:"files"`
		} `json:"plugin"`
		Found        string                `json:"found"`
		RiskyEnv     []string              `json:"riskyEnv"`
		ArgsWithheld int                   `json:"argsWithheld"`
		Contexts     []approvalContextView `json:"contexts"`
	}
	pendingView struct {
		Groups []approvalGroupView `json:"groups"`
	}
	approvalItem struct {
		Context     string `json:"context"`
		Fingerprint string `json:"fingerprint"`
	}
	approveParams struct {
		Approvals []approvalItem `json:"approvals"`
	}
)

// reviewPlugin shows the credential plugin of a source context, with the other contexts of its file
// that use the same one, and asks whether they may run. done gets true once the user allowed them
// and the engine recorded that (or nothing was left to allow); false when the user cancelled or the
// plugin could not be shown.
func (k *kubeconfigUI) reviewPlugin(contextID string, done func(allowed bool)) {
	k.sh.dispatch("kubeconfig.pendingApprovals", map[string]string{contextParam: contextID}, func(res any, err error) {
		if err != nil {
			k.sh.showError(err)
			done(false)
			return
		}
		var out pendingView
		if err := decode(res, &out); err != nil {
			k.sh.toast("error", "Unexpected credential plugin description: "+err.Error())
			done(false)
			return
		}
		if len(out.Groups) == 0 {
			done(true) // allowed meanwhile
			return
		}
		k.showApproval(out.Groups[0], done)
	})
}

// approvalDialog shows the credential plugin of a group of contexts before its first run.
type approvalDialog struct {
	d      *dialog.CustomDialog
	allow  *textButton
	cancel *textButton
	group  approvalGroupView
	// scroll holds the whole content when it is taller than the window allows (many contexts); nil
	// otherwise.
	scroll *container.Scroll
}

// The dialog scrolls as a whole once it would be taller than this share of the window less the
// title and the buttons.
const (
	approvalMaxShare = 0.9
	approvalChrome   = 130
)

// showApproval builds and shows the approval dialog for g.
func (k *kubeconfigUI) showApproval(g approvalGroupView, done func(bool)) {
	if k.approval != nil {
		k.approval.d.Hide()
	}
	th := k.sh.app.Settings().Theme()
	pad := th.Size(theme.SizeNamePadding)
	width := th.Size(sizePickerWidth)
	valueW := width - th.Size(sizeDrawerLabel) - 2*pad
	show := func(s string) string { return kubeconfig.DisplayText(s, maxShownPath) }
	muted := func(s string) fyne.CanvasObject {
		return newTextLine(s, colorMuted, theme.SizeNameText, fyne.TextStyle{})
	}
	mono := func(s string) fyne.CanvasObject {
		l := newTextLine(s, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Monospace: true})
		l.truncate = true
		return l
	}
	row := func(rows *fyne.Container, name string, value fyne.CanvasObject) {
		rows.Objects = append(rows.Objects, newTextLine(name, colorMuted, theme.SizeNameText, fyne.TextStyle{}), value)
	}

	// What the contexts share: the plugin and the file.
	shared := box(kvLayout{})
	pl := g.Plugin
	if g.Kind == kubeconfig.LoginProgram || g.Kind == kubeconfig.LoginBoth {
		row(shared, "Program", mono(show(pl.Command)))
		if t := programFileText(g); t != "" {
			row(shared, "", newParagraph(t, theme.ColorNameForeground, valueW))
		}
		switch {
		case g.Found == "":
			row(shared, "Found", newParagraph("not found on Cogpit's PATH now: connecting would fail", colorStatusWarning, valueW))
		case g.Found != pl.Command:
			row(shared, "Found at", mono(show(g.Found)))
		}
		args := container.NewVBox()
		for i, a := range pl.Args {
			if i == maxShownArgs {
				args.Add(newTextLine(fmt.Sprintf("… and %d more", len(pl.Args)-maxShownArgs), colorMuted, theme.SizeNameText, fyne.TextStyle{}))
				break
			}
			args.Add(mono(kubeconfig.DisplayText(a, maxShownArg)))
		}
		if len(pl.Args) == 0 {
			args.Add(muted("none"))
		}
		row(shared, "Arguments", args)
		env := "none"
		if len(pl.EnvNames) > 0 {
			env = joinNames(pl.EnvNames) + " (values not shown)"
		}
		row(shared, "Environment", newParagraph(env, theme.ColorNameForeground, valueW))
		if len(g.RiskyEnv) > 0 {
			row(shared, "", newParagraph("Sets "+strings.Join(g.RiskyEnv, ", ")+", which decides which programs run or what they load.",
				colorStatusWarning, valueW))
		}
	}
	if g.Kind == kubeconfig.LoginTokenFile || g.Kind == kubeconfig.LoginBoth {
		row(shared, "Token file", mono(show(pl.TokenFile)))
		if pl.Files.TokenFileResolved != "" {
			row(shared, "", newParagraph("a link in the kubeconfig's folder, pointing to "+show(pl.Files.TokenFileResolved), colorMuted, valueW))
		}
	}
	row(shared, "Kubeconfig", newParagraph(show(g.File), theme.ColorNameForeground, valueW))

	content := container.NewVBox(newParagraph(approvalLead(g), theme.ColorNameForeground, width), newSpace(theme.SizeNamePadding))
	if n := changedCount(g); n > 0 {
		content.Add(newParagraph(approvalChangedNote(g, n), colorStatusWarning, width))
		content.Add(newSpace(theme.SizeNamePadding))
	}
	content.Add(shared)
	content.Add(newSpace(theme.SizeNamePadding))

	// What is specific to each context: the server its credential goes to and how that server is
	// reached and trusted. An insecure, proxied or own-CA connection decides who can read the
	// credential: said first, in the warning colour, not as one more row (R-11).
	innerW := width - 8*pad
	valueInner := innerW - th.Size(sizeDrawerLabel) - 2*pad
	blocks := container.NewVBox()
	for i, c := range g.Contexts {
		if i > 0 {
			blocks.Add(newSpace(theme.SizeNameInnerPadding))
		}
		head := container.NewHBox(newTextLine(show(c.KubeContext), theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true}))
		if c.State == engine.ExecChanged {
			head.Add(newPill("changed", colorStatusWarning, true))
		}
		blocks.Add(head)
		facts := connectionFacts(c)
		for _, w := range facts.warnings {
			blocks.Add(newParagraph(w, colorStatusWarning, innerW))
		}
		rows := box(kvLayout{})
		row(rows, "Credential for", mono(show(c.Server)))
		for _, r := range facts.rows {
			row(rows, r[0], newParagraph(r[1], theme.ColorNameForeground, valueInner))
		}
		blocks.Add(rows)
	}
	listBg := newSurface(theme.ColorNameInputBackground)
	listBg.radius, listBg.stroke = theme.SizeNameInputRadius, colorHairline
	content.Add(muted(fmt.Sprintf("%d %s", len(g.Contexts), nounFor(len(g.Contexts), "context", "contexts"))))
	content.Add(container.NewStack(listBg, inset(theme.SizeNamePadding, theme.SizeNamePadding, blocks)))

	var body fyne.CanvasObject = content
	var scroll *container.Scroll
	maxH := k.sh.win.Canvas().Size().Height*approvalMaxShare - approvalChrome
	if maxH < th.Size(sizePickerHeight) {
		maxH = th.Size(sizePickerHeight)
	}
	if content.MinSize().Height > maxH {
		scroll = container.NewVScroll(content)
		scroll.SetMinSize(fyne.NewSize(width+4*pad, maxH)) // room for the scroll bar beside the text
		body = scroll
	}

	a := &approvalDialog{group: g, scroll: scroll}
	allowLabel := "Allow and Connect"
	if len(g.Contexts) > 1 {
		allowLabel = fmt.Sprintf("Allow All %d and Connect", len(g.Contexts))
	}
	a.allow = newPlateButton(allowLabel, theme.ConfirmIcon(), nil)
	a.allow.Importance = widget.WarningImportance
	a.cancel = newPlateButton("Cancel", nil, nil)
	a.d = dialog.NewCustomWithoutButtons(approvalTitle(g), body, k.sh.win)
	a.d.SetButtons([]fyne.CanvasObject{a.cancel, a.allow})
	answered := false
	a.d.SetOnClosed(func() {
		if k.approval == a {
			k.approval = nil
		}
		if !answered {
			answered = true
			done(false)
		}
	})
	a.cancel.OnTapped = a.d.Hide
	a.allow.OnTapped = func() {
		a.allow.Disable()
		items := make([]approvalItem, len(g.Contexts))
		for i, c := range g.Contexts {
			items[i] = approvalItem{Context: c.Context, Fingerprint: c.Fingerprint}
		}
		k.sh.dispatch("kubeconfig.approveExec", approveParams{Approvals: items}, func(_ any, err error) {
			if err != nil {
				// A changed context (the error names it) refuses all of them: the dialog closes, and
				// connecting again shows the file as it is now.
				k.sh.showError(err)
				a.d.Hide()
				return
			}
			answered = true
			a.d.Hide()
			done(true)
		})
	}
	// As the confirmation's: a click meant for something else, or a double click, must not allow.
	if delay := k.sh.confirmer.armDelay; delay > 0 {
		a.allow.Disable()
		time.AfterFunc(delay, func() { k.sh.do(a.allow.Enable) })
	}
	k.approval = a
	k.sh.showDialog(a.d) // no button has the focus: Enter must not allow a program by accident; Escape is Cancel
}
