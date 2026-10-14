package store

import (
	"path/filepath"
	"testing"
)

func TestDirectionsPreserveLegacyAndReverseMappings(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := Settings{SourceOrg: "personal", TargetOrg: "team"}
	if err := s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	state := &State{TargetOrg: "team"}
	p := state.Project("original")
	p.Target = "copy"
	p.Docs["doc-original"] = &ItemState{Status: StatusDone, Target: "doc-copy", SHA: "digest"}
	p.Files["file-original"] = &ItemState{Status: StatusDone, Target: "file-copy", SHA: "bytes"}
	if err := s.SaveState(state); err != nil {
		t.Fatal(err)
	}
	if err := s.ActivateDirection("team", "personal", "Team", "Personal", true); err != nil {
		t.Fatal(err)
	}
	reverse, err := s.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if reverse.Project("copy").Target != "original" || reverse.Project("copy").Docs["doc-copy"].Target != "doc-original" || reverse.Project("copy").Files["file-copy"].SHA != "bytes" {
		t.Fatalf("reverse=%+v", reverse)
	}
	if s.ProfileDir("source") != filepath.Join(s.root, "profiles", "target") {
		t.Fatal("reverse must reuse target login")
	}
	reopened, err := Open(s.Root())
	if err != nil {
		t.Fatal(err)
	}
	if reopened.DataDir() != s.DataDir() {
		t.Fatal("direction lost after restart")
	}
	if err := s.ActivateDirection("personal", "team", "Personal", "Team", false); err != nil {
		t.Fatal(err)
	}
	forward, _ := s.LoadState()
	if forward.Project("original").Docs["doc-original"].Target != "doc-copy" {
		t.Fatal("forward history lost")
	}
}

func TestDirectionsKeepDownloadsAndLocksSeparate(t *testing.T) {
	s, _ := Open(t.TempDir())
	if err := s.ActivateDirection("a", "b", "A", "B", false); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveProject(ProjectMeta{UUID: "only-a", Name: "A"}); err != nil {
		t.Fatal(err)
	}
	release, err := s.AcquireJob("pull")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ActivateDirection("b", "a", "B", "A", true); err != ErrJobRunning {
		t.Fatalf("err=%v", err)
	}
	release()
	if err := s.ActivateDirection("b", "a", "B", "A", true); err != nil {
		t.Fatal(err)
	}
	projects, _ := s.ListProjects()
	if len(projects) != 0 {
		t.Fatal("downloads mixed across directions")
	}
	if err := s.ActivateDirection("a", "b", "A", "B", false); err != nil {
		t.Fatal(err)
	}
	projects, _ = s.ListProjects()
	if len(projects) != 1 {
		t.Fatal("downloads lost")
	}
}
