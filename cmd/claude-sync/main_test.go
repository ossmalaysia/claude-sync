package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ossmalaysia/claude-sync/internal/store"
)

func TestRunUsageErrors(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{nil, "usage:"},
		{[]string{"bogus"}, `unknown command "bogus"`},
		{[]string{"pull"}, "pull needs --org"},
		{[]string{"login"}, "login needs --account source|target"},
		{[]string{"login", "--account", "other"}, "login needs --account source|target"},
	}
	for _, c := range cases {
		var out, errOut bytes.Buffer
		if code := run(context.Background(), c.args, &out, &errOut); code != 2 {
			t.Errorf("%v: exit %d, want 2", c.args, code)
		}
		if !strings.Contains(errOut.String(), c.want) {
			t.Errorf("%v: stderr %q missing %q", c.args, errOut.String(), c.want)
		}
	}
}

type memoryAPI struct{ sent int }

func (m *memoryAPI) ImportMemory(context.Context, string, string) error { m.sent++; return nil }

// Memory switched off in "What to copy" is not sent by the CLI either.
func TestSyncMemoryHonoursSwitch(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st.SaveMemory("**Work context**")
	st.SaveSettings(store.Settings{SkipMemory: true})
	api := &memoryAPI{}
	if res, err := syncMemory(context.Background(), api, st, "org"); err != nil || res != "skipped" || api.sent != 0 {
		t.Fatalf("res=%q err=%v sent=%d", res, err, api.sent)
	}
	st.SaveSettings(store.Settings{})
	if res, err := syncMemory(context.Background(), api, st, "org"); err != nil || res != "sent" || api.sent != 1 {
		t.Fatalf("res=%q err=%v sent=%d", res, err, api.sent)
	}
}
