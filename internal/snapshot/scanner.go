// Package snapshot produces deterministic project snapshots for prompt injection.
// The snapshot is a markdown document describing the project identity, directory
// structure, and coding conventions — designed for stable prompt caching.
package snapshot

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// DefaultMaxSize is the default maximum snapshot size in bytes (~8K tokens).
const DefaultMaxSize = 32000

// DefaultMaxDepth is the default maximum directory tree depth.
const DefaultMaxDepth = 3

// structureBudgetFraction is the fraction of MaxSize allocated to the tree.
const structureBudgetFraction = 0.40

// conventionsBudgetFraction is the fraction of MaxSize allocated to conventions.
const conventionsBudgetFraction = 0.60

// Scanner produces a deterministic project snapshot for prompt injection.
type Scanner struct {
	MaxSize  int    // max snapshot size in bytes (default 32000 ≈ 8K tokens)
	MaxDepth int    // max directory tree depth (default 3)
	WorkDir  string // repo root
}

// projectInfo holds detected project identity.
type projectInfo struct {
	Module   string
	Language string
}

// Scan produces a deterministic markdown snapshot of the project.
// The same repo state always produces identical output byte-for-byte.
func (s *Scanner) Scan(ctx context.Context) (string, error) {
	s.applyDefaults()

	var b strings.Builder

	// 1. Project header.
	info := s.detectProject()
	header := formatHeader(info)
	b.WriteString(header)

	// 2. Structure section.
	structureBudget := int(float64(s.MaxSize) * structureBudgetFraction)
	files, err := s.listFiles(ctx)
	if err != nil {
		return "", fmt.Errorf("listing files: %w", err)
	}
	tree := buildTree(files)
	treeStr := renderTree(tree, s.MaxDepth, structureBudget)

	b.WriteString("\n## Structure\n```\n")
	b.WriteString(treeStr)
	b.WriteString("```\n")

	// 3. Conventions section (CLAUDE.md or variants).
	conventionsBudget := int(float64(s.MaxSize) * conventionsBudgetFraction)
	// Account for bytes already written plus section header overhead.
	overhead := len("\n## Conventions\n")
	remaining := s.MaxSize - b.Len() - overhead
	if remaining < conventionsBudget {
		conventionsBudget = remaining
	}
	conventions := s.readConventions(conventionsBudget)
	if conventions != "" {
		b.WriteString("\n## Conventions\n")
		b.WriteString(conventions)
		if !strings.HasSuffix(conventions, "\n") {
			b.WriteString("\n")
		}
	}

	result := b.String()
	if len(result) > s.MaxSize {
		result = truncateUTF8(result, s.MaxSize)
	}
	return result, nil
}

// truncateUTF8 truncates s to at most maxBytes without splitting a multi-byte
// UTF-8 character. It walks backwards from the cut point to find a valid rune
// boundary.
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	// Walk backwards from the cut point to find a valid rune start.
	for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
}

// applyDefaults fills zero-valued fields with sensible defaults.
func (s *Scanner) applyDefaults() {
	if s.MaxSize <= 0 {
		s.MaxSize = DefaultMaxSize
	}
	if s.MaxDepth <= 0 {
		s.MaxDepth = DefaultMaxDepth
	}
	if s.WorkDir == "" {
		s.WorkDir = "."
	}
}

// formatHeader builds the ## Project section.
func formatHeader(info projectInfo) string {
	var b strings.Builder
	b.WriteString("## Project\n")
	if info.Module != "" {
		fmt.Fprintf(&b, "- Module: %s\n", info.Module)
	}
	if info.Language != "" {
		fmt.Fprintf(&b, "- Language: %s\n", info.Language)
	}
	return b.String()
}

// detectProject checks for common project manifest files and extracts identity.
func (s *Scanner) detectProject() projectInfo {
	detectors := []struct {
		file   string
		detect func(string) projectInfo
	}{
		{"go.mod", detectGo},
		{"package.json", detectNode},
		{"Cargo.toml", detectRust},
		{"pyproject.toml", detectPython},
		{"setup.py", detectSetupPy},
	}

	for _, d := range detectors {
		path := filepath.Join(s.WorkDir, d.file)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		return d.detect(string(data))
	}
	return projectInfo{}
}

// detectGo extracts module path from go.mod.
func detectGo(content string) projectInfo {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			mod := strings.TrimPrefix(line, "module ")
			return projectInfo{Module: strings.TrimSpace(mod), Language: "Go"}
		}
	}
	return projectInfo{Language: "Go"}
}

// detectNode extracts name from package.json.
func detectNode(content string) projectInfo {
	// Simple extraction without JSON dependency.
	// Search for "name" key anywhere in each line (handles minified JSON too).
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		idx := strings.Index(line, `"name"`)
		if idx < 0 {
			continue
		}
		// Find the colon after "name".
		rest := line[idx+len(`"name"`):]
		colonIdx := strings.Index(rest, ":")
		if colonIdx < 0 {
			continue
		}
		val := strings.TrimSpace(rest[colonIdx+1:])
		val = strings.TrimLeft(val, `"`)
		val = strings.TrimRight(val, `",} `)
		if val != "" {
			return projectInfo{Module: val, Language: "JavaScript/TypeScript"}
		}
	}
	return projectInfo{Language: "JavaScript/TypeScript"}
}

// detectRust extracts package name from Cargo.toml.
func detectRust(content string) projectInfo {
	inPackage := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "[package]" {
			inPackage = true
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			inPackage = false
			continue
		}
		if inPackage && strings.HasPrefix(trimmed, "name") {
			parts := strings.SplitN(trimmed, "=", 2)
			if len(parts) == 2 {
				name := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
				return projectInfo{Module: name, Language: "Rust"}
			}
		}
	}
	return projectInfo{Language: "Rust"}
}

// detectPython extracts project name from pyproject.toml.
func detectPython(content string) projectInfo {
	inProject := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "[project]" {
			inProject = true
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			inProject = false
			continue
		}
		if inProject && strings.HasPrefix(trimmed, "name") {
			parts := strings.SplitN(trimmed, "=", 2)
			if len(parts) == 2 {
				name := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
				return projectInfo{Module: name, Language: "Python"}
			}
		}
	}
	return projectInfo{Language: "Python"}
}

// detectSetupPy returns Python language when setup.py is present.
func detectSetupPy(_ string) projectInfo {
	return projectInfo{Language: "Python"}
}

// listFiles returns a sorted list of repo files. It uses git ls-files when
// available and falls back to os.ReadDir walking.
func (s *Scanner) listFiles(ctx context.Context) ([]string, error) {
	files, err := s.gitListFiles(ctx)
	if err == nil {
		return files, nil
	}
	return s.walkFiles(ctx)
}

// gitListFiles runs git ls-files and returns sorted output lines.
func (s *Scanner) gitListFiles(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-files")
	cmd.Dir = s.WorkDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	return parseFileList(string(out)), nil
}

// walkFiles walks the directory tree using os.ReadDir as a git fallback.
// It checks ctx for cancellation periodically during the walk.
func (s *Scanner) walkFiles(ctx context.Context) ([]string, error) {
	var files []string
	err := filepath.Walk(s.WorkDir, func(path string, info os.FileInfo, err error) error {
		// Check for context cancellation on each entry.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return nil // skip unreadable entries
		}
		// Skip hidden directories.
		if info.IsDir() && strings.HasPrefix(info.Name(), ".") && path != s.WorkDir {
			return filepath.SkipDir
		}
		if info.IsDir() {
			return nil
		}
		// Skip hidden files.
		if strings.HasPrefix(info.Name(), ".") {
			return nil
		}
		rel, err := filepath.Rel(s.WorkDir, path)
		if err != nil {
			return nil
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking directory: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

// parseFileList splits git ls-files output into sorted file paths.
func parseFileList(output string) []string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var files []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	sort.Strings(files)
	return files
}

// conventionsFiles lists candidate filenames for coding conventions, in priority order.
var conventionsFiles = []string{
	"CLAUDE.md",
	".claude.md",
	"claude.md",
}

// truncationMarker is appended when conventions content exceeds the budget.
const truncationMarker = "\n[truncated]\n"

// readConventions reads the first matching conventions file, truncated to maxBytes.
// The truncation marker is included within the maxBytes budget, not on top of it.
func (s *Scanner) readConventions(maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	for _, name := range conventionsFiles {
		path := filepath.Join(s.WorkDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := string(data)
		if len(content) > maxBytes {
			// Reserve space for the marker within the budget.
			cutPoint := maxBytes - len(truncationMarker)
			if cutPoint < 0 {
				cutPoint = 0
			}
			content = content[:cutPoint] + truncationMarker
		}
		return content
	}
	return ""
}
