package issue

import (
	"strings"
	"testing"
	"time"
)

const fencedHeading = `---
id: p-f1
title: fenced
type: task
status: open
priority: 2
created: 2026-01-01T00:00:00Z
updated: 2026-01-01T00:00:00Z
---
Description documenting the format:

` + "```" + `
## Log
- fake entry inside code fence
` + "```" + `

More description text after the fence.

## Acceptance
- [ ] real section
`

func TestFencedHeadingsAreNotSections(t *testing.T) {
	t.Parallel()
	iss, err := Parse("f.md", []byte(fencedHeading))
	if err != nil {
		t.Fatal(err)
	}
	if len(iss.Log) != 0 {
		t.Fatalf("fenced ## Log must not parse as a log section: %+v", iss.Log)
	}
	if !strings.HasSuffix(iss.Description, "More description text after the fence.\n\n") || !strings.HasPrefix(iss.Body, "## Acceptance") {
		t.Fatalf("description/body split wrong:\ndesc=%q\nbody=%q", iss.Description, iss.Body)
	}
	AppendLog(iss, LogEntry{At: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Actor: "m", Event: "created"})
	out, _ := Encode(iss)
	if !strings.HasSuffix(string(out), "- [ ] real section\n\n## Log\n- 2026-01-02T00:00:00Z m: created\n") {
		t.Fatalf("log must be appended at the end:\n%s", out)
	}
	if !strings.Contains(string(out), "```\n## Log\n- fake entry inside code fence\n```\n\nMore description") {
		t.Fatalf("fenced content altered:\n%s", out)
	}
}

func TestLogEntryTokensRoundTrip(t *testing.T) {
	t.Parallel()
	cases := []LogEntry{
		{Actor: "Matt Spurlin", Event: "created"},
		{Actor: "matt", Repo: "my repo", SHA: "abc1234", Branch: "feature/fix(bug)", Event: "status open → in_progress"},
		{Actor: "matt", Repo: "r", SHA: "abc1234", Branch: "weird)): name", Event: "note — x): y"},
		{Actor: "", Event: "reopened"},
	}
	for _, in := range cases {
		in.At = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		line := FormatLogEntry(in)
		got, ok := ParseLogEntry(line)
		if !ok {
			t.Errorf("%q does not parse back", line)
			continue
		}
		if got.Event != in.Event || got.SHA != in.SHA || got.Actor != token(in.Actor) || (in.Repo != "" && got.Repo != token(in.Repo)) || (in.Branch != "" && got.Branch != token(in.Branch)) {
			t.Errorf("round trip of %+v gave %+v (line %q)", in, got, line)
		}
	}
}

func TestEncodeProjectConfigEscapesControlCharacters(t *testing.T) {
	t.Parallel()
	data, err := EncodeProjectConfig(ProjectConfig{Name: "bell\x07char", Prefix: "p\vq", Remotes: []string{"https://x/y \"z\""}})
	if err != nil {
		t.Fatal(err)
	}
	var back ProjectConfig
	if err := decodeTOMLBytes(data, &back); err != nil {
		t.Fatalf("output does not decode: %v\n%s", err, data)
	}
	if back.Name != "bell\x07char" || back.Prefix != "p\vq" || back.Remotes[0] != "https://x/y \"z\"" {
		t.Errorf("round trip lost data: %+v", back)
	}
	if strings.Contains(string(data), "[workflow]") {
		t.Errorf("empty workflow must be omitted:\n%s", data)
	}
	withWF, _ := EncodeProjectConfig(ProjectConfig{Name: "a", Prefix: "a", Workflow: WorkflowFile{Default: "in_progress"}})
	if !strings.Contains(string(withWF), "[workflow]") || !strings.Contains(string(withWF), "default = \"in_progress\"") {
		t.Errorf("workflow override missing:\n%s", withWF)
	}
}

func TestParseMemoryDuplicateKey(t *testing.T) {
	t.Parallel()
	_, err := ParseMemory("m.md", []byte("---\nkey: a\nkey: b\n---\n"))
	if err == nil || !strings.Contains(err.Error(), "duplicate frontmatter key") {
		t.Fatalf("err = %v", err)
	}
}

func TestUnclosedFenceDoesNotHideHeadings(t *testing.T) {
	t.Parallel()
	in := "---\nid: p-u1\ntitle: t\ntype: task\nstatus: open\npriority: 2\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\nDescription with an unclosed fence:\n\n```\nsome code\n\n## Log\n- 2026-01-01T00:00:00Z matt: created\n"
	iss, err := Parse("u.md", []byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(iss.Log) != 1 || iss.Log[0].Event != "created" {
		t.Fatalf("the real log must still be found: %+v", iss.Log)
	}
	AppendLog(iss, LogEntry{At: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Actor: "m", Event: "reopened"})
	out, _ := Encode(iss)
	if strings.Count(string(out), "## Log") != 1 || !strings.HasSuffix(string(out), "matt: created\n- 2026-01-02T00:00:00Z m: reopened\n") {
		t.Fatalf("log appended to the existing section:\n%s", out)
	}
	if o, _ := Encode(mustParse(t, in)); string(o) != in {
		t.Fatal("unchanged file must round-trip")
	}
}

func mustParse(t *testing.T, in string) *Issue {
	t.Helper()
	iss, err := Parse("x.md", []byte(in))
	if err != nil {
		t.Fatal(err)
	}
	return iss
}
