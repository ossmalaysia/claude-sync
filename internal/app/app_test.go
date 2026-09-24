package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
	"github.com/ossmalaysia/claude-sync/internal/store"
)

const orgsJSON = `[{"uuid":"personal","name":"sam@example.com's Organization","capabilities":["chat"]},
{"uuid":"api","name":"Example API Org","capabilities":["api"]},
{"uuid":"team","name":"Example Team","capabilities":["raven","chat"]}]`

type fakeSession struct {
	mu       sync.Mutex
	loginErr error
	closed   bool
	handle   func(method, path string) (int, string)
	fail     func(method, path string) error // optional transport error
}

func (s *fakeSession) JSON(_ context.Context, method, path string, _ any) (int, []byte, error) {
	if s.fail != nil {
		if err := s.fail(method, path); err != nil {
			return 0, nil, err
		}
	}
	st, body := s.handle(method, path)
	return st, []byte(body), nil
}
func (s *fakeSession) Download(context.Context, string) (int, []byte, error) { return 404, nil, nil }
func (s *fakeSession) Upload(_ context.Context, path, _, _ string, _ []byte) (int, []byte, error) {
	st, body := s.handle("UPLOAD", path)
	return st, []byte(body), nil
}
func (s *fakeSession) WaitLoggedIn(context.Context, time.Duration) error { return s.loginErr }
func (s *fakeSession) Close()                                            { s.mu.Lock(); s.closed = true; s.mu.Unlock() }

func defaultHandler(method, path string) (int, string) {
	switch {
	case path == "/api/organizations":
		return 200, orgsJSON
	case method == "POST" && strings.HasSuffix(path, "/projects"):
		return 201, `{"uuid":"tp1","name":"x"}`
	}
	return 404, `{}`
}

func newTestApp(t *testing.T, sess *fakeSession) (*App, *int) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	opens := 0
	a := New(st, func(string) (Session, error) { opens++; return sess, nil }, nil)
	a.opts.Sleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	a.opts.WritePause = 0
	return a, &opens
}

func TestSuggestOrg(t *testing.T) {
	orgs := []claudeapi.Org{
		{UUID: "personal", Capabilities: []string{"chat"}},
		{UUID: "api", Capabilities: []string{"api"}},
		{UUID: "team", Capabilities: []string{"raven", "chat"}},
	}
	if got := SuggestOrg(orgs, "target"); got != "team" {
		t.Errorf("target: %s", got)
	}
	if got := SuggestOrg(orgs, "source"); got != "personal" {
		t.Errorf("source: %s", got)
	}
	if got := SuggestOrg(orgs[1:2], "source"); got != "" {
		t.Errorf("no chat org: %q", got)
	}
}

func TestConnectAccountRejectsUnknownAccount(t *testing.T) {
	a, _ := newTestApp(t, &fakeSession{handle: defaultHandler})
	if _, err := a.ConnectAccount("other"); err == nil {
		t.Fatal("want error")
	}
}

func TestConnectAccountReturnsOrgs(t *testing.T) {
	a, _ := newTestApp(t, &fakeSession{handle: defaultHandler})
	info, err := a.ConnectAccount("target")
	if err != nil || len(info.Orgs) != 3 || info.SuggestedOrg != "team" || info.Account != "target" {
		t.Fatalf("info=%+v err=%v", info, err)
	}
}

func TestConnectAccountLoginFailureDropsSession(t *testing.T) {
	sess := &fakeSession{handle: defaultHandler, loginErr: errors.New("browser window was closed")}
	a, opens := newTestApp(t, sess)
	if _, err := a.ConnectAccount("source"); err == nil {
		t.Fatal("want error")
	}
	sess.loginErr = nil
	if _, err := a.ConnectAccount("source"); err != nil {
		t.Fatal(err)
	}
	if *opens != 2 || !sess.closed {
		t.Fatalf("opens=%d closed=%v: a failed login must close and reopen the browser", *opens, sess.closed)
	}
}

func TestPullNeedsSourceOrg(t *testing.T) {
	a, _ := newTestApp(t, &fakeSession{handle: defaultHandler})
	if _, err := a.Pull(""); err == nil || !strings.Contains(err.Error(), "choose the source org") {
		t.Fatalf("err=%v", err)
	}
}

// Jobs connect on their own using the saved browser profile; there is no
// separate "connect" step to repeat after restarting the app.
func TestJobsAutoConnectWithSavedLogin(t *testing.T) {
	a, opens := newTestApp(t, &fakeSession{handle: defaultHandler})
	seedOne(t, a)
	if err := a.SetOrg("target", "team", "Example Team"); err != nil {
		t.Fatal(err)
	}
	out, err := a.Push("")
	if err != nil || out.Status != "completed" || out.Result.CreatedProjects != 1 || *opens != 1 {
		t.Fatalf("out=%+v err=%v opens=%d", out, err, *opens)
	}
}

func TestSetOrgRejectsUnknownAccount(t *testing.T) {
	a, _ := newTestApp(t, &fakeSession{handle: defaultHandler})
	if err := a.SetOrg("other", "x", "y"); err == nil {
		t.Fatal("want error")
	}
}

func TestGetSelectionAppliesDefaults(t *testing.T) {
	a, _ := newTestApp(t, &fakeSession{handle: defaultHandler})
	a.st.SaveProject(store.ProjectMeta{UUID: "p1", Name: "Personal: Family", Order: 0})
	a.st.SaveProject(store.ProjectMeta{UUID: "p2", Name: "Acme: X", Order: 1})
	a.st.SaveDoc("p2", store.DocRecord{UUID: "d1", FileName: "a.md"})
	rows, err := a.GetSelection()
	if err != nil || len(rows) != 2 || rows[0].Selected || !rows[1].Selected || rows[1].Docs != 1 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	saved, _ := a.st.LoadSelection()
	if saved["p1"] || !saved["p2"] {
		t.Fatalf("selection not saved: %v", saved)
	}
}

func seedOne(t *testing.T, a *App) {
	a.st.SaveProject(store.ProjectMeta{UUID: "s1", Name: "Acme: X"})
	a.st.SaveDoc("s1", store.DocRecord{UUID: "d1", FileName: "a.md", Content: "x"})
	a.st.SaveSelection(map[string]bool{"s1": true})
}

func TestPushNeedsLoginStatus(t *testing.T) {
	sess := &fakeSession{handle: func(method, path string) (int, string) {
		if method == "POST" {
			return 401, `{}`
		}
		return defaultHandler(method, path)
	}}
	a, _ := newTestApp(t, sess)
	seedOne(t, a)
	if _, err := a.ConnectAccount("target"); err != nil {
		t.Fatal(err)
	}
	out, err := a.Push("team")
	if err != nil || out.Status != "needs_login" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestStopJobPausesPushAndStatusShowsResume(t *testing.T) {
	a, _ := newTestApp(t, &fakeSession{handle: defaultHandler})
	seedOne(t, a)
	a.st.SaveManifest(store.Manifest{SourceOrg: "personal", Total: 1, Complete: true})
	a.SetOrg("target", "team", "Example Team")
	// The first pause after a write triggers StopJob, as a user click would.
	a.opts.Sleep = func(ctx context.Context, _ time.Duration) error {
		a.StopJob()
		<-ctx.Done()
		return ctx.Err()
	}
	out, err := a.Push("")
	if err != nil || out.Status != "paused" || out.Result.CreatedProjects != 1 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	s, err := a.Status()
	if err != nil || s.Job != "" || s.Pushed != 1 || s.PendingItems != 1 || s.Next != "push" {
		t.Fatalf("status=%+v err=%v", s, err)
	}
}

func TestStatusFreshStore(t *testing.T) {
	a, _ := newTestApp(t, &fakeSession{handle: defaultHandler})
	s, err := a.Status()
	if err != nil || s.Next != "connect_source" || s.Source.SavedLogin || s.PullDone != 0 {
		t.Fatalf("status=%+v err=%v", s, err)
	}
}

func TestStatusReportsPartialPull(t *testing.T) {
	a, _ := newTestApp(t, &fakeSession{handle: defaultHandler})
	a.st.SaveManifest(store.Manifest{SourceOrg: "personal", Total: 5})
	a.st.SaveProject(store.ProjectMeta{UUID: "p1", Name: "A"})
	a.st.SaveProject(store.ProjectMeta{UUID: "p2", Name: "B", Order: 1})
	s, err := a.Status()
	if err != nil || s.Source.Org != "personal" || s.PullDone != 2 || s.PullTotal != 5 || s.PullComplete || s.Next != "pull" {
		t.Fatalf("status=%+v err=%v", s, err)
	}
}

func TestSecondJobIsRefused(t *testing.T) {
	a, _ := newTestApp(t, &fakeSession{handle: defaultHandler})
	seedOne(t, a)
	a.SetOrg("target", "team", "Example Team")
	release, err := a.st.AcquireJob("pull") // e.g. a CLI pull in another process
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := a.Push(""); !errors.Is(err, store.ErrJobRunning) {
		t.Fatalf("err=%v", err)
	}
	s, _ := a.Status()
	if s.Job != "pull" || s.JobHere || s.Next != "wait" {
		t.Fatalf("status=%+v", s)
	}
}

func TestFullFlowReachesDone(t *testing.T) {
	sess := &fakeSession{handle: func(method, path string) (int, string) {
		switch {
		case method == "POST" && strings.HasSuffix(path, "/docs"):
			return 201, `{"uuid":"td1"}`
		case method == "GET" && strings.HasSuffix(path, "/projects/tp1"):
			return 200, `{"uuid":"tp1","docs_count":1,"files_count":0}`
		}
		return defaultHandler(method, path)
	}}
	a, _ := newTestApp(t, sess)
	seedOne(t, a)
	a.st.SaveManifest(store.Manifest{SourceOrg: "personal", Total: 1, Complete: true})
	a.SetOrg("target", "team", "Example Team")
	if out, err := a.Push(""); err != nil || out.Status != "completed" {
		t.Fatalf("push out=%+v err=%v", out, err)
	}
	if s, _ := a.Status(); s.Next != "verify" || s.Pushed != 1 || s.PendingItems != 0 {
		t.Fatalf("after push status=%+v", s)
	}
	rows, err := a.Verify("")
	if err != nil || len(rows) != 1 || !rows[0].OK {
		t.Fatalf("verify rows=%+v err=%v", rows, err)
	}
	if s, _ := a.Status(); s.Next != "done" || s.VerifyOK != 1 || s.VerifyTotal != 1 {
		t.Fatalf("after verify status=%+v", s)
	}
}

func TestNextAction(t *testing.T) {
	base := Status{Source: AccountStatus{Org: "s"}, Target: AccountStatus{Org: "t"}, PullComplete: true, Selected: 3, Pushed: 3, VerifiedAt: "x"}
	cases := []struct {
		name string
		mod  func(*Status)
		want string
	}{
		{"running", func(s *Status) { s.Job = "push" }, "wait"},
		{"no source", func(s *Status) { s.Source.Org = "" }, "connect_source"},
		{"partial pull", func(s *Status) { s.PullComplete = false }, "pull"},
		{"nothing selected", func(s *Status) { s.Selected = 0 }, "select"},
		{"no target", func(s *Status) { s.Target.Org = "" }, "connect_target"},
		{"pending", func(s *Status) { s.PendingItems = 2 }, "push"},
		{"failed", func(s *Status) { s.Failed = 1 }, "push"},
		{"never verified", func(s *Status) { s.VerifiedAt = "" }, "verify"},
		{"verify stale", func(s *Status) { s.VerifyStale = true }, "verify"},
		{"all good", func(s *Status) {}, "done"},
	}
	for _, c := range cases {
		s := base
		c.mod(&s)
		if got := nextAction(s); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestProgressEventsAreEmitted(t *testing.T) {
	st, _ := store.Open(t.TempDir())
	var mu sync.Mutex
	var events []string
	a := New(st, func(string) (Session, error) { return &fakeSession{handle: defaultHandler}, nil },
		func(_ context.Context, name string, _ any) { mu.Lock(); events = append(events, name); mu.Unlock() })
	a.opts.Sleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	seedOne(t, a)
	a.ConnectAccount("target")
	a.Push("team")
	if len(events) == 0 || events[0] != "progress" {
		t.Fatalf("events=%v", events)
	}
}

func TestPushWindowClosedNeedsLoginAndReopensWindow(t *testing.T) {
	sess := &fakeSession{handle: defaultHandler, fail: func(method, _ string) error {
		if method == "POST" {
			return claudeapi.ErrSessionGone
		}
		return nil
	}}
	a, opens := newTestApp(t, sess)
	seedOne(t, a)
	if _, err := a.ConnectAccount("target"); err != nil {
		t.Fatal(err)
	}
	out, err := a.Push("team")
	if err != nil || out.Status != "needs_login" || len(out.Result.Failed) != 0 {
		t.Fatalf("a closed window must stop the push, not fail every item: out=%+v err=%v", out, err)
	}
	if !sess.closed {
		t.Fatal("dead session must be dropped")
	}
	sess.fail = nil
	if _, err := a.ConnectAccount("target"); err != nil {
		t.Fatal(err)
	}
	if *opens != 2 {
		t.Fatalf("opens=%d: reconnect must open a fresh window", *opens)
	}
	out, err = a.Push("team")
	if err != nil || out.Status != "completed" || out.Result.CreatedProjects != 1 {
		t.Fatalf("resume out=%+v err=%v", out, err)
	}
}

// Orgs remembered from older data have no name; the first job that opens
// the account fills it in so the home screen shows names, not ids.
func TestJobFillsMissingOrgName(t *testing.T) {
	a, _ := newTestApp(t, &fakeSession{handle: defaultHandler})
	seedOne(t, a)
	a.SetOrg("target", "team", "")
	if _, err := a.Push(""); err != nil {
		t.Fatal(err)
	}
	if s, _ := a.Status(); s.Target.OrgName != "Example Team" {
		t.Fatalf("org name=%q", s.Target.OrgName)
	}
}
