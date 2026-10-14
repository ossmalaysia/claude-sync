package app

import (
	"github.com/ossmalaysia/claude-sync/internal/store"
	"testing"
)

func TestReverseRestoresDirectionsAndResetsReview(t *testing.T) {
	a, _ := newTestApp(t, &fakeSession{handle: defaultHandler})
	if err := a.SetOrg("source", "a", "Personal"); err != nil {
		t.Fatal(err)
	}
	if err := a.SetOrg("target", "b", "Team"); err != nil {
		t.Fatal(err)
	}
	state := &store.State{TargetOrg: "b"}
	state.Project("original").Target = "copied"
	if err := a.st.SaveState(state); err != nil {
		t.Fatal(err)
	}
	if err := a.ReverseDirection(); err != nil {
		t.Fatal(err)
	}
	settings, _ := a.st.LoadSettings()
	if settings.SourceOrg != "b" || settings.TargetOrg != "a" || !settings.CopyReviewedAt.IsZero() {
		t.Fatalf("settings=%+v", settings)
	}
	reverse, _ := a.st.LoadState()
	if reverse.Project("copied").Target != "original" {
		t.Fatal("mapping lost")
	}
	if err := a.SelectDirection("a", "b"); err != nil {
		t.Fatal(err)
	}
	forward, _ := a.st.LoadState()
	if forward.Project("original").Target != "copied" {
		t.Fatal("history lost")
	}
}

func TestMapProjectValidatesTargetAndRejectsDuplicateLink(t *testing.T) {
	a, _ := newTestApp(t, &fakeSession{handle: func(method, path string) (int, string) {
		if path == "/api/organizations/b/projects/existing" {
			return 200, `{"uuid":"existing"}`
		}
		return defaultHandler(method, path)
	}})
	a.SetOrg("source", "a", "A")
	a.SetOrg("target", "b", "B")
	a.st.SaveProject(store.ProjectMeta{UUID: "src", Name: "Same"})
	a.st.SaveProject(store.ProjectMeta{UUID: "other", Name: "Same"})
	if err := a.MapProject("src", "existing"); err != nil {
		t.Fatal(err)
	}
	if err := a.MapProject("other", "existing"); err == nil {
		t.Fatal("two source projects linked to one target")
	}
	if err := a.MapProject("other", "missing"); err == nil {
		t.Fatal("accepted missing target")
	}
	state, _ := a.st.LoadState()
	if state.Project("src").Target != "existing" {
		t.Fatal("mapping not saved")
	}
}
