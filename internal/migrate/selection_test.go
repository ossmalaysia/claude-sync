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
		if got := DefaultSelected(name, false); got != want || !DefaultSelected(name, true) {
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
	got := MergeSelection(projects, map[string]bool{"a": true, "b": false}, false)
	want := map[string]bool{"a": true, "b": false, "c": false, "d": true}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %v want %v", k, got[k], v)
		}
	}
}

func TestSetPersonalChoiceTicksOrUnticksPersonalProjects(t *testing.T) {
	st := newTestStore(t)
	st.SaveProject(store.ProjectMeta{UUID: "p", Name: "Personal: Diary"})
	st.SaveProject(store.ProjectMeta{UUID: "w", Name: "Acme: Work", Order: 1})
	st.SaveSelection(map[string]bool{"p": false, "w": false}) // earlier choices

	if err := SetPersonalChoice(st, true); err != nil {
		t.Fatal(err)
	}
	sel, _ := st.LoadSelection()
	if !sel["p"] || sel["w"] {
		t.Fatalf("include: sel=%v (work projects keep their own choice)", sel)
	}
	st.SaveProject(store.ProjectMeta{UUID: "p2", Name: "Family: Trip", Order: 2})
	if sel, _ := EffectiveSelection(st); !sel["p2"] {
		t.Fatal("a personal project found later follows the choice")
	}
	SetPersonalChoice(st, false)
	if sel, _ := st.LoadSelection(); sel["p"] || sel["p2"] {
		t.Fatalf("skip: sel=%v", sel)
	}
	if s, _ := st.LoadSettings(); s.PersonalChoice != store.PersonalSkip {
		t.Fatalf("choice=%q", s.PersonalChoice)
	}
}
