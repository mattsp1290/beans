package issue

import (
	"strings"
	"testing"
	"time"
)

const handoffFixture = "---\nid: alpha-a1b2\naliases: [alpha-a1b2]\ntitle: Continue work\nissue: \"[[alpha-c3d4-fix-it]]\"\ncreated: 2026-09-10T21:44:02Z\nupdated: 2026-09-10T21:44:02Z\ncustom: keep me\n---\n# Context\n\nBody.\n"

func TestHandoffRoundTripAndMinimalAttachmentEdit(t *testing.T) {
	p := "projects/alpha/handoffs/alpha-a1b2-continue-work.md"
	h, err := ParseHandoff(p, []byte(handoffFixture))
	if err != nil {
		t.Fatal(err)
	}
	got, err := EncodeHandoff(h)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != handoffFixture {
		t.Fatalf("round trip:\n%s", got)
	}
	h.Issue = Link{}
	h.Updated = h.Updated.Add(time.Second)
	got, err = EncodeHandoff(h)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "issue:") || !strings.Contains(string(got), "custom: keep me") || !strings.Contains(string(got), "# Context") {
		t.Fatalf("attachment edit lost user content:\n%s", got)
	}
}

func TestParseHandoffRejectsBadInput(t *testing.T) {
	p := "projects/alpha/handoffs/alpha-a1b2-title.md"
	for _, data := range []string{
		strings.Replace(handoffFixture, "id: alpha-a1b2", "id: bad", 1),
		strings.Replace(handoffFixture, "updated: 2026-09-10T21:44:02Z", "updated: nope", 1),
		strings.Replace(handoffFixture, "title: Continue work", "title: Continue work\ntitle: duplicate", 1),
		strings.Replace(handoffFixture, "\n", "\r\n", 1),
	} {
		if _, err := ParseHandoff(p, []byte(data)); err == nil {
			t.Fatalf("accepted invalid input %q", data)
		}
	}
}

func TestParseHandoffRejectsMisleadingPath(t *testing.T) {
	for _, path := range []string{
		"projects/alpha/handoffs/archive/not-a-year/alpha-a1b2-title.md",
		"projects/alpha/handoffs/archive/abcd/alpha-a1b2-title.md",
		"projects/alpha/handoffs/nested/alpha-a1b2-title.md",
		"docs/alpha-a1b2-title.md",
	} {
		if _, err := ParseHandoff(path, []byte(handoffFixture)); err == nil {
			t.Fatalf("accepted path %q", path)
		}
	}
}
