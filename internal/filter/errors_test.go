package filter

import (
	"strings"
	"testing"
)

func TestParseCheckOutput(t *testing.T) {
	t.Parallel()

	t.Run("DispatchesBuild", func(t *testing.T) {
		t.Parallel()
		cr := CheckResult{
			Name:   "build",
			Output: "./internal/loop/loop.go:42:15: undefined: foo",
		}
		pr := ParseCheckOutput(cr)
		if pr.CheckName != "build" {
			t.Errorf("CheckName = %q, want %q", pr.CheckName, "build")
		}
		if pr.RawOutput != cr.Output {
			t.Error("RawOutput should preserve original output")
		}
		if len(pr.Errors) != 1 {
			t.Fatalf("expected 1 error, got %d", len(pr.Errors))
		}
		if pr.Errors[0].Tool != "build" {
			t.Errorf("Tool = %q, want %q", pr.Errors[0].Tool, "build")
		}
	})

	t.Run("DispatchesVet", func(t *testing.T) {
		t.Parallel()
		cr := CheckResult{
			Name:   "vet",
			Output: "internal/loop/loop.go:10:2: printf: Sprintf format has no verbs",
		}
		pr := ParseCheckOutput(cr)
		if pr.CheckName != "vet" {
			t.Errorf("CheckName = %q, want %q", pr.CheckName, "vet")
		}
		if len(pr.Errors) != 1 {
			t.Fatalf("expected 1 error, got %d", len(pr.Errors))
		}
		if pr.Errors[0].Tool != "vet" {
			t.Errorf("Tool = %q, want %q", pr.Errors[0].Tool, "vet")
		}
	})

	t.Run("DispatchesLint", func(t *testing.T) {
		t.Parallel()
		cr := CheckResult{
			Name:   "lint",
			Output: "internal/loop/loop.go:42:15: SA1029: should not use built-in type string (staticcheck)",
		}
		pr := ParseCheckOutput(cr)
		if pr.CheckName != "lint" {
			t.Errorf("CheckName = %q, want %q", pr.CheckName, "lint")
		}
		if len(pr.Errors) != 1 {
			t.Fatalf("expected 1 error, got %d", len(pr.Errors))
		}
		if pr.Errors[0].Tool != "lint" {
			t.Errorf("Tool = %q, want %q", pr.Errors[0].Tool, "lint")
		}
	})

	t.Run("DispatchesTest", func(t *testing.T) {
		t.Parallel()
		cr := CheckResult{
			Name:   "test",
			Output: "main_test.go:12:3: undefined: nonexistent",
		}
		pr := ParseCheckOutput(cr)
		if pr.CheckName != "test" {
			t.Errorf("CheckName = %q, want %q", pr.CheckName, "test")
		}
		if len(pr.Errors) != 1 {
			t.Fatalf("expected 1 error, got %d", len(pr.Errors))
		}
		if pr.Errors[0].Tool != "test" {
			t.Errorf("Tool = %q, want %q", pr.Errors[0].Tool, "test")
		}
	})

	t.Run("UnknownCheckReturnsEmpty", func(t *testing.T) {
		t.Parallel()
		cr := CheckResult{
			Name:   "claims",
			Output: "file.go is owned by another task",
		}
		pr := ParseCheckOutput(cr)
		if len(pr.Errors) != 0 {
			t.Errorf("expected 0 errors for unknown check, got %d", len(pr.Errors))
		}
		if pr.RawOutput != cr.Output {
			t.Error("RawOutput should still be preserved")
		}
	})

	t.Run("EmptyOutput", func(t *testing.T) {
		t.Parallel()
		cr := CheckResult{Name: "build", Output: ""}
		pr := ParseCheckOutput(cr)
		if len(pr.Errors) != 0 {
			t.Errorf("expected 0 errors for empty output, got %d", len(pr.Errors))
		}
	})
}

func TestParseBuildErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantLen int
		check   func(t *testing.T, errs []Error)
	}{
		{
			name: "SingleError",
			input: `# github.com/aaronsalm/quasar/internal/loop
./internal/loop/loop.go:42:15: undefined: foo`,
			wantLen: 1,
			check: func(t *testing.T, errs []Error) {
				e := errs[0]
				if e.File != "internal/loop/loop.go" {
					t.Errorf("File = %q, want %q", e.File, "internal/loop/loop.go")
				}
				if e.Line != 42 {
					t.Errorf("Line = %d, want %d", e.Line, 42)
				}
				if e.Column != 15 {
					t.Errorf("Column = %d, want %d", e.Column, 15)
				}
				if e.Message != "undefined: foo" {
					t.Errorf("Message = %q, want %q", e.Message, "undefined: foo")
				}
				if e.Tool != "build" {
					t.Errorf("Tool = %q, want %q", e.Tool, "build")
				}
			},
		},
		{
			name: "MultipleErrors",
			input: `# github.com/aaronsalm/quasar/internal/loop
./internal/loop/loop.go:42:15: undefined: foo
./internal/loop/loop.go:50:2: syntax error: unexpected newline
./internal/loop/state.go:10:5: cannot use x (variable of type int) as string`,
			wantLen: 3,
			check: func(t *testing.T, errs []Error) {
				if errs[0].Line != 42 {
					t.Errorf("first error line = %d, want 42", errs[0].Line)
				}
				if errs[1].Line != 50 {
					t.Errorf("second error line = %d, want 50", errs[1].Line)
				}
				if errs[2].File != "internal/loop/state.go" {
					t.Errorf("third error file = %q, want %q", errs[2].File, "internal/loop/state.go")
				}
			},
		},
		{
			name: "MultiplePackageHeaders",
			input: `# github.com/aaronsalm/quasar/internal/loop
./internal/loop/loop.go:42:15: undefined: foo
# github.com/aaronsalm/quasar/internal/filter
./internal/filter/chain.go:10:5: too many arguments`,
			wantLen: 2,
			check: func(t *testing.T, errs []Error) {
				if errs[0].File != "internal/loop/loop.go" {
					t.Errorf("first error file = %q", errs[0].File)
				}
				if errs[1].File != "internal/filter/chain.go" {
					t.Errorf("second error file = %q", errs[1].File)
				}
			},
		},
		{
			name:    "SkipsNonMatchingLines",
			input:   "some random text\nanother line without errors\n",
			wantLen: 0,
		},
		{
			name:    "EmptyInput",
			input:   "",
			wantLen: 0,
		},
		{
			name:    "PathWithoutDotSlash",
			input:   `internal/loop/loop.go:42:15: undefined: bar`,
			wantLen: 1,
			check: func(t *testing.T, errs []Error) {
				if errs[0].File != "internal/loop/loop.go" {
					t.Errorf("File = %q, want %q", errs[0].File, "internal/loop/loop.go")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			errs := parseBuildErrors(tt.input)
			if len(errs) != tt.wantLen {
				t.Fatalf("got %d errors, want %d", len(errs), tt.wantLen)
			}
			if tt.check != nil {
				tt.check(t, errs)
			}
		})
	}
}

func TestParseVetErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantLen int
		check   func(t *testing.T, errs []Error)
	}{
		{
			name: "StandardVetOutput",
			input: `# github.com/aaronsalm/quasar/internal/loop
./internal/loop/loop.go:10:2: printf: Sprintf format %d has arg of wrong type string`,
			wantLen: 1,
			check: func(t *testing.T, errs []Error) {
				e := errs[0]
				if e.File != "internal/loop/loop.go" {
					t.Errorf("File = %q", e.File)
				}
				if e.Line != 10 {
					t.Errorf("Line = %d", e.Line)
				}
				if e.Tool != "vet" {
					t.Errorf("Tool = %q", e.Tool)
				}
			},
		},
		{
			name: "MultipleVetErrors",
			input: `# github.com/aaronsalm/quasar/cmd
./cmd/run.go:25:3: unreachable code
./cmd/run.go:30:5: result of fmt.Sprintf call not used`,
			wantLen: 2,
			check: func(t *testing.T, errs []Error) {
				if errs[0].Message != "unreachable code" {
					t.Errorf("first message = %q", errs[0].Message)
				}
			},
		},
		{
			name:    "EmptyInput",
			input:   "",
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			errs := parseVetErrors(tt.input)
			if len(errs) != tt.wantLen {
				t.Fatalf("got %d errors, want %d", len(errs), tt.wantLen)
			}
			if tt.check != nil {
				tt.check(t, errs)
			}
		})
	}
}

func TestParseLintErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantLen int
		check   func(t *testing.T, errs []Error)
	}{
		{
			name:    "SingleLintError",
			input:   `internal/loop/loop.go:42:15: SA1029: should not use built-in type string as key for value (staticcheck)`,
			wantLen: 1,
			check: func(t *testing.T, errs []Error) {
				e := errs[0]
				if e.File != "internal/loop/loop.go" {
					t.Errorf("File = %q", e.File)
				}
				if e.Line != 42 {
					t.Errorf("Line = %d", e.Line)
				}
				if e.Column != 15 {
					t.Errorf("Column = %d", e.Column)
				}
				if e.Tool != "lint" {
					t.Errorf("Tool = %q", e.Tool)
				}
				if !strings.Contains(e.Message, "[staticcheck]") {
					t.Errorf("Message should contain linter name, got %q", e.Message)
				}
				// The trailing "(staticcheck)" should be reformatted to "[staticcheck]"
				if strings.Contains(e.Message, "(staticcheck)") {
					t.Errorf("Message should not contain parens-wrapped linter, got %q", e.Message)
				}
			},
		},
		{
			name: "MultipleLinters",
			input: `internal/loop/loop.go:10:5: error return value not checked (errcheck)
internal/loop/loop.go:20:3: func is unused (unused)`,
			wantLen: 2,
			check: func(t *testing.T, errs []Error) {
				if !strings.Contains(errs[0].Message, "[errcheck]") {
					t.Errorf("first message = %q, want [errcheck]", errs[0].Message)
				}
				if !strings.Contains(errs[1].Message, "[unused]") {
					t.Errorf("second message = %q, want [unused]", errs[1].Message)
				}
			},
		},
		{
			name:    "NoTrailingLinterName",
			input:   `internal/loop/loop.go:10:5: some generic warning`,
			wantLen: 1,
			check: func(t *testing.T, errs []Error) {
				if errs[0].Message != "some generic warning" {
					t.Errorf("Message = %q", errs[0].Message)
				}
			},
		},
		{
			name:    "EmptyInput",
			input:   "",
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			errs := parseLintErrors(tt.input)
			if len(errs) != tt.wantLen {
				t.Fatalf("got %d errors, want %d", len(errs), tt.wantLen)
			}
			if tt.check != nil {
				tt.check(t, errs)
			}
		})
	}
}

func TestParseTestErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantLen int
		check   func(t *testing.T, errs []Error)
	}{
		{
			name: "CompilationError",
			input: `# testmod
./main_test.go:12:3: undefined: nonexistent
FAIL	testmod [build failed]`,
			wantLen: 1,
			check: func(t *testing.T, errs []Error) {
				e := errs[0]
				if e.File != "main_test.go" {
					t.Errorf("File = %q", e.File)
				}
				if e.Line != 12 {
					t.Errorf("Line = %d", e.Line)
				}
				if e.Column != 3 {
					t.Errorf("Column = %d", e.Column)
				}
				if e.Tool != "test" {
					t.Errorf("Tool = %q", e.Tool)
				}
			},
		},
		{
			name: "TestAssertionFailure",
			input: `--- FAIL: TestAdd (0.00s)
    main_test.go:15: expected 99, got 3
FAIL
FAIL	testmod	0.005s`,
			wantLen: 1,
			check: func(t *testing.T, errs []Error) {
				e := errs[0]
				if e.File != "main_test.go" {
					t.Errorf("File = %q", e.File)
				}
				if e.Line != 15 {
					t.Errorf("Line = %d", e.Line)
				}
				if e.Column != 0 {
					t.Errorf("Column = %d, want 0", e.Column)
				}
				if !strings.Contains(e.Message, "[TestAdd]") {
					t.Errorf("Message should contain test name, got %q", e.Message)
				}
				if !strings.Contains(e.Message, "expected 99") {
					t.Errorf("Message should contain assertion, got %q", e.Message)
				}
			},
		},
		{
			name: "MultipleTestFailures",
			input: `--- FAIL: TestFoo (0.00s)
    foo_test.go:10: wrong value
--- FAIL: TestBar (0.01s)
    bar_test.go:20: missing field`,
			wantLen: 2,
			check: func(t *testing.T, errs []Error) {
				if errs[0].File != "foo_test.go" {
					t.Errorf("first file = %q", errs[0].File)
				}
				if !strings.Contains(errs[0].Message, "[TestFoo]") {
					t.Errorf("first message = %q", errs[0].Message)
				}
				if errs[1].File != "bar_test.go" {
					t.Errorf("second file = %q", errs[1].File)
				}
				if !strings.Contains(errs[1].Message, "[TestBar]") {
					t.Errorf("second message = %q", errs[1].Message)
				}
			},
		},
		{
			name: "PanicStackTrace",
			input: `goroutine 1 [running]:
main.doSomething()
	/home/user/project/internal/loop/loop.go:55 +0x1a4
main.main()
	/home/user/project/main.go:10 +0x25`,
			wantLen: 2,
			check: func(t *testing.T, errs []Error) {
				// Panic traces don't strip ./ prefix since they use absolute paths.
				if errs[0].Line != 55 {
					t.Errorf("first panic line = %d", errs[0].Line)
				}
				if errs[0].Message != "panic stack trace" {
					t.Errorf("first message = %q", errs[0].Message)
				}
				if errs[1].Line != 10 {
					t.Errorf("second panic line = %d", errs[1].Line)
				}
			},
		},
		{
			name: "MixedCompilationAndAssertionFailures",
			input: `# testmod
./main_test.go:5:3: undefined: helper
--- FAIL: TestOther (0.00s)
    main_test.go:20: expected true
FAIL	testmod	0.003s`,
			wantLen: 2,
			check: func(t *testing.T, errs []Error) {
				if errs[0].Line != 5 {
					t.Errorf("compilation error line = %d", errs[0].Line)
				}
				if errs[0].Column != 3 {
					t.Errorf("compilation error column = %d", errs[0].Column)
				}
				if errs[1].Line != 20 {
					t.Errorf("assertion failure line = %d", errs[1].Line)
				}
			},
		},
		{
			name:    "EmptyInput",
			input:   "",
			wantLen: 0,
		},
		{
			name: "OnlyFAILSummaryLines",
			input: `FAIL
FAIL	testmod	0.003s
FAIL`,
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			errs := parseTestErrors(tt.input)
			if len(errs) != tt.wantLen {
				t.Fatalf("got %d errors, want %d", len(errs), tt.wantLen)
			}
			if tt.check != nil {
				tt.check(t, errs)
			}
		})
	}
}

func TestDedup(t *testing.T) {
	t.Parallel()

	t.Run("RemovesDuplicates", func(t *testing.T) {
		t.Parallel()
		errs := []Error{
			{File: "a.go", Line: 10, Message: "undefined: foo", Tool: "build"},
			{File: "a.go", Line: 10, Message: "undefined: foo", Tool: "build"},
			{File: "a.go", Line: 10, Message: "undefined: foo", Tool: "vet"},
		}
		result := dedup(errs)
		// Two unique (File, Line, Message) triples — the tool field is not part of the key.
		if len(result) != 1 {
			t.Fatalf("got %d, want 1 (same file:line:message dedups regardless of tool)", len(result))
		}
	})

	t.Run("PreservesDifferentMessages", func(t *testing.T) {
		t.Parallel()
		errs := []Error{
			{File: "a.go", Line: 10, Message: "undefined: foo", Tool: "build"},
			{File: "a.go", Line: 10, Message: "cannot use x as y", Tool: "build"},
		}
		result := dedup(errs)
		if len(result) != 2 {
			t.Fatalf("got %d, want 2 (different messages on same line)", len(result))
		}
	})

	t.Run("PreservesDifferentLines", func(t *testing.T) {
		t.Parallel()
		errs := []Error{
			{File: "a.go", Line: 10, Message: "undefined: foo", Tool: "build"},
			{File: "a.go", Line: 20, Message: "undefined: foo", Tool: "build"},
		}
		result := dedup(errs)
		if len(result) != 2 {
			t.Fatalf("got %d, want 2 (different lines)", len(result))
		}
	})

	t.Run("NilInput", func(t *testing.T) {
		t.Parallel()
		result := dedup(nil)
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})

	t.Run("EmptyInput", func(t *testing.T) {
		t.Parallel()
		result := dedup([]Error{})
		if len(result) != 0 {
			t.Errorf("expected empty, got %d", len(result))
		}
	})
}

func TestCleanPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"./internal/loop/loop.go", "internal/loop/loop.go"},
		{"internal/loop/loop.go", "internal/loop/loop.go"},
		{"./main.go", "main.go"},
		{"main.go", "main.go"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := cleanPath(tt.input)
			if got != tt.want {
				t.Errorf("cleanPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestAtoi(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  int
	}{
		{"42", 42},
		{"0", 0},
		{"", 0},
		{"abc", 0},
		{"1000", 1000},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := atoi(tt.input)
			if got != tt.want {
				t.Errorf("atoi(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestRealWorldBuildOutput tests with a realistic multi-package build failure.
func TestRealWorldBuildOutput(t *testing.T) {
	t.Parallel()

	input := `# github.com/aaronsalm/quasar/internal/loop
./internal/loop/loop.go:42:15: undefined: runFilterFixLoop
./internal/loop/loop.go:58:9: cannot use result (variable of type *filter.ParseResult) as *filter.Result value in assignment
# github.com/aaronsalm/quasar/cmd
./cmd/run.go:115:28: too many arguments in call to loop.New`

	pr := ParseCheckOutput(CheckResult{Name: "build", Output: input})

	if len(pr.Errors) != 3 {
		t.Fatalf("expected 3 errors, got %d", len(pr.Errors))
	}

	if pr.Errors[0].File != "internal/loop/loop.go" || pr.Errors[0].Line != 42 {
		t.Errorf("first error = %+v", pr.Errors[0])
	}
	if pr.Errors[1].File != "internal/loop/loop.go" || pr.Errors[1].Line != 58 {
		t.Errorf("second error = %+v", pr.Errors[1])
	}
	if pr.Errors[2].File != "cmd/run.go" || pr.Errors[2].Line != 115 {
		t.Errorf("third error = %+v", pr.Errors[2])
	}
}

// TestRealWorldLintOutput tests with realistic golangci-lint output.
func TestRealWorldLintOutput(t *testing.T) {
	t.Parallel()

	input := `internal/loop/loop.go:42:15: G104: Errors unhandled (gosec)
internal/loop/prompts.go:10:2: ` + "`" + `magic` + "`" + ` is a magic number (mnd)
internal/filter/chain.go:50:3: ifElseChain: can be replaced with switch (gocritic)`

	pr := ParseCheckOutput(CheckResult{Name: "lint", Output: input})

	if len(pr.Errors) != 3 {
		t.Fatalf("expected 3 errors, got %d", len(pr.Errors))
	}

	if !strings.Contains(pr.Errors[0].Message, "[gosec]") {
		t.Errorf("first error should have [gosec], got %q", pr.Errors[0].Message)
	}
	if !strings.Contains(pr.Errors[2].Message, "[gocritic]") {
		t.Errorf("third error should have [gocritic], got %q", pr.Errors[2].Message)
	}
}

// TestRealWorldTestOutput tests with realistic go test failure output.
func TestRealWorldTestOutput(t *testing.T) {
	t.Parallel()

	input := `=== RUN   TestChainRun
=== RUN   TestChainRun/StopsOnFirstFailure
--- FAIL: TestChainRun (0.01s)
    --- FAIL: TestChainRun/StopsOnFirstFailure (0.00s)
        chain_test.go:94: expected failure output, got ""
=== RUN   TestParseErrors
--- FAIL: TestParseErrors (0.00s)
    errors_test.go:25: got 0 errors, want 1
FAIL
FAIL	github.com/aaronsalm/quasar/internal/filter	0.012s`

	pr := ParseCheckOutput(CheckResult{Name: "test", Output: input})

	if len(pr.Errors) != 2 {
		t.Fatalf("expected 2 errors, got %d", len(pr.Errors))
	}

	if pr.Errors[0].File != "chain_test.go" || pr.Errors[0].Line != 94 {
		t.Errorf("first error = %+v", pr.Errors[0])
	}
	if pr.Errors[1].File != "errors_test.go" || pr.Errors[1].Line != 25 {
		t.Errorf("second error = %+v", pr.Errors[1])
	}
}

// TestDedupViaParseCheckOutput verifies deduplication through the public API.
func TestDedupViaParseCheckOutput(t *testing.T) {
	t.Parallel()

	// Simulate go build repeating the same error from two importing packages.
	input := `# github.com/aaronsalm/quasar/internal/loop
./internal/loop/loop.go:42:15: undefined: foo
# github.com/aaronsalm/quasar/cmd
./internal/loop/loop.go:42:15: undefined: foo`

	pr := ParseCheckOutput(CheckResult{Name: "build", Output: input})

	if len(pr.Errors) != 1 {
		t.Fatalf("expected 1 deduplicated error, got %d", len(pr.Errors))
	}
}
