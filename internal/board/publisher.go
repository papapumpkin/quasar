// publisher.go provides post-phase contract extraction and publishing.
//
// After a phase completes its coder-reviewer loop, the Publisher examines the
// git diff and extracts contracts from changed files. Go files are parsed with
// go/parser to extract exported symbols; non-Go files produce file-level
// contracts only.
package board

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
)

// Publisher extracts and publishes contracts from completed phases.
type Publisher struct {
	Board   Board
	WorkDir string

	// Logger receives non-fatal warnings (e.g. parse errors). If nil, warnings
	// are silently discarded.
	Logger io.Writer
}

// PublishPhase extracts contracts from the git diff of a completed phase
// and writes them to the board. It runs `git diff --name-only` between the
// two SHAs, parses any changed Go files for exported symbols, and publishes
// all extracted contracts in a single batch.
func (p *Publisher) PublishPhase(ctx context.Context, phaseID, beforeSHA, afterSHA string) error {
	files, err := p.changedFiles(ctx, beforeSHA, afterSHA)
	if err != nil {
		return fmt.Errorf("publisher: changed files: %w", err)
	}
	if len(files) == 0 {
		return nil
	}

	var contracts []Contract

	for _, f := range files {
		// Claim the file for this phase.
		if claimErr := p.Board.ClaimFile(ctx, f, phaseID); claimErr != nil {
			p.logf("publisher: claim %s: %v", f, claimErr)
		}

		// All files get a file-level contract.
		contracts = append(contracts, Contract{
			Producer: phaseID,
			Kind:     KindFile,
			Name:     f,
			Package:  packageFromPath(f),
			Status:   StatusFulfilled,
		})

		// Go files get additional symbol-level contracts.
		if strings.HasSuffix(f, ".go") && !strings.HasSuffix(f, "_test.go") {
			syms, parseErr := p.extractGoSymbols(filepath.Join(p.WorkDir, f))
			if parseErr != nil {
				p.logf("publisher: parse %s: %v", f, parseErr)
				continue
			}
			for i := range syms {
				syms[i].Producer = phaseID
				syms[i].Status = StatusFulfilled
			}
			contracts = append(contracts, syms...)
		}
	}

	if len(contracts) == 0 {
		return nil
	}
	if err := p.Board.PublishContracts(ctx, contracts); err != nil {
		return fmt.Errorf("publisher: publish contracts: %w", err)
	}
	return nil
}

// changedFiles runs git diff --name-only between two SHAs and returns the
// list of relative file paths.
func (p *Publisher) changedFiles(ctx context.Context, beforeSHA, afterSHA string) ([]string, error) {
	ref := beforeSHA + ".." + afterSHA
	cmd := exec.CommandContext(ctx, "git", "-C", p.WorkDir, "diff", "--name-only", ref)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git diff --name-only %s: %w: %s", ref, err, strings.TrimSpace(stderr.String()))
	}
	raw := strings.TrimSpace(stdout.String())
	if raw == "" {
		return nil, nil
	}
	return strings.Split(raw, "\n"), nil
}

// extractGoSymbols parses a Go source file and extracts exported symbols as
// contracts. It uses go/parser with ParseComments but does not perform
// type-checking — signatures are extracted from source text.
func (p *Publisher) extractGoSymbols(path string) ([]Contract, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	pkg := f.Name.Name
	var contracts []Contract

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			contracts = append(contracts, p.extractFuncDecl(d, pkg)...)
		case *ast.GenDecl:
			if d.Tok == token.TYPE {
				contracts = append(contracts, p.extractTypeSpecs(d, pkg)...)
			}
		}
	}

	return contracts, nil
}

// extractFuncDecl extracts a contract from a function or method declaration.
// Only exported symbols are included.
func (p *Publisher) extractFuncDecl(d *ast.FuncDecl, pkg string) []Contract {
	if !d.Name.IsExported() {
		return nil
	}

	kind := KindFunction
	name := d.Name.Name
	sig := formatFuncSignature(d)

	if d.Recv != nil && len(d.Recv.List) > 0 {
		kind = KindMethod
		recvType := formatRecvType(d.Recv.List[0].Type)
		name = recvType + "." + d.Name.Name
	}

	return []Contract{{
		Kind:      kind,
		Name:      name,
		Signature: sig,
		Package:   pkg,
	}}
}

// extractTypeSpecs extracts contracts from type declarations in a GenDecl.
// Interfaces get KindInterface; all other exported types get KindType.
// Methods on interfaces are also extracted as KindMethod.
func (p *Publisher) extractTypeSpecs(d *ast.GenDecl, pkg string) []Contract {
	var contracts []Contract
	for _, spec := range d.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok || !ts.Name.IsExported() {
			continue
		}

		iface, isIface := ts.Type.(*ast.InterfaceType)
		if isIface {
			contracts = append(contracts, Contract{
				Kind:      KindInterface,
				Name:      ts.Name.Name,
				Signature: "interface " + ts.Name.Name,
				Package:   pkg,
			})
			// Extract interface methods.
			contracts = append(contracts, extractInterfaceMethods(iface, ts.Name.Name, pkg)...)
		} else {
			contracts = append(contracts, Contract{
				Kind:      KindType,
				Name:      ts.Name.Name,
				Signature: formatTypeSignature(ts),
				Package:   pkg,
			})
		}
	}
	return contracts
}

// extractInterfaceMethods extracts exported method signatures from an
// interface type as KindMethod contracts.
func extractInterfaceMethods(iface *ast.InterfaceType, ifaceName, pkg string) []Contract {
	if iface.Methods == nil {
		return nil
	}
	var contracts []Contract
	for _, method := range iface.Methods.List {
		if len(method.Names) == 0 {
			continue // embedded interface
		}
		name := method.Names[0].Name
		if !ast.IsExported(name) {
			continue
		}
		contracts = append(contracts, Contract{
			Kind:      KindMethod,
			Name:      ifaceName + "." + name,
			Signature: name + formatFieldType(method.Type),
			Package:   pkg,
		})
	}
	return contracts
}

// formatFuncSignature builds a human-readable signature from a FuncDecl.
func formatFuncSignature(d *ast.FuncDecl) string {
	var b strings.Builder
	b.WriteString("func ")
	if d.Recv != nil && len(d.Recv.List) > 0 {
		b.WriteString("(")
		b.WriteString(formatRecvType(d.Recv.List[0].Type))
		b.WriteString(") ")
	}
	b.WriteString(d.Name.Name)
	b.WriteString(formatFieldType(d.Type))
	return b.String()
}

// formatRecvType returns the receiver type name, stripping pointer markers.
func formatRecvType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return formatRecvType(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return formatRecvType(t.X)
	case *ast.IndexListExpr:
		return formatRecvType(t.X)
	default:
		return "?"
	}
}

// formatFieldType returns a string representation of a function type.
func formatFieldType(expr ast.Expr) string {
	ft, ok := expr.(*ast.FuncType)
	if !ok {
		return ""
	}
	var b strings.Builder
	b.WriteString("(")
	b.WriteString(formatFieldList(ft.Params))
	b.WriteString(")")
	if ft.Results != nil && len(ft.Results.List) > 0 {
		results := formatFieldList(ft.Results)
		if len(ft.Results.List) == 1 && len(ft.Results.List[0].Names) == 0 {
			b.WriteString(" ")
			b.WriteString(results)
		} else {
			b.WriteString(" (")
			b.WriteString(results)
			b.WriteString(")")
		}
	}
	return b.String()
}

// formatFieldList formats a parameter or result list.
func formatFieldList(fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 {
		return ""
	}
	var parts []string
	for _, field := range fl.List {
		typStr := exprString(field.Type)
		if len(field.Names) == 0 {
			parts = append(parts, typStr)
		} else {
			for _, name := range field.Names {
				parts = append(parts, name.Name+" "+typStr)
			}
		}
	}
	return strings.Join(parts, ", ")
}

// exprString returns a best-effort string for an AST expression.
func exprString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return exprString(t.X) + "." + t.Sel.Name
	case *ast.StarExpr:
		return "*" + exprString(t.X)
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + exprString(t.Elt)
		}
		return "[...]" + exprString(t.Elt)
	case *ast.MapType:
		return "map[" + exprString(t.Key) + "]" + exprString(t.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.Ellipsis:
		return "..." + exprString(t.Elt)
	case *ast.FuncType:
		return "func" + formatFieldType(t)
	case *ast.ChanType:
		switch t.Dir {
		case ast.SEND:
			return "chan<- " + exprString(t.Value)
		case ast.RECV:
			return "<-chan " + exprString(t.Value)
		default:
			return "chan " + exprString(t.Value)
		}
	default:
		return "?"
	}
}

// formatTypeSignature returns a compact type declaration string.
func formatTypeSignature(ts *ast.TypeSpec) string {
	var b strings.Builder
	b.WriteString("type ")
	b.WriteString(ts.Name.Name)
	b.WriteString(" ")

	switch t := ts.Type.(type) {
	case *ast.StructType:
		b.WriteString("struct")
	case *ast.Ident:
		b.WriteString(t.Name)
	case *ast.SelectorExpr:
		b.WriteString(exprString(t))
	case *ast.ArrayType:
		b.WriteString(exprString(t))
	case *ast.MapType:
		b.WriteString(exprString(t))
	case *ast.FuncType:
		b.WriteString("func" + formatFieldType(t))
	default:
		b.WriteString("?")
	}
	return b.String()
}

// packageFromPath extracts a rough package name from a file path.
// For Go files, this is the parent directory name. For other files, it's
// the immediate parent directory.
func packageFromPath(path string) string {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return ""
	}
	return filepath.Base(dir)
}

// logf writes a formatted warning to the publisher's logger.
func (p *Publisher) logf(format string, args ...any) {
	if p.Logger != nil {
		fmt.Fprintf(p.Logger, format+"\n", args...)
	}
}
