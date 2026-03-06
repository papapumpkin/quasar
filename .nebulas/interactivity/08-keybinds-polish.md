+++
id = "keybinds-polish"
title = "Vim keybindings, model switching, and UX polish"
type = "feature"
priority = 3
depends_on = ["streaming", "cli-command"]
+++

## Problem

Shore has vim-inspired keybindings, model switching via `{/}`, and polished UX
details (search highlighting, title editing, loading spinners). The initial
implementation needs these finishing touches.

## Solution

**Vim keybindings (mirroring Shore):**
- `h/l` — switch focus between sidebar and chat (or collapse/expand sidebar)
- `j/k` — navigate within focused panel
- `g/G` — jump to top/bottom of message list or conversation list
- `/` — enter search mode in sidebar
- `i` — focus input (compose mode)
- `Esc` — exit current mode, return to normal
- `q` — quit chat

**Model switching (mirroring Shore's `{/}` carousel):**
- `{` and `}` cycle through available models
- Model name shown in title bar and sidebar badge
- New messages use the currently selected model
- Existing messages retain their original model tag

**Title editing:**
- `t` on a selected conversation enters title edit mode
- Inline text input replaces the title in the sidebar
- Enter confirms, Esc cancels
- Mirrors Shore's `TitleEdit` AppState

**Polish:**
- Consistent color scheme using existing galactic palette
- Smooth transitions when switching conversations
- Empty state: helpful message when no conversations exist
- Resize handling: sidebar adapts, messages reflow

## Files

- `internal/tui/chatmodel.go` — key handling additions (extend)
- `internal/tui/chatsidebar.go` — title edit mode, search (extend)
- `internal/tui/chatview.go` — model indicator, scroll improvements (extend)

## Acceptance Criteria

- [ ] Full vim keybinding set implemented
- [ ] `{/}` cycles through available models with visual indicator
- [ ] `t` enables inline title editing in sidebar
- [ ] Empty state renders helpful onboarding message
- [ ] Consistent styling with existing Quasar TUI palette
- [ ] Terminal resize handled gracefully
