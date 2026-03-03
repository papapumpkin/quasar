+++
id = "web-feedback"
title = "Web feedback panel with text input, microphone, and HTMX updates"
type = "feature"
priority = 2
depends_on = ["worker-integration", "audio-pipeline"]
scope = ["internal/web/feedback.go", "internal/web/feedback_test.go", "internal/web/templates/partials/feedback_panel.html"]
+++

## Problem

The TUI feedback panel (phase 7) serves terminal users. The web dashboard (from meridian) needs its own feedback panel: a text input, a microphone button for browser-based voice capture, and a live-updating feedback history. The web panel targets richer interactions — longer text input, visual action cards, and the browser's MediaRecorder API for audio capture without requiring system-level STT.

## Solution

### Routes

```go
// Feedback web routes:
// GET  /feedback               — feedback history (HTMX partial)
// POST /feedback               — submit text feedback
// POST /feedback/audio         — submit audio blob for server-side STT
// GET  /feedback/stream        — SSE stream for feedback updates
```

### Feedback panel template

The feedback panel is embedded in the dashboard page (or in the canvas page) as a collapsible sidebar section:

```html
<!-- internal/web/templates/partials/feedback_panel.html -->
{{define "feedback-panel"}}
<div class="feedback-panel" id="feedback-panel">
    <h3>Feedback</h3>

    <div class="feedback-history" id="feedback-history"
         hx-ext="sse"
         sse-connect="/feedback/stream"
         sse-swap="feedback-update"
         hx-swap="beforeend">
        {{range .History}}
        <div class="feedback-entry feedback-entry--{{.Source}}">
            <span class="feedback-source">{{.Source}}</span>
            <span class="feedback-content">{{.Content}}</span>
            {{range .Actions}}
            <div class="feedback-action">-> {{.}}</div>
            {{end}}
        </div>
        {{end}}
    </div>

    <div class="feedback-input">
        <form hx-post="/feedback"
              hx-target="#feedback-history"
              hx-swap="beforeend"
              hx-on::after-request="this.reset()">
            <input type="hidden" name="phase_id" value="{{.TargetPhaseID}}">
            <textarea name="content"
                      placeholder="Type feedback for the running execution..."
                      rows="2"></textarea>
            <div class="feedback-buttons">
                <button type="submit" class="feedback-send">Send</button>
                <button type="button" class="feedback-mic" id="mic-btn"
                        onclick="toggleRecording()">
                    Mic
                </button>
            </div>
        </form>
    </div>
</div>
{{end}}
```

### Text feedback handler

```go
// internal/web/feedback.go

func (s *Server) handleFeedbackPost(w http.ResponseWriter, r *http.Request) {
    content := r.FormValue("content")
    phaseID := r.FormValue("phase_id")

    if content == "" {
        http.Error(w, "empty feedback", http.StatusBadRequest)
        return
    }

    item := feedback.FeedbackItem{
        ID:            generateID(),
        Source:        feedback.SourceWeb,
        Content:       content,
        TargetPhaseID: phaseID,
        Timestamp:     time.Now(),
    }

    // Process through advisor asynchronously.
    go func() {
        summary := s.buildNebulaSummary()
        if err := s.advisor.Process(r.Context(), item, summary, s.feedbackQueue); err != nil {
            fmt.Fprintf(os.Stderr, "[web] feedback error: %v\n", err)
        }
    }()

    // Return the entry immediately for optimistic rendering.
    s.renderPartial(w, "feedback-entry", feedbackEntryData{
        Source:  "web",
        Content: content,
        Pending: true,
    })
}
```

### Audio upload handler

The browser captures audio via the MediaRecorder API and uploads it as a blob. The server runs it through the STT pipeline:

```go
func (s *Server) handleFeedbackAudio(w http.ResponseWriter, r *http.Request) {
    if s.transcriber == nil || !s.transcriber.Available() {
        http.Error(w, "STT not available", http.StatusServiceUnavailable)
        return
    }

    // Read audio blob (limit to 10MB).
    r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
    audioData, err := io.ReadAll(r.Body)
    if err != nil {
        http.Error(w, "read audio: "+err.Error(), http.StatusBadRequest)
        return
    }

    // Run through STT pipeline.
    text, err := s.transcriber.TranscribeBlob(r.Context(), audioData)
    if err != nil {
        http.Error(w, "transcription failed: "+err.Error(), http.StatusInternalServerError)
        return
    }

    item := feedback.FeedbackItem{
        ID:        generateID(),
        Source:    feedback.SourceVoice,
        Content:   text,
        Timestamp: time.Now(),
    }

    go func() {
        summary := s.buildNebulaSummary()
        s.advisor.Process(context.Background(), item, summary, s.feedbackQueue)
    }()

    // Return the transcribed entry.
    s.renderPartial(w, "feedback-entry", feedbackEntryData{
        Source:  "voice",
        Content: text,
        Pending: true,
    })
}
```

### Browser-side audio capture

A minimal inline script handles the MediaRecorder API (no external JS dependencies beyond HTMX):

```html
<script>
let mediaRecorder, audioChunks = [];

function toggleRecording() {
    const btn = document.getElementById('mic-btn');
    if (mediaRecorder && mediaRecorder.state === 'recording') {
        mediaRecorder.stop();
        btn.textContent = 'Mic';
        btn.classList.remove('recording');
        return;
    }

    navigator.mediaDevices.getUserMedia({audio: true}).then(stream => {
        mediaRecorder = new MediaRecorder(stream);
        audioChunks = [];

        mediaRecorder.ondataavailable = e => audioChunks.push(e.data);
        mediaRecorder.onstop = () => {
            const blob = new Blob(audioChunks, {type: 'audio/webm'});
            const fd = new FormData();
            fd.append('audio', blob);

            fetch('/feedback/audio', {method: 'POST', body: fd})
                .then(r => r.text())
                .then(html => {
                    document.getElementById('feedback-history')
                        .insertAdjacentHTML('beforeend', html);
                });

            stream.getTracks().forEach(t => t.stop());
        };

        mediaRecorder.start();
        btn.textContent = 'Stop';
        btn.classList.add('recording');
    });
}
</script>
```

### SSE for feedback updates

When the advisor finishes processing feedback, broadcast the result via SSE so all connected browsers update:

```go
func (s *Server) broadcastFeedbackProcessed(item feedback.FeedbackItem, actions []feedback.FeedbackAction) {
    actionDescs := make([]string, len(actions))
    for i, a := range actions {
        actionDescs[i] = fmt.Sprintf("%s %s", a.Kind, a.PhaseID)
    }

    var buf strings.Builder
    s.templates.ExecuteTemplate(&buf, "feedback-entry-processed", feedbackEntryData{
        Source:  string(item.Source),
        Content: item.Content,
        Actions: actionDescs,
    })
    s.broadcast(SSEEvent{
        Type: "feedback-update",
        Data: buf.String(),
    })
}
```

### Route registration

```go
s.mux.HandleFunc("GET /feedback", s.handleFeedbackHistory)
s.mux.HandleFunc("POST /feedback", s.handleFeedbackPost)
s.mux.HandleFunc("POST /feedback/audio", s.handleFeedbackAudio)
s.mux.HandleFunc("GET /feedback/stream", s.handleFeedbackStream)
```

## Files

- `internal/web/feedback.go` — `handleFeedbackPost`, `handleFeedbackAudio`, `handleFeedbackHistory`, `handleFeedbackStream`, `broadcastFeedbackProcessed`
- `internal/web/feedback_test.go` — tests for: text submission creates feedback item, empty content returns 400, audio upload transcribes and submits, SSE events contain processed actions, STT unavailable returns 503
- `internal/web/templates/partials/feedback_panel.html` — feedback panel with history, text input, mic button
- `internal/web/static/feedback.js` — `toggleRecording` function for MediaRecorder (can be inline in template)
- `internal/web/server.go` — register feedback routes, add `advisor`, `feedbackQueue`, `transcriber` fields

## Acceptance Criteria

- [ ] Text feedback submitted via POST is processed by the advisor agent
- [ ] Audio blobs uploaded via POST are transcribed by the STT pipeline
- [ ] Feedback history displays in the panel with source, content, and actions
- [ ] SSE pushes processed feedback results to all connected browsers
- [ ] MediaRecorder captures audio in the browser without external JS libraries
- [ ] Empty feedback text returns 400 Bad Request
- [ ] Audio upload when STT is unavailable returns 503 Service Unavailable
- [ ] Audio upload is limited to 10MB
- [ ] Feedback panel can be embedded in both dashboard and canvas pages
- [ ] `go test ./internal/web/...` passes
- [ ] `go vet ./...` passes
