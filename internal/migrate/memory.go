package migrate

import (
	"context"
	"strings"
	"time"

	"github.com/ossmalaysia/claude-sync/internal/store"
)

// MemoryAPI is the call used to import memory into the target account.
type MemoryAPI interface {
	ImportMemory(ctx context.Context, org, text string) error
}

// MemoryPending reports whether memory.md has text that was not sent yet.
func MemoryPending(st *store.Store) (bool, error) {
	mem, err := st.LoadMemory()
	if err != nil || strings.TrimSpace(mem) == "" {
		return false, err
	}
	settings, err := st.LoadSettings()
	if err != nil {
		return false, err
	}
	return contentSHA(mem) != settings.MemorySHA, nil
}

// SyncMemory sends memory.md to the target's memory import (the same call as
// Settings > Memory > Start import). It returns "sent", "unchanged" (this
// exact text was already sent; claude.ai merges imports, so a repeat could
// duplicate entries) or "empty".
func SyncMemory(ctx context.Context, api MemoryAPI, st *store.Store, org string) (string, error) {
	mem, err := st.LoadMemory()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(mem) == "" {
		return "empty", nil
	}
	settings, err := st.LoadSettings()
	if err != nil {
		return "", err
	}
	sha := contentSHA(mem)
	if sha == settings.MemorySHA {
		return "unchanged", nil
	}
	if err := api.ImportMemory(ctx, org, mem); err != nil {
		return "", err
	}
	settings.MemorySentAt, settings.MemorySHA = time.Now().UTC(), sha
	return "sent", st.SaveSettings(settings)
}
