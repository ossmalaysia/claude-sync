package migrate

import (
	"strings"

	"github.com/ossmalaysia/claude-sync/internal/store"
)

// Projects whose lower-cased, trimmed name starts with one of these look
// personal. Whether they are sent is the user's choice (Settings.PersonalChoice);
// until they choose, they are left out, since the target is often a company.
var personalPrefixes = []string{"personal:", "family:", "travel"}

// LooksPersonal reports whether a project name looks personal.
func LooksPersonal(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, p := range personalPrefixes {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

// DefaultSelected is the default for a project the user has not ticked or
// unticked yet.
func DefaultSelected(name string, includePersonal bool) bool {
	return includePersonal || !LooksPersonal(name)
}

// MergeSelection keeps earlier choices and applies defaults to new projects.
func MergeSelection(projects []store.ProjectMeta, existing map[string]bool, includePersonal bool) map[string]bool {
	out := make(map[string]bool, len(projects))
	for k, v := range existing {
		out[k] = v
	}
	for _, p := range projects {
		if _, ok := out[p.UUID]; !ok {
			out[p.UUID] = DefaultSelected(p.Name, includePersonal)
		}
	}
	return out
}

// EffectiveSelection returns the saved selection with defaults applied to
// projects that are new since it was saved, and saves the result.
func EffectiveSelection(st *store.Store) (map[string]bool, error) {
	projects, err := st.ListProjects()
	if err != nil {
		return nil, err
	}
	existing, err := st.LoadSelection()
	if err != nil {
		return nil, err
	}
	settings, err := st.LoadSettings()
	if err != nil {
		return nil, err
	}
	sel := MergeSelection(projects, existing, settings.PersonalChoice == store.PersonalInclude)
	return sel, st.SaveSelection(sel)
}

// SetPersonalChoice records whether personal-looking projects are sent, and
// ticks or unticks all of them to match.
func SetPersonalChoice(st *store.Store, include bool) error {
	settings, err := st.LoadSettings()
	if err != nil {
		return err
	}
	settings.PersonalChoice = store.PersonalSkip
	if include {
		settings.PersonalChoice = store.PersonalInclude
	}
	if err := st.SaveSettings(settings); err != nil {
		return err
	}
	sel, err := EffectiveSelection(st)
	if err != nil {
		return err
	}
	projects, err := st.ListProjects()
	if err != nil {
		return err
	}
	for _, p := range projects {
		if LooksPersonal(p.Name) {
			sel[p.UUID] = include
		}
	}
	return st.SaveSelection(sel)
}
