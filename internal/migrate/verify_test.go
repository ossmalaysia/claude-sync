package migrate

import (
	"context"
	"errors"
	"testing"

	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
	"github.com/ossmalaysia/claude-sync/internal/store"
)

func TestVerifyMatchesLocalCopy(t *testing.T) {
	api, st, state, sel := pushFixture(t)
	if err := st.SaveSelection(sel); err != nil {
		t.Fatal(err)
	}
	if _, err := push(t, api, st, state, sel, testOpts()); err != nil {
		t.Fatal(err)
	}
	rows, err := Verify(context.Background(), api, st, state, "org", testOpts(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%+v", rows)
	}
	r := rows[0]
	if !r.OK || r.WantDocs != 2 || r.GotDocs != 2 || r.WantFiles != 1 || r.GotFiles != 1 || r.Name != "Acme: Launch" || !rows[1].OK {
		t.Fatalf("rows=%+v", rows)
	}
}

// A failed doc lowers only the target count: expected counts come from the
// local copy, not from what state marks done.
func TestVerifyFlagsFailedItems(t *testing.T) {
	api, st, state, sel := pushFixture(t)
	st.SaveSelection(sel)
	e := httpErr(400, nil)
	api.failNext("CreateDoc", e)
	api.failNext("UploadFile", e)
	if _, err := push(t, api, st, state, sel, testOpts()); err != nil {
		t.Fatal(err)
	}
	rows, err := Verify(context.Background(), api, st, state, "org", testOpts(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := rows[0]
	if r.OK || r.WantDocs != 2 || r.GotDocs != 1 || r.WantFiles != 1 || r.GotFiles != 0 {
		t.Fatalf("row=%+v", r)
	}
}

func TestVerifyReportsProjectsNeverCreated(t *testing.T) {
	api, st, state, sel := pushFixture(t)
	st.SaveSelection(sel)
	api.failNext("CreateProject", httpErr(403, claudeapi.ErrForbidden))
	if _, err := push(t, api, st, state, sel, testOpts()); err != nil {
		t.Fatal(err)
	}
	rows, err := Verify(context.Background(), api, st, state, "org", testOpts(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("a selected project that failed to create must still get a row: %+v", rows)
	}
	r := rows[0]
	if r.OK || r.SourceUUID != "s1" || r.TargetUUID != "" || r.WantDocs != 2 || r.Error == "" {
		t.Fatalf("row=%+v", r)
	}
	if !rows[1].OK {
		t.Fatalf("row1=%+v", rows[1])
	}
}

func TestVerifyExcludesOversizeFiles(t *testing.T) {
	api, st, state, sel := pushFixture(t)
	st.SaveSelection(sel)
	opts := testOpts()
	opts.MaxFileBytes = 1
	if _, err := push(t, api, st, state, sel, opts); err != nil {
		t.Fatal(err)
	}
	rows, err := Verify(context.Background(), api, st, state, "org", opts, nil, nil)
	if err != nil || !rows[0].OK || rows[0].WantFiles != 0 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
}

func TestVerifyFlagsMismatchAndMissing(t *testing.T) {
	ctx := context.Background()
	api := newFakeAPI()
	tp, _ := api.CreateProject(ctx, "org", claudeapi.NewProject{Name: "A"})
	st := newTestStore(t)
	seedProject(t, st, 0, "s1", "A", "", []seedDoc{{"d1", "a.md", "x"}}, nil)
	seedProject(t, st, 1, "s2", "B", "", nil, nil)
	seedProject(t, st, 2, "s3", "C (not selected)", "", nil, nil)
	st.SaveSelection(map[string]bool{"s1": true, "s2": true, "s3": false})
	state := &store.State{TargetOrg: "org"}
	state.Project("s1").Target = tp.UUID
	state.Project("s1").Docs["d1"] = &store.ItemState{Status: store.StatusDone}
	state.Project("s2").Target = "deleted-in-target"

	rows, err := Verify(ctx, api, st, state, "org", testOpts(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("unselected, unpushed projects are not verified, got %+v", rows)
	}
	if rows[0].OK || rows[0].GotDocs != 0 || rows[0].WantDocs != 1 {
		t.Fatalf("row0=%+v", rows[0])
	}
	if rows[1].OK || rows[1].Error != "project missing in target" {
		t.Fatalf("row1=%+v", rows[1])
	}
}

func TestVerifyStopsWhenWindowClosed(t *testing.T) {
	api, st, state, sel := pushFixture(t)
	st.SaveSelection(sel)
	push(t, api, st, state, sel, testOpts())
	api.failNext("GetProject", claudeapi.ErrSessionGone)
	if _, err := Verify(context.Background(), api, st, state, "org", testOpts(), nil, nil); !errors.Is(err, claudeapi.ErrSessionGone) {
		t.Fatalf("err=%v", err)
	}
}

func TestVerifyRefusesOtherOrg(t *testing.T) {
	_, err := Verify(context.Background(), newFakeAPI(), newTestStore(t), &store.State{TargetOrg: "a"}, "b", testOpts(), nil, nil)
	if !errors.Is(err, ErrOrgMismatch) {
		t.Fatalf("err=%v", err)
	}
}

func TestVerifyExpectsArtifactDocs(t *testing.T) {
	ctx := context.Background()
	api := newFakeAPI()
	tp, _ := api.CreateProject(ctx, "org", claudeapi.NewProject{Name: "A"})
	api.CreateDoc(ctx, "org", tp.UUID, "a.md", "x")
	api.CreateDoc(ctx, "org", tp.UUID, "Artifact - b.md", "y")
	st := newTestStore(t)
	seedProject(t, st, 0, "s1", "A", "", []seedDoc{{"d1", "a.md", "x"}}, nil)
	st.SaveChat(store.ChatRecord{UUID: "c1", ProjectUUID: "s1", Artifacts: []store.ArtifactRecord{{ID: "artifact:b", FileName: "Artifact - b.md", Content: "y"}}})
	st.SaveSelection(map[string]bool{"s1": true})
	state := &store.State{TargetOrg: "org"}
	state.Project("s1").Target = tp.UUID
	rows, err := Verify(ctx, api, st, state, "org", testOpts(), nil, nil)
	if err != nil || len(rows) != 1 || rows[0].WantDocs != 2 || !rows[0].OK {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
}

// A project whose only difference is artifacts not sent yet is reported as
// waiting, with a clear reason, not as an unexplained mismatch.
func TestVerifyExplainsUnsentArtifacts(t *testing.T) {
	ctx := context.Background()
	api := newFakeAPI()
	tp, _ := api.CreateProject(ctx, "org", claudeapi.NewProject{Name: "A"})
	api.CreateDoc(ctx, "org", tp.UUID, "a.md", "x")
	api.CreateDoc(ctx, "org", tp.UUID, "Artifact - b.md", "y") // 1 of 3 artifacts sent
	st := newTestStore(t)
	seedProject(t, st, 0, "s1", "A", "", []seedDoc{{"d1", "a.md", "x"}}, nil)
	st.SaveChat(store.ChatRecord{UUID: "c1", ProjectUUID: "s1", Artifacts: []store.ArtifactRecord{
		{ID: "artifact:b", FileName: "Artifact - b.md", Content: "y"},
		{ID: "artifact:c", FileName: "Artifact - c.md", Content: "z"},
		{ID: "artifact:d", FileName: "Artifact - d.md", Content: "w"}}})
	st.SaveSelection(map[string]bool{"s1": true})
	state := &store.State{TargetOrg: "org"}
	ps := state.Project("s1")
	ps.Target = tp.UUID
	ps.Artifacts["c1/artifact:b"] = &store.ItemState{Status: store.StatusDone}
	var events []Event
	rows, err := Verify(ctx, api, st, state, "org", testOpts(), func(e Event) { events = append(events, e) }, nil)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	r := rows[0]
	if r.OK || r.Waiting != 2 || r.Error != "2 artifacts not sent yet" {
		t.Fatalf("row=%+v", r)
	}
	if len(events) == 0 || events[0].Stage != "verify" || events[0].Total != 1 {
		t.Fatalf("events=%+v", events)
	}
}

// verifyFixture: s1 and s2 sent with one doc each; only s1 still selected.
func verifyFixture(t *testing.T) (*fakeAPI, *store.Store, *store.State) {
	ctx := context.Background()
	api := newFakeAPI()
	st := newTestStore(t)
	state := &store.State{TargetOrg: "org"}
	for i, id := range []string{"s1", "s2"} {
		tp, _ := api.CreateProject(ctx, "org", claudeapi.NewProject{Name: id})
		api.CreateDoc(ctx, "org", tp.UUID, "a.md", "x")
		seedProject(t, st, i, id, id, "", []seedDoc{{"d-" + id, "a.md", "x"}}, nil)
		ps := state.Project(id)
		ps.Target = tp.UUID
		ps.Docs["d-"+id] = &store.ItemState{Status: store.StatusDone}
	}
	st.SaveSelection(map[string]bool{"s1": true, "s2": false})
	return api, st, state
}

func TestVerifyChecksSelectedProjectsAndRecordsResults(t *testing.T) {
	api, st, state := verifyFixture(t)
	rows, err := Verify(context.Background(), api, st, state, "org", testOpts(), nil, nil)
	if err != nil || len(rows) != 1 || rows[0].SourceUUID != "s1" || !rows[0].OK {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	res, _ := st.LoadVerify()
	if r, ok := res["s1"]; !ok || !r.OK || r.CheckedAt.IsZero() {
		t.Fatalf("results=%+v", res)
	}
	if _, ok := res["s2"]; ok {
		t.Fatal("unselected project must not be checked")
	}
}

func TestVerifyOnlyChecksTheGivenProjects(t *testing.T) {
	api, st, state := verifyFixture(t)
	st.SaveSelection(map[string]bool{"s1": true, "s2": true})
	rows, err := Verify(context.Background(), api, st, state, "org", testOpts(), nil, map[string]bool{"s2": true})
	if err != nil || len(rows) != 1 || rows[0].SourceUUID != "s2" {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if n := api.count("GetProject"); n != 1 {
		t.Fatalf("GetProject calls=%d, want 1", n)
	}
}

// Writing to a project invalidates its last check, so "verified" never
// describes content that changed afterwards.
func TestPushClearsCheckOfProjectsItChanges(t *testing.T) {
	api, st, state, sel := pushFixture(t)
	if _, err := push(t, api, st, state, sel, testOpts()); err != nil {
		t.Fatal(err)
	}
	st.RecordVerify(VerifyRows{{SourceUUID: "s1", OK: true}, {SourceUUID: "s2", OK: true}}.toResults())
	seedChat(t, st, "chat1", "s1", store.ArtifactRecord{ID: "artifact:a", FileName: "Artifact - a.md", Content: "a"})
	res, err := push(t, api, st, state, sel, testOpts())
	if err != nil || len(res.Touched) != 1 || res.Touched[0] != "s1" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	checks, _ := st.LoadVerify()
	if _, ok := checks["s1"]; ok {
		t.Fatal("check of the changed project must be cleared")
	}
	if _, ok := checks["s2"]; !ok {
		t.Fatal("check of an untouched project must stay")
	}
}
