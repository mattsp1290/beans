package issue

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRoundTripFixtures asserts Encode(Parse(f)) == f byte for byte for every
// non-error fixture under testdata/roundtrip, and that files whose name
// starts with "err_" fail to parse.
func TestRoundTripFixtures(t *testing.T) {
	files, err := filepath.Glob("testdata/roundtrip/*.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 12 {
		t.Fatalf("expected at least 12 fixtures, found %d", len(files))
	}

	for _, f := range files {
		name := filepath.Base(f)
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}

			path := "projects/roundtrip/issues/" + name
			if name == "archived.md" {
				// Exercise the archive/<year>/ path convention with a
				// synthetic path distinct from the fixture's location on
				// disk; the codec only cares about the path string.
				path = "projects/p/archive/2026/p-x1.md"
			}

			iss, err := Parse(path, data)
			if strings.HasPrefix(name, "err_") {
				if err == nil {
					t.Fatalf("%s: expected Parse error, got none", name)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: unexpected Parse error: %v", name, err)
			}

			out, err := Encode(iss)
			if err != nil {
				t.Fatalf("%s: Encode error: %v", name, err)
			}
			if string(out) != string(data) {
				removed, added := lineDiff(string(data), string(out))
				t.Fatalf("%s: round trip mismatch\nremoved: %#v\nadded: %#v\n--- got ---\n%s\n--- want ---\n%s",
					name, removed, added, out, data)
			}
		})
	}

	// Spot-check the archived fixture's parsed fields, since its path is
	// synthesized rather than taken from the fixture's own location.
	data, err := os.ReadFile("testdata/roundtrip/archived.md")
	if err != nil {
		t.Fatal(err)
	}
	iss, err := Parse("projects/p/archive/2026/p-x1.md", data)
	if err != nil {
		t.Fatal(err)
	}
	if !iss.Archived {
		t.Error("archived.md: expected Archived true")
	}
	if iss.Project != "p" {
		t.Errorf("archived.md: expected Project %q, got %q", "p", iss.Project)
	}

	// Spot-check the mixed-log fixture's parsed entries.
	data, err = os.ReadFile("testdata/roundtrip/n_log_mixed.md")
	if err != nil {
		t.Fatal(err)
	}
	iss, err = Parse("n_log_mixed.md", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(iss.Log) != 3 {
		t.Fatalf("n_log_mixed.md: expected 3 log entries, got %d: %+v", len(iss.Log), iss.Log)
	}
	if iss.Log[0].Event != "created" {
		t.Errorf("n_log_mixed.md: entry 0 event = %q", iss.Log[0].Event)
	}
	if iss.Log[1].Raw != "- hand-written note without proper format" {
		t.Errorf("n_log_mixed.md: entry 1 Raw = %q", iss.Log[1].Raw)
	}
	wantEvent := "note — multi-line event\ncontinuation of the note\nanother continuation line"
	if iss.Log[2].Event != wantEvent {
		t.Errorf("n_log_mixed.md: entry 2 event = %q, want %q", iss.Log[2].Event, wantEvent)
	}

	// Spot-check the flow-style and single-scalar/no-indent blocked_by
	// fixtures parse into the expected links.
	for _, tc := range []struct {
		file string
		want []string
	}{
		{"d_flow_blocked_by.md", []string{"proj-d001-a", "proj-d001-b"}},
		{"k_blocked_by_no_indent.md", []string{"proj-k001-a", "proj-k001-b"}},
		{"m_single_scalar_blocked_by.md", []string{"proj-m001-a"}},
	} {
		data, err := os.ReadFile("testdata/roundtrip/" + tc.file)
		if err != nil {
			t.Fatal(err)
		}
		iss, err := Parse(tc.file, data)
		if err != nil {
			t.Fatalf("%s: %v", tc.file, err)
		}
		if len(iss.BlockedBy) != len(tc.want) {
			t.Fatalf("%s: got %d blockers, want %d", tc.file, len(iss.BlockedBy), len(tc.want))
		}
		for i, w := range tc.want {
			if iss.BlockedBy[i].Target != w {
				t.Errorf("%s: blocker %d target = %q, want %q", tc.file, i, iss.BlockedBy[i].Target, w)
			}
		}
	}
}
