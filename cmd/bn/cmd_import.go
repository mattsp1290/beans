package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	store "github.com/mattsp1290/beans/store"
)

// bdExportLine mirrors the JSON shape emitted by bd export.
// Fields are named after the real bd export output (grounding gate confirmed:
// bd exports full dep edges in "dependencies[]").
// close_reason and owner are explicitly dropped — no ImportInput equivalent.
type bdExportLine struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Description  string        `json:"description"`
	Status       string        `json:"status"`   // bd uses "status"; bn store uses "state"
	Priority     int           `json:"priority"` // 0-indexed (bd and bn store are both 0-indexed — no conversion)
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
	Type      string `json:"type"`          // "blocks" or other
}

type importSummary struct {
	DryRun                    bool `json:"dry_run"`
	Parsed                    int  `json:"parsed"`
	Warnings                  int  `json:"warnings"`
	Created                   int  `json:"created"`
	Updated                   int  `json:"updated"`
	Skipped                   int  `json:"skipped"`
	CrossPrefixConflicts      int  `json:"cross_prefix_conflicts"`
	DepsAdded                 int  `json:"deps_added"`
	DepsSkippedMissingBlocker int  `json:"deps_skipped_missing_blocker"`
	DepsSkippedDuplicate      int  `json:"deps_skipped_duplicate"`
	DepsSkippedSelf           int  `json:"deps_skipped_self"`
	DepsSkippedCycle          int  `json:"deps_skipped_cycle"`
	ParentEdgesAdded          int  `json:"parent_edges_added"`
	ParentEdgesSkippedMissing int  `json:"parent_edges_skipped_missing"`
}

func newImportCmd(rs *appState) *cobra.Command {
	var (
		filePath string
		mode     string
		dryRun   bool
	)

	cmd := &cobra.Command{
		Use:   "import [file]",
		Short: "Seed issues from a bd-export-compatible JSONL file",
		Long: `Import issues from a JSONL file (one issue per line, bd-export format).

Modes:
  create-only (default) — insert new issues; skip existing (safe for idempotent re-runs)
  merge                 — update non-terminal fields; never regress a terminal state

The bd "status" field maps to state. Priority is 0-indexed in both bd and bn (no conversion).
Dep edges with a blocker not present in the batch or DB are silently skipped (flagged in summary).
close_reason and owner are not imported.`,
		Args: cobra.MaximumNArgs(1),
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if dryRun {
				prefix, err := resolveProjectPrefix(rs.prefix, false)
				if err != nil {
					return err
				}
				rs.prefix = prefix
				return nil
			}
			return rs.initConn(cmd.Context())
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.requirePrefix(); err != nil {
				return err
			}

			// Resolve input source: positional arg > --file flag > stdin.
			var r io.Reader
			switch {
			case len(args) > 0:
				f, err := os.Open(args[0])
				if err != nil {
					return fmt.Errorf("import: open %s: %w", args[0], err)
				}
				defer f.Close()
				r = f
			case filePath != "":
				f, err := os.Open(filePath)
				if err != nil {
					return fmt.Errorf("import: open %s: %w", filePath, err)
				}
				defer f.Close()
				r = f
			default:
				r = cmd.InOrStdin()
			}

			// Parse mode.
			var importMode store.ImportMode
			switch strings.ToLower(mode) {
			case "create-only", "create_only", "":
				importMode = store.ImportModeCreateOnly
			case "merge":
				importMode = store.ImportModeMerge
			default:
				return fmt.Errorf("import: unknown mode %q (valid: create-only, merge)", mode)
			}

			// Parse JSONL line by line.
			items, parseWarnings, err := parseImportJSONL(r, rs.prefix)
			if err != nil {
				return fmt.Errorf("import: parse: %w", err)
			}

			if dryRun {
				summary := importSummary{
					DryRun:   true,
					Parsed:   len(items),
					Warnings: parseWarnings,
				}
				return writeImportSummary(cmd, rs.jsonOut, summary)
			}

			// B2: auto-register project so import doesn't FK-fail on first run.
			if err := rs.store.EnsureProject(cmd.Context(), rs.prefix); err != nil {
				return fmt.Errorf("import: ensure project: %w", err)
			}

			result, err := rs.store.ImportIssuesFull(cmd.Context(), items, store.ImportOptions{
				TerminalStates: defaultTerminalStates,
				Mode:           importMode,
			})
			if err != nil {
				return fmt.Errorf("import: %w", err)
			}

			summary := importSummaryFromResult(result, len(items), parseWarnings)
			if err := writeImportSummary(cmd, rs.jsonOut, summary); err != nil {
				return err
			}
			if parseWarnings > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %d lines skipped\n", parseWarnings)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&filePath, "file", "f", "", "JSONL file to import (default: stdin)")
	cmd.Flags().StringVar(&mode, "mode", "create-only", "import mode: create-only|merge")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "parse and count only, no DB writes")
	return cmd
}

func importSummaryFromResult(result store.ImportResult, parsed, warnings int) importSummary {
	return importSummary{
		Parsed:                    parsed,
		Warnings:                  warnings,
		Created:                   result.Created,
		Updated:                   result.Updated,
		Skipped:                   result.Skipped,
		CrossPrefixConflicts:      result.CrossPrefixConflicts,
		DepsAdded:                 result.DepsAdded,
		DepsSkippedMissingBlocker: result.DepsSkippedMissingBlocker,
		DepsSkippedDuplicate:      result.DepsSkippedDuplicate,
		DepsSkippedSelf:           result.DepsSkippedSelf,
		DepsSkippedCycle:          result.DepsSkippedCycle,
		ParentEdgesAdded:          result.ParentEdgesAdded,
		ParentEdgesSkippedMissing: result.ParentEdgesSkippedMissing,
	}
}

func writeImportSummary(cmd *cobra.Command, jsonOut bool, summary importSummary) error {
	if jsonOut {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(summary)
	}

	w := cmd.OutOrStdout()
	if summary.DryRun {
		fmt.Fprintf(w, "dry-run: would import %d issues (%d warnings)\n", summary.Parsed, summary.Warnings)
		return nil
	}
	fmt.Fprintf(w, "Import complete: created=%d updated=%d skipped=%d deps_added=%d",
		summary.Created, summary.Updated, summary.Skipped, summary.DepsAdded)
	if summary.ParentEdgesAdded > 0 {
		fmt.Fprintf(w, " parent_edges_added=%d", summary.ParentEdgesAdded)
	}
	if summary.ParentEdgesSkippedMissing > 0 {
		fmt.Fprintf(w, " parent_edges_skipped(missing_parent)=%d", summary.ParentEdgesSkippedMissing)
	}
	if summary.CrossPrefixConflicts > 0 {
		fmt.Fprintf(w, " cross_prefix_conflicts=%d", summary.CrossPrefixConflicts)
	}
	if summary.DepsSkippedMissingBlocker > 0 {
		fmt.Fprintf(w, " deps_skipped(missing_blocker)=%d", summary.DepsSkippedMissingBlocker)
	}
	if summary.DepsSkippedDuplicate > 0 {
		fmt.Fprintf(w, " deps_skipped(duplicate)=%d", summary.DepsSkippedDuplicate)
	}
	if summary.DepsSkippedSelf > 0 {
		fmt.Fprintf(w, " deps_skipped(self)=%d", summary.DepsSkippedSelf)
	}
	if summary.DepsSkippedCycle > 0 {
		fmt.Fprintf(w, " deps_skipped(cycle)=%d", summary.DepsSkippedCycle)
	}
	fmt.Fprintln(w)
	return nil
}

// parseImportJSONL parses a bd-export-compatible JSONL stream into ImportInputs.
// Returns the items, the count of lines skipped due to parse errors, and any
// fatal IO error.
func parseImportJSONL(r io.Reader, destPrefix string) ([]store.ImportInput, int, error) {
	var items []store.ImportInput
	var warnings int

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
		// Validate priority range to avoid a single bad row aborting the entire
		// import transaction. Consistent with the lenient skip-and-warn handling
		// for malformed JSON above.
		if raw.Priority < 0 || raw.Priority > 4 {
			warnings++
			continue
		}
		if !isAllowedState(raw.Status) {
			warnings++
			continue
		}

		// Extract dep edges where this issue is the child (issue_id == raw.ID).
		// "blocks" edges become blocking deps; "parent-child" edges become
		// non-blocking membership. bn gives semantics only to these two kinds, so
		// any other type (related, discovered-from, reverse/parent edges bd
		// includes, …) is intentionally NOT imported — export emits them but a
		// bn→bn round-trip only preserves blocks + parent-child. This asymmetry
		// is deliberate and scoped to what the planning skills require.
		var deps []string
		var parentEdges []string
		for _, dep := range raw.Dependencies {
			if dep.IssueID != raw.ID || dep.DependsOn == "" {
				continue
			}
			switch dep.Type {
			case store.DepTypeBlocks:
				deps = append(deps, dep.DependsOn)
			case store.DepTypeParentChild:
				parentEdges = append(parentEdges, dep.DependsOn)
			}
		}

		// Note: raw.ID may carry the source project's prefix token (e.g. "oldproj-abc")
		// while Prefix is rewritten to destPrefix. Queries scope by the prefix column
		// so this is functionally correct, but the id-prefix and prefix-column diverge
		// for cross-project imports. This is intentional for bd-migration compatibility.
		items = append(items, store.ImportInput{
			ID:          raw.ID,
			Prefix:      destPrefix,
			Title:       raw.Title,
			Description: raw.Description,
			Priority:    raw.Priority, // 0-indexed in both bd and bn — no conversion
			IssueType:   raw.IssueType,
			State:       raw.Status, // bd uses "status", ImportInput uses "State"
			Labels:      raw.Labels,
			BranchName:  raw.BranchName,
			URL:         raw.URL,
			Deps:        deps,
			ParentEdges: parentEdges,
		})
	}

	if err := sc.Err(); err != nil {
		return nil, warnings, err
	}
	return items, warnings, nil
}
