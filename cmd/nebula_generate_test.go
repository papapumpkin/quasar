package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestSlugify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "simple prompt",
			input:  "Add JWT authentication",
			maxLen: 50,
			want:   "add-jwt-authentication",
		},
		{
			name:   "special characters stripped",
			input:  "Fix bug #123 in parser!!!",
			maxLen: 50,
			want:   "fix-bug-123-in-parser",
		},
		{
			name:   "extra spaces collapsed",
			input:  "   spaces   everywhere   ",
			maxLen: 50,
			want:   "spaces-everywhere",
		},
		{
			name:   "underscores replaced",
			input:  "my_cool_feature",
			maxLen: 50,
			want:   "my-cool-feature",
		},
		{
			name:   "truncated to maxLen",
			input:  "this is a very long prompt that should be truncated to the maximum length",
			maxLen: 20,
			want:   "this-is-a-very-long",
		},
		{
			name:   "trailing hyphen after truncation removed",
			input:  "abcdefghij-klmnopqrst",
			maxLen: 11,
			want:   "abcdefghij",
		},
		{
			name:   "empty input",
			input:  "",
			maxLen: 50,
			want:   "",
		},
		{
			name:   "only special characters",
			input:  "!@#$%^&*()",
			maxLen: 50,
			want:   "",
		},
		{
			name:   "mixed case normalized",
			input:  "Add OAuth2 Support",
			maxLen: 50,
			want:   "add-oauth2-support",
		},
		{
			name:   "consecutive hyphens collapsed",
			input:  "foo---bar---baz",
			maxLen: 50,
			want:   "foo-bar-baz",
		},
		{
			name:   "leading and trailing hyphens stripped",
			input:  " - hello world - ",
			maxLen: 50,
			want:   "hello-world",
		},
		{
			name:   "zero maxLen means no truncation",
			input:  "no truncation",
			maxLen: 0,
			want:   "no-truncation",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := slugify(tc.input, tc.maxLen)
			if got != tc.want {
				t.Errorf("slugify(%q, %d) = %q, want %q", tc.input, tc.maxLen, got, tc.want)
			}
		})
	}
}

func TestSlugify_NeverExceedsMaxLen(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a very long phrase ", 20)
	got := slugify(long, defaultMaxSlugLen)
	if len(got) > defaultMaxSlugLen {
		t.Errorf("slugify output length %d exceeds maxLen %d: %q", len(got), defaultMaxSlugLen, got)
	}
}

func TestAddNebulaGenerateFlags(t *testing.T) {
	t.Parallel()

	// Create a fresh command and register flags.
	cmd := &cobra.Command{Use: "test"}
	addNebulaGenerateFlags(cmd)

	tests := []struct {
		flag       string
		wantExists bool
	}{
		{"name", true},
		{"output", true},
		{"model", true},
		{"budget", true},
		{"force", true},
		{"dry-run", true},
	}

	for _, tc := range tests {
		t.Run(tc.flag, func(t *testing.T) {
			f := cmd.Flags().Lookup(tc.flag)
			if (f != nil) != tc.wantExists {
				t.Errorf("flag %q: exists=%v, want exists=%v", tc.flag, f != nil, tc.wantExists)
			}
		})
	}
}

func TestAddNebulaGenerateFlags_Defaults(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "test"}
	addNebulaGenerateFlags(cmd)

	// Verify budget default.
	budgetFlag := cmd.Flags().Lookup("budget")
	if budgetFlag == nil {
		t.Fatal("budget flag not registered")
	}
	if budgetFlag.DefValue != "10" {
		t.Errorf("budget default = %q, want %q", budgetFlag.DefValue, "10")
	}

	// Verify force default.
	forceFlag := cmd.Flags().Lookup("force")
	if forceFlag == nil {
		t.Fatal("force flag not registered")
	}
	if forceFlag.DefValue != "false" {
		t.Errorf("force default = %q, want %q", forceFlag.DefValue, "false")
	}

	// Verify dry-run default.
	dryRunFlag := cmd.Flags().Lookup("dry-run")
	if dryRunFlag == nil {
		t.Fatal("dry-run flag not registered")
	}
	if dryRunFlag.DefValue != "false" {
		t.Errorf("dry-run default = %q, want %q", dryRunFlag.DefValue, "false")
	}
}

func TestNebulaGenerateRegistered(t *testing.T) {
	t.Parallel()

	// Verify the generate subcommand is registered under nebulaCmd.
	found := false
	for _, sub := range nebulaCmd.Commands() {
		if sub.Name() == "generate" {
			found = true
			break
		}
	}
	if !found {
		t.Error("generate subcommand not registered under nebulaCmd")
	}
}
