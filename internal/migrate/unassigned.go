package migrate

import "github.com/ossmalaysia/claude-sync/internal/store"

// MigrationProjects adds a virtual destination without changing source data.
// Retain created destinations for verification after the option is disabled.
func MigrationProjects(st *store.Store) ([]store.ProjectMeta, error) {
	projects, err := st.ListProjects()
	if err != nil {
		return nil, err
	}
	settings, err := st.LoadSettings()
	if err != nil {
		return nil, err
	}
	state, err := st.LoadState()
	if err != nil {
		return nil, err
	}
	ps := state.Projects[store.UnassignedProjectID]
	created := ps != nil && ps.Target != ""
	if !settings.CopyUnassignedChats && !created {
		return projects, nil
	}
	chats, err := st.ListChats()
	if err != nil {
		return nil, err
	}
	eligible := false
	for _, c := range chats {
		if c.ProjectUUID == "" && (c.Transcript != "" || (!settings.SkipArtifacts && len(c.Artifacts) > 0)) {
			eligible = true
			break
		}
	}
	if !eligible && !created {
		return projects, nil
	}
	name := settings.UnassignedName()
	if created && ps.Name != "" {
		name = ps.Name
	}
	return append(projects, store.ProjectMeta{UUID: store.UnassignedProjectID, Name: name, IsPrivate: true}), nil
}

func migrationSelection(sel map[string]bool, enabled bool) map[string]bool {
	out := make(map[string]bool, len(sel)+1)
	for k, v := range sel {
		out[k] = v
	}
	out[store.UnassignedProjectID] = enabled
	return out
}
