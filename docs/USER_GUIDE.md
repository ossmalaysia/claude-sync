# Claude Sync user guide

Claude Sync copies your claude.ai Projects, the artifacts from your chats, your
own skills, and your memory from one account to another, and can copy your chats
as documents. For example, from a personal account to your company's team
account. This guide takes about ten minutes the first
time.

> Claude Sync is an independent open-source tool, not an Anthropic product. It
> uses claude.ai's web interface through a browser window, so it can stop
> working if claude.ai changes.

## 1. Install

**You need:** macOS 10.13 or later, or Windows 10/11, and **Google Chrome** or
**Microsoft Edge**.

Download the latest release from
<https://github.com/ossmalaysia/claude-sync/releases>:

| System | File | First launch |
|---|---|---|
| macOS (Apple Silicon and Intel) | `Claude-Sync-macOS.dmg` | Open the `.dmg` and drag **Claude Sync** to Applications. The app is not code-signed yet, so the first time, **right-click** it and choose **Open**, then **Open** again. |
| Windows | `Claude-Sync-Windows-installer.exe` (or `Claude-Sync-Windows.exe` to run without installing) | Run it. If SmartScreen says "Windows protected your PC", click **More info**, then **Run anyway**. This appears because the app is not code-signed yet; see the [note for Windows users](../README.md#note-for-windows-users). |

## 2. Connect the account you are moving **from**

![First launch](images/welcome.png)

The first time you open Claude Sync, it shows a short notice, **Before you
start**: what the app does,
that it only adds to the target account and never edits or deletes, and that you
are responsible for what you copy there (for example, your employer's rules for a
company account). Tick the box and click **Continue**. You choose what to copy
later, after the first download.

1. Click **Connect source account**. A Chrome (or Edge) window opens on claude.ai.
2. Log in to the account you are moving **from**, as you normally would.
   Claude Sync never sees your password. The login is saved in a private browser
   profile, so you only do this once.
3. Pick the organization if the account has more than one.

## 3. Download

Click **Start pull**. Claude Sync reads your source account (it never changes
it) and saves a copy on your computer:

- every Project with its instructions, docs and files
- your chats, and the artifacts from them at their final version
- your own skills, with every file they contain (built-in Anthropic skills are
  left out, since every account already has them)
- your memory

The first download can take a while if you have many chats. The screen shows
progress and the time left, and you can stop at any time: **Resume** continues
where it stopped.

![Reading chats, with progress and time left](images/syncing.png)

## 4. Choose what to copy

When the first download finishes, Claude Sync opens **What to copy**. It shows
what it found in your source account, with real counts, and one switch for each
kind of content:

| What | Default | What it does |
|---|---|---|
| Projects | Always copied | Shows how many projects are selected. **Choose projects** opens the list, where you tick the Projects to move. |
| Artifacts from chats | On | Adds each artifact from a chat in a Project to that Project as a document. |
| Chats as documents | Off | Adds each chat in a Project to that Project as a document: what you and Claude wrote, without hidden reasoning. |
| Your own skills | On | Uploads your skills with all their files. |
| Memory | On | Sends your memory to the target account. |
| Personal projects | Off | Projects whose names start with `Personal:`, `Family:` or `Travel`. The page lists their names. |

Chats are off by default because they can be long and often hold things you
would not want in a shared account. Personal projects are off for the same
reason: the target is often a company account. Turn either on if you want them.

Click **Save**. You must save this page once before Claude Sync sends anything.

To change your choices later, click **Settings** at the top of the app. It
opens the same page. Turning something off stops it being sent from then on;
nothing already in the target account is removed. The home screen always shows
what is left out, for example "16 personal left out" next to **Choose
projects**, or "Not copied" on a row you switched off, with a way to change it.

## 5. Connect the account you are moving **to**, then send

![Ready to send](images/ready.png)

1. Click **Connect target account** and log in, in the second window (SSO works).
   Pick the organization, for example your team. Company SSO logins are often
   not remembered by the browser, so you may be asked to sign in again each time
   you start Claude Sync; that is expected.
2. Click **Start push**. Projects are created one request at a time, then your
   skills are uploaded. Nothing that already exists in the target is edited or
   deleted: a skill with the same name already in the target is left as it is.
   You can stop and resume.
3. When it finishes, **Check the target** confirms that every Project has the
   expected number of docs and files.
4. Next to **Memory**, click **Send**. claude.ai merges your memory into the
   target account in the background; it appears under Settings, Memory after a
   few minutes.

## 6. Keep them in sync

![Everything sent and verified](images/done.png)

If you keep using the old account, click **Scan & sync changes** now and then.
It reads only what is new or changed since last time (usually under a minute),
sends it, and checks the Projects it changed. The line under the button says
what the last sync found and sent. It follows your choices in **What to copy**.

## Where to find everything

After a send, the **Send to target** row has a link, **Where to find
everything**. It opens a screen that shows where each kind of content is in the
target account, using names from your own migration. Its links open in your
default browser, so that browser must be signed in to the target account. If a
project does not open, switch to the target account in claude.ai and try again.

In the target claude.ai account:

- **Projects** are under Projects, with the same names as before.
- **Artifacts** are documents in their Project's knowledge, named
  "Artifact - …".
- **Chats**, if you turned them on, are documents in their Project's knowledge,
  named "Chat - title (date)". Ask Claude about them in a new chat in the
  Project.
- **Skills** are under Customize, Skills.
- **Memory** is under Settings, Memory. claude.ai merges it in the background,
  so it can take a few minutes to appear.

Artifacts and chats from chats outside any Project have no Project to go to, so
they stay on this computer. Click **Open folder** next to *Artifacts from chats*
to see the artifacts.

## What is not copied

- Your downloaded copy is in `~/Library/Application Support/claude-sync` (macOS)
  or `%AppData%\claude-sync` (Windows).
- A chat cannot be recreated as a chat in another account. With **Chats as
  documents** on, chats in a Project are copied as documents instead.
- **Images** in Project knowledge are copied as full-resolution WebP previews
  (claude.ai keeps no original).
- Published artifact pages, and files that Claude produced by running code (for
  example a generated `.docx`), are not copied yet.
- A skill or document you edit later in the old account is not re-sent; only
  new items are. Claude Sync lists such changes instead.
- Files larger than 30 MB are skipped and listed.

## Troubleshooting

| What you see | What to do |
|---|---|
| "No Chrome, Edge or Chromium found" | Install Google Chrome, or set `CLAUDE_SYNC_BROWSER` to another Chromium-based browser. |
| "login expired" or a browser window closed | Click the same button again and log in to the window that opens. Progress is kept. |
| Check the target says projects "only need their artifacts sent" | Those Projects are not selected, or a push has not run since the last pull. Choose them and push. |
| Sending says "review What to copy before sending" | Click **Settings**, check the switches and click **Save**. |
| You want a trial run without touching your real data folder | Start the app with `CLAUDE_SYNC_DATA=/some/empty/folder`. |

Found a problem? Please open an issue:
<https://github.com/ossmalaysia/claude-sync/issues>. Remove project names, emails
and document contents from anything you paste.
