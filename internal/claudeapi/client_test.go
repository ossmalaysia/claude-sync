package claudeapi

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

type call struct {
	Method, Path, FileName, Mime string
	Body                         any
	Data                         []byte
}

type fakeDoer struct {
	calls  []call
	status int
	resp   string
	data   []byte
}

func (d *fakeDoer) JSON(_ context.Context, method, path string, body any) (int, []byte, error) {
	d.calls = append(d.calls, call{Method: method, Path: path, Body: body})
	return d.status, []byte(d.resp), nil
}

func (d *fakeDoer) Download(_ context.Context, path string) (int, []byte, error) {
	d.calls = append(d.calls, call{Method: "GET", Path: path})
	return d.status, d.data, nil
}

func (d *fakeDoer) Upload(_ context.Context, path, name, mime string, data []byte) (int, []byte, error) {
	d.calls = append(d.calls, call{Method: "UPLOAD", Path: path, FileName: name, Mime: mime, Data: data})
	return d.status, []byte(d.resp), nil
}

func assertJSON(t *testing.T, got any, want string) {
	t.Helper()
	g, _ := json.Marshal(got)
	var gv, wv any
	json.Unmarshal(g, &gv)
	json.Unmarshal([]byte(want), &wv)
	if !reflect.DeepEqual(gv, wv) {
		t.Errorf("body = %s, want %s", g, want)
	}
}

var ctx = context.Background()

func TestListOrgs(t *testing.T) {
	d := &fakeDoer{status: 200, resp: `[{"uuid":"o1","name":"Example Team","capabilities":["raven","chat"],"settings":{}}]`}
	orgs, err := New(d).ListOrgs(ctx)
	if err != nil || len(orgs) != 1 || !orgs[0].Has("raven") || d.calls[0].Path != "/api/organizations" {
		t.Fatalf("orgs=%+v err=%v calls=%+v", orgs, err, d.calls)
	}
}

func TestListProjectsDecodes(t *testing.T) {
	d := &fakeDoer{status: 200, resp: `[{"uuid":"p1","name":"Acme: Research","is_private":true,"is_starred":true,"extra":1}]`}
	ps, err := New(d).ListProjects(ctx, "o1")
	if err != nil || len(ps) != 1 || ps[0].Name != "Acme: Research" || !ps[0].IsStarred {
		t.Fatalf("ps=%+v err=%v", ps, err)
	}
	if d.calls[0].Method != "GET" || d.calls[0].Path != "/api/organizations/o1/projects" {
		t.Fatalf("call=%+v", d.calls[0])
	}
}

func TestGetProjectDecodesCountsAndInstructions(t *testing.T) {
	d := &fakeDoer{status: 200, resp: `{"uuid":"p1","name":"X","prompt_template":"Be terse.","docs_count":10,"files_count":1}`}
	p, err := New(d).GetProject(ctx, "o1", "p1")
	if err != nil || p.PromptTemplate != "Be terse." || p.DocsCount != 10 || p.FilesCount != 1 {
		t.Fatalf("p=%+v err=%v", p, err)
	}
	if d.calls[0].Path != "/api/organizations/o1/projects/p1" {
		t.Fatalf("path=%s", d.calls[0].Path)
	}
}

func TestCreateProjectSendsBody(t *testing.T) {
	d := &fakeDoer{status: 201, resp: `{"uuid":"new","name":"A"}`}
	p, err := New(d).CreateProject(ctx, "o1", NewProject{Name: "A", Description: "d", IsPrivate: true})
	if err != nil || p.UUID != "new" {
		t.Fatalf("p=%+v err=%v", p, err)
	}
	c := d.calls[0]
	if c.Method != "POST" || c.Path != "/api/organizations/o1/projects" {
		t.Fatalf("call=%+v", c)
	}
	assertJSON(t, c.Body, `{"name":"A","description":"d","is_private":true}`)
}

func TestSetInstructions(t *testing.T) {
	d := &fakeDoer{status: 202, resp: `{"uuid":"p1"}`}
	if err := New(d).SetInstructions(ctx, "o1", "p1", "hi"); err != nil {
		t.Fatal(err)
	}
	c := d.calls[0]
	if c.Method != "PUT" || c.Path != "/api/organizations/o1/projects/p1" {
		t.Fatalf("call=%+v", c)
	}
	assertJSON(t, c.Body, `{"prompt_template":"hi"}`)
}

func TestDeleteProjectAcceptsEmpty204(t *testing.T) {
	d := &fakeDoer{status: 204}
	if err := New(d).DeleteProject(ctx, "o1", "p1"); err != nil {
		t.Fatal(err)
	}
	if d.calls[0].Method != "DELETE" || d.calls[0].Path != "/api/organizations/o1/projects/p1" {
		t.Fatalf("call=%+v", d.calls[0])
	}
}

func TestDocs(t *testing.T) {
	d := &fakeDoer{status: 200, resp: `[{"uuid":"d1","file_name":"claude/a.md","content":"alpha","estimated_token_count":3}]`}
	docs, err := New(d).ListDocs(ctx, "o1", "p1")
	if err != nil || docs[0].FileName != "claude/a.md" || docs[0].Content != "alpha" || d.calls[0].Path != "/api/organizations/o1/projects/p1/docs" {
		t.Fatalf("docs=%+v err=%v", docs, err)
	}
	d = &fakeDoer{status: 201, resp: `{"uuid":"d9","file_name":"a.md","content":"x"}`}
	doc, err := New(d).CreateDoc(ctx, "o1", "p1", "a.md", "x")
	if err != nil || doc.UUID != "d9" || d.calls[0].Method != "POST" || d.calls[0].Path != "/api/organizations/o1/projects/p1/docs" {
		t.Fatalf("doc=%+v err=%v call=%+v", doc, err, d.calls[0])
	}
	assertJSON(t, d.calls[0].Body, `{"file_name":"a.md","content":"x"}`)
}

func TestListFiles(t *testing.T) {
	d := &fakeDoer{status: 200, resp: `[{"success":true,"file_uuid":"f1","file_name":"a.pdf","file_kind":"blob","size_bytes":1837431}]`}
	fs, err := New(d).ListFiles(ctx, "o1", "p1")
	if err != nil || fs[0].SizeBytes != 1837431 || d.calls[0].Path != "/api/organizations/o1/projects/p1/files" {
		t.Fatalf("fs=%+v err=%v", fs, err)
	}
}

func TestDownloadUsesContentsNotPreview(t *testing.T) {
	d := &fakeDoer{status: 200, data: []byte("%PDF-1.4")}
	b, err := New(d).DownloadFile(ctx, "o1", "f1")
	if err != nil || string(b) != "%PDF-1.4" {
		t.Fatalf("b=%q err=%v", b, err)
	}
	if d.calls[0].Path != "/api/organizations/o1/files/f1/contents" {
		t.Fatalf("path=%s", d.calls[0].Path)
	}
}

func TestUploadFile(t *testing.T) {
	d := &fakeDoer{status: 200, resp: `{"success":true,"file_uuid":"u1","file_name":"x.png","file_kind":"image","size_bytes":70}`}
	f, err := New(d).UploadFile(ctx, "o1", "p1", "x.png", "image/png", []byte{1, 2})
	if err != nil || f.FileUUID != "u1" {
		t.Fatalf("f=%+v err=%v", f, err)
	}
	c := d.calls[0]
	if c.Path != "/api/organizations/o1/projects/p1/upload" || c.FileName != "x.png" || c.Mime != "image/png" || len(c.Data) != 2 {
		t.Fatalf("call=%+v", c)
	}
}

func TestGetMemory(t *testing.T) {
	d := &fakeDoer{status: 200, resp: `{"memory":"**Work context**","controls":null}`}
	m, err := New(d).GetMemory(ctx, "o1")
	if err != nil || m != "**Work context**" || d.calls[0].Path != "/api/organizations/o1/memory" {
		t.Fatalf("m=%q err=%v", m, err)
	}
}

func TestErrorsAreMapped(t *testing.T) {
	d := &fakeDoer{status: 401, resp: `{}`}
	_, err := New(d).ListProjects(ctx, "o1")
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("err=%v", err)
	}
	d = &fakeDoer{status: 404, data: []byte(`{}`)}
	if _, err := New(d).DownloadFile(ctx, "o1", "f1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("download err=%v", err)
	}
}

func TestGetAccount(t *testing.T) {
	d := &fakeDoer{status: 200, resp: `{"uuid":"u1","email_address":"sam@example.com","full_name":"J"}`}
	a, err := New(d).GetAccount(ctx)
	if err != nil || a.EmailAddress != "sam@example.com" || d.calls[0].Path != "/api/account" || d.calls[0].Method != "GET" {
		t.Fatalf("a=%+v err=%v calls=%+v", a, err, d.calls)
	}
}

func TestDownloadPreviewUsesPreviewPath(t *testing.T) {
	d := &fakeDoer{status: 200, data: []byte("RIFF....WEBP")}
	b, err := New(d).DownloadPreview(ctx, "o1", "f1")
	if err != nil || string(b) != "RIFF....WEBP" {
		t.Fatalf("b=%q err=%v", b, err)
	}
	if d.calls[0].Path != "/api/o1/files/f1/preview" {
		t.Fatalf("path=%s", d.calls[0].Path)
	}
}
