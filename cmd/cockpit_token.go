package cmd

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// cockpitTokenFilename is the file under the data dir that holds the cockpit
// bearer token. The cockpit server reads it; operators distribute it to the UI.
const cockpitTokenFilename = "cockpit-token"

// cockpitTokenCmd implements `quasar cockpit token`: (re)generate the bearer
// token the cockpit HTTP API authenticates against.
var cockpitTokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Generate a new cockpit bearer token",
	Long: `Generate a new 32-byte random bearer token, write it to
<data-dir>/cockpit-token with 0600 permissions, and print its path. Any
previously issued token is overwritten and immediately invalidated.`,
	RunE: runCockpitToken,
}

func init() {
	cockpitCmd.AddCommand(cockpitTokenCmd)
}

// cockpitTokenPath returns the absolute path to the cockpit token file, which
// lives alongside the fabric database in the data directory.
func cockpitTokenPath() string {
	return filepath.Join(filepath.Dir(fabricDBPath()), cockpitTokenFilename)
}

// runCockpitToken generates 32 random bytes, hex-encodes them, and writes the
// result to the token file with owner-only permissions.
func runCockpitToken(cmd *cobra.Command, _ []string) error {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(buf[:])

	path := cockpitTokenPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return fmt.Errorf("write token file %s: %w", path, err)
	}
	// Re-assert 0600 in case the file pre-existed with looser permissions
	// (WriteFile does not chmod an existing file).
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod token file %s: %w", path, err)
	}

	fmt.Fprintf(os.Stderr, "wrote cockpit token to %s\n", path)
	return nil
}

// readCockpitToken reads and trims the cockpit token from disk. It returns an
// error if the file is missing so the server fails closed rather than starting
// with an empty (reject-all) token.
func readCockpitToken() (string, error) {
	path := cockpitTokenPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read cockpit token %s (run `quasar cockpit token`): %w", path, err)
	}
	token := string(data)
	for len(token) > 0 && (token[len(token)-1] == '\n' || token[len(token)-1] == '\r') {
		token = token[:len(token)-1]
	}
	if token == "" {
		return "", fmt.Errorf("cockpit token file %s is empty", path)
	}
	return token, nil
}
