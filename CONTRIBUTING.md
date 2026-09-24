# Contributing to Claude Sync

Thanks for helping. Bug reports, fixes and small focused features are all welcome.

## Before you start

- For anything larger than a small fix, open an issue first so we can agree on the approach.
- Never include real account data in issues, logs or tests: project names, document
  contents, emails or org ids. Use made-up examples such as `Acme: Website`.

## Set up

```bash
git clone https://github.com/ossmalaysia/claude-sync
cd claude-sync
go install github.com/wailsapp/wails/v2/cmd/wails@latest
(cd frontend && npm install)
wails dev
```

## Making a change

1. Write a failing test first. The migration core (`internal/migrate`) is tested
   against an in-memory fake API, so most changes need no browser or account.
2. Keep changes to claude.ai endpoints inside `internal/claudeapi`.
3. Keep the core safety rules:
   - push only creates; it never edits or deletes anything in the target account;
   - progress is saved after every successful write;
   - items are matched by source id, never by name.
4. Run everything before opening a pull request:

   ```bash
   gofmt -l .            # must print nothing
   go vet ./...
   go test -race ./...
   (cd frontend && npm test)
   ```

5. Open a pull request describing what changed and how you tested it. If you
   tested against real claude.ai accounts, say so (without sharing their data).

## Screenshots for the docs

Never take screenshots with a real account. `scripts/demo_data.py` builds demo
data folders with made-up projects, and `scripts/screenshot.sh` (macOS) runs the
app on one of them and captures its window:

```bash
python3 scripts/demo_data.py /tmp/claude-sync-demo
scripts/screenshot.sh /tmp/claude-sync-demo/done docs/images/done.png
```

## Reporting bugs

Use the bug report template. Include your OS, app version, browser, and the
steps to reproduce. Redact any personal or account data from logs.

By contributing you agree that your contributions are licensed under the MIT
License, and you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md).
