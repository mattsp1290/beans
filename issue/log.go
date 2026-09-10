package issue

import (
	"regexp"
	"strings"
	"time"
)

// logLineRe matches the first line of a log entry:
//
//   - <RFC3339> <actor>[ (<repo>@<sha> <branch>)]: <event>
//
// The actor and the repo, sha, and branch fields never contain whitespace
// (FormatLogEntry replaces runs of whitespace in them with "-"), so the
// closing ")" before ": " is unambiguous even when a branch name contains
// ")" itself. Git branch names cannot contain spaces.
var logLineRe = regexp.MustCompile(`^- (\S+) (\S+?)(?: \(([^@\s)]+)@([0-9a-f]+)(?: (\S+?))?\))?: (.*)$`)

// FormatLogEntry renders one entry as a list item (without a trailing
// newline). Continuation lines of a multi-line event are indented by two
// spaces.
func FormatLogEntry(e LogEntry) string {
	var b strings.Builder
	b.WriteString("- ")
	b.WriteString(e.At.UTC().Format(time.RFC3339))
	b.WriteString(" ")
	b.WriteString(token(e.Actor))
	if e.Repo != "" && e.SHA != "" {
		b.WriteString(" (")
		b.WriteString(token(e.Repo))
		b.WriteString("@")
		b.WriteString(e.SHA)
		if e.Branch != "" {
			b.WriteString(" ")
			b.WriteString(token(e.Branch))
		}
		b.WriteString(")")
	}
	b.WriteString(": ")
	b.WriteString(strings.ReplaceAll(e.Event, "\n", "\n  "))
	return b.String()
}

// token makes s safe as a whitespace-delimited log field: runs of
// whitespace become "-" and an empty value becomes "-".
func token(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return "-"
	}
	return strings.Join(fields, "-")
}

// ParseLogEntry parses a list item (first line plus indented continuation
// lines joined by "\n"). ok is false when the text is not a bn log line.
func ParseLogEntry(text string) (LogEntry, bool) {
	first, rest, _ := strings.Cut(text, "\n")
	m := logLineRe.FindStringSubmatch(first)
	if m == nil {
		return LogEntry{}, false
	}
	at, err := time.Parse(time.RFC3339, m[1])
	if err != nil {
		// Accept the minute-precision form 2026-09-10T08:01Z as well.
		at, err = time.Parse("2006-01-02T15:04Z07:00", m[1])
		if err != nil {
			return LogEntry{}, false
		}
	}
	e := LogEntry{At: at, Actor: m[2], Repo: m[3], SHA: m[4], Branch: m[5], Event: m[6]}
	if rest != "" {
		var cont []string
		for _, l := range strings.Split(rest, "\n") {
			cont = append(cont, strings.TrimPrefix(l, "  "))
		}
		e.Event += "\n" + strings.Join(cont, "\n")
	}
	return e, true
}

// AppendLog adds an entry to the issue's log and bumps Updated to the entry
// time when it is later.
func AppendLog(iss *Issue, e LogEntry) {
	iss.Log = append(iss.Log, e)
	if e.At.After(iss.Updated) {
		iss.Updated = e.At.UTC()
	}
}

// SetDescription replaces the description span with text, normalizing the
// trailing newlines so the following section keeps one blank line before it.
func SetDescription(iss *Issue, text string) {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		iss.Description = ""
		if iss.Body != "" || len(iss.Log) > 0 || (iss.orig != nil && iss.orig.rawLog != "") {
			iss.Description = "\n"
		}
		return
	}
	iss.Description = text + "\n"
	if iss.Body != "" || len(iss.Log) > 0 || (iss.orig != nil && iss.orig.rawLog != "") {
		iss.Description += "\n"
	}
}
