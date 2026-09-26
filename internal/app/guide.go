package app

import (
	"fmt"
	"regexp"

	"github.com/ossmalaysia/claude-sync/internal/store"
)

// Guide says where migrated content is in the target account, with real
// names from this migration as examples.
type Guide struct {
	TargetName string        `json:"target_name"`
	Projects   int           `json:"projects"` // projects in the target
	Example    *GuideProject `json:"example"`  // nil until something is sent
	Skills     int           `json:"skills"`
	MemorySent bool          `json:"memory_sent"`
	LocalOnly  int           `json:"local_only"` // artifacts from chats outside projects
}

// GuideProject is the sent project with the most artifacts and chats.
type GuideProject struct {
	SourceUUID string `json:"source_uuid"`
	Name       string `json:"name"`
	Artifact   string `json:"artifact"` // an artifact document name, if any
	Chat       string `json:"chat"`     // a chat document name, if any
}

func (a *App) Guide() (Guide, error) {
	var g Guide
	_, g.TargetName = a.orgOf("target")
	state, err := a.st.LoadState()
	if err != nil {
		return g, err
	}
	chats, err := a.st.ListChats()
	if err != nil {
		return g, err
	}
	artifactName, chatName := map[string]string{}, map[string]string{}
	for _, c := range chats {
		if c.ProjectUUID == "" {
			g.LocalOnly += len(c.Artifacts)
			continue
		}
		ps := state.Projects[c.ProjectUUID]
		if ps == nil {
			continue
		}
		for _, art := range c.Artifacts {
			if it := ps.Artifacts[c.UUID+"/"+art.ID]; it != nil && it.Status == store.StatusDone && artifactName[c.ProjectUUID] == "" {
				artifactName[c.ProjectUUID] = art.FileName
			}
		}
		if it := ps.Chats[c.UUID]; it != nil && it.Status == store.StatusDone && chatName[c.ProjectUUID] == "" {
			chatName[c.ProjectUUID] = c.TranscriptAs
		}
	}
	best := -1
	for src, ps := range state.Projects {
		if ps.Target == "" {
			continue
		}
		g.Projects++
		score := countDone(ps.Artifacts) + countDone(ps.Chats)
		if score > best || (score == best && g.Example != nil && src < g.Example.SourceUUID) {
			p, ok, err := a.st.LoadProject(src)
			if err != nil || !ok {
				continue
			}
			best = score
			g.Example = &GuideProject{SourceUUID: src, Name: p.Name, Artifact: artifactName[src], Chat: chatName[src]}
		}
	}
	for _, it := range state.Skills {
		if it.Status == store.StatusDone {
			g.Skills++
		}
	}
	settings, err := a.st.LoadSettings()
	g.MemorySent = err == nil && !settings.MemorySentAt.IsZero()
	return g, err
}

var targetUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// OpenTargetProject opens the target copy of a source project on claude.ai.
func (a *App) OpenTargetProject(sourceUUID string) error {
	state, err := a.st.LoadState()
	if err != nil {
		return err
	}
	ps := state.Projects[sourceUUID]
	if ps == nil || !targetUUID.MatchString(ps.Target) {
		return fmt.Errorf("this project is not in the target account yet")
	}
	return a.openURL("https://claude.ai/project/" + ps.Target)
}

func countDone(items map[string]*store.ItemState) int {
	n := 0
	for _, it := range items {
		if it.Status == store.StatusDone {
			n++
		}
	}
	return n
}
