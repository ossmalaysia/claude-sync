# Security Policy

## Reporting a vulnerability

Please do not open a public issue for security problems. Report them privately
through [GitHub security advisories](https://github.com/ossmalaysia/claude-sync/security/advisories/new).
We aim to respond within 7 days.

## How Claude Sync handles your data

- You log in to claude.ai directly in a browser window. Claude Sync never sees
  or stores your password.
- Login sessions stay in browser profiles on your computer
  (`profiles/` in the data folder). Anyone with access to that folder can use
  those sessions, so protect it like any browser profile. Delete it to log out.
- Downloaded projects, documents and files are stored unencrypted in the data
  folder on your computer. Delete the `migration/` folder when you no longer
  need the copy.
- Claude Sync talks only to claude.ai. It has no server and sends no telemetry.
