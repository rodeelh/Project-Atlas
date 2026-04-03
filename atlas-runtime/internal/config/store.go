package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// Store loads and saves the RuntimeConfigSnapshot atomically.
// It mirrors AtlasConfigStore in the Swift runtime: in-process cache +
// atomic file writes (temp file → rename). Thread-safe via RWMutex.
type Store struct {
	mu         sync.RWMutex
	cached     *RuntimeConfigSnapshot
	path       string
	legacyPath string
}

// NewStore returns a Store that reads/writes the canonical config paths.
func NewStore() *Store {
	return &Store{
		path:       ConfigPath(),
		legacyPath: LegacyConfigPath(),
	}
}

// NewStoreAt returns a Store bound to specific paths (useful in tests).
func NewStoreAt(path, legacyPath string) *Store {
	return &Store{path: path, legacyPath: legacyPath}
}

// Load returns the current config snapshot, reading from disk on first call.
// Returns defaults if the config file does not exist or is unreadable.
func (s *Store) Load() RuntimeConfigSnapshot {
	s.mu.RLock()
	if s.cached != nil {
		c := *s.cached
		s.mu.RUnlock()
		return c
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cached != nil {
		return *s.cached
	}

	snap := s.readFromDisk()
	s.cached = &snap
	return snap
}

// Save writes the snapshot atomically and refreshes the cache.
// Uses the same temp-file → rename pattern as AtlasConfigStore.persist().
func (s *Store) Save(snap RuntimeConfigSnapshot) error {
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("config: mkdir: %w", err)
	}

	tmp := s.path + ".tmp." + randomHex(4)
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("config: write tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("config: rename: %w", err)
	}

	s.mu.Lock()
	s.cached = &snap
	s.mu.Unlock()
	return nil
}

// Invalidate clears the in-process cache, forcing the next Load to re-read disk.
func (s *Store) Invalidate() {
	s.mu.Lock()
	s.cached = nil
	s.mu.Unlock()
}

// readFromDisk reads and JSON-decodes the config file.
// Tries the canonical path first, then the legacy path.
// Returns defaults on any error so the runtime always starts cleanly.
func (s *Store) readFromDisk() RuntimeConfigSnapshot {
	path := s.path
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if _, err2 := os.Stat(s.legacyPath); err2 == nil {
			path = s.legacyPath
		} else {
			return Defaults()
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Defaults()
	}

	snap := Defaults()
	if err := json.Unmarshal(data, &snap); err != nil {
		log.Printf("Atlas: config file at %s is malformed (%v) — using defaults", path, err)
		return Defaults()
	}
	return snap
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
