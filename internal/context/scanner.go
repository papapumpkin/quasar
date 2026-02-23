package context

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultMaxChars is the default maximum character count for a snapshot.
// ~8K tokens ≈ ~32K characters.
const DefaultMaxChars = 32000

// DefaultTreeDepth is the default maximum depth for the directory tree.
const DefaultTreeDepth = 3

// conventionFiles lists filenames to look for project conventions, in priority order.
var conventionFiles = []string{
	"CLAUDE.md",
	"AGENTS.md",
	"CONTRIBUTING.md",
}

// Scanner generates deterministic project context snapshots.
type Scanner struct {
	// MaxChars is the maximum snapshot size in characters. Zero uses DefaultMaxChars.
	MaxChars int

	// TreeDepth is the maximum directory tree depth. Zero uses DefaultTreeDepth.
	TreeDepth int
}

// Scan produces a deterministic project context snapshot for the given directory.
// It uses git ls-files to enumerate tracked files (respecting .gitignore),
// detects the project language from manifest files, and includes conventions
// from CLAUDE.md or similar files if present.
func (s *Scanner) Scan(ctx context.Context, workDir string) (string, error) {
	maxChars := s.MaxChars
	if maxChars <= 0 {
		maxChars = DefaultMaxChars
	}
	treeDepth := s.TreeDepth
	if treeDepth <= 0 {
		treeDepth = DefaultTreeDepth
	}

	// Get tracked files via git ls-files.
	files, err := gitLsFiles(ctx, workDir)
	if err != nil {
		return "", fmt.Errorf("listing repo files: %w", err)
	}

	// Build directory tree.
	root := buildTree(files)

	// Read manifest files for project detection.
	fileContents := readManifests(workDir, root)

	// Detect project identity.
	language, module := detectProject(root, fileContents)

	// Read convention files.
	conventions := readConventionFile(workDir)

	// Build the snapshot.
	var b strings.Builder
	b.WriteString("# Project Context\n\n")

	// Project section.
	writeProjectSection(&b, language, module, workDir)

	// Structure section.
	writeTreeSection(&b, root, treeDepth)

	// Conventions section.
	if conventions != "" {
		writeConventionsSection(&b, conventions)
	}

	snapshot := b.String()

	// Truncate if needed.
	if len(snapshot) > maxChars {
		snapshot = truncate(snapshot, maxChars)
	}

	return snapshot, nil
}

// gitLsFiles runs git ls-files in the given directory and returns a sorted list
// of tracked file paths.
func gitLsFiles(ctx context.Context, dir string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-files")
	cmd.Dir = dir

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var files []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	sort.Strings(files)
	return files, nil
}

// readManifests reads the content of known manifest files present in the root.
func readManifests(workDir string, root *dirNode) map[string]string {
	contents := make(map[string]string)
	for manifest := range projectManifests {
		if _, ok := root.children[manifest]; ok {
			data, err := os.ReadFile(filepath.Join(workDir, manifest))
			if err == nil {
				contents[manifest] = string(data)
			}
		}
	}
	return contents
}

// readConventionFile reads the first convention file found in the work directory.
func readConventionFile(workDir string) string {
	for _, name := range conventionFiles {
		data, err := os.ReadFile(filepath.Join(workDir, name))
		if err == nil {
			return string(data)
		}
	}
	return ""
}

// writeProjectSection writes the ## Project section.
func writeProjectSection(b *strings.Builder, language, module, workDir string) {
	b.WriteString("## Project\n\n")
	if module != "" {
		b.WriteString("- **Module**: ")
		b.WriteString(module)
		b.WriteString("\n")
	}
	if language != "" {
		b.WriteString("- **Language**: ")
		b.WriteString(language)
		b.WriteString("\n")
	}
	if module == "" && language == "" {
		b.WriteString("- **Directory**: ")
		b.WriteString(filepath.Base(workDir))
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

// writeTreeSection writes the ## Structure section with the directory tree.
func writeTreeSection(b *strings.Builder, root *dirNode, depth int) {
	b.WriteString("## Structure\n\n```\n")
	renderTree(b, root, "", 0, depth)
	b.WriteString("```\n\n")
}

// writeConventionsSection writes the ## Conventions section.
func writeConventionsSection(b *strings.Builder, content string) {
	b.WriteString("## Conventions\n\n")
	b.WriteString(strings.TrimSpace(content))
	b.WriteString("\n\n")
}

// truncate cuts the snapshot at maxChars, ending at a clean line boundary
// and appending a truncation marker.
func truncate(s string, maxChars int) string {
	const marker = "\n\n[... truncated for size ...]\n"
	limit := maxChars - len(marker)
	if limit <= 0 {
		return marker
	}

	// Find the last newline before the limit.
	cut := strings.LastIndex(s[:limit], "\n")
	if cut <= 0 {
		cut = limit
	}

	return s[:cut] + marker
}
