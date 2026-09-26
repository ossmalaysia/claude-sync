package app

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ossmalaysia/claude-sync/internal/store"
)

// unreview puts the app back to the state before "What to copy" was saved.
func unreview(t *testing.T, a *App) {
	t.Helper()
	s, err := a.st.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	s.CopyReviewedAt, s.PersonalChoice, s.ChatChoice = time.Time{}, "", ""
	if err := a.st.SaveSettings(s); err != nil {
		t.Fatal(err)
	}
}

// seedCopy is a pulled source with something of every kind.
func seedCopy(t *testing.T, a *App) {
	t.Helper()
	seedOne(t, a) // s1 "Acme: X"
	a.st.SaveProject(store.ProjectMeta{UUID: "s2", Name: "Travel 2026", Order: 1})
	a.st.SaveProject(store.ProjectMeta{UUID: "s3", Name: "Personal: Diary", Order: 2})
	a.st.SaveChat(store.ChatRecord{UUID: "c1", ProjectUUID: "s1", UpdatedAt: "t", Transcript: "# Hi", TranscriptAs: "Chat - Hi.md",
		Artifacts: []store.ArtifactRecord{{ID: "artifact:a", FileName: "Artifact - a.md", Content: "a"}, {ID: "artifact:b", FileName: "Artifact - b.md", Content: "b"}}})
	a.st.SaveChat(store.ChatRecord{UUID: "c2", ProjectUUID: "s3", UpdatedAt: "t", Transcript: "# Dear diary", TranscriptAs: "Chat - Dear diary.md"})
	a.st.SaveChat(store.ChatRecord{UUID: "c3", UpdatedAt: "t", Transcript: "# Loose", TranscriptAs: "Chat - Loose.md",
		Artifacts: []store.ArtifactRecord{{ID: "artifact:c", FileName: "Artifact - c.md", Content: "c"}}})
	a.st.SaveSkill(store.SkillMeta{ID: "skill_q", Name: "quotation"}, []byte("PK"))
	a.st.SaveMemory("**Work context**")
	a.SetOrg("target", "team", "Example Team")
}

func TestCopySettingsDefaultsBeforeReview(t *testing.T) {
	a, _ := newTestApp(t, &fakeSession{handle: defaultHandler})
	unreview(t, a)
	seedCopy(t, a)
	v, err := a.GetCopySettings()
	if err != nil {
		t.Fatal(err)
	}
	want := CopyView{
		Settings: CopySettings{Artifacts: true, Chats: false, Skills: true, Memory: true, Personal: false},
		Counts: CopyCounts{Projects: 3, Selected: 1, Artifacts: 2, ArtifactsLocal: 1, Chats: 2, ChatsLocal: 1, Skills: 1, Memory: true,
			PersonalProjects: []string{"Personal: Diary", "Travel 2026"}},
	}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("got  %+v\nwant %+v", v, want)
	}
	if s, _ := a.Status(); s.CopyReviewed {
		t.Fatal("status must say the page was not reviewed")
	}
}

func TestSaveCopySettingsRoundTripAndSelection(t *testing.T) {
	a, _ := newTestApp(t, &fakeSession{handle: defaultHandler})
	unreview(t, a)
	seedCopy(t, a)
	in := CopySettings{Artifacts: false, Chats: true, Skills: false, Memory: false, Personal: true}
	if err := a.SaveCopySettings(in); err != nil {
		t.Fatal(err)
	}
	v, err := a.GetCopySettings()
	if err != nil || !v.Reviewed || v.Settings != in || v.Counts.Selected != 3 {
		t.Fatalf("view=%+v err=%v", v, err)
	}
	sel, _ := a.st.LoadSelection()
	if !sel["s1"] || !sel["s2"] || !sel["s3"] {
		t.Fatalf("personal projects must be selected: %v", sel)
	}
	s, _ := a.Status()
	if !s.CopyReviewed || !s.SkipArtifacts || !s.SkipSkills || !s.SkipMemory || s.ChatChoice != store.ChatsInclude || s.PersonalChoice != store.PersonalInclude || s.PersonalSkipped != 0 {
		t.Fatalf("status=%+v", s)
	}
	if s.MemoryPending || s.Next == "memory" {
		t.Fatalf("memory switched off must not be pending: %+v", s)
	}

	// Turning personal projects off again unticks them.
	in.Personal = false
	if err := a.SaveCopySettings(in); err != nil {
		t.Fatal(err)
	}
	if sel, _ := a.st.LoadSelection(); !sel["s1"] || sel["s2"] || sel["s3"] {
		t.Fatalf("selection=%v", sel)
	}
	// Saving again with the same personal choice keeps projects picked by hand.
	sel, _ = a.st.LoadSelection()
	sel["s2"] = true
	a.st.SaveSelection(sel)
	in.Artifacts = true
	if err := a.SaveCopySettings(in); err != nil {
		t.Fatal(err)
	}
	if sel, _ := a.st.LoadSelection(); !sel["s2"] {
		t.Fatalf("a project ticked by hand was unticked: %v", sel)
	}
}

// Nothing is sent until "What to copy" has been saved once.
func TestPushWaitsForWhatToCopy(t *testing.T) {
	a, _ := newTestApp(t, &fakeSession{handle: defaultHandler})
	unreview(t, a)
	seedOne(t, a)
	a.SetOrg("target", "team", "Example Team")
	if _, err := a.Push(""); !errors.Is(err, ErrReviewCopy) {
		t.Fatalf("err=%v", err)
	}
	if err := a.SaveCopySettings(CopySettings{Artifacts: true, Skills: true, Memory: true}); err != nil {
		t.Fatal(err)
	}
	if out, err := a.Push(""); err != nil || out.Status != "completed" || out.Result.CreatedProjects != 1 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

// Scan & sync downloads, then stops before sending until the page is saved.
func TestSyncChangesWaitsForWhatToCopy(t *testing.T) {
	sess, created := syncWorld()
	a, _ := newTestApp(t, sess)
	unreview(t, a)
	a.SetOrg("source", "src", "Personal")
	a.SetOrg("target", "team", "Example Team")
	out, err := a.SyncChanges()
	if !errors.Is(err, ErrReviewCopy) || out.Pull.ProjectsNew != 1 || *created != 0 {
		t.Fatalf("out=%+v err=%v created=%d", out, err, *created)
	}
}

// With nothing pulled there is nothing to review, so push is not refused.
func TestPushWithoutProjectsNeedsNoReview(t *testing.T) {
	a, _ := newTestApp(t, &fakeSession{handle: defaultHandler})
	unreview(t, a)
	a.SetOrg("target", "team", "Example Team")
	if out, err := a.Push(""); err != nil || out.Status != "completed" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

// Chats are sent only when switched on, and the switch can be changed later.
func TestChatsFollowWhatToCopy(t *testing.T) {
	sess := &fakeSession{handle: func(method, path string) (int, string) {
		if method == "POST" && strings.HasSuffix(path, "/docs") {
			return 201, `{"uuid":"td"}`
		}
		return defaultHandler(method, path)
	}}
	a, _ := newTestApp(t, sess)
	seedOne(t, a)
	a.st.SaveChat(store.ChatRecord{UUID: "c1", ProjectUUID: "s1", UpdatedAt: "t", Transcript: "# Hi", TranscriptAs: "Chat - Hi.md"})
	a.SetOrg("target", "team", "Example Team")
	if out, err := a.Push(""); err != nil || out.Result.Chats != 0 {
		t.Fatalf("chats off: out=%+v err=%v", out, err)
	}
	if err := a.SaveCopySettings(CopySettings{Artifacts: true, Chats: true, Skills: true, Memory: true}); err != nil {
		t.Fatal(err)
	}
	if st, _ := a.Status(); st.ChatsInProjects != 1 || st.ChatsPending != 1 {
		t.Fatalf("status=%+v", st)
	}
	if out, err := a.Push(""); err != nil || out.Result.Chats != 1 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if st, _ := a.Status(); st.ChatsSent != 1 || st.ChatsPending != 0 {
		t.Fatalf("status=%+v", st)
	}
}

// Memory switched off is never sent, and the browser is not opened for it.
func TestSyncMemorySkippedWhenSwitchedOff(t *testing.T) {
	sess := &fakeSession{handle: func(method, path string) (int, string) {
		if strings.HasSuffix(path, "/melange/import_external") {
			t.Error("memory was sent")
		}
		return defaultHandler(method, path)
	}}
	a, opens := newTestApp(t, sess)
	a.SetOrg("target", "team", "Example Team")
	a.st.SaveMemory("**Work context**")
	if err := a.SaveCopySettings(CopySettings{Artifacts: true, Skills: true, Memory: false}); err != nil {
		t.Fatal(err)
	}
	if out, err := a.SyncMemory(); err != nil || out.Status != "skipped" || *opens != 0 {
		t.Fatalf("out=%+v err=%v opens=%d", out, err, *opens)
	}
}

func TestSyncChangesSkipsMemoryWhenSwitchedOff(t *testing.T) {
	sess, _ := syncWorld()
	a, _ := newTestApp(t, sess)
	a.SetOrg("source", "src", "Personal")
	a.SetOrg("target", "team", "Example Team")
	if err := a.SaveCopySettings(CopySettings{Artifacts: true, Skills: true, Memory: false}); err != nil {
		t.Fatal(err)
	}
	out, err := a.SyncChanges()
	if err != nil || out.Status != "completed" || out.Memory != "skipped" || out.Push.CreatedProjects != 1 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

// Someone upgrading sees their earlier answers about personal projects and
// chats on the page, not the defaults.
func TestCopySettingsKeepEarlierAnswers(t *testing.T) {
	a, _ := newTestApp(t, &fakeSession{handle: defaultHandler})
	unreview(t, a)
	seedCopy(t, a)
	s, _ := a.st.LoadSettings()
	s.PersonalChoice, s.ChatChoice = store.PersonalInclude, store.ChatsInclude
	a.st.SaveSettings(s)
	v, err := a.GetCopySettings()
	if err != nil || v.Reviewed || !v.Settings.Personal || !v.Settings.Chats || !v.Settings.Artifacts || !v.Settings.Skills || !v.Settings.Memory {
		t.Fatalf("view=%+v err=%v", v, err)
	}
}

// A personal project ticked by hand during the first review stays ticked when
// the page is saved with personal projects off.
func TestFirstSaveKeepsPersonalProjectPickedByHand(t *testing.T) {
	a, _ := newTestApp(t, &fakeSession{handle: defaultHandler})
	unreview(t, a)
	seedCopy(t, a)
	a.st.SaveSelection(map[string]bool{"s1": true, "s2": true, "s3": false})
	if err := a.SaveCopySettings(CopySettings{Artifacts: true, Skills: true, Memory: true}); err != nil {
		t.Fatal(err)
	}
	if sel, _ := a.st.LoadSelection(); !sel["s1"] || !sel["s2"] || sel["s3"] {
		t.Fatalf("selection=%v", sel)
	}
	if s, _ := a.Status(); s.PersonalChoice != store.PersonalSkip || s.PersonalSkipped != 1 {
		t.Fatalf("status=%+v", s)
	}
}
