package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Error codes in the structured content of a failed tool call, for agents that branch on them.
// The text content says the same in words.
const (
	codeInvalidParams           = "invalidParams"
	codeDisabled                = "disabled"
	codeGUIOnly                 = "guiOnly"
	codeDeclined                = "declined"
	codeConfirmationUnavailable = "confirmationUnavailable"
	codeCancelled               = "cancelled"
	codeNotConnected            = "notConnected"
	codeUnknownCommand          = "unknownCommand"
	codeFailed                  = "failed"
	codeInternal                = "internal"
)

var (
	errServerClosed = errors.New("the MCP server is shutting down")
	errRequestEnded = errors.New("the client closed the request")
)

// newMCPServer builds an SDK server whose tools are the registry's commands at this moment, and
// returns it with the number of tools. A command that cannot be a tool is logged and left out;
// it is a programming error, but one command must not cost the agent every other tool.
func (s *Server) newMCPServer() (*sdk.Server, int) {
	reg := s.session.Registry()
	srv := sdk.NewServer(&sdk.Implementation{Name: serverName, Title: "Cogpit", Version: s.opts.Version}, &sdk.ServerOptions{
		Instructions: instructions(reg),
		Logger:       sdkLogger(s.log),
		// Tools only: the tool list is fixed for the server's life, and Cogpit sends no MCP logs.
		Capabilities: &sdk.ServerCapabilities{Tools: &sdk.ToolCapabilities{}},
	})
	n := 0
	for _, cmd := range reg.Commands() {
		tool, schema, err := toolFor(cmd)
		if err == nil {
			err = addTool(srv, tool, s.handler(cmd, schema))
		}
		if err != nil {
			s.log.Error("command left out of the MCP tools", "command", cmd.ID, "err", err)
			continue
		}
		n++
	}
	return srv, n
}

// addTool adds a tool, turning the SDK's panic on a malformed tool into an error.
func addTool(srv *sdk.Server, tool *sdk.Tool, h sdk.ToolHandler) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("add tool %s: %v", tool.Name, r)
		}
	}()
	srv.AddTool(tool, h)
	return nil
}

// toolFor describes a command as an MCP tool. It also returns the input schema as JSON, which
// error results quote back to the model.
func toolFor(cmd *engine.Command) (*sdk.Tool, json.RawMessage, error) {
	schema, err := cmd.ToolSchema() // shared with the built-in agent (engine/toolschema.go)
	if err != nil {
		return nil, nil, err
	}
	// Read commands read the cache and UI commands change only what the window shows: neither
	// changes a cluster, and repeating either has no further effect.
	harmless := cmd.Danger == engine.DangerRead || cmd.Danger == engine.DangerUI
	return &sdk.Tool{
		Name:        cmd.ToolName(),
		Title:       cmd.Label,
		Description: cmd.ToolDescription(),
		InputSchema: schema,
		Annotations: &sdk.ToolAnnotations{
			Title:          cmd.Label, // for clients older than the tool-level title
			ReadOnlyHint:   harmless,
			IdempotentHint: harmless,
			// Explicit, because both hints default to true in the specification.
			DestructiveHint: ptr(cmd.Danger == engine.DangerDestructive || cmd.Escalates),
			OpenWorldHint:   ptr(false),
		},
	}, schema, nil
}

// instructions tell the model how Cogpit's tools fit together. The workflow hints name only
// tools that exist.
func instructions(reg *engine.Registry) string {
	var b strings.Builder
	b.WriteString("Cogpit is a Kubernetes IDE; these tools drive its window and read the clusters it is connected to. " +
		"Read and UI tools answer from Cogpit's cache and run without asking. Mutating and destructive tools wait " +
		"until the user confirms them in Cogpit's window; if the user declines, do not retry unless asked.")
	has := func(id string) bool { _, ok := reg.Get(id); return ok }
	if has("ui.state") {
		b.WriteString(" Call ui_state to see what the user sees.")
	}
	if has("resources.find") && has("ui.navigate") {
		b.WriteString(" To show the user an object, find it with resources_find and open it with ui_navigate.")
	}
	return b.String()
}

// handler dispatches a tool call as the command with origin ExternalAgent. Engine failures become
// error results (IsError) that a model can act on, never protocol errors.
func (s *Server) handler(cmd *engine.Command, schema json.RawMessage) sdk.ToolHandler {
	return func(ctx context.Context, req *sdk.CallToolRequest) (res *sdk.CallToolResult, _ error) {
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("MCP tool call panicked", "tool", cmd.ToolName(), "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
				res = toolError(codeInternal, fmt.Sprintf("%q failed with an internal error in Cogpit.", cmd.Label))
			}
		}()
		ctx, cancel := s.callContext(ctx)
		defer cancel()
		var args json.RawMessage
		if req != nil && req.Params != nil {
			args = req.Params.Arguments
		}
		out, err := s.session.Dispatch(ctx, engine.Invocation{ID: cmd.ID, Params: args, Origin: engine.OriginExternalAgent})
		if err != nil {
			return failure(cmd, schema, err), nil
		}
		res = success(out)
		// A read while the connection is interrupted: the data is the cache's as of then (D-58).
		if note := s.session.StaleNoteFor(engine.Invocation{ID: cmd.ID, Params: args, Origin: engine.OriginExternalAgent}); note != "" && cmd.Danger == engine.DangerRead && !res.IsError {
			res.Content = append(res.Content, &sdk.TextContent{Text: note})
		}
		return res, nil
	}
}

// callContext bounds a tool call by the server's lifetime and, over HTTP, by the request that
// carries it, so that a call waiting for the user's confirmation ends when the client gives up or
// Cogpit's endpoint closes, and its command does not run. The SDK detaches handler contexts
// from the transport; the request's context is the guard's, carried as a value. That is sound
// only because the HTTP handler is stateless: each POST is its own session, so the stored request
// is the one that carries this call.
func (s *Server) callContext(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(ctx)
	stops := []func() bool{context.AfterFunc(s.life(), func() { cancel(errServerClosed) })}
	if reqCtx, ok := ctx.Value(requestContextKey{}).(context.Context); ok {
		stops = append(stops, context.AfterFunc(reqCtx, func() { cancel(errRequestEnded) }))
	}
	return ctx, func() {
		for _, stop := range stops {
			stop()
		}
		cancel(nil)
	}
}

// success returns the command's result as JSON text and, when it is a JSON object, as structured
// content too.
func success(out any) *sdk.CallToolResult {
	data, err := json.Marshal(out)
	if err != nil {
		return toolError(codeInternal, "The command ran, but Cogpit could not encode its result as JSON: "+err.Error())
	}
	res := &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(data)}}}
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		res.StructuredContent = json.RawMessage(data)
	}
	return res
}

// failure turns a Dispatch error into an error result whose text tells a model what happened and
// what to do next.
func failure(cmd *engine.Command, schema json.RawMessage, err error) *sdk.CallToolResult {
	var pe *engine.ParamsError
	var de *engine.DisabledError
	var ge *engine.GUIOnlyError
	switch {
	case errors.As(err, &pe):
		return toolError(codeInvalidParams, fmt.Sprintf("Invalid arguments for %s: %v. Call it again with arguments that match its input schema: %s",
			cmd.ToolName(), pe.Err, schema))
	case errors.As(err, &ge):
		return toolError(codeGUIOnly, fmt.Sprintf("%q runs only on the user's own action in Cogpit's window, so it did not run "+
			"and nothing was asked, read or written: %v. Ask the user to do it in Cogpit's window if it is needed.", cmd.Label, ge.Reason))
	case errors.As(err, &de):
		return toolError(codeDisabled, fmt.Sprintf("%q is not available right now: %s.", cmd.Label, de.Reason))
	case errors.Is(err, engine.ErrDeclined):
		return toolError(codeDeclined, fmt.Sprintf("The user declined %q in Cogpit's confirmation dialog, so it did not run. "+
			"Do not retry it unless the user asks you to.", cmd.Label))
	case errors.Is(err, engine.ErrConfirmationUnavailable):
		return toolError(codeConfirmationUnavailable, fmt.Sprintf("%q needs the user's confirmation, but Cogpit has no window "+
			"to ask in (it runs headless), so it did not run. Ask the user to run it in Cogpit's window.", cmd.Label))
	case errors.Is(err, engine.ErrConnectCancelled):
		return toolError(codeCancelled, fmt.Sprintf("%q did not finish: its connect was cancelled (cluster_disconnect: the user's "+
			"Cancel Connecting in Cogpit's window, or an agent's), so the cluster is not connected. Do not connect it again "+
			"unless the user asks you to.", cmd.Label))
	case errors.Is(err, engine.ErrConnectSuperseded):
		return toolError(codeCancelled, fmt.Sprintf("%q did not connect: another cluster was chosen in Cogpit's window before "+
			"this connect began (ui_state names the cluster shown). Nothing failed.", cmd.Label))
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return toolError(codeCancelled, fmt.Sprintf("%q was cancelled before it finished (%v). If it was waiting for the "+
			"user's confirmation, it did not run.", cmd.Label, err))
	case errors.Is(err, engine.ErrNotConnected):
		return toolError(codeNotConnected, fmt.Sprintf("%q needs a connected cluster: %v. Ask the user to connect one in Cogpit.", cmd.Label, err))
	case errors.Is(err, engine.ErrUnknownCommand):
		return toolError(codeUnknownCommand, fmt.Sprintf("Cogpit no longer has the command behind %s.", cmd.ToolName()))
	default:
		return toolError(codeFailed, fmt.Sprintf("%q failed: %v", cmd.Label, err))
	}
}

func toolError(code, msg string) *sdk.CallToolResult {
	return &sdk.CallToolResult{
		IsError:           true,
		Content:           []sdk.Content{&sdk.TextContent{Text: msg}},
		StructuredContent: map[string]string{"error": code, "message": msg},
	}
}

func ptr[T any](v T) *T { return &v }

// sdkLogger is the logger handed to the SDK, at warning level and above: the SDK logs every
// stateless session's connect and disconnect at info level, which is noise here.
func sdkLogger(log *slog.Logger) *slog.Logger {
	return slog.New(minLevel{Handler: log.With("sdk", true).Handler(), min: slog.LevelWarn})
}

// minLevel drops records below min.
type minLevel struct {
	slog.Handler
	min slog.Level
}

func (h minLevel) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= h.min && h.Handler.Enabled(ctx, l)
}

func (h minLevel) WithAttrs(attrs []slog.Attr) slog.Handler {
	return minLevel{Handler: h.Handler.WithAttrs(attrs), min: h.min}
}

func (h minLevel) WithGroup(name string) slog.Handler {
	return minLevel{Handler: h.Handler.WithGroup(name), min: h.min}
}
