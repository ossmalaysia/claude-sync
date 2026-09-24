package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ArtifactRecord is one artifact or Claude-written file recovered from a chat,
// at its final version.
type ArtifactRecord struct {
	ID       string `json:"id"`   // "artifact:<id>" or "file:<path>", unique within the chat
	Kind     string `json:"kind"` // "artifact" | "file"
	Title    string `json:"title"`
	Type     string `json:"type"`      // artifact type, e.g. text/markdown
	FileName string `json:"file_name"` // name used for the target doc and the export
	Content  string `json:"content"`
}

// ChatRecord is what pull keeps per chat. UpdatedAt lets the next pull skip
// chats that have not changed.
type ChatRecord struct {
	UUID        string           `json:"uuid"`
	Name        string           `json:"name"`
	ProjectUUID string           `json:"project_uuid"`
	UpdatedAt   string           `json:"updated_at"`
	Artifacts   []ArtifactRecord `json:"artifacts"`
}

func (s *Store) SaveChat(c ChatRecord) error {
	if err := validID(c.UUID); err != nil {
		return err
	}
	return writeJSON(s.path("chats", c.UUID+".json"), c)
}

// LoadChat returns the saved chat and whether it exists.
func (s *Store) LoadChat(uuid string) (ChatRecord, bool, error) {
	var c ChatRecord
	if err := validID(uuid); err != nil {
		return c, false, err
	}
	err := readJSON(s.path("chats", uuid+".json"), &c)
	if errors.Is(err, fs.ErrNotExist) {
		return c, false, nil
	}
	return c, err == nil, err
}

func (s *Store) ListChats() ([]ChatRecord, error) {
	var out []ChatRecord
	err := s.eachJSON(s.path("chats"), func(b []byte) error {
		var c ChatRecord
		if err := jsonUnmarshal(b, &c); err != nil {
			return err
		}
		out = append(out, c)
		return nil
	})
	return out, err
}

// safeName makes a string usable as one path element on macOS and Windows.
func safeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < 32 {
			return '-'
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(s, ". ")
	if len([]rune(s)) > 120 {
		s = string([]rune(s)[:120])
	}
	if s == "" {
		s = "untitled"
	}
	// Windows cannot create files named after devices (CON, NUL, COM1...).
	base := strings.ToUpper(strings.SplitN(s, ".", 2)[0])
	if windowsReserved[base] {
		s = "_" + s
	}
	return s
}

var windowsReserved = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// ExportArtifact writes a readable copy under artifacts-export/<project>/<chat>/.
func (s *Store) ExportArtifact(projectName, chatName string, a ArtifactRecord) error {
	p := s.path("artifacts-export", safeName(projectName), safeName(chatName), safeName(a.FileName))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return WriteFileAtomic(p, []byte(a.Content))
}
