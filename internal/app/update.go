package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

const releaseAPI = "https://api.github.com/repos/ossmalaysia/claude-sync/releases?per_page=100"
const releaseBase = "https://github.com/ossmalaysia/claude-sync/releases/tag/"
const updateCacheFor = 6 * time.Hour

type UpdateInfo struct {
	Status         string `json:"status"` // available | current | development | unavailable
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	ReleaseURL     string `json:"release_url"`
	PublishedAt    string `json:"published_at"`
	CheckedAt      string `json:"checked_at"`
}
type publishedRelease struct {
	Tag         string    `json:"tag_name"`
	Draft       bool      `json:"draft"`
	PublishedAt time.Time `json:"published_at"`
}

var versionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

type releaseVersion struct {
	parts       [3]uint64
	development bool
}

func parseVersion(tag string) (releaseVersion, bool) {
	var v releaseVersion
	m := versionPattern.FindStringSubmatch(tag)
	if m == nil {
		return v, false
	}
	for i := range v.parts {
		n, err := strconv.ParseUint(m[i+1], 10, 64)
		if err != nil {
			return v, false
		}
		v.parts[i] = n
	}
	v.development = m[4] != ""
	return v, true
}
func newerVersion(a, b releaseVersion) bool {
	for i := range a.parts {
		if a.parts[i] != b.parts[i] {
			return a.parts[i] > b.parts[i]
		}
	}
	return !a.development && b.development
}

// CheckForUpdates reads public release metadata only. It never sends Claude
// credentials, account identifiers, local contents or telemetry to GitHub.
func (a *App) CheckForUpdates(force bool) UpdateInfo { return a.checkUpdates(a.ctx, Version, force) }

func (a *App) checkUpdates(ctx context.Context, current string, force bool) UpdateInfo {
	a.updateMu.Lock()
	defer a.updateMu.Unlock()
	if !force && !a.updateCheckedAt.IsZero() && time.Since(a.updateCheckedAt) < updateCacheFor {
		return a.updateInfo
	}
	now := time.Now().UTC()
	info := UpdateInfo{Status: "unavailable", CurrentVersion: current, CheckedAt: now.Format(time.RFC3339)}
	a.updateCheckedAt = now
	defer func() { a.updateInfo = info }()
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseAPI, nil)
	if err != nil {
		return info
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Claude-Sync-update-check")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := a.updateClient.Do(req)
	if err != nil {
		return info
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return info
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(body) > 2<<20 {
		return info
	}
	var releases []publishedRelease
	if json.Unmarshal(body, &releases) != nil {
		return info
	}
	var latest releaseVersion
	found := false
	for _, r := range releases {
		candidate, ok := parseVersion(r.Tag)
		// This repository marks its v0.x numeric releases as GitHub prereleases.
		// Offer those published builds, but exclude semantic RC/dev tags and drafts.
		if !ok || candidate.development || r.Draft || r.PublishedAt.IsZero() {
			continue
		}
		if !found || newerVersion(candidate, latest) {
			latest = candidate
			found = true
			info.LatestVersion = r.Tag
			info.PublishedAt = r.PublishedAt.Format(time.RFC3339)
		}
	}
	if !found {
		return info
	}
	info.ReleaseURL = releaseBase + info.LatestVersion
	if installed, ok := parseVersion(current); !ok {
		info.Status = "development"
	} else if newerVersion(latest, installed) {
		info.Status = "available"
	} else {
		info.Status = "current"
	}
	return info
}

// OpenUpdate opens only the canonical repository release page we checked.
// Download and installation remain explicit user actions in the browser.
func (a *App) OpenUpdate() error {
	a.updateMu.Lock()
	info := a.updateInfo
	a.updateMu.Unlock()
	if info.Status != "available" {
		return fmt.Errorf("no checked update is available")
	}
	return a.openURL(releaseBase + info.LatestVersion)
}
