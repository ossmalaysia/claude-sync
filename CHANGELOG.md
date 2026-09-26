# Changelog

## 0.1.3 (2026-09-26)

- What to copy: after the first download, a settings page shows real counts and one switch for each kind of content: artifacts from chats, your own skills and memory (on by default), chats as documents and personal projects (off by default). Claude Sync sends nothing until the page has been saved once, and the new Settings button reopens it. Turning something off never removes what is already in the target. The home screen shows what is left out. This replaces the separate questions about personal projects and chats.
- Chats as documents (opt-in): each chat in a selected project is added to that project as a Markdown transcript (what you and Claude wrote, without hidden reasoning). Chats downloaded by earlier versions are read once more to build their transcript.
- Where to find everything: a screen, opened from the Send to target row, shows where projects, artifacts, chats, skills and memory are in the target account, and what stays on this computer.

## 0.1.2 (2026-09-25)

- A first-use notice explains what Claude Sync does and that the user is responsible for what is copied into the target account; nothing runs until it is accepted.

## 0.1.1 (2026-09-25)

- Using Claude Sync on a second computer no longer sends everything again: before creating a project, the push looks for the same project in the target and sends only the docs, files and artifacts it does not have yet. Existing instructions are never overwritten.
- Footer links open through the app itself, limited to its own pages.

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
