+++
id = "audio-pipeline"
title = "Audio capture pipeline with VAD and silence chunking"
type = "feature"
priority = 2
depends_on = ["transcriber-interface"]
scope = ["internal/stt/pipeline.go", "internal/stt/vad.go"]
+++

## Problem

Raw transcription streams from `Transcriber.Transcriptions()` deliver continuous partial results, which are noisy and difficult for downstream consumers (the advisor agent, TUI panel, web panel) to process. We need an intermediate pipeline that:

1. Detects voice activity and ignores silence/noise periods
2. Chunks continuous speech into sentence-sized segments based on silence gaps
3. Buffers partial results and emits only finalized, cleaned utterances
4. Supports push-to-talk mode (externally gated) and always-on mode (VAD-gated)

Without this, the feedback system would receive a firehose of partial transcriptions with no sentence boundaries, making the advisor agent's job much harder.

## Solution

Build a pipeline that sits between the `Transcriber` and the feedback system. The pipeline consumes `Transcription` values from the transcriber, applies VAD and silence-based chunking, and emits complete utterances on an output channel.

### Voice Activity Detection (VAD)

A simple energy-based VAD that operates on the `Confidence` field of incoming transcriptions. When confidence drops below a threshold for a configurable silence duration, the current utterance is considered complete.

```go
// internal/stt/vad.go

package stt

import "time"

// VADConfig controls voice activity detection behavior.
type VADConfig struct {
    // SilenceThreshold is the confidence level below which input
    // is considered silence. Default: 0.3.
    SilenceThreshold float64

    // SilenceDuration is how long confidence must stay below the
    // threshold before an utterance boundary is declared. Default: 1.5s.
    SilenceDuration time.Duration

    // MinUtteranceLen is the minimum character length for an
    // utterance to be emitted. Shorter results are discarded as
    // noise. Default: 5.
    MinUtteranceLen int
}

// DefaultVADConfig returns sensible defaults for conversational speech.
func DefaultVADConfig() VADConfig {
    return VADConfig{
        SilenceThreshold: 0.3,
        SilenceDuration:  1500 * time.Millisecond,
        MinUtteranceLen:  5,
    }
}

// vadState tracks the current voice activity state.
type vadState struct {
    speaking     bool
    silenceSince time.Time
    buffer       strings.Builder
    lastConf     float64
}

// feed processes a transcription and returns true + the buffered
// utterance when a complete segment boundary is detected.
func (v *vadState) feed(t Transcription, cfg VADConfig) (string, bool) {
    if t.Final {
        // Engine-declared boundary always emits.
        text := strings.TrimSpace(v.buffer.String() + " " + t.Text)
        v.buffer.Reset()
        v.speaking = false
        if len(text) < cfg.MinUtteranceLen {
            return "", false
        }
        return text, true
    }

    if t.Confidence >= cfg.SilenceThreshold {
        v.speaking = true
        v.silenceSince = time.Time{}
        v.buffer.WriteString(" ")
        v.buffer.WriteString(t.Text)
        return "", false
    }

    // Below threshold — track silence duration.
    if v.silenceSince.IsZero() {
        v.silenceSince = t.Timestamp
    }
    if v.speaking && time.Since(v.silenceSince) >= cfg.SilenceDuration {
        text := strings.TrimSpace(v.buffer.String())
        v.buffer.Reset()
        v.speaking = false
        if len(text) < cfg.MinUtteranceLen {
            return "", false
        }
        return text, true
    }
    return "", false
}
```

### Pipeline

```go
// internal/stt/pipeline.go

package stt

import (
    "context"
    "strings"
    "time"
)

// Utterance is a complete, finalized speech segment emitted by
// the Pipeline after VAD and chunking.
type Utterance struct {
    Text      string    // Cleaned, trimmed text of the utterance.
    Timestamp time.Time // When the utterance started.
}

// PipelineConfig controls pipeline behavior.
type PipelineConfig struct {
    VAD          VADConfig
    // PushToTalk when true disables VAD gating — the pipeline
    // only processes input between Activate/Deactivate calls.
    PushToTalk   bool
}

// DefaultPipelineConfig returns sensible defaults.
func DefaultPipelineConfig() PipelineConfig {
    return PipelineConfig{
        VAD: DefaultVADConfig(),
    }
}

// Pipeline processes a Transcriber's output through VAD and silence
// chunking, emitting complete Utterances. It supports both always-on
// (VAD-gated) and push-to-talk (externally gated) modes.
type Pipeline struct {
    transcriber Transcriber
    cfg         PipelineConfig
    utterances  chan Utterance
    active      chan bool // push-to-talk control channel
}

// NewPipeline creates a pipeline that reads from the given transcriber.
func NewPipeline(t Transcriber, cfg PipelineConfig) *Pipeline {
    return &Pipeline{
        transcriber: t,
        cfg:         cfg,
        utterances:  make(chan Utterance, 16),
        active:      make(chan bool, 1),
    }
}

// Utterances returns the output channel of finalized speech segments.
func (p *Pipeline) Utterances() <-chan Utterance {
    return p.utterances
}

// Activate signals push-to-talk start. No-op in always-on mode.
func (p *Pipeline) Activate() {
    if p.cfg.PushToTalk {
        select {
        case p.active <- true:
        default:
        }
    }
}

// Deactivate signals push-to-talk stop. Flushes the current buffer
// as an utterance. No-op in always-on mode.
func (p *Pipeline) Deactivate() {
    if p.cfg.PushToTalk {
        select {
        case p.active <- false:
        default:
        }
    }
}

// Run processes transcriptions until the context is cancelled or the
// transcription channel is closed. It closes the utterances channel
// on exit.
func (p *Pipeline) Run(ctx context.Context) error {
    defer close(p.utterances)

    incoming := p.transcriber.Transcriptions()
    if incoming == nil {
        return ErrNotAvailable
    }

    var vad vadState
    pttActive := !p.cfg.PushToTalk // always-on if not push-to-talk

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()

        case active, ok := <-p.active:
            if !ok {
                return nil
            }
            pttActive = active
            if !active {
                // Flush buffer on deactivate.
                text := strings.TrimSpace(vad.buffer.String())
                vad.buffer.Reset()
                vad.speaking = false
                if len(text) >= p.cfg.VAD.MinUtteranceLen {
                    p.emit(Utterance{Text: text, Timestamp: time.Now()})
                }
            }

        case t, ok := <-incoming:
            if !ok {
                return nil
            }
            if !pttActive {
                continue
            }
            if text, complete := vad.feed(t, p.cfg.VAD); complete {
                p.emit(Utterance{Text: text, Timestamp: t.Timestamp})
            }
        }
    }
}

func (p *Pipeline) emit(u Utterance) {
    select {
    case p.utterances <- u:
    default:
        // Drop oldest if buffer full (non-blocking to keep
        // the audio processing path responsive).
    }
}
```

### Testing

Test the pipeline with a mock transcriber that sends canned `Transcription` values:

```go
// internal/stt/pipeline_test.go

type mockTranscriber struct {
    ch chan Transcription
}

func (m *mockTranscriber) Start(ctx context.Context) error { return nil }
func (m *mockTranscriber) Stop() error                     { close(m.ch); return nil }
func (m *mockTranscriber) Transcriptions() <-chan Transcription { return m.ch }
func (m *mockTranscriber) Available() bool                 { return true }

func TestPipelineEmitsOnFinal(t *testing.T) {
    ch := make(chan Transcription, 10)
    mock := &mockTranscriber{ch: ch}
    p := NewPipeline(mock, DefaultPipelineConfig())

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    go p.Run(ctx)

    ch <- Transcription{Text: "make the tests faster", Confidence: 0.95, Final: true, Timestamp: time.Now()}

    select {
    case u := <-p.Utterances():
        if !strings.Contains(u.Text, "make the tests faster") {
            t.Errorf("unexpected utterance: %q", u.Text)
        }
    case <-time.After(time.Second):
        t.Fatal("timed out waiting for utterance")
    }
}

func TestPipelinePushToTalk(t *testing.T) {
    ch := make(chan Transcription, 10)
    mock := &mockTranscriber{ch: ch}
    cfg := DefaultPipelineConfig()
    cfg.PushToTalk = true
    p := NewPipeline(mock, cfg)

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    go p.Run(ctx)

    // Send while inactive — should be dropped.
    ch <- Transcription{Text: "ignored", Confidence: 0.9, Final: true, Timestamp: time.Now()}
    time.Sleep(50 * time.Millisecond)
    select {
    case u := <-p.Utterances():
        t.Fatalf("expected no utterance while inactive, got %q", u.Text)
    default:
    }

    // Activate, send, deactivate — should emit.
    p.Activate()
    ch <- Transcription{Text: "add a test phase", Confidence: 0.92, Final: false, Timestamp: time.Now()}
    p.Deactivate()

    select {
    case u := <-p.Utterances():
        if !strings.Contains(u.Text, "add a test phase") {
            t.Errorf("unexpected utterance: %q", u.Text)
        }
    case <-time.After(time.Second):
        t.Fatal("timed out waiting for utterance after deactivate")
    }
}
```

## Files

- `internal/stt/vad.go` -- `VADConfig`, `DefaultVADConfig()`, `vadState` with `feed()` method for voice activity detection and silence chunking
- `internal/stt/pipeline.go` -- `Pipeline` struct, `Utterance` type, `NewPipeline()`, `Run()`, `Utterances()`, `Activate()`/`Deactivate()` for push-to-talk
- `internal/stt/pipeline_test.go` -- mock transcriber, tests for final-emission, VAD silence boundary, push-to-talk gating
- `internal/stt/vad_test.go` -- unit tests for `vadState.feed()` with various confidence/timing patterns

## Acceptance Criteria

- [ ] `Pipeline` consumes `Transcription` values and emits `Utterance` values on a separate channel
- [ ] VAD detects silence gaps and chunks continuous speech into sentence-sized segments
- [ ] Final transcriptions (`Final=true`) always trigger immediate utterance emission
- [ ] Push-to-talk mode drops transcriptions unless `Activate()` has been called
- [ ] `Deactivate()` flushes the current buffer as an utterance
- [ ] Utterances shorter than `MinUtteranceLen` are discarded as noise
- [ ] Pipeline respects context cancellation and closes its output channel on exit
- [ ] `go test ./internal/stt/...` passes with no failures
- [ ] `go vet ./internal/stt/...` passes
