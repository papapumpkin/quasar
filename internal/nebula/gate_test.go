package nebula

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestParseGateInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  GateAction
	}{
		{"accept short", "a", GateActionAccept},
		{"accept full", "accept", GateActionAccept},
		{"accept upper", "Accept", GateActionAccept},
		{"reject short", "r", GateActionReject},
		{"reject full", "reject", GateActionReject},
		{"retry short", "t", GateActionRetry},
		{"retry full", "retry", GateActionRetry},
		{"skip short", "s", GateActionSkip},
		{"skip full", "skip", GateActionSkip},
		{"whitespace", "  a  ", GateActionAccept},
		{"empty defaults to accept", "", GateActionAccept},
		{"unknown defaults to accept", "xyz", GateActionAccept},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseGateInput(tt.input)
			if got != tt.want {
				t.Errorf("parseGateInput(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTerminalGater_NonTTY(t *testing.T) {
	t.Parallel()

	// strings.Reader is not *os.File, so isTTY returns false.
	in := strings.NewReader("")
	var out bytes.Buffer
	g := newTerminalGaterWithIO(in, &out)

	cp := &Checkpoint{PhaseID: "test-phase"}
	action, err := g.Prompt(context.Background(), cp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != GateActionAccept {
		t.Errorf("expected accept for non-TTY, got %q", action)
	}
	if !strings.Contains(out.String(), "non-TTY") {
		t.Errorf("expected non-TTY warning, got %q", out.String())
	}
}

func TestTerminalGater_NonTTY_NilCheckpoint(t *testing.T) {
	t.Parallel()

	in := strings.NewReader("")
	var out bytes.Buffer
	g := newTerminalGaterWithIO(in, &out)

	action, err := g.Prompt(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != GateActionAccept {
		t.Errorf("expected accept for non-TTY with nil checkpoint, got %q", action)
	}
	if !strings.Contains(out.String(), `"unknown"`) {
		t.Errorf("expected fallback phase ID 'unknown', got %q", out.String())
	}
}

func TestTerminalGater_ContextCanceled(t *testing.T) {
	t.Parallel()

	// blockReader blocks on Read, simulating a terminal waiting for input.
	// forceTTY bypasses the isTTY check so we reach the select/context path.
	br := &blockReader{ch: make(chan struct{})}
	var out bytes.Buffer
	ttyTrue := true
	g := &terminalGater{in: br, out: &out, forceTTY: &ttyTrue}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	cp := &Checkpoint{PhaseID: "test-phase"}
	action, err := g.Prompt(ctx, cp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != GateActionSkip {
		t.Errorf("expected skip on context cancellation, got %q", action)
	}
}

// blockReader blocks on Read until its channel is closed.
type blockReader struct {
	ch chan struct{}
}

func (r *blockReader) Read(p []byte) (int, error) {
	<-r.ch
	return 0, nil
}

func TestTerminalGater_TTYInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  GateAction
	}{
		{"accept", "a\n", GateActionAccept},
		{"reject", "r\n", GateActionReject},
		{"retry", "t\n", GateActionRetry},
		{"skip", "s\n", GateActionSkip},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := strings.NewReader(tt.input)
			var out bytes.Buffer
			ttyTrue := true
			g := &terminalGater{in: in, out: &out, forceTTY: &ttyTrue}
			cp := &Checkpoint{PhaseID: "test"}
			action, err := g.Prompt(context.Background(), cp)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if action != tt.want {
				t.Errorf("got %q, want %q", action, tt.want)
			}
		})
	}
}

func TestTerminalGater_PlanPrompt(t *testing.T) {
	t.Parallel()

	in := strings.NewReader("a\n")
	var out bytes.Buffer
	ttyTrue := true
	g := &terminalGater{in: in, out: &out, forceTTY: &ttyTrue}

	cp := &Checkpoint{PhaseID: PlanPhaseID}
	action, err := g.Prompt(context.Background(), cp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != GateActionAccept {
		t.Errorf("expected accept, got %q", action)
	}
	prompt := out.String()
	if !strings.Contains(prompt, "[a]pprove") {
		t.Errorf("plan prompt should show [a]pprove, got %q", prompt)
	}
	if strings.Contains(prompt, "[r]eject") {
		t.Errorf("plan prompt should not show [r]eject, got %q", prompt)
	}
	if strings.Contains(prompt, "re[t]ry") {
		t.Errorf("plan prompt should not show re[t]ry, got %q", prompt)
	}
}

func TestTerminalGater_PhasePrompt(t *testing.T) {
	t.Parallel()

	in := strings.NewReader("a\n")
	var out bytes.Buffer
	ttyTrue := true
	g := &terminalGater{in: in, out: &out, forceTTY: &ttyTrue}

	cp := &Checkpoint{PhaseID: "build"}
	action, err := g.Prompt(context.Background(), cp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != GateActionAccept {
		t.Errorf("expected accept, got %q", action)
	}
	prompt := out.String()
	if !strings.Contains(prompt, "[a]ccept") {
		t.Errorf("phase prompt should show [a]ccept, got %q", prompt)
	}
	if !strings.Contains(prompt, "[r]eject") {
		t.Errorf("phase prompt should show [r]eject, got %q", prompt)
	}
}

func TestTerminalGater_EOF(t *testing.T) {
	t.Parallel()

	// Empty reader with forceTTY=true simulates EOF on stdin.
	in := strings.NewReader("")
	var out bytes.Buffer
	ttyTrue := true
	g := &terminalGater{in: in, out: &out, forceTTY: &ttyTrue}

	cp := &Checkpoint{PhaseID: "test"}
	action, err := g.Prompt(context.Background(), cp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != GateActionSkip {
		t.Errorf("expected skip on EOF, got %q", action)
	}
}
