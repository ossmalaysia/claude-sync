package migrate

import (
	"errors"

	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
	"github.com/ossmalaysia/claude-sync/internal/store"
)

// errStopProject ends one project's push without stopping the others.
var errStopProject = errors.New("project skipped")

// existingProject returns a target project with this name that no source
// project is mapped to yet, or "". Same-named source projects each claim a
// different target project, in listing order.
func (r *pushRun) existingProject(name string) (string, error) {
	if r.unclaimed == nil {
		projects, _, err := withRetry(r.ctx, r.opts, func() ([]claudeapi.Project, error) { return r.api.ListProjects(r.ctx, r.org) })
		if err != nil {
			return "", err
		}
		claimed := map[string]bool{}
		for _, ps := range r.state.Projects {
			if ps.Target != "" {
				claimed[ps.Target] = true
			}
		}
		r.unclaimed = map[string][]string{}
		for _, p := range projects {
			if !claimed[p.UUID] {
				r.unclaimed[p.Name] = append(r.unclaimed[p.Name], p.UUID)
			}
		}
	}
	ids := r.unclaimed[name]
	if len(ids) == 0 {
		return "", nil
	}
	r.unclaimed[name] = ids[1:]
	return ids[0], nil
}

// adoptItems marks the local docs, files, artifacts and instructions that an
// adopted target project already has as sent, matching by file name, so only
// what is missing gets sent. Existing instructions are never overwritten.
func (r *pushRun) adoptItems(p store.ProjectMeta, ps *store.ProjectState) error {
	target, _, err := withRetry(r.ctx, r.opts, func() (claudeapi.Project, error) { return r.api.GetProject(r.ctx, r.org, ps.Target) })
	if err != nil {
		return r.adoptFailed(ps, err)
	}
	docs, _, err := withRetry(r.ctx, r.opts, func() ([]claudeapi.Doc, error) { return r.api.ListDocs(r.ctx, r.org, ps.Target) })
	if err != nil {
		return r.adoptFailed(ps, err)
	}
	files, _, err := withRetry(r.ctx, r.opts, func() ([]claudeapi.File, error) { return r.api.ListFiles(r.ctx, r.org, ps.Target) })
	if err != nil {
		return r.adoptFailed(ps, err)
	}
	docNames, fileNames := map[string]int{}, map[string]int{}
	for _, d := range docs {
		docNames[d.FileName]++
	}
	for _, f := range files {
		fileNames[f.FileName]++
	}
	have := func(names map[string]int, name string, items map[string]*store.ItemState, key string) {
		if it := items[key]; (it != nil && it.Status == store.StatusDone) || names[name] == 0 {
			return
		}
		names[name]--
		items[key] = &store.ItemState{Status: store.StatusDone}
		r.res.AlreadyThere++
	}

	if p.PromptTemplate != "" && target.PromptTemplate != "" {
		ps.Instructions = &store.ItemState{Status: store.StatusDone}
	}
	local, err := r.st.ListDocs(p.UUID)
	if err != nil {
		return err
	}
	for _, d := range local {
		have(docNames, d.FileName, ps.Docs, d.UUID)
	}
	for _, a := range r.arts[p.UUID] {
		have(docNames, a.FileName, ps.Artifacts, a.key())
	}
	localFiles, err := r.st.ListFiles(p.UUID)
	if err != nil {
		return err
	}
	for _, f := range localFiles {
		have(fileNames, f.FileName, ps.Files, f.UUID)
	}
	return r.save()
}

// adoptFailed stops the project rather than risk sending duplicates when the
// target's contents cannot be read.
func (r *pushRun) adoptFailed(ps *store.ProjectState, err error) error {
	if stop := stopErr(err); stop != nil {
		return stop
	}
	ps.Target = ""
	ps.Status, ps.Error = store.StatusFailed, "could not read the existing project in the target: "+err.Error()
	r.fail("project", ps.Error)
	return errStopProject
}
