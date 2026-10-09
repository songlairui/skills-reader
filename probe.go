package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// probe is a self-check for development: with SKILLS_READER_PROBE=<dir> the
// app drives its own window (wait for the list, optionally run the update
// check, open a few skills) and writes what the page shows, plus the page's
// HTML, into dir. Nothing happens without the variable.
func probe(win *mygo.Window) {
	dir := os.Getenv("SKILLS_READER_PROBE")
	if dir == "" {
		return
	}
	os.MkdirAll(dir, 0o755)
	eval := func(code string) any {
		v, err := win.Page().Eval(code)
		if err != nil {
			log.Printf("probe eval: %v", err)
		}
		return v
	}
	waitFor := func(cond string, timeout time.Duration) bool {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			if v, _ := win.Page().Eval("return !!(" + cond + ")"); v == true {
				return true
			}
			time.Sleep(300 * time.Millisecond)
		}
		return false
	}
	const stateJS = `return {
		items: document.querySelectorAll('#list .item').length,
		groups: [...document.querySelectorAll('#list .group-head')].map(g => g.innerText.replace(/\s+/g,' ').trim()),
		filters: [...document.querySelectorAll('#filters .chip')].map(c => c.innerText.replace(/\s+/g,' ').trim()),
		foot: document.querySelector('#foot').innerText,
		title: document.querySelector('.fm-title')?.textContent ?? null,
		badges: [...document.querySelectorAll('.fm-card .badge')].map(b => b.textContent),
		fmRows: [...document.querySelectorAll('.fm-card .fm-grid > dt')].map(b => b.innerText),
		source: document.querySelector('.src-card')?.innerText ?? null,
		headings: document.querySelectorAll('.markdown h2, .markdown h3').length,
		codeBlocks: document.querySelectorAll('.markdown pre.code').length,
		highlighted: document.querySelectorAll('.markdown pre.code [class^=hljs-]').length,
		tables: document.querySelectorAll('.markdown table').length,
		files: document.querySelectorAll('.files button').length,
		toc: document.querySelectorAll('.toc a').length,
		dark: matchMedia('(prefers-color-scheme: dark)').matches,
	}`
	dump := func(name string) {
		v := eval(stateJS)
		data, _ := json.MarshalIndent(v, "", "  ")
		os.WriteFile(fmt.Sprintf("%s/%s.json", dir, name), data, 0o644)
		html := eval(`return '<!doctype html>' + document.documentElement.outerHTML`)
		if s, ok := html.(string); ok {
			os.WriteFile(fmt.Sprintf("%s/%s.html", dir, name), []byte(s), 0o644)
		}
	}
	go func() {
		if !waitFor("document.querySelectorAll('#list .item').length > 0 && document.querySelector('.fm-title')", 20*time.Second) {
			os.WriteFile(dir+"/error.txt", []byte("list never appeared"), 0o644)
		}
		time.Sleep(500 * time.Millisecond)
		dump("01-start")
		if os.Getenv("SKILLS_READER_PROBE_CHECK") == "1" {
			eval(`document.querySelector('#checkBtn')?.click()`)
			time.Sleep(time.Second)
			waitFor("!document.querySelector('#checkBtn')?.disabled", 5*time.Minute)
			time.Sleep(500 * time.Millisecond)
			dump("02-after-check")
		}
		for i, id := range strings.Split(os.Getenv("SKILLS_READER_PROBE_OPEN"), ",") {
			if id == "" {
				continue
			}
			eval(fmt.Sprintf(`document.querySelector('#list .item[data-id=%q]')?.click()`, id))
			time.Sleep(1200 * time.Millisecond)
			dump(fmt.Sprintf("%02d-%s", i+3, id))
		}
		os.WriteFile(dir+"/done.txt", []byte(time.Now().Format(time.RFC3339)), 0o644)
		if os.Getenv("SKILLS_READER_PROBE_QUIT") == "1" {
			mygo.App.Quit()
		}
	}()
}
