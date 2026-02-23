package board

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// mockBoard records calls for testing. It implements the Board interface.
type mockBoard struct {
	contracts  []Contract
	claims     map[string]string // filepath -> ownerPhaseID
	claimErr   error
	publishErr error
}

func newMockBoard() *mockBoard {
	return &mockBoard{claims: make(map[string]string)}
}

func (m *mockBoard) SetPhaseState(_ context.Context, _, _ string) error { return nil }
func (m *mockBoard) GetPhaseState(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (m *mockBoard) PublishContract(_ context.Context, c Contract) error {
	m.contracts = append(m.contracts, c)
	return m.publishErr
}

func (m *mockBoard) PublishContracts(_ context.Context, cs []Contract) error {
	m.contracts = append(m.contracts, cs...)
	return m.publishErr
}

func (m *mockBoard) ContractsFor(_ context.Context, _ string) ([]Contract, error) {
	return nil, nil
}
func (m *mockBoard) AllContracts(_ context.Context) ([]Contract, error) { return nil, nil }

func (m *mockBoard) ClaimFile(_ context.Context, fp, owner string) error {
	if m.claimErr != nil {
		return m.claimErr
	}
	m.claims[fp] = owner
	return nil
}

func (m *mockBoard) ReleaseClaims(_ context.Context, _ string) error       { return nil }
func (m *mockBoard) FileOwner(_ context.Context, _ string) (string, error) { return "", nil }
func (m *mockBoard) ClaimsFor(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (m *mockBoard) Close() error { return nil }

// initGitRepo creates a temporary git repo and returns its path.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "test")

	// Initial commit so we have a baseline SHA.
	initial := filepath.Join(dir, "README.md")
	if err := os.WriteFile(initial, []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "initial")
	return dir
}

// gitSHA returns the current HEAD SHA in the given repo.
func gitSHA(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// gitCommit stages all and commits.
func gitCommit(t *testing.T, dir, msg string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("add", ".")
	run("commit", "-m", msg)
}

func TestPublishPhase(t *testing.T) {
	t.Parallel()

	t.Run("extracts exported symbols from Go files", func(t *testing.T) {
		t.Parallel()

		dir := initGitRepo(t)
		beforeSHA := gitSHA(t, dir)

		// Create a Go file with exported symbols.
		goSrc := `package widgets

// Widget is a UI component.
type Widget struct {
	Name string
	Size int
}

// Renderer draws widgets.
type Renderer interface {
	Render(w Widget) error
	Reset()
}

// unexportedType should be skipped.
type unexportedType struct{}

// NewWidget creates a new Widget.
func NewWidget(name string, size int) *Widget {
	return &Widget{Name: name, Size: size}
}

// helper is unexported and should be skipped.
func helper() {}

// String returns the widget name.
func (w *Widget) String() string {
	return w.Name
}
`
		pkg := filepath.Join(dir, "widgets")
		if err := os.MkdirAll(pkg, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pkg, "widget.go"), []byte(goSrc), 0o644); err != nil {
			t.Fatal(err)
		}

		gitCommit(t, dir, "add widgets")
		afterSHA := gitSHA(t, dir)

		mb := newMockBoard()
		pub := &Publisher{Board: mb, WorkDir: dir}

		if err := pub.PublishPhase(context.Background(), "phase-1", beforeSHA, afterSHA); err != nil {
			t.Fatalf("PublishPhase: %v", err)
		}

		// Verify file claim.
		if owner, ok := mb.claims["widgets/widget.go"]; !ok || owner != "phase-1" {
			t.Errorf("file claim = %v, want phase-1 owning widgets/widget.go", mb.claims)
		}

		// Build a map of contract kind+name for easy checking.
		got := make(map[string]string) // "kind:name" -> signature
		for _, c := range mb.contracts {
			key := c.Kind + ":" + c.Name
			got[key] = c.Signature
		}

		// Verify expected contracts.
		wantKeys := []string{
			"file:widgets/widget.go",
			"type:Widget",
			"interface:Renderer",
			"method:Renderer.Render",
			"method:Renderer.Reset",
			"function:NewWidget",
			"method:Widget.String",
		}
		for _, key := range wantKeys {
			if _, ok := got[key]; !ok {
				t.Errorf("missing contract %q; have: %v", key, keys(got))
			}
		}

		// Verify unexported symbols are NOT present.
		unwantedKeys := []string{
			"type:unexportedType",
			"function:helper",
		}
		for _, key := range unwantedKeys {
			if _, ok := got[key]; ok {
				t.Errorf("unexpected contract %q should not be extracted", key)
			}
		}

		// Verify all contracts have correct producer and status.
		for _, c := range mb.contracts {
			if c.Producer != "phase-1" {
				t.Errorf("contract %s producer = %q, want %q", c.Name, c.Producer, "phase-1")
			}
			if c.Status != StatusFulfilled {
				t.Errorf("contract %s status = %q, want %q", c.Name, c.Status, StatusFulfilled)
			}
		}
	})

	t.Run("non-Go files produce file contracts only", func(t *testing.T) {
		t.Parallel()

		dir := initGitRepo(t)
		beforeSHA := gitSHA(t, dir)

		// Create a non-Go file.
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[server]\nport = 8080\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitCommit(t, dir, "add config")
		afterSHA := gitSHA(t, dir)

		mb := newMockBoard()
		pub := &Publisher{Board: mb, WorkDir: dir}

		if err := pub.PublishPhase(context.Background(), "phase-2", beforeSHA, afterSHA); err != nil {
			t.Fatalf("PublishPhase: %v", err)
		}

		if len(mb.contracts) != 1 {
			t.Fatalf("got %d contracts, want 1", len(mb.contracts))
		}
		c := mb.contracts[0]
		if c.Kind != KindFile {
			t.Errorf("kind = %q, want %q", c.Kind, KindFile)
		}
		if c.Name != "config.toml" {
			t.Errorf("name = %q, want %q", c.Name, "config.toml")
		}
	})

	t.Run("skips test files", func(t *testing.T) {
		t.Parallel()

		dir := initGitRepo(t)
		beforeSHA := gitSHA(t, dir)

		// Create a test file — should only get a file contract, not symbols.
		testSrc := `package foo

import "testing"

func TestFoo(t *testing.T) {}
`
		if err := os.MkdirAll(filepath.Join(dir, "foo"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "foo", "foo_test.go"), []byte(testSrc), 0o644); err != nil {
			t.Fatal(err)
		}
		gitCommit(t, dir, "add test")
		afterSHA := gitSHA(t, dir)

		mb := newMockBoard()
		pub := &Publisher{Board: mb, WorkDir: dir}

		if err := pub.PublishPhase(context.Background(), "phase-3", beforeSHA, afterSHA); err != nil {
			t.Fatalf("PublishPhase: %v", err)
		}

		// Only a file contract should exist.
		if len(mb.contracts) != 1 {
			t.Fatalf("got %d contracts, want 1 (file only)", len(mb.contracts))
		}
		if mb.contracts[0].Kind != KindFile {
			t.Errorf("kind = %q, want %q", mb.contracts[0].Kind, KindFile)
		}
	})

	t.Run("handles parse errors gracefully", func(t *testing.T) {
		t.Parallel()

		dir := initGitRepo(t)
		beforeSHA := gitSHA(t, dir)

		// Create a malformed Go file.
		badSrc := `package bad

func Broken( {
`
		if err := os.MkdirAll(filepath.Join(dir, "bad"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "bad", "bad.go"), []byte(badSrc), 0o644); err != nil {
			t.Fatal(err)
		}
		gitCommit(t, dir, "add bad file")
		afterSHA := gitSHA(t, dir)

		var logBuf bytes.Buffer
		mb := newMockBoard()
		pub := &Publisher{Board: mb, WorkDir: dir, Logger: &logBuf}

		if err := pub.PublishPhase(context.Background(), "phase-4", beforeSHA, afterSHA); err != nil {
			t.Fatalf("PublishPhase should not fail on parse errors: %v", err)
		}

		// Should still produce a file contract.
		if len(mb.contracts) != 1 {
			t.Fatalf("got %d contracts, want 1 (file only)", len(mb.contracts))
		}
		if mb.contracts[0].Kind != KindFile {
			t.Errorf("kind = %q, want %q", mb.contracts[0].Kind, KindFile)
		}

		// Logger should have received a warning.
		if !strings.Contains(logBuf.String(), "publisher: parse") {
			t.Errorf("expected parse warning in log, got: %q", logBuf.String())
		}
	})

	t.Run("no changes yields no contracts", func(t *testing.T) {
		t.Parallel()

		dir := initGitRepo(t)
		sha := gitSHA(t, dir)

		mb := newMockBoard()
		pub := &Publisher{Board: mb, WorkDir: dir}

		if err := pub.PublishPhase(context.Background(), "phase-5", sha, sha); err != nil {
			t.Fatalf("PublishPhase: %v", err)
		}
		if len(mb.contracts) != 0 {
			t.Errorf("got %d contracts, want 0", len(mb.contracts))
		}
	})
}

func TestExtractGoSymbols(t *testing.T) {
	t.Parallel()

	t.Run("function signatures include parameters and returns", func(t *testing.T) {
		t.Parallel()

		src := `package api

func Fetch(url string, timeout int) ([]byte, error) {
	return nil, nil
}
`
		dir := t.TempDir()
		path := filepath.Join(dir, "api.go")
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}

		pub := &Publisher{}
		contracts, err := pub.extractGoSymbols(path)
		if err != nil {
			t.Fatalf("extractGoSymbols: %v", err)
		}

		if len(contracts) != 1 {
			t.Fatalf("got %d contracts, want 1", len(contracts))
		}
		c := contracts[0]
		if c.Kind != KindFunction {
			t.Errorf("kind = %q, want %q", c.Kind, KindFunction)
		}
		if !strings.Contains(c.Signature, "url string") {
			t.Errorf("signature missing params: %q", c.Signature)
		}
		if !strings.Contains(c.Signature, "[]byte, error") {
			t.Errorf("signature missing returns: %q", c.Signature)
		}
	})

	t.Run("method receiver is included in name", func(t *testing.T) {
		t.Parallel()

		src := `package svc

type Server struct{}

func (s *Server) Start() error {
	return nil
}
`
		dir := t.TempDir()
		path := filepath.Join(dir, "svc.go")
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}

		pub := &Publisher{}
		contracts, err := pub.extractGoSymbols(path)
		if err != nil {
			t.Fatalf("extractGoSymbols: %v", err)
		}

		// Should have: Server (type) + Server.Start (method)
		got := make(map[string]Contract)
		for _, c := range contracts {
			got[c.Kind+":"+c.Name] = c
		}

		mc, ok := got["method:Server.Start"]
		if !ok {
			t.Fatalf("missing method:Server.Start; have %v", keys2(got))
		}
		if !strings.Contains(mc.Signature, "Server") {
			t.Errorf("method signature should include receiver: %q", mc.Signature)
		}
	})

	t.Run("interface methods are extracted", func(t *testing.T) {
		t.Parallel()

		src := `package store

type Store interface {
	Get(key string) (string, error)
	Set(key, value string) error
	unexported() // should be skipped
}
`
		dir := t.TempDir()
		path := filepath.Join(dir, "store.go")
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}

		pub := &Publisher{}
		contracts, err := pub.extractGoSymbols(path)
		if err != nil {
			t.Fatalf("extractGoSymbols: %v", err)
		}

		got := make(map[string]string) // kind:name -> kind
		for _, c := range contracts {
			got[c.Kind+":"+c.Name] = c.Kind
		}

		want := []string{
			"interface:Store",
			"method:Store.Get",
			"method:Store.Set",
		}
		for _, key := range want {
			if _, ok := got[key]; !ok {
				t.Errorf("missing %q; have %v", key, keys(got))
			}
		}

		// unexported method should not appear.
		if _, ok := got["method:Store.unexported"]; ok {
			t.Error("unexported interface method should not be extracted")
		}
	})
}

func TestPackageFromPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want string
	}{
		{"internal/board/publisher.go", "board"},
		{"cmd/root.go", "cmd"},
		{"main.go", ""},
		{"a/b/c/deep.go", "c"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			got := packageFromPath(tt.path)
			if got != tt.want {
				t.Errorf("packageFromPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// keys returns sorted keys from a map for error messages.
func keys(m map[string]string) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// keys2 returns sorted keys from a contract map.
func keys2(m map[string]Contract) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
