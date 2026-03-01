package filter

import (
	"regexp"
	"strconv"
	"strings"
)

// FilterError represents a single structured error extracted from tool output.
type FilterError struct {
	File    string // relative file path (e.g. "internal/loop/loop.go")
	Line    int    // 1-based line number, 0 if unknown
	Column  int    // 1-based column, 0 if unknown
	Message string // the error message text
	Tool    string // which tool produced this: "build", "vet", "lint", "test"
}

// ParseResult holds all errors extracted from a single check's output.
type ParseResult struct {
	Errors    []FilterError // structured errors, may be empty if parsing fails
	RawOutput string        // original output preserved as fallback
	CheckName string        // "build", "vet", "lint", "test"
}

// errLineRe matches the standard Go toolchain error format: file.go:line:col: message
var errLineRe = regexp.MustCompile(`^([^\s:]+\.go):(\d+):(\d+):\s*(.+)$`)

// errLineNoColRe matches file.go:line: message (no column), used by test assertions.
var errLineNoColRe = regexp.MustCompile(`^([^\s:]+\.go):(\d+):\s*(.+)$`)

// testFailRe matches test failure lines: --- FAIL: TestName (duration)
var testFailRe = regexp.MustCompile(`^--- FAIL: (\S+)\s+\(`)

// panicTraceRe matches panic stack trace lines: file.go:123 +0x...
var panicTraceRe = regexp.MustCompile(`^\s+([^\s:]+\.go):(\d+)\s+\+0x`)

// linterNameRe matches a trailing parenthesized linter name: (lintername)
var linterNameRe = regexp.MustCompile(`\s+\(([a-zA-Z][\w-]*)\)\s*$`)

// ParseCheckOutput extracts structured errors from a CheckResult.
// It dispatches to the appropriate format parser based on CheckResult.Name.
// If no structured errors can be extracted, ParseResult.Errors will be empty
// and callers should fall back to RawOutput.
func ParseCheckOutput(cr CheckResult) ParseResult {
	pr := ParseResult{
		RawOutput: cr.Output,
		CheckName: cr.Name,
	}

	switch cr.Name {
	case "build":
		pr.Errors = parseBuildErrors(cr.Output)
	case "vet":
		pr.Errors = parseVetErrors(cr.Output)
	case "lint":
		pr.Errors = parseLintErrors(cr.Output)
	case "test":
		pr.Errors = parseTestErrors(cr.Output)
	}

	pr.Errors = dedup(pr.Errors)
	return pr
}

// parseBuildErrors handles `go build ./...` output.
// Format: <file>:<line>:<col>: <message>
// Lines starting with # (package headers) are skipped.
func parseBuildErrors(output string) []FilterError {
	return parseStandardErrors(output, "build")
}

// parseVetErrors handles `go vet ./...` output.
// Format: <file>:<line>:<col>: <message>
// Also handles "# <package>" header lines and optional "vet:" prefix.
func parseVetErrors(output string) []FilterError {
	return parseStandardErrors(output, "vet")
}

// parseLintErrors handles `golangci-lint run` output.
// Format: <file>:<line>:<col>: <message> (<linter-name>)
func parseLintErrors(output string) []FilterError {
	var errs []FilterError
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := errLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		fe := FilterError{
			File:    cleanPath(m[1]),
			Line:    atoi(m[2]),
			Column:  atoi(m[3]),
			Message: m[4],
			Tool:    "lint",
		}
		// Extract linter name from trailing parentheses and
		// preserve it in the message for context.
		if lm := linterNameRe.FindStringSubmatch(fe.Message); lm != nil {
			fe.Message = strings.TrimSuffix(fe.Message, lm[0])
			fe.Message = strings.TrimSpace(fe.Message) + " [" + lm[1] + "]"
		}
		errs = append(errs, fe)
	}
	return errs
}

// parseTestErrors handles `go test ./...` output.
// Extracts both compilation errors (same format as build) and test assertion
// failures with file:line: message format. Also extracts panic stack traces.
func parseTestErrors(output string) []FilterError {
	var errs []FilterError
	var currentTest string
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Track current failing test name.
		if fm := testFailRe.FindStringSubmatch(trimmed); fm != nil {
			currentTest = fm[1]
			continue
		}

		// Standard file:line:col: message (compilation errors within tests).
		if m := errLineRe.FindStringSubmatch(trimmed); m != nil {
			errs = append(errs, FilterError{
				File:    cleanPath(m[1]),
				Line:    atoi(m[2]),
				Column:  atoi(m[3]),
				Message: m[4],
				Tool:    "test",
			})
			continue
		}

		// Panic stack trace: file.go:123 +0x...
		if m := panicTraceRe.FindStringSubmatch(line); m != nil {
			errs = append(errs, FilterError{
				File:    cleanPath(m[1]),
				Line:    atoi(m[2]),
				Column:  0,
				Message: "panic stack trace",
				Tool:    "test",
			})
			continue
		}

		// Test assertion failure: file.go:line: message (no column).
		// Use the original (non-trimmed) line for indent-aware matching,
		// but try trimmed to handle both indented and non-indented forms.
		if m := errLineNoColRe.FindStringSubmatch(trimmed); m != nil {
			msg := m[3]
			if currentTest != "" {
				msg = "[" + currentTest + "] " + msg
			}
			errs = append(errs, FilterError{
				File:    cleanPath(m[1]),
				Line:    atoi(m[2]),
				Column:  0,
				Message: msg,
				Tool:    "test",
			})
			continue
		}
	}
	return errs
}

// parseStandardErrors parses the common file:line:col: message format
// shared by go build and go vet. Lines starting with # are skipped.
func parseStandardErrors(output, tool string) []FilterError {
	var errs []FilterError
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := errLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		errs = append(errs, FilterError{
			File:    cleanPath(m[1]),
			Line:    atoi(m[2]),
			Column:  atoi(m[3]),
			Message: m[4],
			Tool:    tool,
		})
	}
	return errs
}

// dedup removes duplicate FilterError entries with the same File, Line, and Message.
func dedup(errs []FilterError) []FilterError {
	if len(errs) == 0 {
		return errs
	}
	type key struct {
		File    string
		Line    int
		Message string
	}
	seen := make(map[key]struct{}, len(errs))
	out := make([]FilterError, 0, len(errs))
	for _, e := range errs {
		k := key{File: e.File, Line: e.Line, Message: e.Message}
		if _, exists := seen[k]; exists {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, e)
	}
	return out
}

// cleanPath strips leading "./" from file paths for consistency.
func cleanPath(p string) string {
	return strings.TrimPrefix(p, "./")
}

// atoi converts a string to int, returning 0 on failure.
func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
