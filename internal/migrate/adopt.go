package migrate

import (
	"errors"

	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
	"github.com/ossmalaysia/claude-sync/internal/store"
)

// errStopProject ends one project's push without stopping the others.
var errStopProject = errors.New("project skipped")

// existingProject returns a target project with this name that no source
// project is mapped to yet, or "". Ambiguous names require an explicit mapping.
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
	if len(ids) > 1 {
		return "", &ambiguousProject{name: name}
	}
	if len(ids) == 0 {
		return "", nil
	}
	r.unclaimed[name] = ids[1:]
	return ids[0], nil
}

// adoptItems verifies content before recording an existing copy.
func (r *pushRun) adoptItems(p store.ProjectMeta, ps *store.ProjectState) error {
	before := 0
	for _, items := range []map[string]*store.ItemState{ps.Docs, ps.Files, ps.Artifacts, ps.Chats} {
		for _, it := range items {
			if it != nil && it.Status == store.StatusDone {
				before++
			}
		}
	}
	if err := r.reconcileItems(p, ps); err != nil {
		return r.adoptFailed(ps, err)
	}
	after := 0
	for _, items := range []map[string]*store.ItemState{ps.Docs, ps.Files, ps.Artifacts, ps.Chats} {
		for _, it := range items {
			if it != nil && it.Status == store.StatusDone {
				after++
			}
		}
	}
	r.res.AlreadyThere += after - before
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
