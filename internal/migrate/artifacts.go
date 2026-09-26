package migrate

import (
	"path"
	"sort"
	"strings"

	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
	"github.com/ossmalaysia/claude-sync/internal/store"
)

// Branch returns the messages on the chat's current branch, oldest first.
// Edited or retried messages leave abandoned branches in the tree; following
// parents back from the current leaf keeps only what the user sees. If the
// leaf is unknown, all messages are returned in index order.
func Branch(c claudeapi.ChatDetail) []claudeapi.ChatMessage {
	byID := make(map[string]claudeapi.ChatMessage, len(c.Messages))
	for _, m := range c.Messages {
		byID[m.UUID] = m
	}
	var branch []claudeapi.ChatMessage
	for id, seen := c.CurrentLeaf, 0; id != "" && seen <= len(c.Messages); seen++ {
		m, ok := byID[id]
		if !ok {
			break
		}
		branch = append(branch, m)
		id = m.ParentUUID
	}
	if len(branch) == 0 {
		all := append([]claudeapi.ChatMessage(nil), c.Messages...)
		sort.SliceStable(all, func(i, j int) bool { return all[i].Index < all[j].Index })
		return all
	}
	for i, j := 0, len(branch)-1; i < j; i, j = i+1, j-1 {
		branch[i], branch[j] = branch[j], branch[i]
	}
	return branch
}

func str(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok {
			return v
		}
	}
	return ""
}

var codeExt = map[string]string{
	"python": ".py", "javascript": ".js", "typescript": ".ts", "go": ".go", "java": ".java",
	"sql": ".sql", "bash": ".sh", "shell": ".sh", "json": ".json", "yaml": ".yaml", "css": ".css",
	"html": ".html", "rust": ".rs", "c": ".c", "cpp": ".cpp", "csharp": ".cs", "ruby": ".rb",
	"php": ".php", "kotlin": ".kt", "swift": ".swift", "markdown": ".md",
}

func artifactExt(typ, language string) string {
	switch typ {
	case "text/markdown":
		return ".md"
	case "text/html":
		return ".html"
	case "application/vnd.ant.react":
		return ".jsx"
	case "image/svg+xml":
		return ".svg"
	case "application/vnd.ant.mermaid":
		return ".mmd"
	case "application/vnd.ant.code":
		if e, ok := codeExt[strings.ToLower(language)]; ok {
			return e
		}
	}
	return ".txt"
}

// ExtractArtifacts returns the final version of every artifact and every
// Claude-written file in the given messages (in order). Artifacts replay
// create/update/rewrite; files replay create_file/Write then str_replace/Edit.
// Entries with no content are dropped.
func ExtractArtifacts(msgs []claudeapi.ChatMessage) []store.ArtifactRecord {
	byID := map[string]*store.ArtifactRecord{}
	var order []string
	get := func(id string) *store.ArtifactRecord {
		a := byID[id]
		if a == nil {
			a = &store.ArtifactRecord{ID: id}
			byID[id] = a
			order = append(order, id)
		}
		return a
	}
	language := map[string]string{}
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Type != "tool_use" || b.Input == nil {
				continue
			}
			in := b.Input
			switch b.Name {
			case "artifacts":
				id := str(in, "id")
				if id == "" {
					continue
				}
				a := get("artifact:" + id)
				a.Kind = "artifact"
				if t := str(in, "title"); t != "" {
					a.Title = t
				}
				if t := str(in, "type"); t != "" {
					a.Type = t
				}
				if l := str(in, "language"); l != "" {
					language[a.ID] = l
				}
				switch str(in, "command") {
				case "create", "rewrite":
					a.Content = str(in, "content")
				case "update":
					a.Content = strings.Replace(a.Content, str(in, "old_str"), str(in, "new_str"), 1)
				}
			case "create_file", "Write":
				p := str(in, "path", "file_path")
				if p == "" {
					continue
				}
				a := get("file:" + p)
				a.Kind, a.Title = "file", path.Base(p)
				a.Content = str(in, "file_text", "content")
			case "str_replace", "Edit":
				p := str(in, "path", "file_path")
				a, ok := byID["file:"+p]
				if !ok {
					continue // editing a file created outside this chat
				}
				oldS, newS := str(in, "old_str", "old_string"), str(in, "new_str", "new_string")
				if all, _ := in["replace_all"].(bool); all {
					a.Content = strings.ReplaceAll(a.Content, oldS, newS)
				} else {
					a.Content = strings.Replace(a.Content, oldS, newS, 1)
				}
			}
		}
	}
	var out []store.ArtifactRecord
	for _, id := range order {
		a := *byID[id]
		if strings.TrimSpace(a.Content) == "" {
			continue
		}
		if a.Kind == "artifact" {
			title := a.Title
			if title == "" {
				title = strings.TrimPrefix(a.ID, "artifact:")
			}
			a.FileName = "Artifact - " + title + artifactExt(a.Type, language[a.ID])
		} else {
			a.FileName = "Artifact - " + a.Title
		}
		out = append(out, a)
	}
	return out
}

// artifactRef is an artifact together with the chat it came from.
type artifactRef struct {
	Chat string
	store.ArtifactRecord
}

func (r artifactRef) key() string { return r.Chat + "/" + r.ID }

// artifactsToSend returns artifactsByProject, or nil when the user switched
// artifacts off.
func artifactsToSend(st *store.Store) (map[string][]artifactRef, error) {
	settings, err := st.LoadSettings()
	if err != nil || settings.SkipArtifacts {
		return nil, err
	}
	return artifactsByProject(st)
}

// artifactsByProject groups the saved artifacts by source project uuid.
// Artifacts from chats outside a project are not included.
func artifactsByProject(st *store.Store) (map[string][]artifactRef, error) {
	chats, err := st.ListChats()
	if err != nil {
		return nil, err
	}
	sort.Slice(chats, func(i, j int) bool { return chats[i].UUID < chats[j].UUID })
	out := map[string][]artifactRef{}
	for _, c := range chats {
		if c.ProjectUUID == "" {
			continue
		}
		for _, a := range c.Artifacts {
			out[c.ProjectUUID] = append(out[c.ProjectUUID], artifactRef{Chat: c.UUID, ArtifactRecord: a})
		}
	}
	return out, nil
}
