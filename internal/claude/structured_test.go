package claude

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExtractJSONObject(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string // "" => expect an error
		wantErr bool
	}{
		{"raw object", `{"a":1}`, `{"a":1}`, false},
		{"fenced object", "```json\n{\"a\":1}\n```", `{"a":1}`, false},
		{"prose then fenced", "Here is the plan:\n```json\n{\"a\":1,\"b\":2}\n```", `{"a":1,"b":2}`, false},
		{"prose then bare object", "Sure!\n{\"verdict\":\"approve\"}", `{"verdict":"approve"}`, false},
		{"braces inside strings", `{"body":"if (x) { y }","id":"p1"}`, `{"body":"if (x) { y }","id":"p1"}`, false},
		{"escaped quote in string", `{"s":"a \" { brace"}`, `{"s":"a \" { brace"}`, false},
		{"trailing prose ignored", `{"a":1} -- done`, `{"a":1}`, false},
		{"no object", "no json here", "", true},
		{"invalid json object", `{"a":}`, "", true},
		{"unterminated", `{"a":1`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractJSONObject(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("extractJSONObject(%q) = %q, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("extractJSONObject(%q): unexpected error %v", tt.in, err)
			}
			if string(got) != tt.want {
				t.Errorf("extractJSONObject(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestStructuredResult(t *testing.T) {
	t.Run("native prefers structured_output", func(t *testing.T) {
		resp := CLIResponse{
			StructuredOutput: json.RawMessage(`{"verdict":"approve"}`),
			Result:           "ignored prose",
		}
		got, err := structuredResult(true, resp)
		if err != nil {
			t.Fatalf("structuredResult: %v", err)
		}
		if string(got) != `{"verdict":"approve"}` {
			t.Errorf("got %q", got)
		}
	})

	t.Run("native missing structured_output falls back to result text", func(t *testing.T) {
		resp := CLIResponse{Result: "```json\n{\"verdict\":\"approve\"}\n```"}
		got, err := structuredResult(true, resp)
		if err != nil {
			t.Fatalf("structuredResult: %v", err)
		}
		if string(got) != `{"verdict":"approve"}` {
			t.Errorf("got %q", got)
		}
	})

	t.Run("fallback extracts from result text", func(t *testing.T) {
		resp := CLIResponse{Result: "Here you go:\n{\"score\":42}"}
		got, err := structuredResult(false, resp)
		if err != nil {
			t.Fatalf("structuredResult: %v", err)
		}
		if string(got) != `{"score":42}` {
			t.Errorf("got %q", got)
		}
	})

	t.Run("fallback with no JSON errors", func(t *testing.T) {
		if _, err := structuredResult(false, CLIResponse{Result: "no json"}); err == nil {
			t.Fatal("expected error when result has no JSON object")
		}
	})
}

func TestAppendSchemaInstruction(t *testing.T) {
	schema := []byte(`{"type":"object"}`)
	got := appendSchemaInstruction("do the thing", schema)
	for _, want := range []string{"do the thing", "ONLY a single JSON object", `{"type":"object"}`} {
		if !strings.Contains(got, want) {
			t.Errorf("instruction missing %q in:\n%s", want, got)
		}
	}
}
