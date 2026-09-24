package migrate

import (
	"testing"

	"github.com/ossmalaysia/claude-sync/internal/store"
)

func TestDefaultSelected(t *testing.T) {
	cases := map[string]bool{
		"Personal: Family":         false,
		"personal:Investment":      false,
		"Family: Travel / Holiday": false,
		"Travel : Thailand 2025":   false,
		"  Travel : Thailand 2025": false,
		"Acme: Research":           true,
		"Training":                 true,
		"Personal Branding":        true, // no colon: not the Personal: prefix
	}
	for name, want := range cases {
		if got := DefaultSelected(name); got != want {
			t.Errorf("%q: got %v want %v", name, got, want)
		}
	}
}

func TestMergeSelectionKeepsExistingChoices(t *testing.T) {
	projects := []store.ProjectMeta{
		{UUID: "a", Name: "Personal: Family"},
		{UUID: "b", Name: "Acme: X"},
		{UUID: "c", Name: "Personal: New"},
		{UUID: "d", Name: "Acme: New"},
	}
	got := MergeSelection(projects, map[string]bool{"a": true, "b": false})
	want := map[string]bool{"a": true, "b": false, "c": false, "d": true}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %v want %v", k, got[k], v)
		}
	}
}
