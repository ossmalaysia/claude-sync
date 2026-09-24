package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSettingsRoundTrip(t *testing.T) {
	s := open(t)
	got, err := s.LoadSettings()
	if err != nil || got.SourceOrg != "" {
		t.Fatalf("empty settings: %+v %v", got, err)
	}
	want := Settings{SourceOrg: "o1", SourceOrgName: "Personal", TargetOrg: "o2", TargetOrgName: "Example Team", VerifyOK: 2, VerifyTotal: 3}
	if err := s.SaveSettings(want); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.LoadSettings(); got != want {
		t.Fatalf("got %+v", got)
	}
}

func TestHasProfile(t *testing.T) {
	s := open(t)
	if s.HasProfile("source") {
		t.Fatal("no profile yet")
	}
	os.MkdirAll(filepath.Join(s.ProfileDir("source"), "Default"), 0o755)
	if !s.HasProfile("source") {
		t.Fatal("profile with content should count")
	}
}

func TestJobLockExcludesSecondJob(t *testing.T) {
	s := open(t)
	release, err := s.AcquireJob("push")
	if err != nil {
		t.Fatal(err)
	}
	if got := s.RunningJob(); got != "push" {
		t.Fatalf("RunningJob=%q", got)
	}
	if _, err := s.AcquireJob("pull"); !errors.Is(err, ErrJobRunning) {
		t.Fatalf("second acquire: %v", err)
	}
	release()
	if got := s.RunningJob(); got != "" {
		t.Fatalf("after release RunningJob=%q", got)
	}
	r, err := s.AcquireJob("pull")
	if err != nil {
		t.Fatalf("re-acquire after release: %v", err)
	}
	r()
}

func TestStaleJobLockIsIgnored(t *testing.T) {
	s := open(t)
	if _, err := s.AcquireJob("pull"); err != nil { // never released: simulates a crash
		t.Fatal(err)
	}
	old := time.Now().Add(-10 * time.Minute)
	os.Chtimes(filepath.Join(s.Root(), "migration", "job.lock"), old, old)
	if got := s.RunningJob(); got != "" {
		t.Fatalf("stale lock must not count as running, got %q", got)
	}
	r, err := s.AcquireJob("push")
	if err != nil {
		t.Fatalf("stale lock should be taken over: %v", err)
	}
	r()
}

func TestTouchJobKeepsLockFresh(t *testing.T) {
	s := open(t)
	if _, err := s.AcquireJob("push"); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-10 * time.Minute)
	lock := filepath.Join(s.Root(), "migration", "job.lock")
	os.Chtimes(lock, old, old)
	s.TouchJob()
	if got := s.RunningJob(); got != "push" {
		t.Fatalf("touched lock should be fresh, got %q", got)
	}
}
