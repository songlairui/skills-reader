package main

import (
	"bytes"
	"strings"

	"gopkg.in/yaml.v3"
)

// FMField is one key of the frontmatter, in the order the file has them.
// Value is an FMValue.
type FMField struct {
	Key   string  `json:"key"`
	Value FMValue `json:"value"`
}

// FMValue is a YAML value: a scalar, a list or a map.
type FMValue struct {
	Type   string    `json:"type"` // "scalar" | "list" | "map"
	Value  string    `json:"value,omitempty"`
	Tag    string    `json:"tag,omitempty"` // str, bool, int, float, null, timestamp
	Items  []FMValue `json:"items,omitempty"`
	Fields []FMField `json:"fields,omitempty"`
}

// splitFrontmatter separates a leading YAML frontmatter block from the body.
func splitFrontmatter(src []byte) (fm string, body string, ok bool) {
	s := string(bytes.TrimPrefix(src, []byte("\xef\xbb\xbf")))
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], " ") != "---" {
		return "", s, false
	}
	for j := 1; j < len(lines); j++ {
		if t := strings.TrimRight(lines[j], " "); t == "---" || t == "..." {
			return strings.Join(lines[1:j], "\n"), strings.Join(lines[j+1:], "\n"), true
		}
	}
	return "", s, false
}

func nodeToValue(n *yaml.Node) FMValue {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) > 0 {
			return nodeToValue(n.Content[0])
		}
		return FMValue{Type: "scalar", Tag: "null"}
	case yaml.AliasNode:
		if n.Alias != nil {
			return nodeToValue(n.Alias)
		}
	case yaml.SequenceNode:
		v := FMValue{Type: "list", Items: []FMValue{}}
		for _, c := range n.Content {
			v.Items = append(v.Items, nodeToValue(c))
		}
		return v
	case yaml.MappingNode:
		v := FMValue{Type: "map", Fields: []FMField{}}
		for i := 0; i+1 < len(n.Content); i += 2 {
			v.Fields = append(v.Fields, FMField{Key: n.Content[i].Value, Value: nodeToValue(n.Content[i+1])})
		}
		return v
	}
	return FMValue{Type: "scalar", Value: n.Value, Tag: strings.TrimPrefix(n.ShortTag(), "!!")}
}

// parseFrontmatter parses the YAML of a frontmatter block into ordered
// fields. When the YAML is invalid (frequent: unquoted colons in a
// description), it falls back to reading top-level `key: value` lines and
// returns the parse error alongside.
func parseFrontmatter(fm string) ([]FMField, string) {
	if strings.TrimSpace(fm) == "" {
		return nil, ""
	}
	var doc yaml.Node
	err := yaml.Unmarshal([]byte(fm), &doc)
	if err == nil {
		v := nodeToValue(&doc)
		if v.Type == "map" {
			return v.Fields, ""
		}
		return nil, "frontmatter 不是键值映射"
	}
	var fields []FMField
	for _, line := range strings.Split(fm, "\n") {
		if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' {
			if len(fields) > 0 && strings.TrimSpace(line) != "" {
				last := &fields[len(fields)-1]
				last.Value.Value = strings.TrimSpace(last.Value.Value + " " + strings.TrimSpace(line))
			}
			continue
		}
		k, v, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		fields = append(fields, FMField{Key: strings.TrimSpace(k), Value: FMValue{Type: "scalar", Value: v, Tag: "str"}})
	}
	return fields, err.Error()
}

func fmLookup(fields []FMField, path ...string) *FMValue {
	for _, f := range fields {
		if f.Key != path[0] {
			continue
		}
		if len(path) == 1 {
			v := f.Value
			return &v
		}
		if f.Value.Type == "map" {
			return fmLookup(f.Value.Fields, path[1:]...)
		}
	}
	return nil
}

func fmString(fields []FMField, paths ...[]string) string {
	for _, p := range paths {
		if v := fmLookup(fields, p...); v != nil && v.Type == "scalar" && v.Value != "" {
			return v.Value
		}
	}
	return ""
}

func fmList(fields []FMField, paths ...[]string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, p := range paths {
		v := fmLookup(fields, p...)
		if v == nil {
			continue
		}
		switch v.Type {
		case "list":
			for _, it := range v.Items {
				if it.Type == "scalar" {
					add(it.Value)
				}
			}
		case "scalar":
			for _, s := range strings.Split(v.Value, ",") {
				add(s)
			}
		}
	}
	return out
}
