package migrate

import (
	"context"
	"errors"
	"fmt"

	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
	"github.com/ossmalaysia/claude-sync/internal/store"
)

// pullSkills saves every personal skill (never Anthropic's built-in ones) as a
// .skill package holding all of its files. Skills whose updated_at is
// unchanged since the last pull are not downloaded again.
func pullSkills(ctx context.Context, api API, st *store.Store, org string, opts Options, report Reporter, res *PullResult) error {
	skills, _, err := withRetry(ctx, opts, func() ([]claudeapi.Skill, error) { return api.ListSkills(ctx, org) })
	if errors.Is(err, claudeapi.ErrNotFound) {
		return nil // the account has no skills feature
	}
	if err != nil {
		return err
	}
	for _, sk := range skills {
		if !sk.Personal() {
			continue
		}
		res.Skills++
		if saved, ok, err := st.LoadSkill(sk.ID); err != nil {
			return err
		} else if ok && saved.UpdatedAt == sk.UpdatedAt {
			continue
		}
		pkg, _, err := withRetry(ctx, opts, func() ([]byte, error) { return api.DownloadSkill(ctx, org, sk.ID) })
		if err != nil {
			if stopErr(err) != nil || errors.Is(err, claudeapi.ErrSessionGone) {
				return err
			}
			fail := Failure{Item: "skill " + sk.Name, Error: err.Error()}
			res.Failed = append(res.Failed, fail)
			report.emit(Event{Stage: "pull", Level: "warn", Message: fail.Item + ": " + fail.Error})
			continue
		}
		meta := store.SkillMeta{ID: sk.ID, Name: sk.Name, Description: sk.Description, Source: sk.Source, UpdatedAt: sk.UpdatedAt, SizeBytes: int64(len(pkg))}
		if err := st.SaveSkill(meta, pkg); err != nil {
			return err
		}
		res.SkillsRead++
		report.emit(Event{Stage: "pull", Level: "info", Message: "skill " + sk.Name})
	}
	return nil
}

// pushSkills uploads each pulled skill once. A skill whose name already
// exists in the target (for example uploaded by hand) is adopted as sent
// rather than uploaded again, since uploads never overwrite.
func (r *pushRun) pushSkills() error {
	skills, err := r.st.ListSkills()
	if err != nil || len(skills) == 0 {
		return err
	}
	if r.state.Skills == nil {
		r.state.Skills = map[string]*store.ItemState{}
	}
	var pending []store.SkillMeta
	for _, sk := range skills {
		if it := r.state.Skills[sk.ID]; it == nil || it.Status != store.StatusDone {
			pending = append(pending, sk)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	existing, _, err := withRetry(r.ctx, r.opts, func() ([]claudeapi.Skill, error) { return r.api.ListSkills(r.ctx, r.org) })
	if errors.Is(err, claudeapi.ErrNotFound) {
		existing, err = nil, nil // no listing: uploads report their own errors
	}
	if err != nil {
		if stop := stopErr(err); stop != nil {
			return stop
		}
		return fmt.Errorf("list target skills: %w", err)
	}
	have := map[string]bool{}
	for _, s := range existing {
		have[s.Name] = true
	}
	r.project = "Skills"
	for _, sk := range pending {
		it := r.state.Skills[sk.ID]
		if it == nil {
			it = &store.ItemState{}
			r.state.Skills[sk.ID] = it
		}
		if have[sk.Name] {
			it.Status, it.Error = store.StatusDone, ""
			if err := r.save(); err != nil {
				return err
			}
			continue
		}
		pkg, err := r.st.ReadSkill(sk.ID)
		if err != nil {
			it.Status, it.Error = store.StatusFailed, "local copy missing: "+err.Error()
			r.fail("skill "+sk.Name, it.Error)
			if err := r.save(); err != nil {
				return err
			}
			continue
		}
		name := sk.Name
		retry := false
		if err := r.item("skill "+name, it, "", func() (string, error) {
			// An upload can take effect even when its reply is lost, and a
			// second upload of the same name is refused: look before retrying.
			if retry {
				if there, err := r.targetHasSkill(name); err != nil || there {
					return "", err
				}
			}
			retry = true
			return "", r.api.UploadSkill(r.ctx, r.org, name+".skill", pkg)
		}); err != nil {
			return err
		}
		if it.Status == store.StatusDone {
			r.res.Skills++
			have[name] = true
		}
	}
	return nil
}

func (r *pushRun) targetHasSkill(name string) (bool, error) {
	skills, err := r.api.ListSkills(r.ctx, r.org)
	if err != nil {
		return false, err
	}
	for _, s := range skills {
		if s.Name == name {
			return true, nil
		}
	}
	return false, nil
}
