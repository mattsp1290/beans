package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/internal/ops"
	"github.com/mattsp1290/beans/issue"
	"github.com/mattsp1290/beans/vault"
)

func newRememberCmd(rs *appState) *cobra.Command {
	var in ops.MemoryInput
	cmd := &cobra.Command{
		Use:   "remember <text...>",
		Short: "Save a memory as memories/<key>.md",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			in.Body = strings.Join(args, " ")
			if err := rs.setupProject(!in.Global, in.Global); err != nil {
				return err
			}
			op, key := ops.Remember(rs.opsEnv(), in)
			res, err := rs.mutate(cmd.Context(), op)
			if err != nil {
				return err
			}
			if rs.jsonOut {
				return writeJSON(map[string]any{"key": *key, "commit": res.SHA, "pushed": res.Pushed, "message": res.Message})
			}
			return rs.printCommit(cmd, res, "remembered "+*key)
		},
	}
	cmd.Flags().StringVar(&in.Key, "key", "", "memory key (default: derived from the first words)")
	cmd.Flags().StringVar(&in.Type, "type", "", "user, feedback, project, or reference")
	cmd.Flags().StringArrayVar(&in.Tags, "tag", nil, "tag (repeatable)")
	cmd.Flags().BoolVar(&in.Global, "global", false, "hub-wide memory instead of the project's")
	return cmd
}

type memoryJSON struct {
	Key     string   `json:"key"`
	Type    string   `json:"type"`
	Tags    []string `json:"tags"`
	Project string   `json:"project"`
	Path    string   `json:"path"`
	Body    string   `json:"body"`
}

func newMemoriesCmd(rs *appState) *cobra.Command {
	var typ, tag string
	var all bool
	var limit int
	cmd := &cobra.Command{
		Use:   "memories [keyword...]",
		Short: "List memories, optionally filtered by keyword",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.setupProject(false, true); err != nil {
				return err
			}
			ix, err := rs.readIndex(cmd.Context())
			if err != nil {
				return err
			}
			project := rs.projectScope(all)
			var out []memoryJSON
			for _, n := range ix.Notes {
				if n.Kind != vault.KindMemory || n.Memory == nil {
					continue
				}
				m := n.Memory
				if project != "" && n.Project != "" && n.Project != project {
					continue
				}
				if typ != "" && m.Type != typ {
					continue
				}
				if tag != "" && !containsString(m.Tags, tag) {
					continue
				}
				if !matchesKeywords(m, args) {
					continue
				}
				tags := m.Tags
				if tags == nil {
					tags = []string{}
				}
				out = append(out, memoryJSON{Key: m.Key, Type: m.Type, Tags: tags, Project: n.Project, Path: n.Path, Body: m.Body})
			}
			sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
			if limit > 0 && len(out) > limit {
				out = out[:limit]
			}
			if rs.jsonOut {
				if out == nil {
					out = []memoryJSON{}
				}
				return writeJSON(out)
			}
			for _, m := range out {
				scope := m.Project
				if scope == "" {
					scope = "hub"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s (%s", m.Key, scope)
				if m.Type != "" {
					fmt.Fprintf(cmd.OutOrStdout(), ", %s", m.Type)
				}
				fmt.Fprintf(cmd.OutOrStdout(), ")\n  %s\n", strings.ReplaceAll(strings.TrimSpace(m.Body), "\n", "\n  "))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&typ, "type", "", "only this memory type")
	cmd.Flags().StringVar(&tag, "tag", "", "only memories with this tag")
	cmd.Flags().BoolVar(&all, "all", false, "hub-wide, not just the current project")
	cmd.Flags().IntVarP(&limit, "limit", "n", 0, "maximum entries (0 = all)")
	return cmd
}

func matchesKeywords(m *issue.Memory, words []string) bool {
	if len(words) == 0 {
		return true
	}
	hay := strings.ToLower(m.Key + " " + m.Body + " " + strings.Join(m.Tags, " "))
	for _, w := range words {
		if !strings.Contains(hay, strings.ToLower(w)) {
			return false
		}
	}
	return true
}

func newForgetCmd(rs *appState) *cobra.Command {
	var global bool
	cmd := &cobra.Command{
		Use:   "forget <key>",
		Short: "Delete a memory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.setupProject(false, global); err != nil {
				return err
			}
			res, err := rs.mutate(cmd.Context(), ops.Forget(rs.opsEnv(), args[0], global))
			if err != nil {
				return err
			}
			return rs.printCommit(cmd, res, "forgot "+args[0])
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "hub-wide memory instead of the project's")
	return cmd
}
