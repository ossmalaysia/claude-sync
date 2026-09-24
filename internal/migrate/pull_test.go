package migrate

import (
	"bytes"
	"context"
	"errors"
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
