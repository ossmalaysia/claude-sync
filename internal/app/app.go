// Package app exposes the migration core to the Wails frontend. Browser
// launching and event emission are injected so this package is testable
// without Chrome or Wails.
package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
	"github.com/ossmalaysia/claude-sync/internal/migrate"
	"github.com/ossmalaysia/claude-sync/internal/store"
)

type Session interface {
	claudeapi.Doer
	WaitLoggedIn(ctx context.Context, poll time.Duration) error
	Close()
}

type Opener func(profileDir string) (Session, error)

type Emitter func(ctx context.Context, event string, data any)

type App struct {
	ctx          context.Context
	st           *store.Store
	open         Opener
	emit         Emitter
	opts         migrate.Options
	loginTimeout time.Duration

	mu        sync.Mutex
	sessions  map[string]Session
	jobCancel context.CancelFunc // non-nil while a pull or push runs in this app
}

func New(st *store.Store, open Opener, emit Emitter) *App {
	return &App{
		ctx: context.Background(), st: st, open: open, emit: emit,
		opts: migrate.DefaultOptions(), loginTimeout: 10 * time.Minute,
		sessions: map[string]Session{},
	}
}

func (a *App) Startup(ctx context.Context) { a.ctx = ctx }

func (a *App) Shutdown(context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.jobCancel != nil {
		a.jobCancel()
	}
	for k, s := range a.sessions {
		s.Close()
		delete(a.sessions, k)
	}
}

func (a *App) report(e migrate.Event) {
	if a.emit != nil {
		a.emit(a.ctx, "progress", e)
	}
}

type OrgInfo struct {
	UUID         string   `json:"uuid"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
}

type AccountInfo struct {
	Account      string    `json:"account"`
	Orgs         []OrgInfo `json:"orgs"`
	SuggestedOrg string    `json:"suggested_org"`
}

// SuggestOrg picks the default org: a team org ("raven") for the target,
// the personal chat org for the source.
func SuggestOrg(orgs []claudeapi.Org, account string) string {
	if account == "target" {
		for _, o := range orgs {
			if o.Has("raven") {
				return o.UUID
			}
		}
	}
	for _, o := range orgs {
		if o.Has("chat") && !o.Has("raven") {
			return o.UUID
		}
	}
	for _, o := range orgs {
		if o.Has("chat") {
			return o.UUID
		}
	}
	return ""
}

func validAccount(account string) error {
	if account != "source" && account != "target" {
		return fmt.Errorf("unknown account %q (want source or target)", account)
	}
	return nil
}

func (a *App) dropSession(account string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if s := a.sessions[account]; s != nil {
		s.Close()
		delete(a.sessions, account)
	}
}

// ConnectAccount opens (or reuses) the account's browser window, waits for
// login and lists its orgs. Calling it again after "needs_login" re-waits
// on the same window.
func (a *App) ConnectAccount(account string) (AccountInfo, error) {
	if err := validAccount(account); err != nil {
		return AccountInfo{}, err
	}
	a.mu.Lock()
	s := a.sessions[account]
	a.mu.Unlock()
	if s == nil {
		var err error
		if s, err = a.open(a.st.ProfileDir(account)); err != nil {
			return AccountInfo{}, err
		}
		a.mu.Lock()
		a.sessions[account] = s
		a.mu.Unlock()
	}
	ctx, cancel := context.WithTimeout(a.ctx, a.loginTimeout)
	defer cancel()
	if err := s.WaitLoggedIn(ctx, 2*time.Second); err != nil {
		a.dropSession(account)
		return AccountInfo{}, fmt.Errorf("login not completed: %w", err)
	}
	orgs, err := claudeapi.New(s).ListOrgs(ctx)
	if err != nil {
		return AccountInfo{}, err
	}
	info := AccountInfo{Account: account, SuggestedOrg: SuggestOrg(orgs, account)}
	for _, o := range orgs {
		info.Orgs = append(info.Orgs, OrgInfo{UUID: o.UUID, Name: o.Name, Capabilities: o.Capabilities})
	}
	if cur, _ := a.orgOf(account); cur == "" && info.SuggestedOrg != "" {
		for _, o := range info.Orgs {
			if o.UUID == info.SuggestedOrg {
				if err := a.SetOrg(account, o.UUID, o.Name); err != nil {
					return info, err
				}
			}
		}
	}
	return info, nil
}

// session returns the account's live session, opening its browser window
// with the saved profile when needed. With a saved login this needs no user
// action; otherwise the window waits for the user to log in.
func (a *App) session(account string) (Session, error) {
	a.mu.Lock()
	s := a.sessions[account]
	a.mu.Unlock()
	if s != nil {
		return s, nil
	}
	s, err := a.open(a.st.ProfileDir(account))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, a.loginTimeout)
	defer cancel()
	if err := s.WaitLoggedIn(ctx, 2*time.Second); err != nil {
		s.Close()
		return nil, fmt.Errorf("%s login not completed: %w", account, err)
	}
	a.mu.Lock()
	a.sessions[account] = s
	a.mu.Unlock()
	a.fillOrgName(ctx, account, s)
	return s, nil
}

// fillOrgName looks up the remembered org's display name when it is missing
// (e.g. remembered from a manifest or state written by an older version).
func (a *App) fillOrgName(ctx context.Context, account string, s Session) {
	uuid, name := a.orgOf(account)
	if uuid == "" || name != "" {
		return
	}
	orgs, err := claudeapi.New(s).ListOrgs(ctx)
	if err != nil {
		return // cosmetic only; the job itself does not need the name
	}
	for _, o := range orgs {
		if o.UUID == uuid {
			a.SetOrg(account, o.UUID, o.Name)
		}
	}
}

// dropIfGone forgets the account's session when its window was closed, so
// the next job opens a fresh window instead of reusing a dead one.
func (a *App) dropIfGone(account string, err error) {
	if errors.Is(err, claudeapi.ErrSessionGone) || errors.Is(err, migrate.ErrAuthPaused) {
		a.dropSession(account)
	}
}

// SetOrg remembers the org chosen for an account.
func (a *App) SetOrg(account, uuid, name string) error {
	if err := validAccount(account); err != nil {
		return err
	}
	st, err := a.st.LoadSettings()
	if err != nil {
		return err
	}
	if account == "source" {
		st.SourceOrg, st.SourceOrgName = uuid, name
	} else {
		st.TargetOrg, st.TargetOrgName = uuid, name
	}
	return a.st.SaveSettings(st)
}

// orgOf returns the remembered org for an account, falling back to what an
// earlier pull (manifest) or push (state) used.
func (a *App) orgOf(account string) (uuid, name string) {
	st, _ := a.st.LoadSettings()
	if account == "source" {
		if st.SourceOrg != "" {
			return st.SourceOrg, st.SourceOrgName
		}
		if m, err := a.st.LoadManifest(); err == nil {
			return m.SourceOrg, ""
		}
		return "", ""
	}
	if st.TargetOrg != "" {
		return st.TargetOrg, st.TargetOrgName
	}
	if state, err := a.st.LoadState(); err == nil {
		return state.TargetOrg, ""
	}
	return "", ""
}

func (a *App) resolveOrg(account, org string) (string, error) {
	if org != "" {
		return org, nil
	}
	if o, _ := a.orgOf(account); o != "" {
		return o, nil
	}
	return "", fmt.Errorf("choose the %s org first", account)
}

// startJob takes the cross-process job lock and a cancellable context that
// StopJob cancels. finish must be called when the job ends.
func (a *App) startJob(kind string) (ctx context.Context, report migrate.Reporter, finish func(), err error) {
	release, err := a.st.AcquireJob(kind)
	if err != nil {
		return nil, nil, nil, err
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.mu.Lock()
	a.jobCancel = cancel
	a.mu.Unlock()
	report = func(e migrate.Event) {
		a.st.TouchJob()
		a.report(e)
	}
	finish = func() {
		a.mu.Lock()
		a.jobCancel = nil
		a.mu.Unlock()
		cancel()
		release()
	}
	return ctx, report, finish, nil
}

// StopJob stops the running pull or push. Everything done so far is saved;
// starting the same job again resumes.
func (a *App) StopJob() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.jobCancel != nil {
		a.jobCancel()
	}
}

// jobStatus maps a job's error to "completed" | "paused" | "needs_login",
// or returns the error when it is a real failure.
func jobStatus(err error) (string, error) {
	switch {
	case err == nil:
		return "completed", nil
	case errors.Is(err, migrate.ErrAuthPaused), errors.Is(err, claudeapi.ErrSessionGone), errors.Is(err, claudeapi.ErrAuth):
		return "needs_login", nil
	case errors.Is(err, context.Canceled):
		return "paused", nil
	}
	return "", err
}

type PullOutcome struct {
	Result migrate.PullResult `json:"result"`
	Status string             `json:"status"` // "completed" | "paused" | "needs_login"
}

// Pull downloads (or resumes downloading) the source org. org may be "" to
// use the remembered source org.
func (a *App) Pull(org string) (PullOutcome, error) {
	org, err := a.resolveOrg("source", org)
	if err != nil {
		return PullOutcome{}, err
	}
	ctx, report, finish, err := a.startJob("pull")
	if err != nil {
		return PullOutcome{}, err
	}
	defer finish()
	s, err := a.session("source")
	if err != nil {
		return PullOutcome{}, err
	}
	res, err := migrate.Pull(ctx, claudeapi.New(s), a.st, org, a.opts, report)
	a.dropIfGone("source", err)
	status, err := jobStatus(err)
	return PullOutcome{Result: res, Status: status}, err
}

type SelectionRow struct {
	UUID     string `json:"uuid"`
	Name     string `json:"name"`
	Docs     int    `json:"docs"`
	Files    int    `json:"files"`
	Selected bool   `json:"selected"`
}

func (a *App) GetSelection() ([]SelectionRow, error) {
	projects, err := a.st.ListProjects()
	if err != nil {
		return nil, err
	}
	existing, err := a.st.LoadSelection()
	if err != nil {
		return nil, err
	}
	sel := migrate.MergeSelection(projects, existing)
	if err := a.st.SaveSelection(sel); err != nil {
		return nil, err
	}
	rows := make([]SelectionRow, 0, len(projects))
	for _, p := range projects {
		docs, err := a.st.ListDocs(p.UUID)
		if err != nil {
			return nil, err
		}
		files, err := a.st.ListFiles(p.UUID)
		if err != nil {
			return nil, err
		}
		rows = append(rows, SelectionRow{UUID: p.UUID, Name: p.Name, Docs: len(docs), Files: len(files), Selected: sel[p.UUID]})
	}
	return rows, nil
}

func (a *App) SaveSelection(sel map[string]bool) error { return a.st.SaveSelection(sel) }

func (a *App) Plan(org string) (migrate.PlanResult, error) {
	org, err := a.resolveOrg("target", org)
	if err != nil {
		return migrate.PlanResult{}, err
	}
	state, err := a.st.LoadState()
	if err != nil {
		return migrate.PlanResult{}, err
	}
	sel, err := a.st.LoadSelection()
	if err != nil {
		return migrate.PlanResult{}, err
	}
	return migrate.Plan(a.st, sel, state, org, a.opts)
}

type PushOutcome struct {
	Result migrate.PushResult `json:"result"`
	Status string             `json:"status"` // "completed" | "paused" | "needs_login"
}

// Push creates (or resumes creating) the selected projects in the target
// org. org may be "" to use the remembered target org.
func (a *App) Push(org string) (PushOutcome, error) {
	org, err := a.resolveOrg("target", org)
	if err != nil {
		return PushOutcome{}, err
	}
	ctx, report, finish, err := a.startJob("push")
	if err != nil {
		return PushOutcome{}, err
	}
	defer finish()
	s, err := a.session("target")
	if err != nil {
		return PushOutcome{}, err
	}
	state, err := a.st.LoadState()
	if err != nil {
		return PushOutcome{}, err
	}
	sel, err := a.st.LoadSelection()
	if err != nil {
		return PushOutcome{}, err
	}
	res, err := migrate.Push(ctx, claudeapi.New(s), a.st, state, org, sel, a.opts, report)
	a.dropIfGone("target", err)
	status, err := jobStatus(err)
	return PushOutcome{Result: res, Status: status}, err
}

// Verify compares the target org with the local copy and remembers the result.
func (a *App) Verify(org string) ([]migrate.VerifyRow, error) {
	org, err := a.resolveOrg("target", org)
	if err != nil {
		return nil, err
	}
	s, err := a.session("target")
	if err != nil {
		return nil, err
	}
	state, err := a.st.LoadState()
	if err != nil {
		return nil, err
	}
	rows, err := migrate.Verify(a.ctx, claudeapi.New(s), a.st, state, org, a.opts)
	a.dropIfGone("target", err)
	if err != nil {
		return rows, err
	}
	ok := 0
	for _, r := range rows {
		if r.OK {
			ok++
		}
	}
	settings, err := a.st.LoadSettings()
	if err != nil {
		return rows, err
	}
	settings.VerifiedAt, settings.VerifyOK, settings.VerifyTotal = time.Now().UTC(), ok, len(rows)
	return rows, a.st.SaveSettings(settings)
}

func (a *App) MemoryText() (string, error) { return a.st.LoadMemory() }

type AccountStatus struct {
	SavedLogin bool   `json:"saved_login"` // a browser profile with a login exists
	Connected  bool   `json:"connected"`   // a browser window is open in this app
	Org        string `json:"org"`
	OrgName    string `json:"org_name"`
}

// Status is everything the home screen shows, rebuilt from the files on
// disk, so it is correct after a restart and when the CLI did the work.
type Status struct {
	Source AccountStatus `json:"source"`
	Target AccountStatus `json:"target"`

	Job     string `json:"job"`      // "pull" | "push" | "" — running anywhere (app or CLI)
	JobHere bool   `json:"job_here"` // running in this app, so it can be stopped here

	PullTotal    int    `json:"pull_total"`
	PullDone     int    `json:"pull_done"` // projects on disk
	PullComplete bool   `json:"pull_complete"`
	PulledAt     string `json:"pulled_at"`

	Projects int `json:"projects"` // projects on disk
	Selected int `json:"selected"`

	Pushed          int `json:"pushed"`           // selected projects already created in the target
	PendingProjects int `json:"pending_projects"` // selected projects not created yet
	PendingItems    int `json:"pending_items"`    // instructions, docs and files still to send
	Failed          int `json:"failed"`           // items that failed and will be retried
	Skipped         int `json:"skipped"`          // files too large to upload

	VerifiedAt  string `json:"verified_at"`
	VerifyOK    int    `json:"verify_ok"`
	VerifyTotal int    `json:"verify_total"`
	VerifyStale bool   `json:"verify_stale"` // pushed again since the last verify

	Next  string `json:"next"`  // see nextAction
	Error string `json:"error"` // a problem to show, e.g. state for a different org
}

// nextAction is the one step the home screen suggests: "wait",
// "connect_source", "pull", "select", "connect_target", "push", "verify"
// or "done".
func nextAction(s Status) string {
	switch {
	case s.Job != "":
		return "wait"
	case s.Source.Org == "":
		return "connect_source"
	case !s.PullComplete:
		return "pull"
	case s.Selected == 0:
		return "select"
	case s.Target.Org == "":
		return "connect_target"
	case s.PendingProjects+s.PendingItems+s.Failed > 0:
		return "push"
	case s.VerifiedAt == "" || s.VerifyStale:
		return "verify"
	}
	return "done"
}

func (a *App) Status() (Status, error) {
	var s Status
	a.mu.Lock()
	s.Source.Connected = a.sessions["source"] != nil
	s.Target.Connected = a.sessions["target"] != nil
	s.JobHere = a.jobCancel != nil
	a.mu.Unlock()
	s.Source.SavedLogin = a.st.HasProfile("source")
	s.Target.SavedLogin = a.st.HasProfile("target")
	s.Source.Org, s.Source.OrgName = a.orgOf("source")
	s.Target.Org, s.Target.OrgName = a.orgOf("target")
	s.Job = a.st.RunningJob()

	if m, err := a.st.LoadManifest(); err == nil {
		s.PullTotal, s.PullComplete = m.Total, m.Complete
		if !m.PulledAt.IsZero() {
			s.PulledAt = m.PulledAt.Format(time.RFC3339)
		}
	}
	projects, err := a.st.ListProjects()
	if err != nil {
		return s, err
	}
	s.Projects, s.PullDone = len(projects), len(projects)
	sel, err := a.st.LoadSelection()
	if err != nil {
		return s, err
	}
	for _, p := range projects {
		if sel[p.UUID] {
			s.Selected++
		}
	}
	state, err := a.st.LoadState()
	if err != nil {
		s.Error = err.Error()
		s.Next = nextAction(s)
		return s, nil
	}
	if s.Target.Org != "" {
		plan, err := migrate.Plan(a.st, sel, state, s.Target.Org, a.opts)
		if err != nil {
			s.Error = err.Error()
		} else {
			s.PendingProjects = plan.NewProjects
			s.PendingItems = plan.NewInstructions + plan.NewDocs + plan.NewFiles
			s.Failed = plan.RetryFailed
			s.Skipped = len(plan.Skipped)
			s.Pushed = plan.SelectedProjects - plan.NewProjects
		}
	}
	settings, err := a.st.LoadSettings()
	if err != nil {
		return s, err
	}
	if !settings.VerifiedAt.IsZero() {
		s.VerifiedAt = settings.VerifiedAt.Format(time.RFC3339)
		s.VerifyOK, s.VerifyTotal = settings.VerifyOK, settings.VerifyTotal
		s.VerifyStale = a.st.StateUpdatedAt().After(settings.VerifiedAt)
	}
	s.Next = nextAction(s)
	return s, nil
}
