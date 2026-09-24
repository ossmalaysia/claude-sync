// Package browser drives a real Chrome/Edge window with chromedp and
// performs claude.ai API calls as fetch() inside it.
package browser

import (
	"errors"
	"os"
	"runtime"
)

var ErrNoBrowser = errors.New("no Chrome, Edge or Chromium found; install Google Chrome from https://www.google.com/chrome/ or set CLAUDE_SYNC_BROWSER")

func candidates(goos string, getenv func(string) string) []string {
	if p := getenv("CLAUDE_SYNC_BROWSER"); p != "" {
		return []string{p}
	}
	switch goos {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	case "windows":
		var out []string
		for _, base := range []string{getenv("ProgramFiles"), getenv("ProgramFiles(x86)"), getenv("LocalAppData")} {
			if base != "" {
				out = append(out, base+`\Google\Chrome\Application\chrome.exe`)
			}
		}
		for _, base := range []string{getenv("ProgramFiles(x86)"), getenv("ProgramFiles")} {
			if base != "" {
				out = append(out, base+`\Microsoft\Edge\Application\msedge.exe`)
			}
		}
		return out
	default:
		return []string{"/usr/bin/google-chrome", "/usr/bin/chromium", "/usr/bin/microsoft-edge"}
	}
}

// FindBrowser returns the first installed Chromium-based browser.
func FindBrowser() (string, error) {
	for _, c := range candidates(runtime.GOOS, os.Getenv) {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	return "", ErrNoBrowser
}
