package migrate

import (
	"strings"
	"testing"

	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
)

func TestTranscriptKeepsTheConversationOnly(t *testing.T) {
	chat := claudeapi.Chat{UUID: "c1", Name: "Pricing plan", CreatedAt: "2026-08-12T10:31:00Z", UpdatedAt: "2026-08-14T09:00:00Z"}
	msgs := []claudeapi.ChatMessage{
		{Sender: "human", CreatedAt: "2026-08-12T10:31:00Z", Content: []claudeapi.ContentBlock{{Type: "text", Text: "What should we charge?"}},
			Files: []claudeapi.Attachment{{FileName: "costs.xlsx"}}},
		{Sender: "assistant", Content: []claudeapi.ContentBlock{
			{Type: "thinking", Text: "secret reasoning"},
			{Type: "tool_use", Name: "web_search"},
			{Type: "tool_result"},
			{Type: "text", Text: "Charge RM 99 a month."},
		}},
		{Sender: "human", Text: "Thanks"}, // older message format
	}
	got := Transcript(chat, msgs)
	for _, want := range []string{"# Pricing plan", "2026-08-12", "https://claude.ai/chat/c1",
		"**You:**", "What should we charge?", "costs.xlsx", "**Claude:**", "Charge RM 99 a month.", "Thanks", "web_search"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "secret reasoning") {
		t.Error("thinking must be left out")
	}
}

func TestTranscriptFileName(t *testing.T) {
	c := claudeapi.Chat{Name: "Q3 plan: v2/final", CreatedAt: "2026-08-12T10:31:00Z"}
	if got := TranscriptFileName(c); got != "Chat - Q3 plan- v2-final (2026-08-12).md" {
		t.Fatalf("got %q", got)
	}
	if got := TranscriptFileName(claudeapi.Chat{}); got != "Chat - Untitled.md" {
		t.Fatalf("got %q", got)
	}
}
