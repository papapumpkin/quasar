package arch_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// gitWallEnv gates the output-safety arch tests during the incremental
// migration of scattered git calls into internal/gitops. With the variable
// unset (or any value other than "warn") violations are real test failures.
// Setting it to "warn" downgrades every violation to a logged message so a
// package owner mid-migration can still get a green suite. This escape hatch is
// temporary and is removed in Nebula 3 once all callers are migrated.
const gitWallEnv = "QUASAR_ARCH_TEST_GIT_WALL"

// gitExecExceptions are files that still call exec.Command("git", …) directly
// and predate the internal/gitops perimeter. They are grandfathered so this
// phase can land without a tree-wide migration; new direct callers are NOT
// exempt and will fail the wall.
//
// TODO(nebula-3): migrate each of these onto internal/gitops and delete the
// entry. The list shrinking to empty is the migration's definition of done.
var gitExecExceptions = map[string]bool{
	"internal/loop/git.go":              true,
	"internal/nebula/git.go":            true,
	"internal/nebula/branch.go":         true,
	"internal/checkpoint/checkpoint.go": true,
	"internal/fabric/publisher.go":      true,
	"internal/snapshot/scanner.go":      true,
	"internal/tui/bridge.go":            true,
}

// ghExecAllowedPrefixes are the package path prefixes (relative to repo root)
// permitted to call exec.Command("gh", …). `gh` is forge-specific and confined
// to ticket reading inside the GitHub adapter; internal/forge is reserved for
// the Nebula 3 write side and added here ahead of time so it is a single-line
// edit later.
var ghExecAllowedPrefixes = []string{
	"internal/integrations/github/",
	"internal/forge/",
}

// gitExec is a detected exec.Command("git"/"gh", …) call site.
type gitExec struct {
	relPath string
	line    int
}

// gitWallEnforced reports whether wall violations are hard failures. They are,
// unless QUASAR_ARCH_TEST_GIT_WALL is exactly "warn".
func gitWallEnforced() bool {
	return os.Getenv(gitWallEnv) != "warn"
}

// reportf fails the test, or merely logs, depending on the wall mode.
func reportf(t *testing.T, format string, args ...any) {
	t.Helper()
	if !gitWallEnforced() {
		t.Logf("[git-wall warn] "+format, args...)
		return
	}
	t.Errorf(format, args...)
}

// TestGitWallModeGate verifies the env gate: unset or any value other than
// "warn" enforces hard failures; "warn" downgrades violations to logs so a
// mid-migration suite stays green.
func TestGitWallModeGate(t *testing.T) {
	t.Run("unset enforces", func(t *testing.T) {
		t.Setenv(gitWallEnv, "")
		if !gitWallEnforced() {
			t.Error("unset should enforce (hard failures)")
		}
	})
	t.Run("warn downgrades", func(t *testing.T) {
		t.Setenv(gitWallEnv, "warn")
		if gitWallEnforced() {
			t.Error("warn should downgrade violations to logs")
		}
	})
	t.Run("other value enforces", func(t *testing.T) {
		t.Setenv(gitWallEnv, "on")
		if !gitWallEnforced() {
			t.Error("any non-warn value should enforce")
		}
	})
}

// TestNoDirectGitExecOutsideGitops verifies that no production code outside
// internal/gitops shells out to git directly. All git writes must go through
// the gitops Client so the output-safety perimeter cannot be bypassed.
func TestNoDirectGitExecOutsideGitops(t *testing.T) {
	t.Parallel()

	for _, hit := range findExecCalls(t, "git") {
		// internal/gitops is the perimeter itself; arch_test is this scanner.
		if strings.HasPrefix(hit.relPath, "internal/gitops/") ||
			strings.HasPrefix(hit.relPath, "internal/arch_test/") {
			continue
		}
		if gitExecExceptions[hit.relPath] {
			t.Logf("known pre-gitops caller (TODO migrate): %s:%d", hit.relPath, hit.line)
			continue
		}
		reportf(t, "%s:%d calls exec.Command(\"git\", …) directly; route git writes through internal/gitops",
			hit.relPath, hit.line)
	}
}

// TestNoDirectGHExecOutsideAllowedPackages verifies that only the GitHub
// adapter (and the reserved internal/forge) shell out to gh. Using gh for git
// operations would fork Quasar's forge support, so it is confined to ticket
// reading.
func TestNoDirectGHExecOutsideAllowedPackages(t *testing.T) {
	t.Parallel()

	for _, hit := range findExecCalls(t, "gh") {
		if isGHAllowed(hit.relPath) {
			continue
		}
		reportf(t, "%s:%d calls exec.Command(\"gh\", …); gh is confined to the GitHub adapter (ticket reading only)",
			hit.relPath, hit.line)
	}
}

// TestNoForbiddenGitSubcommands scans internal/gitops source for string
// literals that suggest a destructive git operation slipped in (unconditional
// --force, hard reset, interactive rebase, ref deletion, base-branch -D). It is
// a smell detector for obvious mistakes during review, not an airtight proof.
func TestNoForbiddenGitSubcommands(t *testing.T) {
	t.Parallel()

	gitopsDir := filepath.Join(internalDirPath(t), "gitops")
	for _, file := range goFilesIn(t, gitopsDir) {
		rel := relForSafety(t, file)
		for _, lit := range stringLiterals(t, file) {
			if smell := forbiddenSubcommandSmell(lit.value); smell != "" {
				reportf(t, "%s:%d contains forbidden git pattern %q (%s)", rel, lit.line, lit.value, smell)
			}
		}
	}
}

// forbiddenSubcommandSmell returns a description if the literal looks like a
// forbidden git operation, or "" otherwise.
func forbiddenSubcommandSmell(lit string) string {
	if strings.Contains(lit, "--force") && !strings.Contains(lit, "--force-with-lease") {
		return "unconditional force; use --force-with-lease"
	}
	patterns := map[string]string{
		"reset --hard":  "hard reset",
		"rebase -i":     "interactive rebase",
		"branch -D":     "force branch delete",
		"push origin :": "ref deletion",
	}
	for p, desc := range patterns {
		if strings.Contains(lit, p) {
			return desc
		}
	}
	return ""
}

// isGHAllowed reports whether relPath is in a package permitted to exec gh.
func isGHAllowed(relPath string) bool {
	if strings.HasPrefix(relPath, "internal/arch_test/") {
		return true
	}
	for _, p := range ghExecAllowedPrefixes {
		if strings.HasPrefix(relPath, p) {
			return true
		}
	}
	return false
}

// findExecCalls walks cmd/ and internal/ for non-test .go files and returns
// every exec.Command / exec.CommandContext call whose command argument is the
// string literal cmdName.
func findExecCalls(t *testing.T, cmdName string) []gitExec {
	t.Helper()

	root := repoRoot(t)
	var hits []gitExec
	for _, sub := range []string{"cmd", "internal"} {
		base := filepath.Join(root, sub)
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			hits = append(hits, execCallsInFile(t, path, cmdName)...)
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("walking %s: %v", base, err)
		}
	}
	return hits
}

// execCallsInFile parses one file and returns exec.Command/CommandContext call
// sites whose command argument literal equals cmdName.
func execCallsInFile(t *testing.T, path, cmdName string) []gitExec {
	t.Helper()

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	rel := relForSafety(t, path)

	var hits []gitExec
	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		argIdx, ok := execCommandArgIndex(call)
		if !ok || argIdx >= len(call.Args) {
			return true
		}
		if literalEquals(call.Args[argIdx], cmdName) {
			hits = append(hits, gitExec{relPath: rel, line: fset.Position(call.Pos()).Line})
		}
		return true
	})
	return hits
}

// execCommandArgIndex reports the index of the command-name argument if call is
// exec.Command (index 0) or exec.CommandContext (index 1).
func execCommandArgIndex(call *ast.CallExpr) (int, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return 0, false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "exec" {
		return 0, false
	}
	switch sel.Sel.Name {
	case "Command":
		return 0, true
	case "CommandContext":
		return 1, true
	default:
		return 0, false
	}
}

// literalEquals reports whether expr is a string literal equal to want.
func literalEquals(expr ast.Expr, want string) bool {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	val, err := strconv.Unquote(lit.Value)
	if err != nil {
		return false
	}
	return val == want
}

// stringLit is a string literal with its source line.
type stringLit struct {
	value string
	line  int
}

// stringLiterals returns every string literal in the file with its line.
func stringLiterals(t *testing.T, path string) []stringLit {
	t.Helper()

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	var lits []stringLit
	ast.Inspect(node, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if val, err := strconv.Unquote(lit.Value); err == nil {
			lits = append(lits, stringLit{value: val, line: fset.Position(lit.Pos()).Line})
		}
		return true
	})
	return lits
}

// relForSafety returns the repo-root-relative path for cleaner diagnostics.
func relForSafety(t *testing.T, fullPath string) string {
	t.Helper()
	rel, err := filepath.Rel(repoRoot(t), fullPath)
	if err != nil {
		return fullPath
	}
	return filepath.ToSlash(rel)
}
