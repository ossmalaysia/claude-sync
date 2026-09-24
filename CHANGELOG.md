# Changelog

## 0.1.0 (2026-09-25)

First public release.

- Desktop app for macOS and Windows (Wails + Svelte) and a CLI.
- Pull a claude.ai org's Projects (instructions, docs, files) and memory to a local folder.
- Choose projects, then push them to another account or org. Create-only, resumable, no duplicates.
- Stop and resume pull and push; the home screen rebuilds its status from disk.
- Verify doc and file counts in the target.
- Artifacts and Claude-written files are recovered from chats (final versions), added to the matching Project, and exported locally.
- Personal skills are synced as complete packages (SKILL.md plus every file); built-in Anthropic skills are skipped.
- Memory is sent through claude.ai's memory import, once per change.
- Scan & sync changes: a delta scan that reads only new or changed projects, chats and skills, then sends them.
- Pull and push progress with totals, a progress bar and time left; stop and resume anywhere.
- Report an issue link and the version number in the app.
- Images are migrated as full-resolution WebP previews (claude.ai keeps no original).
