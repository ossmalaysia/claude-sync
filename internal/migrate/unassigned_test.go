package migrate

import (
	"context"
	"errors"
	"testing"

	"github.com/ossmalaysia/claude-sync/internal/store"
)

func unassignedFixture(t *testing.T) (*fakeAPI, *store.Store, *store.State) {
	t.Helper()
	st := newTestStore(t)
	if err := st.SaveChat(store.ChatRecord{UUID: "loose-1", Name: "A conversation", Transcript: "# Chat\nHello", TranscriptAs: "Chat - A conversation.md", Artifacts: []store.ArtifactRecord{{ID: "artifact:a", FileName: "Artifact - a.md", Content: "artifact text"}}}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveChat(store.ChatRecord{UUID: "loose-2", Name: "No artifacts", Transcript: "# Another chat", TranscriptAs: "Chat - No artifacts.md"}); err != nil {
		t.Fatal(err)
	}
	state, err := st.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	return newFakeAPI(), st, state
}

func TestUnassignedOptInPlanPushVerifyAndSync(t *testing.T) {
	api, st, state := unassignedFixture(t)
	plan, err := Plan(st, nil, state, "org", testOpts())
	if err != nil || plan.NewProjects != 0 || plan.NewChats != 0 {
		t.Fatalf("default plan=%+v err=%v", plan, err)
	}
	if err := st.SaveSettings(store.Settings{CopyUnassignedChats: true}); err != nil {
		t.Fatal(err)
	}
	plan, err = Plan(st, nil, state, "org", testOpts())
	if err != nil || plan.NewProjects != 1 || plan.NewChats != 2 || plan.NewArtifacts != 1 {
		t.Fatalf("enabled plan=%+v err=%v", plan, err)
	}
	// An unrelated same-name project must remain untouched.
	unrelated := api.addProject(store.DefaultUnassignedProjectName, "", nil, nil)
	res, err := push(t, api, st, state, nil, testOpts())
	if err != nil || res.CreatedProjects != 1 || res.Chats != 2 || res.Artifacts != 1 {
		t.Fatalf("push=%+v err=%v", res, err)
	}
	ps := state.Projects[store.UnassignedProjectID]
	if ps.Target == unrelated.UUID || ps.Name != store.DefaultUnassignedProjectName || len(api.docs[unrelated.UUID]) != 0 {
		t.Fatalf("destination=%+v", ps)
	}
	for _, p := range api.projects {
		if p.UUID == ps.Target && !p.IsPrivate {
			t.Fatal("destination must be private")
		}
	}
	rows, err := Verify(context.Background(), api, st, state, "org", testOpts(), nil, nil)
	if err != nil || len(rows) != 1 || !rows[0].OK || rows[0].WantDocs != 3 {
		t.Fatalf("verify=%+v err=%v", rows, err)
	}
	before := api.count("CreateDoc")
	res, err = push(t, api, st, state, nil, testOpts())
	if err != nil || res.CreatedProjects != 0 || api.count("CreateDoc") != before {
		t.Fatalf("duplicate push=%+v err=%v", res, err)
	}
	if err := st.SaveChat(store.ChatRecord{UUID: "loose-3", Transcript: "new chat", TranscriptAs: "Chat - New.md"}); err != nil {
		t.Fatal(err)
	}
	res, err = push(t, api, st, state, nil, testOpts())
	if err != nil || res.CreatedProjects != 0 || res.Chats != 1 {
		t.Fatalf("incremental push=%+v err=%v", res, err)
	}
	if err := st.SaveSettings(store.Settings{}); err != nil {
		t.Fatal(err)
	}
	res, err = push(t, api, st, state, nil, testOpts())
	if err != nil || res.Chats != 0 {
		t.Fatalf("disabled push=%+v err=%v", res, err)
	}
	rows, err = Verify(context.Background(), api, st, state, "org", testOpts(), nil, nil)
	if err != nil || len(rows) != 1 || !rows[0].OK {
		t.Fatalf("disabled verify=%+v err=%v", rows, err)
	}
	c, _, err := st.LoadChat("loose-1")
	if err != nil || c.ProjectUUID != "" {
		t.Fatal("source assignment changed")
	}
}

func TestUnassignedResumesWithCustomNameAndArtifactsDisabled(t *testing.T) {
	api, st, state := unassignedFixture(t)
	if err := st.SaveSettings(store.Settings{CopyUnassignedChats: true, UnassignedProjectName: "My earlier chats", SkipArtifacts: true}); err != nil {
		t.Fatal(err)
	}
	api.failNext("CreateDoc", nil, context.Canceled)
	if _, err := push(t, api, st, state, nil, testOpts()); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	reloaded, err := st.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	res, err := push(t, api, st, reloaded, nil, testOpts())
	if err != nil || res.CreatedProjects != 0 || res.Chats != 1 || res.Artifacts != 0 || api.count("CreateProject") != 1 {
		t.Fatalf("resume=%+v err=%v", res, err)
	}
	if api.projects[0].Name != "My earlier chats" {
		t.Fatalf("name=%q", api.projects[0].Name)
	}
	if len(api.docs[reloaded.Projects[store.UnassignedProjectID].Target]) != 2 {
		t.Fatal("incorrect resumed document count")
	}
}

func TestUnassignedDoesNotCreateEmptyProject(t *testing.T) {
	st := newTestStore(t)
	if err := st.SaveSettings(store.Settings{CopyUnassignedChats: true}); err != nil {
		t.Fatal(err)
	}
	state, err := st.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	res, err := push(t, newFakeAPI(), st, state, nil, testOpts())
	if err != nil || res.CreatedProjects != 0 {
		t.Fatalf("push=%+v err=%v", res, err)
	}
}
