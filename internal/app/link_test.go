package app

import "testing"

func TestOpenLinkOpensOnlyKnownPages(t *testing.T) {
	a := New(nil, nil, nil)
	var opened []string
	a.openURL = func(u string) error { opened = append(opened, u); return nil }
	if err := a.OpenLink("issues"); err != nil {
		t.Fatal(err)
	}
	if err := a.OpenLink("https://evil.example"); err == nil {
		t.Fatal("an unknown link must be refused")
	}
	if len(opened) != 1 || opened[0] != "https://github.com/ossmalaysia/claude-sync/issues/new/choose" {
		t.Fatalf("opened=%v", opened)
	}
}
