package engine

import "context"

// A private connection of the built-in agent (D-10, H-94; the owner's decision). The user marks an
// LLM connection "private (a local model or a private cloud)"; the agent then dispatches its tool
// calls with Invocation.ReadsLikeUser, and the engine gives such a turn what the window shows
// instead of what agents are given: Secret values (object.get, an editor's text, a logs tab's lines
// unscrubbed), Helm's user-supplied values, Flux's credential fields, a failed Helm action's error,
// the window's notifications in full. Dispatch honours the mark only with OriginBuiltInAgent and
// carries it in the command's context (ReadsLikeUser); every read path that withholds from agents
// asks ReadsLikeUser or ReadsAsAgent instead of the origin alone. Write rules never ask it: D-2's
// confirmations, the agents-ask rules (H-27, A1, A2), the GUI-only commands (secret.reveal,
// secret.create, ...), a Secret's Edit and Create tabs (D-19), the markers an agent may not write
// back, and a terminal tab's agent-read switch (H-83) are the same for a private turn as for any
// agent. The audit log marks the private turn's lines (AuditEntry.ReadsLikeUser) and keeps values
// out of them as for every origin.

// readsLikeUserKey is the context key under which Dispatch hands a command whether its reads are
// a private turn's.
type readsLikeUserKey struct{}

// invocationKey is the context key under which Dispatch hands a command its invocation.
type invocationKey struct{}

// privateTurn reports whether the invocation is a turn of the built-in agent over a connection the
// user marked private: the mark of any other origin is ignored.
func (inv Invocation) privateTurn() bool {
	return inv.Origin == OriginBuiltInAgent && inv.ReadsLikeUser
}

// withInvocation is the context a command runs with: its origin and whether it reads like the
// user. The mark is set either way, so that a context a command passes on to a nested Dispatch
// never carries an outer turn's mark into another origin's command.
func withInvocation(ctx context.Context, inv Invocation) context.Context {
	ctx = context.WithValue(ctx, invocationKey{}, inv) // a background job's "ended" audit line (jobs.go)
	ctx = context.WithValue(ctx, originKey{}, inv.Origin)
	return context.WithValue(ctx, readsLikeUserKey{}, inv.privateTurn())
}

// PrivateTurn reports whether the running command is a turn of the built-in agent over a
// connection the user marked private: the one reader like the user (ReadsLikeUser) that is not the
// window, and so the one whose reads can reach the audit log (UserOnlyError).
func PrivateTurn(ctx context.Context) bool {
	on, _ := ctx.Value(readsLikeUserKey{}).(bool)
	return on && OriginOf(ctx) == OriginBuiltInAgent
}

// ReadsLikeUser reports whether the running command reads what Cogpit's window shows: the GUI's
// own command, or the built-in agent's over a connection the user marked private. A read path that
// withholds a value from every caller but the window asks this; one that withholds from agents
// only asks ReadsAsAgent. Write rules never ask either.
func ReadsLikeUser(ctx context.Context) bool {
	return OriginOf(ctx) == OriginGUI || PrivateTurn(ctx)
}

// ReadsAsAgent reports whether the running command's reads are filtered as agents' are: an
// external agent's, or the built-in agent's over a connection not marked private.
func ReadsAsAgent(ctx context.Context) bool {
	return OriginOf(ctx).IsAgent() && !ReadsLikeUser(ctx)
}

// UserOnlyError is an error whose text only a reader like the user gets (ReadsLikeUser): a failed
// Helm action's error, which can quote what a chart's lookup read from a Secret. A private turn
// reads Error, as the window does; the audit log, which keeps values out for every origin, records
// Withheld in its place. errors.Is and errors.As see Err.
type UserOnlyError struct {
	Err error
	// Withheld is what the audit log records: that the action failed and where its error is shown.
	Withheld string
}

func (e *UserOnlyError) Error() string { return e.Err.Error() }

// Unwrap returns the error.
func (e *UserOnlyError) Unwrap() error { return e.Err }
