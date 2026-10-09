package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSplitAndParseFrontmatter(t *testing.T) {
	src := "---\nname: demo\ndescription: \"A: quoted\"\nmetadata:\n  version: \"2.0.0\"\ntags: [a, b]\nflag: true\n---\n# Body\n"
	fm, body, ok := splitFrontmatter([]byte(src))
	if !ok || body != "# Body\n" {
		t.Fatalf("split: ok=%v body=%q", ok, body)
	}
	fields, err := parseFrontmatter(fm)
	if err != "" {
		t.Fatal(err)
	}
	if got := fmString(fields, []string{"version"}, []string{"metadata", "version"}); got != "2.0.0" {
		t.Fatalf("version %q", got)
	}
	if got := fmList(fields, []string{"tags"}); len(got) != 2 {
		t.Fatalf("tags %v", got)
	}
	if fields[0].Key != "name" || fields[4].Value.Tag != "bool" {
		t.Fatalf("order/tag: %+v", fields)
	}
}

func TestBrokenYAMLFallsBack(t *testing.T) {
	fields, err := parseFrontmatter("name: x\ndescription: Use when: the user asks: things\n")
	if err == "" {
		t.Skip("yaml accepted it")
	}
	if fmString(fields, []string{"description"}) == "" {
		t.Fatalf("fallback lost description: %+v", fields)
	}
}

func TestNormalizeSource(t *testing.T) {
	for in, want := range map[string]string{
		"mattpocock/skills":                         "mattpocock/skills",
		"https://github.com/vercel-labs/skills.git": "vercel-labs/skills",
		"git@github.com:kepano/obsidian-skills.git": "kepano/obsidian-skills",
		"git@git.example.com:team/tools.git":        "git.example.com/team/tools",
	} {
		if got := normalizeSource(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}

// TestRealSkillsDir scans ~/.agents/skills when it exists and reports what it found.
func TestRealSkillsDir(t *testing.T) {
	root := filepath.Join(homeDir(), ".agents", "skills")
	if _, err := os.Stat(root); err != nil {
		t.Skip("no ~/.agents/skills")
	}
	snapshotsEnabled = false
	defer func() { snapshotsEnabled = true }()
	lib, err := scan(root)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[SkillKind]int{}
	integ := map[string]int{}
	groups := map[string]int{}
	for _, s := range lib.Skills {
		kinds[s.Kind]++
		integ[s.Integrity]++
		if s.Kind != KindMissing {
			groups[s.Group]++
		}
		if s.Error != "" {
			t.Logf("warn %s: %s", s.ID, s.Error)
		}
	}
	t.Logf("lock=%+v", lib.Lock)
	t.Logf("kinds=%v integrity=%v groups=%d", kinds, integ, len(groups))
	t.Logf("groups=%v", groups)
	if len(lib.Skills) == 0 {
		t.Skip("~/.agents/skills is empty")
	}
}
