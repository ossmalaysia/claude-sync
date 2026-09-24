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
	chats    []claudeapi.ChatDetail
	skills   []claudeapi.Skill
	skillPkg map[string][]byte // skill id -> package
	memory   string
	email    string
	failures map[string][]error // op -> results for successive calls (nil = succeed)
	// lostReply: the next UploadSkill takes effect but its reply is lost.
	lostReply error
	calls     []string // "Op:detail"
	seq       int
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{
		docs:     map[string][]claudeapi.Doc{},
		files:    map[string][]claudeapi.File{},
		blobs:    map[string][]byte{},
		previews: map[string][]byte{},
		skillPkg: map[string][]byte{},
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
		p.PromptTemplate = ""                                                 // the real listing omits instructions; pull must call GetProject
		p.DocsCount, p.FilesCount = len(f.docs[p.UUID]), len(f.files[p.UUID]) // the real listing has counts
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

// addChat seeds a chat whose messages form a single branch.
func (f *fakeAPI) addChat(name, project, updatedAt string, msgs ...claudeapi.ChatMessage) claudeapi.ChatDetail {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := claudeapi.ChatDetail{Chat: claudeapi.Chat{UUID: f.newID("c"), Name: name, ProjectUUID: project, UpdatedAt: updatedAt}, Messages: msgs}
	if len(msgs) > 0 {
		c.CurrentLeaf = msgs[len(msgs)-1].UUID
	}
	f.chats = append(f.chats, c)
	return c
}

func (f *fakeAPI) ListChats(_ context.Context, org string, offset, limit int) ([]claudeapi.Chat, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("ListChats", fmt.Sprint(offset)); err != nil {
		return nil, false, err
	}
	var out []claudeapi.Chat
	for i := offset; i < len(f.chats) && i < offset+limit; i++ {
		out = append(out, f.chats[i].Chat)
	}
	return out, offset+limit < len(f.chats), nil
}

func (f *fakeAPI) GetChat(_ context.Context, _, id string) (claudeapi.ChatDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("GetChat", id); err != nil {
		return claudeapi.ChatDetail{}, err
	}
	for _, c := range f.chats {
		if c.UUID == id {
			return c, nil
		}
	}
	return claudeapi.ChatDetail{}, notFound()
}

func (f *fakeAPI) addSkill(name, source, creator, updatedAt string, pkg []byte) claudeapi.Skill {
	f.mu.Lock()
	defer f.mu.Unlock()
	sk := claudeapi.Skill{ID: f.newID("skill"), Name: name, Source: source, CreatorType: creator, UpdatedAt: updatedAt}
	f.skills = append(f.skills, sk)
	f.skillPkg[sk.ID] = pkg
	return sk
}

func (f *fakeAPI) ListSkills(_ context.Context, org string) ([]claudeapi.Skill, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("ListSkills", org); err != nil {
		return nil, err
	}
	return append([]claudeapi.Skill(nil), f.skills...), nil
}

func (f *fakeAPI) DownloadSkill(_ context.Context, _, id string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("DownloadSkill", id); err != nil {
		return nil, err
	}
	b, ok := f.skillPkg[id]
	if !ok {
		return nil, notFound()
	}
	return append([]byte(nil), b...), nil
}

func (f *fakeAPI) UploadSkill(_ context.Context, _, fileName string, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("UploadSkill", fileName); err != nil {
		return err
	}
	sk := claudeapi.Skill{ID: f.newID("tskill"), Name: strings.TrimSuffix(fileName, ".skill"), Source: "custom", CreatorType: "user"}
	f.skills = append(f.skills, sk)
	f.skillPkg[sk.ID] = append([]byte(nil), data...)
	if err := f.lostReply; err != nil {
		f.lostReply = nil
		return err
	}
	return nil
}
