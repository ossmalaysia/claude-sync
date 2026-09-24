package migrate

import (
	"strings"

	"github.com/ossmalaysia/claude-sync/internal/store"
)

// Projects whose lower-cased, trimmed name starts with one of these are
// unticked by default: they look personal and the target is a company org.
var personalPrefixes = []string{"personal:", "family:", "travel"}

func DefaultSelected(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, p := range personalPrefixes {
		if strings.HasPrefix(n, p) {
			return false
		}
	}
	return true
}

// MergeSelection keeps earlier choices and applies defaults to new projects.
func MergeSelection(projects []store.ProjectMeta, existing map[string]bool) map[string]bool {
	out := make(map[string]bool, len(projects))
	for k, v := range existing {
		out[k] = v
	}
	for _, p := range projects {
		if _, ok := out[p.UUID]; !ok {
			out[p.UUID] = DefaultSelected(p.Name)
		}
	}
	return out
}
