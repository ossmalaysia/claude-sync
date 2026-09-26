package app

import (
	"testing"

	"github.com/ossmalaysia/claude-sync/internal/store"
)

func TestGuideUsesRealNamesAndOpensOnlyTargetProjects(t *testing.T) {
	a, _ := newTestApp(t, &fakeSession{handle: defaultHandler})
	a.st.SaveProject(store.ProjectMeta{UUID: "s1", Name: "Acme: Launch"})
	a.st.SaveChat(store.ChatRecord{UUID: "c1", ProjectUUID: "s1", UpdatedAt: "t", TranscriptAs: "Chat - Plan (2026-08-01).md",
		Artifacts: []store.ArtifactRecord{{ID: "artifact:a", FileName: "Artifact - deck.html"}}})
	st := &store.State{TargetOrg: "team"}
	ps := st.Project("s1")
	ps.Target = "019fda13-5609-700e-bab1-35ff9ed78f21"
	ps.Artifacts["c1/artifact:a"] = &store.ItemState{Status: store.StatusDone}
	ps.Chats["c1"] = &store.ItemState{Status: store.StatusDone}
	st.Project("s2").Target = "not-a-uuid; rm -rf"
	a.st.SaveState(st)

	g, err := a.Guide()
	if err != nil || g.Example == nil || g.Example.Name != "Acme: Launch" || g.Example.Artifact != "Artifact - deck.html" || g.Example.Chat != "Chat - Plan (2026-08-01).md" {
		t.Fatalf("guide=%+v example=%+v err=%v", g, g.Example, err)
	}
	var opened []string
	a.openURL = func(u string) error { opened = append(opened, u); return nil }
	if err := a.OpenTargetProject("s1"); err != nil {
		t.Fatal(err)
	}
	if err := a.OpenTargetProject("s2"); err == nil {
		t.Fatal("a malformed target id must not be opened")
	}
	if len(opened) != 1 || opened[0] != "https://claude.ai/project/019fda13-5609-700e-bab1-35ff9ed78f21" {
		t.Fatalf("opened=%v", opened)
	}
}
