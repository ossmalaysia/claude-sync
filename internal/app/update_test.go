package app

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type updateTransport func(*http.Request) (*http.Response, error)

func (f updateTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func updateApp(t *testing.T, body string, status int) (*App, *int) {
	t.Helper()
	a := New(nil, nil, nil)
	calls := 0
	a.updateClient = &http.Client{Transport: updateTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != releaseAPI || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Fatalf("unexpected update request: %s", r.URL)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	return a, &calls
}

func TestUpdateFindsHighestPublishedVersionIncludingEarlyReleases(t *testing.T) {
	a, calls := updateApp(t, `[
 {"tag_name":"v0.1.4","published_at":"2026-10-04T00:00:00Z"},
 {"tag_name":"v0.1.10","prerelease":true,"published_at":"2026-10-14T00:00:00Z"},
 {"tag_name":"v99.0.0","draft":true,"published_at":"2026-10-14T00:00:00Z"},
 {"tag_name":"v2.0.0-rc.1","published_at":"2026-10-14T00:00:00Z"},
 {"tag_name":"v4.0.0"}
 ]`, 200)
	info := a.checkUpdates(context.Background(), "v0.1.5", false)
	if info.Status != "available" || info.LatestVersion != "v0.1.10" || info.ReleaseURL != "https://github.com/ossmalaysia/claude-sync/releases/tag/v0.1.10" {
		t.Fatalf("info=%+v", info)
	}
	a.checkUpdates(context.Background(), "v0.1.5", false)
	if *calls != 1 {
		t.Fatal("automatic check ignored cache")
	}
	a.checkUpdates(context.Background(), "v0.1.5", true)
	if *calls != 2 {
		t.Fatal("manual check did not refresh")
	}
}

func TestUpdateVersionComparison(t *testing.T) {
	cases := []struct{ current, latest, want string }{
		{"v0.1.5", "v0.1.5", "current"}, {"v0.1.6", "v0.1.5", "current"},
		{"v0.1.5-dev", "v0.1.5", "available"}, {"dev-abc123", "v0.1.5", "development"},
		{"v0.9.9", "v1.0.0", "available"}, {"v0.1.9", "v0.1.10", "available"},
	}
	for _, c := range cases {
		t.Run(c.current+"/"+c.latest, func(t *testing.T) {
			a, _ := updateApp(t, `[{"tag_name":"`+c.latest+`","published_at":"2026-10-14T00:00:00Z"}]`, 200)
			if info := a.checkUpdates(context.Background(), c.current, false); info.Status != c.want {
				t.Fatalf("info=%+v", info)
			}
		})
	}
}

func TestUpdateFailuresDoNotBreakAppOrTrustURLs(t *testing.T) {
	for _, c := range []struct {
		body   string
		status int
	}{
		{`{"message":"rate limited"}`, 403}, {`not json`, 200}, {`[]`, 200},
		{`[{"tag_name":"../../evil","html_url":"https://evil.example","published_at":"2026-10-14T00:00:00Z"}]`, 200},
	} {
		a, _ := updateApp(t, c.body, c.status)
		if info := a.checkUpdates(context.Background(), "v0.1.4", false); info.Status != "unavailable" {
			t.Fatalf("info=%+v", info)
		}
	}
	a, _ := updateApp(t, `[{"tag_name":"v0.1.5","published_at":"2026-10-14T00:00:00Z","html_url":"https://evil.example"}]`, 200)
	a.checkUpdates(context.Background(), "v0.1.4", false)
	var opened string
	a.openURL = func(url string) error { opened = url; return nil }
	if err := a.OpenUpdate(); err != nil || opened != "https://github.com/ossmalaysia/claude-sync/releases/tag/v0.1.5" {
		t.Fatalf("opened=%q err=%v", opened, err)
	}
	a.updateCheckedAt = time.Time{}
	a.updateInfo = UpdateInfo{}
	if err := a.OpenUpdate(); err == nil {
		t.Fatal("opened unchecked update")
	}
}

func TestUpdateTransportFailureAndRedirectAreUnavailable(t *testing.T) {
	a := New(nil, nil, nil)
	a.updateClient.Transport = updateTransport(func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded })
	if info := a.checkUpdates(context.Background(), "v0.1.4", true); info.Status != "unavailable" {
		t.Fatalf("info=%+v", info)
	}
	a.updateClient.Transport = updateTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.example"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	if info := a.checkUpdates(context.Background(), "v0.1.4", true); info.Status != "unavailable" {
		t.Fatalf("info=%+v", info)
	}
}
