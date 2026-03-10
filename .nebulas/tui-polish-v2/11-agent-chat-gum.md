+++
id = "agent-chat-gum"
title = "Gum-powered agent chat for in-flight intervention"
type = "feature"
priority = 2
depends_on = ["hail-gum", "process-drilldown"]
+++

## Problem

The custom chat system (`internal/chat/`) tries to replicate Claude Code's conversational interface inside BubbleTea. It's clunky — custom markdown rendering, streaming indicators, conversation persistence — all reimplemented poorly. But the underlying need is real: a developer watching a phase should be able to talk to the agent mid-flight.

## Solution

Replace the custom chat system with gum-powered in-flight intervention. When a developer selects a running phase in the tree view and presses a key (e.g., `c` for chat), the TUI suspends and opens a gum-based conversation interface. The message is injected into the agent's next cycle via the existing hail relay mechanism.

### Approach

1. **Trigger**: In the tree view, when the selected node is a running phase or active agent:
   - Press `c` → open chat intervention
   - Press `g` → send quick guidance (one-liner)

2. **Chat flow using gum**:
   ```
   Developer presses 'c' on a running phase
   → TUI suspends (tea.ExecProcess)
   → gum format renders current phase context:
     "Phase: unified-diff | Cycle 2/5 | Coder active"
     "Last reviewer feedback: 3 issues found..."
   → gum write --header "Send guidance to agent" --placeholder "e.g., focus on the diffview.go file first"
   → Developer types message, presses Ctrl+D
   → TUI resumes
   → Message posted as a hail with Kind=HailGuidance (new kind)
   → Relayed to agent in next cycle's prompt as [HUMAN GUIDANCE] block
   ```

3. **Quick guidance** (`g` key):
   ```
   → gum input --header "Quick guidance" --placeholder "one-line instruction"
   → Posted as hail, relayed same way
   ```

4. **New hail kind**: Add `HailGuidance` to the hail kinds. Unlike other hails (which are agent-initiated), this is human-initiated. The relay formatter includes it as:
   ```
   [HUMAN GUIDANCE]
   The developer sent the following guidance for this phase:
   "{message}"
   Please incorporate this into your current work.
   ```

5. **Phase context display**: Before the gum write prompt, show relevant context so the developer knows what to say:
   - Phase title and description
   - Current cycle number
   - Last reviewer summary (if any)
   - Files currently claimed
   - Recent agent activity

6. **Remove `internal/chat/`**: The standalone chat system (ChatView, ChatModel, ChatProvider, chat command) can be deprecated. The gum-based intervention covers the "talk to agent" use case without maintaining a parallel chat UI. The chat command could redirect to `claude` directly if conversational use is needed.

## Files

- `internal/loop/hail.go` — add `HailGuidance` kind
- `internal/loop/hail_relay.go` — format guidance hails differently from agent-initiated hails
- `internal/gum/chat.go` — gum-based chat intervention flow (context display + write prompt)
- `internal/tui/model.go` — handle 'c' and 'g' keypresses on running phases, trigger gum via tea.ExecProcess
- `internal/tui/msg.go` — add MsgGuidanceSent for post-gum resume

## Acceptance Criteria

- [ ] Developer can press 'c' on a running phase to send multi-line guidance
- [ ] Developer can press 'g' on a running phase to send quick one-line guidance
- [ ] TUI suspends cleanly during gum interaction, resumes after
- [ ] Phase context (title, cycle, last feedback) displayed before the prompt
- [ ] Guidance posted as hail and relayed to agent in next cycle
- [ ] Works for both single-task (loop) and nebula (multi-phase) modes
- [ ] `go test ./internal/gum/...` passes
- [ ] `go build ./...` and `go vet ./...` pass
