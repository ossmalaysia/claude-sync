package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
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
