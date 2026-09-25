First public test release of Claude Sync: copy your claude.ai Projects, artifacts, personal skills and memory from one account to another (for example, personal to your team account).

**This is an early test version.** Please [report anything odd](https://github.com/ossmalaysia/claude-sync/issues/new/choose); the app's version is shown at the bottom of its window.

## Download

| System | File |
|---|---|
| macOS (Apple Silicon and Intel) | `Claude-Sync-macOS.dmg` |
| Windows 10/11 (installer) | `Claude-Sync-Windows-installer.exe` |
| Windows (single file, no install) | `Claude-Sync-Windows.exe` |

You need Google Chrome or Microsoft Edge. The app is not code-signed yet:
- **macOS:** right-click **Claude Sync** in Applications, choose **Open**, then **Open** again.
- **Windows:** if SmartScreen appears, click **More info**, then **Run anyway**.

Code signing: Windows releases are signed with free code signing provided by [SignPath.io](https://about.signpath.io), certificate by [SignPath Foundation](https://signpath.org) (being set up; see the [code signing policy](https://github.com/ossmalaysia/claude-sync#code-signing-policy)).

Step-by-step guide with screenshots: [docs/USER_GUIDE.md](https://github.com/ossmalaysia/claude-sync/blob/main/docs/USER_GUIDE.md)

## What it copies

- Projects with instructions, docs and files (images as full-resolution previews)
- Artifacts from your chats, at their final version
- Your own skills, with every file they contain (built-in Anthropic skills are not copied)
- Memory, through claude.ai's memory import
- **Scan & sync changes** afterwards sends only what is new

It never edits or deletes anything that already exists in either account. Chats themselves cannot be recreated in another account.

Claude Sync is an independent open-source project, not an Anthropic product, and uses claude.ai's web interface, so a claude.ai change can break it.
