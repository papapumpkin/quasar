package loop

import (
	"context"
	"fmt"
	"os"

	"github.com/papapumpkin/quasar/internal/agent"
)

// cacheSystemPrompts pre-computes and stores the system prompts for both
// coder and reviewer agents. This must be called once at phase start (in
// runLoop) before the cycle loop begins. By building the prompts once, we
// guarantee byte-identical system prompts across all cycles within a phase,
// which is critical for prompt cache hits in the Claude CLI.
func (l *Loop) cacheSystemPrompts() {
	opts := agent.PromptOpts{
		FabricEnabled:  l.FabricEnabled,
		TaskID:         l.TaskID,
		ProjectContext: l.ProjectContext,
	}
	l.cachedCoderSystemPrompt = agent.BuildSystemPrompt(l.CoderPrompt, opts)
	l.cachedReviewerSystemPrompt = agent.BuildSystemPrompt(l.ReviewPrompt, opts)
}

// trackCacheMetrics emits a cache metrics event and updates the cycle state's
// cache hit/miss counters. It also logs cache stability diagnostics to stderr
// when CacheVerbose is enabled.
func (l *Loop) trackCacheMetrics(ctx context.Context, state *CycleState, agentRole string, result *agent.InvocationResult) {
	hash := result.SystemPromptHash
	if state.prevSystemPromptHash != "" && hash == state.prevSystemPromptHash {
		state.cacheHitCount++
		state.totalCachedBytes += int64(result.SystemPromptLen)
		if l.CacheVerbose {
			fmt.Fprintf(os.Stderr, "[cache] %s cycle %d: system prompt STABLE (hash match, %d bytes cached)\n",
				agentRole, state.Cycle, result.SystemPromptLen)
		}
	} else {
		state.cacheMissCount++
		if l.CacheVerbose && state.prevSystemPromptHash != "" {
			fmt.Fprintf(os.Stderr, "[cache] %s cycle %d: system prompt CHANGED (cache miss, prev=%.8s curr=%.8s)\n",
				agentRole, state.Cycle, state.prevSystemPromptHash, hash)
		}
	}
	state.prevSystemPromptHash = hash

	l.emit(ctx, Event{
		Kind:   EventCacheMetrics,
		BeadID: state.TaskBeadID,
		Cycle:  state.Cycle,
		Agent:  agentRole,
		Result: result,
		Message: fmt.Sprintf("sys_prompt_hash=%s sys_prompt_len=%d user_prompt_len=%d cost=%.4f",
			hash, result.SystemPromptLen, result.UserPromptLen, result.CostUSD),
	})
}
