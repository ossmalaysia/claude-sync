package store

import (
	"errors"
	"io/fs"
	"time"
)

// VerifyResult is the last check of one source project's copy in the target.
type VerifyResult struct {
	OK        bool      `json:"ok"`
	Waiting   int       `json:"waiting,omitempty"` // artifacts not sent yet (the only difference)
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// LoadVerify returns the last check per source project uuid.
func (s *Store) LoadVerify() (map[string]VerifyResult, error) {
	out := map[string]VerifyResult{}
	err := readJSON(s.path("verify.json"), &out)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]VerifyResult{}, nil
	}
	return out, err
}

// RecordVerify merges new check results into verify.json.
func (s *Store) RecordVerify(results map[string]VerifyResult) error {
	all, err := s.LoadVerify()
	if err != nil {
		return err
	}
	for k, v := range results {
		all[k] = v
	}
	return writeJSON(s.path("verify.json"), all)
}

// ForgetVerify drops a project's last check because its content changed.
func (s *Store) ForgetVerify(project string) error {
	all, err := s.LoadVerify()
	if err != nil {
		return err
	}
	if _, ok := all[project]; !ok {
		return nil
	}
	delete(all, project)
	return writeJSON(s.path("verify.json"), all)
}
