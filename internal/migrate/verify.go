package migrate

import (
	"context"
	"errors"

	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
	"github.com/ossmalaysia/claude-sync/internal/store"
)

type VerifyRow struct {
	SourceUUID string `json:"source_uuid"`
	Name       string `json:"name"`
	TargetUUID string `json:"target_uuid"`
	WantDocs   int    `json:"want_docs"`
	GotDocs    int    `json:"got_docs"`
	WantFiles  int    `json:"want_files"`
	GotFiles   int    `json:"got_files"`
	OK         bool   `json:"ok"`
	Error      string `json:"error"`
}

// expected returns how many docs and files the target should hold for p,
// taken from the local copy. Files over MaxFileBytes are never pushed, so
// they are not expected.
func expected(st *store.Store, p string, opts Options) (docs, files int, err error) {
	ds, err := st.ListDocs(p)
	if err != nil {
		return 0, 0, err
	}
	fs, err := st.ListFiles(p)
	if err != nil {
		return 0, 0, err
	}
	for _, f := range fs {
		if f.SizeBytes <= opts.MaxFileBytes {
			files++
		}
	}
	return len(ds), files, nil
}

// Verify compares every selected (or already pushed) project's target
// docs_count and files_count with the local copy. A selected project that
// has no target is reported as a failed row, never skipped.
func Verify(ctx context.Context, api API, st *store.Store, state *store.State, org string, opts Options) ([]VerifyRow, error) {
	if state.TargetOrg != "" && state.TargetOrg != org {
		return nil, ErrOrgMismatch
	}
	projects, err := st.ListProjects()
	if err != nil {
		return nil, err
	}
	sel, err := st.LoadSelection()
	if err != nil {
		return nil, err
	}
	var rows []VerifyRow
	for _, p := range projects {
		ps := state.Projects[p.UUID]
		pushed := ps != nil && ps.Target != ""
		if !sel[p.UUID] && !pushed {
			continue
		}
		row := VerifyRow{SourceUUID: p.UUID, Name: p.Name}
		if row.WantDocs, row.WantFiles, err = expected(st, p.UUID, opts); err != nil {
			return rows, err
		}
		if !pushed {
			row.Error = "project not created in target"
			if ps != nil && ps.Error != "" {
				row.Error += ": " + ps.Error
			}
			rows = append(rows, row)
			continue
		}
		row.TargetUUID = ps.Target
		got, _, err := withRetry(ctx, opts, func() (claudeapi.Project, error) { return api.GetProject(ctx, org, ps.Target) })
		switch {
		case errors.Is(err, claudeapi.ErrNotFound):
			row.Error = "project missing in target"
		case err != nil:
			if stopErr(err) != nil || errors.Is(err, claudeapi.ErrAuth) {
				return rows, err
			}
			row.Error = err.Error()
		default:
			row.GotDocs, row.GotFiles = got.DocsCount, got.FilesCount
			row.OK = row.GotDocs == row.WantDocs && row.GotFiles == row.WantFiles
		}
		rows = append(rows, row)
	}
	return rows, nil
}
