package issue

import (
	"testing"
	"time"
)

func TestFormatParseLogEntryRoundTrip(t *testing.T) {
	at := time.Date(2026, 9, 10, 8, 1, 0, 0, time.UTC)

	cases := []struct {
		name string
		e    LogEntry
	}{
		{
			name: "full repo sha branch",
			e:    LogEntry{At: at, Actor: "matt", Repo: "exa", SHA: "a1b2c3d", Branch: "feature/x", Event: "status open → in_progress"},
		},
		{
			name: "no repo info",
			e:    LogEntry{At: at, Actor: "claude", Event: "closed — parity gate passes"},
		},
		{
			name: "repo and sha, no branch",
			e:    LogEntry{At: at, Actor: "matt", Repo: "exa", SHA: "a1b2c3d", Event: "note — deployed"},
		},
		{
			name: "multi-line event",
			e:    LogEntry{At: at, Actor: "m", Event: "line1\nline2"},
		},
		{
			name: "multi-line event with three lines",
			e:    LogEntry{At: at, Actor: "m", Event: "first\nsecond\nthird"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text := FormatLogEntry(c.e)
			got, ok := ParseLogEntry(text)
			if !ok {
				t.Fatalf("ParseLogEntry(%q) ok=false", text)
			}
			if !got.At.Equal(c.e.At) || got.Actor != c.e.Actor || got.Repo != c.e.Repo ||
				got.SHA != c.e.SHA || got.Branch != c.e.Branch || got.Event != c.e.Event {
				t.Errorf("round trip mismatch:\n text: %q\n got:  %+v\n want: %+v", text, got, c.e)
			}
		})
	}
}

func TestFormatLogEntryOmitsParensWithoutRepoInfo(t *testing.T) {
	at := time.Date(2026, 9, 10, 8, 1, 0, 0, time.UTC)
	e := LogEntry{At: at, Actor: "claude", Event: "note — hi"}
	got := FormatLogEntry(e)
	want := "- 2026-09-10T08:01:00Z claude: note — hi"
	if got != want {
		t.Errorf("FormatLogEntry = %q, want %q", got, want)
	}
}

func TestFormatLogEntryRepoAndShaNoBranch(t *testing.T) {
	at := time.Date(2026, 9, 10, 8, 1, 0, 0, time.UTC)
	e := LogEntry{At: at, Actor: "matt", Repo: "exa", SHA: "a1b2c3d", Event: "note — x"}
	got := FormatLogEntry(e)
	want := "- 2026-09-10T08:01:00Z matt (exa@a1b2c3d): note — x"
	if got != want {
		t.Errorf("FormatLogEntry = %q, want %q", got, want)
	}
}

func TestParseLogEntryMinutePrecision(t *testing.T) {
	e, ok := ParseLogEntry("- 2026-09-10T08:01Z matt: created")
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := time.Date(2026, 9, 10, 8, 1, 0, 0, time.UTC)
	if !e.At.Equal(want) {
		t.Errorf("At = %v, want %v", e.At, want)
	}
	if e.Actor != "matt" || e.Event != "created" {
		t.Errorf("entry = %+v", e)
	}
}

func TestParseLogEntryNonMatchingLines(t *testing.T) {
	cases := []string{
		"not a log line at all",
		"- missing colon and format",
		"- 2026-09-10T08:01:00Z noactorcolon",
		"",
		"just some text: with a colon",
	}
	for _, c := range cases {
		if _, ok := ParseLogEntry(c); ok {
			t.Errorf("ParseLogEntry(%q) ok=true, want false", c)
		}
	}
}

func TestParseLogEntryMultiLineContinuation(t *testing.T) {
	text := "- 2026-09-10T08:01:00Z matt: note — first line\n  second line\n  third line"
	e, ok := ParseLogEntry(text)
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := "note — first line\nsecond line\nthird line"
	if e.Event != want {
		t.Errorf("Event = %q, want %q", e.Event, want)
	}
}
