package app

import (
	"errors"
	"testing"

	"github.com/ossmalaysia/claude-sync/internal/store"
)

// Nothing reads or writes an account until the user has accepted the notice,
// and the acceptance is remembered.
func TestJobsWaitForTheNotice(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(st, nil, nil)
	if s, _ := a.Status(); s.TermsAccepted {
		t.Fatal("a new install must ask first")
	}
	if _, err := a.Pull("org"); !errors.Is(err, ErrTermsNotAccepted) {
		t.Fatalf("pull err=%v", err)
	}
	if _, err := a.Push("org"); !errors.Is(err, ErrTermsNotAccepted) {
		t.Fatalf("push err=%v", err)
	}
	if err := a.AcceptTerms(); err != nil {
		t.Fatal(err)
	}
	if s, _ := New(st, nil, nil).Status(); !s.TermsAccepted {
		t.Fatal("acceptance must survive a restart")
	}
}

// Changing the notice asks again.
func TestNewNoticeVersionAsksAgain(t *testing.T) {
	st, _ := store.Open(t.TempDir())
	st.SaveSettings(store.Settings{TermsVersion: "0"})
	if s, _ := New(st, nil, nil).Status(); s.TermsAccepted {
		t.Fatal("an older acceptance must not count")
	}
}
