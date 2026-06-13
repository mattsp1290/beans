package main

import (
	"strings"
	"testing"
)

func TestParseImportJSONLSkipsInvalidStatus(t *testing.T) {
	t.Parallel()

	input := strings.NewReader(strings.Join([]string{
		`{"id":"src-good","title":"good","status":"open","priority":2,"issue_type":"task"}`,
		`{"id":"src-bad","title":"bad","status":"archived","priority":2,"issue_type":"task"}`,
		`{"id":"src-done","title":"done","status":"done","priority":2,"issue_type":"task"}`,
	}, "\n"))

	items, warnings, err := parseImportJSONL(input, "dest")
	if err != nil {
		t.Fatalf("parseImportJSONL: %v", err)
	}
	if warnings != 1 {
		t.Fatalf("warnings = %d, want 1", warnings)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2: %+v", len(items), items)
	}
	if items[0].ID != "src-good" || items[0].State != "open" {
		t.Fatalf("first item = %+v, want src-good/open", items[0])
	}
	if items[1].ID != "src-done" || items[1].State != "done" {
		t.Fatalf("second item = %+v, want src-done/done", items[1])
	}
}
