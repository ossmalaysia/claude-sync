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
