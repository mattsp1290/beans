package issue

import (
	"strings"
	"testing"
	"time"
)

const sample = `---
id: exa-a3f2
aliases: [exa-a3f2]
title: Migrate the live infra-host deploy
type: task
status: open
priority: 2
labels: [deploy, infra]
assignee: matt
parent: "[[exa-mkg1-deploy-bean-counter]]"
blocked_by:
  - "[[exa-ued1-schema-parity]]"
url: https://example.invalid/ticket/1
created: 2026-06-15T10:22:00Z
updated: 2026-09-10T08:01:00Z
# a user comment
custom: keep me
---
Description paragraphs. Free markdown.

## Acceptance
- [ ] parity gate passes

## Log
- 2026-09-10T08:01:00Z matt (exa@a1b2c3d feature/x): status open → in_progress
- 2026-09-11T10:00:00Z claude: closed — parity gate passes
`

func TestRoundTrip(t *testing.T) {
	iss, err := Parse("projects/exa/issues/exa-a3f2-migrate.md", []byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Encode(iss)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != sample {
		t.Fatalf("round trip differs:\n%s", string(out))
	}
	if iss.Project != "exa" || iss.Archived || iss.Priority != 2 || iss.Parent.Target != "exa-mkg1-deploy-bean-counter" || len(iss.Log) != 2 || iss.Log[0].SHA != "a1b2c3d" || iss.Log[1].Event != "closed — parity gate passes" {
		t.Errorf("parsed fields wrong: %+v", iss)
	}
	if iss.Extra.Content[0].Value != "custom" {
		t.Errorf("extra key not kept: %+v", iss.Extra)
	}
}

func TestMutationsTouchOnlyTheirLines(t *testing.T) {
	iss, _ := Parse("x.md", []byte(sample))
	iss.Status = "in_progress"
	iss.BlockedBy = append(iss.BlockedBy, NewLink("exa-zzzz"))
	iss.Assignee = ""
	iss.Updated = time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	AppendLog(iss, LogEntry{At: iss.Updated, Actor: "claude", Event: "status open → in_progress"})
	SetDescription(iss, "New description\nwith two lines")
	out, _ := Encode(iss)
	want := `---
id: exa-a3f2
aliases: [exa-a3f2]
title: Migrate the live infra-host deploy
type: task
status: in_progress
priority: 2
labels: [deploy, infra]
parent: "[[exa-mkg1-deploy-bean-counter]]"
blocked_by:
  - "[[exa-ued1-schema-parity]]"
  - "[[exa-zzzz]]"
url: https://example.invalid/ticket/1
created: 2026-06-15T10:22:00Z
updated: 2026-09-12T00:00:00Z
# a user comment
custom: keep me
---
New description
with two lines

## Acceptance
- [ ] parity gate passes

## Log
- 2026-09-10T08:01:00Z matt (exa@a1b2c3d feature/x): status open → in_progress
- 2026-09-11T10:00:00Z claude: closed — parity gate passes
- 2026-09-12T00:00:00Z claude: status open → in_progress
`
	if string(out) != want {
		t.Fatalf("got:\n%s", out)
	}
	// re-parse the output and verify stability
	again, err := Parse("x.md", out)
	if err != nil {
		t.Fatal(err)
	}
	out2, _ := Encode(again)
	if string(out2) != string(out) {
		t.Fatal("second round trip differs")
	}
}

func TestNewIssueEncodeAndLogCreation(t *testing.T) {
	iss := &Issue{ID: "p-ab12", Title: "Title: with colon", Type: "task", Status: "open", Priority: 2,
		Created: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Updated: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Description: "desc\n", Body: "\n## Acceptance\n- [ ] x\n"}
	AppendLog(iss, LogEntry{At: iss.Created, Actor: "matt", Event: "created"})
	out, err := Encode(iss)
	if err != nil {
		t.Fatal(err)
	}
	want := `---
id: p-ab12
aliases: [p-ab12]
title: 'Title: with colon'
type: task
status: open
priority: 2
created: 2026-01-01T00:00:00Z
updated: 2026-01-01T00:00:00Z
---
desc

## Acceptance
- [ ] x

## Log
- 2026-01-01T00:00:00Z matt: created
`
	if string(out) != want {
		t.Fatalf("got:\n%s", out)
	}
	back, err := Parse("projects/p/issues/p-ab12-title.md", out)
	if err != nil {
		t.Fatal(err)
	}
	if back.Title != "Title: with colon" || back.Log[0].Event != "created" {
		t.Errorf("reparse wrong: %+v", back)
	}
	// Appending a log to a file without a ## Log section creates one.
	noLog := "---\nid: a-1\ntitle: t\ntype: task\nstatus: open\npriority: 1\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\nbody text\n"
	iss2, err := Parse("a.md", []byte(noLog))
	if err != nil {
		t.Fatal(err)
	}
	AppendLog(iss2, LogEntry{At: iss2.Created, Actor: "m", Event: "note — hi\nsecond line"})
	out2, _ := Encode(iss2)
	if !strings.HasSuffix(string(out2), "body text\n\n## Log\n- 2026-01-01T00:00:00Z m: note — hi\n  second line\n") {
		t.Fatalf("got:\n%s", out2)
	}
	if strings.Contains(string(out2), "updated: 2026-01-01T00:00:00Z\n---") == false {
		t.Errorf("updated should be unchanged when log time is not later")
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"crlf":       "---\r\nid: x\r\n---\r\n",
		"no fence":   "id: x\n",
		"no close":   "---\nid: x\n",
		"no id":      "---\ntitle: x\n---\n",
		"bad prio":   "---\nid: x\ntitle: t\ntype: task\nstatus: open\npriority: high\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\n",
		"bad time":   "---\nid: x\ntitle: t\ntype: task\nstatus: open\npriority: 1\ncreated: yesterday\nupdated: 2026-01-01T00:00:00Z\n---\n",
		"not a map":  "---\n- a\n---\n",
		"empty":      "---\n---\n",
		"dup key":    "---\nid: x\nid: y\n---\n",
		"list title": "---\nid: x\ntitle: [a]\n---\n",
	}
	for name, in := range cases {
		if _, err := Parse(name+".md", []byte(in)); err == nil {
			t.Errorf("%s: expected error", name)
		} else if !strings.Contains(err.Error(), name+".md") {
			t.Errorf("%s: error should name the file: %v", name, err)
		}
	}
}
