package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// LockEntry is one skill recorded by the `skills` CLI (vercel-labs/skills),
// either in the global ~/.agents/.skill-lock.json (skillFolderHash) or in a
// project's skills-lock.json (computedHash).
type LockEntry struct {
	Source          string `json:"source"`
	SourceType      string `json:"sourceType"`
	SourceURL       string `json:"sourceUrl,omitempty"`
	Ref             string `json:"ref,omitempty"`
	SkillPath       string `json:"skillPath,omitempty"`
	SkillFolderHash string `json:"skillFolderHash,omitempty"`
	ComputedHash    string `json:"computedHash,omitempty"`
	PluginName      string `json:"pluginName,omitempty"`
	InstalledAt     string `json:"installedAt,omitempty"`
	UpdatedAt       string `json:"updatedAt,omitempty"`
}

// Hash returns the hash the lock recorded for the skill folder.
func (e LockEntry) Hash() string {
	if e.SkillFolderHash != "" {
		return e.SkillFolderHash
	}
	return e.ComputedHash
}

// LockInfo describes the lock file found for a skills directory.
type LockInfo struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"` // "global" (.skill-lock.json) or "project" (skills-lock.json)
	Version int    `json:"version"`
	Count   int    `json:"count"`
}

type lockFile struct {
	Version int                  `json:"version"`
	Skills  map[string]LockEntry `json:"skills"`
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

// globalLockPath mirrors getSkillLockPath() of the skills CLI.
func globalLockPath() string {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "skills", ".skill-lock.json")
	}
	return filepath.Join(homeDir(), ".agents", ".skill-lock.json")
}

// findLock looks for the lock that governs root: the global lock for
// ~/.agents/skills, otherwise a .skill-lock.json or skills-lock.json next to
// or above the directory (a project's .agents/skills).
func findLock(root string) (*LockInfo, map[string]LockEntry) {
	var candidates []struct{ path, kind string }
	if filepath.Clean(root) == filepath.Join(homeDir(), ".agents", "skills") {
		candidates = append(candidates, struct{ path, kind string }{globalLockPath(), "global"})
	}
	dir := filepath.Clean(root)
	for i := 0; i < 3; i++ {
		candidates = append(candidates,
			struct{ path, kind string }{filepath.Join(dir, ".skill-lock.json"), "global"},
			struct{ path, kind string }{filepath.Join(dir, "skills-lock.json"), "project"})
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	for _, c := range candidates {
		data, err := os.ReadFile(c.path)
		if err != nil {
			continue
		}
		var lf lockFile
		if json.Unmarshal(data, &lf) != nil || lf.Skills == nil {
			continue
		}
		return &LockInfo{Path: c.path, Kind: c.kind, Version: lf.Version, Count: len(lf.Skills)}, lf.Skills
	}
	return nil, nil
}

// normalizeSource turns a lock source or a git remote URL into a group key:
// "owner/repo" for GitHub, "host/owner/repo" for other hosts.
func normalizeSource(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "/")
	s = strings.TrimSuffix(s, ".git")
	switch {
	case strings.HasPrefix(s, "git@"):
		s = strings.TrimPrefix(s, "git@")
		s = strings.Replace(s, ":", "/", 1)
	case strings.Contains(s, "://"):
		s = s[strings.Index(s, "://")+3:]
		if at := strings.Index(s, "@"); at >= 0 && at < strings.Index(s+"/", "/") {
			s = s[at+1:]
		}
	}
	s = strings.TrimPrefix(s, "github.com/")
	return s
}
