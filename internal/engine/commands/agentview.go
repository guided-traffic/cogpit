package commands

import "github.com/guided-traffic/cogpit/internal/engine"

// The result types of this package that carry cluster text, as an agent reads them (H-82, H-63):
// Dispatch hands an agent's result to ForAgent (engine.AgentResult, engine/agentview.go). The
// built-in agent over a connection the user marked private gets a view that changes nothing but
// a chart's README's frame (engine/readslikeuser.go).

// privateReads ends the description of a read that withholds from agents what the built-in agent
// over a private connection reads as the window shows it (D-10, H-94).
const privateReads = " Cogpit's built-in agent over a connection the user marked private (a local model or a private " +
	"cloud) reads it as Cogpit's window shows it."

// ForAgent filters object.get's model, with its related objects and its volume's usage, as a
// bare model is filtered.
func (o objectResult) ForAgent(v *engine.AgentView) any {
	v.Detail(o.DetailModel)
	return o
}

// ForAgent filters an editor tab's text: manifests (object.edit, resource.createTab) or a chart's
// values (a Helm Install or Upgrade tab).
func (r EditResult) ForAgent(v *engine.AgentView) any {
	r.Text = v.TabText(r.Tab.Type, r.Text)
	return r
}
