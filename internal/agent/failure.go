package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// failure turns a Dispatch error into the tool item's state and the text the model is sent, which
// says what happened and what to do next (as the MCP server's error results do).
func failure(cmd *engine.Command, err error) (engine.ChatToolState, string) {
	var pe *engine.ParamsError
	var de *engine.DisabledError
	var ge *engine.GUIOnlyError
	switch {
	case errors.As(err, &pe):
		schema, _ := cmd.ToolSchema()
		return engine.ChatToolFailed, fmt.Sprintf("invalid arguments for %s: %v. Call it again with arguments that match its "+
			"input schema: %s", cmd.ToolName(), pe.Err, schema)
	case errors.As(err, &ge):
		return engine.ChatToolRefused, fmt.Sprintf("%q runs only on the user's own action in Cogpit's window, so it did not run: "+
			"%v. Ask the user to do it in Cogpit's window if it is needed.", cmd.Label, ge.Reason)
	case errors.As(err, &de):
		return engine.ChatToolFailed, fmt.Sprintf("%q is not available right now: %s.", cmd.Label, de.Reason)
	case errors.Is(err, engine.ErrDeclined):
		return engine.ChatToolRefused, fmt.Sprintf("the user declined %q in Cogpit's confirmation dialog, so it did not run. "+
			"Do not retry it unless the user asks you to.", cmd.Label)
	case errors.Is(err, engine.ErrConfirmationUnavailable):
		return engine.ChatToolRefused, fmt.Sprintf("%q needs the user's confirmation, but Cogpit has no window to ask in, so it "+
			"did not run.", cmd.Label)
	case errors.Is(err, engine.ErrConnectCancelled):
		return engine.ChatToolStopped, fmt.Sprintf("%q did not finish: its connect was cancelled (Cancel Connecting in Cogpit's "+
			"window, or cluster_disconnect), so the cluster is not connected. Do not connect it again unless the user asks you to.", cmd.Label)
	case errors.Is(err, engine.ErrConnectSuperseded):
		return engine.ChatToolStopped, fmt.Sprintf("%q did not connect: another cluster was chosen in Cogpit's window before this "+
			"connect began (ui_state names the cluster shown). Nothing failed.", cmd.Label)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return engine.ChatToolStopped, fmt.Sprintf("%q was cancelled before it finished; if it was waiting for the user's "+
			"confirmation, it did not run.", cmd.Label)
	case errors.Is(err, engine.ErrNotConnected):
		return engine.ChatToolFailed, fmt.Sprintf("%q needs a connected cluster: %v.", cmd.Label, err)
	case errors.Is(err, engine.ErrUnknownCommand):
		return engine.ChatToolFailed, fmt.Sprintf("Cogpit no longer has the command behind %s.", cmd.ToolName())
	}
	return engine.ChatToolFailed, fmt.Sprintf("%q failed: %v", cmd.Label, err)
}
