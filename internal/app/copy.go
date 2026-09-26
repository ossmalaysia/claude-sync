package app

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/ossmalaysia/claude-sync/internal/migrate"
	"github.com/ossmalaysia/claude-sync/internal/store"
)

// ErrReviewCopy stops sending until the user has saved "What to copy" once.
var ErrReviewCopy = errors.New("review What to copy before sending")

// CopySettings are the switches on the "What to copy" page. Projects are
// always copied; which ones is the selection.
type CopySettings struct {
	Artifacts bool `json:"artifacts"` // artifacts from chats, as docs in their project
	Chats     bool `json:"chats"`     // chats as transcript docs in their project
	Skills    bool `json:"skills"`    // the user's own skills
	Memory    bool `json:"memory"`
	Personal  bool `json:"personal"` // projects that look personal (Personal:, Family:, Travel)
}

// CopyCounts is what the local copy holds, so each switch shows real numbers.
type CopyCounts struct {
	Projects         int      `json:"projects"`          // pulled projects
	Selected         int      `json:"selected"`          // currently selected
	Artifacts        int      `json:"artifacts"`         // artifacts in chats inside projects
	ArtifactsLocal   int      `json:"artifacts_local"`   // artifacts in chats outside any project (stay local)
	Chats            int      `json:"chats"`             // chats inside projects (selected or not)
	ChatsLocal       int      `json:"chats_local"`       // chats outside any project
	Skills           int      `json:"skills"`            // personal skills pulled
	Memory           bool     `json:"memory"`            // memory text exists
	PersonalProjects []string `json:"personal_projects"` // names that look personal, sorted
}

type CopyView struct {
	Settings CopySettings `json:"settings"`
	Counts   CopyCounts   `json:"counts"`
	Reviewed bool         `json:"reviewed"`
}

// copySettingsOf reads the switches from the saved settings. Before the page
// is first saved this gives its defaults: artifacts, skills and memory on,
// chats and personal projects off (or what the user answered earlier).
func copySettingsOf(s store.Settings) CopySettings {
	return CopySettings{
		Artifacts: !s.SkipArtifacts,
		Chats:     s.ChatChoice == store.ChatsInclude,
		Skills:    !s.SkipSkills,
		Memory:    !s.SkipMemory,
		Personal:  s.PersonalChoice == store.PersonalInclude,
	}
}

// GetCopySettings returns the "What to copy" page: the current switches and
// what the local copy holds. It changes nothing on disk.
func (a *App) GetCopySettings() (CopyView, error) {
	var v CopyView
	settings, err := a.st.LoadSettings()
	if err != nil {
		return v, err
	}
	v.Settings, v.Reviewed = copySettingsOf(settings), !settings.CopyReviewedAt.IsZero()
	projects, err := a.st.ListProjects()
	if err != nil {
		return v, err
	}
	saved, err := a.st.LoadSelection()
	if err != nil {
		return v, err
	}
	sel := migrate.MergeSelection(projects, saved, settings.PersonalChoice == store.PersonalInclude)
	v.Counts.Projects = len(projects)
	v.Counts.PersonalProjects = []string{}
	for _, p := range projects {
		if sel[p.UUID] {
			v.Counts.Selected++
		}
		if migrate.LooksPersonal(p.Name) {
			v.Counts.PersonalProjects = append(v.Counts.PersonalProjects, p.Name)
		}
	}
	sort.Strings(v.Counts.PersonalProjects)
	chats, err := a.st.ListChats()
	if err != nil {
		return v, err
	}
	for _, c := range chats {
		if c.ProjectUUID != "" {
			v.Counts.Chats++
			v.Counts.Artifacts += len(c.Artifacts)
		} else {
			v.Counts.ChatsLocal++
			v.Counts.ArtifactsLocal += len(c.Artifacts)
		}
	}
	skills, err := a.st.ListSkills()
	if err != nil {
		return v, err
	}
	v.Counts.Skills = len(skills)
	mem, err := a.st.LoadMemory()
	if err != nil {
		return v, err
	}
	v.Counts.Memory = strings.TrimSpace(mem) != ""
	return v, nil
}

// SaveCopySettings records the switches and that the page was reviewed.
// Switching something off only stops it being sent from now on; nothing
// already in the target is removed.
func (a *App) SaveCopySettings(c CopySettings) error {
	settings, err := a.st.LoadSettings()
	if err != nil {
		return err
	}
	// Tick or untick personal projects only when that switch changes, so
	// projects picked by hand (also during the first review) survive a save.
	// Unchanged, the choice is only recorded: the selection already follows it.
	if copySettingsOf(settings).Personal != c.Personal {
		if err := migrate.SetPersonalChoice(a.st, c.Personal); err != nil {
			return err
		}
		if settings, err = a.st.LoadSettings(); err != nil {
			return err
		}
	}
	settings.PersonalChoice = store.PersonalSkip
	if c.Personal {
		settings.PersonalChoice = store.PersonalInclude
	}
	settings.ChatChoice = store.ChatsSkip
	if c.Chats {
		settings.ChatChoice = store.ChatsInclude
	}
	settings.SkipArtifacts, settings.SkipSkills, settings.SkipMemory = !c.Artifacts, !c.Skills, !c.Memory
	settings.CopyReviewedAt = time.Now().UTC()
	return a.st.SaveSettings(settings)
}

// copyReviewPending is true while there are pulled projects and the user has
// not saved "What to copy" yet: nothing is sent until they do.
func (a *App) copyReviewPending() (bool, error) {
	settings, err := a.st.LoadSettings()
	if err != nil || !settings.CopyReviewedAt.IsZero() {
		return false, err
	}
	projects, err := a.st.ListProjects()
	return len(projects) > 0, err
}

func (a *App) chatsInSelectedProjects(sel map[string]bool) (int, error) {
	chats, err := a.st.ListChats()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, c := range chats {
		if c.ProjectUUID != "" && sel[c.ProjectUUID] {
			n++
		}
	}
	return n, nil
}
