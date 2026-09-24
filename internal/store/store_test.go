package store

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestListProjectsSortedByOrder(t *testing.T) {
	s := open(t)
	for _, p := range []ProjectMeta{{UUID: "b", Name: "B", Order: 1}, {UUID: "a", Name: "A", Order: 0}, {UUID: "c", Name: "C", Order: 2, PromptTemplate: "x"}} {
		if err := s.SaveProject(p); err != nil {
			t.Fatal(err)
		}
	}
	ps, err := s.ListProjects()
	if err != nil || len(ps) != 3 || ps[0].UUID != "a" || ps[2].PromptTemplate != "x" {
		t.Fatalf("ps=%+v err=%v", ps, err)
	}
}

func TestDocWithSlashInNameStoredByUUID(t *testing.T) {
	s := open(t)
	name := "claude/Carbon Marker — fee proposal (v2.0).md"
	if err := s.SaveDoc("p1", DocRecord{UUID: "d1", FileName: name, Content: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Root(), "migration", "projects", "p1", "docs", "d1.json")); err != nil {
		t.Fatalf("doc not stored by uuid: %v", err)
	}
	docs, err := s.ListDocs("p1")
	if err != nil || len(docs) != 1 || docs[0].FileName != name || docs[0].Content != "x" {
		t.Fatalf("docs=%+v err=%v", docs, err)
	}
}

func TestFileRoundTrip(t *testing.T) {
	s := open(t)
	if s.HasFile("p1", "f1") {
		t.Fatal("HasFile before save")
	}
	data := []byte("%PDF-1.4 binary\x00\xff")
	if err := s.SaveFile("p1", FileMeta{UUID: "f1", FileName: "a.pdf", SizeBytes: int64(len(data)), Mime: "application/pdf"}, data); err != nil {
		t.Fatal(err)
	}
	if !s.HasFile("p1", "f1") {
		t.Fatal("HasFile after save")
	}
	got, err := s.ReadFile("p1", "f1")
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("got=%q err=%v", got, err)
	}
	metas, err := s.ListFiles("p1")
	if err != nil || len(metas) != 1 || metas[0].Mime != "application/pdf" {
		t.Fatalf("metas=%+v err=%v", metas, err)
	}
}

func TestMissingFilesAreEmpty(t *testing.T) {
	s := open(t)
	if m, err := s.LoadMemory(); err != nil || m != "" {
		t.Fatalf("memory=%q err=%v", m, err)
	}
	if sel, err := s.LoadSelection(); err != nil || sel == nil || len(sel) != 0 {
		t.Fatalf("sel=%v err=%v", sel, err)
	}
	if docs, err := s.ListDocs("nope"); err != nil || len(docs) != 0 {
		t.Fatalf("docs=%v err=%v", docs, err)
	}
}

func TestRejectsUnsafeIDs(t *testing.T) {
	s := open(t)
	if err := s.SaveProject(ProjectMeta{UUID: "../x"}); err == nil {
		t.Error("SaveProject accepted ../x")
	}
	if err := s.SaveDoc("p1", DocRecord{UUID: `a\b`}); err == nil {
		t.Error("SaveDoc accepted a\\b")
	}
	if err := s.SaveFile("p/1", FileMeta{UUID: "f"}, nil); err == nil {
		t.Error("SaveFile accepted p/1")
	}
}

func TestProfileDir(t *testing.T) {
	s := open(t)
	if got := s.ProfileDir("target"); got != filepath.Join(s.Root(), "profiles", "target") {
		t.Fatalf("got %s", got)
	}
}

func TestWriteFileAtomicReplacesAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.json")
	if err := WriteFileAtomic(p, []byte("old")); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(p, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "new" {
		t.Fatalf("content=%q", b)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

func TestWriteFileAtomicFailureKeepsOldFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "state.json")
	if err := WriteFileAtomic(p, []byte("old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil { // no temp file can be created
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)
	if err := WriteFileAtomic(p, []byte("new")); err == nil {
		t.Skip("filesystem ignores directory permissions")
	}
	if b, _ := os.ReadFile(p); string(b) != "old" {
		t.Fatalf("old file damaged: %q", b)
	}
}

func TestLeftoverTempFilesAreIgnored(t *testing.T) {
	s := open(t)
	if err := s.SaveDoc("p1", DocRecord{UUID: "d1", FileName: "a.md", Content: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveFile("p1", FileMeta{UUID: "f1", FileName: "a.pdf"}, []byte("x")); err != nil {
		t.Fatal(err)
	}
	// A crash mid-write leaves temp files: half-written JSON in both dirs.
	for _, sub := range []string{"docs", "files"} {
		d := filepath.Join(s.Root(), "migration", "projects", "p1", sub)
		for _, name := range []string{".tmp-123456", ".tmp-789.json"} {
			if err := os.WriteFile(filepath.Join(d, name), []byte(`{"uuid":"half`), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	docs, err := s.ListDocs("p1")
	if err != nil || len(docs) != 1 {
		t.Fatalf("docs=%+v err=%v", docs, err)
	}
	files, err := s.ListFiles("p1")
	if err != nil || len(files) != 1 {
		t.Fatalf("files=%+v err=%v", files, err)
	}
}

// Manifests written before total/complete existed describe a finished pull.
func TestLoadManifestUpgradesLegacyCompletePull(t *testing.T) {
	s := open(t)
	legacy := `{"source_org":"o","pulled_at":"2026-09-24T14:40:00Z","projects":172,"docs":159,"files":157}`
	if err := os.WriteFile(filepath.Join(s.Root(), "migration", "manifest.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := s.LoadManifest()
	if err != nil || !m.Complete || m.Total != 172 {
		t.Fatalf("m=%+v err=%v", m, err)
	}
}
