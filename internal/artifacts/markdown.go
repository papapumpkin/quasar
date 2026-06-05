package artifacts

import (
	"fmt"
	"strings"
)

// frontmatterDelim opens and closes the TOML frontmatter block in a star or
// skill Markdown file, mirroring the nebula phase parser's convention.
const frontmatterDelim = "+++"

// splitFrontmatter splits a star/skill Markdown file into its TOML frontmatter
// and Markdown body. The file must open with a +++ delimiter line; the
// frontmatter runs up to the next +++ and the body is everything after it.
//
// It also returns the 1-based line number where the frontmatter begins, so the
// loader can translate a TOML decode position into a line in the original file.
func splitFrontmatter(content string) (frontmatter, body string, fmStartLine int, err error) {
	trimmed := strings.TrimLeft(content, " \t\r\n")
	leadingLines := strings.Count(content[:len(content)-len(trimmed)], "\n")

	if !strings.HasPrefix(trimmed, frontmatterDelim) {
		return "", "", 0, fmt.Errorf("file does not start with %s frontmatter delimiter", frontmatterDelim)
	}

	rest := trimmed[len(frontmatterDelim):]
	idx := strings.Index(rest, frontmatterDelim)
	if idx < 0 {
		return "", "", 0, fmt.Errorf("missing closing %s frontmatter delimiter", frontmatterDelim)
	}

	frontmatter = rest[:idx]
	body = rest[idx+len(frontmatterDelim):]

	// The frontmatter's first content line is the line after the opening
	// delimiter: leading blank lines + the delimiter line itself.
	fmStartLine = leadingLines + 2
	return frontmatter, body, fmStartLine, nil
}
