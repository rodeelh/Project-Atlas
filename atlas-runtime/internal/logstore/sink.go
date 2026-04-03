// Package logstore provides a thread-safe in-memory ring buffer for runtime log
// entries. It is the backing store for GET /logs.
//
// The global sink is populated by the agent loop, chat service, and skill
// execution paths. No external dependencies — all consumers import this package
// and call Write() directly.
package logstore

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"
)

const maxEntries = 500

// Entry is one log record. JSON tags match the web UI LogEntry interface in
// contracts.ts (id, level, message, timestamp, metadata).
type Entry struct {
	ID        string            `json:"id"`
	Level     string            `json:"level"`
	Message   string            `json:"message"`
	Timestamp string            `json:"timestamp"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// Sink is a fixed-size ring buffer of log entries. Safe for concurrent use.
type Sink struct {
	mu      sync.Mutex
	entries [maxEntries]Entry
	head    int // next write position
	count   int // number of entries stored (≤ maxEntries)
}

var global = &Sink{}

// Global returns the process-wide log sink.
func Global() *Sink { return global }

// Write appends a log entry to the global sink.
func Write(level, message string, meta map[string]string) {
	global.Write(level, message, meta)
}

// Write appends a log entry to this sink.
func (s *Sink) Write(level, message string, meta map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[s.head] = Entry{
		ID:        newID(),
		Level:     level,
		Message:   message,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Metadata:  meta,
	}
	s.head = (s.head + 1) % maxEntries
	if s.count < maxEntries {
		s.count++
	}
}

// Entries returns the n most recent entries in chronological order (oldest first).
// If n ≤ 0 or the sink is empty, returns nil.
func (s *Sink) Entries(n int) []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= 0 || s.count == 0 {
		return nil
	}
	if n > s.count {
		n = s.count
	}
	// The oldest entry in the ring sits at (head - count + maxEntries) % maxEntries.
	// We want the last n of those, so we skip (count - n).
	oldest := (s.head - s.count + maxEntries) % maxEntries
	skip := s.count - n
	start := (oldest + skip) % maxEntries
	out := make([]Entry, n)
	for i := 0; i < n; i++ {
		out[i] = s.entries[(start+i)%maxEntries]
	}
	return out
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}
