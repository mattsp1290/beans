package issue

import (
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// TestParseAdditionalErrors exercises readOwned/stringList/scalarInto error
// paths not covered by TestParseErrors in codec_test.go.
func TestParseAdditionalErrors(t *testing.T) {
	base := func(extra string) string {
		return "---\nid: x\ntitle: t\ntype: task\nstatus: open\n" + extra +
			"created: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\n"
	}
	cases := map[string]string{
		"priority not scalar":        base("priority:\n  - 1\n  - 2\n"),
		"parent not scalar":          base("priority: 1\nparent:\n  - a\n  - b\n"),
		"blocked_by item not scalar": base("priority: 1\nblocked_by:\n  - \"[[a]]\"\n  - [nested]\n"),
		"created not scalar":         "---\nid: x\ntitle: t\ntype: task\nstatus: open\npriority: 1\ncreated:\n  - a\nupdated: 2026-01-01T00:00:00Z\n---\n",
		"labels item not scalar":     base("priority: 1\nlabels:\n  - a\n  - [nested]\n"),
		"labels not list or scalar":  base("priority: 1\nlabels:\n  a: b\n"),
		"non-scalar top-level key":   "---\n? [a, b]\n: v\nid: x\ntitle: t\ntype: task\nstatus: open\npriority: 1\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\n",
		"invalid yaml syntax":        "---\nid: [unterminated\n---\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(name+".md", []byte(in)); err == nil {
				t.Fatalf("expected an error for input:\n%s", in)
			}
		})
	}
}

// TestParseNoTrailingNewlineAfterClosingFence covers the splitFrontmatter
// branch where the closing "---" is the final bytes of the file (no
// trailing newline), which also drives splitBody's empty-body branch.
func TestParseNoTrailingNewlineAfterClosingFence(t *testing.T) {
	data := "---\nid: x\ntitle: t\ntype: task\nstatus: open\npriority: 1\n" +
		"created: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---"
	iss, err := Parse("no-trailing.md", []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if iss.Description != "" || iss.Body != "" {
		t.Errorf("expected empty description/body, got desc=%q body=%q", iss.Description, iss.Body)
	}
}

// TestParseEmptyBodyAfterFence covers splitBody's body=="" branch via a file
// that ends immediately after the closing fence's newline.
func TestParseEmptyBodyAfterFence(t *testing.T) {
	data := "---\nid: x\ntitle: t\ntype: task\nstatus: open\npriority: 1\n" +
		"created: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\n"
	iss, err := Parse("empty-body.md", []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if iss.Description != "" || iss.Body != "" || len(iss.Log) != 0 {
		t.Errorf("expected empty body/description/log, got %+v", iss)
	}
}

// TestScalarIntoNullValue covers scalarInto's explicit-null branch.
func TestScalarIntoNullValue(t *testing.T) {
	data := "---\nid: x\ntitle: t\ntype: task\nstatus: open\npriority: 1\n" +
		"assignee: null\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\n"
	iss, err := Parse("null-assignee.md", []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if iss.Assignee != "" {
		t.Errorf("Assignee = %q, want empty", iss.Assignee)
	}
}

// TestStringListNullAndEmpty covers stringList's scalar null/empty branch.
func TestStringListNullAndEmpty(t *testing.T) {
	data := "---\nid: x\ntitle: t\ntype: task\nstatus: open\npriority: 1\nlabels: null\n" +
		"created: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\n"
	iss, err := Parse("null-labels.md", []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(iss.Labels) != 0 {
		t.Errorf("Labels = %v, want empty", iss.Labels)
	}
}

// TestBlockedBySkipsEmptyElements covers the readOwned blocked_by loop's
// "skip blank entries" continue branch.
func TestBlockedBySkipsEmptyElements(t *testing.T) {
	data := `---
id: x
title: t
type: task
status: open
priority: 1
blocked_by: ["[[a]]", ""]
created: 2026-01-01T00:00:00Z
updated: 2026-01-01T00:00:00Z
---
`
	iss, err := Parse("skip-blank.md", []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(iss.BlockedBy) != 1 || iss.BlockedBy[0].Target != "a" {
		t.Errorf("BlockedBy = %+v, want a single link to %q", iss.BlockedBy, "a")
	}
}

// TestParseLinkPipeAndHeading covers ParseLink's alias (|) and heading (#)
// stripping branches.
func TestParseLinkPipeAndHeading(t *testing.T) {
	data := `---
id: x
title: t
type: task
status: open
priority: 1
parent: "[[proj-x|Some Alias]]"
blocked_by:
  - "[[proj-y#section]]"
created: 2026-01-01T00:00:00Z
updated: 2026-01-01T00:00:00Z
---
`
	iss, err := Parse("pipe-heading.md", []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if iss.Parent.Target != "proj-x" {
		t.Errorf("Parent.Target = %q, want %q", iss.Parent.Target, "proj-x")
	}
	if len(iss.BlockedBy) != 1 || iss.BlockedBy[0].Target != "proj-y" {
		t.Errorf("BlockedBy = %+v, want target %q", iss.BlockedBy, "proj-y")
	}
}

// TestEncodeNewWithExtraAndRawLog covers encodeNew's Extra-append branch and
// logLine's Raw-entry branch.
func TestEncodeNewWithExtraAndRawLog(t *testing.T) {
	iss := &Issue{
		ID: "p-ab12", Title: "T", Type: "task", Status: "open", Priority: 1,
		Created:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Updated:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Description: "d\n",
		Extra: yaml.Node{
			Kind: yaml.MappingNode,
			Tag:  "!!map",
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Tag: "!!str", Value: "custom"},
				{Kind: yaml.ScalarNode, Tag: "!!str", Value: "value"},
			},
		},
	}
	iss.Log = []LogEntry{{Raw: "- a hand-written log line"}}
	out, err := Encode(iss)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "custom: value") {
		t.Errorf("expected extra key in output:\n%s", out)
	}
	if !strings.Contains(string(out), "- a hand-written log line") {
		t.Errorf("expected raw log line in output:\n%s", out)
	}
}

// TestSectionSeparatorBranches covers the remaining sectionSeparator
// branches via encodeNew with different Description/Body endings.
func TestSectionSeparatorBranches(t *testing.T) {
	base := func(desc, body string) *Issue {
		return &Issue{
			ID: "p-ab12", Title: "T", Type: "task", Status: "open", Priority: 1,
			Created:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Updated:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Description: desc, Body: body,
			Log: []LogEntry{{Raw: "- x"}},
		}
	}

	// Empty description/body: text already ends with the closing fence, so
	// sectionSeparator must return "".
	iss := base("", "")
	out, err := Encode(iss)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "---\n## Log\n") {
		t.Errorf("expected Log to follow the fence directly:\n%s", out)
	}

	// Body with no trailing newline at all: sectionSeparator must insert a
	// full blank-line separator ("\n\n").
	iss2 := base("", "no trailing newline")
	out2, err := Encode(iss2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out2), "no trailing newline\n\n## Log\n") {
		t.Errorf("expected a blank line before Log:\n%s", out2)
	}

	// Body already ending in a blank line ("\n\n"): sectionSeparator must
	// add nothing further.
	iss3 := base("", "para\n\n")
	out3, err := Encode(iss3)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out3), "para\n\n## Log\n") {
		t.Errorf("expected no extra blank line:\n%s", out3)
	}
	if strings.Contains(string(out3), "para\n\n\n## Log\n") {
		t.Errorf("unexpected extra blank line:\n%s", out3)
	}
}
