// Package fabric — static.go provides pre-execution entanglement analysis.
//
// StaticScanner extracts expected entanglements from phase spec bodies
// by parsing structured sections (## Files, scope globs, and inline
// code references) without executing any phase.
package fabric

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// PhaseInput is the minimal phase information needed for static scanning.
// It mirrors the relevant fields of nebula.PhaseSpec without creating a
// circular import.
type PhaseInput struct {
	ID        string
	Body      string   // Markdown body after +++ frontmatter
	Scope     []string // Glob patterns for owned files/dirs
	DependsOn []string // Phase IDs this phase depends on
}

// PhaseContract represents the statically-derived inputs and outputs of a phase.
type PhaseContract struct {
	PhaseID  string
	Produces []Entanglement // what this phase is expected to create
	Consumes []Entanglement // what this phase expects to find
	Scope    []string       // resolved file paths from scope globs
}

// StaticScanner extracts expected entanglements from phase spec bodies
// by parsing structured sections (## Files, scope globs, and inline
// code references).
type StaticScanner struct {
	WorkDir string // for resolving scope globs against the filesystem
}

// Scan analyzes all phases and returns their predicted contracts.
// It combines three extraction strategies:
//   - Scope-based: resolve scope globs and parse matched Go files
//   - Files-section: parse ## Files markdown section for file paths
//   - Cross-reference: match producer outputs against consumer body text
func (s *StaticScanner) Scan(phases []PhaseInput) ([]PhaseContract, error) {
	contracts := make([]PhaseContract, 0, len(phases))

	for i := range phases {
		c, err := s.scanPhase(&phases[i])
		if err != nil {
			return nil, err
		}
		contracts = append(contracts, c)
	}

	// Cross-reference: for each phase, check if its body text mentions
	// symbols produced by its dependency phases.
	s.crossReference(contracts, phases)

	return contracts, nil
}

// scanPhase extracts produces and scope for a single phase.
func (s *StaticScanner) scanPhase(p *PhaseInput) (PhaseContract, error) {
	c := PhaseContract{PhaseID: p.ID}

	// Strategy 1: scope-based extraction.
	scopeFiles, err := s.resolveScope(p.Scope)
	if err != nil {
		return c, err
	}
	c.Scope = scopeFiles

	seen := make(map[string]bool)
	for _, path := range scopeFiles {
		syms := s.parseGoFile(path)
		for i := range syms {
			syms[i].Producer = p.ID
			syms[i].Status = StatusPending
			key := syms[i].Kind + ":" + syms[i].Name
			if !seen[key] {
				c.Produces = append(c.Produces, syms[i])
				seen[key] = true
			}
		}
	}

	// Strategy 2: ## Files section parsing.
	filePaths := parseFilesSection(p.Body)
	for _, fp := range filePaths {
		abs := filepath.Join(s.WorkDir, fp)
		if !strings.HasSuffix(fp, ".go") || strings.HasSuffix(fp, "_test.go") {
			// Non-Go files produce a file-level entanglement.
			key := KindFile + ":" + fp
			if !seen[key] {
				c.Produces = append(c.Produces, Entanglement{
					Producer: p.ID,
					Kind:     KindFile,
					Name:     fp,
					Package:  packageFromPath(fp),
					Status:   StatusPending,
				})
				seen[key] = true
			}
			continue
		}

		// For existing Go files, parse them.
		syms := s.parseGoFile(abs)
		for i := range syms {
			syms[i].Producer = p.ID
			syms[i].Status = StatusPending
			key := syms[i].Kind + ":" + syms[i].Name
			if !seen[key] {
				c.Produces = append(c.Produces, syms[i])
				seen[key] = true
			}
		}

		// If the Go file doesn't exist yet, try to extract type/function
		// names from inline code blocks in the body.
		if len(syms) == 0 {
			inlineSym := extractInlineSymbols(p.Body, p.ID, fp)
			for _, sym := range inlineSym {
				key := sym.Kind + ":" + sym.Name
				if !seen[key] {
					c.Produces = append(c.Produces, sym)
					seen[key] = true
				}
			}
		}
	}

	return c, nil
}

// resolveScope expands scope glob patterns against the filesystem,
// returning matching Go file paths (excluding test files).
func (s *StaticScanner) resolveScope(patterns []string) ([]string, error) {
	if len(patterns) == 0 {
		return nil, nil
	}

	var matched []string
	seen := make(map[string]bool)

	for _, pattern := range patterns {
		abs := filepath.Join(s.WorkDir, pattern)
		matches, err := filepath.Glob(abs)
		if err != nil {
			return nil, err
		}

		for _, m := range matches {
			info, statErr := os.Stat(m)
			if statErr != nil {
				continue
			}
			if info.IsDir() {
				dirFiles, walkErr := collectGoFiles(m)
				if walkErr != nil {
					return nil, walkErr
				}
				for _, f := range dirFiles {
					if !seen[f] {
						matched = append(matched, f)
						seen[f] = true
					}
				}
			} else if strings.HasSuffix(m, ".go") && !strings.HasSuffix(m, "_test.go") {
				if !seen[m] {
					matched = append(matched, m)
					seen[m] = true
				}
			}
		}
	}

	return matched, nil
}

// collectGoFiles walks a directory and returns all non-test .go file paths.
func collectGoFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// parseGoFile parses a Go source file and extracts exported symbols as
// entanglements. Returns nil on any error (file not found, parse error, etc.).
func (s *StaticScanner) parseGoFile(path string) []Entanglement {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil
	}

	pkg := f.Name.Name
	var entanglements []Entanglement

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			entanglements = append(entanglements, extractFuncDeclSymbols(d, pkg)...)
		case *ast.GenDecl:
			if d.Tok == token.TYPE {
				entanglements = append(entanglements, extractTypeSpecSymbols(d, pkg)...)
			}
		}
	}

	return entanglements
}

// extractFuncDeclSymbols extracts an entanglement from a function or method
// declaration. Only exported symbols are included. This is a standalone
// version of Publisher.extractFuncDecl for use without a Publisher instance.
func extractFuncDeclSymbols(d *ast.FuncDecl, pkg string) []Entanglement {
	if !d.Name.IsExported() {
		return nil
	}

	kind := KindFunction
	name := d.Name.Name
	sig := FormatFuncSignature(d)

	if d.Recv != nil && len(d.Recv.List) > 0 {
		kind = KindMethod
		recvType := FormatRecvType(d.Recv.List[0].Type)
		name = recvType + "." + d.Name.Name
	}

	return []Entanglement{{
		Kind:      kind,
		Name:      name,
		Signature: sig,
		Package:   pkg,
	}}
}

// extractTypeSpecSymbols extracts entanglements from type declarations in a
// GenDecl. This is a standalone version of Publisher.extractTypeSpecs.
func extractTypeSpecSymbols(d *ast.GenDecl, pkg string) []Entanglement {
	var entanglements []Entanglement
	for _, spec := range d.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok || !ts.Name.IsExported() {
			continue
		}

		iface, isIface := ts.Type.(*ast.InterfaceType)
		if isIface {
			entanglements = append(entanglements, Entanglement{
				Kind:      KindInterface,
				Name:      ts.Name.Name,
				Signature: "interface " + ts.Name.Name,
				Package:   pkg,
			})
			entanglements = append(entanglements, extractInterfaceMethods(iface, ts.Name.Name, pkg)...)
		} else {
			entanglements = append(entanglements, Entanglement{
				Kind:      KindType,
				Name:      ts.Name.Name,
				Signature: FormatTypeSignature(ts),
				Package:   pkg,
			})
		}
	}
	return entanglements
}

// filesLineRe matches markdown lines like "- `path/to/file.go` — description"
// or "- `path/to/file.go` - description".
var filesLineRe = regexp.MustCompile("^\\s*-\\s*`([^`]+)`")

// parseFilesSection extracts file paths from a ## Files markdown section.
func parseFilesSection(body string) []string {
	lines := strings.Split(body, "\n")
	inFiles := false
	var paths []string
	seen := make(map[string]bool)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect ## Files header.
		if strings.HasPrefix(trimmed, "## Files") {
			inFiles = true
			continue
		}

		// Stop at next ## header.
		if inFiles && strings.HasPrefix(trimmed, "## ") {
			break
		}

		if !inFiles {
			continue
		}

		// Match file path lines.
		if m := filesLineRe.FindStringSubmatch(line); m != nil {
			path := m[1]
			if !seen[path] {
				paths = append(paths, path)
				seen[path] = true
			}
		}
	}

	return paths
}

// inlineTypeRe matches "type Name struct", "type Name interface", etc. in code blocks.
var inlineTypeRe = regexp.MustCompile(`(?m)^type\s+([A-Z]\w*)\s+(struct|interface)`)

// inlineFuncRe matches "func Name(" in code blocks.
var inlineFuncRe = regexp.MustCompile(`(?m)^func\s+([A-Z]\w*)\s*\(`)

// extractInlineSymbols extracts type and function names from inline code
// blocks (```go ... ```) in the phase body. Used for files that don't exist
// yet on disk.
func extractInlineSymbols(body, phaseID, filePath string) []Entanglement {
	blocks := extractCodeBlocks(body)
	var entanglements []Entanglement
	seen := make(map[string]bool)
	pkg := packageFromPath(filePath)

	for _, block := range blocks {
		// Extract type declarations.
		for _, m := range inlineTypeRe.FindAllStringSubmatch(block, -1) {
			name := m[1]
			kind := KindType
			if m[2] == "interface" {
				kind = KindInterface
			}
			key := kind + ":" + name
			if !seen[key] {
				entanglements = append(entanglements, Entanglement{
					Producer:  phaseID,
					Kind:      kind,
					Name:      name,
					Signature: "type " + name + " " + m[2],
					Package:   pkg,
					Status:    StatusPending,
				})
				seen[key] = true
			}
		}

		// Extract function declarations.
		for _, m := range inlineFuncRe.FindAllStringSubmatch(block, -1) {
			name := m[1]
			key := KindFunction + ":" + name
			if !seen[key] {
				entanglements = append(entanglements, Entanglement{
					Producer:  phaseID,
					Kind:      KindFunction,
					Name:      name,
					Signature: "func " + name + "(...)",
					Package:   pkg,
					Status:    StatusPending,
				})
				seen[key] = true
			}
		}
	}

	return entanglements
}

// codeBlockRe matches fenced code blocks (```...```).
var codeBlockRe = regexp.MustCompile("(?s)```(?:go)?\\n(.*?)```")

// extractCodeBlocks returns the contents of fenced code blocks in markdown.
func extractCodeBlocks(body string) []string {
	matches := codeBlockRe.FindAllStringSubmatch(body, -1)
	blocks := make([]string, 0, len(matches))
	for _, m := range matches {
		blocks = append(blocks, m[1])
	}
	return blocks
}

// crossReference checks each phase's body against the produces of its
// dependency phases. If a symbol name produced by a dependency appears in
// the consumer's body text, it's marked as a consumed entanglement.
func (s *StaticScanner) crossReference(contracts []PhaseContract, phases []PhaseInput) {
	// Build phase ID -> contract index map.
	idxByID := make(map[string]int, len(contracts))
	for i := range contracts {
		idxByID[contracts[i].PhaseID] = i
	}

	// Build phase ID -> body map.
	bodyByID := make(map[string]string, len(phases))
	for i := range phases {
		bodyByID[phases[i].ID] = phases[i].Body
	}

	// Build dependency map.
	depsByID := make(map[string][]string, len(phases))
	for i := range phases {
		depsByID[phases[i].ID] = phases[i].DependsOn
	}

	for i := range contracts {
		consumer := &contracts[i]
		consumerBody := bodyByID[consumer.PhaseID]
		deps := depsByID[consumer.PhaseID]

		for _, depID := range deps {
			depIdx, ok := idxByID[depID]
			if !ok {
				continue
			}
			producer := &contracts[depIdx]

			for _, prod := range producer.Produces {
				if containsSymbolRef(consumerBody, prod.Name) {
					consumer.Consumes = append(consumer.Consumes, Entanglement{
						Producer: depID,
						Consumer: consumer.PhaseID,
						Kind:     prod.Kind,
						Name:     prod.Name,
						Package:  prod.Package,
						Status:   StatusPending,
					})
				}
			}
		}
	}
}

// containsSymbolRef checks if a symbol name appears in text as a word boundary
// match (not as a substring of a larger word).
func containsSymbolRef(text, symbol string) bool {
	// For method names like "Foo.Bar", check for the full qualified name
	// or just the method name after the dot.
	if idx := strings.LastIndex(symbol, "."); idx >= 0 {
		method := symbol[idx+1:]
		return containsWord(text, symbol) || containsWord(text, method)
	}
	return containsWord(text, symbol)
}

// containsWord checks if text contains word as a whole word (word boundary match).
func containsWord(text, word string) bool {
	if word == "" {
		return false
	}
	for i := 0; i < len(text); {
		idx := strings.Index(text[i:], word)
		if idx < 0 {
			return false
		}
		pos := i + idx
		end := pos + len(word)

		leftOK := pos == 0 || !isIdentChar(text[pos-1])
		rightOK := end >= len(text) || !isIdentChar(text[end])

		if leftOK && rightOK {
			return true
		}
		i = pos + 1
	}
	return false
}

// isIdentChar returns true for characters that can be part of an identifier.
func isIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}
