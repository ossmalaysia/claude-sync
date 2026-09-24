package migrate

import (
	"testing"

	"github.com/ossmalaysia/claude-sync/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

type seedDoc struct{ UUID, Name, Content string }

type seedFile struct {
	UUID, Name string
	Data       []byte
}

// seedProject writes a pulled project into the store, as Pull would.
func seedProject(t *testing.T, st *store.Store, order int, uuid, name, prompt string, docs []seedDoc, files []seedFile) store.ProjectMeta {
	t.Helper()
	p := store.ProjectMeta{UUID: uuid, Name: name, PromptTemplate: prompt, IsPrivate: true, Order: order}
	if err := st.SaveProject(p); err != nil {
		t.Fatal(err)
	}
	for _, d := range docs {
		if err := st.SaveDoc(uuid, store.DocRecord{UUID: d.UUID, FileName: d.Name, Content: d.Content}); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range files {
		m := store.FileMeta{UUID: f.UUID, FileName: f.Name, FileKind: "blob", SizeBytes: int64(len(f.Data)), Mime: "application/pdf"}
		if err := st.SaveFile(uuid, m, f.Data); err != nil {
			t.Fatal(err)
		}
	}
	return p
}
