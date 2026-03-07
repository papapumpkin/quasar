+++
id = "contextual-chat"
title = "Context-aware chat seeded with execution state"
type = "feature"
priority = 1
depends_on = ["work-summary"]
+++

## Problem

The chat TUI is a standalone conversation — it has no knowledge of what the agent fleet is doing. If a developer sees a phase going sideways and wants to intervene ("hey, don't refactor that module, just fix the bug"), they'd need to manually explain the full context. The whole point of chatting with agents is that they should already know what's happening.

## Solution

Make the chat context-aware by seeding conversations with execution state when launched from the cockpit.

1. **"Chat about this phase" action.** Add a keybinding (e.g., `c`) on the board view that opens the chat TUI pre-seeded with the selected phase's context: phase spec (from the .md file), current cycle, last agent output summary, current diff, and reviewer findings. This becomes a system message at the top of the conversation.

2. **Context builder.** Create a `chat.ContextBuilder` that assembles a structured context blob from phase execution state. Inputs: phase ID, phase spec body, cycle summaries (from phase 03), diff stat, reviewer report, file claims. Output: a formatted system message that gives the AI full situational awareness.

3. **Refresh context command.** Add a `/refresh` command in the chat input that re-fetches the latest execution state and appends it as a new system message, so long-running conversations stay current.

4. **Chat-to-phase linking.** When a chat is initiated from a phase, store the `phaseID` on the conversation. Show the phase ID in the chat title bar. This lets the developer resume context-aware conversations from the sidebar.

5. **Feedback relay (stretch).** When the developer provides feedback in a contextual chat, surface an option to inject the feedback as a hail to the running phase, so the agent picks it up in its next cycle.

## Files

- `internal/chat/context.go` — new `ContextBuilder` that assembles phase context into a system message
- `internal/tui/chatmodel.go` — accept initial context, render phase-linked title
- `internal/tui/chatview.go` — show phase badge in title bar when context-linked
- `internal/tui/boardview.go` — add `c` keybinding to launch contextual chat for selected phase
- `internal/tui/model.go` — wire board-to-chat transition with context payload
- `internal/tui/keys.go` — register `c` keybinding for contextual chat
- `internal/chat/store.go` — persist `phaseID` linkage on conversations

## Acceptance Criteria

- [ ] Pressing `c` on a phase in the board opens chat pre-seeded with that phase's context
- [ ] Context includes: phase spec, cycle count, last summary, diff stat, reviewer findings
- [ ] `/refresh` command in chat re-fetches and appends latest execution state
- [ ] Chat title bar shows linked phase ID when context-aware
- [ ] Conversations persist the phase linkage and can be resumed from the sidebar
