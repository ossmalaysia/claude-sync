package claudeapi

import (
	"errors"
	"testing"
)

func TestCheckStatus(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   error // nil means CheckStatus must return nil
	}{
		{200, "", nil},
		{204, "", nil},
		{401, `{}`, ErrAuth},
		{403, `{"type":"error","error":{"type":"permission_error","message":"Invalid authorization","details":{"error_code":"account_session_invalid"}}}`, ErrAuth},
		{403, `{"type":"error","error":{"type":"permission_error","message":"Project creation is disabled for this organization"}}`, ErrForbidden},
		{404, `{}`, ErrNotFound},
		{429, `{}`, ErrRateLimited},
		{502, `bad gateway`, ErrServer},
	}
	for _, c := range cases {
		err := CheckStatus(c.status, []byte(c.body))
		if c.want == nil {
			if err != nil {
				t.Errorf("%d: got %v, want nil", c.status, err)
			}
			continue
		}
		if !errors.Is(err, c.want) {
			t.Errorf("%d %s: got %v, want %v", c.status, c.body, err, c.want)
		}
		var he *HTTPError
		if !errors.As(err, &he) || he.Status != c.status {
			t.Errorf("%d: expected *HTTPError carrying the status", c.status)
		}
	}
	err := CheckStatus(400, []byte("bad request"))
	for _, s := range []error{ErrAuth, ErrForbidden, ErrNotFound, ErrRateLimited, ErrServer} {
		if errors.Is(err, s) {
			t.Errorf("400 must carry no sentinel, matched %v", s)
		}
	}
}
