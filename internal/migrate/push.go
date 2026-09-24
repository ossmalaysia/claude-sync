package migrate

import (
	"context"
	"errors"
	"fmt"

	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
	"github.com/ossmalaysia/claude-sync/internal/store"
)

type PushResult struct {
	CreatedProjects int       `json:"created_projects"`
	Instructions    int       `json:"instructions"`
	Docs            int       `json:"docs"`
	Files           int       `json:"files"`
	Failed          []Failure `json:"failed"`
}

type pushRun struct {
	ctx     context.Context
	api     API
	st      *store.Store
	state   *store.State
	org     string
	opts    Options
	report  Reporter
	res     *PushResult
	project string // name of the project being pushed, for messages
}

// Push creates the selected projects in the target org. It only ever
// creates; state is saved after every successful write so a re-run resumes
// exactly where the previous one stopped.
func Push(ctx context.Context, api API, st *store.Store, state *store.State, org string, sel map[string]bool, opts Options, report Reporter) (PushResult, error) {
	var res PushResult
	if state.TargetOrg != "" && state.TargetOrg != org {
		return res, ErrOrgMismatch
	}
	all, err := st.ListProjects()
	if err != nil {
		return res, err
	}
	var projects []store.ProjectMeta
	for _, p := range all {
		if sel[p.UUID] {
			projects = append(projects, p)
		}
	}
	state.TargetOrg = org
	r := &pushRun{ctx: ctx, api: api, st: st, state: state, org: org, opts: opts, report: report, res: &res}
	if err := r.save(); err != nil {
		return res, err
	}
	for i, p := range projects {
		report.emit(Event{Stage: "push", Done: i, Total: len(projects), Level: "info", Message: p.Name})
		r.project = p.Name
		if err := r.pushProject(p); err != nil {
			return res, err
		}
	}
	report.emit(Event{Stage: "push", Done: len(projects), Total: len(projects), Level: "info", Message: "push complete"})
	return res, nil
}

func (r *pushRun) save() error { return r.st.SaveState(r.state) }

// stopErr returns non-nil when err must stop the whole push.
func stopErr(err error) error {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), errors.Is(err, claudeapi.ErrSessionGone):
		return err
	case errors.Is(err, claudeapi.ErrAuth):
		return fmt.Errorf("%w: %w", ErrAuthPaused, err)
	}
	return nil
}

func (r *pushRun) fail(item, msg string) {
	r.res.Failed = append(r.res.Failed, Failure{Project: r.project, Item: item, Error: msg})
	r.report.emit(Event{Stage: "push", Level: "error", Message: r.project + ": " + item + ": " + msg})
}

func (r *pushRun) pushProject(p store.ProjectMeta) error {
	ps := r.state.Project(p.UUID)
	if ps.Target == "" {
		created, attempts, err := withRetry(r.ctx, r.opts, func() (claudeapi.Project, error) {
			return r.api.CreateProject(r.ctx, r.org, claudeapi.NewProject{Name: p.Name, Description: p.Description, IsPrivate: p.IsPrivate})
		})
		if err != nil {
			if stop := stopErr(err); stop != nil {
				return errors.Join(stop, r.save())
			}
			ps.Status, ps.Error = store.StatusFailed, fmt.Sprintf("%v (after %d attempts)", err, attempts)
			r.fail("project", ps.Error)
			return r.save()
		}
		ps.Target, ps.Status, ps.Error = created.UUID, "", ""
		r.res.CreatedProjects++
		if err := r.save(); err != nil {
			return err
		}
		if err := r.opts.Sleep(r.ctx, r.opts.WritePause); err != nil {
			return err
		}
	}

	if p.PromptTemplate != "" && (ps.Instructions == nil || ps.Instructions.Status != store.StatusDone) {
		if ps.Instructions == nil {
			ps.Instructions = &store.ItemState{}
		}
		err := r.item("instructions", ps.Instructions, contentSHA(p.PromptTemplate), func() (string, error) {
			return "", r.api.SetInstructions(r.ctx, r.org, ps.Target, p.PromptTemplate)
		})
		if err != nil {
			return err
		}
		if ps.Instructions.Status == store.StatusDone {
			r.res.Instructions++
		}
	}

	docs, err := r.st.ListDocs(p.UUID)
	if err != nil {
		return err
	}
	for _, d := range docs {
		it := ps.Docs[d.UUID]
		if it != nil && it.Status == store.StatusDone {
			continue
		}
		if it == nil {
			it = &store.ItemState{}
			ps.Docs[d.UUID] = it
		}
		err := r.item("doc "+d.FileName, it, contentSHA(d.Content), func() (string, error) {
			doc, err := r.api.CreateDoc(r.ctx, r.org, ps.Target, d.FileName, d.Content)
			return doc.UUID, err
		})
		if err != nil {
			return err
		}
		if it.Status == store.StatusDone {
			r.res.Docs++
		}
	}

	files, err := r.st.ListFiles(p.UUID)
	if err != nil {
		return err
	}
	for _, f := range files {
		it := ps.Files[f.UUID]
		if it != nil && (it.Status == store.StatusDone || it.Status == store.StatusSkipped) {
			continue
		}
		if it == nil {
			it = &store.ItemState{}
			ps.Files[f.UUID] = it
		}
		label := "file " + f.FileName
		if f.SizeBytes > r.opts.MaxFileBytes {
			it.Status, it.Error = store.StatusSkipped, fmt.Sprintf("larger than %d MB", r.opts.MaxFileBytes>>20)
			r.report.emit(Event{Stage: "push", Level: "warn", Message: r.project + ": " + label + " skipped: " + it.Error})
			if err := r.save(); err != nil {
				return err
			}
			continue
		}
		data, err := r.st.ReadFile(p.UUID, f.UUID)
		if err != nil {
			it.Status, it.Error = store.StatusFailed, "local copy missing: "+err.Error()
			r.fail(label, it.Error)
			if err := r.save(); err != nil {
				return err
			}
			continue
		}
		err = r.item(label, it, "", func() (string, error) {
			up, err := r.api.UploadFile(r.ctx, r.org, ps.Target, f.FileName, f.Mime, data)
			return up.FileUUID, err
		})
		if err != nil {
			return err
		}
		if it.Status == store.StatusDone {
			r.res.Files++
		}
	}
	return nil
}

// item performs one write with retry and records the outcome in it. It
// returns non-nil only when the whole push must stop.
func (r *pushRun) item(label string, it *store.ItemState, sha string, write func() (string, error)) error {
	target, attempts, err := withRetry(r.ctx, r.opts, write)
	it.Attempts += attempts
	if err != nil {
		if stop := stopErr(err); stop != nil {
			return errors.Join(stop, r.save())
		}
		it.Status, it.Error = store.StatusFailed, err.Error()
		r.fail(label, it.Error)
		return r.save()
	}
	it.Status, it.Target, it.SHA, it.Error = store.StatusDone, target, sha, ""
	if err := r.save(); err != nil {
		return err
	}
	return r.opts.Sleep(r.ctx, r.opts.WritePause)
}
