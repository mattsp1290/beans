package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	store "github.com/mattsp1290/beans/store"
)

func newRememberCmd(rs *appState) *cobra.Command {
	var (
		memType string
		tags    []string
		global  bool
	)

	cmd := &cobra.Command{
		Use:   "remember <body>",
		Short: "Persist a memory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := strings.TrimSpace(args[0])
			if body == "" {
				return fmt.Errorf("memory body must not be empty")
			}

			prefix := rs.prefix
			if global {
				prefix = "" // NULL in DB = global
			} else {
				if err := rs.requirePrefix(); err != nil {
					return err
				}
				// Auto-register project so the FK doesn't fail.
				if err := rs.store.EnsureProject(cmd.Context(), prefix); err != nil {
					return fmt.Errorf("remember: ensure project: %w", err)
				}
			}

			mem, err := rs.store.InsertMemory(cmd.Context(), store.MemoryInput{
				Prefix: prefix,
				Body:   body,
				Type:   memType,
				Tags:   tags,
			})
			if err != nil {
				return fmt.Errorf("remember: %w", err)
			}

			if rs.jsonOut {
				return writeJSON(memoryToJSON(mem))
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Remembered #%d\n", mem.ID)
			return nil
		},
	}

	cmd.Flags().StringVar(&memType, "type", "", "memory type: user|feedback|project|reference")
	cmd.Flags().StringArrayVar(&tags, "tag", nil, "tags (repeatable: --tag impl --tag design)")
	cmd.Flags().BoolVar(&global, "global", false, "store as a global memory (not scoped to --project)")
	return cmd
}

func newMemoriesCmd(rs *appState) *cobra.Command {
	var (
		all     bool
		memType string
		tags    []string
		limit   int
	)

	cmd := &cobra.Command{
		Use:   "memories [keyword...]",
		Short: "Search memories",
		Long: `Search memories using the configured database's search support.
Without keywords, returns recent memories ordered by created_at DESC.

Search quality differs from bd's matcher and may vary by database dialect.
Use quotes or --type/--tag to narrow results.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !all {
				if err := rs.requirePrefix(); err != nil {
					return err
				}
			}

			query := strings.Join(args, " ")

			f := store.MemoryFilter{
				All:   all,
				Type:  memType,
				Tags:  tags,
				Limit: limit,
			}
			if !all {
				f.Prefix = rs.prefix
			}

			memories, err := rs.store.SearchMemories(cmd.Context(), query, f)
			if err != nil {
				return fmt.Errorf("memories: %w", err)
			}

			if rs.jsonOut {
				out := make([]memJSON, len(memories))
				for i, m := range memories {
					out[i] = memoryToJSON(m)
				}
				return writeJSON(out)
			}

			if len(memories) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No memories found.")
				return nil
			}

			w := cmd.OutOrStdout()
			for _, m := range memories {
				prefix := "(global)"
				if m.Prefix != nil {
					prefix = *m.Prefix
				}
				mtype := ""
				if m.Type != nil {
					mtype = fmt.Sprintf(" [%s]", *m.Type)
				}
				tags := ""
				if len(m.Tags) > 0 {
					tags = fmt.Sprintf(" #%s", strings.Join(m.Tags, " #"))
				}
				fmt.Fprintf(w, "#%d %s%s%s\n  %s\n", m.ID, prefix, mtype, tags, m.Body)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "search across all repos and projects")
	cmd.Flags().BoolVar(&all, "all-repos", false, "search across all repos and projects (alias for --all)")
	cmd.Flags().StringVar(&memType, "type", "", "filter by type")
	cmd.Flags().StringArrayVar(&tags, "tag", nil, "filter by tag (repeatable)")
	cmd.Flags().IntVarP(&limit, "limit", "n", 0, "max results (default 50)")
	return cmd
}

func newPrimeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prime",
		Short: "Print workflow context and command reference",
		Args:  cobra.NoArgs,
		// Override PersistentPreRunE: prime needs no DB connection.
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprint(cmd.OutOrStdout(), primeText)
			return nil
		},
	}
}

const primeText = `
bn — database-backed issue tracker

ENVIRONMENT
  BN_DRIVER   Database driver: postgres, mysql, or sqlite
              Optional for existing Postgres URL/keyword DSNs
  BN_DSN      Driver-specific connection string (required; never pass on argv)
  BN_PROJECT  Default project prefix
  BN_ACTOR    Default actor for audit notes

WORKFLOW
  Single-repo (custom prefix):
  1. bn init --prefix=<proj>     Register project + write .bn marker
  2. bn create "Task title"       Create issues
     bn create "Slice" --parent <epic>  Add non-blocking epic membership
     bn dep add <child> <parent>  Wire dependencies
  3. bn ready                     List unblocked, open issues
  4. bn show <id>                  Inspect an issue
  5. bn close <id> -r "done"      Close (idempotent)

  Multi-repo (auto-detect, no bn init needed):
  1. Set BN_DRIVER + BN_DSN (shared database)
  2. cd ~/repos/my-api && bn create "..."
     → auto-registers repo, prefix == slug derived from remote URL
  3. bn list / bn ready           Scoped to current repo by default
     bn list --all-repos          All repos in the shared database

SCRIPT-SAFE INIT / PROBE
  Git repo with auto-detect (may register the current repo context):
    if ! bn list --json --limit 1 >/dev/null 2>&1; then
      echo "beans issue filing unavailable; set BN_DRIVER and BN_DSN" >&2
      exit 0
    fi

  Explicit-prefix repo:
    if [ ! -f .bn ]; then
      bn init --prefix <slug> --non-interactive --quiet
    fi

  Use bn children <epic> or bn list --epic <epic> to inspect child tasks.

SKILL INTEGRATION
  A .bn file at the repo root tells skills to use bn instead of bd.
  bn init writes this file. Skills detect it via: [ -f ".bn" ] && TRACKER=bn

IMPORT
  bn import issues.jsonl              Seed from a bd-export JSONL file
  bn import --mode=merge issues.jsonl Update non-terminal fields

MEMORY
  bn remember "insight" --type feedback
  bn remember "global note" --global
  bn memories search terms
  bn memories --all-repos --json

EXIT CODES
  0    success
  1    error (error message on stderr, classifiable: not-found / validation / conflict)
`

// memJSON is the JSON shape for a memory (used by --json output).
type memJSON struct {
	ID        int64    `json:"id"`
	Prefix    *string  `json:"prefix"`
	Body      string   `json:"body"`
	Type      *string  `json:"type,omitempty"`
	Tags      []string `json:"tags"`
	CreatedAt string   `json:"created_at"`
}

func memoryToJSON(m store.Memory) memJSON {
	tags := m.Tags
	if tags == nil {
		tags = []string{}
	}
	return memJSON{
		ID:        m.ID,
		Prefix:    m.Prefix,
		Body:      m.Body,
		Type:      m.Type,
		Tags:      tags,
		CreatedAt: m.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
}
