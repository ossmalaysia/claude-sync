package claudeapi

import (
	"bytes"
	"errors"
	"fmt"
)

var (
	ErrAuth        = errors.New("session expired or not logged in")
	ErrForbidden   = errors.New("forbidden by organization policy")
	ErrNotFound    = errors.New("not found")
	ErrRateLimited = errors.New("rate limited")
	ErrServer      = errors.New("server error")

	// ErrSessionGone means the browser tab that carries the session no
	// longer exists (the user closed the window). Retrying cannot help; a
	// new window must be opened.
	ErrSessionGone = errors.New("browser window was closed")
)

// HTTPError is returned for every non-2xx response. Kind is one of the
// sentinels above, or nil for other 4xx responses.
type HTTPError struct {
	Status int
	Body   string
	Kind   error
}

func (e *HTTPError) Error() string {
	body := e.Body
	if len(body) > 200 {
		body = body[:200] + "…"
	}
	return fmt.Sprintf("HTTP %d: %s", e.Status, body)
}

func (e *HTTPError) Unwrap() error { return e.Kind }

// CheckStatus returns nil for 2xx and an *HTTPError otherwise. A 403 is only
// treated as an expired session when the body says so; any other 403 is an
// organization policy denial and must not trigger a re-login.
func CheckStatus(status int, body []byte) error {
	if status >= 200 && status < 300 {
		return nil
	}
	e := &HTTPError{Status: status, Body: string(body)}
	switch {
	case status == 401:
		e.Kind = ErrAuth
	case status == 403 && (bytes.Contains(body, []byte("account_session_invalid")) || bytes.Contains(body, []byte("Invalid authorization"))):
		e.Kind = ErrAuth
	case status == 403:
		e.Kind = ErrForbidden
	case status == 404:
		e.Kind = ErrNotFound
	case status == 429:
		e.Kind = ErrRateLimited
	case status >= 500:
		e.Kind = ErrServer
	}
	return e
}
