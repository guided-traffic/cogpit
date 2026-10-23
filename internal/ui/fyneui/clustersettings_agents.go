package fyneui

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A cluster's Settings › Agents (D-2, H-14; engine/allowance.go): the allowance of agents'
// mutating commands in this cluster without asking, which only the user sets (the engine refuses
// every other origin). Lens has no such section; its place (last), wording and the confirmation
// that turning it on asks for are Cogpit's (inferred). Turning it on asks first, with the
// confirmation's armed button, because an agent may open this page for the user
// (cluster_settings_open is a UI command) and hope for a click; turning it off asks nothing.

const (
	agentWritesSetting = "agentWrites"
	agentsSectionLabel = "Agents"
	agentWritesTitle   = "Changes by agents"
	agentWritesCheck   = "Let agents change this cluster without asking"
	agentWritesHelp    = "Scale, cordon, apply and other changes by agents. Role bindings and destructive commands always ask."
	agentWritesNoteOff = "Not allowed: every change an agent asks for waits for you."
	agentWritesNoteOn  = "Allowed: agents change this cluster without asking. Their commands are in the audit log."
	agentWritesDemo    = "The demo cluster is generated in memory: no agent can change it."
)

func init() { clusterSectionLabels[engine.SectionAgents] = agentsSectionLabel }

// clusterAgentsSection builds a cluster's Settings › Agents.
func (p *settingsPage) clusterAgentsSection() fyne.CanvasObject {
	ctx := p.shown.Context
	note := newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	note.truncate = true
	p.controls["agentWritesNote"] = note
	check := widget.NewCheck(agentWritesCheck, func(on bool) {
		if p.syncing {
			return
		}
		if !on {
			p.setAgentWrites(ctx, false)
			return
		}
		p.confirmAgentWrites(ctx, func(ok bool) {
			if ok {
				p.setAgentWrites(ctx, true)
				return
			}
			p.syncControls() // declined: the check shows the setting again
		})
	})
	p.sync = func() {
		on := p.sh.ui.ClusterSettings[ctx].AgentWrites // the UI state's, so the check follows at once
		check.SetChecked(on)
		switch {
		case engine.IsDemoContext(ctx):
			check.Disable()
			note.SetText(agentWritesDemo)
		case on:
			check.Enable()
			note.SetText(agentWritesNoteOn)
		default:
			check.Enable()
			note.SetText(agentWritesNoteOff)
		}
	}
	return container.NewVBox(
		p.setting(agentWritesSetting, agentWritesTitle, agentWritesHelp, container.NewVBox(check, note)),
	)
}

// setAgentWrites dispatches the user's change of the allowance; a refusal shows under it.
func (p *settingsPage) setAgentWrites(ctx string, allowed bool) {
	p.sh.dispatch("cluster.settings.setAgentWrites", map[string]any{contextParam: ctx, "allowed": allowed},
		p.settled(agentWritesSetting))
}

// agentsDialog is the open confirmation of turning the allowance on, for tests.
type agentsDialog struct {
	d       *dialog.CustomDialog
	confirm *textButton
	cancel  *textButton
}

// confirmAgentWrites asks before agents' changes are allowed in ctx: a dialog naming the cluster,
// its Allow button armed after the confirmations' delay. done receives the answer once.
func (p *settingsPage) confirmAgentWrites(ctx string, done func(bool)) {
	sh := p.sh
	width := sh.app.Settings().Theme().Size(sizeConfirmWidth)
	name := sh.identity(ctx).name
	text := "Agents' mutating commands in " + name + " (context " + ctx + ") will run without asking you, until you " +
		"turn this off. Role bindings and destructive commands (delete, drain, evict, scale to zero, …) still ask."
	cd := &agentsDialog{confirm: newPlateButton("Allow", nil, nil), cancel: newPlateButton("Cancel", nil, nil)}
	cd.confirm.Importance = widget.HighImportance
	answered := false
	answer := func(ok bool) {
		if !answered {
			answered = true
			p.agentsDialog = nil
			done(ok)
		}
	}
	cd.d = dialog.NewCustomWithoutButtons("Let agents change "+name+"?", newParagraph(text, theme.ColorNameForeground, width), sh.win)
	cd.d.SetButtons([]fyne.CanvasObject{cd.cancel, cd.confirm})
	cd.d.SetOnClosed(func() { answer(false) })
	cd.confirm.OnTapped = func() { answer(true); cd.d.Hide() }
	cd.cancel.OnTapped = func() { answer(false); cd.d.Hide() }
	if delay := sh.confirmer.armDelay; delay > 0 {
		cd.confirm.Disable()
		time.AfterFunc(delay, func() { sh.do(cd.confirm.Enable) })
	}
	p.agentsDialog = cd
	sh.showDialog(cd.d) // Escape closes it, answering no (SetOnClosed)
}

// allowanceHint is the confirmation's line about the allowance (Cluster Settings › Agents) for an
// agent's request: how the user can let such changes run without asking, or why this one asks
// although they did. Empty for the user's own commands.
func (sh *shell) allowanceHint(req engine.ConfirmRequest) string {
	if !req.Origin.IsAgent() {
		return ""
	}
	allowed := sh.ui.ClusterSettings[req.Context].AgentWrites
	switch {
	case req.AgentsAsk: // a role binding (H-27): the allowance does not skip this question
		return "A role binding an agent asks for always waits for you, also where agents may change this cluster without asking."
	case req.Danger == engine.DangerDestructive && allowed:
		return "Agents may change this cluster without asking (Cluster Settings › Agents), but destructive commands always ask."
	case req.Danger == engine.DangerDestructive:
		return "Destructive commands from agents always ask."
	case req.Danger == engine.DangerMutating:
		return "To let agents change this cluster without asking, allow it in Cluster Settings › Agents; destructive commands would still ask."
	}
	return ""
}
