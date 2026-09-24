package migrate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
	"github.com/ossmalaysia/claude-sync/internal/store"
)

var _ API = (*claudeapi.Client)(nil)

type PullResult struct {
	Projects  int       `json:"projects"`
	Docs      int       `json:"docs"`
	Files     int       `json:"files"`
	Bytes     int64     `json:"bytes"`
	Converted int       `json:"converted"` // images saved as their WebP preview
	Failed    []Failure `json:"failed"`
}

func mimeFor(name string) string {
	if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); t != "" {
		return t
	}
	return "application/octet-stream"
}

// Pull copies every project of the source org into the store. It is
// read-only against the source and safe to re-run: files already on disk
// are not downloaded again.
func Pull(ctx context.Context, api API, st *store.Store, org string, opts Options, report Reporter) (PullResult, error) {
	var res PullResult
	projects, _, err := withRetry(ctx, opts, func() ([]claudeapi.Project, error) { return api.ListProjects(ctx, org) })
	if err != nil {
		return res, fmt.Errorf("list projects: %w", err)
	}
	// Record the pull as started-but-incomplete first, so a stopped pull is
	// visible as partial and resumable (files already on disk are skipped).
	man, err := st.LoadManifest()
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return res, err
	}
	man.SourceOrg, man.Total, man.Complete = org, len(projects), false
	if err := st.SaveManifest(man); err != nil {
		return res, err
	}
	// progress reports the running doc, file and byte totals so the UI can
	// show counts and MB while the pull runs, not only at the end.
	progress := func(done int, msg string) {
		report.emit(Event{Stage: "pull", Done: done, Total: len(projects), Level: "info", Message: msg, Docs: res.Docs, Files: res.Files, Bytes: res.Bytes})
	}
	for i, p := range projects {
		progress(i, p.Name)
		tick := func() { progress(i, p.Name) }
		if err := pullProject(ctx, api, st, org, p, i, opts, report, &res, tick); err != nil {
			return res, fmt.Errorf("project %q: %w", p.Name, err)
		}
		res.Projects++
	}
	memory, _, err := withRetry(ctx, opts, func() (string, error) { return api.GetMemory(ctx, org) })
	if err != nil {
		return res, fmt.Errorf("memory: %w", err)
	}
	if err := st.SaveMemory(memory); err != nil {
		return res, err
	}
	man.SourceAccount = sourceEmail(ctx, api)
	man.PulledAt, man.Complete = time.Now().UTC(), true
	man.Projects, man.Docs, man.Files = res.Projects, res.Docs, res.Files
	if err := st.SaveManifest(man); err != nil {
		return res, err
	}
	progress(len(projects), "pull complete")
	return res, nil
}

// accountAPI is implemented by the claudeapi client; it is optional so that
// API stays the minimal set of endpoints the migration depends on.
type accountAPI interface {
	GetAccount(ctx context.Context) (claudeapi.Account, error)
}

// sourceEmail returns the source login's email for manifest.json, or "" if
// the API cannot tell. It never fails the pull.
func sourceEmail(ctx context.Context, api API) string {
	a, ok := api.(accountAPI)
	if !ok {
		return ""
	}
	acc, err := a.GetAccount(ctx)
	if err != nil {
		return ""
	}
	return acc.EmailAddress
}

func pullProject(ctx context.Context, api API, st *store.Store, org string, p claudeapi.Project, order int, opts Options, report Reporter, res *PullResult, tick func()) error {
	full, _, err := withRetry(ctx, opts, func() (claudeapi.Project, error) { return api.GetProject(ctx, org, p.UUID) })
	if err != nil {
		return err
	}
	meta := store.ProjectMeta{
		UUID: p.UUID, Name: full.Name, Description: full.Description, IsPrivate: full.IsPrivate,
		IsStarred: full.IsStarred, PromptTemplate: full.PromptTemplate, UpdatedAt: full.UpdatedAt, Order: order,
	}
	if err := st.SaveProject(meta); err != nil {
		return err
	}
	docs, _, err := withRetry(ctx, opts, func() ([]claudeapi.Doc, error) { return api.ListDocs(ctx, org, p.UUID) })
	if err != nil {
		return err
	}
	for _, d := range docs {
		if err := st.SaveDoc(p.UUID, store.DocRecord{UUID: d.UUID, FileName: d.FileName, Content: d.Content}); err != nil {
			return err
		}
		res.Docs++
	}
	tick()
	files, _, err := withRetry(ctx, opts, func() ([]claudeapi.File, error) { return api.ListFiles(ctx, org, p.UUID) })
	if err != nil {
		return err
	}
	for _, f := range files {
		if st.HasFile(p.UUID, f.FileUUID) {
			res.Files++
			res.Bytes += f.SizeBytes
			continue
		}
		name, mimeType, convertedFrom := f.FileName, mimeFor(f.FileName), ""
		data, _, err := withRetry(ctx, opts, func() ([]byte, error) { return api.DownloadFile(ctx, org, f.FileUUID) })
		if errors.Is(err, claudeapi.ErrNotFound) && f.FileKind == "image" {
			// No original is kept for images; the full-size preview is the best copy.
			data, _, err = withRetry(ctx, opts, func() ([]byte, error) { return api.DownloadPreview(ctx, org, f.FileUUID) })
			if err == nil {
				convertedFrom = f.FileName
				name = strings.TrimSuffix(f.FileName, filepath.Ext(f.FileName)) + ".webp"
				mimeType = "image/webp"
				res.Converted++
				report.emit(Event{Stage: "pull", Level: "warn", Message: p.Name + ": " + f.FileName + ": original not available, saved full-size preview as " + name})
			}
		}
		if err != nil {
			if errors.Is(err, claudeapi.ErrAuth) || errors.Is(err, claudeapi.ErrSessionGone) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			fail := Failure{Project: p.Name, Item: "file " + f.FileName, Error: err.Error()}
			res.Failed = append(res.Failed, fail)
			report.emit(Event{Stage: "pull", Level: "warn", Message: fail.Project + ": " + fail.Item + ": " + fail.Error})
			continue
		}
		m := store.FileMeta{UUID: f.FileUUID, FileName: name, FileKind: f.FileKind, SizeBytes: int64(len(data)), Mime: mimeType, ConvertedFrom: convertedFrom}
		if err := st.SaveFile(p.UUID, m, data); err != nil {
			return err
		}
		res.Files++
		res.Bytes += int64(len(data))
		tick()
	}
	return nil
}
