package app

import (
	"fmt"
	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
	"github.com/ossmalaysia/claude-sync/internal/migrate"
	"github.com/ossmalaysia/claude-sync/internal/store"
)

func (a *App) ListDirections() ([]store.Direction, error) { return a.st.ListDirections() }

// ReverseDirection keeps both histories and reuses the opposite browser login.
func (a *App) ReverseDirection() error {
	from, fromName := a.orgOf("source")
	to, toName := a.orgOf("target")
	if err := a.st.ActivateDirection(to, from, toName, fromName, !a.st.Reversed()); err != nil {
		return err
	}
	a.dropSession("source")
	a.dropSession("target")
	return nil
}

func (a *App) SelectDirection(from, to string) error {
	directions, err := a.st.ListDirections()
	if err != nil {
		return err
	}
	for _, d := range directions {
		if d.SourceOrg == from && d.TargetOrg == to {
			if err := a.st.ActivateDirection(from, to, d.SourceName, d.TargetName, d.Reversed); err != nil {
				return err
			}
			a.dropSession("source")
			a.dropSession("target")
			return nil
		}
	}
	return fmt.Errorf("saved direction not found")
}

// Preview reads the target and stores content-verified links. No target writes.
func (a *App) Preview() (migrate.PlanResult, error) {
	org, err := a.resolveOrg("target", "")
	if err != nil {
		return migrate.PlanResult{}, err
	}
	ctx, _, finish, err := a.startJob("preview")
	if err != nil {
		return migrate.PlanResult{}, err
	}
	defer finish()
	s, err := a.session("target")
	if err != nil {
		return migrate.PlanResult{}, err
	}
	state, err := a.st.LoadState()
	if err != nil {
		return migrate.PlanResult{}, err
	}
	sel, err := migrate.EffectiveSelection(a.st)
	if err != nil {
		return migrate.PlanResult{}, err
	}
	result, err := migrate.Preview(ctx, claudeapi.New(s), a.st, state, org, sel, a.opts)
	a.dropIfGone("target", err)
	return result, err
}

// MapProject resolves an ambiguous name with a user-selected, verified target ID.
func (a *App) MapProject(source, target string) error {
	org, err := a.resolveOrg("target", "")
	if err != nil {
		return err
	}
	ctx, _, finish, err := a.startJob("preview")
	if err != nil {
		return err
	}
	defer finish()
	_, known, err := a.st.LoadProject(source)
	if err != nil {
		return err
	}
	if !known {
		return fmt.Errorf("source project not found")
	}
	state, err := a.st.LoadState()
	if err != nil {
		return err
	}
	if state.TargetOrg != "" && state.TargetOrg != org {
		return migrate.ErrOrgMismatch
	}
	for id, p := range state.Projects {
		if id != source && p.Target == target {
			return fmt.Errorf("target project is already linked to another source project")
		}
	}
	ps := state.Project(source)
	if ps.Target != "" && ps.Target != target {
		return fmt.Errorf("project already has a saved ID mapping")
	}
	s, err := a.session("target")
	if err != nil {
		return err
	}
	if _, err := claudeapi.New(s).GetProject(ctx, org, target); err != nil {
		return err
	}
	ps.Target, ps.Status, ps.Error = target, "", ""
	state.TargetOrg = org
	return a.st.SaveState(state)
}
