// Package ansi provides ANSI escape code constants and helpers for terminal output.
// All colored/styled terminal output should reference these constants to avoid duplication.
package ansi

import "fmt"

// ANSI SGR (Select Graphic Rendition) codes.
const (
	Reset   = "\033[0m"
	Bold    = "\033[1m"
	Dim     = "\033[2m"
	Blue    = "\033[34m"
	Yellow  = "\033[33m"
	Green   = "\033[32m"
	Red     = "\033[31m"
	Cyan    = "\033[36m"
	Magenta = "\033[35m"
)

// ANSI cursor and line control codes.
const (
	// ClearLine clears the entire current line.
	ClearLine = "\033[2K"

	// CursorUpFmt is a format string for moving the cursor up N lines.
	// Use with fmt.Sprintf or the CursorUp helper.
	CursorUpFmt = "\033[%dA"
)

// CursorUp returns an ANSI escape sequence to move the cursor up n lines.
func CursorUp(n int) string {
	return fmt.Sprintf(CursorUpFmt, n)
}
