package engine

import (
	"context"
	"errors"
	"fmt"
)

// The built-in agent keeps its conversations across restarts in an owner-only file, scrubbed
// (D-10, the owner's decision, as D-19 keeps Secret values out of drafts): before a tool result, an
// answer or a message is kept, the values of the connected cluster's Secrets in it are replaced by
// «secret <ns>/<name>/<key>», as the H-82 filter replaces them in what agents read. A connection
// the user marked private reads like the user (Invocation.ReadsLikeUser), so its results carry
// values the filter did not see; this is the step that keeps them off the disk.

// ErrScrubContext refuses to scrub for a context that is not connected: the Secrets the filter
// knows are the connected clusters'.
var ErrScrubContext = errors.New("the conversation's cluster is not connected, so its Secrets are not known")

// ScrubSecretValues returns texts with the values of the Secrets of kubeContext's cluster replaced,
// as the agents' filter replaces them (AgentView.Text). It first takes the Secrets as an agent's
// read does (agentSecrets: listed, or refused, within 10 s; the cache keeps them warm afterwards),
// so that a value is replaced even when no view held the Secrets. It fails closed: an error
// (ErrScrubContext when kubeContext is not connected, ErrAgentSecretsPending, ctx's error) and no
// text when the Secrets could not be read; the caller then keeps nothing of the texts. It never
// logs or returns a value it replaced.
func (s *Session) ScrubSecretValues(ctx context.Context, kubeContext string, texts ...string) ([]string, error) {
	b := s.BackendOf(kubeContext) // any connected cluster's (D-69)
	if b == nil || kubeContext == "" {
		return nil, ErrScrubContext
	}
	if err := b.agentSecrets(ctx); err != nil {
		return nil, err
	}
	out := make([]string, len(texts))
	err := s.safeCall("chat.scrub", func() error {
		v := &AgentView{m: scrubberOf(b.secretMatcher())}
		for i, t := range texts {
			out[i] = v.Text(t)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scrub: %w", err)
	}
	return out, nil
}
