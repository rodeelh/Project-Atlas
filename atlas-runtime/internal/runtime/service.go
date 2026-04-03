// Package runtime tracks the lifecycle state of the Go runtime process and
// provides the RuntimeStatus response shape consumed by the web UI.
package runtime

import (
	"sync"
	"sync/atomic"
	"time"
)

// State represents the runtime lifecycle state.
type State string

const (
	StateStarting State = "starting"
	StateReady    State = "ready"
	StateDegraded State = "degraded"
	StateStopped  State = "stopped"
)

// Status is the JSON response shape for GET /status.
// Field names match the contracts.ts RuntimeStatus interface exactly.
type Status struct {
	IsRunning               bool    `json:"isRunning"`
	ActiveConversationCount int     `json:"activeConversationCount"`
	LastMessageAt           *string `json:"lastMessageAt,omitempty"`
	LastError               *string `json:"lastError,omitempty"`
	State                   string  `json:"state"`
	RuntimePort             int     `json:"runtimePort"`
	StartedAt               *string `json:"startedAt,omitempty"`
	ActiveRequests          int32   `json:"activeRequests"`
	PendingApprovalCount    int     `json:"pendingApprovalCount"`
	Details                 string  `json:"details"`
	TokensIn                int64   `json:"tokensIn"`
	TokensOut               int64   `json:"tokensOut"`
}

// Service tracks the runtime's lifecycle state.
// It is safe for concurrent use.
type Service struct {
	mu            sync.RWMutex
	state         State
	startedAt     *time.Time
	lastMessageAt *time.Time
	lastError     string
	port          int
	activeReqs    int32 // atomic
}

// NewService returns a Service in the StateStarting state.
func NewService(port int) *Service {
	return &Service{state: StateStarting, port: port}
}

// MarkStarted transitions the service to StateReady.
func (s *Service) MarkStarted() {
	now := time.Now()
	s.mu.Lock()
	s.state = StateReady
	s.startedAt = &now
	s.mu.Unlock()
}

// RecordMessage records the timestamp of the most recent message.
func (s *Service) RecordMessage() {
	now := time.Now()
	s.mu.Lock()
	s.lastMessageAt = &now
	s.mu.Unlock()
}

// RecordError records an error string for surfacing in the status response.
func (s *Service) RecordError(msg string) {
	s.mu.Lock()
	s.lastError = msg
	s.mu.Unlock()
}

// TrackRequest increments / decrements the active request counter.
func (s *Service) TrackRequest(delta int32) {
	atomic.AddInt32(&s.activeReqs, delta)
}

// GetStatus returns the current RuntimeStatus snapshot.
// tokensIn and tokensOut are the session-wide token counts, typically obtained
// from agent.GetSessionTokens().
func (s *Service) GetStatus(activeConvCount int, tokensIn, tokensOut int64) Status {
	s.mu.RLock()
	defer s.mu.RUnlock()

	st := Status{
		IsRunning:               s.state == StateReady,
		ActiveConversationCount: activeConvCount,
		State:                   string(s.state),
		RuntimePort:             s.port,
		ActiveRequests:          atomic.LoadInt32(&s.activeReqs),
		PendingApprovalCount:    0,
		Details:                 "Atlas Go runtime",
		TokensIn:                tokensIn,
		TokensOut:               tokensOut,
	}

	if s.startedAt != nil {
		ts := s.startedAt.UTC().Format(time.RFC3339)
		st.StartedAt = &ts
	}
	if s.lastMessageAt != nil {
		ts := s.lastMessageAt.UTC().Format(time.RFC3339)
		st.LastMessageAt = &ts
	}
	if s.lastError != "" {
		msg := s.lastError
		st.LastError = &msg
	}

	return st
}
