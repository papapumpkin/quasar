package claude

import "encoding/json"

// CLIResponse is the JSON object the claude CLI emits with --output-format json.
type CLIResponse struct {
	Type          string  `json:"type"`
	Subtype       string  `json:"subtype"`
	IsError       bool    `json:"is_error"`
	DurationMs    int64   `json:"duration_ms"`
	DurationAPIMs int64   `json:"duration_api_ms"`
	NumTurns      int     `json:"num_turns"`
	Result        string  `json:"result"`
	SessionID     string  `json:"session_id"`
	TotalCostUSD  float64 `json:"total_cost_usd"`
	Usage         Usage   `json:"usage"`
	// StructuredOutput is the schema-validated object the CLI returns when
	// invoked with --json-schema (constrained decoding). Empty when no schema was
	// requested or the running CLI predates the flag.
	StructuredOutput json.RawMessage `json:"structured_output"`
}

// Usage captures the token-accounting block from a claude CLI response. The
// cache fields are the signal used to verify prompt caching is effective:
// CacheReadInputTokens counts tokens served from the cache (discounted), while
// CacheCreationInputTokens counts tokens written into the cache (full rate).
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}
