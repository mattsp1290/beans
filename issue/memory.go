package issue

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Memory is one memories/<key>.md file.
type Memory struct {
	Key     string
	Type    string // user, feedback, project, reference, or ""
	Tags    []string
	Created time.Time
	Updated time.Time

	// Extra holds user-owned frontmatter keys (MappingNode), preserved verbatim.
	Extra yaml.Node
	// Body is the markdown after the frontmatter, verbatim.
	Body string

	Path    string
	Project string

	orig *memoryOriginal
}

type memoryOriginal struct {
	fmLines []string
	spans   []keySpan
	snap    Memory
}

var memoryKeys = []string{"key", "type", "tags", "created", "updated"}

// MemoryTypes is the accepted vocabulary of Memory.Type (plus empty).
var MemoryTypes = []string{"user", "feedback", "project", "reference"}

// memoryKeyRe is the key grammar.
var memoryKeyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// MemoryKeyMaxLen caps the key length.
const MemoryKeyMaxLen = 80

// ValidMemoryKey reports whether key matches the grammar and length limit.
func ValidMemoryKey(key string) bool {
	return len(key) <= MemoryKeyMaxLen && memoryKeyRe.MatchString(key)
}

// ValidMemoryType reports whether typ is empty or one of MemoryTypes.
func ValidMemoryType(typ string) bool {
	if typ == "" {
		return true
	}
	for _, t := range MemoryTypes {
		if t == typ {
			return true
		}
	}
	return false
}

// ParseMemory decodes a memory file. key is required; created and updated are
// optional and zero when absent.
func ParseMemory(path string, data []byte) (*Memory, error) {
	if strings.Contains(string(data), "\r\n") {
		return nil, fmt.Errorf("%s: has Windows line endings (\\r\\n); bn requires \\n", path)
	}
	fmText, body, fmStart, err := splitFrontmatter(path, data)
	if err != nil {
		return nil, err
	}
	root, err := parseMapping(path, fmText, fmStart)
	if err != nil {
		return nil, err
	}
	fmLines := strings.Split(strings.TrimSuffix(fmText, "\n"), "\n")
	if fmText == "" {
		fmLines = nil
	}
	m := &Memory{Path: path, Body: body, orig: &memoryOriginal{fmLines: fmLines}}
	m.Extra = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	m.Project, _ = pathInfo(path)
	seen := map[string]bool{}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i], root.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("%s: line %d: frontmatter keys must be strings", path, fmStart+k.Line-1)
		}
		m.orig.spans = append(m.orig.spans, keySpan{key: k.Value, start: k.Line - 1, keyNode: k, valueNode: v})
		if seen[k.Value] {
			return nil, fmt.Errorf("%s: line %d: duplicate frontmatter key %q", path, fmStart+k.Line-1, k.Value)
		}
		var perr error
		switch k.Value {
		case "key":
			perr = scalarInto("key", v, &m.Key)
		case "type":
			perr = scalarInto("type", v, &m.Type)
		case "tags":
			m.Tags, perr = stringList("tags", v)
		case "created", "updated":
			var s string
			if perr = scalarInto(k.Value, v, &s); perr == nil && strings.TrimSpace(s) != "" {
				t, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
				if err != nil {
					perr = fmt.Errorf("%s must be an RFC3339 timestamp, got %q", k.Value, s)
				} else if k.Value == "created" {
					m.Created = t
				} else {
					m.Updated = t
				}
			}
		default:
			m.Extra.Content = append(m.Extra.Content, k, v)
			continue
		}
		if perr != nil {
			return nil, fmt.Errorf("%s: line %d: %w", path, fmStart+v.Line-1, perr)
		}
		seen[k.Value] = true
	}
	if m.Key == "" {
		return nil, fmt.Errorf("%s: frontmatter is missing required key \"key\"", path)
	}
	closeSpans(m.orig.spans, fmLines)
	m.orig.snap = m.snapshot()
	return m, nil
}

func (m *Memory) snapshot() Memory {
	return Memory{Key: m.Key, Type: m.Type, Tags: append([]string(nil), m.Tags...), Created: m.Created, Updated: m.Updated}
}

func (m *Memory) ownedNode(key string) *yaml.Node {
	switch key {
	case "key":
		return strNode(m.Key)
	case "type":
		if m.Type == "" {
			return nil
		}
		return strNode(m.Type)
	case "tags":
		if len(m.Tags) == 0 {
			return nil
		}
		return flowSeq(m.Tags)
	case "created":
		if m.Created.IsZero() {
			return nil
		}
		return timeNode(m.Created)
	case "updated":
		if m.Updated.IsZero() {
			return nil
		}
		return timeNode(m.Updated)
	}
	return nil
}

// EncodeMemory renders the memory. For a parsed memory only changed keys are
// re-emitted; the body is used as it stands.
func EncodeMemory(m *Memory) ([]byte, error) {
	var b strings.Builder
	if m.orig == nil {
		root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, key := range memoryKeys {
			if v := m.ownedNode(key); v != nil {
				root.Content = append(root.Content, keyNode(key), v)
			}
		}
		if m.Extra.Kind == yaml.MappingNode {
			root.Content = append(root.Content, m.Extra.Content...)
		}
		fm, err := encodeNode(root)
		if err != nil {
			return nil, err
		}
		b.WriteString(fence + "\n")
		b.WriteString(fm)
		b.WriteString(fence + "\n")
		b.WriteString(m.Body)
		return []byte(b.String()), nil
	}
	snap := m.orig.snap
	values := map[string]*yaml.Node{}
	if snap.Key != m.Key {
		values["key"] = m.ownedNode("key")
	}
	if snap.Type != m.Type {
		values["type"] = m.ownedNode("type")
	}
	if !equalStrings(snap.Tags, m.Tags) {
		values["tags"] = m.ownedNode("tags")
	}
	if !snap.Created.Equal(m.Created) {
		values["created"] = m.ownedNode("created")
	}
	if !snap.Updated.Equal(m.Updated) {
		values["updated"] = m.ownedNode("updated")
	}
	fmLines, err := spliceFrontmatter(m.orig.fmLines, m.orig.spans, memoryKeys, values)
	if err != nil {
		return nil, err
	}
	writeFrontmatter(&b, fmLines)
	b.WriteString(m.Body)
	return []byte(b.String()), nil
}
