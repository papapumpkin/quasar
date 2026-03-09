package web

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/papapumpkin/quasar/internal/nebula"
)

// GateOption represents one selectable action in a gate prompt form.
type GateOption struct {
	Label  string
	Action string
}

// GateRequest represents a pending gate prompt waiting for a user response.
// It holds all the checkpoint data needed to render the gate form and a
// response channel that unblocks the waiting worker when resolved.
type GateRequest struct {
	PhaseID      string
	PhaseTitle   string
	Satisfaction string
	Risk         string
	Summary      string
	FilesChanged []string
	ReviewCycles int
	CostUSD      float64
	Options      []GateOption
	ResponseCh   chan nebula.GateAction
	CreatedAt    time.Time
}

// WebGater manages pending gate prompts for the web UI. It implements
// nebula.GatePrompter by serving gate prompts as HTML forms and collecting
// responses via HTTP POST. Safe for concurrent use.
type WebGater struct {
	mu      sync.RWMutex
	pending map[string]*GateRequest // keyed by phase ID
	server  *Server
}

// Compile-time interface check.
var _ nebula.GatePrompter = (*WebGater)(nil)

// NewWebGater creates a WebGater attached to the given server.
func NewWebGater(server *Server) *WebGater {
	return &WebGater{
		pending: make(map[string]*GateRequest),
		server:  server,
	}
}

// Prompt implements nebula.GatePrompter. It enqueues a gate request,
// broadcasts the form via SSE, and blocks until the user responds or
// the context is cancelled.
func (g *WebGater) Prompt(ctx context.Context, cp *nebula.Checkpoint) (nebula.GateAction, error) {
	req := buildGateRequest(cp)

	g.Enqueue(req)

	select {
	case <-ctx.Done():
		// Clean up the pending request on cancellation.
		g.mu.Lock()
		delete(g.pending, req.PhaseID)
		g.mu.Unlock()
		return nebula.GateActionSkip, ctx.Err()
	case action := <-req.ResponseCh:
		return action, nil
	}
}

// buildGateRequest constructs a GateRequest from a nebula Checkpoint.
func buildGateRequest(cp *nebula.Checkpoint) *GateRequest {
	phaseID := "unknown"
	var phaseTitle, satisfaction, risk, summary string
	var filesChanged []string
	var reviewCycles int
	var costUSD float64

	if cp != nil {
		phaseID = cp.PhaseID
		phaseTitle = cp.PhaseTitle
		satisfaction = cp.Satisfaction
		risk = cp.Risk
		summary = cp.ReviewSummary
		reviewCycles = cp.ReviewCycles
		costUSD = cp.CostUSD
		for _, fc := range cp.FilesChanged {
			filesChanged = append(filesChanged, fc.Path)
		}
	}

	isPlan := cp != nil && cp.PhaseID == nebula.PlanPhaseID
	var options []GateOption
	if isPlan {
		options = []GateOption{
			{Label: "Accept", Action: string(nebula.GateActionAccept)},
			{Label: "Skip", Action: string(nebula.GateActionSkip)},
		}
	} else {
		options = []GateOption{
			{Label: "Accept", Action: string(nebula.GateActionAccept)},
			{Label: "Reject", Action: string(nebula.GateActionReject)},
			{Label: "Retry", Action: string(nebula.GateActionRetry)},
			{Label: "Skip", Action: string(nebula.GateActionSkip)},
		}
	}

	return &GateRequest{
		PhaseID:      phaseID,
		PhaseTitle:   phaseTitle,
		Satisfaction: satisfaction,
		Risk:         risk,
		Summary:      summary,
		FilesChanged: filesChanged,
		ReviewCycles: reviewCycles,
		CostUSD:      costUSD,
		Options:      options,
		ResponseCh:   make(chan nebula.GateAction, 1),
		CreatedAt:    time.Now(),
	}
}

// Enqueue adds a gate prompt to the pending queue and notifies connected
// SSE clients via a gate-prompt event containing the rendered form HTML.
func (g *WebGater) Enqueue(req *GateRequest) {
	g.mu.Lock()
	g.pending[req.PhaseID] = req
	g.mu.Unlock()

	g.server.broadcastGatePrompt(req)
}

// Resolve submits a response for a pending gate prompt, unblocking the
// waiting worker goroutine. Returns an error if no pending gate exists
// for the given phase ID (e.g. already resolved).
func (g *WebGater) Resolve(phaseID string, action string) error {
	g.mu.Lock()
	req, ok := g.pending[phaseID]
	if !ok {
		g.mu.Unlock()
		return fmt.Errorf("no pending gate for phase %s", phaseID)
	}
	delete(g.pending, phaseID)
	g.mu.Unlock()

	req.ResponseCh <- nebula.GateAction(action)
	return nil
}

// Pending returns all pending gate requests. The returned slice is a
// snapshot; callers may read it without holding a lock.
func (g *WebGater) Pending() []*GateRequest {
	g.mu.RLock()
	defer g.mu.RUnlock()
	result := make([]*GateRequest, 0, len(g.pending))
	for _, req := range g.pending {
		result = append(result, req)
	}
	return result
}
