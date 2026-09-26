// Package app exposes the migration core to the Wails frontend. Browser
// launching and event emission are injected so this package is testable
// without Chrome or Wails.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
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

// Version is shown in the app. Release builds set it from the git tag:
// -ldflags "-X github.com/ossmalaysia/claude-sync/internal/app.Version=v0.1.0".
var Version = "v0.1.3-dev"

// Version reports the build's version for display.
func (a *App) Version() string { return Version }

type App struct {
	ctx          context.Context
	st           *store.Store
	open         Opener
	emit         Emitter
	opts         migrate.Options
	loginTimeout time.Duration
	openPath     func(path string) error // opens a folder in Finder / Explorer
	openURL      func(url string) error  // opens a web page in the default browser

	mu        sync.Mutex
	sessions  map[string]Session
	jobCancel context.CancelFunc // non-nil while a pull or push runs in this app
}

func New(st *store.Store, open Opener, emit Emitter) *App {
	return &App{
		ctx: context.Background(), st: st, open: open, emit: emit,
		opts: migrate.DefaultOptions(), loginTimeout: 10 * time.Minute,
		sessions: map[string]Session{},
		openPath: openInFileManager,
		openURL:  openInBrowser,
	}
}

func openInFileManager(path string) error {
	switch goruntime.GOOS {
	case "windows":
		return exec.Command("explorer", path).Start() // #nosec G204 -- path is our own artifacts folder, not user input
	case "darwin":
		return exec.Command("open", path).Start() // #nosec G204 -- path is our own artifacts folder, not user input
	}
	return exec.Command("xdg-open", path).Start() // #nosec G204 -- path is our own artifacts folder, not user input
}

// links are the only pages the app opens, so the page cannot be used to
// launch arbitrary URLs.
var links = map[string]string{
	"website":  "https://www.anchorsprint.com/",
	"issues":   "https://github.com/ossmalaysia/claude-sync/issues/new/choose",
	"projects": "https://claude.ai/projects",
}

// OpenLink opens one of the app's own web pages in the default browser.
func (a *App) OpenLink(name string) error {
	u, ok := links[name]
	if !ok {
		return fmt.Errorf("unknown link %q", name)
	}
	return a.openURL(u)
}

func openInBrowser(u string) error {
	switch goruntime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start() // #nosec G204 -- u is one of the fixed links
	case "darwin":
		return exec.Command("open", u).Start() // #nosec G204 -- u is one of the fixed links
	}
	return exec.Command("xdg-open", u).Start() // #nosec G204 -- u is one of the fixed links
}

// OpenArtifactsFolder shows the readable copies of recovered artifacts.
func (a *App) OpenArtifactsFolder() error {
	dir := filepath.Join(a.st.Root(), "migration", "artifacts-export")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return a.openPath(dir)
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
			_ = a.SetOrg(account, o.UUID, o.Name) // cosmetic; the job does not need the name
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
	if !a.termsAccepted() {
		return nil, nil, nil, ErrTermsNotAccepted
	}
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
	settings, err := a.st.LoadSettings()
	if err != nil {
		return nil, err
	}
	sel := migrate.MergeSelection(projects, existing, settings.PersonalChoice == store.PersonalInclude)
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
	sel, err := migrate.EffectiveSelection(a.st)
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
	if pending, err := a.copyReviewPending(); err != nil {
		return PushOutcome{}, err
	} else if pending {
		return PushOutcome{}, ErrReviewCopy
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
	sel, err := migrate.EffectiveSelection(a.st)
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
	if _, err := migrate.EffectiveSelection(a.st); err != nil {
		return nil, err
	}
	rows, err := migrate.Verify(a.ctx, claudeapi.New(s), a.st, state, org, a.opts, a.report, nil)
	a.dropIfGone("target", err)
	return rows, err
}

func (a *App) MemoryText() (string, error) { return a.st.LoadMemory() }

type MemoryOutcome struct {
	Status string `json:"status"` // "sent" | "unchanged" | "empty" | "skipped" | "needs_login"
	SentAt string `json:"sent_at"`
}

// SyncMemory sends memory.md to the target account's memory import, the same
// call as Settings > Memory > Start import. The same text is never sent twice:
// claude.ai merges imports, so a repeat could duplicate entries. Nothing is
// sent ("skipped") while memory is switched off in "What to copy".
func (a *App) SyncMemory() (MemoryOutcome, error) {
	if settings, err := a.st.LoadSettings(); err != nil {
		return MemoryOutcome{}, err
	} else if settings.SkipMemory {
		return MemoryOutcome{Status: "skipped", SentAt: a.memorySentAt()}, nil
	}
	if pending, err := migrate.MemoryPending(a.st); err != nil {
		return MemoryOutcome{}, err
	} else if !pending {
		// Nothing new: report without opening the browser.
		out, err := migrate.SyncMemory(a.ctx, nil, a.st, "")
		return MemoryOutcome{Status: out, SentAt: a.memorySentAt()}, err
	}
	org, err := a.resolveOrg("target", "")
	if err != nil {
		return MemoryOutcome{}, err
	}
	_, _, finish, err := a.startJob("memory")
	if err != nil {
		return MemoryOutcome{}, err
	}
	defer finish()
	s, err := a.session("target")
	if err != nil {
		return MemoryOutcome{}, err
	}
	out, err := migrate.SyncMemory(a.ctx, claudeapi.New(s), a.st, org)
	if err != nil {
		a.dropIfGone("target", err)
		if status, serr := jobStatus(err); serr == nil {
			return MemoryOutcome{Status: status}, nil
		}
		return MemoryOutcome{}, err
	}
	return MemoryOutcome{Status: out, SentAt: a.memorySentAt()}, nil
}

func (a *App) memorySentAt() string {
	if st, err := a.st.LoadSettings(); err == nil && !st.MemorySentAt.IsZero() {
		return st.MemorySentAt.Format(time.RFC3339)
	}
	return ""
}

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
	PullPhase    string `json:"pull_phase"` // "projects" | "chats" while a pull runs

	// Progress of the running pull phase, for the time-left estimate.
	PhaseStartedAt string `json:"phase_started_at"`
	PhaseDone      int    `json:"phase_done"`
	PhaseTotal     int    `json:"phase_total"`
	ChatsTotal     int    `json:"chats_total"`
	Chats          int    `json:"chats"`     // chats read by the last pull
	Artifacts      int    `json:"artifacts"` // artifacts recovered from those chats

	ArtifactsSent      int `json:"artifacts_sent"`       // added to target projects
	ArtifactsPending   int `json:"artifacts_pending"`    // in selected projects, not sent yet
	ArtifactsNoProject int `json:"artifacts_no_project"` // from chats outside a project: local export only

	Skills        int  `json:"skills"`         // personal skills in the source (built-in ones are not copied)
	SkillsSent    int  `json:"skills_sent"`    // uploaded to (or already in) the target
	SkillsPending int  `json:"skills_pending"` // pulled, not sent yet
	SkillsFailed  int  `json:"skills_failed"`  // last upload failed; retried by the next push
	TermsAccepted bool `json:"terms_accepted"` // the first-use notice was accepted
	// "What to copy" was saved at least once, and the kinds switched off there.
	CopyReviewed  bool `json:"copy_reviewed"`
	SkipArtifacts bool `json:"skip_artifacts"`
	SkipSkills    bool `json:"skip_skills"`
	SkipMemory    bool `json:"skip_memory"`
	// Projects that look personal (Personal:, Family:, Travel), how many of
	// them are not selected, and the user's choice ("" = not asked yet).
	PersonalProjects int    `json:"personal_projects"`
	PersonalSkipped  int    `json:"personal_skipped"`
	PersonalChoice   string `json:"personal_choice"`
	// Chats in the selected projects, and how many of their transcripts are
	// sent or waiting; ChatChoice is "" until the user is asked.
	ChatsInProjects int    `json:"chats_in_projects"`
	ChatsSent       int    `json:"chats_sent"`
	ChatsPending    int    `json:"chats_pending"`
	ChatChoice      string `json:"chat_choice"`

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
	// VerifyWaiting counts projects that differ only by artifacts not sent yet.
	VerifyWaiting     int  `json:"verify_waiting"`
	VerifyStale       bool `json:"verify_stale"`        // pushed again since the last verify
	VerifyUnchecked   int  `json:"verify_unchecked"`    // selected projects without a current check
	VerifyNotSelected int  `json:"verify_not_selected"` // sent earlier but not selected now (not checked)

	MemorySentAt  string `json:"memory_sent_at"` // last time memory was sent to the target
	MemoryPending bool   `json:"memory_pending"` // memory.md has text that was not sent yet (false while memory is switched off)

	LastSyncAt              string `json:"last_sync_at"`
	LastSyncNewProjects     int    `json:"last_sync_new_projects"`
	LastSyncChangedProjects int    `json:"last_sync_changed_projects"`
	LastSyncChats           int    `json:"last_sync_chats"`
	LastSyncArtifacts       int    `json:"last_sync_artifacts"`
	LastSyncSent            int    `json:"last_sync_sent"`

	Next  string `json:"next"`  // see nextAction
	Error string `json:"error"` // a problem to show, e.g. state for a different org
}

// nextAction is the one step the home screen suggests: "wait",
// "connect_source", "pull", "select", "connect_target", "push", "verify",
// "review", "memory" or "done".
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
	case s.VerifyOK < s.VerifyTotal:
		return "review" // verified, but some projects do not match
	case s.MemoryPending:
		return "memory"
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
	s.TermsAccepted = a.termsAccepted()

	if m, err := a.st.LoadManifest(); err == nil {
		s.PullTotal, s.PullComplete, s.PullPhase = m.Total, m.Complete, m.Phase
		s.PhaseDone, s.PhaseTotal, s.ChatsTotal = m.PhaseDone, m.PhaseTotal, m.ChatsTotal
		if !m.PhaseStartedAt.IsZero() {
			s.PhaseStartedAt = m.PhaseStartedAt.Format(time.RFC3339)
		}
		s.Chats, s.Artifacts, s.ArtifactsNoProject = m.Chats, m.Artifacts, m.ArtifactsNoProject
		s.Skills = m.Skills
		if !m.PulledAt.IsZero() {
			s.PulledAt = m.PulledAt.Format(time.RFC3339)
		}
	}
	projects, err := a.st.ListProjects()
	if err != nil {
		return s, err
	}
	s.Projects, s.PullDone = len(projects), len(projects)
	saved, err := a.st.LoadSelection()
	if err != nil {
		return s, err
	}
	choice, err := a.st.LoadSettings()
	if err != nil {
		return s, err
	}
	s.PersonalChoice, s.ChatChoice = choice.PersonalChoice, choice.ChatChoice
	s.CopyReviewed = !choice.CopyReviewedAt.IsZero()
	s.SkipArtifacts, s.SkipSkills, s.SkipMemory = choice.SkipArtifacts, choice.SkipSkills, choice.SkipMemory
	sel := migrate.MergeSelection(projects, saved, choice.PersonalChoice == store.PersonalInclude) // read-only: Status must not write

	for _, p := range projects {
		if sel[p.UUID] {
			s.Selected++
		}
		if migrate.LooksPersonal(p.Name) {
			s.PersonalProjects++
			if !sel[p.UUID] {
				s.PersonalSkipped++
			}
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
			s.PendingItems = plan.NewInstructions + plan.NewDocs + plan.NewFiles + plan.NewArtifacts + plan.NewChats
			s.ArtifactsPending = plan.NewArtifacts
			s.ChatsPending = plan.NewChats
			s.SkillsPending = plan.NewSkills
			s.PendingItems += plan.NewSkills
			s.Failed = plan.RetryFailed
			s.Skipped = len(plan.Skipped)
			s.Pushed = plan.SelectedProjects - plan.NewProjects
		}
	}
	for _, it := range state.Skills {
		switch it.Status {
		case store.StatusDone:
			s.SkillsSent++
		case store.StatusFailed:
			s.SkillsFailed++
		}
	}
	for _, ps := range state.Projects {
		for _, it := range ps.Artifacts {
			if it.Status == store.StatusDone {
				s.ArtifactsSent++
			}
		}
		for _, it := range ps.Chats {
			if it.Status == store.StatusDone {
				s.ChatsSent++
			}
		}
	}
	if s.ChatsInProjects, err = a.chatsInSelectedProjects(sel); err != nil {
		return s, err
	}
	settings, err := a.st.LoadSettings()
	if err != nil {
		return s, err
	}
	if err := a.verifySummary(&s, projects, sel, state); err != nil {
		return s, err
	}
	if !settings.MemorySentAt.IsZero() {
		s.MemorySentAt = settings.MemorySentAt.Format(time.RFC3339)
	}
	if !settings.SkipMemory {
		s.MemoryPending, _ = migrate.MemoryPending(a.st)
	}
	if !settings.LastSyncAt.IsZero() {
		s.LastSyncAt = settings.LastSyncAt.Format(time.RFC3339)
		s.LastSyncNewProjects, s.LastSyncChangedProjects = settings.LastSyncNewProjects, settings.LastSyncChangedProjects
		s.LastSyncChats, s.LastSyncArtifacts, s.LastSyncSent = settings.LastSyncChats, settings.LastSyncArtifacts, settings.LastSyncSent
	}
	s.Next = nextAction(s)
	return s, nil
}

type SyncOutcome struct {
	Status string             `json:"status"` // "completed" | "paused" | "needs_login"
	Stage  string             `json:"stage"`  // where it stopped: "pull" | "push" | "memory"
	Pull   migrate.PullResult `json:"pull"`
	Push   migrate.PushResult `json:"push"`
	Memory string             `json:"memory"` // SyncMemory status
	// Projects changed by this sync, checked in the target afterwards.
	Checked   int `json:"checked"`
	CheckedOK int `json:"checked_ok"`
}

func (a *App) verifyOnly(only map[string]bool) ([]migrate.VerifyRow, error) {
	org, err := a.resolveOrg("target", "")
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
	return migrate.Verify(a.ctx, claudeapi.New(s), a.st, state, org, a.opts, a.report, only)
}

// SyncChanges brings the target up to date with the source: a delta pull
// (only new or changed projects and chats are read), a push of everything
// new, then memory if it changed. It stops at the first step that does not
// complete; running it again resumes.
func (a *App) SyncChanges() (SyncOutcome, error) {
	var out SyncOutcome
	pull, err := a.Pull("")
	out.Pull = pull.Result
	if err != nil {
		return out, err
	}
	if pull.Status != "completed" {
		out.Status, out.Stage = pull.Status, "pull"
		return out, nil
	}
	push, err := a.Push("")
	out.Push = push.Result
	if err != nil {
		return out, err
	}
	if push.Status != "completed" {
		out.Status, out.Stage = push.Status, "push"
		return out, nil
	}
	mem, err := a.SyncMemory()
	if err != nil {
		return out, err
	}
	out.Memory = mem.Status
	if mem.Status == "needs_login" {
		out.Status, out.Stage = "needs_login", "memory"
		return out, nil
	}
	if len(out.Push.Touched) > 0 {
		// Check just the projects this sync changed, so "Check the target"
		// stays current without a full check.
		only := map[string]bool{}
		for _, id := range out.Push.Touched {
			only[id] = true
		}
		rows, err := a.verifyOnly(only)
		if err != nil {
			a.dropIfGone("target", err)
			if status, serr := jobStatus(err); serr == nil {
				out.Status, out.Stage = status, "verify"
				return out, nil
			}
			return out, err
		}
		for _, r := range rows {
			out.Checked++
			if r.OK {
				out.CheckedOK++
			}
		}
	}
	out.Status = "completed"
	settings, err := a.st.LoadSettings()
	if err != nil {
		return out, err
	}
	p, r := out.Pull, out.Push
	settings.LastSyncAt = time.Now().UTC()
	settings.LastSyncNewProjects, settings.LastSyncChangedProjects = p.ProjectsNew, p.ProjectsRead-p.ProjectsNew
	settings.LastSyncChats, settings.LastSyncArtifacts = p.ChatsRead, p.ArtifactsNew
	settings.LastSyncSent = r.CreatedProjects + r.Instructions + r.Docs + r.Files + r.Artifacts + r.Chats + r.Skills
	return out, a.st.SaveSettings(settings)
}

// verifySummary fills the verify fields from the per-project results in
// verify.json, over the selected projects. A push clears the result of every
// project it changes, so a missing result means "changed since last check".
func (a *App) verifySummary(s *Status, projects []store.ProjectMeta, sel map[string]bool, state *store.State) error {
	checks, err := a.st.LoadVerify()
	if err != nil {
		return err
	}
	var latest time.Time
	for _, p := range projects {
		ps := state.Projects[p.UUID]
		if !sel[p.UUID] {
			if ps != nil && ps.Target != "" {
				s.VerifyNotSelected++
			}
			continue
		}
		s.VerifyTotal++
		r, ok := checks[p.UUID]
		switch {
		case !ok:
			s.VerifyUnchecked++
		case r.OK:
			s.VerifyOK++
		case r.Waiting > 0:
			s.VerifyWaiting++
		}
		if ok && r.CheckedAt.After(latest) {
			latest = r.CheckedAt
		}
	}
	if !latest.IsZero() {
		s.VerifiedAt = latest.Format(time.RFC3339)
		s.VerifyStale = s.VerifyUnchecked > 0
	}
	return nil
}
