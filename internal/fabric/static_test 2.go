package fabric

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticScannerScan(t *testing.T) {
	t.Parallel()

	t.Run("empty phases", func(t *testing.T) {
		t.Parallel()

		scanner := &StaticScanner{WorkDir: t.TempDir()}
		contracts, err := scanner.Scan(nil)
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if len(contracts) != 0 {
			t.Errorf("got %d contracts, want 0", len(contracts))
		}
	})

	t.Run("phase with scope only", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		pkg := filepath.Join(dir, "widgets")
		if err := os.MkdirAll(pkg, 0o755); err != nil {
			t.Fatal(err)
		}
		src := `package widgets

type Widget struct {
	Name string
}

func NewWidget(name string) *Widget {
	return &Widget{Name: name}
}

func helper() {}
`
		if err := os.WriteFile(filepath.Join(pkg, "widget.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}

		scanner := &StaticScanner{WorkDir: dir}
		phases := []PhaseInput{
			{
				ID:    "phase-1",
				Scope: []string{"widgets/*.go"},
			},
		}

		contracts, err := scanner.Scan(phases)
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if len(contracts) != 1 {
			t.Fatalf("got %d contracts, want 1", len(contracts))
		}

		c := contracts[0]
		if c.PhaseID != "phase-1" {
			t.Errorf("PhaseID = %q, want %q", c.PhaseID, "phase-1")
		}
		if len(c.Scope) != 1 {
			t.Fatalf("got %d scope files, want 1", len(c.Scope))
		}

		// Should find Widget (type) and NewWidget (function), but not helper.
		got := make(map[string]bool)
		for _, p := range c.Produces {
			got[p.Kind+":"+p.Name] = true
		}
		wantKeys := []string{"type:Widget", "function:NewWidget"}
		for _, key := range wantKeys {
			if !got[key] {
				t.Errorf("missing produces %q; have %v", key, producesKeys(c))
			}
		}
		if got["function:helper"] {
			t.Error("unexported function helper should not be in produces")
		}

		// All produces should have correct producer and pending status.
		for _, p := range c.Produces {
			if p.Producer != "phase-1" {
				t.Errorf("produce %s producer = %q, want %q", p.Name, p.Producer, "phase-1")
			}
			if p.Status != StatusPending {
				t.Errorf("produce %s status = %q, want %q", p.Name, p.Status, StatusPending)
			}
		}
	})

	t.Run("phase with Files section only", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		pkg := filepath.Join(dir, "internal", "store")
		if err := os.MkdirAll(pkg, 0o755); err != nil {
			t.Fatal(err)
		}
		src := `package store

type Store interface {
	Get(key string) (string, error)
	Set(key, value string) error
}
`
		if err := os.WriteFile(filepath.Join(pkg, "store.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}

		body := `## Problem

We need a persistent store.

## Solution

Implement a SQLite-backed Store.

## Files

- ` + "`internal/store/store.go`" + ` — Store interface definition
- ` + "`internal/store/config.toml`" + ` — Configuration file

## Acceptance Criteria

- [ ] Store interface exists
`

		scanner := &StaticScanner{WorkDir: dir}
		phases := []PhaseInput{
			{ID: "phase-files", Body: body},
		}

		contracts, err := scanner.Scan(phases)
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if len(contracts) != 1 {
			t.Fatalf("got %d contracts, want 1", len(contracts))
		}

		c := contracts[0]
		got := make(map[string]bool)
		for _, p := range c.Produces {
			got[p.Kind+":"+p.Name] = true
		}

		// Should find Store interface and its methods from the existing Go file.
		wantKeys := []string{
			"interface:Store",
			"method:Store.Get",
			"method:Store.Set",
		}
		for _, key := range wantKeys {
			if !got[key] {
				t.Errorf("missing produces %q; have %v", key, producesKeys(c))
			}
		}

		// Should also have the config.toml as a file entanglement.
		if !got["file:internal/store/config.toml"] {
			t.Errorf("missing file entanglement for config.toml; have %v", producesKeys(c))
		}
	})

	t.Run("phase with inline code blocks for new files", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		body := `## Problem

We need new types.

## Solution

Create the following:

` + "```go\n" + `type StaticScanner struct {
    WorkDir string
}

func Scan(phases []Phase) error {
    return nil
}
` + "```\n" + `

## Files

- ` + "`internal/scanner/scanner.go`" + ` — Scanner implementation

## Acceptance Criteria

- [ ] Done
`

		scanner := &StaticScanner{WorkDir: dir}
		phases := []PhaseInput{
			{ID: "phase-inline", Body: body},
		}

		contracts, err := scanner.Scan(phases)
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}

		c := contracts[0]
		got := make(map[string]bool)
		for _, p := range c.Produces {
			got[p.Kind+":"+p.Name] = true
		}

		// Should extract StaticScanner type and Scan function from inline code.
		if !got["type:StaticScanner"] {
			t.Errorf("missing type:StaticScanner; have %v", producesKeys(c))
		}
		if !got["function:Scan"] {
			t.Errorf("missing function:Scan; have %v", producesKeys(c))
		}
	})

	t.Run("multi-phase contract cross-reference", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		pkg := filepath.Join(dir, "models")
		if err := os.MkdirAll(pkg, 0o755); err != nil {
			t.Fatal(err)
		}
		src := `package models

type User struct {
	ID   int
	Name string
}

func NewUser(name string) *User {
	return &User{Name: name}
}
`
		if err := os.WriteFile(filepath.Join(pkg, "user.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}

		producerBody := `## Files

- ` + "`models/user.go`" + ` — User model
`
		consumerBody := `## Problem

We need an API endpoint that uses the User type from the models package.
The endpoint should call NewUser to create users.

## Solution

Build a handler that accepts User objects.
`

		scanner := &StaticScanner{WorkDir: dir}
		phases := []PhaseInput{
			{ID: "producer", Body: producerBody, Scope: []string{"models/*.go"}},
			{ID: "consumer", Body: consumerBody, DependsOn: []string{"producer"}},
		}

		contracts, err := scanner.Scan(phases)
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if len(contracts) != 2 {
			t.Fatalf("got %d contracts, want 2", len(contracts))
		}

		// Producer should have produces.
		producer := contracts[0]
		if len(producer.Produces) == 0 {
			t.Fatal("producer has no produces")
		}

		producerGot := make(map[string]bool)
		for _, p := range producer.Produces {
			producerGot[p.Kind+":"+p.Name] = true
		}
		if !producerGot["type:User"] {
			t.Errorf("producer missing type:User; have %v", producesKeys(producer))
		}
		if !producerGot["function:NewUser"] {
			t.Errorf("producer missing function:NewUser; have %v", producesKeys(producer))
		}

		// Consumer should have consumes from cross-referencing.
		consumer := contracts[1]
		if len(consumer.Consumes) == 0 {
			t.Fatal("consumer has no consumes — cross-reference failed")
		}

		consumerGot := make(map[string]bool)
		for _, c := range consumer.Consumes {
			consumerGot[c.Kind+":"+c.Name] = true
		}
		if !consumerGot["type:User"] {
			t.Errorf("consumer missing consumed type:User; have %v", consumesKeys(consumer))
		}
		if !consumerGot["function:NewUser"] {
			t.Errorf("consumer missing consumed function:NewUser; have %v", consumesKeys(consumer))
		}

		// All consumed entanglements should reference the producer.
		for _, c := range consumer.Consumes {
			if c.Producer != "producer" {
				t.Errorf("consumed %s producer = %q, want %q", c.Name, c.Producer, "producer")
			}
			if c.Consumer != "consumer" {
				t.Errorf("consumed %s consumer = %q, want %q", c.Name, c.Consumer, "consumer")
			}
		}
	})

	t.Run("scope resolves directories", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		subdir := filepath.Join(dir, "pkg")
		if err := os.MkdirAll(subdir, 0o755); err != nil {
			t.Fatal(err)
		}

		// Regular Go file.
		if err := os.WriteFile(filepath.Join(subdir, "a.go"),
			[]byte("package pkg\n\ntype Alpha struct{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		// Test file — should be excluded.
		if err := os.WriteFile(filepath.Join(subdir, "a_test.go"),
			[]byte("package pkg\n\nimport \"testing\"\n\nfunc TestAlpha(t *testing.T) {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		scanner := &StaticScanner{WorkDir: dir}
		phases := []PhaseInput{
			{ID: "scope-dir", Scope: []string{"pkg"}},
		}

		contracts, err := scanner.Scan(phases)
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}

		c := contracts[0]
		if len(c.Scope) != 1 {
			t.Fatalf("got %d scope files, want 1 (test file excluded)", len(c.Scope))
		}
		if !strings.HasSuffix(c.Scope[0], "a.go") {
			t.Errorf("scope[0] = %q, want to end with a.go", c.Scope[0])
		}

		got := make(map[string]bool)
		for _, p := range c.Produces {
			got[p.Kind+":"+p.Name] = true
		}
		if !got["type:Alpha"] {
			t.Errorf("missing type:Alpha from directory scope")
		}
	})
}

func TestParseFilesSection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "standard files section",
			body: "## Problem\n\nSomething\n\n## Files\n\n- `internal/foo/bar.go` — Bar implementation\n- `internal/foo/baz.go` — Baz helper\n\n## Acceptance Criteria\n",
			want: []string{"internal/foo/bar.go", "internal/foo/baz.go"},
		},
		{
			name: "no files section",
			body: "## Problem\n\nNo files here\n\n## Solution\n\nDo stuff\n",
			want: nil,
		},
		{
			name: "files section with dash separator",
			body: "## Files\n\n- `config.yaml` - config file\n",
			want: []string{"config.yaml"},
		},
		{
			name: "deduplicates paths",
			body: "## Files\n\n- `a.go` — first\n- `a.go` — duplicate\n",
			want: []string{"a.go"},
		},
		{
			name: "stops at next header",
			body: "## Files\n\n- `a.go` — included\n\n## Other\n\n- `b.go` — excluded\n",
			want: []string{"a.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseFilesSection(tt.body)
			if len(got) != len(tt.want) {
				t.Fatalf("parseFilesSection() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseFilesSection()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestExtractCodeBlocks(t *testing.T) {
	t.Parallel()

	body := "Some text\n\n```go\ntype Foo struct{}\n```\n\nMore text\n\n```\nfunc Bar() {}\n```\n"
	blocks := extractCodeBlocks(body)
	if len(blocks) != 2 {
		t.Fatalf("got %d blocks, want 2", len(blocks))
	}
	if !strings.Contains(blocks[0], "Foo") {
		t.Errorf("block[0] = %q, want to contain Foo", blocks[0])
	}
	if !strings.Contains(blocks[1], "Bar") {
		t.Errorf("block[1] = %q, want to contain Bar", blocks[1])
	}
}

func TestExtractInlineSymbols(t *testing.T) {
	t.Parallel()

	body := "```go\ntype Scanner struct {\n    Dir string\n}\n\ntype Reader interface {\n    Read() error\n}\n\nfunc Process(input string) error {\n    return nil\n}\n\nfunc helper() {}\n```\n"

	syms := extractInlineSymbols(body, "phase-1", "internal/scan/scanner.go")

	got := make(map[string]bool)
	for _, s := range syms {
		got[s.Kind+":"+s.Name] = true
	}

	want := []string{
		"type:Scanner",
		"interface:Reader",
		"function:Process",
	}
	for _, key := range want {
		if !got[key] {
			t.Errorf("missing %q; have %v", key, got)
		}
	}

	// Unexported helper should not appear.
	if got["function:helper"] {
		t.Error("unexported function helper should not be extracted")
	}

	// Check package derivation.
	for _, s := range syms {
		if s.Package != "scan" {
			t.Errorf("symbol %s package = %q, want %q", s.Name, s.Package, "scan")
		}
	}
}

func TestContainsSymbolRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		text   string
		symbol string
		want   bool
	}{
		{"exact match", "Use the Widget type", "Widget", true},
		{"as part of word", "Use the WidgetFactory type", "Widget", false},
		{"at start", "Widget is used here", "Widget", true},
		{"at end", "create a Widget", "Widget", true},
		{"method name", "call Foo.Bar method", "Foo.Bar", true},
		{"method name partial", "use Bar method", "Foo.Bar", true},
		{"not present", "no match here", "Widget", false},
		{"empty symbol", "some text", "", false},
		{"with punctuation boundary", "Widget, and more", "Widget", true},
		{"in backticks", "use `Widget` type", "Widget", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := containsSymbolRef(tt.text, tt.symbol)
			if got != tt.want {
				t.Errorf("containsSymbolRef(%q, %q) = %v, want %v", tt.text, tt.symbol, got, tt.want)
			}
		})
	}
}

func TestResolveContracts(t *testing.T) {
	t.Parallel()

	t.Run("fulfilled contract", func(t *testing.T) {
		t.Parallel()

		contracts := []PhaseContract{
			{
				PhaseID: "producer",
				Produces: []Entanglement{
					{Kind: KindType, Name: "Widget", Package: "widgets"},
				},
			},
			{
				PhaseID: "consumer",
				Consumes: []Entanglement{
					{Kind: KindType, Name: "Widget", Package: "widgets", Producer: "producer", Consumer: "consumer"},
				},
			},
		}
		deps := map[string][]string{
			"consumer": {"producer"},
		}

		report := ResolveContracts(contracts, deps)

		if len(report.Fulfilled) != 1 {
			t.Fatalf("got %d fulfilled, want 1", len(report.Fulfilled))
		}
		if report.Fulfilled[0].Producer != "producer" {
			t.Errorf("fulfilled producer = %q, want %q", report.Fulfilled[0].Producer, "producer")
		}
		if report.Fulfilled[0].Consumer != "consumer" {
			t.Errorf("fulfilled consumer = %q, want %q", report.Fulfilled[0].Consumer, "consumer")
		}
		if len(report.Missing) != 0 {
			t.Errorf("got %d missing, want 0", len(report.Missing))
		}
	})

	t.Run("missing contract", func(t *testing.T) {
		t.Parallel()

		contracts := []PhaseContract{
			{
				PhaseID: "consumer",
				Consumes: []Entanglement{
					{Kind: KindType, Name: "Widget", Package: "widgets", Consumer: "consumer"},
				},
			},
		}
		deps := map[string][]string{}

		report := ResolveContracts(contracts, deps)

		if len(report.Missing) != 1 {
			t.Fatalf("got %d missing, want 1", len(report.Missing))
		}
		if report.Missing[0].Consumer != "consumer" {
			t.Errorf("missing consumer = %q, want %q", report.Missing[0].Consumer, "consumer")
		}
	})

	t.Run("conflicting producers", func(t *testing.T) {
		t.Parallel()

		contracts := []PhaseContract{
			{
				PhaseID: "phase-a",
				Produces: []Entanglement{
					{Kind: KindType, Name: "Config", Package: "config"},
				},
			},
			{
				PhaseID: "phase-b",
				Produces: []Entanglement{
					{Kind: KindType, Name: "Config", Package: "config"},
				},
			},
		}
		deps := map[string][]string{}

		report := ResolveContracts(contracts, deps)

		if len(report.Conflicts) != 2 {
			t.Fatalf("got %d conflicts, want 2 (one per producer)", len(report.Conflicts))
		}
		if len(report.Warnings) == 0 {
			t.Error("expected warning about conflicting producers")
		}
	})

	t.Run("producer not a dependency", func(t *testing.T) {
		t.Parallel()

		contracts := []PhaseContract{
			{
				PhaseID: "producer",
				Produces: []Entanglement{
					{Kind: KindFunction, Name: "Init", Package: "app"},
				},
			},
			{
				PhaseID: "consumer",
				Consumes: []Entanglement{
					{Kind: KindFunction, Name: "Init", Package: "app", Consumer: "consumer"},
				},
			},
		}
		// Consumer does NOT depend on producer.
		deps := map[string][]string{}

		report := ResolveContracts(contracts, deps)

		if len(report.Missing) != 1 {
			t.Fatalf("got %d missing, want 1 (producer not a dep)", len(report.Missing))
		}
		if len(report.Warnings) == 0 {
			t.Error("expected warning about producer not being a dependency")
		}
	})

	t.Run("empty contracts", func(t *testing.T) {
		t.Parallel()

		report := ResolveContracts(nil, nil)
		if len(report.Fulfilled) != 0 || len(report.Missing) != 0 ||
			len(report.Conflicts) != 0 || len(report.Warnings) != 0 {
			t.Error("expected empty report for nil contracts")
		}
	})
}

// producesKeys returns a string slice of "kind:name" for debugging.
func producesKeys(c PhaseContract) []string {
	var ks []string
	for _, p := range c.Produces {
		ks = append(ks, p.Kind+":"+p.Name)
	}
	return ks
}

// consumesKeys returns a string slice of "kind:name" for debugging.
func consumesKeys(c PhaseContract) []string {
	var ks []string
	for _, p := range c.Consumes {
		ks = append(ks, p.Kind+":"+p.Name)
	}
	return ks
}
