package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// UpdateStatus is the result of checking one skill against its source.
type UpdateStatus string

const (
	UpdateCurrent  UpdateStatus = "current"  // upstream folder hash equals the lock's
	UpdateOutdated UpdateStatus = "outdated" // upstream changed since install/update
	UpdateGone     UpdateStatus = "gone"     // the folder is no longer at skillPath upstream
	UpdateError    UpdateStatus = "error"    // the source could not be reached
	UpdateSkipped  UpdateStatus = "skipped"  // no hash or path to compare
)

// UpdateResult is the check of one skill.
type UpdateResult struct {
	ID         string       `json:"id"`
	Source     string       `json:"source"`
	Status     UpdateStatus `json:"status"`
	LockHash   string       `json:"lockHash,omitempty"`
	RemoteHash string       `json:"remoteHash,omitempty"`
	Via        string       `json:"via,omitempty"` // "gh api", "github api", "git clone"
	Message    string       `json:"message,omitempty"`
}

// UpdateReport is the result of a whole check, saved between runs.
type UpdateReport struct {
	CheckedAt string                  `json:"checkedAt"`
	Results   map[string]UpdateResult `json:"results"`
	Outdated  int                     `json:"outdated"`
	Errors    int                     `json:"errors"`
	Checked   int                     `json:"checked"`
}

// UpdateProgress is streamed while a check runs.
type UpdateProgress struct {
	Done   int    `json:"done"`
	Total  int    `json:"total"`
	Source string `json:"source"`
}

func updatesPath(root string) string {
	return filepath.Join(dataDir(), "updates-"+rootKey(root)+".json")
}

func loadUpdateReport(root string) *UpdateReport {
	data, err := os.ReadFile(updatesPath(root))
	if err != nil {
		return nil
	}
	var r UpdateReport
	if json.Unmarshal(data, &r) != nil {
		return nil
	}
	return &r
}

func findBin(name string) string {
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

// githubGET calls the GitHub REST API, through the gh CLI when it is
// installed (authenticated, like the skills CLI's fallback) or anonymously.
func githubGET(ctx context.Context, endpoint string, out any) (string, error) {
	if gh := findBin("gh"); gh != "" {
		cmd := exec.CommandContext(ctx, gh, "api", endpoint, "--method", "GET")
		cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1")
		data, err := cmd.Output()
		if err == nil {
			return "gh api", json.Unmarshal(data, out)
		}
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/"+endpoint, nil)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "skills-reader")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "github api", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "github api", fmt.Errorf("GitHub API %s", resp.Status)
	}
	return "github api", json.NewDecoder(resp.Body).Decode(out)
}

func skillFolder(skillPath string) string {
	p := strings.ReplaceAll(skillPath, `\`, "/")
	if strings.HasSuffix(strings.ToLower(p), "skill.md") {
		p = p[:len(p)-len("SKILL.md")]
	}
	return strings.Trim(p, "/")
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

type checkItem struct {
	id    string
	entry LockEntry
}

// checkGitHub resolves the folder hashes of items from one GitHub repo with
// the contents API: the listing of a folder's parent carries the folder's
// tree sha, the value skillFolderHash records.
func checkGitHub(ctx context.Context, items []checkItem) ([]UpdateResult, error) {
	src := items[0].entry
	repo := normalizeSource(src.Source)
	ref := src.Ref
	if ref == "" {
		ref = "HEAD"
	}
	type listing struct {
		via     string
		entries map[string]string
		err     error
	}
	cache := map[string]*listing{}
	var results []UpdateResult
	for _, it := range items {
		folder := skillFolder(it.entry.SkillPath)
		r := UpdateResult{ID: it.id, Source: repo, LockHash: it.entry.Hash()}
		if folder == "" {
			var commit struct {
				Commit struct {
					Tree struct {
						SHA string `json:"sha"`
					} `json:"tree"`
				} `json:"commit"`
			}
			via, err := githubGET(ctx, fmt.Sprintf("repos/%s/commits/%s", repo, url.PathEscape(ref)), &commit)
			r.Via = via
			if err != nil {
				return nil, err
			}
			r.RemoteHash = commit.Commit.Tree.SHA
		} else {
			parent := path.Dir(folder)
			if parent == "." {
				parent = ""
			}
			l, ok := cache[parent]
			if !ok {
				var entries []struct {
					Name string `json:"name"`
					Type string `json:"type"`
					SHA  string `json:"sha"`
				}
				ep := fmt.Sprintf("repos/%s/contents/%s", repo, escapePath(parent))
				if src.Ref != "" {
					ep += "?ref=" + url.QueryEscape(src.Ref)
				}
				via, err := githubGET(ctx, ep, &entries)
				l = &listing{via: via, entries: map[string]string{}, err: err}
				for _, e := range entries {
					if e.Type == "dir" {
						l.entries[e.Name] = e.SHA
					}
				}
				cache[parent] = l
			}
			if l.err != nil {
				return nil, l.err
			}
			r.Via = l.via
			sha, ok := l.entries[path.Base(folder)]
			if !ok {
				r.Status = UpdateGone
				r.Message = "上游已不在 " + it.entry.SkillPath + "（可能已删除或改了路径）"
				results = append(results, r)
				continue
			}
			r.RemoteHash = sha
		}
		if strings.EqualFold(r.RemoteHash, r.LockHash) {
			r.Status = UpdateCurrent
		} else {
			r.Status = UpdateOutdated
		}
		results = append(results, r)
	}
	return results, nil
}

// checkClone shallow-clones the source and hashes the folders the way the
// skills CLI does for that hash format.
func checkClone(ctx context.Context, items []checkItem) []UpdateResult {
	src := items[0].entry
	repo := normalizeSource(src.Source)
	cloneURL := src.SourceURL
	if cloneURL == "" {
		cloneURL = src.Source
	}
	fail := func(msg string) []UpdateResult {
		var out []UpdateResult
		for _, it := range items {
			out = append(out, UpdateResult{ID: it.id, Source: repo, Status: UpdateError, LockHash: it.entry.Hash(), Via: "git clone", Message: msg})
		}
		return out
	}
	tmp, err := os.MkdirTemp("", "skills-reader-*")
	if err != nil {
		return fail(err.Error())
	}
	defer os.RemoveAll(tmp)
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	args := []string{"clone", "--depth", "1", "--quiet"}
	if src.Ref != "" {
		args = append(args, "--branch", src.Ref)
	}
	args = append(args, cloneURL, tmp)
	cmd := exec.CommandContext(cctx, gitBin(), args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes -o ConnectTimeout=10")
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fail("git clone 失败: " + msg)
	}
	var results []UpdateResult
	for _, it := range items {
		folder := skillFolder(it.entry.SkillPath)
		r := UpdateResult{ID: it.id, Source: repo, LockHash: it.entry.Hash(), Via: "git clone"}
		dir := filepath.Join(tmp, filepath.FromSlash(folder))
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			r.Status = UpdateGone
			r.Message = "上游已不在 " + it.entry.SkillPath + "（可能已删除或改了路径）"
			results = append(results, r)
			continue
		}
		if len(r.LockHash) == 40 {
			rev := "HEAD^{tree}"
			if folder != "" {
				rev = "HEAD:" + folder
			}
			r.RemoteHash = gitOut(tmp, "rev-parse", "--verify", rev)
			if r.RemoteHash == "" {
				// The path goes through a symlink upstream (e.g. .claude/skills
				// -> skills): hash the folder it resolves to.
				if real, err := filepath.EvalSymlinks(dir); err == nil {
					r.RemoteHash, _ = gitTreeHash(real)
					r.Message = "上游路径经过软链，按解析后的目录内容比对"
				}
			}
		} else {
			r.RemoteHash, _ = contentHash(dir)
		}
		if r.RemoteHash == "" {
			r.Status = UpdateError
			r.Message = "无法计算上游哈希"
		} else if strings.EqualFold(r.RemoteHash, r.LockHash) {
			r.Status = UpdateCurrent
		} else {
			r.Status = UpdateOutdated
		}
		results = append(results, r)
	}
	return results
}

// checkUpdates compares every installed skill that has a lock entry with
// its source, grouped by source and ref like `npx skills check`.
func checkUpdates(ctx context.Context, root string, progress func(UpdateProgress)) (*UpdateReport, error) {
	lib, err := scan(root)
	if err != nil {
		return nil, err
	}
	report := &UpdateReport{Results: map[string]UpdateResult{}}
	groups := map[string][]checkItem{}
	var keys []string
	for _, s := range lib.Skills {
		if s.Kind == KindMissing || s.Lock == nil {
			continue
		}
		e := *s.Lock
		if e.Hash() == "" || e.SkillPath == "" {
			report.Results[s.ID] = UpdateResult{ID: s.ID, Source: normalizeSource(e.Source), Status: UpdateSkipped, Message: "锁记录缺少 skillPath 或哈希"}
			continue
		}
		k := e.Source + "\n" + e.Ref
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], checkItem{s.ID, e})
	}
	sort.Strings(keys)
	var mu sync.Mutex
	done := 0
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for _, k := range keys {
		items := groups[k]
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var res []UpdateResult
			e := items[0].entry
			allTree := true
			for _, it := range items {
				if len(it.entry.Hash()) != 40 {
					allTree = false
				}
			}
			if e.SourceType == "github" && allTree {
				r, err := checkGitHub(ctx, items)
				if err == nil {
					res = r
				}
			}
			if res == nil && ctx.Err() == nil {
				res = checkClone(ctx, items)
			}
			mu.Lock()
			for _, r := range res {
				report.Results[r.ID] = r
			}
			done++
			p := UpdateProgress{Done: done, Total: len(keys), Source: normalizeSource(e.Source)}
			mu.Unlock()
			if progress != nil {
				progress(p)
			}
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, r := range report.Results {
		report.Checked++
		switch r.Status {
		case UpdateOutdated, UpdateGone:
			report.Outdated++
		case UpdateError:
			report.Errors++
		}
	}
	report.CheckedAt = time.Now().Format(time.RFC3339)
	data, _ := json.MarshalIndent(report, "", "  ")
	os.WriteFile(updatesPath(lib.Root), data, 0o644)
	return report, nil
}
