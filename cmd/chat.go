package cmd

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/chat"
	"github.com/papapumpkin/quasar/internal/claude"
	"github.com/papapumpkin/quasar/internal/config"
	"github.com/papapumpkin/quasar/internal/tui"
)

// chatCmd launches the interactive chat TUI.
var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Launch the interactive chat mode",
	Long: `Launch a conversational chat interface powered by Claude.

The chat TUI provides a left sidebar with saved conversations and a main
panel for composing and reading messages. Conversations are persisted as
JSON files in ~/.quasar/chats/.

Use vim-style keys (h/l) to navigate between sidebar and chat area,
j/k to scroll or navigate conversations, and i to compose a message.`,
	Args: cobra.NoArgs,
	RunE: runChat,
}

func init() {
	chatCmd.Flags().Bool("new", false, "start a fresh conversation")
	chatCmd.Flags().String("model", "", "override the default AI model")
	rootCmd.AddCommand(chatCmd)
}

// runChat initializes the chat store, provider, and TUI model, then
// launches the BubbleTea program in alternate screen mode.
func runChat(cmd *cobra.Command, _ []string) error {
	if !isStderrTTY() {
		return fmt.Errorf("quasar chat requires a TTY (terminal)")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if v, _ := cmd.Flags().GetBool("verbose"); v {
		cfg.Verbose = true
	}

	// Determine model: flag > config > built-in default.
	model, _ := cmd.Flags().GetString("model")
	if model == "" {
		model = cfg.Model
	}

	// Set up chat persistence.
	chatDir, err := chat.DefaultDir()
	if err != nil {
		return fmt.Errorf("failed to determine chat directory: %w", err)
	}
	store, err := chat.NewFileStore(chatDir)
	if err != nil {
		return fmt.Errorf("failed to initialize chat store: %w", err)
	}

	// Set up AI provider backed by the Claude CLI invoker.
	invoker := claude.NewInvoker(cfg.ClaudePath, cfg.Verbose)

	workDir, wdErr := os.Getwd()
	if wdErr != nil {
		return fmt.Errorf("failed to get working directory: %w", wdErr)
	}
	provider := chat.NewClaudeProvider(invoker, workDir)

	// Build and configure the chat TUI model.
	chatModel := tui.NewChatModel(store, provider, model)

	startNew, _ := cmd.Flags().GetBool("new")
	if !startNew {
		// Attempt to resume the most recent conversation.
		convs, listErr := store.List()
		if listErr != nil {
			fmt.Fprintf(os.Stderr, "chat: failed to list conversations: %v\n", listErr)
		}
		if len(convs) > 0 {
			conv, loadErr := store.Load(convs[0].ID)
			if loadErr != nil {
				fmt.Fprintf(os.Stderr, "chat: failed to load last conversation: %v\n", loadErr)
			} else {
				chatModel.ActiveConv = conv
				chatModel.ChatView.Messages = conv.Messages
				chatModel.ChatView.Title = conv.AutoTitle()
				chatModel.ChatView.ModelTag = conv.Model
			}
		}
	}

	p := tea.NewProgram(chatModel, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("chat TUI error: %w", err)
	}

	return nil
}
