package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
)

type fakeSmoke struct {
	uploadErr error
	deleted   bool
	docs      int
	files     int
	prompt    string
}

func (f *fakeSmoke) CreateProject(context.Context, string, claudeapi.NewProject) (claudeapi.Project, error) {
	return claudeapi.Project{UUID: "tp1"}, nil
}
func (f *fakeSmoke) SetInstructions(_ context.Context, _, _, text string) error {
	f.prompt = text
	return nil
}
func (f *fakeSmoke) CreateDoc(context.Context, string, string, string, string) (claudeapi.Doc, error) {
	f.docs++
	return claudeapi.Doc{UUID: "d"}, nil
}
func (f *fakeSmoke) UploadFile(context.Context, string, string, string, string, []byte) (claudeapi.File, error) {
	if f.uploadErr != nil {
		return claudeapi.File{}, f.uploadErr
	}
	f.files++
	return claudeapi.File{FileUUID: "f"}, nil
}
func (f *fakeSmoke) GetProject(context.Context, string, string) (claudeapi.Project, error) {
	return claudeapi.Project{UUID: "tp1", DocsCount: f.docs, FilesCount: f.files, PromptTemplate: f.prompt}, nil
}
func (f *fakeSmoke) DeleteProject(context.Context, string, string) error {
	f.deleted = true
	return nil
}

func TestSmokePasses(t *testing.T) {
	f := &fakeSmoke{}
	var out bytes.Buffer
	if err := runSmoke(context.Background(), f, "org", &out); err != nil {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
	if !f.deleted || !strings.Contains(out.String(), "PASS") {
		t.Fatalf("deleted=%v out=%s", f.deleted, out.String())
	}
}

func TestSmokeFailureStillDeletes(t *testing.T) {
	f := &fakeSmoke{uploadErr: errors.New("HTTP 413")}
	var out bytes.Buffer
	err := runSmoke(context.Background(), f, "org", &out)
	if err == nil || !strings.Contains(err.Error(), "upload") {
		t.Fatalf("err=%v", err)
	}
	if !f.deleted {
		t.Fatal("test project must be deleted even when a step fails")
	}
}
