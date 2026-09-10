package main

import (
	"os"
	"strings"
	"testing"
)

func TestParseBDExportLinesSkipsBlankCommentsAndBadRows(t *testing.T) {
	t.Parallel()
	in := strings.NewReader(`
# comment
{"id":"p-1","title":"one","status":"open","priority":2,"issue_type":"task","dependencies":[{"issue_id":"p-1","depends_on_id":"p-0","type":"blocks"}]}
not json
{"id":"","title":"no id"}
{"id":"p-2","title":"two","status":"closed"}
`)
	lines, warnings, err := parseBDExportLines(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || warnings != 2 {
		t.Fatalf("got %d lines, %d warnings; want 2 and 2", len(lines), warnings)
	}
	if lines[0].Dependencies[0].DependsOn != "p-0" || lines[0].Dependencies[0].Type != "blocks" {
		t.Errorf("dependency not decoded: %+v", lines[0].Dependencies)
	}
}

func TestParseBDExportLinesFixture(t *testing.T) {
	t.Parallel()
	f, err := os.Open("testdata/gastownhall_beads_export.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	lines, warnings, err := parseBDExportLines(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 {
		t.Fatal("fixture parsed to zero lines")
	}
	if warnings != 0 {
		t.Errorf("fixture produced %d warnings, want 0", warnings)
	}
}
