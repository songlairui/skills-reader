package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/egoist/mygo"
)

// Skills reads a skills directory (by default ~/.agents/skills) for the page.
type Skills struct {
	mu   sync.Mutex
	root string
}

func (s *Skills) setRoot(root string) string {
	root = filepath.Clean(expandHome(root))
	s.mu.Lock()
	s.root = root
	s.mu.Unlock()
	return root
}

func (s *Skills) currentRoot() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.root
}

// DefaultRoot returns the default skills directory, ~/.agents/skills.
func (s *Skills) DefaultRoot() string { return defaultRoot() }

// Scan lists every skill of root ("" for the default) with its group, lock
// entry, integrity against the lock, and state against the local snapshot.
func (s *Skills) Scan(root string) (*Library, error) {
	if strings.TrimSpace(root) == "" {
		root = defaultRoot()
	}
	return scan(s.setRoot(root))
}

// Read returns the frontmatter, Markdown body and files of one skill.
func (s *Skills) Read(root, id string) (*SkillDetail, error) {
	s.setRoot(root)
	return readDetail(root, id)
}

// ReadFile returns a file inside a skill folder, as text when it is text.
func (s *Skills) ReadFile(root, id, path string) (*FileContent, error) {
	return readSkillFile(root, id, path)
}

// TakeSnapshot records the current hash of every skill as the local index.
func (s *Skills) TakeSnapshot(root string) (*Library, error) {
	lib, err := scan(s.setRoot(root))
	if err != nil {
		return nil, err
	}
	if err := writeSnapshot(lib); err != nil {
		return nil, err
	}
	return scan(root)
}

// CheckUpdates compares the lock file's folder hashes with the sources,
// streaming progress per source repository.
func (s *Skills) CheckUpdates(ctx context.Context, root string, progress *mygo.Channel[UpdateProgress]) (*UpdateReport, error) {
	return checkUpdates(ctx, s.setRoot(root), func(p UpdateProgress) { progress.Send(p) })
}

// PickRoot asks for another skills directory; "" when canceled.
func (s *Skills) PickRoot(ctx context.Context, current string) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:          mygo.CallerWindow(ctx),
		Title:           "选择 skills 目录",
		DefaultPath:     expandHome(current),
		Directory:       true,
		ShowHiddenFiles: true,
	})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

// Reveal shows a path in Finder.
func (s *Skills) Reveal(path string) { mygo.Shell.ShowItemInFolder(expandHome(path)) }

// OpenExternal opens an http(s) URL in the default browser.
func (s *Skills) OpenExternal(url string) error {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return nil
	}
	return mygo.Shell.OpenExternal(url)
}

func main() {
	skills := &Skills{root: defaultRoot()}
	mygo.Bind(skills)

	// skillfile://localhost/<skill id>/<path> serves files of the current
	// skills directory, for images referenced from SKILL.md.
	mygo.Protocol.HandleFunc("skillfile", func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, "/")
		id, file, ok := strings.Cut(rel, "/")
		if !ok {
			http.NotFound(w, r)
			return
		}
		dir, err := skillDir(skills.currentRoot(), id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		real, err := filepath.EvalSymlinks(dir)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/" + file
		mygo.FileServer(os.DirFS(real)).ServeHTTP(w, r2)
	})

	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:           "Skills Reader",
			Width:           1320,
			Height:          860,
			MinWidth:        860,
			MinHeight:       520,
			TitleBarStyle:   mygo.TitleBarHiddenInset,
			BackgroundColor: "light-dark(#fbfaf8, #17181a)",
			StateKey:        "main",
			URL:             "/",
		})
		probe(win)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
