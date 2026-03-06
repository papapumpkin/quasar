// Package dialogue provides a generic interactive dialogue abstraction for
// back-and-forth communication between automated processes (agents, schedulers)
// and humans. It is display-agnostic — the TUI, stderr, or any other frontend
// implements the Opener interface to bridge sessions to the user.
package dialogue

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrClosed is returned when Send or Receive is called on a closed session.
var ErrClosed = errors.New("dialogue session closed")

// Role identifies the sender of a message.
type Role string

const (
	RoleAgent  Role = "agent"
	RoleHuman  Role = "human"
	RoleSystem Role = "system"
)

// Message is a single entry in a dialogue thread.
type Message struct {
	Role    Role
	Content string
	Time    time.Time
}

// Request describes the context for opening a new dialogue session.
type Request struct {
	PhaseID string   // optional: related phase
	Title   string   // short summary shown in overlay header
	Context string   // detailed markdown context (scrollable panel)
	Kind    string   // "escalation", "question", "review", etc.
	Options []string // optional quick-select options
}

// Opener creates interactive dialogue sessions. Implementations bridge
// to the display layer (TUI, stderr, etc.). Consumed wherever interactive
// human input is needed.
type Opener interface {
	Open(ctx context.Context, req Request) (Session, error)
}

// Session is the agent-facing handle for an active dialogue. The automated
// process calls Send to post messages and Receive to wait for human input.
type Session interface {
	// ID returns the unique session identifier.
	ID() string

	// Send posts a message from the agent to the human.
	Send(ctx context.Context, content string) error

	// Receive blocks until the human sends a message.
	Receive(ctx context.Context) (string, error)

	// Close ends the dialogue. After Close, Send and Receive return errors.
	Close()

	// Transcript returns all messages exchanged so far.
	Transcript() []Message
}

// DisplayHandle provides the display layer (TUI) access to the communication
// channels of a session. This is separate from Session (the agent-facing
// interface) to keep concerns cleanly separated.
type DisplayHandle interface {
	ID() string
	Request() Request
	ToHuman() <-chan Message
	FromHuman() chan<- Message
	Transcript() []Message
	Closed() <-chan struct{}
}

// NewSession creates a new in-process dialogue session. The returned value
// satisfies both Session (for the agent) and DisplayHandle (for the TUI).
func NewSession(req Request) *MemSession {
	return &MemSession{
		id:        generateID(),
		request:   req,
		toHuman:   make(chan Message, 8),
		fromHuman: make(chan Message, 8),
		closed:    make(chan struct{}),
	}
}

// MemSession is the default in-process session implementation. It satisfies
// both Session and DisplayHandle.
type MemSession struct {
	id        string
	request   Request
	toHuman   chan Message
	fromHuman chan Message
	messages  []Message
	mu        sync.Mutex
	closed    chan struct{}
	closeOnce sync.Once
}

func (s *MemSession) ID() string       { return s.id }
func (s *MemSession) Request() Request { return s.request }

// ToHuman returns the channel the display layer reads from.
func (s *MemSession) ToHuman() <-chan Message { return s.toHuman }

// FromHuman returns the channel the display layer writes to.
func (s *MemSession) FromHuman() chan<- Message { return s.fromHuman }

// Closed returns a channel that is closed when the session ends.
func (s *MemSession) Closed() <-chan struct{} { return s.closed }

// Send posts a message from the agent to the human.
func (s *MemSession) Send(ctx context.Context, content string) error {
	msg := Message{Role: RoleAgent, Content: content, Time: time.Now()}
	s.mu.Lock()
	s.messages = append(s.messages, msg)
	s.mu.Unlock()

	select {
	case s.toHuman <- msg:
		return nil
	case <-s.closed:
		return ErrClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Receive blocks until the human sends a message.
func (s *MemSession) Receive(ctx context.Context) (string, error) {
	select {
	case msg, ok := <-s.fromHuman:
		if !ok {
			return "", ErrClosed
		}
		s.mu.Lock()
		s.messages = append(s.messages, msg)
		s.mu.Unlock()
		return msg.Content, nil
	case <-s.closed:
		return "", ErrClosed
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Close ends the dialogue session.
func (s *MemSession) Close() {
	s.closeOnce.Do(func() { close(s.closed) })
}

// Transcript returns a copy of all messages exchanged so far.
func (s *MemSession) Transcript() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Message, len(s.messages))
	copy(out, s.messages)
	return out
}

func generateID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return fmt.Sprintf("dlg-%x", b)
}
