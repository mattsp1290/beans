package issue

import (
	"testing"
	"time"
)

const memSample = `---
key: bean-counter-prod-schema
type: project
tags: [deploy, prod]
created: 2026-06-14T00:00:00Z
updated: 2026-09-10T00:00:00Z
# a user comment
custom: keep me
---
Body text here.

More body.
`

func TestMemoryRoundTrip(t *testing.T) {
	m, err := ParseMemory("memories/bean-counter-prod-schema.md", []byte(memSample))
	if err != nil {
		t.Fatal(err)
	}
	if m.Key != "bean-counter-prod-schema" || m.Type != "project" {
		t.Errorf("parsed fields wrong: %+v", m)
	}
	if !equalStringSlices(m.Tags, []string{"deploy", "prod"}) {
		t.Errorf("Tags = %v", m.Tags)
	}
	if len(m.Extra.Content) < 2 || m.Extra.Content[0].Value != "custom" {
		t.Errorf("extra key not kept: %+v", m.Extra)
	}

	out, err := EncodeMemory(m)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != memSample {
		t.Fatalf("round trip differs:\n%s", out)
	}
}

func TestMemoryMutationSplicesOnlyChangedLines(t *testing.T) {
	m, err := ParseMemory("m.md", []byte(memSample))
	if err != nil {
		t.Fatal(err)
	}
	m.Type = "reference"
	m.Tags = append(m.Tags, "urgent")

	out, err := EncodeMemory(m)
	if err != nil {
		t.Fatal(err)
	}

	removed, added := lineDiff(memSample, string(out))
	wantRemoved := []string{"type: project", "tags: [deploy, prod]"}
	wantAdded := []string{"type: reference", "tags: [deploy, prod, urgent]"}
	if !equalStringSlices(removed, wantRemoved) || !equalStringSlices(added, wantAdded) {
		t.Fatalf("unexpected diff\nremoved: %#v (want %#v)\nadded: %#v (want %#v)\nout:\n%s",
			removed, wantRemoved, added, wantAdded, out)
	}

	again, err := ParseMemory("m.md", out)
	if err != nil {
		t.Fatal(err)
	}
	if again.Type != "reference" {
		t.Errorf("re-parsed type = %q", again.Type)
	}
	if !equalStringSlices(again.Tags, []string{"deploy", "prod", "urgent"}) {
		t.Errorf("re-parsed tags = %v", again.Tags)
	}
}

func TestNewMemoryEncodesInKeyOrder(t *testing.T) {
	m := &Memory{
		Key:     "new-memory-key",
		Type:    "user",
		Tags:    []string{"a", "b"},
		Created: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Updated: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		Body:    "Some body text.\n",
	}
	out, err := EncodeMemory(m)
	if err != nil {
		t.Fatal(err)
	}
	want := `---
key: new-memory-key
type: user
tags: [a, b]
created: 2026-01-01T00:00:00Z
updated: 2026-01-02T00:00:00Z
---
Some body text.
`
	if string(out) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}

	back, err := ParseMemory("new-memory-key.md", out)
	if err != nil {
		t.Fatal(err)
	}
	if back.Key != m.Key || back.Type != m.Type {
		t.Errorf("reparse mismatch: %+v", back)
	}
}

func TestParseMemoryMissingKeyIsError(t *testing.T) {
	data := []byte("---\ntype: user\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\nbody\n")
	_, err := ParseMemory("no-key.md", data)
	if err == nil {
		t.Fatal("expected an error for a missing key")
	}
}

func TestValidMemoryKey(t *testing.T) {
	accept := []string{"bean-counter-prod-schema", "a", "a1-b2-c3"}
	reject := []string{"", "-leading-dash", "Upper", "has_underscore", "has space"}
	for _, k := range accept {
		if !ValidMemoryKey(k) {
			t.Errorf("ValidMemoryKey(%q) = false, want true", k)
		}
	}
	for _, k := range reject {
		if ValidMemoryKey(k) {
			t.Errorf("ValidMemoryKey(%q) = true, want false", k)
		}
	}

	long := make([]byte, MemoryKeyMaxLen+1)
	for i := range long {
		long[i] = 'a'
	}
	if ValidMemoryKey(string(long)) {
		t.Errorf("ValidMemoryKey accepted a key longer than %d chars", MemoryKeyMaxLen)
	}
	ok := make([]byte, MemoryKeyMaxLen)
	for i := range ok {
		ok[i] = 'a'
	}
	if !ValidMemoryKey(string(ok)) {
		t.Errorf("ValidMemoryKey rejected a key of exactly %d chars", MemoryKeyMaxLen)
	}
}

func TestValidMemoryType(t *testing.T) {
	accept := []string{"user", "feedback", "project", "reference", ""}
	reject := []string{"bogus", "USER", "task"}
	for _, typ := range accept {
		if !ValidMemoryType(typ) {
			t.Errorf("ValidMemoryType(%q) = false, want true", typ)
		}
	}
	for _, typ := range reject {
		if ValidMemoryType(typ) {
			t.Errorf("ValidMemoryType(%q) = true, want false", typ)
		}
	}
}

func TestParseMemoryRejectsCRLF(t *testing.T) {
	data := []byte("---\r\nkey: x\r\n---\r\nbody\r\n")
	_, err := ParseMemory("crlf.md", data)
	if err == nil {
		t.Fatal("expected an error for Windows line endings")
	}
}
