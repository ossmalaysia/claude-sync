package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"
)

const (
	StatusDone    = "done"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"
)

var ErrCorruptState = errors.New("state.json is corrupt")

// ItemState tracks one pushed item (instructions, a doc or a file).
type ItemState struct {
	Status   string `json:"status"`
	Target   string `json:"target,omitempty"`
	SHA      string `json:"sha,omitempty"` // content hash at push time (docs, instructions)
	Error    string `json:"error,omitempty"`
	Attempts int    `json:"attempts,omitempty"`
}

type ProjectState struct {
	Target       string                `json:"target,omitempty"`
	Status       string                `json:"status,omitempty"` // "failed" when creation failed
	Error        string                `json:"error,omitempty"`
	Instructions *ItemState            `json:"instructions,omitempty"`
	Docs         map[string]*ItemState `json:"docs"`
	Files        map[string]*ItemState `json:"files"`
}

// State is push progress keyed by SOURCE uuids.
type State struct {
	TargetOrg string                   `json:"target_org"`
	Projects  map[string]*ProjectState `json:"projects"`
}

// Project returns the entry for a source project, creating it if needed.
func (s *State) Project(src string) *ProjectState {
	if s.Projects == nil {
		s.Projects = map[string]*ProjectState{}
	}
	ps := s.Projects[src]
	if ps == nil {
		ps = &ProjectState{}
		s.Projects[src] = ps
	}
	if ps.Docs == nil {
		ps.Docs = map[string]*ItemState{}
	}
	if ps.Files == nil {
		ps.Files = map[string]*ItemState{}
	}
	return ps
}

func (s *Store) LoadState() (*State, error) {
	b, err := os.ReadFile(s.path("state.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return &State{Projects: map[string]*ProjectState{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorruptState, err)
	}
	if st.Projects == nil {
		st.Projects = map[string]*ProjectState{}
	}
	return &st, nil
}

func (s *Store) SaveState(st *State) error { return writeJSON(s.path("state.json"), st) }

// StateUpdatedAt is when push progress last changed (zero if never pushed).
func (s *Store) StateUpdatedAt() time.Time {
	fi, err := os.Stat(s.path("state.json"))
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}
