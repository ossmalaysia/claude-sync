package migrate

import (
	"context"
	"errors"
	"fmt"
	"time"

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
	Waiting    int    `json:"waiting"` // artifacts not sent yet; the only difference when set
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
// Verify checks the selected projects (or, when only is non-nil, just those
// source projects) in the target, and records each result in verify.json.
func Verify(ctx context.Context, api API, st *store.Store, state *store.State, org string, opts Options, report Reporter, only map[string]bool) ([]VerifyRow, error) {
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
	arts, err := artifactsToSend(st)
	if err != nil {
		return nil, err
	}
	chats, err := transcriptsToSend(st)
	if err != nil {
		return nil, err
	}
	var check []store.ProjectMeta
	for _, p := range projects {
		if sel[p.UUID] && (only == nil || only[p.UUID]) {
			check = append(check, p)
		}
	}
	for i, p := range check {
		report.emit(Event{Stage: "verify", Done: i, Total: len(check), Level: "info", Message: p.Name})
		ps := state.Projects[p.UUID]
		pushed := ps != nil && ps.Target != ""
		row := VerifyRow{SourceUUID: p.UUID, Name: p.Name}
		if row.WantDocs, row.WantFiles, err = expected(st, p.UUID, opts); err != nil {
			return rows, err
		}
		wantArts := len(arts[p.UUID]) // artifacts are added as docs
		if arts == nil && ps != nil {
			wantArts = countDone(ps.Artifacts) // sent before the user switched artifacts off
		}
		row.WantDocs += wantArts
		wantChats := len(chats[p.UUID]) // and so are chat transcripts, when chosen
		if wantChats == 0 && ps != nil {
			wantChats = countDone(ps.Chats) // sent before the user switched chats off
		}
		row.WantDocs += wantChats
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
			// If the only difference is artifacts not sent yet, say so.
			unsentArts := wantArts - countDone(ps.Artifacts)
			unsent := unsentArts + wantChats - countDone(ps.Chats)
			if !row.OK && unsent > 0 && row.GotDocs == row.WantDocs-unsent && row.GotFiles == row.WantFiles {
				row.Waiting = unsent
				switch {
				case unsent != unsentArts:
					row.Error = fmt.Sprintf("%d artifacts or chats not sent yet", unsent)
				case unsent == 1:
					row.Error = "1 artifact not sent yet"
				default:
					row.Error = fmt.Sprintf("%d artifacts not sent yet", unsent)
				}
			}
		}
		rows = append(rows, row)
	}
	report.emit(Event{Stage: "verify", Done: len(check), Total: len(check), Level: "info", Message: "verify complete"})
	return rows, st.RecordVerify(VerifyRows(rows).toResults())
}

type VerifyRows []VerifyRow

// toResults turns rows into per-project results stamped with the time now.
func (rows VerifyRows) toResults() map[string]store.VerifyResult {
	now := time.Now().UTC()
	out := make(map[string]store.VerifyResult, len(rows))
	for _, r := range rows {
		out[r.SourceUUID] = store.VerifyResult{OK: r.OK, Waiting: r.Waiting, Error: r.Error, CheckedAt: now}
	}
	return out
}

func countDone(m map[string]*store.ItemState) int {
	n := 0
	for _, it := range m {
		if it.Status == store.StatusDone {
			n++
		}
	}
	return n
}
