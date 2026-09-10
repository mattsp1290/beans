package issue

import (
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// mutBase is a canonical-ish issue file with a parent, one blocker, no
// assignee, and a user-owned key trailing the bn-owned block, so each
// mutation below exercises a distinct splice path in Encode.
const mutBase = `---
id: mut-a1b2
aliases: [mut-a1b2]
title: Mutation base
type: task
status: open
priority: 2
parent: "[[mut-parent1]]"
blocked_by:
  - "[[mut-block1]]"
created: 2026-01-01T00:00:00Z
updated: 2026-01-01T00:00:00Z
custom_after: keep me
---
Original description.

## Acceptance
- [ ] something

## Log
- 2026-01-01T00:00:00Z matt: created
`

func mutParse(t *testing.T) *Issue {
	t.Helper()
	iss, err := Parse("mut.md", []byte(mutBase))
	if err != nil {
		t.Fatal(err)
	}
	return iss
}

func mutEncode(t *testing.T, iss *Issue) string {
	t.Helper()
	out, err := Encode(iss)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestMutationSetStatus(t *testing.T) {
	iss := mutParse(t)
	iss.Status = "in_progress"
	out := mutEncode(t, iss)

	removed, added := lineDiff(mutBase, out)
	wantRemoved := []string{"status: open"}
	wantAdded := []string{"status: in_progress"}
	if !equalStringSlices(removed, wantRemoved) || !equalStringSlices(added, wantAdded) {
		t.Fatalf("unexpected diff\nremoved: %#v (want %#v)\nadded: %#v (want %#v)\nout:\n%s",
			removed, wantRemoved, added, wantAdded, out)
	}

	again, err := Parse("mut.md", []byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != "in_progress" {
		t.Errorf("re-parsed status = %q", again.Status)
	}
}

func TestMutationAppendLog(t *testing.T) {
	iss := mutParse(t)
	at := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	AppendLog(iss, LogEntry{At: at, Actor: "claude", Event: "note — did a thing"})
	out := mutEncode(t, iss)

	removed, added := lineDiff(mutBase, out)
	wantRemoved := []string{"updated: 2026-01-01T00:00:00Z"}
	wantAdded := []string{
		"updated: 2026-01-02T00:00:00Z",
		"- 2026-01-02T00:00:00Z claude: note — did a thing",
	}
	if !equalStringSlices(removed, wantRemoved) || !equalStringSlices(added, wantAdded) {
		t.Fatalf("unexpected diff\nremoved: %#v (want %#v)\nadded: %#v (want %#v)\nout:\n%s",
			removed, wantRemoved, added, wantAdded, out)
	}

	again, err := Parse("mut.md", []byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Log) != 2 {
		t.Fatalf("re-parsed log has %d entries, want 2", len(again.Log))
	}
	if again.Log[1].Event != "note — did a thing" || again.Log[1].Actor != "claude" {
		t.Errorf("re-parsed second log entry = %+v", again.Log[1])
	}
	if !again.Updated.Equal(at) {
		t.Errorf("re-parsed updated = %v, want %v", again.Updated, at)
	}
}

func TestMutationSetDescription(t *testing.T) {
	iss := mutParse(t)
	SetDescription(iss, "New description text.\nSecond line.")
	out := mutEncode(t, iss)

	removed, added := lineDiff(mutBase, out)
	wantRemoved := []string{"Original description."}
	wantAdded := []string{"New description text.", "Second line."}
	if !equalStringSlices(removed, wantRemoved) || !equalStringSlices(added, wantAdded) {
		t.Fatalf("unexpected diff\nremoved: %#v (want %#v)\nadded: %#v (want %#v)\nout:\n%s",
			removed, wantRemoved, added, wantAdded, out)
	}

	again, err := Parse("mut.md", []byte(out))
	if err != nil {
		t.Fatal(err)
	}
	wantDesc := "New description text.\nSecond line.\n\n"
	if again.Description != wantDesc {
		t.Errorf("re-parsed description = %q, want %q", again.Description, wantDesc)
	}
}

func TestMutationAddBlocker(t *testing.T) {
	iss := mutParse(t)
	iss.BlockedBy = append(iss.BlockedBy, NewLink("mut-block2"))
	out := mutEncode(t, iss)

	removed, added := lineDiff(mutBase, out)
	wantAdded := []string{`  - "[[mut-block2]]"`}
	if len(removed) != 0 || !equalStringSlices(added, wantAdded) {
		t.Fatalf("unexpected diff\nremoved: %#v (want none)\nadded: %#v (want %#v)\nout:\n%s",
			removed, added, wantAdded, out)
	}

	again, err := Parse("mut.md", []byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if len(again.BlockedBy) != 2 || again.BlockedBy[1].Target != "mut-block2" {
		t.Errorf("re-parsed blocked_by = %+v", again.BlockedBy)
	}
}

func TestMutationRemoveBlockerToZero(t *testing.T) {
	iss := mutParse(t)
	iss.BlockedBy = nil
	out := mutEncode(t, iss)

	removed, added := lineDiff(mutBase, out)
	wantRemoved := []string{"blocked_by:", `  - "[[mut-block1]]"`}
	if !equalStringSlices(removed, wantRemoved) || len(added) != 0 {
		t.Fatalf("unexpected diff\nremoved: %#v (want %#v)\nadded: %#v (want none)\nout:\n%s",
			removed, wantRemoved, added, out)
	}

	again, err := Parse("mut.md", []byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if len(again.BlockedBy) != 0 {
		t.Errorf("re-parsed blocked_by = %+v, want empty", again.BlockedBy)
	}
}

func TestMutationSetAbsentAssignee(t *testing.T) {
	iss := mutParse(t)
	if iss.Assignee != "" {
		t.Fatalf("base fixture already has an assignee: %q", iss.Assignee)
	}
	iss.Assignee = "matt"
	out := mutEncode(t, iss)

	removed, added := lineDiff(mutBase, out)
	wantAdded := []string{"assignee: matt"}
	if len(removed) != 0 || !equalStringSlices(added, wantAdded) {
		t.Fatalf("unexpected diff\nremoved: %#v (want none)\nadded: %#v (want %#v)\nout:\n%s",
			removed, added, wantAdded, out)
	}

	// The new key must land after the last bn-owned key (updated) and
	// before the trailing user-owned key (custom_after).
	updatedIdx := indexOfLine(out, "updated: 2026-01-01T00:00:00Z")
	assigneeIdx := indexOfLine(out, "assignee: matt")
	customIdx := indexOfLine(out, "custom_after: keep me")
	if updatedIdx < 0 || assigneeIdx < 0 || customIdx < 0 {
		t.Fatalf("expected lines not found in output:\n%s", out)
	}
	if updatedIdx >= assigneeIdx || assigneeIdx >= customIdx {
		t.Errorf("assignee not inserted between updated and custom_after: updated=%d assignee=%d custom=%d",
			updatedIdx, assigneeIdx, customIdx)
	}

	again, err := Parse("mut.md", []byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if again.Assignee != "matt" {
		t.Errorf("re-parsed assignee = %q", again.Assignee)
	}
}

func TestMutationClearParent(t *testing.T) {
	iss := mutParse(t)
	iss.Parent = Link{}
	out := mutEncode(t, iss)

	removed, added := lineDiff(mutBase, out)
	wantRemoved := []string{`parent: "[[mut-parent1]]"`}
	if !equalStringSlices(removed, wantRemoved) || len(added) != 0 {
		t.Fatalf("unexpected diff\nremoved: %#v (want %#v)\nadded: %#v (want none)\nout:\n%s",
			removed, wantRemoved, added, out)
	}

	again, err := Parse("mut.md", []byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if !again.Parent.IsZero() {
		t.Errorf("re-parsed parent = %+v, want zero", again.Parent)
	}
}

// indexOfLine returns the 0-based line index of the first line equal to
// want, or -1.
func indexOfLine(text, want string) int {
	i := 0
	start := 0
	for j := 0; j <= len(text); j++ {
		if j == len(text) || text[j] == '\n' {
			if text[start:j] == want {
				return i
			}
			i++
			start = j + 1
		}
	}
	return -1
}

func TestMutationAppendLogWhenLogNotLast(t *testing.T) {
	t.Parallel()
	in, err := os.ReadFile("testdata/roundtrip/g_log_not_last.md")
	if err != nil {
		t.Fatal(err)
	}
	iss, err := Parse("g.md", in)
	if err != nil {
		t.Fatal(err)
	}
	AppendLog(iss, LogEntry{At: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Actor: "claude", Event: "note — after"})
	out, err := Encode(iss)
	if err != nil {
		t.Fatal(err)
	}
	removed, added := lineDiff(string(in), string(out))
	wantAdded := []string{"- 2026-01-02T00:00:00Z claude: note — after", "updated: 2026-01-02T00:00:00Z"}
	sort.Strings(added)
	if len(removed) != 1 || removed[0] != "updated: 2026-01-01T00:00:00Z" || !equalStringSlices(added, wantAdded) {
		t.Fatalf("removed=%q added=%q", removed, added)
	}
	if !strings.HasSuffix(string(out), "- 2026-01-01T00:00:00Z matt: created\n- 2026-01-02T00:00:00Z claude: note — after\n\n## Notes\nSome trailing notes after the log section.\n") {
		t.Fatalf("entry must land inside the log section, before the tail:\n%s", out)
	}
	again, err := Parse("g.md", out)
	if err != nil || len(again.Log) != 2 {
		t.Fatalf("reparse: %v, log=%d", err, len(again.Log))
	}
}

func TestMutationKeepsInlineCommentOnRewrittenScalar(t *testing.T) {
	t.Parallel()
	in, err := os.ReadFile("testdata/roundtrip/c_comments.md")
	if err != nil {
		t.Fatal(err)
	}
	iss, err := Parse("c.md", in)
	if err != nil {
		t.Fatal(err)
	}
	iss.Status = "closed"
	out, err := Encode(iss)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "status: closed # inline comment on bn scalar\n") {
		t.Fatalf("inline comment lost:\n%s", out)
	}
	if !strings.Contains(string(out), "# head comment about this issue\n") || !strings.Contains(string(out), "# comment block\n# before closing fence\n---\n") {
		t.Fatalf("comment lines lost:\n%s", out)
	}
}
