package issue

import (
	"time"

	"gopkg.in/yaml.v3"
)

// Issue is one issue file: bn-owned frontmatter, the user-owned remainder of
// the frontmatter, and the markdown body split into description, free body,
// and the ## Log section.
type Issue struct {
	ID        string
	Title     string
	Type      string
	Status    string
	Priority  int
	Labels    []string
	Assignee  string
	Parent    Link
	BlockedBy []Link
	URL       string
	Created   time.Time
	Updated   time.Time
	Aliases   []string

	// Extra holds the user-owned frontmatter keys in file order as a
	// yaml.Node of kind MappingNode (Content alternates key, value). bn never
	// interprets them and Encode preserves them byte for byte.
	Extra yaml.Node

	// Description is the text between the frontmatter and the first `## `
	// heading (or end of file), verbatim.
	Description string
	// Body is everything between the description and the `## Log` heading
	// (or end of file), verbatim. Headings in it are user-owned.
	Body string
	// Log holds the parsed entries of the `## Log` section.
	Log []LogEntry

	// Path is the file path this issue was parsed from (as passed to Parse).
	Path string
	// Project is the project name derived from Path when it lies under
	// projects/<name>/; otherwise empty.
	Project string
	// Archived reports whether Path is under an archive/ directory.
	Archived bool

	orig *original
}

// Link is a frontmatter link as written. Raw is the exact text from the file
// (for example "[[exampleA-a3f2-title|alias]]" or a bare id); Target is the
// text inside the brackets before any "|" or "#", or Raw itself for a bare id.
type Link struct {
	Raw    string
	Target string
}

// LogEntry is one list item of the ## Log section.
//
//   - 2026-09-10T08:01:00Z matt (exampleA@a1b2c3d feature/x): status open → in_progress
type LogEntry struct {
	At     time.Time
	Actor  string
	Repo   string
	SHA    string
	Branch string
	Event  string

	// Raw is set for list items that do not parse as a bn log line. They are
	// preserved verbatim by Encode. When Raw is non-empty the other fields are
	// zero.
	Raw string
}
