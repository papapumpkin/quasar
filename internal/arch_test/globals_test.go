package arch_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// allowedGlobals lists package-level var names that are intentionally global
// but don't match the automated detection heuristics. Each entry documents why
// it is acceptable.
var allowedGlobals = map[string][]string{
	// config: exported default values — constant-like slice literal.
	"config": {"DefaultLintCommands"},

	// fabric: lookup tables used for validation — constant-like map literals.
	"fabric": {
		"ValidPulseKinds",
		"ValidDiscoveryKinds",
		"timestampFormats",
	},

	// nebula: validation lookup tables and sentinel maps — constant-like map literals.
	"nebula": {
		"ValidGateModes",
		"interventionFiles",
	},

	// snapshot: list of conventional file names — constant-like slice literal.
	"snapshot": {"conventionsFiles"},

	// tui: UI styles, color constants, ASCII art, and cached render data.
	// These are all effectively immutable after init and are standard patterns
	// in Bubble Tea / lipgloss applications.
	"tui": {
		// styles.go — lipgloss color and style constants
		"colorPrimary", "colorAccent", "colorSuccess", "colorDanger",
		"colorWarning", "colorNebula", "colorStardust",
		"colorBrightWhite", "colorMuted", "colorMutedLight",
		"colorSurface", "colorSurfaceBright", "colorRedshift",
		"styleStatusBar", "styleStatusPhase", "styleStatusIdle",
		"styleStatusSuccess", "styleStatusError", "styleStatusBudget",
		"styleBreadcrumb", "styleBreadcrumbSep",
		"styleRowSelected", "styleRowNormal", "styleColHeader",
		"styleHeaderID", "styleHeaderTitle", "styleHeaderStatus", "styleHeaderPriority",
		"styleTreeConnector", "styleWaveHeader",
		"styleDetailBorder", "styleDetailTitle",
		"styleDetailLabel", "styleDetailValue", "styleDetailDeps",
		"styleDetailSection", "styleDetailDescription",
		"styleDiffAdd", "styleDiffRemove",
		"styleDiffContext", "styleDiffHeader", "styleDiffHunk",
		"styleGateOverlay", "styleGateTitle", "styleGateAction",
		"styleGateApprove", "styleGateReject", "styleGateSkip",
		"styleFooter", "styleFooterKey", "styleFooterDesc",
		"styleSectionBorder",
		"styleOverlaySuccess", "styleOverlayError",
		"styleBeadOpen", "styleBeadClosed", "styleBeadInProgress",
		"styleResourceNormal", "styleResourceWarning", "styleResourceCritical",
		"styleHailOverlay", "styleHailTitle", "styleHailBorder",
		"styleHailInput", "styleHailPlaceholder", "styleHailFocused",
		"styleToast",

		// planview.go — plan view styles
		"stylePlanTitle", "stylePlanPhase", "stylePlanDep",

		// logo.go — logo styles
		"styleLogoJet", "styleLogoCore",

		// banner.go — ASCII art and cached rendering
		"artXS", "artSA", "artSB", "artXL",
		"styleRedOuter", "styleRedInner",
		"renderCache", "renderCacheMu",

		// tabs.go — fixed-size array of tab labels
		"tabLabels",

		// overlay.go — atomic counter for toast IDs (sync-like)
		"nextToastID",

		// splash.go — doppler color ramps and density rune table
		"splashDopplerRamps", "splashDensityRamp",

		// boardview.go — column definitions for the board view
		"columnDefs",
	},
}

// TestNoMutableGlobalState scans all internal packages for package-level var
// declarations and flags any that are not in the allowed categories:
//   - error sentinels (errors.New / fmt.Errorf)
//   - compile-time interface checks (var _ T = ...)
//   - regexp.MustCompile
//   - sync primitives (sync.Once, sync.Mutex, etc.) and atomic types
//   - simple literal values (string, int, bool, float)
func TestNoMutableGlobalState(t *testing.T) {
	t.Parallel()

	dir := internalDirPath(t)
	pkgs := internalPackages(t)

	for _, pkg := range pkgs {
		t.Run(pkg, func(t *testing.T) {
			t.Parallel()

			pkgDir := filepath.Join(dir, pkg)
			files := goFilesIn(t, pkgDir)
			allowed := makeAllowSet(pkg)

			fset := token.NewFileSet()
			for _, filePath := range files {
				node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
				if err != nil {
					t.Fatalf("parsing %s: %v", filePath, err)
				}

				for _, decl := range node.Decls {
					gd, ok := decl.(*ast.GenDecl)
					if !ok || gd.Tok != token.VAR {
						continue
					}
					for _, spec := range gd.Specs {
						vs, ok := spec.(*ast.ValueSpec)
						if !ok {
							continue
						}
						checkVarSpec(t, vs, allowed, filePath)
					}
				}
			}
		})
	}
}

// checkVarSpec checks a single var spec against the allowed patterns.
func checkVarSpec(t *testing.T, vs *ast.ValueSpec, allowed map[string]bool, filePath string) {
	t.Helper()

	for i, name := range vs.Names {
		varName := name.Name

		// 1. Blank identifier — compile-time interface check.
		if varName == "_" {
			continue
		}

		// 2. Explicitly allowlisted.
		if allowed[varName] {
			continue
		}

		// Determine the value expression for this name (may be nil).
		var val ast.Expr
		if i < len(vs.Values) {
			val = vs.Values[i]
		}

		// 3. Error sentinel — type is error or init calls errors.New/fmt.Errorf.
		if isErrorSentinel(vs.Type, val) {
			continue
		}

		// 4. regexp.MustCompile
		if isRegexpCompile(val) {
			continue
		}

		// 5. sync primitive or atomic type.
		if isSyncOrAtomicType(vs.Type) {
			continue
		}

		// 6. Simple literal (string, int, bool, float).
		if isSimpleLiteral(val) {
			continue
		}

		typeName := typeString(vs.Type)
		t.Errorf("mutable global state in %s: var %s (type: %s); use dependency injection or move to a function",
			filepath.Base(filePath), varName, typeName)
	}
}

// makeAllowSet builds a set of allowed var names for a package.
func makeAllowSet(pkg string) map[string]bool {
	names := allowedGlobals[pkg]
	s := make(map[string]bool, len(names))
	for _, n := range names {
		s[n] = true
	}
	return s
}

// isErrorSentinel returns true if the var declaration looks like an error
// sentinel: either the type annotation is `error`, or the initializer calls
// `errors.New(...)` or `fmt.Errorf(...)`.
func isErrorSentinel(typeExpr ast.Expr, val ast.Expr) bool {
	// Check type annotation.
	if ident, ok := typeExpr.(*ast.Ident); ok && ident.Name == "error" {
		return true
	}

	if val == nil {
		return false
	}

	call, ok := val.(*ast.CallExpr)
	if !ok {
		return false
	}

	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}

	return (pkgIdent.Name == "errors" && sel.Sel.Name == "New") ||
		(pkgIdent.Name == "fmt" && sel.Sel.Name == "Errorf")
}

// isRegexpCompile returns true if the initializer is regexp.MustCompile(...).
func isRegexpCompile(val ast.Expr) bool {
	if val == nil {
		return false
	}
	call, ok := val.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return pkgIdent.Name == "regexp" && sel.Sel.Name == "MustCompile"
}

// isSyncOrAtomicType returns true if the type expression is a sync or
// sync/atomic primitive (sync.Once, sync.Mutex, sync.RWMutex, sync.Pool,
// sync.Map, atomic.Int32, etc.).
func isSyncOrAtomicType(typeExpr ast.Expr) bool {
	if typeExpr == nil {
		return false
	}
	sel, ok := typeExpr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return pkgIdent.Name == "sync" || pkgIdent.Name == "atomic"
}

// isSimpleLiteral returns true if the initializer is a basic literal
// (string, int, float, char, imaginary).
func isSimpleLiteral(val ast.Expr) bool {
	if val == nil {
		return false
	}
	_, ok := val.(*ast.BasicLit)
	return ok
}

// typeString returns a human-readable string for a type expression.
// Returns "<inferred>" when the type is implicit.
func typeString(expr ast.Expr) string {
	if expr == nil {
		return "<inferred>"
	}
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		if x, ok := t.X.(*ast.Ident); ok {
			return x.Name + "." + t.Sel.Name
		}
	case *ast.StarExpr:
		return "*" + typeString(t.X)
	case *ast.ArrayType:
		if t.Len != nil {
			return "[...]" + typeString(t.Elt)
		}
		return "[]" + typeString(t.Elt)
	case *ast.MapType:
		return "map[" + typeString(t.Key) + "]" + typeString(t.Value)
	case *ast.InterfaceType:
		return "interface{}"
	}
	return "<complex>"
}

// TestAllowedGlobalsAreUsed ensures entries in the allowlist correspond to
// actual var declarations. This catches stale allowlist entries when globals
// are removed or renamed.
func TestAllowedGlobalsAreUsed(t *testing.T) {
	t.Parallel()

	dir := internalDirPath(t)

	for pkg, names := range allowedGlobals {
		pkg, names := pkg, names
		t.Run(pkg, func(t *testing.T) {
			t.Parallel()

			pkgDir := filepath.Join(dir, pkg)
			files := goFilesIn(t, pkgDir)
			if len(files) == 0 {
				t.Fatalf("no .go files found for allowlisted package %q", pkg)
			}

			// Collect all package-level var names in this package.
			declared := make(map[string]bool)
			fset := token.NewFileSet()
			for _, filePath := range files {
				node, err := parser.ParseFile(fset, filePath, nil, 0)
				if err != nil {
					t.Fatalf("parsing %s: %v", filePath, err)
				}
				for _, decl := range node.Decls {
					gd, ok := decl.(*ast.GenDecl)
					if !ok || gd.Tok != token.VAR {
						continue
					}
					for _, spec := range gd.Specs {
						vs, ok := spec.(*ast.ValueSpec)
						if !ok {
							continue
						}
						for _, n := range vs.Names {
							declared[n.Name] = true
						}
					}
				}
			}

			for _, name := range names {
				if !declared[name] {
					t.Errorf("allowedGlobals[%q] contains %q but no such var exists — remove stale entry",
						pkg, name)
				}
			}
		})
	}
}

// TestGlobalStateDetectionCanary is a compile-time canary that verifies the
// detection logic works. It parses a synthetic Go source containing a
// disallowed global and ensures the checker would flag it.
func TestGlobalStateDetectionCanary(t *testing.T) {
	t.Parallel()

	// Synthetic source with one disallowed global.
	src := `package canary

var badMap = make(map[string]string)
`

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "canary.go", src, 0)
	if err != nil {
		t.Fatalf("parsing canary source: %v", err)
	}

	found := false
	for _, decl := range node.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if name.Name == "_" {
					continue
				}
				var val ast.Expr
				if i < len(vs.Values) {
					val = vs.Values[i]
				}
				if isErrorSentinel(vs.Type, val) ||
					isRegexpCompile(val) ||
					isSyncOrAtomicType(vs.Type) ||
					isSimpleLiteral(val) {
					t.Errorf("canary var %q should NOT be allowed by any heuristic", name.Name)
				}
				found = true
			}
		}
	}

	if !found {
		t.Error("canary: expected to find var badMap in synthetic source")
	}
}

// TestGlobalStateAllowedPatterns verifies each allowed category passes detection.
func TestGlobalStateAllowedPatterns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
	}{
		{
			name: "error_sentinel_errors_new",
			src:  `package p; import "errors"; var ErrFoo = errors.New("foo")`,
		},
		{
			name: "error_sentinel_fmt_errorf",
			src:  `package p; import "fmt"; var ErrBar = fmt.Errorf("bar: %w", nil)`,
		},
		{
			name: "interface_check",
			src:  `package p; type I interface{}; type S struct{}; var _ I = (*S)(nil)`,
		},
		{
			name: "regexp_must_compile",
			src:  `package p; import "regexp"; var re = regexp.MustCompile("^foo$")`,
		},
		{
			name: "simple_string_literal",
			src:  `package p; var name = "hello"`,
		},
		{
			name: "simple_int_literal",
			src:  `package p; var count = 42`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fset := token.NewFileSet()
			node, err := parser.ParseFile(fset, "test.go", tc.src, 0)
			if err != nil {
				t.Fatalf("parsing: %v", err)
			}

			for _, decl := range node.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.VAR {
					continue
				}
				for _, spec := range gd.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, name := range vs.Names {
						if name.Name == "_" {
							continue
						}
						var val ast.Expr
						if i < len(vs.Values) {
							val = vs.Values[i]
						}
						allowed := isErrorSentinel(vs.Type, val) ||
							isRegexpCompile(val) ||
							isSyncOrAtomicType(vs.Type) ||
							isSimpleLiteral(val)
						if !allowed {
							relName := strings.TrimPrefix(name.Name, "_")
							t.Errorf("var %q in test case %q should be allowed but was flagged",
								relName, tc.name)
						}
					}
				}
			}
		})
	}
}
