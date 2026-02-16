package agentmail

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// claimFilesInput is the input schema for the claim_files tool.
type claimFilesInput struct {
	AgentID string   `json:"agent_id" jsonschema:"description=Claiming agent's ID"`
	Files   []string `json:"files" jsonschema:"description=File paths to claim"`
}

// conflictEntry describes a file that could not be claimed because another
// agent already holds the lock.
type conflictEntry struct {
	File   string `json:"file"`
	HeldBy string `json:"held_by"`
}

// claimFilesOutput is the output schema for the claim_files tool.
type claimFilesOutput struct {
	Claimed   []string        `json:"claimed"`
	Conflicts []conflictEntry `json:"conflicts"`
}

// releaseFilesInput is the input schema for the release_files tool.
type releaseFilesInput struct {
	AgentID string   `json:"agent_id" jsonschema:"description=Releasing agent's ID"`
	Files   []string `json:"files" jsonschema:"description=File paths to release"`
}

// releaseFilesOutput is the output schema for the release_files tool.
type releaseFilesOutput struct {
	Released []string `json:"released"`
}

// getFileClaimsInput is the input schema for the get_file_claims tool.
type getFileClaimsInput struct {
	Files []string `json:"files,omitempty" jsonschema:"description=Specific files to check (all if omitted)"`
}

// fileClaimEntry is a single claim in the get_file_claims response.
type fileClaimEntry struct {
	FilePath  string `json:"file_path"`
	AgentID   string `json:"agent_id"`
	ClaimedAt string `json:"claimed_at"`
}

// getFileClaimsOutput is the output schema for the get_file_claims tool.
type getFileClaimsOutput struct {
	Claims []fileClaimEntry `json:"claims"`
}

// normalizePath strips a leading "./" prefix and cleans the path using
// forward-slash separators so that "./foo/bar.go" and "foo/bar.go" are
// treated identically.
func normalizePath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = path.Clean(p)
	p = strings.TrimPrefix(p, "./")
	return p
}

// normalizePaths applies normalizePath to every element and returns the
// cleaned slice.
func normalizePaths(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = normalizePath(p)
	}
	return out
}

// registerFileClaimTools registers the claim_files, release_files, and
// get_file_claims MCP tools.
func (s *Server) registerFileClaimTools() {
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "claim_files",
		Description: "Claim advisory locks on files",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input claimFilesInput) (*mcp.CallToolResult, claimFilesOutput, error) {
		if input.AgentID == "" {
			return nil, claimFilesOutput{}, fmt.Errorf("agent_id is required")
		}
		if len(input.Files) == 0 {
			return nil, claimFilesOutput{}, fmt.Errorf("files is required")
		}

		files := normalizePaths(input.Files)

		claimed, conflictPaths, err := s.store.ClaimFiles(ctx, input.AgentID, files)
		if err != nil {
			return nil, claimFilesOutput{}, fmt.Errorf("claiming files: %w", err)
		}

		// Enrich conflict paths with the holding agent's ID.
		var conflicts []conflictEntry
		if len(conflictPaths) > 0 {
			claims, err := s.store.GetFileClaims(ctx, conflictPaths)
			if err != nil {
				return nil, claimFilesOutput{}, fmt.Errorf("looking up conflict holders: %w", err)
			}
			holderMap := make(map[string]string, len(claims))
			for _, c := range claims {
				holderMap[c.FilePath] = c.AgentID
			}
			for _, f := range conflictPaths {
				conflicts = append(conflicts, conflictEntry{
					File:   f,
					HeldBy: holderMap[f],
				})
			}
		}

		return nil, claimFilesOutput{
			Claimed:   claimed,
			Conflicts: conflicts,
		}, nil
	})

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "release_files",
		Description: "Release advisory file locks",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input releaseFilesInput) (*mcp.CallToolResult, releaseFilesOutput, error) {
		if input.AgentID == "" {
			return nil, releaseFilesOutput{}, fmt.Errorf("agent_id is required")
		}
		if len(input.Files) == 0 {
			return nil, releaseFilesOutput{}, fmt.Errorf("files is required")
		}

		files := normalizePaths(input.Files)

		released, err := s.store.ReleaseFiles(ctx, input.AgentID, files)
		if err != nil {
			return nil, releaseFilesOutput{}, fmt.Errorf("releasing files: %w", err)
		}

		return nil, releaseFilesOutput{Released: released}, nil
	})

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "get_file_claims",
		Description: "Get current file claim status",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getFileClaimsInput) (*mcp.CallToolResult, getFileClaimsOutput, error) {
		var files []string
		if len(input.Files) > 0 {
			files = normalizePaths(input.Files)
		}

		claims, err := s.store.GetFileClaims(ctx, files)
		if err != nil {
			return nil, getFileClaimsOutput{}, fmt.Errorf("getting file claims: %w", err)
		}

		entries := make([]fileClaimEntry, len(claims))
		for i, c := range claims {
			entries[i] = fileClaimEntry{
				FilePath:  c.FilePath,
				AgentID:   c.AgentID,
				ClaimedAt: c.ClaimedAt.Format(time.RFC3339),
			}
		}

		return nil, getFileClaimsOutput{Claims: entries}, nil
	})
}
