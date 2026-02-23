package cmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/fabric"
)

// NOTE: AST formatting helpers (ExprString, FormatFuncSignature,
// FormatRecvType, FormatFieldType, FormatFieldList, FormatTypeSignature)
// are exported from internal/fabric and reused here to stay consistent
// with the Publisher's extraction logic.

func init() {
	cmd := &cobra.Command{
		Use:   "post",
		Short: "Post entanglements to the fabric",
		Long: `Posts entanglements to the fabric. Two modes:

  --from-file <path>    Extract exported Go symbols from a source file and
                        post each as an entanglement.

  --interface "<sig>"   Manually declare a single interface entanglement with
                        the given signature string.

Both modes require a task ID via --task flag or QUASAR_TASK_ID env.`,
		RunE: runFabricPost,
	}
	cmd.Flags().String("from-file", "", "Go source file to extract exports from")
	cmd.Flags().String("interface", "", "manual interface signature to declare")
	fabricCmd.AddCommand(cmd)
}

func runFabricPost(cmd *cobra.Command, _ []string) error {
	fromFile, _ := cmd.Flags().GetString("from-file")
	iface, _ := cmd.Flags().GetString("interface")

	if fromFile == "" && iface == "" {
		return fmt.Errorf("fabric post: either --from-file or --interface is required")
	}
	if fromFile != "" && iface != "" {
		return fmt.Errorf("fabric post: --from-file and --interface are mutually exclusive")
	}

	taskID, err := requireTaskID()
	if err != nil {
		return err
	}

	f, err := openFabric(cmd)
	if err != nil {
		return err
	}
	defer f.Close()

	if fromFile != "" {
		return postFromFile(cmd, f, taskID, fromFile)
	}
	return postInterface(cmd, f, taskID, iface)
}

// postFromFile extracts exported Go symbols from a source file and publishes them.
func postFromFile(cmd *cobra.Command, f *fabric.SQLiteFabric, taskID, path string) error {
	ctx := cmd.Context()

	entanglements, err := extractExports(path)
	if err != nil {
		return fmt.Errorf("fabric post: extract exports from %s: %w", path, err)
	}

	if len(entanglements) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No exported symbols found.")
		return nil
	}

	// Set producer on all entanglements.
	for i := range entanglements {
		entanglements[i].Producer = taskID
	}

	if err := f.PublishEntanglements(ctx, entanglements); err != nil {
		return fmt.Errorf("fabric post: publish entanglements: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Posted %d entanglements from %s\n", len(entanglements), path)
	return nil
}

// postInterface posts a single manually declared interface entanglement.
func postInterface(cmd *cobra.Command, f *fabric.SQLiteFabric, taskID, sig string) error {
	ctx := cmd.Context()

	e := fabric.Entanglement{
		Producer:  taskID,
		Kind:      fabric.KindInterface,
		Name:      interfaceNameFromSig(sig),
		Signature: sig,
		Status:    fabric.StatusPending,
	}

	if err := f.PublishEntanglement(ctx, e); err != nil {
		return fmt.Errorf("fabric post: publish interface: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Posted interface entanglement: %s\n", sig)
	return nil
}

// interfaceNameFromSig extracts a name from an interface signature string.
// For "type Foo interface { ... }", returns "Foo". Falls back to the full string.
func interfaceNameFromSig(sig string) string {
	sig = strings.TrimSpace(sig)
	// Try to extract "type <Name> interface" pattern.
	if strings.HasPrefix(sig, "type ") {
		parts := strings.Fields(sig)
		if len(parts) >= 2 {
			return parts[1]
		}
	}
	return sig
}

// extractExports parses a Go source file and extracts exported symbols as entanglements.
// This mirrors the Publisher.extractGoSymbols logic but operates standalone.
func extractExports(path string) ([]fabric.Entanglement, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	pkg := f.Name.Name
	var entanglements []fabric.Entanglement

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			entanglements = append(entanglements, extractFuncDecl(d, pkg)...)
		case *ast.GenDecl:
			if d.Tok == token.TYPE {
				entanglements = append(entanglements, extractTypeSpecs(d, pkg)...)
			}
		}
	}

	return entanglements, nil
}

// extractFuncDecl extracts an entanglement from an exported function or method.
func extractFuncDecl(d *ast.FuncDecl, pkg string) []fabric.Entanglement {
	if !d.Name.IsExported() {
		return nil
	}

	kind := fabric.KindFunction
	name := d.Name.Name
	sig := fabric.FormatFuncSignature(d)

	if d.Recv != nil && len(d.Recv.List) > 0 {
		kind = fabric.KindMethod
		recvType := fabric.FormatRecvType(d.Recv.List[0].Type)
		name = recvType + "." + d.Name.Name
	}

	return []fabric.Entanglement{{
		Kind:      kind,
		Name:      name,
		Signature: sig,
		Package:   pkg,
	}}
}

// extractTypeSpecs extracts entanglements from exported type declarations.
// Interfaces get KindInterface and their exported methods are also extracted
// as KindMethod, matching the publisher's behavior.
func extractTypeSpecs(d *ast.GenDecl, pkg string) []fabric.Entanglement {
	var entanglements []fabric.Entanglement
	for _, spec := range d.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok || !ts.Name.IsExported() {
			continue
		}

		iface, isIface := ts.Type.(*ast.InterfaceType)
		if isIface {
			entanglements = append(entanglements, fabric.Entanglement{
				Kind:      fabric.KindInterface,
				Name:      ts.Name.Name,
				Signature: "interface " + ts.Name.Name,
				Package:   pkg,
			})
			entanglements = append(entanglements, extractInterfaceMethods(iface, ts.Name.Name, pkg)...)
		} else {
			entanglements = append(entanglements, fabric.Entanglement{
				Kind:      fabric.KindType,
				Name:      ts.Name.Name,
				Signature: fabric.FormatTypeSignature(ts),
				Package:   pkg,
			})
		}
	}
	return entanglements
}

// extractInterfaceMethods extracts exported method signatures from an
// interface type as KindMethod entanglements.
func extractInterfaceMethods(iface *ast.InterfaceType, ifaceName, pkg string) []fabric.Entanglement {
	if iface.Methods == nil {
		return nil
	}
	var entanglements []fabric.Entanglement
	for _, method := range iface.Methods.List {
		if len(method.Names) == 0 {
			continue // embedded interface
		}
		name := method.Names[0].Name
		if !ast.IsExported(name) {
			continue
		}
		entanglements = append(entanglements, fabric.Entanglement{
			Kind:      fabric.KindMethod,
			Name:      ifaceName + "." + name,
			Signature: name + fabric.FormatFieldType(method.Type),
			Package:   pkg,
		})
	}
	return entanglements
}
