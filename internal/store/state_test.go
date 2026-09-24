package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStateRoundTrip(t *testing.T) {
	s := open(t)
	st, err := s.LoadState()
	if err != nil || len(st.Projects) != 0 {
		t.Fatalf("st=%+v err=%v", st, err)
	}
	ps := st.Project("src1")
	ps.Target = "tgt1"
	ps.Docs["d1"] = &ItemState{Status: StatusDone, Target: "td1", SHA: "abc"}
	st.TargetOrg = "org1"
	if err := s.SaveState(st); err != nil {
		t.Fatal(err)
	}
	again, err := s.LoadState()
	if err != nil || again.TargetOrg != "org1" || again.Projects["src1"].Docs["d1"].Target != "td1" {
		t.Fatalf("again=%+v err=%v", again, err)
	}
	entries, _ := os.ReadDir(filepath.Join(s.Root(), "migration"))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestProjectInitialisesMaps(t *testing.T) {
	st := &State{}
	ps := st.Project("x")
	ps.Files["f"] = &ItemState{Status: StatusSkipped}
	if st.Projects["x"] != ps {
		t.Fatal("Project must store the entry")
	}
}

func TestLoadStateCorrupt(t *testing.T) {
	for _, content := range []string{"{not json", ""} {
		s := open(t)
		if err := os.WriteFile(filepath.Join(s.Root(), "migration", "state.json"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := s.LoadState(); !errors.Is(err, ErrCorruptState) {
			t.Errorf("content %q: err=%v, want ErrCorruptState", content, err)
		}
	}
}
