package migrate

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
)

var _ API = (*fakeAPI)(nil)

// fakeAPI is an in-memory claude.ai org. It serves as the source in pull
// tests and as the target in push/verify tests.
type fakeAPI struct {
	mu       sync.Mutex
	projects []claudeapi.Project
	docs     map[string][]claudeapi.Doc
	files    map[string][]claudeapi.File
	blobs    map[string][]byte
	previews map[string][]byte // file uuid -> preview (WebP) bytes
	memory   string
	email    string
	failures map[string][]error // op -> results for successive calls (nil = succeed)
	calls    []string           // "Op:detail"
	seq      int
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{
		docs:     map[string][]claudeapi.Doc{},
		files:    map[string][]claudeapi.File{},
		blobs:    map[string][]byte{},
		previews: map[string][]byte{},
		failures: map[string][]error{},
	}
}

func notFound() error { return &claudeapi.HTTPError{Status: 404, Kind: claudeapi.ErrNotFound} }

// failNext queues outcomes for the next calls of op; a nil entry lets that call succeed.
func (f *fakeAPI) failNext(op string, errs ...error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[op] = append(f.failures[op], errs...)
}

func (f *fakeAPI) count(op string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, op+":") {
			n++
		}
	}
	return n
}

// enter must be called with f.mu held.
func (f *fakeAPI) enter(op, detail string) error {
	f.calls = append(f.calls, op+":"+detail)
	if q := f.failures[op]; len(q) > 0 {
		f.failures[op] = q[1:]
		return q[0]
	}
	return nil
}

func (f *fakeAPI) newID(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s-%04d", prefix, f.seq)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// addProject seeds a project with docs (name -> content) and files (name -> bytes).
func (f *fakeAPI) addProject(name, prompt string, docs map[string]string, files map[string][]byte) claudeapi.Project {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := claudeapi.Project{UUID: f.newID("p"), Name: name, PromptTemplate: prompt, IsPrivate: true}
	f.projects = append(f.projects, p)
	for _, n := range sortedKeys(docs) {
		f.docs[p.UUID] = append(f.docs[p.UUID], claudeapi.Doc{UUID: f.newID("d"), FileName: n, Content: docs[n]})
	}
	for _, n := range sortedKeys(files) {
		id := f.newID("f")
		f.files[p.UUID] = append(f.files[p.UUID], claudeapi.File{FileUUID: id, FileName: n, FileKind: "blob", SizeBytes: int64(len(files[n]))})
		f.blobs[id] = files[n]
	}
	return p
}

func (f *fakeAPI) find(id string) int {
	for i, p := range f.projects {
		if p.UUID == id {
			return i
		}
	}
	return -1
}

func (f *fakeAPI) ListProjects(_ context.Context, org string) ([]claudeapi.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("ListProjects", org); err != nil {
		return nil, err
	}
	out := make([]claudeapi.Project, len(f.projects))
	for i, p := range f.projects {
		p.PromptTemplate = "" // the real listing omits instructions; pull must call GetProject
		out[i] = p
	}
	return out, nil
}

func (f *fakeAPI) GetProject(_ context.Context, _, id string) (claudeapi.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("GetProject", id); err != nil {
		return claudeapi.Project{}, err
	}
	i := f.find(id)
	if i < 0 {
		return claudeapi.Project{}, notFound()
	}
	p := f.projects[i]
	p.DocsCount, p.FilesCount = len(f.docs[id]), len(f.files[id])
	return p, nil
}

func (f *fakeAPI) CreateProject(_ context.Context, _ string, in claudeapi.NewProject) (claudeapi.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("CreateProject", in.Name); err != nil {
		return claudeapi.Project{}, err
	}
	p := claudeapi.Project{UUID: f.newID("tp"), Name: in.Name, Description: in.Description, IsPrivate: in.IsPrivate}
	f.projects = append(f.projects, p)
	return p, nil
}

func (f *fakeAPI) SetInstructions(_ context.Context, _, id, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("SetInstructions", id); err != nil {
		return err
	}
	i := f.find(id)
	if i < 0 {
		return notFound()
	}
	f.projects[i].PromptTemplate = text
	return nil
}

func (f *fakeAPI) ListDocs(_ context.Context, _, id string) ([]claudeapi.Doc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("ListDocs", id); err != nil {
		return nil, err
	}
	return append([]claudeapi.Doc(nil), f.docs[id]...), nil
}

func (f *fakeAPI) CreateDoc(_ context.Context, _, id, name, content string) (claudeapi.Doc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("CreateDoc", name); err != nil {
		return claudeapi.Doc{}, err
	}
	if f.find(id) < 0 {
		return claudeapi.Doc{}, notFound()
	}
	d := claudeapi.Doc{UUID: f.newID("td"), FileName: name, Content: content}
	f.docs[id] = append(f.docs[id], d)
	return d, nil
}

func (f *fakeAPI) ListFiles(_ context.Context, _, id string) ([]claudeapi.File, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("ListFiles", id); err != nil {
		return nil, err
	}
	return append([]claudeapi.File(nil), f.files[id]...), nil
}

func (f *fakeAPI) DownloadFile(_ context.Context, _, fileUUID string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("DownloadFile", fileUUID); err != nil {
		return nil, err
	}
	b, ok := f.blobs[fileUUID]
	if !ok {
		return nil, notFound()
	}
	return append([]byte(nil), b...), nil
}

func (f *fakeAPI) UploadFile(_ context.Context, _, id, name, _ string, data []byte) (claudeapi.File, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("UploadFile", name); err != nil {
		return claudeapi.File{}, err
	}
	if f.find(id) < 0 {
		return claudeapi.File{}, notFound()
	}
	file := claudeapi.File{FileUUID: f.newID("tf"), FileName: name, FileKind: "blob", SizeBytes: int64(len(data))}
	f.files[id] = append(f.files[id], file)
	f.blobs[file.FileUUID] = append([]byte(nil), data...)
	return file, nil
}

func (f *fakeAPI) GetMemory(_ context.Context, org string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("GetMemory", org); err != nil {
		return "", err
	}
	return f.memory, nil
}

func (f *fakeAPI) GetAccount(_ context.Context) (claudeapi.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("GetAccount", ""); err != nil {
		return claudeapi.Account{}, err
	}
	return claudeapi.Account{UUID: "user", EmailAddress: f.email}, nil
}

func (f *fakeAPI) DownloadPreview(_ context.Context, _, fileUUID string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("DownloadPreview", fileUUID); err != nil {
		return nil, err
	}
	b, ok := f.previews[fileUUID]
	if !ok {
		return nil, notFound()
	}
	return append([]byte(nil), b...), nil
}
