// Package migrate implements pull, plan, push and verify on top of an API
// (the claudeapi client in production, a fake in tests) and a local store.
package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
)

// API is the subset of the claude.ai client that migration needs.
type API interface {
	ListProjects(ctx context.Context, org string) ([]claudeapi.Project, error)
	GetProject(ctx context.Context, org, project string) (claudeapi.Project, error)
	CreateProject(ctx context.Context, org string, p claudeapi.NewProject) (claudeapi.Project, error)
	SetInstructions(ctx context.Context, org, project, text string) error
	ListDocs(ctx context.Context, org, project string) ([]claudeapi.Doc, error)
	CreateDoc(ctx context.Context, org, project, fileName, content string) (claudeapi.Doc, error)
	ListFiles(ctx context.Context, org, project string) ([]claudeapi.File, error)
	DownloadFile(ctx context.Context, org, fileUUID string) ([]byte, error)
	DownloadPreview(ctx context.Context, org, fileUUID string) ([]byte, error)
	UploadFile(ctx context.Context, org, project, fileName, mime string, data []byte) (claudeapi.File, error)
	GetMemory(ctx context.Context, org string) (string, error)
}

var (
	ErrOrgMismatch = errors.New("state.json belongs to a different target org; move it aside to start a new migration")
	ErrAuthPaused  = errors.New("session expired: log in again in the target browser, then resume")
)

// Event is a progress/log message for the UI or CLI.
type Event struct {
	Stage   string `json:"stage"` // "pull" | "push" | "verify"
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Level   string `json:"level"` // "info" | "warn" | "error"
	Message string `json:"message"`
	// Running totals, set on pull progress events.
	Docs  int   `json:"docs,omitempty"`
	Files int   `json:"files,omitempty"`
	Bytes int64 `json:"bytes,omitempty"`
}

type Reporter func(Event)

func (r Reporter) emit(e Event) {
	if r != nil {
		r(e)
	}
}

// Failure is one item that could not be pulled or pushed.
type Failure struct {
	Project string `json:"project"`
	Item    string `json:"item"`
	Error   string `json:"error"`
}

type Options struct {
	WritePause   time.Duration
	Backoff      []time.Duration // waits between attempts; attempts = len(Backoff)+1
	MaxFileBytes int64
	Sleep        func(ctx context.Context, d time.Duration) error
}

func DefaultOptions() Options {
	return Options{
		WritePause:   400 * time.Millisecond,
		Backoff:      []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second},
		MaxFileBytes: 30 << 20,
		Sleep:        sleepCtx,
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func contentSHA(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
