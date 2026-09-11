package issue

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const requestFixture = `---
id: beans-r-a3f2
aliases: [beans-r-a3f2]
title: Durable requests
status: open
priority: 2
labels: [planning]
requested_by: matt
issues:
  - "[[beans-a1b2]]"
custom: preserve
created: 2026-09-11T15:51:49Z
updated: 2026-09-11T15:51:49Z
---
Context.

## Request
Do the thing.

## Log
- 2026-09-11T15:51:49Z matt: created

## Later
Keep this too.
`

func TestRequestRoundTripAndMutation(t *testing.T) {
	r, err := ParseRequest("projects/beans/requests/beans-r-a3f2-durable-requests.md", []byte(requestFixture))
	if err != nil {
		t.Fatal(err)
	}
	out, err := EncodeRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != requestFixture {
		t.Fatalf("round trip mismatch\nwant:\n%s\ngot:\n%s", requestFixture, out)
	}
	r.Status = RequestAccepted
	AppendRequestLog(r, LogEntry{At: time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC), Actor: "matt", Event: "status open → accepted"})
	out, err = EncodeRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "status: accepted") || !strings.Contains(string(out), "## Later\nKeep this too.\n") {
		t.Fatalf("mutation lost content:\n%s", out)
	}
	if !strings.Contains(string(out), "- 2026-09-12T00:00:00Z matt: status open → accepted") {
		t.Fatalf("log not appended:\n%s", out)
	}
}

func TestRequestRoundTripFixtures(t *testing.T) {
	files, err := filepath.Glob("testdata/request-roundtrip/*.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 5 {
		t.Fatalf("expected request fixtures, found %d", len(files))
	}
	for _, file := range files {
		name := filepath.Base(file)
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		_, err = ParseRequest("projects/beans/requests/beans-r-a3f2-"+name, data)
		if strings.HasPrefix(name, "err_") {
			if err == nil {
				t.Errorf("%s: expected parse error", name)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: ParseRequest: %v", name, err)
			continue
		}
		r, _ := ParseRequest("projects/beans/requests/beans-r-a3f2-"+name, data)
		out, err := EncodeRequest(r)
		if err != nil || string(out) != string(data) {
			t.Errorf("%s: round trip failed: %v", name, err)
		}
	}
}

func TestRequestParsingErrors(t *testing.T) {
	for name, data := range map[string]struct{ path, data string }{
		"invalid-path":   {"projects/beans/issues/beans-r-a3f2.md", requestFixture},
		"invalid-status": {"projects/beans/requests/beans-r-a3f2.md", strings.Replace(requestFixture, "status: open", "status: later", 1)},
		"crlf":           {"projects/beans/requests/beans-r-a3f2.md", strings.ReplaceAll(requestFixture, "\n", "\r\n")},
		"missing-title":  {"projects/beans/requests/beans-r-a3f2.md", strings.Replace(requestFixture, "title: Durable requests\n", "", 1)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseRequest(data.path, []byte(data.data)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestSetRequestBody(t *testing.T) {
	r, err := ParseRequest("projects/beans/requests/beans-r-a3f2.md", []byte(requestFixture))
	if err != nil {
		t.Fatal(err)
	}
	SetRequestBody(r, "replacement\n\n")
	out, err := EncodeRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "---\nreplacement\n## Log") || !strings.Contains(string(out), "## Later\nKeep this too.") {
		t.Fatalf("body replacement was not delimited correctly:\n%s", out)
	}
}
