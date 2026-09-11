package issue

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ParseRequest decodes one request file while retaining the original bytes
// necessary for minimal-splice encoding.
func ParseRequest(path string, data []byte) (*Request, error) {
	if bytes.Contains(data, []byte("\r\n")) {
		return nil, fmt.Errorf("%s: has Windows line endings (\\r\\n); bn requires \\n", path)
	}
	project, err := requestPathInfo(path)
	if err != nil {
		return nil, err
	}
	fmText, bodyText, fmStart, err := splitFrontmatter(path, data)
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
	r := &Request{Path: path, Project: project, orig: &requestOriginal{fmLines: fmLines}}
	r.Extra = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	seen := map[string]bool{}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i], root.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("%s: line %d: frontmatter keys must be strings", path, fmStart+k.Line-1)
		}
		r.orig.spans = append(r.orig.spans, keySpan{key: k.Value, start: k.Line - 1, keyNode: k, valueNode: v})
		if !requestKeySet[k.Value] {
			r.Extra.Content = append(r.Extra.Content, k, v)
			continue
		}
		if seen[k.Value] {
			return nil, fmt.Errorf("%s: line %d: duplicate frontmatter key %q", path, fmStart+k.Line-1, k.Value)
		}
		seen[k.Value] = true
		if err := r.readOwned(k.Value, v); err != nil {
			return nil, fmt.Errorf("%s: line %d: %w", path, fmStart+v.Line-1, err)
		}
	}
	for _, key := range []string{"id", "aliases", "title", "status", "priority", "created", "updated"} {
		if !seen[key] {
			return nil, fmt.Errorf("%s: frontmatter is missing required key %q", path, key)
		}
	}
	if err := r.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	closeSpans(r.orig.spans, fmLines)
	r.Body, r.orig.rawLog, r.orig.tail = splitRequestBody(bodyText)
	r.Log = parseLogSection(r.orig.rawLog)
	r.orig.logLen = len(r.Log)
	r.orig.snap = r.snapshot()
	return r, nil
}

func (r *Request) readOwned(key string, v *yaml.Node) error {
	switch key {
	case "id":
		return scalarInto(key, v, &r.ID)
	case "title":
		return scalarInto(key, v, &r.Title)
	case "status":
		return scalarInto(key, v, &r.Status)
	case "requested_by":
		return scalarInto(key, v, &r.RequestedBy)
	case "aliases":
		var err error
		r.Aliases, err = stringList(key, v)
		return err
	case "labels":
		var err error
		r.Labels, err = stringList(key, v)
		return err
	case "issues":
		ss, err := stringList(key, v)
		if err != nil {
			return err
		}
		for _, s := range ss {
			if strings.TrimSpace(s) != "" {
				r.Issues = append(r.Issues, ParseLink(s))
			}
		}
		return nil
	case "priority":
		var s string
		if err := scalarInto(key, v, &s); err != nil {
			return err
		}
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return fmt.Errorf("priority must be an integer, got %q", s)
		}
		r.Priority = n
		return nil
	case "created", "updated":
		var s string
		if err := scalarInto(key, v, &s); err != nil {
			return err
		}
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
		if err != nil {
			return fmt.Errorf("%s must be an RFC3339 timestamp, got %q", key, s)
		}
		if key == "created" {
			r.Created = t
		} else {
			r.Updated = t
		}
		return nil
	}
	return nil
}

func (r *Request) validate() error {
	if !ValidRequestID(r.ID) {
		return fmt.Errorf("invalid request id %q", r.ID)
	}
	if strings.TrimSpace(r.Title) == "" {
		return fmt.Errorf("title must not be blank")
	}
	if !ValidRequestStatus(r.Status) {
		return fmt.Errorf("invalid request status %q", r.Status)
	}
	if r.Priority < 0 || r.Priority > 4 {
		return fmt.Errorf("priority must be between 0 and 4")
	}
	if r.Created.IsZero() || r.Updated.IsZero() {
		return fmt.Errorf("created and updated must be RFC3339 timestamps")
	}
	if !containsString(r.Aliases, r.ID) {
		return fmt.Errorf("aliases must contain request id %q", r.ID)
	}
	return nil
}

func splitRequestBody(body string) (beforeLog, rawLog, tail string) {
	if body == "" {
		return "", "", ""
	}
	lines := strings.SplitAfter(body, "\n")
	_, logIdx, logEnd, closed := scanSections(lines, true)
	if !closed {
		_, logIdx, logEnd, _ = scanSections(lines, false)
	}
	if logIdx < 0 {
		return body, "", ""
	}
	if logEnd < 0 {
		logEnd = len(lines)
	}
	return strings.Join(lines[:logIdx], ""), strings.Join(lines[logIdx:logEnd], ""), strings.Join(lines[logEnd:], "")
}

// SetRequestBody replaces the complete Markdown before ## Log. It always
// writes a single final newline for non-empty input.
func SetRequestBody(r *Request, text string) {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		r.Body = ""
		return
	}
	r.Body = text + "\n"
}

// AppendRequestLog appends an audit entry and advances Updated when needed.
func AppendRequestLog(r *Request, e LogEntry) {
	r.Log = append(r.Log, e)
	if e.At.After(r.Updated) {
		r.Updated = e.At.UTC()
	}
}

// EncodeRequest renders a request. Parsed requests preserve all untouched
// frontmatter, body, and log bytes.
func EncodeRequest(r *Request) ([]byte, error) {
	if r.ID != "" && !containsString(r.Aliases, r.ID) {
		r.Aliases = append([]string{r.ID}, r.Aliases...)
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	if r.orig == nil {
		return encodeNewRequest(r)
	}
	return encodeSplicedRequest(r)
}

func (r *Request) ownedNode(key string) *yaml.Node {
	switch key {
	case "id":
		return strNode(r.ID)
	case "aliases":
		aliases := r.Aliases
		if !containsString(aliases, r.ID) {
			aliases = append([]string{r.ID}, aliases...)
		}
		return flowSeq(aliases)
	case "title":
		return strNode(r.Title)
	case "status":
		return strNode(r.Status)
	case "priority":
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(r.Priority)}
	case "labels":
		if len(r.Labels) == 0 {
			return nil
		}
		return flowSeq(r.Labels)
	case "requested_by":
		if r.RequestedBy == "" {
			return nil
		}
		return strNode(r.RequestedBy)
	case "issues":
		if len(r.Issues) == 0 {
			return nil
		}
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, l := range r.Issues {
			seq.Content = append(seq.Content, linkNode(l))
		}
		return seq
	case "created":
		return timeNode(r.Created)
	case "updated":
		return timeNode(r.Updated)
	}
	return nil
}

func encodeNewRequest(r *Request) ([]byte, error) {
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, key := range requestKeys {
		if v := r.ownedNode(key); v != nil {
			root.Content = append(root.Content, keyNode(key), v)
		}
	}
	if r.Extra.Kind == yaml.MappingNode {
		root.Content = append(root.Content, r.Extra.Content...)
	}
	fm, err := encodeNode(root)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString(fence + "\n")
	b.WriteString(fm)
	b.WriteString(fence + "\n")
	b.WriteString(r.Body)
	if len(r.Log) > 0 {
		b.WriteString(sectionSeparator(b.String()))
		b.WriteString(logHeading + "\n")
		for _, e := range r.Log {
			b.WriteString(logLine(e))
		}
	}
	return []byte(b.String()), nil
}

func encodeSplicedRequest(r *Request) ([]byte, error) {
	snap, now := r.orig.snap, r.snapshot()
	values := map[string]*yaml.Node{}
	for _, key := range requestKeys {
		if requestChanged(key, snap, now) {
			values[key] = r.ownedNode(key)
		}
	}
	fmLines, err := spliceFrontmatter(r.orig.fmLines, r.orig.spans, requestKeys, values)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	writeFrontmatter(&b, fmLines)
	b.WriteString(r.Body)
	var newEntries []LogEntry
	if len(r.Log) > r.orig.logLen {
		newEntries = r.Log[r.orig.logLen:]
	}
	if r.orig.rawLog != "" {
		b.WriteString(appendToLog(r.orig.rawLog, newEntries))
	} else if len(newEntries) > 0 {
		b.WriteString(sectionSeparator(b.String()))
		b.WriteString(logHeading + "\n")
		for _, e := range newEntries {
			b.WriteString(logLine(e))
		}
	}
	b.WriteString(r.orig.tail)
	return []byte(b.String()), nil
}

func requestChanged(key string, a, b requestSnapshot) bool {
	switch key {
	case "id":
		return a.id != b.id
	case "aliases":
		return !equalStrings(a.aliases, b.aliases)
	case "title":
		return a.title != b.title
	case "status":
		return a.status != b.status
	case "priority":
		return a.priority != b.priority
	case "labels":
		return !equalStrings(a.labels, b.labels)
	case "requested_by":
		return a.requestedBy != b.requestedBy
	case "issues":
		return !equalLinks(a.issues, b.issues)
	case "created":
		return !a.created.Equal(b.created)
	case "updated":
		return !a.updated.Equal(b.updated)
	}
	return false
}
