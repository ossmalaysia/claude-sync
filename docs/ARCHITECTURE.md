# Architecture

Claude Sync copies claude.ai **Projects** from one account to another. It uses
the same internal web API the claude.ai web app uses, called from inside a real
browser tab. There is no official API for Projects, and the official data
export does not include knowledge-file contents.

## Why a real browser

claude.ai sits behind Cloudflare, which rejects plain HTTP clients (Go
`net/http`, curl) by their TLS fingerprint. Claude Sync starts Chrome or Edge
with [chromedp](https://github.com/chromedp/chromedp), waits until the user is
logged in, and runs each request as `fetch()` inside the claude.ai tab. The
request carries the tab's own cookies and a genuine browser fingerprint.

Each account gets its own browser profile, so two logins can coexist and are
remembered between runs. Credentials never pass through Claude Sync: the user
logs in to claude.ai directly in the browser window.

## Flow

```
source account ──pull──▶ local folder ──push──▶ target account
   (read only)        (backup + progress)     (create only)
```

1. **Pull** reads every Project of the source org: details, instructions, text
   docs, and uploaded files. Files already downloaded are skipped, so a stopped
   pull resumes. Pull then reads every chat, follows the branch on screen
   (edited or retried messages leave abandoned branches), and rebuilds the
   final version of each artifact (replaying create, update and rewrite) and
   of each file Claude wrote (create_file or Write, then str_replace or Edit).
   Unchanged chats are skipped on the next pull.
2. **Select** chooses which Projects to send. Names starting with `Personal:`,
   `Family:` or `Travel` start unticked.
3. **Push** creates each selected Project in the target org, then its
   instructions, docs, files, and the artifacts from that Project's chats
   (as docs named `Artifact - <title>`). Progress is saved after every write, so a
   stopped push resumes exactly where it stopped and never creates duplicates.
4. **Verify** compares the target's doc and file counts with the local copy.

Push only ever creates. It never edits or deletes anything in the target.

## Packages

| Package | Responsibility |
|---|---|
| `internal/browser` | Finds Chrome/Edge, drives it with chromedp, runs `fetch()` in the claude.ai tab. The only package that imports chromedp. |
| `internal/claudeapi` | Typed client for the claude.ai endpoints over a small `Doer` interface; maps HTTP status to typed errors. |
| `internal/store` | The local data folder: pulled projects, selection, push state, settings and the job lock. All writes are atomic. |
| `internal/migrate` | Pull, plan, push and verify, with retry and backoff. Works against an `API` interface, so it is tested with an in-memory fake. |
| `internal/app` | Methods the desktop UI calls, and `Status()`, which rebuilds the home screen from disk. |
| `cmd/claude-sync` | Command-line interface to the same core. |
| `frontend` | Svelte UI for the Wails desktop app. |

## Local data

Stored in `~/Library/Application Support/claude-sync/` (macOS) or
`%AppData%\claude-sync\` (Windows):

```
profiles/source, profiles/target   browser profiles (saved logins)
migration/
  manifest.json    pull progress: total, complete, counts
  projects/<id>/   project.json, docs/<id>.json, files/<id>.bin + .json
  chats/<id>.json  artifacts recovered from each chat, with its updated_at
  skills/<id>.skill + .json   personal skills (zip with every file) and metadata
  artifacts-export/<project>/<chat>/   readable copies of every artifact
  memory.md        account memory, for pasting into the target by hand
  selection.json   which projects to send
  state.json       push progress, keyed by source ids
  settings.json    chosen orgs and the last verify result
  job.lock         the pull or push currently running (app or CLI)
```

## Endpoints used

All paths are relative to `https://claude.ai`. `{org}` is an organization uuid.

| Purpose | Request |
|---|---|
| List orgs | `GET /api/organizations` |
| List / get projects | `GET /api/organizations/{org}/projects[/{id}]` |
| Create project | `POST /api/organizations/{org}/projects` |
| Set instructions | `PUT /api/organizations/{org}/projects/{id}` |
| List / create docs | `GET/POST /api/organizations/{org}/projects/{id}/docs` |
| List files | `GET /api/organizations/{org}/projects/{id}/files` |
| Download original file | `GET /api/organizations/{org}/files/{file}/contents` |
| Download image preview | `GET /api/{org}/files/{file}/preview` |
| Upload file | `POST /api/organizations/{org}/projects/{id}/upload` (multipart) |
| Account memory | `GET /api/organizations/{org}/memory` |
| List skills | `GET /api/organizations/{org}/skills/list-skills` |
| Download a skill (zip with every file) | `GET /api/organizations/{org}/skills/download-dot-skill-file?skill_id=&include_blocked=true` |
| Upload a skill | `POST /api/organizations/{org}/skills/upload-skill?overwrite=false&upload_source=customize_upload` (multipart) |
| Import memory | `POST /api/organizations/{org}/melange/import_external` `{"raw_export": ...}` |
| List chats | `GET /api/organizations/{org}/chat_conversations_v2?limit=&offset=` |
| Get a chat with all messages | `GET /api/organizations/{org}/chat_conversations/{id}?tree=True&rendering_mode=messages&render_all_tools=true` |

These endpoints are undocumented and can change without notice.

## Limitations

- **Chats** are not migrated: claude.ai cannot recreate conversations. Their
  artifacts are recovered as described above; code-generated binary files and
  published artifact pages are not.
- **Memory** is sent through claude.ai's memory import, which merges it into
  the target's memory; the same text is never sent twice.
- **Skills**: only personal skills are copied (built-in Anthropic skills are
  skipped); uploads never overwrite a skill of the same name in the target.
- **Images** have no downloadable original; the full-resolution preview is
  saved and uploaded as WebP.
- Files over 30 MB are skipped and reported.
