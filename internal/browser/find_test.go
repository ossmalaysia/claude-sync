package browser

import (
	"slices"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestCandidatesEnvOverride(t *testing.T) {
	got := candidates("darwin", env(map[string]string{"CLAUDE_SYNC_BROWSER": "/opt/chrome"}))
	if len(got) != 1 || got[0] != "/opt/chrome" {
		t.Fatalf("got %v", got)
	}
}

func TestCandidatesDarwinPrefersChromeThenEdge(t *testing.T) {
	got := candidates("darwin", env(nil))
	if got[0] != "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" {
		t.Fatalf("first = %s", got[0])
	}
	if !slices.Contains(got, "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge") {
		t.Fatalf("no Edge fallback in %v", got)
	}
}

func TestCandidatesWindowsIncludesEdgeFallback(t *testing.T) {
	got := candidates("windows", env(map[string]string{
		"ProgramFiles":      `C:\Program Files`,
		"ProgramFiles(x86)": `C:\Program Files (x86)`,
		"LocalAppData":      `C:\Users\j\AppData\Local`,
	}))
	if got[0] != `C:\Program Files\Google\Chrome\Application\chrome.exe` {
		t.Fatalf("first = %s", got[0])
	}
	if !slices.Contains(got, `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`) {
		t.Fatalf("no Edge in %v", got)
	}
}
