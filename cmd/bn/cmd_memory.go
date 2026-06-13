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
		Short: "Persist a memory to Postgres",
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
		Long: `Search memories using full-text search (Postgres tsvector/plainto_tsquery).
Without keywords, returns recent memories ordered by created_at DESC.

Search quality differs from bd's matcher — bn uses Postgres FTS (English stemming,
stop words). Exact-keyword recall may differ; use quotes or --type/--tag to narrow.`,
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

	cmd.Flags().BoolVar(&all, "all", false, "search across all projects")
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
bn — Postgres-backed issue tracker

ENVIRONMENT
  BN_DSN      Postgres connection string (required; never pass on argv)
  BN_PROJECT  Default project prefix
  BN_ACTOR    Default actor for audit notes

WORKFLOW
  1. bn init --prefix=<proj>     Register a project (creates .bn marker)
  2. bn repo admin add --bootstrap <actor>
                                  Bootstrap repo registry admin
  3. bn repo add <slug> --remote <url> --auth <auth-ref>
                                  Onboard repositories for workspace routing
  4. bn create "Task title"       Create issues (auto-registers project)
     bn dep add <child> <parent>  Wire dependencies
  5. bn ready                     List unblocked, open issues
  6. bn show <id>                  Inspect an issue
  7. bn close <id> -r "done"      Close (idempotent)

SKILL INTEGRATION
  A .bn file at the repo root tells skills to use bn instead of bd.
  bn init writes this file. Skills detect it via: [ -f ".bn" ] && TRACKER=bn

IMPORT
  bn import issues.jsonl              Seed from a bd-export JSONL file
  bn import --mode=merge issues.jsonl Update non-terminal fields

MEMORY
  bn remember "insight" --type feedback
  bn memories search terms
  bn memories --all --json

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
