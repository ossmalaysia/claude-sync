// Package claudeapi is a typed client for the claude.ai web app's internal API.
// All endpoint paths live here; transport is supplied by a Doer.
package claudeapi

import "context"

// Doer performs requests against claude.ai. paths are absolute paths such as
// "/api/organizations". Implementations return the HTTP status and raw body;
// a non-nil error means the request itself failed (network, browser gone).
type Doer interface {
	JSON(ctx context.Context, method, path string, body any) (status int, resp []byte, err error)
	Download(ctx context.Context, path string) (status int, data []byte, err error)
	// Upload posts multipart form field "file" plus any extra text fields.
	Upload(ctx context.Context, path, fileName, mime string, data []byte, fields map[string]string) (status int, resp []byte, err error)
}
