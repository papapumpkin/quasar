// Package telemetry provides a JSONL event stream for recording state transitions.

package telemetry

import (
	"github.com/papapumpkin/quasar/internal/bus"
)

// BusSubscriber subscribes to a bus and writes matching events to a
// telemetry JSONL file via an Emitter. It runs a background goroutine
// that maps bus events to telemetry events and emits them.
type BusSubscriber struct {
	emitter *Emitter
	sub     bus.Subscription
	epochID string
	done    chan struct{}
}

// NewBusSubscriber creates a telemetry subscriber. The epochID is included
// in every emitted event for correlation. Call Start to begin processing.
func NewBusSubscriber(emitter *Emitter, b bus.Bus, epochID string) *BusSubscriber {
	return &BusSubscriber{
		emitter: emitter,
		sub:     b.Subscribe("telemetry", 128),
		epochID: epochID,
		done:    make(chan struct{}),
	}
}

// Start begins the background event processing goroutine.
func (s *BusSubscriber) Start() {
	go s.run()
}

// Stop unsubscribes from the bus and waits for the processing goroutine
// to drain remaining events and exit.
func (s *BusSubscriber) Stop() {
	s.sub.Unsubscribe()
	<-s.done
}

func (s *BusSubscriber) run() {
	defer close(s.done)
	for ev := range s.sub.Events() {
		telEv, ok := s.mapEvent(ev)
		if !ok {
			continue
		}
		// Best-effort emit; telemetry should not block execution.
		_ = s.emitter.Emit(telEv)
	}
}

// mapEvent translates a bus.Event into a telemetry Event. It returns
// false for bus event kinds that have no telemetry mapping.
func (s *BusSubscriber) mapEvent(ev bus.Event) (Event, bool) {
	var kind string
	var data any

	switch ev.Kind {
	case bus.KindPhaseAgentStart, bus.KindAgentStart:
		kind = KindAgentStart
		data = map[string]any{
			"role":     ev.Role,
			"phase_id": ev.PhaseID,
		}

	case bus.KindPhaseAgentDone, bus.KindAgentDone:
		kind = KindAgentDone
		data = map[string]any{
			"role":        ev.Role,
			"cost_usd":    ev.CostUSD,
			"duration_ms": ev.DurationMs,
			"tokens":      ev.Tokens,
			"phase_id":    ev.PhaseID,
		}

	case bus.KindPhaseCycleStart, bus.KindCycleStart:
		kind = KindCycleStart
		data = map[string]any{
			"cycle":      ev.Cycle,
			"max_cycles": ev.MaxCycles,
			"phase_id":   ev.PhaseID,
		}

	case bus.KindPhaseCycleSummary, bus.KindCycleSummary:
		kind = KindCycleDone
		if ev.CycleSummary != nil {
			data = map[string]any{
				"cycle":       ev.CycleSummary.Cycle,
				"cost_usd":    ev.CycleSummary.CostUSD,
				"approved":    ev.CycleSummary.Approved,
				"issue_count": ev.CycleSummary.IssueCount,
				"phase_id":    ev.PhaseID,
			}
		}

	case bus.KindPhaseTaskStarted, bus.KindTaskStarted:
		kind = KindEpochStart
		data = map[string]any{
			"bead_id":  ev.BeadID,
			"title":    ev.Title,
			"phase_id": ev.PhaseID,
		}

	case bus.KindPhaseTaskComplete, bus.KindTaskComplete:
		kind = KindEpochDone
		data = map[string]any{
			"bead_id":    ev.BeadID,
			"total_cost": ev.CostUSD,
			"phase_id":   ev.PhaseID,
		}

	case bus.KindEntanglementUpdate:
		kind = KindEntanglementPosted
		data = nil

	case bus.KindDiscoveryPosted:
		kind = KindDiscoveryPosted
		data = map[string]any{
			"phase_id": ev.PhaseID,
		}

	case bus.KindHealingAttempt:
		if ev.Healing == nil {
			return Event{}, false
		}
		kind = KindHealingStart
		data = map[string]any{
			"failed_phase_id":   ev.Healing.FailedPhaseID,
			"failure_kind":      ev.Healing.FailureKind,
			"remediation_id":    ev.Healing.RemediationID,
			"remediation_title": ev.Healing.RemediationTitle,
		}

	case bus.KindNebulaDone:
		kind = KindEpochDone
		var errMsg string
		if ev.DoneResults != nil && ev.DoneResults.Err != nil {
			errMsg = ev.DoneResults.Err.Error()
		}
		data = map[string]any{
			"error": errMsg,
		}

	default:
		return Event{}, false
	}

	return Event{
		Timestamp: ev.Timestamp,
		Kind:      kind,
		EpochID:   s.epochID,
		TaskID:    ev.PhaseID,
		Data:      data,
	}, true
}
