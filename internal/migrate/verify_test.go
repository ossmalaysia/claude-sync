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
	rows, err := Verify(context.Background(), api, st, state, "org", testOpts())
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
	rows, err := Verify(context.Background(), api, st, state, "org", testOpts())
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
	rows, err := Verify(context.Background(), api, st, state, "org", testOpts())
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
	rows, err := Verify(context.Background(), api, st, state, "org", opts)
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

	rows, err := Verify(ctx, api, st, state, "org", testOpts())
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
	if _, err := Verify(context.Background(), api, st, state, "org", testOpts()); !errors.Is(err, claudeapi.ErrSessionGone) {
		t.Fatalf("err=%v", err)
	}
}

func TestVerifyRefusesOtherOrg(t *testing.T) {
	_, err := Verify(context.Background(), newFakeAPI(), newTestStore(t), &store.State{TargetOrg: "a"}, "b", testOpts())
	if !errors.Is(err, ErrOrgMismatch) {
		t.Fatalf("err=%v", err)
	}
}
