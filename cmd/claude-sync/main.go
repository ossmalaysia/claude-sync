// Command claude-sync is the CLI for migrating claude.ai Projects.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/ossmalaysia/claude-sync/internal/browser"
	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
	"github.com/ossmalaysia/claude-sync/internal/migrate"
	"github.com/ossmalaysia/claude-sync/internal/store"
)

const usage = `usage: claude-sync <command> [flags]

commands:
  login  --account source|target   open the browser, wait for login, list orgs
  pull   --org <source-org-uuid>    download projects from the source account
  plan   --org <target-org-uuid>    show what push would do (no network)
  push   --org <target-org-uuid>    create selected projects in the target org
  verify --org <target-org-uuid>    compare target counts with what was pushed
  smoke  --org <target-org-uuid>    create, check and delete a test project
  memory --org <target-org-uuid>    send memory.md to the target's memory import (once per change)
  sync   [--from <org>] [--to <org>] scan the source for new or changed data and send it
                                    (defaults: the orgs used before)

flags:
  --data <dir>   data folder (default: <user config dir>/claude-sync)
`

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	cmd := args[0]
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	data := fs.String("data", "", "data folder")
	org := fs.String("org", "", "organization uuid")
	account := fs.String("account", "", "source or target (login only)")
	from := fs.String("from", "", "source org uuid (sync; default: the remembered source org)")
	to := fs.String("to", "", "target org uuid (sync; default: the remembered target org)")
	switch cmd {
	case "login", "pull", "plan", "push", "verify", "smoke", "memory", "sync":
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", cmd, usage)
		return 2
	}
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if cmd == "login" && *account != "source" && *account != "target" {
		fmt.Fprintln(stderr, "login needs --account source|target")
		return 2
	}
	if cmd != "login" && cmd != "sync" && *org == "" {
		fmt.Fprintf(stderr, "%s needs --org <uuid>\n", cmd)
		return 2
	}
	root := *data
	if root == "" {
		var err error
		if root, err = store.DefaultRoot(); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
	}
	st, err := store.Open(root)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	if cmd == "sync" {
		if err := runSync(ctx, st, *from, *to, stdout); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return 0
	}
	if err := dispatch(ctx, cmd, st, *org, *account, stdout); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func connect(ctx context.Context, st *store.Store, account string, out io.Writer) (*browser.Session, *claudeapi.Client, error) {
	exe, err := browser.FindBrowser()
	if err != nil {
		return nil, nil, err
	}
	s, err := browser.Open(exe, st.ProfileDir(account))
	if err != nil {
		return nil, nil, err
	}
	fmt.Fprintf(out, "Log in to the %s account in the browser window (waiting up to 10 min)…\n", account)
	wctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err := s.WaitLoggedIn(wctx, 2*time.Second); err != nil {
		s.Close()
		return nil, nil, fmt.Errorf("login: %w", err)
	}
	return s, claudeapi.New(s), nil
}

func loadSelection(st *store.Store) (map[string]bool, error) { return migrate.EffectiveSelection(st) }

func dispatch(ctx context.Context, cmd string, st *store.Store, org, account string, out io.Writer) error {
	opts := migrate.DefaultOptions()
	report := func(e migrate.Event) {
		fmt.Fprintf(out, "[%s %d/%d] %s%s\n", e.Stage, e.Done, e.Total, map[string]string{"warn": "WARN ", "error": "ERROR "}[e.Level], e.Message)
	}
	if cmd == "plan" {
		state, err := st.LoadState()
		if err != nil {
			return err
		}
		sel, err := loadSelection(st)
		if err != nil {
			return err
		}
		res, err := migrate.Plan(st, sel, state, org, opts)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "selected %d projects: create %d projects, %d instructions, %d docs, %d files (%.1f MB), %d artifacts; retry %d failed; %d already done\n",
			res.SelectedProjects, res.NewProjects, res.NewInstructions, res.NewDocs, res.NewFiles, float64(res.NewBytes)/(1<<20), res.NewArtifacts, res.RetryFailed, res.AlreadyDone)
		for _, s := range res.Skipped {
			fmt.Fprintf(out, "  skip (too large): %s / %s (%.1f MB)\n", s.Project, s.File, float64(s.SizeBytes)/(1<<20))
		}
		for _, c := range res.Changed {
			fmt.Fprintf(out, "  changed in source, not synced: %s\n", c)
		}
		fmt.Fprintf(out, "edit %s to change which projects are included\n", st.Root()+"/migration/selection.json")
		return nil
	}

	acct := "target"
	switch cmd {
	case "login":
		acct = account
	case "pull":
		acct = "source"
	}
	if cmd == "pull" || cmd == "push" {
		// Same lock as the desktop app: one job at a time, visible in the app.
		release, err := st.AcquireJob(cmd)
		if err != nil {
			return err
		}
		defer release()
		inner := report
		report = func(e migrate.Event) { st.TouchJob(); inner(e) }
	}
	s, c, err := connect(ctx, st, acct, out)
	if err != nil {
		return err
	}
	defer s.Close()

	switch cmd {
	case "login":
		orgs, err := c.ListOrgs(ctx)
		if err != nil {
			return err
		}
		for _, o := range orgs {
			fmt.Fprintf(out, "%s  %-45q %v\n", o.UUID, o.Name, o.Capabilities)
		}
	case "pull":
		res, err := migrate.Pull(ctx, c, st, org, opts, report)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "pulled %d projects, %d docs, %d files (%.1f MB), %d images as WebP preview; %d chats, %d artifacts; %d failed\n", res.Projects, res.Docs, res.Files, float64(res.Bytes)/(1<<20), res.Converted, res.Chats, res.Artifacts, len(res.Failed))
		fmt.Fprintf(out, "memory saved to %s — paste it into claude.ai Settings → Memory in the target account\n", st.Root()+"/migration/memory.md")
	case "push":
		state, err := st.LoadState()
		if err != nil {
			return err
		}
		sel, err := loadSelection(st)
		if err != nil {
			return err
		}
		res, err := migrate.Push(ctx, c, st, state, org, sel, opts, report)
		fmt.Fprintf(out, "created %d projects, %d instructions, %d docs, %d files, %d artifacts, %d skills; %d failed\n", res.CreatedProjects, res.Instructions, res.Docs, res.Files, res.Artifacts, res.Skills, len(res.Failed))
		for _, f := range res.Failed {
			fmt.Fprintf(out, "  FAILED %s / %s: %s\n", f.Project, f.Item, f.Error)
		}
		switch {
		case errors.Is(err, migrate.ErrAuthPaused):
			return fmt.Errorf("%w (run `claude-sync login --account target`, then push again)", err)
		case errors.Is(err, context.Canceled):
			fmt.Fprintln(out, "paused — run push again to resume")
			return nil
		}
		return err
	case "verify":
		state, err := st.LoadState()
		if err != nil {
			return err
		}
		rows, err := migrate.Verify(ctx, c, st, state, org, opts, nil, nil)
		if err != nil {
			return err
		}
		bad := 0
		for _, r := range rows {
			mark := "ok  "
			if !r.OK {
				mark = "FAIL"
				bad++
			}
			fmt.Fprintf(out, "%s %-45s docs %d/%d files %d/%d %s\n", mark, r.Name, r.GotDocs, r.WantDocs, r.GotFiles, r.WantFiles, r.Error)
		}
		waiting := 0
		for _, r := range rows {
			if !r.OK && r.Waiting > 0 {
				waiting++
			}
		}
		if bad > 0 && bad == waiting {
			fmt.Fprintf(out, "%d of %d projects match; the other %d only need their artifacts sent (select them and push)\n", len(rows)-bad, len(rows), bad)
			return nil
		}
		if bad > 0 {
			return fmt.Errorf("%d of %d projects do not match (%d of them only need their artifacts sent)", bad, len(rows), waiting)
		}
	case "smoke":
		return runSmoke(ctx, c, org, out)
	case "memory":
		res, err := migrate.SyncMemory(ctx, c, st, org)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "memory: %s\n", map[string]string{
			"sent":      "sent; claude.ai adds it to memory in the background",
			"unchanged": "already sent (memory.md has not changed)",
			"empty":     "memory.md is empty; run pull first",
		}[res])
	}
	return nil
}

// runSync is the CLI's "Scan & sync": a delta pull (only new or changed
// projects and chats are read), a push of everything new, then memory if it
// changed. Orgs default to the ones remembered from earlier runs.
func runSync(ctx context.Context, st *store.Store, from, to string, out io.Writer) error {
	settings, err := st.LoadSettings()
	if err != nil {
		return err
	}
	if from == "" {
		from = settings.SourceOrg
		if from == "" {
			if m, err := st.LoadManifest(); err == nil {
				from = m.SourceOrg
			}
		}
	}
	state, err := st.LoadState()
	if err != nil {
		return err
	}
	if to == "" {
		to = settings.TargetOrg
		if to == "" {
			to = state.TargetOrg
		}
	}
	if from == "" || to == "" {
		return errors.New("sync needs --from and --to the first time (or run pull and push once)")
	}
	opts := migrate.DefaultOptions()
	report := func(e migrate.Event) {
		if e.Level != "info" {
			fmt.Fprintf(out, "%s %s\n", strings.ToUpper(e.Level), e.Message)
		}
	}
	release, err := st.AcquireJob("pull")
	if err != nil {
		return err
	}
	src, sc, err := connect(ctx, st, "source", out)
	if err != nil {
		release()
		return err
	}
	pull, err := migrate.Pull(ctx, sc, st, from, opts, func(e migrate.Event) { st.TouchJob(); report(e) })
	src.Close()
	release()
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}
	fmt.Fprintf(out, "scan: %d new projects, %d changed, %d unchanged; %d new or changed chats (%d artifacts); %d of %d personal skills new or changed\n",
		pull.ProjectsNew, pull.ProjectsRead-pull.ProjectsNew, pull.ProjectsSkipped, pull.ChatsRead, pull.ArtifactsNew, pull.SkillsRead, pull.Skills)

	if release, err = st.AcquireJob("push"); err != nil {
		return err
	}
	defer release()
	dst, dc, err := connect(ctx, st, "target", out)
	if err != nil {
		return err
	}
	defer dst.Close()
	sel, err := migrate.EffectiveSelection(st)
	if err != nil {
		return err
	}
	res, err := migrate.Push(ctx, dc, st, state, to, sel, opts, func(e migrate.Event) { st.TouchJob(); report(e) })
	if err != nil {
		return fmt.Errorf("send: %w", err)
	}
	sent := res.CreatedProjects + res.Instructions + res.Docs + res.Files + res.Artifacts + res.Skills
	fmt.Fprintf(out, "send: %d projects (%d already in the target), %d instructions, %d docs, %d files, %d artifacts, %d skills; %d failed\n",
		res.CreatedProjects, res.AdoptedProjects, res.Instructions, res.Docs, res.Files, res.Artifacts, res.Skills, len(res.Failed))
	mem, err := migrate.SyncMemory(ctx, dc, st, to)
	if err != nil {
		return fmt.Errorf("memory: %w", err)
	}
	fmt.Fprintf(out, "memory: %s\n", mem)

	if settings, err = st.LoadSettings(); err != nil {
		return err
	}
	settings.LastSyncAt = time.Now().UTC()
	settings.LastSyncNewProjects, settings.LastSyncChangedProjects = pull.ProjectsNew, pull.ProjectsRead-pull.ProjectsNew
	settings.LastSyncChats, settings.LastSyncArtifacts, settings.LastSyncSent = pull.ChatsRead, pull.ArtifactsNew, sent
	return st.SaveSettings(settings)
}
