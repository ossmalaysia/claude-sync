package migrate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
	"github.com/ossmalaysia/claude-sync/internal/store"
)

const slashName = "claude/Carbon Marker — fee proposal (v2.0).md"

// pushFixture: two source projects with the SAME name, one doc with a "/" name.
func pushFixture(t *testing.T) (*fakeAPI, *store.Store, *store.State, map[string]bool) {
	st := newTestStore(t)
	seedProject(t, st, 0, "s1", "Acme: Launch", "Be terse.",
		[]seedDoc{{"d1", slashName, "alpha"}, {"d2", "b.md", "beta"}},
		[]seedFile{{"f1", "intro.pdf", []byte("%PDF-1.4 one")}})
	seedProject(t, st, 1, "s2", "Acme: Launch", "", []seedDoc{{"d3", "c.md", "gamma"}}, nil)
	state, err := st.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	return newFakeAPI(), st, state, map[string]bool{"s1": true, "s2": true}
}

func push(t *testing.T, api *fakeAPI, st *store.Store, state *store.State, sel map[string]bool, opts Options) (PushResult, error) {
	t.Helper()
	return Push(context.Background(), api, st, state, "org", sel, opts, nil)
}

func TestPushCreatesEverything(t *testing.T) {
	api, st, state, sel := pushFixture(t)
	res, err := push(t, api, st, state, sel, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	if res.CreatedProjects != 2 || res.Docs != 3 || res.Files != 1 || res.Instructions != 1 || len(res.Failed) != 0 {
		t.Fatalf("res=%+v", res)
	}
	if len(api.projects) != 2 || api.projects[0].UUID == api.projects[1].UUID {
		t.Fatalf("same-name projects must become two targets: %+v", api.projects)
	}
	t1 := state.Projects["s1"].Target
	if api.projects[0].PromptTemplate != "Be terse." {
		t.Fatalf("instructions not set: %+v", api.projects[0])
	}
	names := map[string]string{}
	for _, d := range api.docs[t1] {
		names[d.FileName] = d.Content
	}
	if names[slashName] != "alpha" || names["b.md"] != "beta" {
		t.Fatalf("target docs=%v", names)
	}
	up := api.files[t1][0]
	if up.FileName != "intro.pdf" || !bytes.Equal(api.blobs[up.FileUUID], []byte("%PDF-1.4 one")) {
		t.Fatalf("file=%+v", up)
	}
	saved, _ := st.LoadState()
	if saved.TargetOrg != "org" || saved.Projects["s1"].Docs["d1"].Status != store.StatusDone || saved.Projects["s1"].Files["f1"].Status != store.StatusDone {
		t.Fatalf("state not persisted: %+v", saved.Projects["s1"])
	}
}

func TestPushIsIdempotent(t *testing.T) {
	api, st, state, sel := pushFixture(t)
	if _, err := push(t, api, st, state, sel, testOpts()); err != nil {
		t.Fatal(err)
	}
	before := len(api.calls)
	res, err := push(t, api, st, state, sel, testOpts())
	if err != nil || res.CreatedProjects != 0 || res.Docs != 0 || res.Files != 0 {
		t.Fatalf("second run res=%+v err=%v", res, err)
	}
	if len(api.calls) != before {
		t.Fatalf("second run made API calls: %v", api.calls[before:])
	}
}

func TestPushResumesAfterCancelWithoutDuplicateProject(t *testing.T) {
	api, st, state, sel := pushFixture(t)
	api.failNext("CreateDoc", nil, context.Canceled) // first doc ok, then the run is interrupted
	if _, err := push(t, api, st, state, sel, testOpts()); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	reloaded, _ := st.LoadState() // resume from disk, as a restarted app would
	if _, err := push(t, api, st, reloaded, sel, testOpts()); err != nil {
		t.Fatal(err)
	}
	if n := api.count("CreateProject"); n != 2 {
		t.Fatalf("CreateProject called %d times, want 2", n)
	}
	if n := len(api.docs[reloaded.Projects["s1"].Target]); n != 2 {
		t.Fatalf("target s1 has %d docs, want 2", n)
	}
}

func TestPushAuthErrorPausesAndSavesState(t *testing.T) {
	api, st, state, sel := pushFixture(t)
	api.failNext("CreateDoc", nil, httpErr(401, claudeapi.ErrAuth))
	_, err := push(t, api, st, state, sel, testOpts())
	if !errors.Is(err, ErrAuthPaused) || !errors.Is(err, claudeapi.ErrAuth) {
		t.Fatalf("err=%v", err)
	}
	saved, _ := st.LoadState()
	ps := saved.Projects["s1"]
	if ps.Target == "" || ps.Docs["b.md"] != nil {
		t.Fatalf("unexpected state %+v", ps)
	}
	done := 0
	for _, it := range ps.Docs {
		if it.Status == store.StatusDone {
			done++
		}
	}
	if done != 1 {
		t.Fatalf("want exactly 1 doc done before the auth error, state=%+v", ps.Docs)
	}
}

func TestPushForbiddenMarksFailedAndContinues(t *testing.T) {
	api, st, state, sel := pushFixture(t)
	api.failNext("CreateProject", httpErr(403, claudeapi.ErrForbidden))
	res, err := push(t, api, st, state, sel, testOpts())
	if err != nil {
		t.Fatalf("policy 403 must not stop the push: %v", err)
	}
	if len(res.Failed) != 1 || res.Failed[0].Item != "project" || res.CreatedProjects != 1 {
		t.Fatalf("res=%+v", res)
	}
	if state.Projects["s1"].Status != store.StatusFailed || state.Projects["s2"].Target == "" {
		t.Fatalf("state=%+v %+v", state.Projects["s1"], state.Projects["s2"])
	}
}

func TestPushRetriesFailedItemsOnNextRun(t *testing.T) {
	api, st, state, sel := pushFixture(t)
	e := httpErr(503, claudeapi.ErrServer)
	api.failNext("CreateDoc", e, e, e, e, e) // all 5 attempts for the first doc fail
	res, err := push(t, api, st, state, sel, testOpts())
	if err != nil || len(res.Failed) != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	var failedID string
	for id, it := range state.Projects["s1"].Docs {
		if it.Status == store.StatusFailed {
			failedID = id
			if it.Attempts != 5 {
				t.Fatalf("attempts=%d", it.Attempts)
			}
		}
	}
	res, err = push(t, api, st, state, sel, testOpts())
	if err != nil || res.Docs != 1 || state.Projects["s1"].Docs[failedID].Status != store.StatusDone {
		t.Fatalf("retry run res=%+v err=%v", res, err)
	}
}

func TestPushSkipsOversizeFiles(t *testing.T) {
	api, st, state, sel := pushFixture(t)
	opts := testOpts()
	opts.MaxFileBytes = 1
	if _, err := push(t, api, st, state, sel, opts); err != nil {
		t.Fatal(err)
	}
	if api.count("UploadFile") != 0 || state.Projects["s1"].Files["f1"].Status != store.StatusSkipped {
		t.Fatalf("file state=%+v", state.Projects["s1"].Files["f1"])
	}
}

func TestPushOnlySelected(t *testing.T) {
	api, st, state, _ := pushFixture(t)
	if _, err := push(t, api, st, state, map[string]bool{"s1": true, "s2": false}, testOpts()); err != nil {
		t.Fatal(err)
	}
	if len(api.projects) != 1 {
		t.Fatalf("created %d projects, want 1", len(api.projects))
	}
}

func TestPushRefusesOtherOrg(t *testing.T) {
	api, st, state, sel := pushFixture(t)
	state.TargetOrg = "someone-else"
	if _, err := push(t, api, st, state, sel, testOpts()); !errors.Is(err, ErrOrgMismatch) {
		t.Fatalf("err=%v", err)
	}
	if len(api.calls) != 0 {
		t.Fatalf("made calls: %v", api.calls)
	}
}

func TestPushStopsWhenWindowClosed(t *testing.T) {
	api, st, state, sel := pushFixture(t)
	api.failNext("CreateDoc", nil, claudeapi.ErrSessionGone)
	res, err := push(t, api, st, state, sel, testOpts())
	if !errors.Is(err, claudeapi.ErrSessionGone) {
		t.Fatalf("err=%v", err)
	}
	if len(res.Failed) != 0 || api.count("CreateDoc") != 2 || api.count("CreateProject") != 1 {
		t.Fatalf("closed window must stop at once without retries: res=%+v calls=%v", res, api.calls)
	}
	saved, _ := st.LoadState()
	for id, it := range saved.Projects["s1"].Docs {
		if it.Status == store.StatusFailed {
			t.Fatalf("doc %s marked failed; it must stay pending for resume", id)
		}
	}
}

var errCrash = errors.New("crash")

// crashAfter returns options whose Sleep "kills the process" on its nth call.
// Every write is followed by exactly one pause, so the nth pause is the
// boundary straight after the nth write, and the stop path's save never runs.
func crashAfter(n int) Options {
	opts := testOpts()
	calls := 0
	opts.Sleep = func(ctx context.Context, _ time.Duration) error {
		calls++
		if calls == n {
			return errCrash
		}
		return ctx.Err()
	}
	return opts
}

func assertTargetComplete(t *testing.T, api *fakeAPI, state *store.State) {
	t.Helper()
	if n := api.count("CreateProject"); n != 2 {
		t.Fatalf("CreateProject called %d times, want 2", n)
	}
	if n := api.count("CreateDoc"); n != 3 {
		t.Fatalf("CreateDoc called %d times, want 3", n)
	}
	if n := api.count("UploadFile"); n != 1 {
		t.Fatalf("UploadFile called %d times, want 1", n)
	}
	if n := api.count("SetInstructions"); n != 1 {
		t.Fatalf("SetInstructions called %d times, want 1", n)
	}
	t1, t2 := state.Projects["s1"].Target, state.Projects["s2"].Target
	if len(api.docs[t1]) != 2 || len(api.docs[t2]) != 1 || len(api.files[t1]) != 1 {
		t.Fatalf("target contents: docs=%v files=%v", api.docs, api.files)
	}
}

func TestPushCrashAfterCreateProjectDoesNotDuplicate(t *testing.T) {
	api, st, state, sel := pushFixture(t)
	if _, err := push(t, api, st, state, sel, crashAfter(1)); !errors.Is(err, errCrash) {
		t.Fatalf("err=%v", err)
	}
	reloaded, err := st.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Projects["s1"] == nil || reloaded.Projects["s1"].Target == "" {
		t.Fatal("the new target UUID must be saved before the next step")
	}
	if _, err := push(t, api, st, reloaded, sel, testOpts()); err != nil {
		t.Fatal(err)
	}
	assertTargetComplete(t, api, reloaded)
}

func TestPushCrashAfterFirstDocDoesNotDuplicate(t *testing.T) {
	api, st, state, sel := pushFixture(t)
	// pauses: 1 CreateProject s1, 2 SetInstructions, 3 first CreateDoc
	if _, err := push(t, api, st, state, sel, crashAfter(3)); !errors.Is(err, errCrash) {
		t.Fatalf("err=%v", err)
	}
	reloaded, _ := st.LoadState()
	if _, err := push(t, api, st, reloaded, sel, testOpts()); err != nil {
		t.Fatal(err)
	}
	assertTargetComplete(t, api, reloaded)
}

// §8: resume after a crash at every step boundary. The fixture makes 7
// writes (2 projects, 1 instructions, 3 docs, 1 file).
func TestPushResumesAfterCrashAtEveryStepBoundary(t *testing.T) {
	for n := 1; n <= 7; n++ {
		t.Run(fmt.Sprintf("after_write_%d", n), func(t *testing.T) {
			api, st, state, sel := pushFixture(t)
			if _, err := push(t, api, st, state, sel, crashAfter(n)); !errors.Is(err, errCrash) {
				t.Fatalf("err=%v", err)
			}
			reloaded, err := st.LoadState()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := push(t, api, st, reloaded, sel, testOpts()); err != nil {
				t.Fatal(err)
			}
			assertTargetComplete(t, api, reloaded)
		})
	}
}

// §8 / §7: after a completed push, a re-pull that adds a doc and a file to an
// already-migrated project makes a re-push create only those two items.
func TestPushAddOnlyResyncPicksUpOnlyNewItems(t *testing.T) {
	api, st, state, sel := pushFixture(t)
	if _, err := push(t, api, st, state, sel, testOpts()); err != nil {
		t.Fatal(err)
	}
	before := len(api.calls)
	seedProject(t, st, 0, "s1", "Acme: Launch", "Be terse. (edited in source)",
		[]seedDoc{{"d1", slashName, "alpha edited"}, {"d2", "b.md", "beta"}, {"d9", "new.md", "new"}},
		[]seedFile{{"f1", "intro.pdf", []byte("%PDF-1.4 one")}, {"f9", "new.pdf", []byte("%PDF-1.4 new")}})
	res, err := push(t, api, st, state, sel, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	if res.CreatedProjects != 0 || res.Instructions != 0 || res.Docs != 1 || res.Files != 1 {
		t.Fatalf("res=%+v", res)
	}
	calls := api.calls[before:]
	if len(calls) != 2 || calls[0] != "CreateDoc:new.md" || calls[1] != "UploadFile:new.pdf" {
		t.Fatalf("re-sync calls=%v", calls)
	}
}

func seedChat(t *testing.T, st *store.Store, uuid, project string, arts ...store.ArtifactRecord) {
	t.Helper()
	if err := st.SaveChat(store.ChatRecord{UUID: uuid, Name: uuid, ProjectUUID: project, UpdatedAt: "t", Artifacts: arts}); err != nil {
		t.Fatal(err)
	}
}

func TestPushAddsProjectArtifactsAsDocs(t *testing.T) {
	api, st, state, sel := pushFixture(t)
	seedChat(t, st, "chat1", "s1", store.ArtifactRecord{ID: "artifact:a1", Kind: "artifact", FileName: "Artifact - Brief.md", Content: "# Brief"})
	seedChat(t, st, "loose", "", store.ArtifactRecord{ID: "artifact:a2", Kind: "artifact", FileName: "Artifact - Idea.md", Content: "idea"})

	res, err := push(t, api, st, state, sel, testOpts())
	if err != nil || res.Artifacts != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	found := false
	for _, d := range api.docs[state.Projects["s1"].Target] {
		if d.FileName == "Artifact - Brief.md" && d.Content == "# Brief" {
			found = true
		}
		if d.FileName == "Artifact - Idea.md" {
			t.Fatal("artifacts from chats outside a project must not be pushed")
		}
	}
	if !found {
		t.Fatalf("artifact doc missing: %+v", api.docs[state.Projects["s1"].Target])
	}
	if it := state.Projects["s1"].Artifacts["chat1/artifact:a1"]; it == nil || it.Status != store.StatusDone {
		t.Fatalf("artifact state=%+v", it)
	}
	before := len(api.calls)
	if res, err := push(t, api, st, state, sel, testOpts()); err != nil || res.Artifacts != 0 || len(api.calls) != before {
		t.Fatalf("second push res=%+v err=%v extra calls=%v", res, err, api.calls[before:])
	}
}

// Artifacts found by a later pull are added to projects pushed earlier.
func TestPushAddsNewArtifactsToAlreadyPushedProject(t *testing.T) {
	api, st, state, sel := pushFixture(t)
	if _, err := push(t, api, st, state, sel, testOpts()); err != nil {
		t.Fatal(err)
	}
	seedChat(t, st, "chat1", "s2", store.ArtifactRecord{ID: "file:/out/r.md", Kind: "file", FileName: "Artifact - r.md", Content: "r"})
	res, err := push(t, api, st, state, sel, testOpts())
	if err != nil || res.Artifacts != 1 || res.CreatedProjects != 0 || api.count("CreateDoc") != 4 {
		t.Fatalf("res=%+v err=%v CreateDoc=%d", res, err, api.count("CreateDoc"))
	}
}
