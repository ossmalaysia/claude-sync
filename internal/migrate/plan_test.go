package migrate

import (
	"errors"
	"testing"

	"github.com/ossmalaysia/claude-sync/internal/store"
)

func TestPlanCountsNewWork(t *testing.T) {
	st := newTestStore(t)
	seedProject(t, st, 0, "p1", "Acme: A", "Be terse.",
		[]seedDoc{{"d1", "a.md", "alpha"}, {"d2", "b.md", "beta"}},
		[]seedFile{{"f1", "small.pdf", []byte("12345")}, {"f2", "huge.pdf", make([]byte, 50)}})
	seedProject(t, st, 1, "p2", "Personal: Family", "", []seedDoc{{"d3", "c.md", "x"}}, nil)
	opts := testOpts()
	opts.MaxFileBytes = 10

	res, err := Plan(st, map[string]bool{"p1": true, "p2": false}, &store.State{}, "org", opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.SelectedProjects != 1 || res.NewProjects != 1 || res.NewInstructions != 1 || res.NewDocs != 2 || res.NewFiles != 1 || res.NewBytes != 5 {
		t.Fatalf("res=%+v", res)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].File != "huge.pdf" || res.Skipped[0].SizeBytes != 50 {
		t.Fatalf("skipped=%+v", res.Skipped)
	}
}

func TestPlanAfterPartialPush(t *testing.T) {
	st := newTestStore(t)
	seedProject(t, st, 0, "p1", "A", "", []seedDoc{{"d1", "a.md", "alpha"}, {"d2", "b.md", "beta"}}, nil)
	state := &store.State{TargetOrg: "org"}
	ps := state.Project("p1")
	ps.Target = "t1"
	ps.Docs["d1"] = &store.ItemState{Status: store.StatusDone, SHA: contentSHA("alpha")}
	ps.Docs["d2"] = &store.ItemState{Status: store.StatusFailed}

	res, err := Plan(st, map[string]bool{"p1": true}, state, "org", testOpts())
	if err != nil {
		t.Fatal(err)
	}
	if res.NewProjects != 0 || res.NewDocs != 0 || res.RetryFailed != 1 || res.AlreadyDone != 0 {
		t.Fatalf("res=%+v", res)
	}
	ps.Docs["d2"] = &store.ItemState{Status: store.StatusDone, SHA: contentSHA("beta")}
	res, _ = Plan(st, map[string]bool{"p1": true}, state, "org", testOpts())
	if res.AlreadyDone != 1 || res.RetryFailed != 0 {
		t.Fatalf("after all done res=%+v", res)
	}
}

func TestPlanReportsChangedDocsAndInstructions(t *testing.T) {
	st := newTestStore(t)
	seedProject(t, st, 0, "p1", "A", "new prompt", []seedDoc{{"d1", "a.md", "edited"}}, nil)
	state := &store.State{TargetOrg: "org"}
	ps := state.Project("p1")
	ps.Target = "t1"
	ps.Instructions = &store.ItemState{Status: store.StatusDone, SHA: contentSHA("old prompt")}
	ps.Docs["d1"] = &store.ItemState{Status: store.StatusDone, SHA: contentSHA("original")}

	res, err := Plan(st, map[string]bool{"p1": true}, state, "org", testOpts())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changed) != 2 || res.Changed[0] != "A: instructions" || res.Changed[1] != "A: a.md" {
		t.Fatalf("changed=%v", res.Changed)
	}
}

func TestPlanRefusesOtherOrg(t *testing.T) {
	_, err := Plan(newTestStore(t), map[string]bool{}, &store.State{TargetOrg: "orgA"}, "orgB", testOpts())
	if !errors.Is(err, ErrOrgMismatch) {
		t.Fatalf("err=%v", err)
	}
}
