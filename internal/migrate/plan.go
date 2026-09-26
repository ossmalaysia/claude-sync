package migrate

import "github.com/ossmalaysia/claude-sync/internal/store"

type SkippedFile struct {
	Project   string `json:"project"`
	File      string `json:"file"`
	SizeBytes int64  `json:"size_bytes"`
}

// PlanResult is what Push would do, computed without any network calls.
type PlanResult struct {
	TargetOrg        string        `json:"target_org"`
	SelectedProjects int           `json:"selected_projects"`
	NewProjects      int           `json:"new_projects"`
	NewInstructions  int           `json:"new_instructions"`
	NewDocs          int           `json:"new_docs"`
	NewFiles         int           `json:"new_files"`
	NewArtifacts     int           `json:"new_artifacts"`
	NewChats         int           `json:"new_chats"` // chat transcripts, when chosen
	NewSkills        int           `json:"new_skills"`
	NewBytes         int64         `json:"new_bytes"`
	RetryFailed      int           `json:"retry_failed"`
	AlreadyDone      int           `json:"already_done"`
	Skipped          []SkippedFile `json:"skipped"`
	Changed          []string      `json:"changed"` // changed in source after push; not synced in v1
}

func itemOf(m map[string]*store.ItemState, id string) *store.ItemState {
	if m == nil {
		return nil
	}
	return m[id]
}

func Plan(st *store.Store, sel map[string]bool, state *store.State, org string, opts Options) (PlanResult, error) {
	res := PlanResult{TargetOrg: org}
	if state.TargetOrg != "" && state.TargetOrg != org {
		return res, ErrOrgMismatch
	}
	projects, err := st.ListProjects()
	if err != nil {
		return res, err
	}
	arts, err := artifactsToSend(st)
	if err != nil {
		return res, err
	}
	chats, err := transcriptsToSend(st)
	if err != nil {
		return res, err
	}
	for _, p := range projects {
		if !sel[p.UUID] {
			continue
		}
		res.SelectedProjects++
		ps := state.Projects[p.UUID]
		var docsState, filesState, artsState map[string]*store.ItemState
		var instr *store.ItemState
		pending := false
		if ps == nil || ps.Target == "" {
			res.NewProjects++
			pending = true
		}
		if ps != nil {
			docsState, filesState, artsState, instr = ps.Docs, ps.Files, ps.Artifacts, ps.Instructions
		}
		if p.PromptTemplate != "" {
			switch {
			case instr == nil || instr.Status == "":
				res.NewInstructions++
				pending = true
			case instr.Status == store.StatusFailed:
				res.RetryFailed++
				pending = true
			case instr.SHA != "" && instr.SHA != contentSHA(p.PromptTemplate):
				res.Changed = append(res.Changed, p.Name+": instructions")
			}
		}
		docs, err := st.ListDocs(p.UUID)
		if err != nil {
			return res, err
		}
		for _, d := range docs {
			it := itemOf(docsState, d.UUID)
			switch {
			case it == nil || it.Status == "":
				res.NewDocs++
				pending = true
			case it.Status == store.StatusFailed:
				res.RetryFailed++
				pending = true
			case it.SHA != "" && it.SHA != contentSHA(d.Content):
				res.Changed = append(res.Changed, p.Name+": "+d.FileName)
			}
		}
		files, err := st.ListFiles(p.UUID)
		if err != nil {
			return res, err
		}
		for _, f := range files {
			if f.SizeBytes > opts.MaxFileBytes {
				res.Skipped = append(res.Skipped, SkippedFile{Project: p.Name, File: f.FileName, SizeBytes: f.SizeBytes})
				continue
			}
			it := itemOf(filesState, f.UUID)
			switch {
			case it == nil || it.Status == "":
				res.NewFiles++
				res.NewBytes += f.SizeBytes
				pending = true
			case it.Status == store.StatusFailed:
				res.RetryFailed++
				res.NewBytes += f.SizeBytes
				pending = true
			}
		}
		var chatsState map[string]*store.ItemState
		if ps != nil {
			chatsState = ps.Chats
		}
		for _, c := range chats[p.UUID] {
			switch it := itemOf(chatsState, c.UUID); {
			case it == nil || it.Status == "":
				res.NewChats++
				pending = true
			case it.Status == store.StatusFailed:
				res.RetryFailed++
				pending = true
			}
		}
		for _, a := range arts[p.UUID] {
			it := itemOf(artsState, a.key())
			switch {
			case it == nil || it.Status == "":
				res.NewArtifacts++
				pending = true
			case it.Status == store.StatusFailed:
				res.RetryFailed++
				pending = true
			case it.SHA != "" && it.SHA != contentSHA(a.Content):
				res.Changed = append(res.Changed, p.Name+": "+a.FileName)
			}
		}
		if !pending {
			res.AlreadyDone++
		}
	}
	settings, err := st.LoadSettings()
	if err != nil || settings.SkipSkills {
		return res, err
	}
	skills, err := st.ListSkills()
	if err != nil {
		return res, err
	}
	for _, sk := range skills {
		switch it := itemOf(state.Skills, sk.ID); {
		case it == nil || it.Status == "":
			res.NewSkills++
		case it.Status == store.StatusFailed:
			res.RetryFailed++
		}
	}
	return res, nil
}
