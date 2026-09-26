package migrate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
)

// Transcript renders the conversation on screen (the current branch) as
// Markdown, so it can be added to a project as a reference document. It keeps
// what was said and which files were attached; Claude's hidden reasoning and
// tool output are left out (artifacts are copied separately).
func Transcript(c claudeapi.Chat, msgs []claudeapi.ChatMessage) string {
	var b strings.Builder
	name := c.Name
	if name == "" {
		name = "Untitled chat"
	}
	fmt.Fprintf(&b, "# %s\n\n", name)
	fmt.Fprintf(&b, "Chat from %s, last updated %s. Copied by Claude Sync from the previous account", day(c.CreatedAt), day(c.UpdatedAt))
	if c.UUID != "" {
		fmt.Fprintf(&b, " (https://claude.ai/chat/%s)", c.UUID)
	}
	b.WriteString(". This is a record of the conversation, not a live chat.\n")
	for _, m := range msgs {
		who := "Claude"
		if m.Sender == "human" {
			who = "You"
		}
		var text []string
		tools := map[string]int{}
		if m.Text != "" {
			text = append(text, m.Text)
		}
		for _, blk := range m.Content {
			switch blk.Type {
			case "text":
				if blk.Text != "" && blk.Text != m.Text {
					text = append(text, blk.Text)
				}
			case "tool_use":
				if blk.Name != "" {
					tools[blk.Name]++
				}
			}
		}
		var files []string
		for _, f := range append(append([]claudeapi.Attachment(nil), m.Attach...), m.Files...) {
			if f.FileName != "" {
				files = append(files, f.FileName)
			}
		}
		if len(text) == 0 && len(files) == 0 && len(tools) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n---\n\n**%s:**\n\n", who)
		if len(files) > 0 {
			fmt.Fprintf(&b, "_Attached: %s_\n\n", strings.Join(files, ", "))
		}
		if len(tools) > 0 {
			fmt.Fprintf(&b, "_Used: %s_\n\n", toolList(tools))
		}
		if len(text) > 0 {
			b.WriteString(strings.TrimSpace(strings.Join(text, "\n\n")))
			b.WriteString("\n")
		}
	}
	return b.String()
}

// TranscriptFileName is the document name for a chat's transcript.
func TranscriptFileName(c claudeapi.Chat) string {
	name := strings.TrimSpace(c.Name)
	if name == "" {
		name = "Untitled"
	}
	name = strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(name)
	if d := day(c.CreatedAt); d != "" {
		return fmt.Sprintf("Chat - %s (%s).md", name, d)
	}
	return "Chat - " + name + ".md"
}

func day(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return ts
}

func toolList(tools map[string]int) string {
	names := make([]string, 0, len(tools))
	for n := range tools {
		names = append(names, n)
	}
	sort.Strings(names)
	for i, n := range names {
		if tools[n] > 1 {
			names[i] = fmt.Sprintf("%s ×%d", n, tools[n])
		}
	}
	return strings.Join(names, ", ")
}
