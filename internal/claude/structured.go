package claude

import (
	"encoding/json"
	"fmt"
	"strings"
)

// appendSchemaInstruction steers a model toward schema-valid JSON when the
// backend lacks native constrained decoding (the fallback path for an older
// claude CLI or, eventually, a different coder). The instruction is appended to
// the user prompt; extractJSONObject recovers the object from the text reply.
func appendSchemaInstruction(prompt string, schema []byte) string {
	var b strings.Builder
	b.WriteString(prompt)
	b.WriteString("\n\n---\nIMPORTANT: Respond with ONLY a single JSON object that validates" +
		" against the JSON Schema below. No prose, no explanation, no markdown code" +
		" fences — output the raw JSON object and nothing else.\n\nJSON Schema:\n")
	b.Write(schema)
	return b.String()
}

// structuredResult resolves the schema-valid JSON object from a CLI response.
// On the native path it prefers the CLI's structured_output field; otherwise
// (older envelope, or the in-prompt fallback) it recovers the object from the
// result text. It errors when no valid JSON object can be obtained, so a
// downstream operator never receives prose.
func structuredResult(native bool, resp CLIResponse) (json.RawMessage, error) {
	if native && len(strings.TrimSpace(string(resp.StructuredOutput))) > 0 {
		return resp.StructuredOutput, nil
	}
	return extractJSONObject(resp.Result)
}

// extractJSONObject pulls the first complete top-level JSON object out of s,
// tolerating a surrounding markdown code fence and prose. It scans from the
// first '{' to its matching '}', respecting string literals and escapes so
// braces inside string values don't throw off the depth count, then verifies
// the slice parses as JSON. This is the fallback for backends without native
// schema-constrained decoding.
func extractJSONObject(s string) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(s)
	if strings.HasPrefix(trimmed, "{") && json.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed), nil
	}
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return nil, fmt.Errorf("no JSON object found in model output")
	}
	depth, inStr, esc := 0, false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				candidate := s[start : i+1]
				if !json.Valid([]byte(candidate)) {
					return nil, fmt.Errorf("extracted JSON object is not valid JSON")
				}
				return json.RawMessage(candidate), nil
			}
		}
	}
	return nil, fmt.Errorf("unterminated JSON object in model output")
}
