// Package chat implements the SSE message streaming infrastructure.
package chat

import (
	"encoding/json"
	"sync"

	"atlas-runtime-go/internal/logstore"
)

// SSEEvent is a single server-sent event.
// Field names match the Swift StreamBroadcaster event format exactly.
type SSEEvent struct {
	Type           string `json:"type"`
	Content        string `json:"content,omitempty"`
	Role           string `json:"role,omitempty"`
	ConversationID string `json:"conversationID,omitempty"`
	Error          string `json:"error,omitempty"`
	Status         string `json:"status,omitempty"`
	ToolName       string `json:"toolName,omitempty"`
	ApprovalID     string `json:"approvalID,omitempty"`
	ToolCallID     string `json:"toolCallID,omitempty"`
	Arguments      string `json:"arguments,omitempty"`
}

// Encoded returns the event serialised as an SSE data line.
func (e SSEEvent) Encoded() []byte {
	b, _ := json.Marshal(e)
	return append([]byte("data: "), append(b, '\n', '\n')...)
}

// Broadcaster multiplexes SSE events to registered per-conversation channels.
// Safe for concurrent use.
type Broadcaster struct {
	mu      sync.Mutex
	streams map[string]chan SSEEvent
}

// NewBroadcaster returns a ready Broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{streams: make(map[string]chan SSEEvent)}
}

// Register creates a buffered channel for conversationID and returns it.
// The caller must call Remove when the SSE connection closes.
func (b *Broadcaster) Register(convID string) <-chan SSEEvent {
	ch := make(chan SSEEvent, 256)
	b.mu.Lock()
	b.streams[convID] = ch
	b.mu.Unlock()
	return ch
}

// Emit sends an event to the registered channel for convID.
// It is a no-op if no listener is registered (e.g. client disconnected early).
func (b *Broadcaster) Emit(convID string, event SSEEvent) {
	b.mu.Lock()
	ch, ok := b.streams[convID]
	b.mu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- event:
	default:
		// Channel full — drop rather than block. Log so the operator knows.
		logstore.Write("warn", "SSE channel full — event dropped",
			map[string]string{"conv": convID, "type": event.Type})
	}
}

// Finish closes the channel for convID and removes it from the registry.
func (b *Broadcaster) Finish(convID string) {
	b.mu.Lock()
	ch, ok := b.streams[convID]
	if ok {
		delete(b.streams, convID)
	}
	b.mu.Unlock()
	if ok {
		close(ch)
	}
}

// Remove removes the channel for convID without closing it.
// Use this when the SSE handler exits before Finish is called.
func (b *Broadcaster) Remove(convID string) {
	b.mu.Lock()
	delete(b.streams, convID)
	b.mu.Unlock()
}

// ActiveCount returns the number of currently registered SSE listeners.
func (b *Broadcaster) ActiveCount() int {
	b.mu.Lock()
	n := len(b.streams)
	b.mu.Unlock()
	return n
}
