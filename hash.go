package main

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// gitTreeHash computes the git tree object id of dir, the same value as
// `git rev-parse HEAD:<dir>` in a repository holding exactly these files, and
// the value the skills CLI stores as skillFolderHash for GitHub sources.
// Empty directories are left out like git does; .git and .DS_Store too.
func gitTreeHash(dir string) (string, error) {
	sum, err := treeHash(dir)
	if err != nil {
		return "", err
	}
	if sum == nil {
		return "", nil
	}
	return hex.EncodeToString(sum), nil
}

func gitObject(kind string, body []byte) []byte {
	h := sha1.New()
	fmt.Fprintf(h, "%s %d\x00", kind, len(body))
	h.Write(body)
	return h.Sum(nil)
}

func treeHash(dir string) ([]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	type ent struct {
		sortKey, name, mode string
		sum                 []byte
	}
	var ents []ent
	for _, e := range entries {
		name := e.Name()
		if name == ".git" || name == ".DS_Store" {
			continue
		}
		p := filepath.Join(dir, name)
		info, err := os.Lstat(p)
		if err != nil {
			return nil, err
		}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return nil, err
			}
			ents = append(ents, ent{name, name, "120000", gitObject("blob", []byte(target))})
		case info.IsDir():
			sum, err := treeHash(p)
			if err != nil {
				return nil, err
			}
			if sum != nil {
				ents = append(ents, ent{name + "/", name, "40000", sum})
			}
		case info.Mode().IsRegular():
			data, err := os.ReadFile(p)
			if err != nil {
				return nil, err
			}
			mode := "100644"
			if info.Mode()&0o111 != 0 {
				mode = "100755"
			}
			ents = append(ents, ent{name, name, mode, gitObject("blob", data)})
		}
	}
	if len(ents) == 0 {
		return nil, nil
	}
	sort.Slice(ents, func(i, j int) bool { return ents[i].sortKey < ents[j].sortKey })
	var body []byte
	for _, e := range ents {
		body = append(body, e.mode...)
		body = append(body, ' ')
		body = append(body, e.name...)
		body = append(body, 0)
		body = append(body, e.sum...)
	}
	return gitObject("tree", body), nil
}

var (
	collatorMu sync.Mutex
	collator   = collate.New(language.Und)
)

// contentHash mirrors computeSkillFolderHash of the skills CLI (used for git
// sources and skills-lock.json): sha256 over relative path + content of every
// file, sorted with localeCompare (approximated with the CLDR root collation).
func contentHash(dir string) (string, error) {
	type file struct {
		rel  string
		path string
	}
	var files []file
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != dir && (d.Name() == ".git" || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		files = append(files, file{filepath.ToSlash(rel), p})
		return nil
	})
	if err != nil {
		return "", err
	}
	collatorMu.Lock()
	sort.SliceStable(files, func(i, j int) bool { return collator.CompareString(files[i].rel, files[j].rel) < 0 })
	collatorMu.Unlock()
	h := sha256.New()
	for _, f := range files {
		data, err := os.ReadFile(f.path)
		if err != nil {
			return "", err
		}
		h.Write([]byte(f.rel))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// hashLike computes the local hash of dir in the same format as want: a git
// tree id for 40 hex characters, the sha256 content hash for 64.
func hashLike(dir, want string) (string, error) {
	if len(strings.TrimSpace(want)) == 64 {
		return contentHash(dir)
	}
	return gitTreeHash(dir)
}
