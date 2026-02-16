package agentmail

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// mcpServerEntry represents a single MCP server in the config JSON.
type mcpServerEntry struct {
	URL string `json:"url"`
}

// mcpConfigFile represents the top-level MCP config JSON passed to claude --mcp-config.
type mcpConfigFile struct {
	MCPServers map[string]mcpServerEntry `json:"mcpServers"`
}

// GenerateMCPConfig writes a claude MCP config JSON file to the given directory
// and returns the path to the generated file.
func GenerateMCPConfig(dir string, port int) (string, error) {
	cfg := mcpConfigFile{
		MCPServers: map[string]mcpServerEntry{
			"agentmail": {
				URL: fmt.Sprintf("http://localhost:%d/sse", port),
			},
		},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal MCP config: %w", err)
	}

	path := filepath.Join(dir, "mcp-config.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("failed to write MCP config: %w", err)
	}

	return path, nil
}

// ProcessManager manages the lifecycle of the agentmail MCP server process.
type ProcessManager struct {
	BinaryPath string // path to the agentmail binary
	Port       int
	DoltDSN    string

	cmd *exec.Cmd
}

// Start launches the agentmail server process and waits for it to become healthy.
// It returns an error if the server fails to start or doesn't become healthy
// within the timeout.
func (s *ProcessManager) Start(ctx context.Context) error {
	s.cmd = exec.CommandContext(ctx, s.BinaryPath,
		"-port", fmt.Sprintf("%d", s.Port),
		"-dolt-dsn", s.DoltDSN,
	)
	s.cmd.Stdout = os.Stderr // server output goes to stderr
	s.cmd.Stderr = os.Stderr

	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start agentmail server: %w", err)
	}

	if err := s.waitHealthy(ctx); err != nil {
		// Kill the process if health check fails.
		_ = s.cmd.Process.Kill()
		return err
	}

	return nil
}

// Stop gracefully shuts down the agentmail server process.
func (s *ProcessManager) Stop() error {
	if s.cmd == nil || s.cmd.Process == nil {
		return nil
	}
	if err := s.cmd.Process.Signal(os.Interrupt); err != nil {
		// If interrupt fails, force kill.
		return s.cmd.Process.Kill()
	}

	// Wait briefly for graceful shutdown.
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()

	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		return s.cmd.Process.Kill()
	}
}

// waitHealthy polls the SSE endpoint until it responds or the context expires.
func (s *ProcessManager) waitHealthy(ctx context.Context) error {
	url := fmt.Sprintf("http://localhost:%d/sse", s.Port)
	deadline := time.After(10 * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("agentmail health check canceled: %w", ctx.Err())
		case <-deadline:
			return fmt.Errorf("agentmail server failed to become healthy at %s within 10s", url)
		case <-ticker.C:
			resp, err := http.Get(url)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
	}
}
