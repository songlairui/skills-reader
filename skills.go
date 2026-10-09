package main

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// SkillKind tells how a skill got into the directory.
type SkillKind string

const (
	// KindInstalled is a folder recorded in the lock file (installed by `skills add`).
	KindInstalled SkillKind = "installed"
	// KindLinked is a symlink to a folder elsewhere, usually a local repository.
	KindLinked SkillKind = "linked"
	// KindLocal is a folder the lock file does not know.
	KindLocal SkillKind = "local"
	// KindMissing is a lock entry without a folder in this directory.
	KindMissing SkillKind = "missing"
)

// Skill summarizes one skill for the sidebar.
type Skill struct {
	// ID is the folder name in the skills directory.
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Kind        SkillKind `json:"kind"`
	// Group is the skill group key: the source repository ("owner/repo",
	// "host/owner/repo"), or "local" for unmanaged folders.
	Group string `json:"group"`
	// SubGroup is the category inside the source, from the folder that holds
	// the skill in its repository (e.g. "engineering" for
	// skills/engineering/tdd/SKILL.md), or the lock's pluginName.
	SubGroup   string     `json:"subGroup,omitempty"`
	Version    string     `json:"version,omitempty"`
	Tags       []string   `json:"tags"`
	LinkTarget string     `json:"linkTarget,omitempty"`
	Lock       *LockEntry `json:"lock,omitempty"`
	// LocalHash is the git tree hash of the folder as it is on disk.
	LocalHash string `json:"localHash,omitempty"`
	// Integrity compares the folder with the hash the lock recorded:
	// "match", "differs" or "" when there is nothing to compare.
	Integrity string `json:"integrity,omitempty"`
	// Snapshot compares the folder with the last local index snapshot:
	// "new", "changed", "same" or "" without a snapshot.
	Snapshot  string `json:"snapshot,omitempty"`
	FileCount int    `json:"fileCount"`
	Modified  string `json:"modified,omitempty"`
	HasSkill  bool   `json:"hasSkillMd"`
	Error     string `json:"error,omitempty"`
}

// Library is the scanned skills directory.
type Library struct {
	Root     string        `json:"root"`
	Exists   bool          `json:"exists"`
	Lock     *LockInfo     `json:"lock,omitempty"`
	Skills   []Skill       `json:"skills"`
	Snapshot *SnapshotInfo `json:"snapshot,omitempty"`
	Updates  *UpdateReport `json:"updates,omitempty"`
	// RemovedSinceSnapshot lists skills the snapshot had that are gone.
	RemovedSinceSnapshot []string `json:"removedSinceSnapshot"`
	ScannedAt            string   `json:"scannedAt"`
}

func expandHome(p string) string {
	p = strings.TrimSpace(p)
	if p == "~" {
		return homeDir()
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(homeDir(), p[2:])
	}
	return p
}

func defaultRoot() string { return filepath.Join(homeDir(), ".agents", "skills") }

// repoInfo finds the git repository holding dir and its origin URL.
func repoInfo(dir string) (root, origin string) {
	d := dir
	for {
		if st, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			root = d
			if st.IsDir() {
				origin = readOrigin(filepath.Join(d, ".git", "config"))
			}
			return
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", ""
		}
		d = parent
	}
}

func readOrigin(configPath string) string {
	f, err := os.Open(configPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	in := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			in = line == `[remote "origin"]`
			continue
		}
		if in {
			if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == "url" {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

func subGroupFromPath(skillPath string) string {
	dir := path.Dir(path.Dir(skillPath)) // folder holding the skill folder
	if dir == "." || dir == "/" || dir == "" {
		return ""
	}
	base := path.Base(dir)
	if base == "skills" || base == "." {
		return ""
	}
	return base
}

func countFiles(dir string) (int, time.Time) {
	n := 0
	var latest time.Time
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			n++
			if info, err := d.Info(); err == nil && info.ModTime().After(latest) {
				latest = info.ModTime()
			}
		}
		return nil
	})
	return n, latest
}

func readSkillHeader(dir string) (fields []FMField, fmErr string, has bool) {
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return nil, "", false
	}
	fm, _, ok := splitFrontmatter(data)
	if !ok {
		return nil, "", true
	}
	fields, fmErr = parseFrontmatter(fm)
	return fields, fmErr, true
}

// scan reads every skill folder of root.
func scan(root string) (*Library, error) {
	root = filepath.Clean(expandHome(root))
	lib := &Library{Root: root, Skills: []Skill{}, RemovedSinceSnapshot: []string{}, ScannedAt: time.Now().Format(time.RFC3339)}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return lib, nil
		}
		return nil, err
	}
	lib.Exists = true
	lockInfo, lock := findLock(root)
	lib.Lock = lockInfo

	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		names = append(names, e.Name())
	}
	skills := make([]Skill, len(names))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, name := range names {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			skills[i] = loadSkill(root, name, lock)
		}(i, name)
	}
	wg.Wait()
	present := map[string]bool{}
	for _, s := range skills {
		if s.ID == "" {
			continue
		}
		present[s.ID] = true
		lib.Skills = append(lib.Skills, s)
	}
	for name, e := range lock {
		if present[name] {
			continue
		}
		e := e
		sub := e.PluginName
		if sg := subGroupFromPath(e.SkillPath); sg != "" {
			sub = sg
		}
		lib.Skills = append(lib.Skills, Skill{ID: name, Name: name, Kind: KindMissing, Group: normalizeSource(e.Source), SubGroup: sub, Lock: &e, Tags: []string{}})
	}
	sort.Slice(lib.Skills, func(i, j int) bool { return lib.Skills[i].ID < lib.Skills[j].ID })
	applySnapshot(lib)
	if snapshotsEnabled {
		lib.Updates = loadUpdateReport(root)
	}
	return lib, nil
}

func loadSkill(root, name string, lock map[string]LockEntry) Skill {
	p := filepath.Join(root, name)
	li, err := os.Lstat(p)
	if err != nil {
		return Skill{}
	}
	s := Skill{ID: name, Name: name, Tags: []string{}}
	real := p
	if li.Mode()&fs.ModeSymlink != 0 {
		target, _ := os.Readlink(p)
		s.LinkTarget = target
		r, err := filepath.EvalSymlinks(p)
		if err != nil {
			s.Error = "软链目标不存在: " + target
			s.Kind = KindLinked
			s.Group = "local"
			return s
		}
		real = r
	}
	st, err := os.Stat(real)
	if err != nil || !st.IsDir() {
		return Skill{}
	}
	fields, fmErr, has := readSkillHeader(real)
	s.HasSkill = has
	if fmErr != "" {
		s.Error = "frontmatter: " + fmErr
	}
	if n := fmString(fields, []string{"name"}); n != "" {
		s.Name = n
	}
	s.Description = fmString(fields, []string{"description"})
	s.Version = fmString(fields, []string{"version"}, []string{"metadata", "version"})
	s.Tags = fmList(fields, []string{"tags"}, []string{"keywords"}, []string{"category"}, []string{"categories"}, []string{"metadata", "tags"}, []string{"metadata", "category"})
	if s.Tags == nil {
		s.Tags = []string{}
	}
	n, mod := countFiles(real)
	s.FileCount = n
	if !mod.IsZero() {
		s.Modified = mod.Format(time.RFC3339)
	}
	if h, err := gitTreeHash(real); err == nil {
		s.LocalHash = h
	}

	if e, ok := lock[name]; ok {
		e := e
		s.Lock = &e
		s.Group = normalizeSource(e.Source)
		s.SubGroup = e.PluginName
		if sg := subGroupFromPath(e.SkillPath); sg != "" {
			s.SubGroup = sg
		}
		if s.LinkTarget != "" {
			s.Kind = KindLinked
		} else {
			s.Kind = KindInstalled
		}
		if want := e.Hash(); want != "" {
			local := s.LocalHash
			if len(want) == 64 {
				local, _ = contentHash(real)
			}
			if strings.EqualFold(local, want) {
				s.Integrity = "match"
			} else {
				s.Integrity = "differs"
			}
		}
		return s
	}
	if s.LinkTarget != "" {
		s.Kind = KindLinked
		repoRoot, origin := repoInfo(real)
		switch {
		case origin != "":
			s.Group = normalizeSource(origin)
		case repoRoot != "":
			s.Group = "local:" + filepath.Base(repoRoot)
		default:
			s.Group = "local"
		}
		if repoRoot != "" {
			if rel, err := filepath.Rel(repoRoot, real); err == nil {
				s.SubGroup = subGroupFromPath(filepath.ToSlash(filepath.Join(rel, "SKILL.md")))
			}
		}
		return s
	}
	s.Kind = KindLocal
	s.Group = "local"
	return s
}

// FileEntry is a file or folder inside a skill.
type FileEntry struct {
	Path string `json:"path"`
	Dir  bool   `json:"dir"`
	Size int64  `json:"size"`
}

// GitInfo describes the repository a linked skill lives in.
type GitInfo struct {
	Root       string   `json:"root"`
	Origin     string   `json:"origin,omitempty"`
	Branch     string   `json:"branch,omitempty"`
	LastCommit string   `json:"lastCommit,omitempty"`
	LastDate   string   `json:"lastDate,omitempty"`
	LastTitle  string   `json:"lastTitle,omitempty"`
	Dirty      []string `json:"dirty"`
}

// SkillDetail is everything the reader shows for one skill.
type SkillDetail struct {
	Skill            Skill       `json:"skill"`
	Path             string      `json:"path"`
	RealPath         string      `json:"realPath"`
	Frontmatter      []FMField   `json:"frontmatter"`
	FrontmatterRaw   string      `json:"frontmatterRaw"`
	FrontmatterError string      `json:"frontmatterError,omitempty"`
	Body             string      `json:"body"`
	Files            []FileEntry `json:"files"`
	Git              *GitInfo    `json:"git,omitempty"`
}

func gitBin() string {
	for _, p := range []string{"/usr/bin/git", "/opt/homebrew/bin/git", "/usr/local/bin/git"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("git"); err == nil {
		return p
	}
	return "git"
}

func gitOut(dir string, args ...string) string {
	cmd := exec.Command(gitBin(), append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func skillDir(root, id string) (string, error) {
	root = filepath.Clean(expandHome(root))
	if id == "" || strings.ContainsAny(id, `/\`) || id == "." || id == ".." {
		return "", fmt.Errorf("无效的 skill: %q", id)
	}
	return filepath.Join(root, id), nil
}

func readDetail(root, id string) (*SkillDetail, error) {
	p, err := skillDir(root, id)
	if err != nil {
		return nil, err
	}
	_, lock := findLock(filepath.Clean(expandHome(root)))
	s := loadSkill(filepath.Clean(expandHome(root)), id, lock)
	if s.ID == "" {
		e, ok := lock[id]
		if !ok {
			return nil, fmt.Errorf("找不到 skill %q", id)
		}
		sub := e.PluginName
		if sg := subGroupFromPath(e.SkillPath); sg != "" {
			sub = sg
		}
		return &SkillDetail{Skill: Skill{ID: id, Name: id, Kind: KindMissing, Group: normalizeSource(e.Source), SubGroup: sub, Lock: &e, Tags: []string{}}, Path: p, Frontmatter: []FMField{}, Files: []FileEntry{}}, nil
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		real = p
	}
	d := &SkillDetail{Skill: s, Path: p, RealPath: real, Frontmatter: []FMField{}, Files: []FileEntry{}}
	if data, err := os.ReadFile(filepath.Join(real, "SKILL.md")); err == nil {
		fm, body, ok := splitFrontmatter(data)
		d.Body = body
		if ok {
			d.FrontmatterRaw = fm
			fields, fmErr := parseFrontmatter(fm)
			if fields != nil {
				d.Frontmatter = fields
			}
			d.FrontmatterError = fmErr
		}
	}
	filepath.WalkDir(real, func(fp string, de fs.DirEntry, err error) error {
		if err != nil || fp == real {
			return nil
		}
		if de.IsDir() && (de.Name() == ".git" || de.Name() == "node_modules") {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(real, fp)
		fe := FileEntry{Path: filepath.ToSlash(rel), Dir: de.IsDir()}
		if info, err := de.Info(); err == nil && !de.IsDir() {
			fe.Size = info.Size()
		}
		if len(d.Files) < 2000 {
			d.Files = append(d.Files, fe)
		}
		return nil
	})
	if s.Kind == KindLinked {
		if repoRoot, origin := repoInfo(real); repoRoot != "" {
			g := &GitInfo{Root: repoRoot, Origin: origin, Dirty: []string{}}
			g.Branch = gitOut(repoRoot, "rev-parse", "--abbrev-ref", "HEAD")
			if line := gitOut(real, "log", "-1", "--format=%h%x00%cI%x00%s", "--", "."); line != "" {
				parts := strings.SplitN(line, "\x00", 3)
				if len(parts) == 3 {
					g.LastCommit, g.LastDate, g.LastTitle = parts[0], parts[1], parts[2]
				}
			}
			if st := gitOut(real, "status", "--porcelain", "--", "."); st != "" {
				for _, l := range strings.Split(st, "\n") {
					g.Dirty = append(g.Dirty, strings.TrimSpace(l))
				}
			}
			d.Git = g
		}
	}
	return d, nil
}

// FileContent is a file of a skill.
type FileContent struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	Text      string `json:"text"`
	Binary    bool   `json:"binary"`
	Truncated bool   `json:"truncated"`
}

const maxFileBytes = 1 << 20

func readSkillFile(root, id, rel string) (*FileContent, error) {
	p, err := skillDir(root, id)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return nil, err
	}
	rel = filepath.ToSlash(filepath.Clean("/" + rel))[1:]
	if rel == "" || !fs.ValidPath(rel) {
		return nil, fmt.Errorf("无效路径 %q", rel)
	}
	full := filepath.Join(real, filepath.FromSlash(rel))
	info, err := os.Stat(full)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s 是目录", rel)
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, min(info.Size(), maxFileBytes))
	n, _ := f.Read(buf)
	buf = buf[:n]
	fc := &FileContent{Path: rel, Size: info.Size(), Truncated: info.Size() > maxFileBytes}
	if !utf8.Valid(buf) && !fc.Truncated || strings.ContainsRune(string(buf[:min(len(buf), 8000)]), 0) {
		fc.Binary = true
		return fc, nil
	}
	fc.Text = string(buf)
	return fc, nil
}
