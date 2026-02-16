package agentmail

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestClaimAndConflict_FileClaim(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	srv := NewServer(store, 0, nil)
	cs := mcpClientSession(t, srv)

	agentA, err := store.RegisterAgent(ctx, "agent-a", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent A: %v", err)
	}
	agentB, err := store.RegisterAgent(ctx, "agent-b", "reviewer")
	if err != nil {
		t.Fatalf("RegisterAgent B: %v", err)
	}

	// Agent A claims two files.
	result := callTool(t, cs, "claim_files", map[string]any{
		"agent_id": agentA,
		"files":    []any{"pkg/a.go", "pkg/b.go"},
	})
	if result.IsError {
		t.Fatalf("claim_files A returned error: %v", result.Content)
	}

	var claimOut claimFilesOutput
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	if err := json.Unmarshal(raw, &claimOut); err != nil {
		t.Fatalf("unmarshal claimFilesOutput: %v", err)
	}
	if len(claimOut.Claimed) != 2 {
		t.Errorf("expected 2 claimed, got %d", len(claimOut.Claimed))
	}
	if len(claimOut.Conflicts) != 0 {
		t.Errorf("expected 0 conflicts, got %d", len(claimOut.Conflicts))
	}

	// Agent B attempts the same files — should get conflicts.
	result = callTool(t, cs, "claim_files", map[string]any{
		"agent_id": agentB,
		"files":    []any{"pkg/a.go", "pkg/b.go"},
	})
	if result.IsError {
		t.Fatalf("claim_files B returned error: %v", result.Content)
	}

	raw, err = json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	if err = json.Unmarshal(raw, &claimOut); err != nil {
		t.Fatalf("unmarshal claimFilesOutput: %v", err)
	}
	if len(claimOut.Claimed) != 0 {
		t.Errorf("expected 0 claimed for agent B, got %d", len(claimOut.Claimed))
	}
	if len(claimOut.Conflicts) != 2 {
		t.Fatalf("expected 2 conflicts, got %d", len(claimOut.Conflicts))
	}
	for _, c := range claimOut.Conflicts {
		if c.HeldBy != agentA {
			t.Errorf("expected held_by %q, got %q", agentA, c.HeldBy)
		}
	}
}

func TestReleaseAndReclaim_FileClaim(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	srv := NewServer(store, 0, nil)
	cs := mcpClientSession(t, srv)

	agentA, err := store.RegisterAgent(ctx, "agent-a", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent A: %v", err)
	}
	agentB, err := store.RegisterAgent(ctx, "agent-b", "reviewer")
	if err != nil {
		t.Fatalf("RegisterAgent B: %v", err)
	}

	// Agent A claims a file.
	callTool(t, cs, "claim_files", map[string]any{
		"agent_id": agentA,
		"files":    []any{"shared.go"},
	})

	// Agent A releases the file.
	result := callTool(t, cs, "release_files", map[string]any{
		"agent_id": agentA,
		"files":    []any{"shared.go"},
	})
	if result.IsError {
		t.Fatalf("release_files returned error: %v", result.Content)
	}

	var releaseOut releaseFilesOutput
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	if err := json.Unmarshal(raw, &releaseOut); err != nil {
		t.Fatalf("unmarshal releaseFilesOutput: %v", err)
	}
	if len(releaseOut.Released) != 1 {
		t.Errorf("expected 1 released, got %d", len(releaseOut.Released))
	}

	// Agent B can now claim the file.
	result = callTool(t, cs, "claim_files", map[string]any{
		"agent_id": agentB,
		"files":    []any{"shared.go"},
	})
	if result.IsError {
		t.Fatalf("claim_files B returned error: %v", result.Content)
	}

	var claimOut claimFilesOutput
	raw, err = json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	if err := json.Unmarshal(raw, &claimOut); err != nil {
		t.Fatalf("unmarshal claimFilesOutput: %v", err)
	}
	if len(claimOut.Claimed) != 1 {
		t.Errorf("expected 1 claimed by B after release, got %d", len(claimOut.Claimed))
	}
	if len(claimOut.Conflicts) != 0 {
		t.Errorf("expected 0 conflicts after release, got %d", len(claimOut.Conflicts))
	}
}

func TestGetFileClaims_State(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	srv := NewServer(store, 0, nil)
	cs := mcpClientSession(t, srv)

	agentA, err := store.RegisterAgent(ctx, "state-agent", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	// Claim files.
	callTool(t, cs, "claim_files", map[string]any{
		"agent_id": agentA,
		"files":    []any{"state/x.go", "state/y.go"},
	})

	// Get all claims.
	result := callTool(t, cs, "get_file_claims", map[string]any{})
	if result.IsError {
		t.Fatalf("get_file_claims returned error: %v", result.Content)
	}

	var getOut getFileClaimsOutput
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	if err := json.Unmarshal(raw, &getOut); err != nil {
		t.Fatalf("unmarshal getFileClaimsOutput: %v", err)
	}
	if len(getOut.Claims) < 2 {
		t.Errorf("expected at least 2 claims, got %d", len(getOut.Claims))
	}

	// Get specific file claims.
	result = callTool(t, cs, "get_file_claims", map[string]any{
		"files": []any{"state/x.go"},
	})
	raw, err = json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	if err := json.Unmarshal(raw, &getOut); err != nil {
		t.Fatalf("unmarshal getFileClaimsOutput: %v", err)
	}
	if len(getOut.Claims) != 1 {
		t.Errorf("expected 1 claim for state/x.go, got %d", len(getOut.Claims))
	}
	if len(getOut.Claims) > 0 {
		if getOut.Claims[0].AgentID != agentA {
			t.Errorf("agent_id = %q, want %q", getOut.Claims[0].AgentID, agentA)
		}
		if getOut.Claims[0].FilePath != "state/x.go" {
			t.Errorf("file_path = %q, want %q", getOut.Claims[0].FilePath, "state/x.go")
		}
		if getOut.Claims[0].ClaimedAt == "" {
			t.Error("expected non-empty claimed_at")
		}
	}
}

func TestPathNormalization_FileClaim(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	srv := NewServer(store, 0, nil)
	cs := mcpClientSession(t, srv)

	agentA, err := store.RegisterAgent(ctx, "norm-agent-a", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent A: %v", err)
	}
	agentB, err := store.RegisterAgent(ctx, "norm-agent-b", "reviewer")
	if err != nil {
		t.Fatalf("RegisterAgent B: %v", err)
	}

	// Agent A claims with "./" prefix.
	callTool(t, cs, "claim_files", map[string]any{
		"agent_id": agentA,
		"files":    []any{"./foo/bar.go"},
	})

	// Agent B attempts without "./" prefix — should conflict.
	result := callTool(t, cs, "claim_files", map[string]any{
		"agent_id": agentB,
		"files":    []any{"foo/bar.go"},
	})

	var claimOut claimFilesOutput
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	if err := json.Unmarshal(raw, &claimOut); err != nil {
		t.Fatalf("unmarshal claimFilesOutput: %v", err)
	}
	if len(claimOut.Claimed) != 0 {
		t.Errorf("expected 0 claimed (path should normalize), got %d", len(claimOut.Claimed))
	}
	if len(claimOut.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(claimOut.Conflicts))
	}
	if claimOut.Conflicts[0].File != "foo/bar.go" {
		t.Errorf("conflict file = %q, want %q", claimOut.Conflicts[0].File, "foo/bar.go")
	}
	if claimOut.Conflicts[0].HeldBy != agentA {
		t.Errorf("held_by = %q, want %q", claimOut.Conflicts[0].HeldBy, agentA)
	}
}

func TestClaimFiles_ValidationErrors(t *testing.T) {
	store := testStore(t)
	srv := NewServer(store, 0, nil)
	cs := mcpClientSession(t, srv)

	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "missing agent_id",
			args: map[string]any{"files": []any{"a.go"}},
			want: "agent_id is required",
		},
		{
			name: "missing files",
			args: map[string]any{"agent_id": "a"},
			want: "files is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := callTool(t, cs, "claim_files", tc.args)
			if !result.IsError {
				t.Fatal("expected IsError=true")
			}
			text := result.Content[0].(*mcp.TextContent).Text
			if !strings.Contains(text, tc.want) {
				t.Errorf("expected %q in error, got: %s", tc.want, text)
			}
		})
	}
}

func TestReleaseFiles_ValidationErrors(t *testing.T) {
	store := testStore(t)
	srv := NewServer(store, 0, nil)
	cs := mcpClientSession(t, srv)

	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "missing agent_id",
			args: map[string]any{"files": []any{"a.go"}},
			want: "agent_id is required",
		},
		{
			name: "missing files",
			args: map[string]any{"agent_id": "a"},
			want: "files is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := callTool(t, cs, "release_files", tc.args)
			if !result.IsError {
				t.Fatal("expected IsError=true")
			}
			text := result.Content[0].(*mcp.TextContent).Text
			if !strings.Contains(text, tc.want) {
				t.Errorf("expected %q in error, got: %s", tc.want, text)
			}
		})
	}
}

func TestNormalizePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"foo/bar.go", "foo/bar.go"},
		{"./foo/bar.go", "foo/bar.go"},
		{"foo//bar.go", "foo/bar.go"},
		{"foo\\bar.go", "foo/bar.go"},
		{"./foo/../foo/bar.go", "foo/bar.go"},
		{".", "."},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got := normalizePath(tc.input)
			if got != tc.want {
				t.Errorf("normalizePath(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
