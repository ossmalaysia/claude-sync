package claudeapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

type Client struct{ d Doer }

func New(d Doer) *Client { return &Client{d: d} }

func base(org string) string { return "/api/organizations/" + org }

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	status, resp, err := c.d.JSON(ctx, method, path, body)
	if err != nil {
		return err
	}
	if err := CheckStatus(status, resp); err != nil {
		return err
	}
	if out != nil && len(resp) > 0 {
		if err := json.Unmarshal(resp, out); err != nil {
			return fmt.Errorf("decode %s %s: %w", method, path, err)
		}
	}
	return nil
}

func (c *Client) ListOrgs(ctx context.Context) ([]Org, error) {
	var out []Org
	return out, c.do(ctx, "GET", "/api/organizations", nil, &out)
}

// GetAccount returns the logged-in user. The endpoint was not part of the
// spike's verified list, so callers treat a failure as non-fatal.
func (c *Client) GetAccount(ctx context.Context) (Account, error) {
	var out Account
	return out, c.do(ctx, "GET", "/api/account", nil, &out)
}

func (c *Client) ListProjects(ctx context.Context, org string) ([]Project, error) {
	var out []Project
	return out, c.do(ctx, "GET", base(org)+"/projects", nil, &out)
}

func (c *Client) GetProject(ctx context.Context, org, project string) (Project, error) {
	var out Project
	return out, c.do(ctx, "GET", base(org)+"/projects/"+project, nil, &out)
}

func (c *Client) CreateProject(ctx context.Context, org string, p NewProject) (Project, error) {
	var out Project
	return out, c.do(ctx, "POST", base(org)+"/projects", p, &out)
}

func (c *Client) SetInstructions(ctx context.Context, org, project, text string) error {
	return c.do(ctx, "PUT", base(org)+"/projects/"+project, map[string]string{"prompt_template": text}, nil)
}

func (c *Client) DeleteProject(ctx context.Context, org, project string) error {
	return c.do(ctx, "DELETE", base(org)+"/projects/"+project, nil, nil)
}

func (c *Client) ListDocs(ctx context.Context, org, project string) ([]Doc, error) {
	var out []Doc
	return out, c.do(ctx, "GET", base(org)+"/projects/"+project+"/docs", nil, &out)
}

func (c *Client) CreateDoc(ctx context.Context, org, project, fileName, content string) (Doc, error) {
	var out Doc
	body := map[string]string{"file_name": fileName, "content": content}
	return out, c.do(ctx, "POST", base(org)+"/projects/"+project+"/docs", body, &out)
}

func (c *Client) ListFiles(ctx context.Context, org, project string) ([]File, error) {
	var out []File
	return out, c.do(ctx, "GET", base(org)+"/projects/"+project+"/files", nil, &out)
}

// DownloadFile returns the original bytes. Never use /preview: it re-encodes images.
func (c *Client) DownloadFile(ctx context.Context, org, fileUUID string) ([]byte, error) {
	status, data, err := c.d.Download(ctx, base(org)+"/files/"+fileUUID+"/contents")
	if err != nil {
		return nil, err
	}
	if err := CheckStatus(status, data); err != nil {
		return nil, err
	}
	return data, nil
}

// DownloadPreview returns the full-size preview of an image file. claude.ai
// serves no original for images (/contents is 404); the preview is a WebP
// re-encoding at the original resolution. Use only as a fallback for images.
func (c *Client) DownloadPreview(ctx context.Context, org, fileUUID string) ([]byte, error) {
	status, data, err := c.d.Download(ctx, "/api/"+org+"/files/"+fileUUID+"/preview")
	if err != nil {
		return nil, err
	}
	if err := CheckStatus(status, data); err != nil {
		return nil, err
	}
	return data, nil
}

func (c *Client) UploadFile(ctx context.Context, org, project, fileName, mime string, data []byte) (File, error) {
	status, resp, err := c.d.Upload(ctx, base(org)+"/projects/"+project+"/upload", fileName, mime, data, nil)
	if err != nil {
		return File{}, err
	}
	if err := CheckStatus(status, resp); err != nil {
		return File{}, err
	}
	var out File
	if err := json.Unmarshal(resp, &out); err != nil {
		return File{}, fmt.Errorf("decode upload response: %w", err)
	}
	return out, nil
}

// ImportMemory sends memory text to claude.ai's memory import (the same call
// as Settings > Memory > Start import > Add to memory). claude.ai merges it
// into the account's memory in the background.
func (c *Client) ImportMemory(ctx context.Context, org, text string) error {
	return c.do(ctx, "POST", base(org)+"/melange/import_external", map[string]string{"raw_export": text}, nil)
}

// ListSkills returns every skill visible to the account, built-in ones included.
func (c *Client) ListSkills(ctx context.Context, org string) ([]Skill, error) {
	var out struct {
		Skills []Skill `json:"skills"`
	}
	return out.Skills, c.do(ctx, "GET", base(org)+"/skills/list-skills", nil, &out)
}

// DownloadSkill returns the skill as a .skill package: a zip with SKILL.md
// and every other file of the skill (scripts, references, images...).
func (c *Client) DownloadSkill(ctx context.Context, org, skillID string) ([]byte, error) {
	status, data, err := c.d.Download(ctx, base(org)+"/skills/download-dot-skill-file?skill_id="+url.QueryEscape(skillID)+"&include_blocked=true")
	if err != nil {
		return nil, err
	}
	if err := CheckStatus(status, data); err != nil {
		return nil, err
	}
	return data, nil
}

// UploadSkill adds a .skill package to the account, as Customize > Skills >
// Add > Upload skill does. It never overwrites an existing skill.
func (c *Client) UploadSkill(ctx context.Context, org, fileName string, data []byte) error {
	status, resp, err := c.d.Upload(ctx, base(org)+"/skills/upload-skill?overwrite=false&upload_source=customize_upload",
		fileName, "application/zip", data, map[string]string{"upload_source": "customize_upload"})
	if err != nil {
		return err
	}
	return CheckStatus(status, resp)
}

// ListChats returns one page of the org's conversations and whether more exist.
func (c *Client) ListChats(ctx context.Context, org string, offset, limit int) ([]Chat, bool, error) {
	var out struct {
		Data    []Chat `json:"data"`
		HasMore bool   `json:"has_more"`
	}
	err := c.do(ctx, "GET", fmt.Sprintf("%s/chat_conversations_v2?limit=%d&offset=%d", base(org), limit, offset), nil, &out)
	return out.Data, out.HasMore, err
}

// GetChat returns a conversation with every message and tool call.
func (c *Client) GetChat(ctx context.Context, org, chat string) (ChatDetail, error) {
	var out ChatDetail
	return out, c.do(ctx, "GET", base(org)+"/chat_conversations/"+chat+"?tree=True&rendering_mode=messages&render_all_tools=true", nil, &out)
}

func (c *Client) GetMemory(ctx context.Context, org string) (string, error) {
	var out struct {
		Memory string `json:"memory"`
	}
	return out.Memory, c.do(ctx, "GET", base(org)+"/memory", nil, &out)
}
