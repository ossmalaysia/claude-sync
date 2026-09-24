package claudeapi

type Org struct {
	UUID         string   `json:"uuid"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
}

// Has reports whether the org lists the capability ("chat", "raven", ...).
func (o Org) Has(capability string) bool {
	for _, c := range o.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}

// Account is the logged-in user, from GET /api/account.
type Account struct {
	UUID         string `json:"uuid"`
	EmailAddress string `json:"email_address"`
}

type Project struct {
	UUID           string `json:"uuid"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	IsPrivate      bool   `json:"is_private"`
	IsStarred      bool   `json:"is_starred"`
	PromptTemplate string `json:"prompt_template"`
	DocsCount      int    `json:"docs_count"`
	FilesCount     int    `json:"files_count"`
	UpdatedAt      string `json:"updated_at"`
}

type NewProject struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IsPrivate   bool   `json:"is_private"`
}

type Doc struct {
	UUID     string `json:"uuid"`
	FileName string `json:"file_name"`
	Content  string `json:"content"`
}

type File struct {
	FileUUID  string `json:"file_uuid"`
	FileName  string `json:"file_name"`
	FileKind  string `json:"file_kind"`
	SizeBytes int64  `json:"size_bytes"`
}

// Chat is one conversation in a chat listing.
type Chat struct {
	UUID        string `json:"uuid"`
	Name        string `json:"name"`
	ProjectUUID string `json:"project_uuid"` // "" for chats outside a project
	UpdatedAt   string `json:"updated_at"`
}

// ContentBlock is one block of a chat message. Input is set for tool_use blocks.
type ContentBlock struct {
	Type  string         `json:"type"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

type ChatMessage struct {
	UUID       string         `json:"uuid"`
	ParentUUID string         `json:"parent_message_uuid"`
	Index      int            `json:"index"`
	Content    []ContentBlock `json:"content"`
}

// ChatDetail is a conversation with its full message tree. Edited or retried
// messages create branches; CurrentLeaf is the end of the branch on screen.
type ChatDetail struct {
	Chat
	CurrentLeaf string        `json:"current_leaf_message_uuid"`
	Messages    []ChatMessage `json:"chat_messages"`
}

// Skill is one entry of the org's skills listing.
type Skill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`       // "custom", "plugin", "anthropic-example"
	CreatorType string `json:"creator_type"` // "user" or "anthropic"
	UpdatedAt   string `json:"updated_at"`
	Enabled     bool   `json:"enabled"`
}

// Personal reports whether the user made the skill (uploaded, created or
// added through their own plugin); Anthropic's built-in skills are excluded.
func (s Skill) Personal() bool {
	return s.Source != "anthropic-example" && s.CreatorType != "anthropic"
}
