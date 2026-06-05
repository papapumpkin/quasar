package arch_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/sensors"
)

// githubAdapterPkg is the import path of the GitHub sensor adapter. Only its own
// package and the cmd layer (via a blank side-effect import for registry init)
// may reference it; everything else must go through the sensor registry.
const githubAdapterPkg = modulePath + "/internal/sensors/github"

// TestSensorsLayering enforces that no internal package outside the github
// adapter itself imports the github adapter package. Concrete adapters are an
// implementation detail behind the sensor registry; importing one directly
// would couple a caller to a specific tracker and defeat the forge-agnostic
// boundary. The cmd layer is permitted (blank side-effect import to trigger
// registry init) and is not under internal/, so it is not scanned here.
func TestSensorsLayering(t *testing.T) {
	t.Parallel()

	dir := internalDirPath(t)
	for _, pkg := range internalPackages(t) {
		if pkg == "sensors" {
			// The sensors parent package and its github subpackage are allowed
			// to reference github internally.
			continue
		}
		for _, imp := range fullImportsOf(t, filepath.Join(dir, pkg)) {
			if imp == githubAdapterPkg {
				t.Errorf("layering violation: internal/%s imports the github adapter directly; depend on internal/sensors and the registry instead", pkg)
			}
		}
	}
}

// TestNoInlineTokens scans committed config files for an inline `token:` field.
// Tokens must be supplied via token_env or token_file so secrets never live in
// version control. This guards the same invariant config.Load enforces at
// runtime, but at the level of files checked into the repo.
func TestNoInlineTokens(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	scanDirs := []string{filepath.Join(root, "configs")}

	// Include any committed .quasar.yaml at the repo root.
	if _, err := os.Stat(filepath.Join(root, ".quasar.yaml")); err == nil {
		scanDirs = append(scanDirs, root)
	}

	for _, dir := range scanDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatalf("read dir %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !isYAML(e.Name()) {
				continue
			}
			path := filepath.Join(dir, e.Name())
			assertNoInlineToken(t, path)
		}
	}
}

// TestForgeStubMinimal asserts the Forge interface exposes exactly one method
// (Name). The write-side surface (PR creation, comment polling, status sync) is
// deliberately reserved for a later nebula so the config schema and registry can
// be uniform without committing to method signatures prematurely.
//
// To expand Forge in that later nebula: add the methods here, update this
// expected count, and document the rollout in docs/safety.md. The failure this
// test produces is the intended tripwire — it catches a speculative method added
// without the corresponding rollout.
func TestForgeStubMinimal(t *testing.T) {
	t.Parallel()

	const expectedMethods = 1
	forgeType := reflect.TypeOf((*sensors.Forge)(nil)).Elem()
	if got := forgeType.NumMethod(); got != expectedMethods {
		t.Errorf("Forge interface has %d methods, want %d; if expanding the forge surface, do the full rollout (see the test doc and docs/safety.md)", got, expectedMethods)
	}
	if _, ok := forgeType.MethodByName("Name"); !ok {
		t.Error("Forge interface must declare Name()")
	}
}

// fullImportsOf parses all non-test Go files in pkgDir and returns the complete
// set of imported package paths (not just the internal short names). It is the
// full-path complement to helpers_test.go's importsOf.
func fullImportsOf(t *testing.T, pkgDir string) []string {
	t.Helper()

	fset := token.NewFileSet()
	seen := make(map[string]bool)
	for _, f := range goFilesIn(t, pkgDir) {
		node, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing imports in %s: %v", f, err)
		}
		for _, imp := range node.Imports {
			seen[strings.Trim(imp.Path.Value, `"`)] = true
		}
	}
	var result []string
	for p := range seen {
		result = append(result, p)
	}
	return result
}

// isYAML reports whether name has a YAML extension.
func isYAML(name string) bool {
	return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")
}

// assertNoInlineToken fails the test if the file at path contains a line whose
// first key is `token` (case-insensitive). Comments and the token_env /
// token_file keys are not matched.
func assertNoInlineToken(t *testing.T, path string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	for i, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, _, found := strings.Cut(trimmed, ":")
		if !found {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(key), "token") {
			t.Errorf("%s:%d defines an inline token; use token_env or token_file instead", path, i+1)
		}
	}
}
