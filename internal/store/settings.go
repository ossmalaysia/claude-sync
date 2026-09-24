package store

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"time"
)

// Settings are the user's choices, remembered across app restarts.
type Settings struct {
	SourceOrg     string    `json:"source_org"`
	SourceOrgName string    `json:"source_org_name"`
	TargetOrg     string    `json:"target_org"`
	TargetOrgName string    `json:"target_org_name"`
	VerifiedAt    time.Time `json:"verified_at"`
	VerifyOK      int       `json:"verify_ok"`
	VerifyTotal   int       `json:"verify_total"`
	VerifyWaiting int       `json:"verify_waiting"` // mismatches only because artifacts are not sent yet
	// Memory last sent to the target, and a hash of the text sent, so the
	// same memory is not imported twice.
	MemorySentAt time.Time `json:"memory_sent_at"`
	MemorySHA    string    `json:"memory_sha"`
	// Last "Scan & sync": when, and what it found and sent.
	LastSyncAt              time.Time `json:"last_sync_at"`
	LastSyncNewProjects     int       `json:"last_sync_new_projects"`
	LastSyncChangedProjects int       `json:"last_sync_changed_projects"`
	LastSyncChats           int       `json:"last_sync_chats"`
	LastSyncArtifacts       int       `json:"last_sync_artifacts"`
	LastSyncSent            int       `json:"last_sync_sent"`
}

func (s *Store) LoadSettings() (Settings, error) {
	var st Settings
	err := readJSON(s.path("settings.json"), &st)
	if errors.Is(err, fs.ErrNotExist) {
		return Settings{}, nil
	}
	return st, err
}

func (s *Store) SaveSettings(st Settings) error { return writeJSON(s.path("settings.json"), st) }

// HasProfile reports whether the account's browser profile has been used,
// i.e. a saved login probably exists.
func (s *Store) HasProfile(account string) bool {
	entries, err := os.ReadDir(s.ProfileDir(account))
	return err == nil && len(entries) > 0
}

var ErrJobRunning = errors.New("another pull or push is already running (in this app or the CLI)")

// A job lock older than this without a touch is treated as left over from a crash.
const jobStaleAfter = 5 * time.Minute

func (s *Store) jobPath() string { return s.path("job.lock") }

// AcquireJob takes the single job lock for kind ("pull" or "push"). The lock
// is a file so the app and the CLI exclude each other. Callers must call
// TouchJob regularly while working and release when done.
func (s *Store) AcquireJob(kind string) (release func(), err error) {
	if running := s.RunningJob(); running != "" {
		return nil, ErrJobRunning
	}
	if err := WriteFileAtomic(s.jobPath(), []byte(kind)); err != nil {
		return nil, err
	}
	return func() { _ = os.Remove(s.jobPath()) }, nil // best effort: a stale lock expires anyway
}

// RunningJob returns "pull", "push" or "" when no live job holds the lock.
func (s *Store) RunningJob() string {
	fi, err := os.Stat(s.jobPath())
	if err != nil || time.Since(fi.ModTime()) > jobStaleAfter {
		return ""
	}
	b, err := os.ReadFile(s.jobPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// TouchJob marks the running job as alive.
func (s *Store) TouchJob() {
	now := time.Now()
	_ = os.Chtimes(s.jobPath(), now, now) // best effort heartbeat
}
