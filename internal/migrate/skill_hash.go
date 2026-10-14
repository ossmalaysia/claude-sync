package migrate

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
	"github.com/ossmalaysia/claude-sync/internal/store"
	"io"
	"sort"
)

// Ignore zip timestamps/compression so repackaging the same skill is not a change.
func skillSHA(data []byte) (string, error) {
	pkg, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return contentSHA(string(data)), nil
	}
	sort.Slice(pkg.File, func(i, j int) bool { return pkg.File[i].Name < pkg.File[j].Name })
	h := sha256.New()
	for _, f := range pkg.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if f.UncompressedSize64 > 30<<20 {
			return "", fmt.Errorf("skill file too large to compare")
		}
		r, err := f.Open()
		if err != nil {
			return "", err
		}
		data, err := io.ReadAll(io.LimitReader(r, 30<<20+1))
		_ = r.Close()
		if err != nil {
			return "", err
		}
		if len(data) > 30<<20 {
			return "", fmt.Errorf("skill file too large to compare")
		}
		fmt.Fprintf(h, "%d:%s:%d:", len(f.Name), f.Name, len(data))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (r *pushRun) reconcileSkills() error {
	settings, err := r.st.LoadSettings()
	if err != nil || settings.SkipSkills {
		return err
	}
	skills, err := r.st.ListSkills()
	if err != nil || len(skills) == 0 {
		return err
	}
	target, _, err := withRetry(r.ctx, r.opts, func() ([]claudeapi.Skill, error) { return r.api.ListSkills(r.ctx, r.org) })
	if errors.Is(err, claudeapi.ErrNotFound) {
		target, err = nil, nil
	}
	if err != nil {
		return err
	}
	if r.state.Skills == nil {
		r.state.Skills = map[string]*store.ItemState{}
	}
	for _, sk := range skills {
		r.st.TouchJob()
		data, err := r.st.ReadSkill(sk.ID)
		if err != nil {
			return err
		}
		sha, err := skillSHA(data)
		if err != nil {
			return err
		}
		it := r.state.Skills[sk.ID]
		if it == nil {
			it = &store.ItemState{}
			r.state.Skills[sk.ID] = it
		}
		it.Name = sk.Name
		var match *claudeapi.Skill
		for i := range target {
			if (it.Target != "" && target[i].ID == it.Target) || (it.Target == "" && target[i].Name == sk.Name) {
				if match != nil {
					conflict(it, "multiple target skills have this name")
					match = nil
					break
				}
				match = &target[i]
			}
		}
		if match != nil {
			pkg, _, err := withRetry(r.ctx, r.opts, func() ([]byte, error) { return r.api.DownloadSkill(r.ctx, r.org, match.ID) })
			if err != nil {
				return err
			}
			remoteSHA, err := skillSHA(pkg)
			if err != nil {
				return err
			}
			it.Target = match.ID
			if remoteSHA == sha {
				it.Status, it.SHA, it.Error = store.StatusDone, sha, ""
			} else {
				conflict(it, "skill files differ; review both versions")
			}
		} else if it.Status == store.StatusDone {
			conflict(it, "previously copied skill cannot be matched in target")
		}
	}
	return nil
}
