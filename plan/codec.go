package plan

import (
	"bytes"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

var owned = map[string]bool{"id": true, "aliases": true, "title": true, "slug": true, "status": true, "created": true, "updated": true, "sections": true}

func Parse(path string, data []byte) (*Plan, error) {
	if bytes.IndexByte(data, 0) >= 0 {
		return nil, fmt.Errorf("%s: contains NUL bytes", path)
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("%s: invalid UTF-8", path)
	}
	if bytes.Contains(data, []byte("\r")) {
		return nil, fmt.Errorf("%s: requires LF line endings", path)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		return nil, fmt.Errorf("%s: missing final newline", path)
	}
	s := string(data)
	if !strings.HasPrefix(s, "---\n") {
		return nil, fmt.Errorf("%s: missing frontmatter", path)
	}
	end := strings.Index(s[4:], "\n---\n")
	if end < 0 {
		return nil, fmt.Errorf("%s: frontmatter has no closing fence", path)
	}
	end += 4
	fm, body := s[4:end], s[end+5:]
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(fm), &doc); err != nil {
		return nil, fmt.Errorf("%s: frontmatter: %w", path, err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: frontmatter must be mapping", path)
	}
	p := &Plan{Path: path, Body: body}
	root := doc.Content[0]
	seen := map[string]bool{}
	for i := 0; i < len(root.Content); i += 2 {
		k, v := root.Content[i], root.Content[i+1]
		if seen[k.Value] && owned[k.Value] {
			return nil, fmt.Errorf("%s: duplicate frontmatter key %q", path, k.Value)
		}
		seen[k.Value] = true
		if !owned[k.Value] {
			continue
		}
		var err error
		switch k.Value {
		case "id":
			err = decodeScalar(v, &p.ID)
		case "title":
			err = decodeScalar(v, &p.Title)
		case "slug":
			err = decodeScalar(v, &p.Slug)
		case "status":
			var x string
			err = decodeScalar(v, &x)
			p.Status = Status(x)
		case "aliases":
			err = v.Decode(&p.Aliases)
		case "sections":
			err = v.Decode(&p.Sections)
		case "created":
			err = decodeTime(v, &p.Created)
		case "updated":
			err = decodeTime(v, &p.Updated)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w", path, k.Value, err)
		}
	}
	for _, k := range []string{"id", "aliases", "title", "slug", "status", "created", "updated"} {
		if !seen[k] {
			return nil, fmt.Errorf("%s: missing required key %q", path, k)
		}
	}
	if p.ID == "" || p.Title == "" || !p.Status.Valid() || !ValidSlug(p.Slug) || p.Created.IsZero() || p.Updated.IsZero() {
		return nil, fmt.Errorf("%s: invalid required frontmatter values", path)
	}
	found := false
	for _, a := range p.Aliases {
		if a == p.ID {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("%s: aliases must include id", path)
	}
	summary, graph, err := parseSummary(path, body)
	if err != nil {
		return nil, err
	}
	p.Summary, p.Graph = summary, graph
	return p, nil
}
func decodeScalar(n *yaml.Node, out *string) error {
	if n.Kind != yaml.ScalarNode {
		return fmt.Errorf("must be scalar")
	}
	*out = n.Value
	return nil
}
func decodeTime(n *yaml.Node, out *time.Time) error {
	var s string
	if err := decodeScalar(n, &s); err != nil {
		return err
	}
	t, e := time.Parse(time.RFC3339, s)
	if e != nil {
		return e
	}
	if t.Location() != time.UTC {
		return fmt.Errorf("must be UTC")
	}
	*out = t
	return nil
}

// Encode emits canonical owned frontmatter. Parsed bundles retain their body byte-for-byte.
func Encode(p *Plan) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("nil plan")
	}
	if !p.Status.Valid() {
		return nil, fmt.Errorf("invalid status")
	}
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "id: %s\naliases:\n", p.ID)
	for _, a := range p.Aliases {
		fmt.Fprintf(&b, "  - %s\n", a)
	}
	fmt.Fprintf(&b, "title: %s\nslug: %s\nstatus: %s\ncreated: %s\nupdated: %s\n", p.Title, p.Slug, p.Status, p.Created.UTC().Format(time.RFC3339), p.Updated.UTC().Format(time.RFC3339))
	if len(p.Sections) > 0 {
		b.WriteString("sections:\n")
		for _, s := range p.Sections {
			fmt.Fprintf(&b, "  - %s\n", s)
		}
	}
	b.WriteString("---\n")
	b.WriteString(p.Body)
	if !strings.HasSuffix(p.Body, "\n") {
		b.WriteByte('\n')
	}
	return []byte(b.String()), nil
}
