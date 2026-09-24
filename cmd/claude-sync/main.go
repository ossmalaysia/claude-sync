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
	switch cmd {
	case "login", "pull", "plan", "push", "verify", "smoke":
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
	if cmd != "login" && *org == "" {
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

func loadSelection(st *store.Store) (map[string]bool, error) {
	projects, err := st.ListProjects()
	if err != nil {
		return nil, err
	}
	existing, err := st.LoadSelection()
	if err != nil {
		return nil, err
	}
	sel := migrate.MergeSelection(projects, existing)
	return sel, st.SaveSelection(sel)
}

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
		fmt.Fprintf(out, "selected %d projects: create %d projects, %d instructions, %d docs, %d files (%.1f MB); retry %d failed; %d already done\n",
			res.SelectedProjects, res.NewProjects, res.NewInstructions, res.NewDocs, res.NewFiles, float64(res.NewBytes)/(1<<20), res.RetryFailed, res.AlreadyDone)
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
		fmt.Fprintf(out, "pulled %d projects, %d docs, %d files (%.1f MB), %d images as WebP preview, %d failed\n", res.Projects, res.Docs, res.Files, float64(res.Bytes)/(1<<20), res.Converted, len(res.Failed))
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
		fmt.Fprintf(out, "created %d projects, %d instructions, %d docs, %d files; %d failed\n", res.CreatedProjects, res.Instructions, res.Docs, res.Files, len(res.Failed))
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
		rows, err := migrate.Verify(ctx, c, st, state, org, opts)
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
		// Record the result where the desktop app reads it.
		settings, err := st.LoadSettings()
		if err != nil {
			return err
		}
		settings.VerifiedAt, settings.VerifyOK, settings.VerifyTotal = time.Now().UTC(), len(rows)-bad, len(rows)
		if err := st.SaveSettings(settings); err != nil {
			return err
		}
		if bad > 0 {
			return fmt.Errorf("%d of %d projects do not match", bad, len(rows))
		}
	case "smoke":
		return runSmoke(ctx, c, org, out)
	}
	return nil
}
