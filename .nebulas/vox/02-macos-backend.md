+++
id = "macos-backend"
title = "macOS dictation backend via SFSpeechRecognizer"
type = "feature"
priority = 2
depends_on = ["transcriber-interface"]
scope = ["internal/stt/macos.go", "internal/stt/macos_bridge.m", "internal/stt/macos_bridge.h"]
+++

## Problem

The `Transcriber` interface defined in phase 1 has only a noop implementation. On macOS, the system provides `SFSpeechRecognizer` (Speech framework) for on-device speech recognition with streaming support. We need a darwin-only backend that bridges Go to this native API, enabling real-time voice transcription for Quasar's feedback loop without external services or API keys.

## Solution

Create a macOS STT backend using Cgo to bridge to the Apple Speech framework's `SFSpeechRecognizer`. The backend captures audio via `AVAudioEngine`, feeds buffers into an `SFSpeechAudioBufferRecognitionRequest`, and sends partial/final transcription results back to Go through a callback channel.

### Build tag gating

```go
// internal/stt/macos.go
//go:build darwin

package stt
```

This file only compiles on macOS. It registers itself as the default backend via an `init()` function that replaces `defaultConstructor`:

```go
func init() {
    defaultConstructor = func() Transcriber {
        return &MacOS{}
    }
}
```

### Cgo bridge architecture

The bridge uses three files:
- `macos_bridge.h` -- C header declaring the bridge functions
- `macos_bridge.m` -- Objective-C implementation using Speech.framework and AVFoundation.framework
- `macos.go` -- Go struct implementing `Transcriber`, calling the bridge via Cgo

### Objective-C bridge

```objc
// internal/stt/macos_bridge.h

#ifndef MACOS_BRIDGE_H
#define MACOS_BRIDGE_H

typedef void (*TranscriptionCallback)(const char *text, double confidence, int is_final);

int stt_start(TranscriptionCallback callback);
void stt_stop(void);
int stt_available(void);

#endif
```

```objc
// internal/stt/macos_bridge.m

#import <Speech/Speech.h>
#import <AVFoundation/AVFoundation.h>
#include "macos_bridge.h"

static AVAudioEngine *audioEngine = nil;
static SFSpeechRecognizer *recognizer = nil;
static SFSpeechAudioBufferRecognitionRequest *request = nil;
static SFSpeechRecognitionTask *recognitionTask = nil;

int stt_available(void) {
    SFSpeechRecognizer *r = [[SFSpeechRecognizer alloc] init];
    return r.isAvailable ? 1 : 0;
}

int stt_start(TranscriptionCallback callback) {
    recognizer = [[SFSpeechRecognizer alloc] init];
    if (!recognizer.isAvailable) return -1;

    request = [[SFSpeechAudioBufferRecognitionRequest alloc] init];
    request.shouldReportPartialResults = YES;

    audioEngine = [[AVAudioEngine alloc] init];
    AVAudioInputNode *inputNode = [audioEngine inputNode];
    AVAudioFormat *format = [inputNode outputFormatForBus:0];

    recognitionTask = [recognizer recognitionTaskWithRequest:request
        resultHandler:^(SFSpeechRecognitionResult *result, NSError *error) {
            if (result) {
                const char *text = [result.bestTranscription.formattedString UTF8String];
                double conf = result.bestTranscription.segments.lastObject.confidence;
                int isFinal = result.isFinal ? 1 : 0;
                callback(text, conf, isFinal);
            }
        }];

    [inputNode installTapOnBus:0 bufferSize:1024 format:format
        block:^(AVAudioPCMBuffer *buffer, AVAudioTime *when) {
            [request appendAudioPCMBuffer:buffer];
        }];

    [audioEngine prepare];
    NSError *err = nil;
    [audioEngine startAndReturnError:&err];
    return err ? -1 : 0;
}

void stt_stop(void) {
    [audioEngine stop];
    [[audioEngine inputNode] removeTapOnBus:0];
    [request endAudio];
    [recognitionTask cancel];
    audioEngine = nil;
    request = nil;
    recognitionTask = nil;
    recognizer = nil;
}
```

### Go implementation

```go
// internal/stt/macos.go
//go:build darwin

package stt

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Speech -framework AVFoundation
#include "macos_bridge.h"
*/
import "C"

import (
    "context"
    "sync"
    "time"
    "unsafe"
)

// MacOS implements Transcriber using Apple's SFSpeechRecognizer via Cgo.
// It captures audio from the default input device, streams buffers
// into on-device recognition, and delivers partial/final results on
// the Transcriptions channel.
type MacOS struct {
    ch      chan Transcription
    mu      sync.Mutex
    started bool
}

// transcriptionCallback is the C callback that bridges recognition
// results from Objective-C into the Go channel.
//
//export transcriptionCallback
func transcriptionCallback(text *C.char, confidence C.double, isFinal C.int) {
    if activeMacOS == nil {
        return
    }
    t := Transcription{
        Text:       C.GoString(text),
        Confidence: float64(confidence),
        Timestamp:  time.Now(),
        Final:      isFinal != 0,
    }
    select {
    case activeMacOS.ch <- t:
    default:
        // Drop if channel is full to avoid blocking the audio thread.
    }
}

// activeMacOS holds the currently running MacOS transcriber for the
// C callback to deliver results. Only one instance can be active.
var activeMacOS *MacOS

func (m *MacOS) Start(ctx context.Context) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    if m.started {
        return nil
    }
    if !m.Available() {
        return ErrNotAvailable
    }
    m.ch = make(chan Transcription, 64)
    activeMacOS = m
    rc := C.stt_start(C.TranscriptionCallback(C.transcriptionCallback))
    if rc != 0 {
        close(m.ch)
        activeMacOS = nil
        return fmt.Errorf("stt: macOS speech recognizer failed to start")
    }
    m.started = true

    // Stop when context is cancelled.
    go func() {
        <-ctx.Done()
        m.Stop()
    }()
    return nil
}

func (m *MacOS) Stop() error {
    m.mu.Lock()
    defer m.mu.Unlock()
    if !m.started {
        return nil
    }
    C.stt_stop()
    close(m.ch)
    activeMacOS = nil
    m.started = false
    return nil
}

func (m *MacOS) Transcriptions() <-chan Transcription {
    return m.ch
}

func (m *MacOS) Available() bool {
    return C.stt_available() != 0
}
```

### Permissions

macOS requires the `NSSpeechRecognitionUsageDescription` and `NSMicrophoneUsageDescription` entitlements. Document this in a comment at the top of `macos.go` and in the Quasar README. For CLI usage, the user must grant microphone and speech recognition permissions when prompted by the OS.

## Files

- `internal/stt/macos.go` -- `MacOS` struct implementing `Transcriber` via Cgo, `init()` to register as default on darwin, `//go:build darwin` tag
- `internal/stt/macos_bridge.h` -- C header declaring `stt_start`, `stt_stop`, `stt_available`, `TranscriptionCallback` typedef
- `internal/stt/macos_bridge.m` -- Objective-C implementation using `SFSpeechRecognizer`, `AVAudioEngine`, `SFSpeechAudioBufferRecognitionRequest`
- `internal/stt/macos_test.go` -- tests (darwin-only) verifying `MacOS` satisfies `Transcriber`, `Available()` returns a boolean, `Start`/`Stop` lifecycle

## Acceptance Criteria

- [ ] `macos.go` only compiles on darwin (`//go:build darwin`)
- [ ] `MacOS` satisfies `Transcriber` (compile-time check: `var _ Transcriber = (*MacOS)(nil)`)
- [ ] `Default()` returns `*MacOS` on darwin builds
- [ ] `Start(ctx)` begins audio capture and recognition; `Stop()` halts them
- [ ] Partial results (`Final=false`) and final results (`Final=true`) are delivered on the `Transcriptions()` channel
- [ ] Context cancellation triggers `Stop()` automatically
- [ ] Channel has buffer (64) to prevent blocking the audio callback thread
- [ ] `go build ./internal/stt/...` passes on darwin
- [ ] `go vet ./internal/stt/...` passes on darwin
- [ ] `go build ./internal/stt/...` still passes on linux (noop only, macos.go excluded)
- [ ] Required macOS entitlements are documented in comments
