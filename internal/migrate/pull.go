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
	Projects  int   `json:"projects"`
	Docs      int   `json:"docs"`
	Files     int   `json:"files"`
	Bytes     int64 `json:"bytes"`
	Converted int   `json:"converted"` // images saved as their WebP preview
	// Delta scan: projects read in full, skipped as unchanged, and new.
	ProjectsRead    int `json:"projects_read"`
	ProjectsSkipped int `json:"projects_skipped"`
	ProjectsNew     int `json:"projects_new"`
	ChatsRead       int `json:"chats_read"`    // new or changed chats fetched
	ArtifactsNew    int `json:"artifacts_new"` // artifacts in those chats
	Chats           int `json:"chats"`
	Artifacts       int `json:"artifacts"` // artifacts and Claude-written files recovered from chats
	// ArtifactsNoProject counts artifacts from chats outside any project;
	// they are exported locally but not pushed.
	ArtifactsNoProject int       `json:"artifacts_no_project"`
	Failed             []Failure `json:"failed"`
	Skills             int       `json:"skills"`      // personal skills in the source
	SkillsRead         int       `json:"skills_read"` // new or changed skills downloaded
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
	man.SourceOrg, man.Total, man.Complete, man.Phase = org, len(projects), false, "projects"
	man.PhaseStartedAt, man.PhaseDone, man.PhaseTotal = time.Now().UTC(), 0, len(projects)
	if err := st.SaveManifest(man); err != nil {
		return res, err
	}
	// progress reports the running doc, file and byte totals so the UI can
	// show counts and MB while the pull runs, not only at the end.
	progress := func(done int, msg string) {
		report.emit(Event{Stage: "pull", Done: done, Total: len(projects), Level: "info", Message: msg, Docs: res.Docs, Files: res.Files, Bytes: res.Bytes})
	}
	for i, p := range projects {
		man.PhaseDone = i
		if err := st.SaveManifest(man); err != nil {
			return res, err
		}
		progress(i, p.Name)
		sig := fmt.Sprintf("%s|%d|%d", p.UpdatedAt, p.DocsCount, p.FilesCount)
		saved, known, err := st.LoadProject(p.UUID)
		if err != nil {
			return res, err
		}
		if known && saved.Synced == "" && saved.UpdatedAt == p.UpdatedAt {
			// Pulled by a version without markers: adopt it as complete when
			// the local copy matches the listing's counts.
			if ok, err := matchesCounts(st, p); err != nil {
				return res, err
			} else if ok {
				saved.Synced = sig
				if err := st.SaveProject(saved); err != nil {
					return res, err
				}
			}
		}
		if known && saved.Synced != "" && saved.Synced == sig {
			if err := countLocal(st, p.UUID, &res); err != nil {
				return res, err
			}
			res.Projects++
			res.ProjectsSkipped++
			continue
		}
		if !known {
			res.ProjectsNew++
		}
		tick := func() { progress(i, p.Name) }
		if err := pullProject(ctx, api, st, org, p, i, sig, opts, report, &res, tick); err != nil {
			return res, fmt.Errorf("project %q: %w", p.Name, err)
		}
		res.Projects++
		res.ProjectsRead++
	}
	memory, _, err := withRetry(ctx, opts, func() (string, error) { return api.GetMemory(ctx, org) })
	if err != nil {
		return res, fmt.Errorf("memory: %w", err)
	}
	if err := st.SaveMemory(memory); err != nil {
		return res, err
	}
	// onPhase starts the chat phase once the chats are listed; onProgress
	// saves running counts so the app can show progress and time left.
	onPhase := func(total, toRead int) error {
		man.Phase, man.ChatsTotal = "chats", total
		man.PhaseStartedAt, man.PhaseDone, man.PhaseTotal = time.Now().UTC(), 0, toRead
		man.Chats, man.Artifacts, man.ArtifactsNoProject = res.Chats, res.Artifacts, res.ArtifactsNoProject
		return st.SaveManifest(man)
	}
	onProgress := func(read int) error {
		man.PhaseDone = read
		man.Chats, man.Artifacts, man.ArtifactsNoProject = res.Chats, res.Artifacts, res.ArtifactsNoProject
		return st.SaveManifest(man)
	}
	if err := pullChats(ctx, api, st, org, opts, report, &res, onPhase, onProgress); err != nil {
		return res, fmt.Errorf("chats: %w", err)
	}
	if err := pullSkills(ctx, api, st, org, opts, report, &res); err != nil {
		return res, fmt.Errorf("skills: %w", err)
	}
	man.SourceAccount = sourceEmail(ctx, api)
	man.PulledAt, man.Complete, man.Phase = time.Now().UTC(), true, ""
	man.PhaseDone, man.PhaseTotal = 0, 0
	man.Projects, man.Docs, man.Files = res.Projects, res.Docs, res.Files
	man.Chats, man.Artifacts, man.ArtifactsNoProject = res.Chats, res.Artifacts, res.ArtifactsNoProject
	man.Skills = res.Skills
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

func pullProject(ctx context.Context, api API, st *store.Store, org string, p claudeapi.Project, order int, sig string, opts Options, report Reporter, res *PullResult, tick func()) error {
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
	// Mark the project complete only now, so an interrupted pull re-reads it.
	meta.Synced = sig
	if err := st.SaveProject(meta); err != nil {
		return err
	}
	return nil
}

// chatPageSize is how many chats are listed per request.
var chatPageSize = 50

// chatProgressEvery is how often (in chats read) running counts are saved.
var chatProgressEvery = 10

// pullChats saves the final version of every artifact and Claude-written
// file from every chat, plus a readable copy under artifacts-export/.
// All chats are listed first, so the number still to read is known; chats
// whose updated_at is unchanged since the last pull are not fetched again.
func pullChats(ctx context.Context, api API, st *store.Store, org string, opts Options, report Reporter, res *PullResult,
	onPhase func(total, toRead int) error, onProgress func(read int) error) error {
	projects, err := st.ListProjects()
	if err != nil {
		return err
	}
	projectName := map[string]string{}
	for _, p := range projects {
		projectName[p.UUID] = p.Name
	}

	// 1. List every chat (cheap: one request per page).
	var all []claudeapi.Chat
	for offset := 0; ; {
		type page struct {
			chats []claudeapi.Chat
			more  bool
		}
		pg, _, err := withRetry(ctx, opts, func() (page, error) {
			c, more, err := api.ListChats(ctx, org, offset, chatPageSize)
			return page{c, more}, err
		})
		if err != nil {
			return err
		}
		all = append(all, pg.chats...)
		report.emit(Event{Stage: "pull", Level: "info", Message: fmt.Sprintf("listing chats: %d found", len(all))})
		if !pg.more || len(pg.chats) == 0 {
			break
		}
		offset += len(pg.chats)
	}

	// 2. Unchanged chats are counted now; only the rest is work to do.
	var toRead []claudeapi.Chat
	for _, c := range all {
		saved, ok, err := st.LoadChat(c.UUID)
		if err != nil {
			return err
		}
		if ok && saved.UpdatedAt == c.UpdatedAt && saved.Transcript != "" {
			res.Chats++
			res.Artifacts += len(saved.Artifacts)
			if saved.ProjectUUID == "" {
				res.ArtifactsNoProject += len(saved.Artifacts)
			}
			continue
		}
		toRead = append(toRead, c)
	}
	if err := onPhase(len(all), len(toRead)); err != nil {
		return err
	}

	// 3. Read the changed chats.
	for i, c := range toRead {
		if err := ctx.Err(); err != nil {
			return err
		}
		detail, _, err := withRetry(ctx, opts, func() (claudeapi.ChatDetail, error) { return api.GetChat(ctx, org, c.UUID) })
		if err != nil {
			if errors.Is(err, claudeapi.ErrAuth) || errors.Is(err, claudeapi.ErrSessionGone) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			fail := Failure{Project: projectName[c.ProjectUUID], Item: "chat " + c.Name, Error: err.Error()}
			res.Failed = append(res.Failed, fail)
			report.emit(Event{Stage: "pull", Level: "warn", Message: fail.Item + ": " + fail.Error})
			continue
		}
		branch := Branch(detail)
		rec := store.ChatRecord{UUID: c.UUID, Name: c.Name, ProjectUUID: c.ProjectUUID, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
			Artifacts: ExtractArtifacts(branch), Transcript: Transcript(c, branch), TranscriptAs: TranscriptFileName(c)}
		folder := projectName[c.ProjectUUID]
		if folder == "" {
			folder = "No project"
		}
		for _, a := range rec.Artifacts {
			if err := st.ExportArtifact(folder, c.Name, a); err != nil {
				return err
			}
		}
		// Saved last: its presence with this updated_at means "done".
		if err := st.SaveChat(rec); err != nil {
			return err
		}
		res.Chats++
		res.ChatsRead++
		res.Artifacts += len(rec.Artifacts)
		res.ArtifactsNew += len(rec.Artifacts)
		if rec.ProjectUUID == "" {
			res.ArtifactsNoProject += len(rec.Artifacts)
		}
		read := i + 1
		if read%chatProgressEvery == 0 {
			if err := onProgress(read); err != nil {
				return err
			}
		}
		report.emit(Event{Stage: "pull", Level: "info", Done: read, Total: len(toRead), Message: fmt.Sprintf("chats: %d of %d read, %d artifacts", read, len(toRead), res.Artifacts)})
	}
	return onProgress(len(toRead))
}

// countLocal adds a skipped project's local docs and files to the totals.
func countLocal(st *store.Store, project string, res *PullResult) error {
	docs, err := st.ListDocs(project)
	if err != nil {
		return err
	}
	files, err := st.ListFiles(project)
	if err != nil {
		return err
	}
	res.Docs += len(docs)
	res.Files += len(files)
	for _, f := range files {
		res.Bytes += f.SizeBytes
	}
	return nil
}

// matchesCounts reports whether the local copy has as many docs and files as
// the listing says the project has.
func matchesCounts(st *store.Store, p claudeapi.Project) (bool, error) {
	docs, err := st.ListDocs(p.UUID)
	if err != nil {
		return false, err
	}
	files, err := st.ListFiles(p.UUID)
	if err != nil {
		return false, err
	}
	return len(docs) == p.DocsCount && len(files) == p.FilesCount, nil
}
