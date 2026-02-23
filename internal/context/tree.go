// Package context provides a project context scanner that generates deterministic
// snapshots of repository structure and conventions for prompt caching.
package context

import (
	"sort"
	"strings"
)

// dirNode represents a node in a directory tree.
type dirNode struct {
	name     string
	children map[string]*dirNode
	isFile   bool
}

// newDirNode creates a new directory node.
func newDirNode(name string) *dirNode {
	return &dirNode{
		name:     name,
		children: make(map[string]*dirNode),
	}
}

// buildTree constructs a directory tree from a sorted list of file paths.
// Paths should use forward slashes as separators.
func buildTree(paths []string) *dirNode {
	root := newDirNode("")
	for _, p := range paths {
		parts := strings.Split(p, "/")
		cur := root
		for i, part := range parts {
			if part == "" {
				continue
			}
			child, ok := cur.children[part]
			if !ok {
				child = newDirNode(part)
				cur.children[part] = child
			}
			if i == len(parts)-1 {
				child.isFile = true
			}
			cur = child
		}
	}
	return root
}

// renderTree writes the directory tree as indented text into a strings.Builder.
// maxDepth limits how deep to render (0 = root's children only). A negative
// maxDepth means unlimited depth.
func renderTree(b *strings.Builder, node *dirNode, indent string, depth, maxDepth int) {
	if maxDepth >= 0 && depth > maxDepth {
		return
	}

	names := sortedChildren(node)
	// Separate dirs and files for cleaner output.
	var dirs, files []string
	for _, name := range names {
		child := node.children[name]
		if child.isFile && len(child.children) == 0 {
			files = append(files, name)
		} else {
			dirs = append(dirs, name)
		}
	}

	// Render directories first, then files.
	for _, name := range dirs {
		child := node.children[name]
		b.WriteString(indent)
		b.WriteString(name)
		b.WriteString("/\n")
		renderTree(b, child, indent+"  ", depth+1, maxDepth)
	}
	for _, name := range files {
		b.WriteString(indent)
		b.WriteString(name)
		b.WriteString("\n")
	}
}

// sortedChildren returns the child names in sorted order for deterministic output.
func sortedChildren(node *dirNode) []string {
	names := make([]string, 0, len(node.children))
	for name := range node.children {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// countEntries counts total nodes (dirs + files) in the tree up to maxDepth.
func countEntries(node *dirNode, depth, maxDepth int) int {
	if maxDepth >= 0 && depth > maxDepth {
		return 0
	}
	count := 0
	for _, child := range node.children {
		count++
		count += countEntries(child, depth+1, maxDepth)
	}
	return count
}

// projectManifests maps manifest filenames to their language/ecosystem.
var projectManifests = map[string]string{
	"go.mod":         "Go",
	"package.json":   "JavaScript/TypeScript",
	"Cargo.toml":     "Rust",
	"pyproject.toml": "Python",
	"setup.py":       "Python",
	"Gemfile":        "Ruby",
	"pom.xml":        "Java",
	"build.gradle":   "Java/Kotlin",
	"mix.exs":        "Elixir",
	"composer.json":  "PHP",
	"CMakeLists.txt": "C/C++",
	"Makefile":       "Make",
}

// detectProject scans the root entries for known manifest files and returns
// the detected language and module name if found.
func detectProject(root *dirNode, fileContents map[string]string) (language, module string) {
	// Check manifests in a deterministic order.
	manifests := make([]string, 0, len(projectManifests))
	for m := range projectManifests {
		manifests = append(manifests, m)
	}
	sort.Strings(manifests)

	for _, manifest := range manifests {
		if _, ok := root.children[manifest]; ok {
			language = projectManifests[manifest]
			// Try to extract module name from file content.
			if content, ok := fileContents[manifest]; ok {
				module = extractModuleName(manifest, content)
			}
			return language, module
		}
	}
	return "", ""
}

// extractModuleName extracts the module/package name from manifest content.
func extractModuleName(manifest, content string) string {
	switch manifest {
	case "go.mod":
		return extractGoModule(content)
	case "package.json":
		return extractJSONField(content, "name")
	case "Cargo.toml":
		return extractTOMLField(content, "name")
	case "pyproject.toml":
		return extractTOMLField(content, "name")
	default:
		return ""
	}
}

// extractGoModule extracts the module path from go.mod content.
func extractGoModule(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

// extractJSONField extracts a top-level string field from JSON content.
// This is a simple parser to avoid importing encoding/json for a single field.
func extractJSONField(content, field string) string {
	// Look for "field": "value"
	needle := `"` + field + `"`
	idx := strings.Index(content, needle)
	if idx < 0 {
		return ""
	}
	rest := content[idx+len(needle):]
	// Skip whitespace and colon.
	rest = strings.TrimLeft(rest, " \t\n\r:")
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	rest = rest[1:]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// extractTOMLField extracts a top-level key = "value" from TOML content.
func extractTOMLField(content, field string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, field) {
			rest := strings.TrimPrefix(line, field)
			rest = strings.TrimLeft(rest, " \t")
			if len(rest) == 0 || rest[0] != '=' {
				continue
			}
			rest = rest[1:]
			rest = strings.TrimLeft(rest, " \t")
			rest = strings.Trim(rest, `"'`)
			return rest
		}
	}
	return ""
}
