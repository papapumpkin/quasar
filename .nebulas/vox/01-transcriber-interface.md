+++
id = "transcriber-interface"
title = "Define Transcriber interface and noop backend"
type = "feature"
priority = 1
depends_on = []
scope = ["internal/stt/**"]
+++

## Problem

Quasar has no speech-to-text capability. Before adding platform-specific backends or an audio pipeline, we need a stable interface that the rest of the system can program against. The interface must be small enough that backends only need to implement four methods, and there must always be a noop fallback so the binary compiles and runs on platforms without STT support.

The design must support build-tag gating so that platform-specific Cgo code (macOS, Linux PulseAudio, etc.) is only compiled on the target OS, while the noop backend is the universal default.

## Solution

Create `internal/stt/` with two files: the interface definition and the noop implementation.

### Transcriber interface and types

```go
// internal/stt/stt.go

package stt

import (
    "context"
    "time"
)

// Transcription represents a single speech-to-text result from the
// underlying recognition engine. Partial results have Final=false;
// the engine may revise them before emitting a Final=true result.
type Transcription struct {
    Text       string    // Recognized text.
    Confidence float64   // 0.0–1.0 confidence score.
    Timestamp  time.Time // When the utterance was captured.
    Final      bool      // True when the engine has finalized this segment.
}

// Transcriber is the pluggable speech-to-text interface. Implementations
// are selected at build time via build tags. The consumer (feedback
// pipeline, TUI, web handler) programs against this interface and
// never imports a specific backend.
type Transcriber interface {
    // Start begins audio capture and recognition. The transcriber
    // sends results on the channel returned by Transcriptions().
    // Calling Start on an already-started transcriber is a no-op.
    Start(ctx context.Context) error

    // Stop halts audio capture and recognition. After Stop returns,
    // no more Transcriptions will be sent. The Transcriptions channel
    // is closed. Calling Stop on an already-stopped transcriber is a
    // no-op.
    Stop() error

    // Transcriptions returns a receive-only channel that delivers
    // recognition results. The channel is created by Start and closed
    // by Stop. Calling before Start returns nil.
    Transcriptions() <-chan Transcription

    // Available reports whether this backend can operate on the
    // current platform. The noop backend always returns false.
    Available() bool
}

// ErrNotAvailable is returned when Start is called on a backend that
// is not supported on the current platform.
var ErrNotAvailable = errors.New("stt: backend not available on this platform")
```

### Backend selection via build tags

Use a `Default()` constructor function with build-tag variants:

```go
// internal/stt/stt.go (continued)

// Default returns the platform-appropriate Transcriber. On platforms
// without a native backend, this returns the noop transcriber.
// Platform-specific files (macos.go, etc.) override this via build
// tags using an init() function that replaces defaultConstructor.
var defaultConstructor = func() Transcriber { return &Noop{} }

// Default returns the Transcriber for the current platform.
func Default() Transcriber {
    return defaultConstructor()
}
```

### Noop backend

```go
// internal/stt/noop.go

package stt

import "context"

// Noop is a Transcriber that does nothing. It compiles on all
// platforms and acts as the fallback when no native STT backend
// is available. Available() always returns false.
type Noop struct{}

// Start is a no-op. It returns ErrNotAvailable because the noop
// backend cannot perform recognition.
func (n *Noop) Start(ctx context.Context) error { return ErrNotAvailable }

// Stop is a no-op.
func (n *Noop) Stop() error { return nil }

// Transcriptions always returns nil because the noop backend
// produces no results.
func (n *Noop) Transcriptions() <-chan Transcription { return nil }

// Available always returns false.
func (n *Noop) Available() bool { return false }
```

### Testing

```go
// internal/stt/stt_test.go

func TestNoopAvailable(t *testing.T) {
    n := &Noop{}
    if n.Available() {
        t.Error("Noop.Available() should return false")
    }
}

func TestNoopStartReturnsError(t *testing.T) {
    n := &Noop{}
    err := n.Start(context.Background())
    if !errors.Is(err, ErrNotAvailable) {
        t.Errorf("expected ErrNotAvailable, got %v", err)
    }
}

func TestNoopTranscriptionsNil(t *testing.T) {
    n := &Noop{}
    if n.Transcriptions() != nil {
        t.Error("Noop.Transcriptions() should return nil")
    }
}

func TestDefaultReturnsTranscriber(t *testing.T) {
    tr := Default()
    if tr == nil {
        t.Fatal("Default() returned nil")
    }
}
```

## Files

- `internal/stt/stt.go` -- `Transcriber` interface, `Transcription` struct, `ErrNotAvailable` sentinel, `Default()` constructor with replaceable `defaultConstructor`
- `internal/stt/noop.go` -- `Noop` struct implementing `Transcriber` with all no-op methods
- `internal/stt/stt_test.go` -- tests for `Noop` behavior and `Default()` constructor

## Acceptance Criteria

- [ ] `Transcriber` interface compiles with `go build ./internal/stt/...`
- [ ] `Noop` satisfies `Transcriber` (compile-time check via `var _ Transcriber = (*Noop)(nil)`)
- [ ] `Noop.Available()` returns `false`
- [ ] `Noop.Start()` returns `ErrNotAvailable`
- [ ] `Noop.Transcriptions()` returns `nil`
- [ ] `Default()` returns a `Transcriber` (noop on non-darwin platforms)
- [ ] `go vet ./internal/stt/...` passes
- [ ] `go test ./internal/stt/...` passes with no failures
- [ ] All exported types and functions have GoDoc comments
