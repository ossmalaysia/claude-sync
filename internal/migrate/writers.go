package migrate

import (
	"fmt"
	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
	"github.com/ossmalaysia/claude-sync/internal/store"
)

func (r *pushRun) projectWriter(p store.ProjectMeta) func() (claudeapi.Project, error) {
	tried := false
	return func() (claudeapi.Project, error) {
		if tried {
			projects, err := r.api.ListProjects(r.ctx, r.org)
			if err != nil {
				return claudeapi.Project{}, err
			}
			claimed := map[string]bool{}
			for _, ps := range r.state.Projects {
				claimed[ps.Target] = true
			}
			var matches []claudeapi.Project
			for _, remote := range projects {
				if !claimed[remote.UUID] && remote.Name == p.Name {
					matches = append(matches, remote)
				}
			}
			if len(matches) > 1 {
				return claudeapi.Project{}, &ambiguousProject{name: p.Name}
			}
			if len(matches) == 1 {
				remote, err := r.api.GetProject(r.ctx, r.org, matches[0].UUID)
				if err != nil {
					return remote, err
				}
				if remote.Description != p.Description || remote.IsPrivate != p.IsPrivate {
					return claudeapi.Project{}, fmt.Errorf("target project differs after interrupted creation; preview before retrying")
				}
				return remote, nil
			}
		}
		tried = true
		return r.api.CreateProject(r.ctx, r.org, claudeapi.NewProject{Name: p.Name, Description: p.Description, IsPrivate: p.IsPrivate})
	}
}

// Check the target before retrying an upload whose response may have been lost.
// The successful remote ID is then saved by item(), exactly as for a normal reply.
func (r *pushRun) docWriter(name, content string) func() (string, error) {
	tried := false
	return func() (string, error) {
		target := r.state.Project(r.current).Target
		if tried {
			docs, err := r.api.ListDocs(r.ctx, r.org, target)
			if err != nil {
				return "", err
			}
			for _, d := range docs {
				if contentSHA(d.Content) == contentSHA(content) {
					return d.UUID, nil
				}
			}
			for _, d := range docs {
				if d.FileName == name {
					return "", fmt.Errorf("target document contents differ after an interrupted write")
				}
			}
		}
		tried = true
		doc, err := r.api.CreateDoc(r.ctx, r.org, target, name, content)
		return doc.UUID, err
	}
}

func (r *pushRun) fileWriter(name, mime string, data []byte) func() (string, error) {
	tried := false
	return func() (string, error) {
		target := r.state.Project(r.current).Target
		if tried {
			files, err := r.api.ListFiles(r.ctx, r.org, target)
			if err != nil {
				return "", err
			}
			for _, f := range files {
				if f.FileName == name {
					if f.SizeBytes > r.opts.MaxFileBytes {
						return "", fmt.Errorf("target file too large to compare after interrupted upload")
					}
					remote, err := r.api.DownloadFile(r.ctx, r.org, f.FileUUID)
					if err != nil {
						return "", err
					}
					if contentSHA(string(remote)) == contentSHA(string(data)) {
						return f.FileUUID, nil
					}
					return "", fmt.Errorf("target file contents differ after an interrupted upload")
				}
			}
		}
		tried = true
		file, err := r.api.UploadFile(r.ctx, r.org, target, name, mime, data)
		return file.FileUUID, err
	}
}
