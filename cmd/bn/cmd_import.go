package main

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

// bdExportLine mirrors the JSON shape emitted by bd export. The full mapping
// into vault files is implemented in WP5; WP1 keeps the parser and its
// testdata alive across the module collapse.
type bdExportLine struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Description  string        `json:"description"`
	Status       string        `json:"status"`
	Priority     int           `json:"priority"`
	IssueType    string        `json:"issue_type"`
	Labels       []string      `json:"labels"`
	BranchName   string        `json:"branch_name"`
	URL          string        `json:"url"`
	Dependencies []bdExportDep `json:"dependencies"`
}

// bdExportDep is one edge from bd's dependencies[].
type bdExportDep struct {
	IssueID   string `json:"issue_id"`      // child (blocked)
	DependsOn string `json:"depends_on_id"` // parent (blocker)
	Type      string `json:"type"`          // "blocks" or "parent-child"
}

// parseBDExportLines reads a bd-export JSONL stream, skipping blank lines and
// `#` comments. Lines that do not decode or lack an id or title are counted as
// warnings and skipped; only an IO error is fatal.
func parseBDExportLines(r io.Reader) (lines []bdExportLine, warnings int, err error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024) // 1 MiB line buffer

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		var raw bdExportLine
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			warnings++
			continue
		}
		if raw.ID == "" || raw.Title == "" {
			warnings++
			continue
		}
		lines = append(lines, raw)
	}
	if err := sc.Err(); err != nil {
		return nil, warnings, err
	}
	return lines, warnings, nil
}
