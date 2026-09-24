#!/bin/bash
# scripts/screenshot.sh <data-dir> <out.png> (macOS): launch Claude Sync on a
# demo data folder (see demo_data.py) and capture its window for the docs.: launch Claude Sync on a demo data folder and capture its window.
set -e
APP="$(cd "$(dirname "$0")/.." && pwd)/build/bin/Claude Sync.app/Contents/MacOS/Claude Sync"
osascript -e 'quit app "Claude Sync"' 2>/dev/null || true
sleep 1
touch "$1/migration/job.lock" 2>/dev/null || true
CLAUDE_SYNC_DATA="$1" "$APP" >/dev/null 2>&1 &
sleep 5
osascript -e 'tell application "Claude Sync" to activate' || true
sleep 1.5
WID=$(swift "$(dirname "$0")/winid.swift")
screencapture -x -o -l "$WID" "$2"
osascript -e 'quit app "Claude Sync"' || true
