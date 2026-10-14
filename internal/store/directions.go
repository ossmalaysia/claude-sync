package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Direction owns an isolated download, selection and push history for an org pair.
// Empty ID refers to the original migration directory, which is never moved.
type Direction struct {
	ID         string `json:"id"`
	SourceOrg  string `json:"source_org"`
	TargetOrg  string `json:"target_org"`
	SourceName string `json:"source_name"`
	TargetName string `json:"target_name"`
	Reversed   bool   `json:"reversed"`
	Active     bool   `json:"active"`
}
type directionIndex struct {
	Active     string               `json:"active"`
	Directions map[string]Direction `json:"directions"`
}

func directionKey(from, to string) string {
	sum := sha256.Sum256([]byte(from + "\x00" + to))
	return hex.EncodeToString(sum[:])
}
func (s *Store) directionDir(id string) string {
	if id == "" {
		return filepath.Join(s.root, "migration")
	}
	return filepath.Join(s.root, "directions", id, "migration")
}
func (s *Store) directionIndex() (directionIndex, error) {
	var idx directionIndex
	err := readJSON(filepath.Join(s.root, "directions.json"), &idx)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return idx, err
	}
	for _, d := range idx.Directions {
		if d.ID != "" {
			if err := validID(d.ID); err != nil {
				return idx, err
			}
		}
	}
	if idx.Directions == nil {
		idx.Directions = map[string]Direction{}
	}
	// Register the pre-profile history with its actual orgs, not the newly chosen orgs.
	if len(idx.Directions) == 0 {
		legacy := &Store{root: s.root, dataDir: filepath.Join(s.root, "migration")}
		settings, err := legacy.LoadSettings()
		if err != nil {
			return idx, err
		}
		if settings.SourceOrg == "" {
			if m, e := legacy.LoadManifest(); e == nil {
				settings.SourceOrg = m.SourceOrg
			}
		}
		if settings.TargetOrg == "" {
			if state, e := legacy.LoadState(); e == nil {
				settings.TargetOrg = state.TargetOrg
			}
		}
		if settings.SourceOrg != "" && settings.TargetOrg != "" {
			key := directionKey(settings.SourceOrg, settings.TargetOrg)
			idx.Active = key
			idx.Directions[key] = Direction{SourceOrg: settings.SourceOrg, TargetOrg: settings.TargetOrg, SourceName: settings.SourceOrgName, TargetName: settings.TargetOrgName}
		}
	}
	return idx, nil
}
func (s *Store) ListDirections() ([]Direction, error) {
	idx, err := s.directionIndex()
	if err != nil {
		return nil, err
	}
	out := make([]Direction, 0, len(idx.Directions))
	for key, d := range idx.Directions {
		d.Active = key == idx.Active
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SourceOrg+out[i].TargetOrg < out[j].SourceOrg+out[j].TargetOrg })
	return out, nil
}

func (s *Store) ActivateDirection(from, to, fromName, toName string, reversed bool) error {
	release, err := s.AcquireJob("direction")
	if err != nil {
		return err
	}
	defer release()
	if from == "" || to == "" || from == to {
		return fmt.Errorf("choose two different organizations")
	}
	idx, err := s.directionIndex()
	if err != nil {
		return err
	}
	key := directionKey(from, to)
	d, exists := idx.Directions[key]
	current, err := s.LoadSettings()
	if err != nil {
		return err
	}
	initial := len(idx.Directions) == 0
	if !exists {
		d = Direction{ID: key, SourceOrg: from, TargetOrg: to}
		if initial {
			d.ID = ""
		}
	}
	d.SourceName, d.TargetName, d.Reversed = fromName, toName, reversed
	// Do all local preparation before publishing the active pointer.
	dest := &Store{root: s.root, dataDir: s.directionDir(d.ID), reversed: reversed}
	if err := os.MkdirAll(filepath.Join(dest.dataDir, "projects"), 0o700); err != nil {
		return err
	}
	settings, err := dest.LoadSettings()
	if err != nil {
		return err
	}
	if !exists && !initial {
		settings = current
		settings.VerifiedAt, settings.MemorySentAt, settings.LastSyncAt, settings.CopyReviewedAt = time.Time{}, time.Time{}, time.Time{}, time.Time{}
		settings.VerifyOK, settings.VerifyTotal, settings.VerifyWaiting = 0, 0, 0
		settings.MemorySHA = ""
		settings.LastSyncNewProjects, settings.LastSyncChangedProjects, settings.LastSyncChats, settings.LastSyncArtifacts, settings.LastSyncSent = 0, 0, 0, 0, 0
	}
	settings.SourceOrg, settings.TargetOrg, settings.SourceOrgName, settings.TargetOrgName = from, to, fromName, toName
	if err := dest.SaveSettings(settings); err != nil {
		return err
	}
	state, err := dest.LoadState()
	if err != nil {
		return err
	}
	if state.TargetOrg != "" && state.TargetOrg != to {
		return fmt.Errorf("saved direction has a different target")
	}
	state.TargetOrg = to
	if opposite, ok := idx.Directions[directionKey(to, from)]; ok {
		other := &Store{root: s.root, dataDir: s.directionDir(opposite.ID)}
		forward, err := other.LoadState()
		if err != nil {
			return err
		}
		seedReverse(state, forward)
	}
	if err := dest.SaveState(state); err != nil {
		return err
	}
	idx.Active = key
	idx.Directions[key] = d
	if err := writeJSON(filepath.Join(s.root, "directions.json"), idx); err != nil {
		return err
	}
	s.mu.Lock()
	s.dataDir, s.reversed = dest.dataDir, reversed
	s.mu.Unlock()
	return nil
}

// Both histories retain their IDs. Seed only missing links, so switching back
// cannot erase a conflict or newer baseline recorded in the other direction.
func seedReverse(reverse, forward *State) {
	if reverse.Skills == nil {
		reverse.Skills = map[string]*ItemState{}
	}
	for id, it := range forward.Skills {
		if it != nil && it.Status == StatusDone && it.Target != "" && reverse.Skills[it.Target] == nil {
			reverse.Skills[it.Target] = &ItemState{Status: StatusDone, Target: id, SHA: it.SHA, Name: it.Name}
		}
	}
	for src, p := range forward.Projects {
		if p == nil || p.Target == "" || src == UnassignedProjectID {
			continue
		}
		r := reverse.Project(p.Target)
		if r.Target != "" && r.Target != src {
			continue
		}
		r.Target = src
		invert := func(from, to map[string]*ItemState, export bool) {
			for id, it := range from {
				if it == nil || it.Status != StatusDone || it.Target == "" || to[it.Target] != nil {
					continue
				}
				target := id
				if export {
					target = ""
				}
				to[it.Target] = &ItemState{Status: StatusDone, Target: target, SHA: it.SHA, OriginExport: export}
			}
		}
		invert(p.Docs, r.Docs, false)
		invert(p.Files, r.Files, false)
		invert(p.Artifacts, r.Docs, true)
		invert(p.Chats, r.Docs, true)
		if r.Instructions == nil && p.Instructions != nil && p.Instructions.Status == StatusDone {
			r.Instructions = &ItemState{Status: StatusDone, SHA: p.Instructions.SHA}
		}
	}
}

func (s *Store) Reversed() bool { s.mu.RLock(); defer s.mu.RUnlock(); return s.reversed }
