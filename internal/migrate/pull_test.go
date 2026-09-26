package migrate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/ossmalaysia/claude-sync/internal/store"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
)

func TestPullSavesProjectsDocsFilesAndMemory(t *testing.T) {
	api := newFakeAPI()
	api.memory = "# about me"
	pdf := []byte("%PDF-1.4 bytes")
	p := api.addProject("Acme: Pricing", "Be terse.", map[string]string{"claude/a.md": "alpha"}, map[string][]byte{"intro.pdf": pdf})
	st := newTestStore(t)

	res, err := Pull(context.Background(), api, st, "src-org", testOpts(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Projects != 1 || res.Docs != 1 || res.Files != 1 || res.Bytes != int64(len(pdf)) {
		t.Fatalf("res=%+v", res)
	}
	ps, _ := st.ListProjects()
	if len(ps) != 1 || ps[0].UUID != p.UUID || ps[0].PromptTemplate != "Be terse." {
		t.Fatalf("projects=%+v (instructions must come from GetProject)", ps)
	}
	docs, _ := st.ListDocs(p.UUID)
	if len(docs) != 1 || docs[0].FileName != "claude/a.md" || docs[0].Content != "alpha" {
		t.Fatalf("docs=%+v", docs)
	}
	files, _ := st.ListFiles(p.UUID)
	if len(files) != 1 || files[0].Mime != "application/pdf" {
		t.Fatalf("files=%+v", files)
	}
	got, _ := st.ReadFile(p.UUID, files[0].UUID)
	if !bytes.Equal(got, pdf) {
		t.Fatalf("bytes differ")
	}
	if m, _ := st.LoadMemory(); m != "# about me" {
		t.Fatalf("memory=%q", m)
	}
	if man, err := st.LoadManifest(); err != nil || man.SourceOrg != "src-org" || man.Projects != 1 {
		t.Fatalf("manifest=%+v err=%v", man, err)
	}
}

func TestPullSkipsFilesAlreadyDownloaded(t *testing.T) {
	api := newFakeAPI()
	api.addProject("P", "", nil, map[string][]byte{"a.pdf": []byte("x")})
	st := newTestStore(t)
	for i := 0; i < 2; i++ {
		if _, err := Pull(context.Background(), api, st, "o", testOpts(), nil); err != nil {
			t.Fatal(err)
		}
	}
	if n := api.count("DownloadFile"); n != 1 {
		t.Fatalf("DownloadFile called %d times, want 1", n)
	}
}

func TestPullContinuesWhenOneFileFails(t *testing.T) {
	api := newFakeAPI()
	api.addProject("P", "", nil, map[string][]byte{"a.pdf": []byte("a"), "b.pdf": []byte("b")})
	api.failNext("DownloadFile", notFound())
	res, err := Pull(context.Background(), api, newTestStore(t), "o", testOpts(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 1 || len(res.Failed) != 1 || res.Failed[0].Item != "file a.pdf" {
		t.Fatalf("res=%+v", res)
	}
}

func TestPullAbortsOnAuthError(t *testing.T) {
	api := newFakeAPI()
	api.addProject("P", "", map[string]string{"a.md": "x"}, nil)
	api.failNext("ListDocs", httpErr(401, claudeapi.ErrAuth))
	_, err := Pull(context.Background(), api, newTestStore(t), "o", testOpts(), nil)
	if !errors.Is(err, claudeapi.ErrAuth) {
		t.Fatalf("err=%v", err)
	}
}

func TestPullReportsProgress(t *testing.T) {
	api := newFakeAPI()
	api.addProject("A", "", nil, nil)
	api.addProject("B", "", nil, nil)
	var events []Event
	if _, err := Pull(context.Background(), api, newTestStore(t), "o", testOpts(), func(e Event) { events = append(events, e) }); err != nil {
		t.Fatal(err)
	}
	last := events[len(events)-1]
	if last.Stage != "pull" || last.Done != 2 || last.Total != 2 {
		t.Fatalf("last=%+v", last)
	}
}

func TestPullAbortsWhenWindowClosed(t *testing.T) {
	api := newFakeAPI()
	api.addProject("A", "", nil, map[string][]byte{"a.pdf": []byte("x"), "b.pdf": []byte("y")})
	api.failNext("DownloadFile", claudeapi.ErrSessionGone)
	res, err := Pull(context.Background(), api, newTestStore(t), "o", testOpts(), nil)
	if !errors.Is(err, claudeapi.ErrSessionGone) || len(res.Failed) != 0 || api.count("DownloadFile") != 1 {
		t.Fatalf("res=%+v err=%v calls=%v", res, err, api.calls)
	}
}

func TestPullRecordsSourceAccountInManifest(t *testing.T) {
	api := newFakeAPI()
	api.email = "sam@example.com"
	api.addProject("A", "", map[string]string{"a.md": "x"}, nil)
	st := newTestStore(t)
	if _, err := Pull(context.Background(), api, st, "src-org", testOpts(), nil); err != nil {
		t.Fatal(err)
	}
	m, err := st.LoadManifest()
	if err != nil || m.SourceAccount != "sam@example.com" || m.SourceOrg != "src-org" || m.Projects != 1 || m.Docs != 1 {
		t.Fatalf("manifest=%+v err=%v", m, err)
	}
}

func TestPullSucceedsWhenAccountLookupFails(t *testing.T) {
	api := newFakeAPI()
	api.failNext("GetAccount", httpErr(404, claudeapi.ErrNotFound))
	st := newTestStore(t)
	if _, err := Pull(context.Background(), api, st, "src-org", testOpts(), nil); err != nil {
		t.Fatal(err)
	}
	if m, _ := st.LoadManifest(); m.SourceAccount != "" || m.SourceOrg != "src-org" {
		t.Fatalf("manifest=%+v", m)
	}
}

func TestPullProgressCarriesCountsAndBytes(t *testing.T) {
	api := newFakeAPI()
	api.addProject("A", "", map[string]string{"a.md": "x", "b.md": "y"}, map[string][]byte{"a.pdf": []byte("12345")})
	api.addProject("B", "", nil, map[string][]byte{"b.pdf": []byte("123")})
	var events []Event
	if _, err := Pull(context.Background(), api, newTestStore(t), "o", testOpts(), func(e Event) { events = append(events, e) }); err != nil {
		t.Fatal(err)
	}
	sawMidPull := false
	for _, e := range events {
		if e.Level == "info" && e.Done == 0 && e.Docs == 2 && e.Files == 1 && e.Bytes == 5 {
			sawMidPull = true
		}
	}
	if !sawMidPull {
		t.Fatalf("no progress event with running totals during project A: %+v", events)
	}
	last := events[len(events)-1]
	if last.Done != 2 || last.Docs != 2 || last.Files != 2 || last.Bytes != 8 {
		t.Fatalf("last=%+v", last)
	}
}

// claude.ai does not serve originals for image files (/contents is 404);
// the full-size preview (WebP) is the best available copy.
func TestPullFallsBackToPreviewForImages(t *testing.T) {
	api := newFakeAPI()
	p := api.addProject("Acme Retail", "", nil, map[string][]byte{"shot.png": []byte("png-original")})
	f := api.files[p.UUID][0]
	api.files[p.UUID][0].FileKind = "image"
	delete(api.blobs, f.FileUUID) // /contents -> 404, as on claude.ai
	api.previews[f.FileUUID] = []byte("RIFF-webp")
	st := newTestStore(t)

	res, err := Pull(context.Background(), api, st, "o", testOpts(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 1 || res.Converted != 1 || len(res.Failed) != 0 {
		t.Fatalf("res=%+v", res)
	}
	metas, _ := st.ListFiles(p.UUID)
	m := metas[0]
	if m.FileName != "shot.webp" || m.Mime != "image/webp" || m.ConvertedFrom != "shot.png" {
		t.Fatalf("meta=%+v", m)
	}
	got, _ := st.ReadFile(p.UUID, m.UUID)
	if string(got) != "RIFF-webp" {
		t.Fatalf("bytes=%q", got)
	}
}

func TestPullDoesNotUsePreviewForMissingNonImages(t *testing.T) {
	api := newFakeAPI()
	p := api.addProject("P", "", nil, map[string][]byte{"a.pdf": []byte("x")})
	f := api.files[p.UUID][0]
	delete(api.blobs, f.FileUUID)
	api.previews[f.FileUUID] = []byte("should-not-be-used")
	res, err := Pull(context.Background(), api, newTestStore(t), "o", testOpts(), nil)
	if err != nil || len(res.Failed) != 1 || res.Converted != 0 || api.count("DownloadPreview") != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

// A stopped pull must leave a record the app can show ("1/2, not complete")
// and a later pull must finish it.
func TestPullRecordsPartialProgressForResume(t *testing.T) {
	api := newFakeAPI()
	api.addProject("A", "", nil, nil)
	api.addProject("B", "", nil, nil)
	st := newTestStore(t)
	api.failNext("GetProject", nil, context.Canceled) // stop while on the 2nd project
	if _, err := Pull(context.Background(), api, st, "o", testOpts(), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	man, err := st.LoadManifest()
	if err != nil || man.Complete || man.Total != 2 || man.SourceOrg != "o" {
		t.Fatalf("partial manifest=%+v err=%v", man, err)
	}
	if ps, _ := st.ListProjects(); len(ps) != 1 {
		t.Fatalf("want 1 project on disk, got %d", len(ps))
	}
	if _, err := Pull(context.Background(), api, st, "o", testOpts(), nil); err != nil {
		t.Fatal(err)
	}
	man, _ = st.LoadManifest()
	if !man.Complete || man.Projects != 2 || man.PulledAt.IsZero() {
		t.Fatalf("final manifest=%+v", man)
	}
}

func artifactMsg(id, title, content string) claudeapi.ChatMessage {
	return msg("m-"+id, "", 0, tool("artifacts", map[string]any{"id": id, "command": "create", "type": "text/markdown", "title": title, "content": content}))
}

func TestPullSavesAndExportsChatArtifacts(t *testing.T) {
	api := newFakeAPI()
	p := api.addProject("Acme: Website", "", nil, nil)
	api.addChat("Kick-off", p.UUID, "t1", artifactMsg("a1", "Brief", "# Brief"))
	api.addChat("Loose chat", "", "t1", artifactMsg("a2", "Idea", "idea"))
	api.addChat("No artifacts", p.UUID, "t1", msg("m", "", 0))
	st := newTestStore(t)

	res, err := Pull(context.Background(), api, st, "o", testOpts(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Chats != 3 || res.Artifacts != 2 {
		t.Fatalf("res=%+v", res)
	}
	chats, _ := st.ListChats()
	if len(chats) != 3 {
		t.Fatalf("chats saved=%d", len(chats))
	}
	export := filepath.Join(st.Root(), "migration", "artifacts-export", "Acme- Website", "Kick-off", "Artifact - Brief.md")
	if b, err := os.ReadFile(export); err != nil || string(b) != "# Brief" {
		t.Fatalf("export %s: %q %v", export, b, err)
	}
	loose := filepath.Join(st.Root(), "migration", "artifacts-export", "No project", "Loose chat", "Artifact - Idea.md")
	if _, err := os.Stat(loose); err != nil {
		t.Fatalf("loose export: %v", err)
	}
	if man, _ := st.LoadManifest(); man.Chats != 3 || man.Artifacts != 2 {
		t.Fatalf("manifest=%+v", man)
	}
}

func TestPullSkipsUnchangedChatsAndPages(t *testing.T) {
	old := chatPageSize
	chatPageSize = 2
	defer func() { chatPageSize = old }()
	api := newFakeAPI()
	for i := 0; i < 5; i++ {
		api.addChat(fmt.Sprintf("c%d", i), "", "t1", artifactMsg(fmt.Sprint(i), "T", "x"))
	}
	st := newTestStore(t)
	if _, err := Pull(context.Background(), api, st, "o", testOpts(), nil); err != nil {
		t.Fatal(err)
	}
	if n := api.count("ListChats"); n != 3 {
		t.Fatalf("ListChats pages=%d, want 3", n)
	}
	api.chats[0].UpdatedAt = "t2" // one chat changed
	res, err := Pull(context.Background(), api, st, "o", testOpts(), nil)
	if err != nil || res.Chats != 5 || res.Artifacts != 5 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if n := api.count("GetChat"); n != 6 {
		t.Fatalf("GetChat calls=%d, want 5 + 1 changed", n)
	}
}

// While chats are being read, the manifest must say so, with running counts,
// so the app (which only reads disk) shows real progress for a CLI pull.
func TestPullRecordsChatPhaseProgress(t *testing.T) {
	old := chatProgressEvery
	chatProgressEvery = 1
	defer func() { chatProgressEvery = old }()
	api := newFakeAPI()
	api.addChat("c1", "", "t", artifactMsg("a", "T", "x"))
	api.addChat("c2", "", "t", artifactMsg("b", "T", "y"))
	st := newTestStore(t)
	var seen []store.Manifest
	report := func(e Event) {
		if strings.HasPrefix(e.Message, "chats:") {
			m, _ := st.LoadManifest()
			seen = append(seen, m)
		}
	}
	if _, err := Pull(context.Background(), api, st, "o", testOpts(), report); err != nil {
		t.Fatal(err)
	}
	if len(seen) == 0 {
		t.Fatal("no chat progress events")
	}
	last := seen[len(seen)-1]
	if last.Phase != "chats" || last.Chats != 2 || last.Artifacts != 2 || last.Complete {
		t.Fatalf("mid-pull manifest=%+v", last)
	}
	if final, _ := st.LoadManifest(); final.Phase != "" || !final.Complete {
		t.Fatalf("final manifest=%+v", final)
	}
}

// Chats are listed first so the pull knows how many still need reading;
// unchanged chats are excluded from the work total used for the time estimate.
func TestPullChatPhaseKnowsTotalWork(t *testing.T) {
	old := chatProgressEvery
	chatProgressEvery = 1
	defer func() { chatProgressEvery = old }()
	api := newFakeAPI()
	for i := 0; i < 5; i++ {
		api.addChat(fmt.Sprintf("c%d", i), "", "t1", artifactMsg(fmt.Sprint(i), "T", "x"))
	}
	st := newTestStore(t)
	if _, err := Pull(context.Background(), api, st, "o", testOpts(), nil); err != nil {
		t.Fatal(err)
	}
	api.chats[1].UpdatedAt, api.chats[3].UpdatedAt = "t2", "t2" // 2 of 5 changed
	var mids []store.Manifest
	report := func(e Event) {
		if strings.HasPrefix(e.Message, "chats:") {
			m, _ := st.LoadManifest()
			mids = append(mids, m)
		}
	}
	if _, err := Pull(context.Background(), api, st, "o", testOpts(), report); err != nil {
		t.Fatal(err)
	}
	last := mids[len(mids)-1]
	if last.ChatsTotal != 5 || last.PhaseTotal != 2 || last.PhaseDone != 2 || last.PhaseStartedAt.IsZero() {
		t.Fatalf("manifest=%+v", last)
	}
}

func TestPullProjectPhaseProgress(t *testing.T) {
	api := newFakeAPI()
	api.addProject("A", "", nil, nil)
	api.addProject("B", "", nil, nil)
	st := newTestStore(t)
	var seen []store.Manifest
	report := func(e Event) {
		if e.Stage == "pull" && e.Message == "B" {
			m, _ := st.LoadManifest()
			seen = append(seen, m)
		}
	}
	if _, err := Pull(context.Background(), api, st, "o", testOpts(), report); err != nil {
		t.Fatal(err)
	}
	if len(seen) == 0 || seen[0].Phase != "projects" || seen[0].PhaseTotal != 2 || seen[0].PhaseDone != 1 || seen[0].PhaseStartedAt.IsZero() {
		t.Fatalf("seen=%+v", seen)
	}
}

// Delta scan: a project whose listing signature (updated_at, doc and file
// counts) is unchanged since its last complete pull is not read again.
func TestPullSkipsUnchangedProjects(t *testing.T) {
	api := newFakeAPI()
	a := api.addProject("A", "", map[string]string{"a.md": "x"}, nil)
	api.addProject("B", "", nil, nil)
	st := newTestStore(t)
	if _, err := Pull(context.Background(), api, st, "o", testOpts(), nil); err != nil {
		t.Fatal(err)
	}
	res, err := Pull(context.Background(), api, st, "o", testOpts(), nil)
	if err != nil || res.ProjectsSkipped != 2 || res.ProjectsRead != 0 || api.count("GetProject") != 2 {
		t.Fatalf("unchanged: res=%+v err=%v GetProject=%d", res, err, api.count("GetProject"))
	}
	if res.Projects != 2 || res.Docs != 1 {
		t.Fatalf("skipped projects must still be counted: %+v", res)
	}
	// A new doc changes A's doc count; a new project C appears.
	api.CreateDoc(context.Background(), "o", a.UUID, "b.md", "y")
	api.addProject("C", "", nil, nil)
	res, err = Pull(context.Background(), api, st, "o", testOpts(), nil)
	if err != nil || res.ProjectsRead != 2 || res.ProjectsNew != 1 || res.ProjectsSkipped != 1 {
		t.Fatalf("delta: res=%+v err=%v", res, err)
	}
	if docs, _ := st.ListDocs(a.UUID); len(docs) != 2 {
		t.Fatalf("new doc not pulled: %+v", docs)
	}
}

// A project interrupted mid-pull has no completion marker, so the next pull
// reads it again even though its listing is unchanged.
func TestPullRereadsInterruptedProject(t *testing.T) {
	api := newFakeAPI()
	api.addProject("A", "", nil, map[string][]byte{"f.pdf": []byte("x")})
	st := newTestStore(t)
	api.failNext("ListFiles", context.Canceled)
	if _, err := Pull(context.Background(), api, st, "o", testOpts(), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	res, err := Pull(context.Background(), api, st, "o", testOpts(), nil)
	if err != nil || res.ProjectsRead != 1 || res.ProjectsSkipped != 0 || res.Files != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

// Projects pulled by an older version have no completion marker. If their
// saved updated_at and local counts match the listing, they are adopted as
// complete instead of being read again; otherwise they are read.
func TestPullAdoptsCompleteLegacyProjects(t *testing.T) {
	api := newFakeAPI()
	a := api.addProject("A", "", map[string]string{"a.md": "x"}, nil)
	b := api.addProject("B", "", map[string]string{"b.md": "y"}, nil)
	st := newTestStore(t)
	seedProject(t, st, 0, a.UUID, "A", "", []seedDoc{{"d-a", "a.md", "x"}}, nil) // complete, no marker
	seedProject(t, st, 1, b.UUID, "B", "", nil, nil)                             // missing its doc
	res, err := Pull(context.Background(), api, st, "o", testOpts(), nil)
	if err != nil || res.ProjectsSkipped != 1 || res.ProjectsRead != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if m, _, _ := st.LoadProject(a.UUID); m.Synced == "" {
		t.Fatal("adopted project should get its marker")
	}
}

// Chats keep their conversation as a transcript; chats saved before
// transcripts existed are read once more to fill it in.
func TestPullKeepsTranscriptsAndBackfillsOldChats(t *testing.T) {
	api := newFakeAPI()
	p := api.addProject("Acme: Website", "", nil, nil)
	c := api.addChat("Kick-off", p.UUID, "t1", claudeapi.ChatMessage{UUID: "m1", Sender: "human", Content: []claudeapi.ContentBlock{{Type: "text", Text: "Hello there"}}})
	st := newTestStore(t)
	st.SaveChat(store.ChatRecord{UUID: c.UUID, ProjectUUID: p.UUID, UpdatedAt: "t1"}) // from an older version
	if _, err := Pull(context.Background(), api, st, "o", testOpts(), nil); err != nil {
		t.Fatal(err)
	}
	rec, _, _ := st.LoadChat(c.UUID)
	if !strings.Contains(rec.Transcript, "Hello there") || rec.TranscriptAs != "Chat - Kick-off.md" {
		t.Fatalf("rec=%+v", rec)
	}
	before := api.count("GetChat")
	Pull(context.Background(), api, st, "o", testOpts(), nil)
	if api.count("GetChat") != before {
		t.Fatal("a chat with a transcript and the same updated_at must not be read again")
	}
}
