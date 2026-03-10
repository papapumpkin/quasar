package claude

// CLIResponse represents the JSON output from the Claude CLI.
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
	InputTokens   int     `json:"input_tokens"`
	OutputTokens  int     `json:"output_tokens"`
}
