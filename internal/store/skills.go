package store

import (
	"errors"
	"io/fs"
	"os"
	"sort"
)

// SkillMeta describes a personal skill pulled from the source account. The
// package itself (a zip with every file of the skill) is stored next to it.
type SkillMeta struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	UpdatedAt   string `json:"updated_at"`
	SizeBytes   int64  `json:"size_bytes"`
}

// SaveSkill writes the package first and the metadata last, so the metadata's
// presence means the download completed.
func (s *Store) SaveSkill(m SkillMeta, pkg []byte) error {
	if err := validID(m.ID); err != nil {
		return err
	}
	if err := WriteFileAtomic(s.path("skills", m.ID+".skill"), pkg); err != nil {
		return err
	}
	return writeJSON(s.path("skills", m.ID+".json"), m)
}

// LoadSkill returns a pulled skill's metadata and whether it exists.
func (s *Store) LoadSkill(id string) (SkillMeta, bool, error) {
	var m SkillMeta
	if err := validID(id); err != nil {
		return m, false, err
	}
	err := readJSON(s.path("skills", id+".json"), &m)
	if errors.Is(err, fs.ErrNotExist) {
		return m, false, nil
	}
	return m, err == nil, err
}

func (s *Store) ReadSkill(id string) ([]byte, error) {
	if err := validID(id); err != nil {
		return nil, err
	}
	return os.ReadFile(s.path("skills", id+".skill")) // #nosec G304 -- validated id inside the data folder
}

// ListSkills returns the pulled skills sorted by name.
func (s *Store) ListSkills() ([]SkillMeta, error) {
	var out []SkillMeta
	err := s.eachJSON(s.path("skills"), func(b []byte) error {
		var m SkillMeta
		if err := jsonUnmarshal(b, &m); err != nil {
			return err
		}
		out = append(out, m)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, err
}
