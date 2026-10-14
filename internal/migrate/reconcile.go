package migrate

import (
	"context"
	"fmt"
	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
	"github.com/ossmalaysia/claude-sync/internal/store"
	"sort"
)

// Preview inspects the target without writes, persists verified ID mappings,
// and reports exactly which selected items are missing or need review.
func Preview(ctx context.Context, api API, st *store.Store, state *store.State, org string, sel map[string]bool, opts Options) (PlanResult, error) {
	if state.TargetOrg != "" && state.TargetOrg != org {
		return PlanResult{}, ErrOrgMismatch
	}
	settings, err := st.LoadSettings()
	if err != nil {
		return PlanResult{}, err
	}
	sel = migrationSelection(sel, settings.CopyUnassignedChats)
	projects, err := MigrationProjects(st)
	if err != nil {
		return PlanResult{}, err
	}
	res := &PushResult{}
	r := &pushRun{ctx: ctx, api: api, st: st, state: state, org: org, opts: opts, res: res}
	r.arts, err = artifactsToSend(st)
	if err != nil {
		return PlanResult{}, err
	}
	r.chats, err = transcriptsToSend(st)
	if err != nil {
		return PlanResult{}, err
	}
	state.TargetOrg = org
	targetProjects, _, err := withRetry(ctx, opts, func() ([]claudeapi.Project, error) { return api.ListProjects(ctx, org) })
	if err != nil {
		return PlanResult{}, err
	}
	var links []ProjectLink
	for _, p := range projects {
		if !sel[p.UUID] {
			continue
		}
		ps := state.Project(p.UUID)
		r.st.TouchJob()
		if ps.Target == "" && p.UUID != store.UnassignedProjectID {
			id, err := r.existingProject(p.Name)
			if err != nil {
				if _, ok := err.(*ambiguousProject); !ok {
					return PlanResult{}, err
				}
				ps.Status, ps.Error = store.StatusConflict, err.Error()
				links = append(links, ProjectLink{Source: p.UUID, Name: p.Name})
				continue
			}
			ps.Status, ps.Error = "", ""
			ps.Target = id
		}
		if ps.Target != "" {
			if err := r.reconcileItems(p, ps); err != nil {
				return PlanResult{}, err
			}
		}
		links = append(links, ProjectLink{Source: p.UUID, Name: p.Name, Target: ps.Target})
	}
	if err := r.reconcileSkills(); err != nil {
		return PlanResult{}, err
	}
	if err := r.save(); err != nil {
		return PlanResult{}, err
	}
	plan, err := Plan(st, sel, state, org, opts)
	plan.ProjectLinks = links
	for _, p := range targetProjects {
		plan.TargetProjects = append(plan.TargetProjects, TargetProject{UUID: p.UUID, Name: p.Name})
	}
	return plan, err
}

type ambiguousProject struct{ name string }

func (e *ambiguousProject) Error() string {
	return fmt.Sprintf("multiple target projects named %q; an explicit project mapping is required", e.name)
}

func conflict(it *store.ItemState, message string) {
	it.Status, it.Error = store.StatusConflict, message
}

// reconcileItems uses remote IDs first, then exact content, then a unique name.
// A name match with different contents is a conflict, never proof of a copy.
func (r *pushRun) reconcileItems(p store.ProjectMeta, ps *store.ProjectState) error {
	r.st.TouchJob()
	target, _, err := withRetry(r.ctx, r.opts, func() (claudeapi.Project, error) { return r.api.GetProject(r.ctx, r.org, ps.Target) })
	if err != nil {
		return err
	}
	docs, _, err := withRetry(r.ctx, r.opts, func() ([]claudeapi.Doc, error) { return r.api.ListDocs(r.ctx, r.org, ps.Target) })
	if err != nil {
		return err
	}
	files, _, err := withRetry(r.ctx, r.opts, func() ([]claudeapi.File, error) { return r.api.ListFiles(r.ctx, r.org, ps.Target) })
	if err != nil {
		return err
	}
	if p.PromptTemplate != "" {
		if ps.Instructions == nil {
			ps.Instructions = &store.ItemState{}
		}
		it := ps.Instructions
		sha := contentSHA(p.PromptTemplate)
		switch {
		case target.PromptTemplate == p.PromptTemplate:
			it.Status, it.SHA, it.Error = store.StatusDone, sha, ""
		case target.PromptTemplate != "" || it.Status == store.StatusDone || it.Status == store.StatusConflict:
			conflict(it, "instructions differ; review both versions")
		}
	}
	docItem := func(items map[string]*store.ItemState, key, name, content string) {
		it := items[key]
		if it == nil {
			it = &store.ItemState{}
			items[key] = it
		}
		it.Name = name
		sha := contentSHA(content)
		if it.OriginExport {
			if it.SHA == sha {
				it.Status, it.Error = store.StatusDone, ""
			} else {
				conflict(it, "exported chat or artifact changed; review the original conversation")
			}
			return
		}
		var match *claudeapi.Doc
		if it.Target != "" {
			for i := range docs {
				if docs[i].UUID == it.Target {
					match = &docs[i]
					break
				}
			}
			if match == nil {
				conflict(it, "previously copied document is missing in target")
				return
			}
		} else {
			// Reuse equal content even when the file was renamed.
			for i := range docs {
				if contentSHA(docs[i].Content) == sha {
					match = &docs[i]
					break
				}
			}
			if match == nil {
				for i := range docs {
					if docs[i].FileName == name {
						if match != nil {
							conflict(it, "multiple target documents have this filename")
							return
						}
						match = &docs[i]
					}
				}
			}
		}
		if match != nil {
			it.Target = match.UUID
			if contentSHA(match.Content) == sha {
				it.Status, it.SHA, it.Error = store.StatusDone, sha, ""
			} else {
				conflict(it, "document contents differ; review both versions")
			}
		} else if it.Status == store.StatusDone || it.Status == store.StatusConflict {
			conflict(it, "previously copied document cannot be matched in target")
		}
	}
	local, err := r.st.ListDocs(p.UUID)
	if err != nil {
		return err
	}
	for _, d := range local {
		docItem(ps.Docs, d.UUID, d.FileName, d.Content)
	}
	for _, a := range r.arts[p.UUID] {
		docItem(ps.Artifacts, a.key(), a.FileName, a.Content)
	}
	for _, c := range r.chats[p.UUID] {
		docItem(ps.Chats, c.UUID, c.TranscriptAs, c.Transcript)
	}
	localFiles, err := r.st.ListFiles(p.UUID)
	if err != nil {
		return err
	}
	hashes := map[string]string{}
	fileHash := func(f claudeapi.File) (string, error) {
		if sha, ok := hashes[f.FileUUID]; ok {
			return sha, nil
		}
		data, _, err := withRetry(r.ctx, r.opts, func() ([]byte, error) { return r.api.DownloadFile(r.ctx, r.org, f.FileUUID) })
		if err != nil && f.FileKind == "image" {
			data, _, err = withRetry(r.ctx, r.opts, func() ([]byte, error) { return r.api.DownloadPreview(r.ctx, r.org, f.FileUUID) })
		}
		if err != nil {
			return "", err
		}
		sha := contentSHA(string(data))
		hashes[f.FileUUID] = sha
		return sha, nil
	}
	for _, f := range localFiles {
		r.st.TouchJob()
		if f.SizeBytes > r.opts.MaxFileBytes {
			continue
		}
		data, err := r.st.ReadFile(p.UUID, f.UUID)
		if err != nil {
			return err
		}
		sha := contentSHA(string(data))
		it := ps.Files[f.UUID]
		if it == nil {
			it = &store.ItemState{}
			ps.Files[f.UUID] = it
		}
		it.Name = f.FileName
		candidates := []claudeapi.File{}
		for _, remote := range files {
			if it.Target != "" && remote.FileUUID == it.Target {
				candidates = []claudeapi.File{remote}
				break
			}
			if it.Target == "" && (remote.FileName == f.FileName || remote.SizeBytes == int64(len(data))) {
				candidates = append(candidates, remote)
			}
		}
		if it.Target != "" && len(candidates) == 0 {
			conflict(it, "previously copied file is missing in target")
			continue
		}
		matched := false
		sameName := false
		for _, remote := range candidates {
			if remote.SizeBytes > r.opts.MaxFileBytes {
				sameName = sameName || remote.FileName == f.FileName
				continue
			}
			remoteSHA, err := fileHash(remote)
			if err != nil {
				return fmt.Errorf("check target file %q: %w", f.FileName, err)
			}
			if remoteSHA == sha {
				it.Status, it.Target, it.SHA, it.Error = store.StatusDone, remote.FileUUID, sha, ""
				matched = true
				break
			}
			sameName = sameName || remote.FileName == f.FileName
		}
		if !matched && (sameName || it.Target != "" || it.Status == store.StatusDone || it.Status == store.StatusConflict) {
			conflict(it, "file contents differ; review both versions")
		}
	}
	if ps.Status == store.StatusFailed {
		ps.Status, ps.Error = "", ""
	}
	return nil
}

func projectConflicts(p store.ProjectMeta, ps *store.ProjectState) []string {
	if ps == nil {
		return nil
	}
	var out []string
	if ps.Status == store.StatusConflict {
		out = append(out, p.Name+": "+ps.Error)
	}
	if ps.Instructions != nil && ps.Instructions.Status == store.StatusConflict {
		out = append(out, p.Name+": "+ps.Instructions.Error)
	}
	for _, items := range []map[string]*store.ItemState{ps.Docs, ps.Files, ps.Artifacts, ps.Chats} {
		for id, it := range items {
			if it != nil && it.Status == store.StatusConflict {
				name := it.Name
				if name == "" {
					name = id
				}
				out = append(out, p.Name+": "+name+": "+it.Error)
			}
		}
	}
	sort.Strings(out)
	return out
}
