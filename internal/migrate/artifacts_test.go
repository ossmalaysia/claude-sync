package migrate

import (
	"testing"

	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
)

func tool(name string, input map[string]any) claudeapi.ContentBlock {
	return claudeapi.ContentBlock{Type: "tool_use", Name: name, Input: input}
}

func msg(id, parent string, idx int, blocks ...claudeapi.ContentBlock) claudeapi.ChatMessage {
	return claudeapi.ChatMessage{UUID: id, ParentUUID: parent, Index: idx, Content: blocks}
}

func TestBranchFollowsCurrentLeaf(t *testing.T) {
	// m1 -> m2a (abandoned retry) and m1 -> m2b -> m3 (current)
	c := claudeapi.ChatDetail{CurrentLeaf: "m3", Messages: []claudeapi.ChatMessage{
		msg("m1", "root", 0), msg("m2a", "m1", 1), msg("m2b", "m1", 1), msg("m3", "m2b", 2),
	}}
	var ids []string
	for _, m := range Branch(c) {
		ids = append(ids, m.UUID)
	}
	if len(ids) != 3 || ids[0] != "m1" || ids[1] != "m2b" || ids[2] != "m3" {
		t.Fatalf("branch=%v", ids)
	}
}

func TestBranchFallsBackToIndexOrder(t *testing.T) {
	c := claudeapi.ChatDetail{CurrentLeaf: "missing", Messages: []claudeapi.ChatMessage{msg("b", "a", 1), msg("a", "root", 0)}}
	b := Branch(c)
	if len(b) != 2 || b[0].UUID != "a" {
		t.Fatalf("branch=%+v", b)
	}
}

func TestExtractReplaysArtifactVersions(t *testing.T) {
	msgs := []claudeapi.ChatMessage{
		msg("m1", "", 0, tool("artifacts", map[string]any{"id": "plan", "command": "create", "type": "text/markdown", "title": "Q3 plan", "content": "# Plan\nstep one"})),
		msg("m2", "m1", 1, tool("artifacts", map[string]any{"id": "plan", "command": "update", "old_str": "step one", "new_str": "step one\nstep two"})),
		msg("m3", "m2", 2, tool("artifacts", map[string]any{"id": "code", "command": "create", "type": "application/vnd.ant.code", "language": "python", "title": "etl", "content": "print(1)"})),
		msg("m4", "m3", 3, tool("artifacts", map[string]any{"id": "code", "command": "rewrite", "content": "print(2)"})),
	}
	arts := ExtractArtifacts(msgs)
	if len(arts) != 2 {
		t.Fatalf("arts=%+v", arts)
	}
	if arts[0].FileName != "Artifact - Q3 plan.md" || arts[0].Content != "# Plan\nstep one\nstep two" || arts[0].Kind != "artifact" {
		t.Fatalf("plan=%+v", arts[0])
	}
	if arts[1].FileName != "Artifact - etl.py" || arts[1].Content != "print(2)" {
		t.Fatalf("code=%+v", arts[1])
	}
}

func TestExtractClaudeWrittenFilesWithEdits(t *testing.T) {
	msgs := []claudeapi.ChatMessage{
		msg("m1", "", 0,
			tool("create_file", map[string]any{"path": "/mnt/user-data/outputs/report.md", "file_text": "# Report\nv1", "description": "x"}),
			tool("Write", map[string]any{"file_path": "/tmp/notes.txt", "content": "a a a"}),
		),
		msg("m2", "m1", 1,
			tool("str_replace", map[string]any{"path": "/mnt/user-data/outputs/report.md", "old_str": "v1", "new_str": "v2"}),
			tool("Edit", map[string]any{"file_path": "/tmp/notes.txt", "old_string": "a", "new_string": "b", "replace_all": true}),
			tool("create_file", map[string]any{"path": "/x/empty.md", "file_text": ""}), // empty: dropped
		),
	}
	arts := ExtractArtifacts(msgs)
	if len(arts) != 2 {
		t.Fatalf("arts=%+v", arts)
	}
	if arts[0].FileName != "Artifact - report.md" || arts[0].Content != "# Report\nv2" || arts[0].Kind != "file" {
		t.Fatalf("report=%+v", arts[0])
	}
	if arts[1].FileName != "Artifact - notes.txt" || arts[1].Content != "b b b" {
		t.Fatalf("notes=%+v", arts[1])
	}
}

func TestArtifactExtensions(t *testing.T) {
	cases := map[[2]string]string{
		{"text/markdown", ""}:                  ".md",
		{"text/html", ""}:                      ".html",
		{"application/vnd.ant.react", ""}:      ".jsx",
		{"image/svg+xml", ""}:                  ".svg",
		{"application/vnd.ant.mermaid", ""}:    ".mmd",
		{"application/vnd.ant.code", "go"}:     ".go",
		{"application/vnd.ant.code", "Python"}: ".py",
		{"application/vnd.ant.code", "cobol"}:  ".txt",
		{"", ""}:                               ".txt",
	}
	for in, want := range cases {
		if got := artifactExt(in[0], in[1]); got != want {
			t.Errorf("%v: got %s want %s", in, got, want)
		}
	}
}
