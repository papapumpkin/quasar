package agentmail

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateMCPConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path, err := GenerateMCPConfig(dir, 8391)
	if err != nil {
		t.Fatalf("GenerateMCPConfig: %v", err)
	}

	if filepath.Dir(path) != dir {
		t.Errorf("config path %q not in expected dir %q", path, dir)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading generated config: %v", err)
	}

	var cfg mcpConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshalling config: %v", err)
	}

	entry, ok := cfg.MCPServers["agentmail"]
	if !ok {
		t.Fatal("expected 'agentmail' key in mcpServers")
	}

	want := "http://localhost:8391/sse"
	if entry.URL != want {
		t.Errorf("url = %q, want %q", entry.URL, want)
	}
}

func TestGenerateMCPConfig_CustomPort(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path, err := GenerateMCPConfig(dir, 9999)
	if err != nil {
		t.Fatalf("GenerateMCPConfig: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading generated config: %v", err)
	}

	if !strings.Contains(string(data), "http://localhost:9999/sse") {
		t.Errorf("expected port 9999 in config, got: %s", string(data))
	}
}

func TestGenerateMCPConfig_InvalidDir(t *testing.T) {
	t.Parallel()

	_, err := GenerateMCPConfig("/nonexistent/path/to/dir", 8391)
	if err == nil {
		t.Fatal("expected error for invalid directory")
	}
}
