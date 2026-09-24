package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
)

type smokeAPI interface {
	CreateProject(ctx context.Context, org string, p claudeapi.NewProject) (claudeapi.Project, error)
	SetInstructions(ctx context.Context, org, project, text string) error
	CreateDoc(ctx context.Context, org, project, fileName, content string) (claudeapi.Doc, error)
	UploadFile(ctx context.Context, org, project, fileName, mime string, data []byte) (claudeapi.File, error)
	GetProject(ctx context.Context, org, project string) (claudeapi.Project, error)
	DeleteProject(ctx context.Context, org, project string) error
}

var pixelPNG, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")

// runSmoke checks every write endpoint on a throwaway project, then deletes it.
func runSmoke(ctx context.Context, api smokeAPI, org string, out io.Writer) error {
	p, err := api.CreateProject(ctx, org, claudeapi.NewProject{Name: "claude-sync smoke test", Description: "temporary; deleted automatically", IsPrivate: true})
	if err != nil {
		return fmt.Errorf("create project: %w", err)
	}
	fmt.Fprintln(out, "created test project", p.UUID)
	var failed error
	step := func(name string, err error) {
		status := "ok"
		if err != nil {
			status = "FAIL: " + err.Error()
			if failed == nil {
				failed = fmt.Errorf("%s: %w", name, err)
			}
		}
		fmt.Fprintf(out, "  %-13s %s\n", name, status)
	}
	step("instructions", api.SetInstructions(ctx, org, p.UUID, "smoke test"))
	_, err = api.CreateDoc(ctx, org, p.UUID, "smoke.md", "# smoke")
	step("doc", err)
	_, err = api.UploadFile(ctx, org, p.UUID, "smoke.png", "image/png", pixelPNG)
	step("upload", err)
	if failed == nil {
		got, err := api.GetProject(ctx, org, p.UUID)
		if err == nil && (got.DocsCount != 1 || got.FilesCount != 1 || got.PromptTemplate != "smoke test") {
			err = fmt.Errorf("read back docs=%d files=%d instructions=%q", got.DocsCount, got.FilesCount, got.PromptTemplate)
		}
		step("read back", err)
	}
	step("delete", api.DeleteProject(ctx, org, p.UUID))
	if failed != nil {
		return failed
	}
	fmt.Fprintln(out, "PASS")
	return nil
}
