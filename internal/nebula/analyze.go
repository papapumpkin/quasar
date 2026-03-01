package nebula

import (
	"bufio"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/papapumpkin/quasar/internal/snapshot"
)

// CodebaseAnalysis holds structured codebase context for the architect agent.
// It combines a raw project snapshot with package-level metadata to give
// the architect LLM enough context to generate accurate, well-scoped phases.
type CodebaseAnalysis struct {
	ProjectSnapshot string           // Raw snapshot from snapshot.Scanner
	Packages        []PackageSummary // Top-level package summaries
	ModulePath      string           // Go module path (e.g., "github.com/papapumpkin/quasar")
	maxOutputSize   int              // Budget for total FormatForPrompt output size
}

// PackageSummary describes a single Go package and its key exports.
type PackageSummary struct {
	ImportPath      string   // Full import path
	RelativePath    string   // Path relative to module root
	GoFiles         []string // .go filenames (excluding test files)
	ExportedSymbols []string // Exported type/func/var names (top-level only)
}

// AnalyzeCodebase runs the snapshot scanner and enriches the result with
// package-level metadata. The analysis is used to provide codebase context
// to the multi-phase architect agent.
func AnalyzeCodebase(ctx context.Context, workDir string, maxSnapshotSize int) (*CodebaseAnalysis, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// 1. Run snapshot scanner for the raw project overview.
	scanner := &snapshot.Scanner{
		WorkDir: workDir,
		MaxSize: maxSnapshotSize,
	}
	snap, err := scanner.Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("scanning project: %w", err)
	}

	// 2. Detect the Go module path from go.mod.
	modulePath, err := detectModulePath(workDir)
	if err != nil {
		// Non-Go projects won't have go.mod; treat as non-fatal.
		modulePath = ""
	}

	// 3. Discover Go packages and their exported symbols.
	packages, err := discoverPackages(ctx, workDir, modulePath)
	if err != nil {
		return nil, fmt.Errorf("discovering packages: %w", err)
	}

	return &CodebaseAnalysis{
		ProjectSnapshot: snap,
		Packages:        packages,
		ModulePath:      modulePath,
		maxOutputSize:   maxSnapshotSize,
	}, nil
}

// FormatForPrompt renders the analysis as a markdown string suitable for
// embedding in an architect agent prompt. The output contains sections for
// the project snapshot, module info, and package summaries. The total output
// is capped to the maxOutputSize budget set during analysis. When the budget
// is approached, remaining packages are omitted with a truncation notice.
func (a *CodebaseAnalysis) FormatForPrompt() string {
	var b strings.Builder

	b.WriteString("## Project Snapshot\n\n")
	b.WriteString(a.ProjectSnapshot)
	if !strings.HasSuffix(a.ProjectSnapshot, "\n") {
		b.WriteString("\n")
	}

	b.WriteString("\n## Module\n\n")
	if a.ModulePath != "" {
		fmt.Fprintf(&b, "- Path: `%s`\n", a.ModulePath)
	} else {
		b.WriteString("- Path: (not detected)\n")
	}

	b.WriteString("\n## Packages\n\n")
	if len(a.Packages) == 0 {
		b.WriteString("No Go packages detected.\n")
	} else {
		// Reserve space for a potential truncation notice.
		const truncNotice = "\n... (remaining packages omitted to stay within budget)\n"
		budget := a.maxOutputSize
		for i, pkg := range a.Packages {
			var entry strings.Builder
			fmt.Fprintf(&entry, "### `%s`\n", pkg.RelativePath)
			if len(pkg.GoFiles) > 0 {
				fmt.Fprintf(&entry, "- Files: %s\n", strings.Join(pkg.GoFiles, ", "))
			}
			if len(pkg.ExportedSymbols) > 0 {
				fmt.Fprintf(&entry, "- Exports: %s\n", strings.Join(pkg.ExportedSymbols, ", "))
			}
			entry.WriteString("\n")

			// If a budget is set, check whether adding this entry would exceed it.
			if budget > 0 {
				needed := b.Len() + entry.Len()
				if i < len(a.Packages)-1 {
					needed += len(truncNotice)
				}
				if needed > budget {
					b.WriteString(truncNotice)
					break
				}
			}

			b.WriteString(entry.String())
		}
	}

	return b.String()
}

// detectModulePath reads the go.mod file in workDir and extracts the module path.
func detectModulePath(workDir string) (string, error) {
	f, err := os.Open(filepath.Join(workDir, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("opening go.mod: %w", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("reading go.mod: %w", err)
	}
	return "", fmt.Errorf("no module directive found in go.mod")
}

// discoverPackages walks workDir to find directories containing .go files
// and extracts exported symbol names from each package.
func discoverPackages(ctx context.Context, workDir string, modulePath string) ([]PackageSummary, error) {
	var packages []PackageSummary

	err := filepath.Walk(workDir, func(path string, info os.FileInfo, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return nil // skip unreadable entries
		}
		if !info.IsDir() {
			return nil
		}
		// Skip hidden directories and common non-code dirs.
		name := info.Name()
		if strings.HasPrefix(name, ".") && path != workDir {
			return filepath.SkipDir
		}
		if name == "vendor" || name == "node_modules" || name == "testdata" {
			return filepath.SkipDir
		}

		// Check for .go files (excluding tests).
		goFiles, exports := scanGoDir(path)
		if len(goFiles) == 0 {
			return nil
		}

		relPath, err := filepath.Rel(workDir, path)
		if err != nil {
			return nil
		}
		relPath = filepath.ToSlash(relPath)
		if relPath == "." {
			relPath = "."
		}

		importPath := relPath
		if modulePath != "" && relPath != "." {
			importPath = modulePath + "/" + relPath
		} else if modulePath != "" {
			importPath = modulePath
		}

		packages = append(packages, PackageSummary{
			ImportPath:      importPath,
			RelativePath:    relPath,
			GoFiles:         goFiles,
			ExportedSymbols: exports,
		})

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking directory: %w", err)
	}

	sort.Slice(packages, func(i, j int) bool {
		return packages[i].RelativePath < packages[j].RelativePath
	})

	return packages, nil
}

// scanGoDir reads a directory for .go files (excluding test files) and extracts
// top-level exported symbol names using go/parser.
func scanGoDir(dir string) (goFiles []string, exports []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}

	fset := token.NewFileSet()
	exportSet := make(map[string]struct{})

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		goFiles = append(goFiles, name)

		// Parse only top-level declarations — no full AST needed.
		filePath := filepath.Join(dir, name)
		f, err := parser.ParseFile(fset, filePath, nil, 0)
		if err != nil {
			continue // skip unparseable files
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							exportSet[s.Name.Name] = struct{}{}
						}
					case *ast.ValueSpec:
						for _, n := range s.Names {
							if n.IsExported() {
								exportSet[n.Name] = struct{}{}
							}
						}
					}
				}
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name.IsExported() {
					exportSet[d.Name.Name] = struct{}{}
				}
			}
		}
	}

	sort.Strings(goFiles)

	for name := range exportSet {
		exports = append(exports, name)
	}
	sort.Strings(exports)

	return goFiles, exports
}
