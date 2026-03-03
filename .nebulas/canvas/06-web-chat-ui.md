+++
id = "web-chat-ui"
title = "Web chat UI for canvas sessions with streaming responses"
type = "feature"
priority = 2
depends_on = ["session-persistence", "architect-agent"]
scope = ["internal/web/canvas.go", "internal/web/canvas_test.go", "internal/web/templates/canvas.html", "internal/web/templates/partials/chat_message.html"]
+++

## Problem

The CLI REPL (phase 3) provides a functional but limited canvas experience — no rich formatting, no side-by-side phase preview, no persistent visual context. The web UI is the primary canvas experience: a chat interface with streaming architect responses, a draft phase sidebar, and a generate button. The meridian web infrastructure (SSE, templates, HTMX) provides the foundation.

## Solution

### Routes

```go
// internal/web/canvas.go

// Canvas routes:
// GET  /canvas              — new session page or session list
// GET  /canvas/{id}         — resume existing session
// POST /canvas/{id}/message — send user message, stream architect response
// POST /canvas/{id}/generate — generate nebula files from draft
// GET  /canvas/{id}/phases  — current draft phases sidebar (HTMX partial)
```

### Chat page template

The canvas page has three sections: chat history (left/center), draft phases sidebar (right), and input bar (bottom).

```html
<!-- internal/web/templates/canvas.html -->
<div class="canvas-layout">
    <div class="canvas-chat" id="chat-history">
        {{range .Session.Turns}}
        {{template "chat-message" .}}
        {{end}}
    </div>

    <div class="canvas-sidebar" id="draft-phases">
        {{template "phase-sidebar" .Session.DraftPhases}}
    </div>

    <div class="canvas-input">
        <form hx-post="/canvas/{{.Session.ID}}/message"
              hx-target="#chat-history"
              hx-swap="beforeend"
              hx-on::after-request="this.reset(); scrollToBottom()">
            <textarea name="content" placeholder="Describe what you want to build..."
                      rows="3" autofocus></textarea>
            <button type="submit">Send</button>
        </form>
        <button hx-post="/canvas/{{.Session.ID}}/generate"
                hx-target="#generate-result"
                class="canvas-generate">
            Generate Nebula
        </button>
        <div id="generate-result"></div>
    </div>
</div>
```

### Message handler with SSE streaming

When the user sends a message, the handler appends the user turn, invokes the architect agent, and streams the response token-by-token via SSE:

```go
func (s *Server) handleCanvasMessage(w http.ResponseWriter, r *http.Request) {
    sessionID := r.PathValue("id")
    content := r.FormValue("content")

    session, err := s.canvasStore.Load(sessionID)
    if err != nil {
        http.Error(w, "session not found", http.StatusNotFound)
        return
    }

    // Append user turn.
    session.AddTurn(canvas.Turn{
        Role:      canvas.RoleUser,
        Content:   content,
        Timestamp: time.Now(),
    })

    // Render user message bubble immediately.
    s.renderPartial(w, "chat-message", canvas.Turn{
        Role: canvas.RoleUser, Content: content,
    })

    // Invoke architect asynchronously. Stream response via SSE.
    go func() {
        response, draftPhases := s.architect.Respond(r.Context(), session)
        session.AddTurn(canvas.Turn{
            Role:      canvas.RoleArchitect,
            Content:   response,
            Timestamp: time.Now(),
        })
        session.DraftPhases = draftPhases
        s.canvasStore.Save(session)

        // Push architect response and updated sidebar via SSE.
        s.broadcastCanvasUpdate(sessionID, response, draftPhases)
    }()
}
```

### SSE events for canvas

Canvas-specific SSE event types stream to the chat page:

```go
// Canvas SSE events:
// "canvas-message"  — new architect message (appended to chat)
// "canvas-phases"   — updated draft phase sidebar
// "canvas-typing"   — typing indicator (architect is thinking)
// "canvas-done"     — architect finished responding
```

The chat page subscribes to these:

```html
<div id="chat-history"
     hx-ext="sse"
     sse-connect="/events?channel=canvas-{{.Session.ID}}"
     sse-swap="canvas-message"
     hx-swap="beforeend">
```

### Generate handler

```go
func (s *Server) handleCanvasGenerate(w http.ResponseWriter, r *http.Request) {
    sessionID := r.PathValue("id")
    session, err := s.canvasStore.Load(sessionID)
    if err != nil {
        http.Error(w, "session not found", http.StatusNotFound)
        return
    }

    draft := session.BuildDraftNebula()
    writer := canvas.NewWriter(".nebulas")

    // Validate before writing.
    if errs := canvas.ValidateDraft(draft); len(errs) > 0 {
        s.renderPartial(w, "generate-errors", errs)
        return
    }

    dir, validationErrs, err := writer.Generate(draft)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    session.Generated = true
    session.GeneratedDir = dir
    s.canvasStore.Save(session)

    s.renderPartial(w, "generate-success", map[string]any{
        "Dir":    dir,
        "Errors": validationErrs,
    })
}
```

### Draft phase sidebar

The sidebar shows the current draft phases as a compact list with phase ID, title, and dependency arrows. Updated via SSE whenever the architect proposes changes:

```html
{{define "phase-sidebar"}}
<div class="phase-list">
    {{range .}}
    <div class="phase-card phase-card--draft">
        <span class="phase-id">{{.ID}}</span>
        <span class="phase-title">{{.Title}}</span>
        {{if .DependsOn}}
        <span class="phase-deps">depends: {{join .DependsOn ", "}}</span>
        {{end}}
    </div>
    {{end}}
</div>
{{end}}
```

## Files

- `internal/web/canvas.go` — route handlers: `handleCanvasPage`, `handleCanvasMessage`, `handleCanvasGenerate`, `handleCanvasPhases`, SSE broadcasting for canvas events
- `internal/web/canvas_test.go` — tests for: message POST appends turn, generate writes files, session not found returns 404, SSE events contain correct data
- `internal/web/templates/canvas.html` — full canvas page layout with chat, sidebar, input
- `internal/web/templates/partials/chat_message.html` — chat message bubble partial (user vs architect styling)
- `internal/web/templates/partials/phase_sidebar.html` — draft phase list partial
- `internal/web/templates/partials/generate_result.html` — generation success/error partials
- `internal/web/static/canvas.css` — canvas-specific styles (chat bubbles, sidebar, input bar)
- `internal/web/server.go` — register canvas routes, wire `SessionStore`

## Acceptance Criteria

- [ ] `GET /canvas` shows session list or new session form
- [ ] `GET /canvas/{id}` renders chat history with all prior turns
- [ ] `POST /canvas/{id}/message` appends user turn and invokes architect
- [ ] Architect response streams to chat via SSE (progressive rendering)
- [ ] Draft phase sidebar updates when architect proposes phase changes
- [ ] `POST /canvas/{id}/generate` writes nebula files to `.nebulas/<name>/`
- [ ] Validation errors are displayed without writing files
- [ ] Chat messages are styled differently for user vs architect
- [ ] Session state is saved after every turn
- [ ] Canvas page is functional without JavaScript beyond HTMX
- [ ] `go test ./internal/web/...` passes
- [ ] `go vet ./...` passes
