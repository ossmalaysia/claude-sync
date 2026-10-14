package migrate

import (
	"context"
	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
	"github.com/ossmalaysia/claude-sync/internal/store"
	"testing"
)

func TestPreviewMatchesContentsAndBlocksDifferentSameNamedFiles(t *testing.T) {
	source := newFakeAPI()
	p := source.addProject("Example", "", map[string]string{"same.md": "same", "changed.md": "new"}, map[string][]byte{"same.pdf": []byte("same"), "changed.pdf": []byte("new")})
	st := newTestStore(t)
	if _, err := Pull(context.Background(), source, st, "source", testOpts(), nil); err != nil {
		t.Fatal(err)
	}
	target := newFakeAPI()
	target.addProject("Example", "", map[string]string{"same.md": "same", "changed.md": "old"}, map[string][]byte{"same.pdf": []byte("same"), "changed.pdf": []byte("old")})
	state := &store.State{}
	sel := map[string]bool{p.UUID: true}
	preview, err := Preview(context.Background(), target, st, state, "target", sel, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Conflicts) != 2 || preview.AlreadyCopied != 2 || preview.NewDocs != 0 || preview.NewFiles != 0 {
		t.Fatalf("preview=%+v", preview)
	}
	if target.count("CreateDoc")+target.count("UploadFile")+target.count("CreateProject") != 0 {
		t.Fatal("preview wrote to target")
	}
	res, err := Push(context.Background(), target, st, state, "target", sel, testOpts(), nil)
	if err != nil || res.Docs != 0 || res.Files != 0 || len(res.Conflicts) != 2 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestReverseUsesIDsAfterRenameAndAddsOnlyNewContent(t *testing.T) {
	original := newFakeAPI()
	p := original.addProject("Original", "", map[string]string{"old.md": "existing"}, map[string][]byte{"old.pdf": []byte("file")})
	st := newTestStore(t)
	if err := st.SaveSettings(store.Settings{SourceOrg: "a", TargetOrg: "b"}); err != nil {
		t.Fatal(err)
	}
	if err := st.ActivateDirection("a", "b", "A", "B", false); err != nil {
		t.Fatal(err)
	}
	if _, err := Pull(context.Background(), original, st, "a", testOpts(), nil); err != nil {
		t.Fatal(err)
	}
	copyAPI := newFakeAPI()
	state, _ := st.LoadState()
	if _, err := Push(context.Background(), copyAPI, st, state, "b", map[string]bool{p.UUID: true}, testOpts(), nil); err != nil {
		t.Fatal(err)
	}
	copiedID := state.Project(p.UUID).Target
	copyAPI.projects[0].Name = "Renamed"
	copyAPI.docs[copiedID] = append(copyAPI.docs[copiedID], claudeapi.Doc{UUID: "new-doc", FileName: "new.md", Content: "added"})
	if err := st.ActivateDirection("b", "a", "B", "A", true); err != nil {
		t.Fatal(err)
	}
	if _, err := Pull(context.Background(), copyAPI, st, "b", testOpts(), nil); err != nil {
		t.Fatal(err)
	}
	reverse, _ := st.LoadState()
	res, err := Push(context.Background(), original, st, reverse, "a", map[string]bool{copiedID: true}, testOpts(), nil)
	if err != nil || res.CreatedProjects != 0 || res.Docs != 1 || res.Files != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if len(original.docs[p.UUID]) != 2 || len(original.files[p.UUID]) != 1 {
		t.Fatal("original content duplicated")
	}
	if err := st.ActivateDirection("a", "b", "A", "B", false); err != nil {
		t.Fatal(err)
	}
	original.projects[0].UpdatedAt = "changed"
	if _, err := Pull(context.Background(), original, st, "a", testOpts(), nil); err != nil {
		t.Fatal(err)
	}
	state, _ = st.LoadState()
	again, err := Push(context.Background(), copyAPI, st, state, "b", map[string]bool{p.UUID: true}, testOpts(), nil)
	if err != nil || again.Docs != 0 {
		t.Fatalf("round trip copied content back: %+v %v", again, err)
	}
}

func TestPreviewRejectsAmbiguousProjectNames(t *testing.T) {
	source := newFakeAPI()
	p := source.addProject("Same", "", nil, nil)
	st := newTestStore(t)
	_, _ = Pull(context.Background(), source, st, "a", testOpts(), nil)
	target := newFakeAPI()
	target.addProject("Same", "", nil, nil)
	target.addProject("Same", "", nil, nil)
	preview, err := Preview(context.Background(), target, st, &store.State{}, "b", map[string]bool{p.UUID: true}, testOpts())
	if err != nil || len(preview.Conflicts) != 1 || preview.NewProjects != 0 {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
}

func TestMappedEditsAndDeletedTargetsAreConflicts(t *testing.T) {
	api, st, state, sel := pushFixture(t)
	if _, err := push(t, api, st, state, sel, testOpts()); err != nil {
		t.Fatal(err)
	}
	target := state.Project("s1").Target
	for i := range api.docs[target] {
		if api.docs[target][i].UUID == state.Project("s1").Docs["d1"].Target {
			api.docs[target][i].Content = "edited in target"
		}
	}
	st.SaveDoc("s1", store.DocRecord{UUID: "d2", FileName: "b.md", Content: "edited in source"})
	api.files[target] = nil
	plan, err := Preview(context.Background(), api, st, state, "org", sel, testOpts())
	if err != nil || len(plan.Conflicts) != 3 {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	res, err := push(t, api, st, state, sel, testOpts())
	if err != nil || res.Docs+res.Files != 0 || len(res.Conflicts) != 3 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	st.SaveSelection(sel)
	rows, err := Verify(context.Background(), api, st, state, "org", testOpts(), nil, nil)
	if err != nil || rows[0].OK {
		t.Fatalf("conflicts verified as success: %v %v", rows, err)
	}
}

func TestReconcileRecognizesRenamedFileByContent(t *testing.T) {
	source := newFakeAPI()
	p := source.addProject("P", "", nil, map[string][]byte{"new-name.pdf": []byte("bytes")})
	target := newFakeAPI()
	target.addProject("P", "", nil, map[string][]byte{"old-name.pdf": []byte("bytes")})
	st := newTestStore(t)
	_, _ = Pull(context.Background(), source, st, "a", testOpts(), nil)
	plan, err := Preview(context.Background(), target, st, &store.State{}, "b", map[string]bool{p.UUID: true}, testOpts())
	if err != nil || plan.AlreadyCopied != 1 || plan.NewFiles != 0 {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
}

func TestUnreadableTargetFilePreventsProjectWrites(t *testing.T) {
	api, st, state, sel := pushFixture(t)
	_, _ = push(t, api, st, state, sel, testOpts())
	st.SaveDoc("s1", store.DocRecord{UUID: "new", FileName: "new.md", Content: "new"})
	api.failNext("DownloadFile", notFound())
	res, err := push(t, api, st, state, sel, testOpts())
	if err != nil || res.Docs != 0 || len(res.Failed) != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	plan, err := Plan(st, sel, state, "org", testOpts())
	if err != nil || plan.RetryFailed != 1 {
		t.Fatalf("unreadable target not marked for retry: %+v %v", plan, err)
	}
}

func TestPullRejectsDifferentSourceBeforeReadingOrWriting(t *testing.T) {
	st := newTestStore(t)
	api := newFakeAPI()
	st.SaveManifest(store.Manifest{SourceOrg: "a"})
	if _, err := Pull(context.Background(), api, st, "b", testOpts(), nil); err == nil || api.count("ListProjects") != 0 {
		t.Fatal("mixed source histories")
	}
}
