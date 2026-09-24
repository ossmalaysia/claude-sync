package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChatRoundTrip(t *testing.T) {
	s := open(t)
	if _, ok, err := s.LoadChat("c1"); ok || err != nil {
		t.Fatalf("missing chat: ok=%v err=%v", ok, err)
	}
	rec := ChatRecord{UUID: "c1", Name: "Plan", ProjectUUID: "p1", UpdatedAt: "t1",
		Artifacts: []ArtifactRecord{{ID: "artifact:a1", Kind: "artifact", Title: "T", FileName: "Artifact - T.md", Content: "# x"}}}
	if err := s.SaveChat(rec); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.LoadChat("c1")
	if err != nil || !ok || got.UpdatedAt != "t1" || len(got.Artifacts) != 1 || got.Artifacts[0].Content != "# x" {
		t.Fatalf("got=%+v ok=%v err=%v", got, ok, err)
	}
	all, err := s.ListChats()
	if err != nil || len(all) != 1 {
		t.Fatalf("all=%+v err=%v", all, err)
	}
}

func TestExportArtifactWritesReadableFile(t *testing.T) {
	s := open(t)
	a := ArtifactRecord{FileName: "Artifact - Q3: plan/draft.md", Content: "hello"}
	if err := s.ExportArtifact("Acme: Website", "Kick-off / notes", a); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(s.Root(), "migration", "artifacts-export", "Acme- Website", "Kick-off - notes", "Artifact - Q3- plan-draft.md")
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "hello" {
		t.Fatalf("read %s: %q %v", p, b, err)
	}
}

func TestProjectStateInitialisesArtifacts(t *testing.T) {
	st := &State{}
	st.Project("p").Artifacts["c1/artifact:a1"] = &ItemState{Status: StatusDone}
	if len(st.Projects["p"].Artifacts) != 1 {
		t.Fatal("artifacts map not initialised")
	}
}

func TestSafeNameAvoidsWindowsReservedNames(t *testing.T) {
	for in, want := range map[string]string{"CON": "_CON", "nul.md": "_nul.md", "Com1.txt": "_Com1.txt", "Console.md": "Console.md", "..": "untitled"} {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q)=%q want %q", in, got, want)
		}
	}
}
