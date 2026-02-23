// publisher.go provides post-phase entanglement extraction and publishing.
//
// After a phase completes its coder-reviewer loop, the Publisher examines the
// git diff and extracts entanglements from changed files. Go files are parsed with
// go/parser to extract exported symbols; non-Go files produce file-level
// entanglements only.
package fabric

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

// Publisher extracts and publishes entanglements from completed phases.
type Publisher struct {
	Fabric  Fabric
	WorkDir string

	// Logger receives non-fatal warnings (e.g. parse errors). If nil, warnings
	// are silently discarded.
	Logger io.Writer
}

// PublishPhase extracts entanglements from the git diff of a completed phase
// and writes them to the fabric. It runs `git diff --name-only` between the
// two SHAs, parses any changed Go files for exported symbols, and publishes
// all extracted entanglements in a single batch.
func (p *Publisher) PublishPhase(ctx context.Context, phaseID, beforeSHA, afterSHA string) error {
	files, err := p.changedFiles(ctx, beforeSHA, afterSHA)
	if err != nil {
		return fmt.Errorf("publisher: changed files: %w", err)
	}
	if len(files) == 0 {
		return nil
	}

	var entanglements []Entanglement

	for _, f := range files {
		// Claim the file for this phase.
		if claimErr := p.Fabric.ClaimFile(ctx, f, phaseID); claimErr != nil {
			p.logf("publisher: claim %s: %v", f, claimErr)
		}

		// All files get a file-level entanglement.
		entanglements = append(entanglements, Entanglement{
			Producer: phaseID,
			Kind:     KindFile,
			Name:     f,
			Package:  packageFromPath(f),
			Status:   StatusFulfilled,
		})

		// Go files get additional symbol-level entanglements.
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
			entanglements = append(entanglements, syms...)
		}
	}

	if len(entanglements) == 0 {
		return nil
	}
	if err := p.Fabric.PublishEntanglements(ctx, entanglements); err != nil {
		return fmt.Errorf("publisher: publish entanglements: %w", err)
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
// entanglements. It uses go/parser with ParseComments but does not perform
// type-checking — signatures are extracted from source text.
func (p *Publisher) extractGoSymbols(path string) ([]Entanglement, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	pkg := f.Name.Name
	var entanglements []Entanglement

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			entanglements = append(entanglements, p.extractFuncDecl(d, pkg)...)
		case *ast.GenDecl:
			if d.Tok == token.TYPE {
				entanglements = append(entanglements, p.extractTypeSpecs(d, pkg)...)
			}
		}
	}

	return entanglements, nil
}

// extractFuncDecl extracts an entanglement from a function or method declaration.
// Only exported symbols are included.
func (p *Publisher) extractFuncDecl(d *ast.FuncDecl, pkg string) []Entanglement {
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

// extractTypeSpecs extracts entanglements from type declarations in a GenDecl.
// Interfaces get KindInterface; all other exported types get KindType.
// Methods on interfaces are also extracted as KindMethod.
func (p *Publisher) extractTypeSpecs(d *ast.GenDecl, pkg string) []Entanglement {
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
			// Extract interface methods.
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

// extractInterfaceMethods extracts exported method signatures from an
// interface type as KindMethod entanglements.
func extractInterfaceMethods(iface *ast.InterfaceType, ifaceName, pkg string) []Entanglement {
	if iface.Methods == nil {
		return nil
	}
	var entanglements []Entanglement
	for _, method := range iface.Methods.List {
		if len(method.Names) == 0 {
			continue // embedded interface
		}
		name := method.Names[0].Name
		if !ast.IsExported(name) {
			continue
		}
		entanglements = append(entanglements, Entanglement{
			Kind:      KindMethod,
			Name:      ifaceName + "." + name,
			Signature: name + FormatFieldType(method.Type),
			Package:   pkg,
		})
	}
	return entanglements
}

// FormatFuncSignature builds a human-readable signature from a FuncDecl.
func FormatFuncSignature(d *ast.FuncDecl) string {
	var b strings.Builder
	b.WriteString("func ")
	if d.Recv != nil && len(d.Recv.List) > 0 {
		b.WriteString("(")
		b.WriteString(FormatRecvType(d.Recv.List[0].Type))
		b.WriteString(") ")
	}
	b.WriteString(d.Name.Name)
	b.WriteString(FormatFieldType(d.Type))
	return b.String()
}

// FormatRecvType returns the receiver type name, stripping pointer markers.
func FormatRecvType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return FormatRecvType(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return FormatRecvType(t.X)
	case *ast.IndexListExpr:
		return FormatRecvType(t.X)
	default:
		return "?"
	}
}

// FormatFieldType returns a string representation of a function type.
func FormatFieldType(expr ast.Expr) string {
	ft, ok := expr.(*ast.FuncType)
	if !ok {
		return ""
	}
	var b strings.Builder
	b.WriteString("(")
	b.WriteString(FormatFieldList(ft.Params))
	b.WriteString(")")
	if ft.Results != nil && len(ft.Results.List) > 0 {
		results := FormatFieldList(ft.Results)
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

// FormatFieldList formats a parameter or result list.
func FormatFieldList(fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 {
		return ""
	}
	var parts []string
	for _, field := range fl.List {
		typStr := ExprString(field.Type)
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

// ExprString returns a best-effort string for an AST expression.
func ExprString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return ExprString(t.X) + "." + t.Sel.Name
	case *ast.StarExpr:
		return "*" + ExprString(t.X)
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + ExprString(t.Elt)
		}
		return "[...]" + ExprString(t.Elt)
	case *ast.MapType:
		return "map[" + ExprString(t.Key) + "]" + ExprString(t.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.Ellipsis:
		return "..." + ExprString(t.Elt)
	case *ast.FuncType:
		return "func" + FormatFieldType(t)
	case *ast.ChanType:
		switch t.Dir {
		case ast.SEND:
			return "chan<- " + ExprString(t.Value)
		case ast.RECV:
			return "<-chan " + ExprString(t.Value)
		default:
			return "chan " + ExprString(t.Value)
		}
	default:
		return "?"
	}
}

// FormatTypeSignature returns a compact type declaration string.
func FormatTypeSignature(ts *ast.TypeSpec) string {
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
		b.WriteString(ExprString(t))
	case *ast.ArrayType:
		b.WriteString(ExprString(t))
	case *ast.MapType:
		b.WriteString(ExprString(t))
	case *ast.FuncType:
		b.WriteString("func" + FormatFieldType(t))
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
