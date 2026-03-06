package cmd

import (
	"testing"
)

func TestChatCmd_Registered(t *testing.T) {
	t.Parallel()

	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "chat" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'chat' subcommand to be registered on rootCmd")
	}
}

func TestChatCmd_Flags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		flag string
	}{
		{"new", "new"},
		{"model", "model"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := chatCmd.Flags().Lookup(tt.flag)
			if f == nil {
				t.Errorf("expected flag %q to be registered on chat command", tt.flag)
			}
		})
	}
}

func TestChatCmd_RequiresTTY(t *testing.T) {
	// Not parallel: calls runChat which checks stderr TTY state.
	err := runChat(chatCmd, nil)
	if err == nil {
		t.Fatal("expected error when not on a TTY")
	}
	if got := err.Error(); got != "quasar chat requires a TTY (terminal)" {
		t.Errorf("unexpected error: %q", got)
	}
}
