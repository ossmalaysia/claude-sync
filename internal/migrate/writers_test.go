package migrate

import (
	"context"
	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
	"testing"
)

type lostWriteAPI struct {
	*fakeAPI
	docLost, fileLost, projectLost bool
}

func (a *lostWriteAPI) CreateProject(ctx context.Context, org string, p claudeapi.NewProject) (claudeapi.Project, error) {
	result, err := a.fakeAPI.CreateProject(ctx, org, p)
	if err == nil && !a.projectLost {
		a.projectLost = true
		return claudeapi.Project{}, httpErr(503, claudeapi.ErrServer)
	}
	return result, err
}

func (a *lostWriteAPI) CreateDoc(ctx context.Context, org, p, name, content string) (claudeapi.Doc, error) {
	d, err := a.fakeAPI.CreateDoc(ctx, org, p, name, content)
	if err == nil && !a.docLost {
		a.docLost = true
		return claudeapi.Doc{}, httpErr(503, claudeapi.ErrServer)
	}
	return d, err
}
func (a *lostWriteAPI) UploadFile(ctx context.Context, org, p, name, mime string, data []byte) (claudeapi.File, error) {
	f, err := a.fakeAPI.UploadFile(ctx, org, p, name, mime, data)
	if err == nil && !a.fileLost {
		a.fileLost = true
		return claudeapi.File{}, httpErr(503, claudeapi.ErrServer)
	}
	return f, err
}
func TestLostUploadRepliesDoNotCreateSecondCopies(t *testing.T) {
	base, st, state, sel := pushFixture(t)
	api := &lostWriteAPI{fakeAPI: base}
	res, err := Push(context.Background(), api, st, state, "org", sel, testOpts(), nil)
	if err != nil || len(res.Failed) != 0 || api.count("CreateDoc") != 3 || api.count("UploadFile") != 1 || api.count("CreateProject") != 2 {
		t.Fatalf("res=%+v err=%v docs=%d files=%d", res, err, api.count("CreateDoc"), api.count("UploadFile"))
	}
}
