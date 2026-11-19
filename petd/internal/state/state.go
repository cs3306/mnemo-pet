// Package state persists the few things the user changes at runtime (chosen
// skin, which speech channels are switched on/off) in <data>/state.json.
package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

const (
	ModeAuto   = "auto"   // skin follows the wardrobe schedule
	ModeManual = "manual" // skin pinned by the user
)

type State struct {
	SkinMode string          `json:"skin_mode"`
	Skin     string          `json:"skin,omitempty"`
	Channels map[string]bool `json:"channels,omitempty"`
}

type Store struct {
	path string // empty = memory only
	mu   sync.Mutex
	s    State
}

// Memory returns a store that never touches the disk (tests, fallback).
func Memory() *Store { return &Store{s: State{SkinMode: ModeAuto}} }

// Open loads path; a missing file yields defaults.
func Open(path string) (*Store, error) {
	st := &Store{path: path, s: State{SkinMode: ModeAuto}}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(b, &st.s); err != nil {
		return nil, err
	}
	if st.s.SkinMode != ModeManual {
		st.s.SkinMode = ModeAuto
	}
	return st, nil
}

// Get returns a copy of the current state.
func (s *Store) Get() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.s)
}

// Update applies f and persists atomically. The in-memory value is updated even
// if writing fails, and the write error is returned.
func (s *Store) Update(f func(*State)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(&s.s)
	if s.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(s.s, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(s.path), ".state.json.tmp")
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func clone(s State) State {
	out := s
	if s.Channels != nil {
		out.Channels = make(map[string]bool, len(s.Channels))
		for k, v := range s.Channels {
			out.Channels[k] = v
		}
	}
	return out
}
