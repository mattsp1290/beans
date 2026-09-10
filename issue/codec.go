package issue

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ownedKeys lists the frontmatter keys bn owns, in the order Encode writes
// them for a new file.
var ownedKeys = []string{
	"id", "aliases", "title", "type", "status", "priority", "labels",
	"assignee", "parent", "blocked_by", "url", "created", "updated",
}

var ownedKeySet = func() map[string]bool {
	m := make(map[string]bool, len(ownedKeys))
	for _, k := range ownedKeys {
		m[k] = true
	}
	return m
}()

// requiredKeys must be present in every issue file.
var requiredKeys = []string{"id", "title", "type", "status", "priority", "created", "updated"}

const (
	fence      = "---"
	logHeading = "## Log"
)

var (
	h2Re         = regexp.MustCompile(`^## `)
	logHeadingRe = regexp.MustCompile(`^## Log[ \t]*$`)
)

// original captures the exact text Parse saw so Encode can splice minimal
// edits into it and leave every untouched line byte-identical.
type original struct {
	fmLines []string  // frontmatter lines between the fences, no trailing \n
	spans   []keySpan // top-level pairs in file order
	rawLog  string    // "## Log" heading line through the end of the section ("" when absent)
	tail    string    // text after the log section when ## Log is not the last H2
	logLen  int       // number of entries parsed from rawLog
	snap    snapshot
}

type keySpan struct {
	key        string
	start, end int // [start, end) indexes into fmLines
	keyNode    *yaml.Node
	valueNode  *yaml.Node
}

// snapshot holds the bn-owned values as parsed, for change detection.
type snapshot struct {
	id, title, typ, status, assignee, url string
	priority                              int
	labels, aliases                       []string
	parent                                Link
	blockedBy                             []Link
	created, updated                      time.Time
}

// Parse decodes one issue file. Errors name the path and, for frontmatter
// problems, the line. Files with Windows line endings are rejected.
func Parse(path string, data []byte) (*Issue, error) {
	if bytes.Contains(data, []byte("\r\n")) {
		return nil, fmt.Errorf("%s: has Windows line endings (\\r\\n); bn requires \\n", path)
	}
	fmText, bodyText, fmStartLine, err := splitFrontmatter(path, data)
	if err != nil {
		return nil, err
	}

	root, err := parseMapping(path, fmText, fmStartLine)
	if err != nil {
		return nil, err
	}

	fmLines := strings.Split(strings.TrimSuffix(fmText, "\n"), "\n")
	if fmText == "" {
		fmLines = nil
	}
	orig := &original{fmLines: fmLines}
	iss := &Issue{Path: path, orig: orig}
	iss.Extra = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	seen := map[string]bool{}

	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i], root.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("%s: line %d: frontmatter keys must be strings", path, fmStartLine+k.Line-1)
		}
		orig.spans = append(orig.spans, keySpan{key: k.Value, start: k.Line - 1, keyNode: k, valueNode: v})
		if !ownedKeySet[k.Value] {
			iss.Extra.Content = append(iss.Extra.Content, k, v)
			continue
		}
		if seen[k.Value] {
			return nil, fmt.Errorf("%s: line %d: duplicate frontmatter key %q", path, fmStartLine+k.Line-1, k.Value)
		}
		seen[k.Value] = true
		if err := iss.readOwned(k.Value, v); err != nil {
			return nil, fmt.Errorf("%s: line %d: %w", path, fmStartLine+v.Line-1, err)
		}
	}
	for _, k := range requiredKeys {
		if !seen[k] {
			return nil, fmt.Errorf("%s: frontmatter is missing required key %q", path, k)
		}
	}
	closeSpans(orig.spans, fmLines)

	iss.Description, iss.Body, orig.rawLog, orig.tail = splitBody(bodyText)
	iss.Log = parseLogSection(orig.rawLog)
	orig.logLen = len(iss.Log)
	iss.Project, iss.Archived = pathInfo(path)
	orig.snap = iss.snapshot()
	return iss, nil
}

// splitFrontmatter returns the text between the fences (with a trailing
// newline unless empty), the body after the closing fence, and the 1-based
// file line of the first frontmatter line.
// parseMapping decodes frontmatter text into its top-level mapping node.
func parseMapping(path, fmText string, fmStartLine int) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(fmText), &doc); err != nil {
		return nil, fmt.Errorf("%s: frontmatter: %w", path, err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, fmt.Errorf("%s: line %d: frontmatter is empty", path, fmStartLine)
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: line %d: frontmatter must be a mapping", path, fmStartLine+root.Line-1)
	}
	return root, nil
}

// closeSpans sets each span's end at the next key's line, giving trailing
// blank and comment lines to the following key so they survive a replacement.
func closeSpans(spans []keySpan, fmLines []string) {
	for i := range spans {
		end := len(fmLines)
		if i+1 < len(spans) {
			end = spans[i+1].start
		}
		for end-1 > spans[i].start && isBlankOrComment(fmLines[end-1]) {
			end--
		}
		spans[i].end = end
	}
}

func splitFrontmatter(path string, data []byte) (fm, body string, fmStart int, err error) {
	s := string(data)
	if !strings.HasPrefix(s, fence+"\n") {
		return "", "", 0, fmt.Errorf("%s: line 1: file must start with a --- frontmatter fence", path)
	}
	rest := s[len(fence)+1:]
	// The closing fence is a line that is exactly "---".
	for off := 0; off <= len(rest); {
		nl := strings.IndexByte(rest[off:], '\n')
		var line string
		if nl < 0 {
			line = rest[off:]
		} else {
			line = rest[off : off+nl]
		}
		if line == fence {
			fm = rest[:off]
			if nl < 0 {
				return fm, "", 2, nil
			}
			return fm, rest[off+nl+1:], 2, nil
		}
		if nl < 0 {
			break
		}
		off += nl + 1
	}
	return "", "", 0, fmt.Errorf("%s: frontmatter has no closing --- fence", path)
}

func isBlankOrComment(line string) bool {
	t := strings.TrimSpace(line)
	return t == "" || strings.HasPrefix(t, "#")
}

func (iss *Issue) readOwned(key string, v *yaml.Node) error {
	switch key {
	case "id":
		return scalarInto(key, v, &iss.ID)
	case "title":
		return scalarInto(key, v, &iss.Title)
	case "type":
		return scalarInto(key, v, &iss.Type)
	case "status":
		return scalarInto(key, v, &iss.Status)
	case "assignee":
		return scalarInto(key, v, &iss.Assignee)
	case "url":
		return scalarInto(key, v, &iss.URL)
	case "priority":
		var s string
		if err := scalarInto(key, v, &s); err != nil {
			return err
		}
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return fmt.Errorf("priority must be an integer, got %q", s)
		}
		iss.Priority = n
		return nil
	case "labels":
		ss, err := stringList(key, v)
		iss.Labels = ss
		return err
	case "aliases":
		ss, err := stringList(key, v)
		iss.Aliases = ss
		return err
	case "parent":
		var s string
		if err := scalarInto(key, v, &s); err != nil {
			return err
		}
		iss.Parent = ParseLink(s)
		return nil
	case "blocked_by":
		ss, err := stringList(key, v)
		if err != nil {
			return err
		}
		iss.BlockedBy = nil
		for _, s := range ss {
			if strings.TrimSpace(s) == "" {
				continue
			}
			iss.BlockedBy = append(iss.BlockedBy, ParseLink(s))
		}
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
			iss.Created = t
		} else {
			iss.Updated = t
		}
		return nil
	}
	return nil
}

func scalarInto(key string, v *yaml.Node, dst *string) error {
	if v.Kind != yaml.ScalarNode {
		return fmt.Errorf("%s must be a string", key)
	}
	if v.Tag == "!!null" {
		*dst = ""
		return nil
	}
	*dst = v.Value
	return nil
}

// stringList accepts a sequence of scalars or a single scalar.
func stringList(key string, v *yaml.Node) ([]string, error) {
	switch v.Kind {
	case yaml.ScalarNode:
		if v.Tag == "!!null" || strings.TrimSpace(v.Value) == "" {
			return nil, nil
		}
		return []string{v.Value}, nil
	case yaml.SequenceNode:
		out := make([]string, 0, len(v.Content))
		for _, item := range v.Content {
			if item.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("%s must be a list of strings", key)
			}
			out = append(out, item.Value)
		}
		return out, nil
	}
	return nil, fmt.Errorf("%s must be a list of strings", key)
}

// ParseLink reads a frontmatter link. It accepts [[target]], [[target|alias]],
// [[target#heading]], and a bare id.
func ParseLink(s string) Link {
	raw := strings.TrimSpace(s)
	target := raw
	if strings.HasPrefix(raw, "[[") && strings.HasSuffix(raw, "]]") && len(raw) >= 4 {
		target = raw[2 : len(raw)-2]
		if i := strings.IndexByte(target, '|'); i >= 0 {
			target = target[:i]
		}
		if i := strings.IndexByte(target, '#'); i >= 0 {
			target = target[:i]
		}
		target = strings.TrimSpace(target)
	}
	return Link{Raw: raw, Target: target}
}

// NewLink builds the link bn writes for a target basename or id.
func NewLink(target string) Link {
	return Link{Raw: "[[" + target + "]]", Target: target}
}

// IsZero reports whether the link is empty.
func (l Link) IsZero() bool { return l.Raw == "" && l.Target == "" }

// splitBody divides the markdown after the frontmatter into the description
// (up to the first H2), the body (from the first H2 to the ## Log heading),
// the raw ## Log section, and any tail after the log section.
func splitBody(body string) (desc, mid, rawLog, tail string) {
	if body == "" {
		return "", "", "", ""
	}
	lines := strings.SplitAfter(body, "\n")
	firstH2, logIdx, logEnd, closed := scanSections(lines, true)
	if !closed {
		// An unterminated fence would hide every later heading, including a
		// real ## Log, and each mutation would then append another one. Treat
		// the fences as inert instead.
		firstH2, logIdx, logEnd, _ = scanSections(lines, false)
	}
	if firstH2 < 0 {
		return body, "", "", ""
	}
	desc = strings.Join(lines[:firstH2], "")
	if logIdx < 0 {
		return desc, strings.Join(lines[firstH2:], ""), "", ""
	}
	mid = strings.Join(lines[firstH2:logIdx], "")
	if logEnd < 0 {
		logEnd = len(lines)
	}
	rawLog = strings.Join(lines[logIdx:logEnd], "")
	tail = strings.Join(lines[logEnd:], "")
	return desc, mid, rawLog, tail
}

// scanSections finds the first H2, the ## Log heading, and the heading that
// ends the log section. With honorFences, heading-like lines inside a fenced
// code block are ignored; closed reports whether every fence was closed.
func scanSections(lines []string, honorFences bool) (firstH2, logIdx, logEnd int, closed bool) {
	firstH2, logIdx, logEnd = -1, -1, -1
	inFence := false
	for i, l := range lines {
		t := strings.TrimSuffix(l, "\n")
		if honorFences && isFenceLine(t) {
			inFence = !inFence
			continue
		}
		if inFence || !h2Re.MatchString(t) {
			continue
		}
		if firstH2 < 0 {
			firstH2 = i
		}
		if logIdx < 0 && logHeadingRe.MatchString(t) {
			logIdx = i
		} else if logIdx >= 0 && logEnd < 0 {
			logEnd = i
		}
	}
	return firstH2, logIdx, logEnd, !inFence
}

// isFenceLine reports whether a line opens or closes a fenced code block
// (``` or ~~~ with up to three spaces of indentation). Headings inside a
// fence are not headings.
func isFenceLine(line string) bool {
	t := strings.TrimLeft(line, " ")
	if len(line)-len(t) > 3 {
		return false
	}
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

// parseLogSection reads the list items of a raw ## Log section. Items that
// are not bn log lines are kept as LogEntry{Raw}.
func parseLogSection(rawLog string) []LogEntry {
	if rawLog == "" {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(rawLog, "\n"), "\n")
	var entries []LogEntry
	var cur []string
	flush := func() {
		if cur == nil {
			return
		}
		text := strings.Join(cur, "\n")
		if e, ok := ParseLogEntry(text); ok {
			entries = append(entries, e)
		} else {
			entries = append(entries, LogEntry{Raw: text})
		}
		cur = nil
	}
	for i, l := range lines {
		if i == 0 {
			continue // heading
		}
		switch {
		case strings.HasPrefix(l, "- "):
			flush()
			cur = []string{l}
		case cur != nil && strings.HasPrefix(l, "  "):
			cur = append(cur, l)
		default:
			flush()
		}
	}
	flush()
	return entries
}

func pathInfo(path string) (project string, archived bool) {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i, p := range parts {
		if p == "projects" && i+1 < len(parts) {
			project = parts[i+1]
		}
		if p == "archive" {
			archived = true
		}
	}
	return project, archived
}

func (iss *Issue) snapshot() snapshot {
	return snapshot{
		id: iss.ID, title: iss.Title, typ: iss.Type, status: iss.Status,
		assignee: iss.Assignee, url: iss.URL, priority: iss.Priority,
		labels: append([]string(nil), iss.Labels...), aliases: append([]string(nil), iss.Aliases...),
		parent: iss.Parent, blockedBy: append([]Link(nil), iss.BlockedBy...),
		created: iss.Created, updated: iss.Updated,
	}
}

// ---------------------------------------------------------------------------
// Encode
// ---------------------------------------------------------------------------

// Encode renders the issue. For an issue that came from Parse, only the
// frontmatter keys whose values changed are re-emitted (spliced into the
// original lines), the description and body are used as they stand, and log
// entries added since Parse are appended to the ## Log section; everything
// else is byte-identical to the input. A new issue is written in the
// documented key order.
func Encode(iss *Issue) ([]byte, error) {
	if iss.orig == nil {
		return encodeNew(iss)
	}
	return encodeSpliced(iss)
}

func encodeNew(iss *Issue) ([]byte, error) {
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, key := range ownedKeys {
		v := iss.ownedNode(key)
		if v == nil {
			continue
		}
		root.Content = append(root.Content, keyNode(key), v)
	}
	if iss.Extra.Kind == yaml.MappingNode {
		root.Content = append(root.Content, iss.Extra.Content...)
	}
	fm, err := encodeNode(root)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString(fence + "\n")
	b.WriteString(fm)
	b.WriteString(fence + "\n")
	b.WriteString(iss.Description)
	b.WriteString(iss.Body)
	if len(iss.Log) > 0 {
		b.WriteString(sectionSeparator(b.String()))
		b.WriteString(logHeading + "\n")
		for _, e := range iss.Log {
			b.WriteString(logLine(e))
		}
	}
	return []byte(b.String()), nil
}

// sectionSeparator returns what must be appended to text so a new H2 starts
// on its own line with one blank line before it.
func sectionSeparator(text string) string {
	if text == "" || strings.HasSuffix(text, fence+"\n") {
		return ""
	}
	if !strings.HasSuffix(text, "\n") {
		return "\n\n"
	}
	if !strings.HasSuffix(text, "\n\n") {
		return "\n"
	}
	return ""
}

func logLine(e LogEntry) string {
	if e.Raw != "" {
		return e.Raw + "\n"
	}
	return FormatLogEntry(e) + "\n"
}

type fmEdit struct {
	start, end int
	lines      []string
}

// spliceFrontmatter applies per-key replacements to the original frontmatter
// lines. values maps a key to its new node (nil removes the key); keys not in
// the map are untouched. New keys are inserted, in the order of ownedOrder,
// after the last span whose key is in ownedOrder.
func spliceFrontmatter(fmLines []string, spans []keySpan, ownedOrder []string, values map[string]*yaml.Node) ([]string, error) {
	owned := make(map[string]bool, len(ownedOrder))
	for _, k := range ownedOrder {
		owned[k] = true
	}
	spanByKey := map[string]keySpan{}
	lastOwnedEnd := -1
	for _, sp := range spans {
		if owned[sp.key] {
			spanByKey[sp.key] = sp
			if sp.end > lastOwnedEnd {
				lastOwnedEnd = sp.end
			}
		}
	}
	if lastOwnedEnd < 0 {
		lastOwnedEnd = len(fmLines)
	}

	var edits []fmEdit
	var added []string
	for _, key := range ownedOrder {
		v, ok := values[key]
		if !ok {
			continue
		}
		sp, had := spanByKey[key]
		switch {
		case v == nil && had:
			edits = append(edits, fmEdit{start: sp.start, end: sp.end})
		case v == nil:
			// nothing to write and nothing to remove
		case had:
			if sp.valueNode.Kind == yaml.ScalarNode && v.Kind == yaml.ScalarNode {
				v.LineComment = sp.valueNode.LineComment
			}
			lines, err := encodePair(key, v)
			if err != nil {
				return nil, err
			}
			edits = append(edits, fmEdit{start: sp.start, end: sp.end, lines: lines})
		default:
			lines, err := encodePair(key, v)
			if err != nil {
				return nil, err
			}
			added = append(added, lines...)
		}
	}
	if len(added) > 0 {
		edits = append(edits, fmEdit{start: lastOwnedEnd, end: lastOwnedEnd, lines: added})
	}
	// Apply from the bottom up so earlier indexes stay valid. Insertions at
	// the same position as a replacement's end must land after it.
	out := append([]string(nil), fmLines...)
	sortEdits(edits)
	for _, e := range edits {
		out = append(out[:e.start], append(e.lines, out[e.end:]...)...)
	}
	return out, nil
}

func encodeSpliced(iss *Issue) ([]byte, error) {
	orig := iss.orig
	snap := orig.snap
	now := iss.snapshot()

	values := map[string]*yaml.Node{}
	for _, key := range ownedKeys {
		if changed(key, snap, now) {
			values[key] = iss.ownedNode(key)
		}
	}
	fmLines, err := spliceFrontmatter(orig.fmLines, orig.spans, ownedKeys, values)
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	writeFrontmatter(&b, fmLines)
	b.WriteString(iss.Description)
	b.WriteString(iss.Body)

	newEntries := iss.Log
	if len(newEntries) > orig.logLen {
		newEntries = newEntries[orig.logLen:]
	} else {
		newEntries = nil
	}
	switch {
	case orig.rawLog != "":
		b.WriteString(appendToLog(orig.rawLog, newEntries))
	case len(newEntries) > 0:
		b.WriteString(sectionSeparator(b.String()))
		b.WriteString(logHeading + "\n")
		for _, e := range newEntries {
			b.WriteString(logLine(e))
		}
	}
	b.WriteString(orig.tail)
	return []byte(b.String()), nil
}

func writeFrontmatter(b *strings.Builder, fmLines []string) {
	b.WriteString(fence + "\n")
	for _, l := range fmLines {
		b.WriteString(l)
		b.WriteString("\n")
	}
	b.WriteString(fence + "\n")
}

// appendToLog inserts entries after the last non-blank line of the section so
// trailing blank lines stay where they were.
func appendToLog(rawLog string, entries []LogEntry) string {
	if len(entries) == 0 {
		return rawLog
	}
	lines := strings.SplitAfter(rawLog, "\n")
	cut := len(lines)
	for cut > 1 && strings.TrimSpace(lines[cut-1]) == "" {
		cut--
	}
	var b strings.Builder
	for _, l := range lines[:cut] {
		b.WriteString(l)
	}
	if !strings.HasSuffix(b.String(), "\n") {
		b.WriteString("\n")
	}
	for _, e := range entries {
		b.WriteString(logLine(e))
	}
	for _, l := range lines[cut:] {
		b.WriteString(l)
	}
	return b.String()
}

func sortEdits(edits []fmEdit) {
	// insertion sort, descending by start; for equal starts a pure insertion
	// (start == end) is applied first so it ends up after the replacement.
	for i := 1; i < len(edits); i++ {
		for j := i; j > 0 && editBefore(edits[j], edits[j-1]); j-- {
			edits[j], edits[j-1] = edits[j-1], edits[j]
		}
	}
}

func editBefore(a, b fmEdit) bool {
	if a.start != b.start {
		return a.start > b.start
	}
	return a.start == a.end && b.start != b.end
}

func changed(key string, a, b snapshot) bool {
	switch key {
	case "id":
		return a.id != b.id
	case "title":
		return a.title != b.title
	case "type":
		return a.typ != b.typ
	case "status":
		return a.status != b.status
	case "assignee":
		return a.assignee != b.assignee
	case "url":
		return a.url != b.url
	case "priority":
		return a.priority != b.priority
	case "labels":
		return !equalStrings(a.labels, b.labels)
	case "aliases":
		return !equalStrings(a.aliases, b.aliases)
	case "parent":
		return a.parent != b.parent
	case "blocked_by":
		return !equalLinks(a.blockedBy, b.blockedBy)
	case "created":
		return !a.created.Equal(b.created)
	case "updated":
		return !a.updated.Equal(b.updated)
	}
	return false
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalLinks(a, b []Link) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ownedNode builds the YAML value for a bn-owned key, or nil when the key is
// empty and therefore omitted. aliases always contains at least the id.
func (iss *Issue) ownedNode(key string) *yaml.Node {
	switch key {
	case "id":
		return strNode(iss.ID)
	case "aliases":
		aliases := iss.Aliases
		if !containsString(aliases, iss.ID) && iss.ID != "" {
			aliases = append([]string{iss.ID}, aliases...)
		}
		return flowSeq(aliases)
	case "title":
		return strNode(iss.Title)
	case "type":
		return strNode(iss.Type)
	case "status":
		return strNode(iss.Status)
	case "priority":
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(iss.Priority)}
	case "labels":
		if len(iss.Labels) == 0 {
			return nil
		}
		return flowSeq(iss.Labels)
	case "assignee":
		if iss.Assignee == "" {
			return nil
		}
		return strNode(iss.Assignee)
	case "parent":
		if iss.Parent.IsZero() {
			return nil
		}
		return linkNode(iss.Parent)
	case "blocked_by":
		if len(iss.BlockedBy) == 0 {
			return nil
		}
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, l := range iss.BlockedBy {
			seq.Content = append(seq.Content, linkNode(l))
		}
		return seq
	case "url":
		if iss.URL == "" {
			return nil
		}
		return strNode(iss.URL)
	case "created":
		return timeNode(iss.Created)
	case "updated":
		return timeNode(iss.Updated)
	}
	return nil
}

func keyNode(key string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
}

func strNode(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

func linkNode(l Link) *yaml.Node {
	raw := l.Raw
	if raw == "" {
		raw = "[[" + l.Target + "]]"
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Style: yaml.DoubleQuotedStyle, Value: raw}
}

func timeNode(t time.Time) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!timestamp", Value: t.UTC().Format(time.RFC3339)}
}

func flowSeq(items []string) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
	for _, s := range items {
		seq.Content = append(seq.Content, strNode(s))
	}
	return seq
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// encodePair renders one key/value pair as frontmatter lines.
func encodePair(key string, v *yaml.Node) ([]string, error) {
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{keyNode(key), v}}
	text, err := encodeNode(m)
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n"), nil
}

func encodeNode(n *yaml.Node) (string, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(n); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}
