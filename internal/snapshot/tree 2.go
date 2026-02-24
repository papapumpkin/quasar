package snapshot

import (
	"fmt"
	"sort"
	"strings"
)

// dirNode represents a directory in the tree hierarchy.
type dirNode struct {
	name     string
	children []*dirNode
	files    []string
}

// buildTree converts a sorted flat file list into a tree structure.
// Files must be forward-slash separated paths.
func buildTree(files []string) *dirNode {
	root := &dirNode{name: "."}
	for _, f := range files {
		parts := strings.Split(f, "/")
		insertPath(root, parts)
	}
	return root
}

// insertPath inserts a file path (split into parts) into the tree rooted at node.
func insertPath(node *dirNode, parts []string) {
	if len(parts) == 1 {
		node.files = append(node.files, parts[0])
		return
	}
	dirName := parts[0]
	for _, child := range node.children {
		if child.name == dirName {
			insertPath(child, parts[1:])
			return
		}
	}
	child := &dirNode{name: dirName}
	node.children = append(node.children, child)
	insertPath(child, parts[1:])
}

// collapseThreshold is the maximum number of entries (files + dirs) before
// a directory is collapsed to a summary line.
const collapseThreshold = 20

// renderTree renders the tree as an indented text block.
// maxDepth limits how deep the tree is rendered (0 = root's children only).
// maxBytes caps the output size; rendering stops when exceeded.
func renderTree(root *dirNode, maxDepth, maxBytes int) string {
	var b strings.Builder
	renderNode(&b, root, 0, maxDepth, maxBytes)
	return b.String()
}

// renderNode recursively renders a single node and its children.
func renderNode(b *strings.Builder, node *dirNode, depth, maxDepth, maxBytes int) {
	// Sort children and files for determinism.
	sort.Slice(node.children, func(i, j int) bool {
		return node.children[i].name < node.children[j].name
	})
	sort.Strings(node.files)

	if depth > maxDepth {
		return
	}

	totalEntries := len(node.children) + len(node.files)
	indent := strings.Repeat("  ", depth)

	// If this non-root directory has too many entries, collapse it.
	if depth > 0 && totalEntries > collapseThreshold {
		line := fmt.Sprintf("%s%s/ (%d entries)\n", indent, node.name, totalEntries)
		if b.Len()+len(line) > maxBytes {
			return
		}
		b.WriteString(line)
		return
	}

	// Render child directories first, then files.
	for _, child := range node.children {
		dirLine := fmt.Sprintf("%s%s/\n", indent, child.name)
		if b.Len()+len(dirLine) > maxBytes {
			return
		}
		b.WriteString(dirLine)
		renderNode(b, child, depth+1, maxDepth, maxBytes)
	}
	for _, f := range node.files {
		fileLine := fmt.Sprintf("%s%s\n", indent, f)
		if b.Len()+len(fileLine) > maxBytes {
			return
		}
		b.WriteString(fileLine)
	}
}
