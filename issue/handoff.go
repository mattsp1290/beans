package issue

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Handoff is a versioned continuation note. Its body is ordinary Markdown.
type Handoff struct {
	ID       string
	Aliases  []string
	Title    string
	Issue    Link
	Created  time.Time
	Updated  time.Time
	Extra    yaml.Node
	Body     string
	Path     string
	Project  string
	Archived bool
	orig     *handoffOriginal
}

type handoffOriginal struct {
	fmLines []string
	spans   []keySpan
	snap    Handoff
}

var handoffKeys = []string{"id", "aliases", "title", "issue", "created", "updated"}

// ParseHandoff decodes one handoff file while preserving unowned frontmatter.
func ParseHandoff(path string, data []byte) (*Handoff, error) {
	if strings.Contains(string(data), "\r\n") {
		return nil, fmt.Errorf("%s: has Windows line endings (\\r\\n); bn requires \\n", path)
	}
	fm, body, start, err := splitFrontmatter(path, data)
	if err != nil {
		return nil, err
	}
	root, err := parseMapping(path, fm, start)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSuffix(fm, "\n"), "\n")
	if fm == "" {
		lines = nil
	}
	h := &Handoff{Path: path, Body: body, Extra: yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}, orig: &handoffOriginal{fmLines: lines}}
	var ok bool
	h.Project, h.Archived, ok = handoffPathInfo(path)
	if !ok {
		return nil, fmt.Errorf("%s: not a handoff path", path)
	}
	seen := map[string]bool{}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i], root.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("%s: line %d: frontmatter keys must be strings", path, start+k.Line-1)
		}
		if seen[k.Value] {
			return nil, fmt.Errorf("%s: line %d: duplicate frontmatter key %q", path, start+k.Line-1, k.Value)
		}
		seen[k.Value] = true
		h.orig.spans = append(h.orig.spans, keySpan{key: k.Value, start: k.Line - 1, keyNode: k, valueNode: v})
		var e error
		switch k.Value {
		case "id":
			e = scalarInto("id", v, &h.ID)
		case "aliases":
			h.Aliases, e = stringList("aliases", v)
		case "title":
			e = scalarInto("title", v, &h.Title)
		case "issue":
			var s string
			e = scalarInto("issue", v, &s)
			h.Issue = ParseLink(s)
		case "created", "updated":
			var s string
			e = scalarInto(k.Value, v, &s)
			if e == nil {
				t, x := time.Parse(time.RFC3339, strings.TrimSpace(s))
				if x != nil {
					e = fmt.Errorf("%s must be an RFC3339 timestamp, got %q", k.Value, s)
				} else if k.Value == "created" {
					h.Created = t
				} else {
					h.Updated = t
				}
			}
		default:
			h.Extra.Content = append(h.Extra.Content, k, v)
		}
		if e != nil {
			return nil, fmt.Errorf("%s: line %d: %w", path, start+v.Line-1, e)
		}
	}
	if h.ID == "" || h.Title == "" || h.Created.IsZero() || h.Updated.IsZero() {
		return nil, fmt.Errorf("%s: frontmatter is missing required handoff fields", path)
	}
	if !ValidID(h.ID) {
		return nil, fmt.Errorf("%s: invalid handoff id %q", path, h.ID)
	}
	if !matchesHandoffFilename(path, h.ID) {
		return nil, fmt.Errorf("%s: filename does not match handoff id %q", path, h.ID)
	}
	closeSpans(h.orig.spans, lines)
	h.orig.snap = h.snapshot()
	return h, nil
}

func handoffPathInfo(path string) (project string, archived, ok bool) {
	segs := strings.Split(filepath.ToSlash(path), "/")
	if len(segs) == 4 && segs[0] == "projects" && segs[1] != "" && segs[2] == "handoffs" && strings.HasSuffix(segs[3], ".md") {
		return segs[1], false, true
	}
	if len(segs) == 6 && segs[0] == "projects" && segs[1] != "" && segs[2] == "handoffs" && segs[3] == "archive" && validArchiveYear(segs[4]) && strings.HasSuffix(segs[5], ".md") {
		return segs[1], true, true
	}
	return "", false, false
}

func validArchiveYear(s string) bool {
	if len(s) != 4 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func matchesHandoffFilename(path, id string) bool {
	n := strings.TrimSuffix(filepath.Base(path), ".md")
	return n == id || strings.HasPrefix(n, id+"-")
}
func (h *Handoff) snapshot() Handoff {
	return Handoff{ID: h.ID, Aliases: append([]string(nil), h.Aliases...), Title: h.Title, Issue: h.Issue, Created: h.Created, Updated: h.Updated}
}
func (h *Handoff) ownedNode(k string) *yaml.Node {
	switch k {
	case "id":
		return strNode(h.ID)
	case "aliases":
		a := h.Aliases
		if len(a) == 0 {
			a = []string{h.ID}
		}
		return flowSeq(a)
	case "title":
		return strNode(h.Title)
	case "issue":
		if h.Issue.IsZero() {
			return nil
		}
		return strNode(h.Issue.Raw)
	case "created":
		return timeNode(h.Created)
	case "updated":
		return timeNode(h.Updated)
	}
	return nil
}

// EncodeHandoff preserves a parsed note byte-for-byte unless owned fields changed.
func EncodeHandoff(h *Handoff) ([]byte, error) {
	if h.orig == nil {
		root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, k := range handoffKeys {
			if v := h.ownedNode(k); v != nil {
				root.Content = append(root.Content, keyNode(k), v)
			}
		}
		root.Content = append(root.Content, h.Extra.Content...)
		fm, e := encodeNode(root)
		if e != nil {
			return nil, e
		}
		return []byte(fence + "\n" + fm + fence + "\n" + h.Body), nil
	}
	s := h.orig.snap
	vals := map[string]*yaml.Node{}
	if s.ID != h.ID {
		vals["id"] = h.ownedNode("id")
	}
	if !equalStrings(s.Aliases, h.Aliases) {
		vals["aliases"] = h.ownedNode("aliases")
	}
	if s.Title != h.Title {
		vals["title"] = h.ownedNode("title")
	}
	if s.Issue != h.Issue {
		vals["issue"] = h.ownedNode("issue")
	}
	if !s.Created.Equal(h.Created) {
		vals["created"] = h.ownedNode("created")
	}
	if !s.Updated.Equal(h.Updated) {
		vals["updated"] = h.ownedNode("updated")
	}
	lines, e := spliceFrontmatter(h.orig.fmLines, h.orig.spans, handoffKeys, vals)
	if e != nil {
		return nil, e
	}
	var b strings.Builder
	writeFrontmatter(&b, lines)
	b.WriteString(h.Body)
	return []byte(b.String()), nil
}
