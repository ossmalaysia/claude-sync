package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
)

const origin = "https://claude.ai"

// ErrWindowClosed is claudeapi.ErrSessionGone, so migrate can stop on it
// without importing this package.
var ErrWindowClosed = claudeapi.ErrSessionGone

var _ claudeapi.Doer = (*Session)(nil)

// Session is one headed browser window with its own profile directory.
type Session struct {
	ctx         context.Context // chromedp context; cancelled when the window closes
	cancel      context.CancelFunc
	allocCancel context.CancelFunc
}

func Open(execPath, profileDir string) (*Session, error) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(execPath),
		chromedp.UserDataDir(profileDir),
		chromedp.Flag("headless", false),
		chromedp.Flag("enable-automation", false),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
	)
	actx, acancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(actx)
	if err := chromedp.Run(ctx, chromedp.Navigate(origin+"/")); err != nil {
		cancel()
		acancel()
		return nil, fmt.Errorf("start browser: %w", err)
	}
	return &Session{ctx: ctx, cancel: cancel, allocCancel: acancel}, nil
}

func (s *Session) Close() {
	s.cancel()
	s.allocCancel()
}

// WaitLoggedIn polls until the tab is on claude.ai and /api/organizations
// returns 200. It never fetches while the tab is on an SSO provider page.
func (s *Session) WaitLoggedIn(ctx context.Context, poll time.Duration) error {
	for {
		var host string
		if err := chromedp.Run(s.ctx, chromedp.Evaluate(`location.host`, &host)); err != nil && s.ctx.Err() != nil {
			return ErrWindowClosed
		}
		if host == "claude.ai" {
			if status, _, err := s.JSON(ctx, "GET", "/api/organizations", nil); err == nil && status == 200 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.ctx.Done():
			return ErrWindowClosed
		case <-time.After(poll):
		}
	}
}

type evalResult struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
	B64    string `json:"b64"`
	Host   string `json:"host"` // set only when hostGuard refused to fetch
}

// ErrNotOnClaude is returned when the tab has left claude.ai (typically to
// an SSO login page). It wraps claudeapi.ErrAuth so a push pauses for login
// instead of retrying and recording item failures.
var ErrNotOnClaude = fmt.Errorf("the browser tab is not on claude.ai: %w", claudeapi.ErrAuth)

// checkHost turns a guarded snippet's refusal into ErrNotOnClaude.
func checkHost(r evalResult) error {
	if r.Host != "" {
		return fmt.Errorf("%w (tab is on %s)", ErrNotOnClaude, r.Host)
	}
	return nil
}

func (s *Session) eval(ctx context.Context, src string) (evalResult, error) {
	if err := ctx.Err(); err != nil {
		return evalResult{}, err
	}
	var r evalResult
	err := chromedp.Run(s.ctx, chromedp.Evaluate(src, &r, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
		return p.WithAwaitPromise(true)
	}))
	if err != nil && s.ctx.Err() != nil {
		return r, ErrWindowClosed
	}
	if err != nil {
		return r, err
	}
	return r, checkHost(r)
}

func (s *Session) JSON(ctx context.Context, method, path string, body any) (int, []byte, error) {
	var raw []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		raw = b
	}
	r, err := s.eval(ctx, jsJSON(method, origin+path, raw))
	return r.Status, []byte(r.Body), err
}

func (s *Session) Download(ctx context.Context, path string) (int, []byte, error) {
	r, err := s.eval(ctx, jsDownload(origin+path))
	if err != nil {
		return 0, nil, err
	}
	data, err := base64.StdEncoding.DecodeString(r.B64)
	return r.Status, data, err
}

func (s *Session) Upload(ctx context.Context, path, fileName, mime string, data []byte, fields map[string]string) (int, []byte, error) {
	r, err := s.eval(ctx, jsUpload(origin+path, fileName, mime, base64.StdEncoding.EncodeToString(data), fields))
	return r.Status, []byte(r.Body), err
}
