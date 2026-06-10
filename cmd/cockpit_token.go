package cmd

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// cockpitTokenCmd generates a 32-byte hex bearer token and writes it to
// ~/.quasar/cockpit-token with mode 0600, then prints the path.
var cockpitTokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Generate a bearer token for the cockpit web server",
	Long: `Generate a cryptographically random 32-byte token, persist it to
~/.quasar/cockpit-token (mode 0600), and print the path.

Pass the token to 'quasar cockpit' via --token or QUASAR_COCKPIT_TOKEN so the
web server can validate browser sessions.`,
	Args: cobra.NoArgs,
	RunE: runCockpitToken,
}

func init() {
	cockpitCmd.AddCommand(cockpitTokenCmd)
}

// runCockpitToken generates a fresh token and writes it to the user data dir.
func runCockpitToken(_ *cobra.Command, _ []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}

	dataDir := filepath.Join(home, ".quasar")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir %s: %w", dataDir, err)
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Errorf("generate random token: %w", err)
	}
	tok := []byte(hex.EncodeToString(raw))

	path := filepath.Join(dataDir, "cockpit-token")
	if err := os.WriteFile(path, tok, 0o600); err != nil {
		return fmt.Errorf("write token to %s: %w", path, err)
	}

	// The token path is machine-readable output a caller can capture (e.g.
	// `tok=$(quasar cockpit token)`), like `version`.
	// arch-test: stdout-allowed
	fmt.Println(path)
	return nil
}
