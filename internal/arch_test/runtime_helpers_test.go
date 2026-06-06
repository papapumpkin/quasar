package arch_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// dirExists reports whether absDir is an existing directory. The constellation
// runtime adds packages (stars, gc, runtime) incrementally; the boundary tests
// that guard those namespaces must pass on a tree where the directory does not
// exist yet, then start enforcing the moment it lands.
func dirExists(absDir string) bool {
	info, err := os.Stat(absDir)
	return err == nil && info.IsDir()
}

// goFilesUnder returns every non-test .go file at or below absDir (recursive).
// A missing directory yields nil, not an error, so forward-looking guards stay
// green until their package exists.
func goFilesUnder(t *testing.T, absDir string) []string {
	t.Helper()
	if !dirExists(absDir) {
		return nil
	}
	var files []string
	err := filepath.WalkDir(absDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", absDir, err)
	}
	return files
}

// fullImport pairs an imported package path with the file that imports it, so
// boundary violations can be reported with a precise location.
type fullImport struct {
	path    string
	relFile string
}

// fullImportsUnder parses the imports of every non-test .go file at or below
// absDir and returns each import path with its importing file. Unlike importsOf
// (which collapses to the first internal/ component), this preserves the full
// path so a test can distinguish internal/sensors from internal/sensors/github.
func fullImportsUnder(t *testing.T, absDir string) []fullImport {
	t.Helper()

	var imports []fullImport
	fset := token.NewFileSet()
	for _, f := range goFilesUnder(t, absDir) {
		node, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing imports in %s: %v", f, err)
		}
		rel := relForSafety(t, f)
		for _, imp := range node.Imports {
			imports = append(imports, fullImport{
				path:    strings.Trim(imp.Path.Value, `"`),
				relFile: rel,
			})
		}
	}
	return imports
}

// blobColumn identifies a (table, column) pair, used for both the migration
// scan and the RegisterReference call scan in TestBlobHashColumnsRegistered.
type blobColumn struct {
	table  string
	column string
}

func (b blobColumn) String() string { return b.table + "." + b.column }

var (
	createTableRe = regexp.MustCompile("(?i)^\\s*CREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?[\"`]?([A-Za-z_][A-Za-z0-9_]*)")
	columnRe      = regexp.MustCompile("^\\s*([A-Za-z_][A-Za-z0-9_]*)")
	// alterTableRe captures the table and column of an `ALTER TABLE t ADD COLUMN c …`
	// statement. Columns added this way live on a single self-contained line rather
	// than inside a CREATE TABLE body, so the line-oriented scanner must match them
	// directly — otherwise a *_blob_hash column added by ALTER is invisible to the
	// registration check and its blobs could be reclaimed while still referenced.
	alterTableRe = regexp.MustCompile("(?i)^\\s*ALTER\\s+TABLE\\s+[\"`]?([A-Za-z_][A-Za-z0-9_]*)[\"`]?\\s+ADD\\s+(?:COLUMN\\s+)?[\"`]?([A-Za-z_][A-Za-z0-9_]*)")
)

// blobHashColumnsInMigrations scans the SQL migrations and returns every column
// whose name ends in _blob_hash, attributed to the table it was declared in.
// This is a deliberately small line-oriented scan, not a SQL parser: the
// migrations are hand-written, comment-annotated, and grep-friendly.
func blobHashColumnsInMigrations(t *testing.T) []blobColumn {
	t.Helper()

	dir := filepath.Join(internalDirPath(t), "fabric", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading migrations dir %s: %v", dir, err)
	}

	var cols []blobColumn
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		cols = append(cols, blobHashColumnsInFile(t, filepath.Join(dir, e.Name()))...)
	}
	return cols
}

// blobHashColumnsInFile scans a single .sql file, tracking the current table as
// CREATE TABLE statements are seen and recording any *_blob_hash column.
func blobHashColumnsInFile(t *testing.T, path string) []blobColumn {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	var (
		cols    []blobColumn
		current string
	)
	for _, raw := range strings.Split(string(data), "\n") {
		// Drop inline comments so `body_blob_hash TEXT -- sha256` parses cleanly.
		line := raw
		if i := strings.Index(line, "--"); i != -1 {
			line = line[:i]
		}
		if m := createTableRe.FindStringSubmatch(line); m != nil {
			current = strings.TrimSuffix(m[1], "_new") // CREATE TABLE x_new ... RENAME TO x
			continue
		}
		// ALTER TABLE … ADD COLUMN is self-contained: the table and column are on
		// the same line and do not change the current CREATE TABLE context.
		if m := alterTableRe.FindStringSubmatch(line); m != nil {
			if strings.HasSuffix(m[2], "_blob_hash") {
				cols = append(cols, blobColumn{table: m[1], column: m[2]})
			}
			continue
		}
		if current == "" {
			continue
		}
		if m := columnRe.FindStringSubmatch(line); m != nil && strings.HasSuffix(m[1], "_blob_hash") {
			cols = append(cols, blobColumn{table: current, column: m[1]})
		}
	}
	return cols
}

// registeredBlobRefs scans all non-test Go source for blobstore.RegisterReference
// calls with two string-literal arguments and returns the (table, column) pairs.
func registeredBlobRefs(t *testing.T) []blobColumn {
	t.Helper()

	root := repoRoot(t)
	var refs []blobColumn
	for _, sub := range []string{"cmd", "internal"} {
		base := filepath.Join(root, sub)
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			refs = append(refs, registerCallsInFile(t, path)...)
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("walking %s: %v", base, err)
		}
	}
	return refs
}

// registerCallsInFile finds blobstore.RegisterReference("table","column") calls
// in one file. Non-literal arguments are skipped: registration must use string
// literals so the reference set is statically knowable (and arch-testable).
func registerCallsInFile(t *testing.T, path string) []blobColumn {
	t.Helper()

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	var refs []blobColumn
	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "RegisterReference" {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "blobstore" || len(call.Args) != 2 {
			return true
		}
		table, ok1 := stringLitValue(call.Args[0])
		column, ok2 := stringLitValue(call.Args[1])
		if ok1 && ok2 {
			refs = append(refs, blobColumn{table: table, column: column})
		}
		return true
	})
	return refs
}

// stringLitValue returns the unquoted value of a string-literal expression.
func stringLitValue(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	val, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return val, true
}
