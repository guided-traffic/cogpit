package engine

import (
	"context"
	"errors"
	"time"

	"github.com/guided-traffic/cogpit/internal/cache"
)

// The Secrets an agent's read waits for (H-82; the owner's decision, option b). The filter in
// Dispatch replaces the values of the Secrets the cache holds in what an agent reads, and until
// then the cache held them only while the Secrets view was open and for its warm period after: a
// log an agent read in a session without that view went out as printed. Now a result the filter
// reads (forAgent) first takes the Secrets as the Secrets view would read them (the access reviews'
// plan: in all namespaces, or in each namespace the user may list them in), waits for them bounded
// by agentSecretsWait, and lets them go at once: the cache keeps them warm for its warm period (5
// minutes by default) after the last agent read, and a read within it costs no request. Options
// were (a) only what the cache holds anyway, (b) this, (c) the Secrets watched whenever the MCP
// server is on; the owner chose (b). The window's reads and the CLI's start nothing.

// agentSecretsWait bounds how long an agent's read waits for the Secrets to be listed.
const agentSecretsWait = 10 * time.Second

// ErrAgentSecretsPending refuses an agent's read whose Secrets were not listed in time: without
// them the filter could not replace their values, so the result is withheld rather than sent as
// printed (fail closed).
var ErrAgentSecretsPending = errors.New("the cluster's Secrets are still being read, to keep their values out of what agents read; try again in a moment")

// needsAgentSecrets reports whether forAgent reads a result's text for Secret values.
func needsAgentSecrets(res any) bool {
	switch res.(type) {
	case *DetailModel, DockOutput, EditorState, EditDiff, ApplyResult, RevertResult, AgentResult:
		return true
	}
	return false
}

// agentSecrets takes the backend's Secrets for an agent's read and waits until they are listed,
// were refused (the user may not list them there: nothing to replace, H-82), or the user may list
// them in none of the namespaces Cogpit reads; nil then. ErrAgentSecretsPending when they are still
// being listed after agentSecretsWait. Without a cache or a Secrets kind there is nothing to take.
func (b *Backend) agentSecrets(ctx context.Context) error {
	if b == nil || b.Cache == nil {
		return nil
	}
	p := b.builtinProfile(secretsKind)
	if p == nil {
		return nil
	}
	if plan := b.settledPlan(ctx, p); plan.perNamespace && len(plan.watch) == 0 {
		return nil
	}
	h := b.acquireSettled(ctx, p)
	defer h.Release() // the cache keeps them warm (its warm period): the next read costs nothing
	wctx, cancel := context.WithTimeout(ctx, agentSecretsWait)
	defer cancel()
	done := func() bool { return h.HasSynced() || cache.IsRefused(h.Err()) }
	if done() {
		return nil
	}
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-wctx.Done():
			if done() {
				return nil
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			return ErrAgentSecretsPending
		case <-t.C:
			if done() {
				return nil
			}
		}
	}
}
