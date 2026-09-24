// Package store owns the local data folder: pulled projects, selection,
// push state and browser profiles.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Store struct{ root string }

// DefaultRoot is <user config dir>/claude-sync.
func DefaultRoot() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "claude-sync"), nil
}

func Open(root string) (*Store, error) {
	for _, d := range []string{filepath.Join(root, "migration", "projects"), filepath.Join(root, "profiles")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	return &Store{root: root}, nil
}

func (s *Store) Root() string { return s.root }

func (s *Store) ProfileDir(account string) string { return filepath.Join(s.root, "profiles", account) }

func (s *Store) path(parts ...string) string {
	return filepath.Join(append([]string{s.root, "migration"}, parts...)...)
}

func validID(id string) error {
	if id == "" || strings.ContainsAny(id, `/\:`) || strings.Contains(id, "..") {
		return fmt.Errorf("unsafe id %q", id)
	}
	return nil
}

// WriteFileAtomic writes data to a temp file in the same directory and
// renames it over path, so readers never see a half-written file.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, b)
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

type Manifest struct {
	SourceAccount string    `json:"source_account"` // email of the source login, "" if unknown
	SourceOrg     string    `json:"source_org"`
	PulledAt      time.Time `json:"pulled_at"` // when the last COMPLETE pull finished
	Total         int       `json:"total"`     // projects in the source at the last pull start
	Complete      bool      `json:"complete"`  // false while a pull is running or was stopped
	Projects      int       `json:"projects"`
	Docs          int       `json:"docs"`
	Files         int       `json:"files"`
}

func (s *Store) SaveManifest(m Manifest) error { return writeJSON(s.path("manifest.json"), m) }

func (s *Store) LoadManifest() (Manifest, error) {
	var m Manifest
	if err := readJSON(s.path("manifest.json"), &m); err != nil {
		return m, err
	}
	// Manifests from before total/complete existed were only written when a
	// pull finished.
	if m.Total == 0 && !m.Complete && !m.PulledAt.IsZero() {
		m.Total, m.Complete = m.Projects, true
	}
	return m, nil
}

type ProjectMeta struct {
	UUID           string `json:"uuid"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	IsPrivate      bool   `json:"is_private"`
	IsStarred      bool   `json:"is_starred"`
	PromptTemplate string `json:"prompt_template"`
	UpdatedAt      string `json:"updated_at"`
	Order          int    `json:"order"`
}

func (s *Store) SaveProject(p ProjectMeta) error {
	if err := validID(p.UUID); err != nil {
		return err
	}
	return writeJSON(s.path("projects", p.UUID, "project.json"), p)
}

func (s *Store) ListProjects() ([]ProjectMeta, error) {
	entries, err := os.ReadDir(s.path("projects"))
	if err != nil {
		return nil, err
	}
	var out []ProjectMeta
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var p ProjectMeta
		if err := readJSON(s.path("projects", e.Name(), "project.json"), &p); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

type DocRecord struct {
	UUID     string `json:"uuid"`
	FileName string `json:"file_name"`
	Content  string `json:"content"`
}

func (s *Store) SaveDoc(projectUUID string, d DocRecord) error {
	if err := errors.Join(validID(projectUUID), validID(d.UUID)); err != nil {
		return err
	}
	return writeJSON(s.path("projects", projectUUID, "docs", d.UUID+".json"), d)
}

func (s *Store) ListDocs(projectUUID string) ([]DocRecord, error) {
	var out []DocRecord
	err := s.eachJSON(s.path("projects", projectUUID, "docs"), func(b []byte) error {
		var d DocRecord
		if err := json.Unmarshal(b, &d); err != nil {
			return err
		}
		out = append(out, d)
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].FileName != out[j].FileName {
			return out[i].FileName < out[j].FileName
		}
		return out[i].UUID < out[j].UUID
	})
	return out, err
}

type FileMeta struct {
	UUID      string `json:"uuid"`
	FileName  string `json:"file_name"`
	FileKind  string `json:"file_kind"`
	SizeBytes int64  `json:"size_bytes"`
	Mime      string `json:"mime"`
	// ConvertedFrom is the original file name when only a re-encoded
	// preview could be downloaded (claude.ai keeps no original for images).
	ConvertedFrom string `json:"converted_from,omitempty"`
}

// SaveFile writes the bytes first and the .json metadata last, so the
// metadata's presence (HasFile) means the download completed.
func (s *Store) SaveFile(projectUUID string, m FileMeta, data []byte) error {
	if err := errors.Join(validID(projectUUID), validID(m.UUID)); err != nil {
		return err
	}
	if err := WriteFileAtomic(s.path("projects", projectUUID, "files", m.UUID+".bin"), data); err != nil {
		return err
	}
	return writeJSON(s.path("projects", projectUUID, "files", m.UUID+".json"), m)
}

func (s *Store) HasFile(projectUUID, fileUUID string) bool {
	_, err := os.Stat(s.path("projects", projectUUID, "files", fileUUID+".json"))
	return err == nil
}

func (s *Store) ReadFile(projectUUID, fileUUID string) ([]byte, error) {
	return os.ReadFile(s.path("projects", projectUUID, "files", fileUUID+".bin"))
}

func (s *Store) ListFiles(projectUUID string) ([]FileMeta, error) {
	var out []FileMeta
	err := s.eachJSON(s.path("projects", projectUUID, "files"), func(b []byte) error {
		var m FileMeta
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		out = append(out, m)
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].FileName != out[j].FileName {
			return out[i].FileName < out[j].FileName
		}
		return out[i].UUID < out[j].UUID
	})
	return out, err
}

// eachJSON calls fn for every *.json file in dir; a missing dir is empty.
func (s *Store) eachJSON(dir string, fn func([]byte) error) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" || strings.HasPrefix(e.Name(), ".tmp") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		if err := fn(b); err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
	}
	return nil
}

func (s *Store) SaveMemory(m string) error { return WriteFileAtomic(s.path("memory.md"), []byte(m)) }

func (s *Store) LoadMemory() (string, error) {
	b, err := os.ReadFile(s.path("memory.md"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

func (s *Store) LoadSelection() (map[string]bool, error) {
	sel := map[string]bool{}
	err := readJSON(s.path("selection.json"), &sel)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]bool{}, nil
	}
	return sel, err
}

func (s *Store) SaveSelection(sel map[string]bool) error {
	return writeJSON(s.path("selection.json"), sel)
}
