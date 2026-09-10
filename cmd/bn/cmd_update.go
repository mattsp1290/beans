package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/internal/ops"
)

func newUpdateCmd(rs *appState) *cobra.Command {
	var in ops.UpdateInput
	var status, title, desc, typ, assignee, parent string
	var priority int
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Change fields of an issue (one commit, one log line per field)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fl := cmd.Flags()
			if fl.Changed("status") {
				in.Status = &status
			}
			if fl.Changed("title") {
				in.Title = &title
			}
			if fl.Changed("description") {
				in.Description = &desc
			}
			if fl.Changed("priority") {
				in.Priority = &priority
			}
			if fl.Changed("type") {
				in.Type = &typ
			}
			if fl.Changed("assignee") {
				in.Assignee = &assignee
			}
			if fl.Changed("parent") {
				in.Parent = &parent
			}
			if in.IsEmpty() {
				return errors.New("nothing to update; pass at least one field flag")
			}
			if err := rs.setupProject(false, true); err != nil {
				return err
			}
			res, err := rs.mutate(cmd.Context(), ops.Update(rs.opsEnv(), args[0], in))
			if err != nil {
				return err
			}
			return rs.printCommit(cmd, res, "updated "+args[0])
		},
	}
	fl := cmd.Flags()
	fl.BoolVar(&in.Claim, "claim", false, "set status in_progress and assign yourself")
	fl.StringVar(&status, "status", "", "new status")
	fl.StringVar(&title, "title", "", "new title (the file is not renamed)")
	fl.StringVar(&desc, "description", "", "replace the description")
	fl.IntVar(&priority, "priority", 2, "new priority 0-4")
	fl.StringVar(&typ, "type", "", "new type")
	fl.StringVar(&assignee, "assignee", "", "new assignee (empty clears)")
	fl.StringArrayVar(&in.AddLabels, "label", nil, "add a label (repeatable)")
	fl.StringArrayVar(&in.RemoveLabel, "unlabel", nil, "remove a label (repeatable)")
	fl.StringVar(&parent, "parent", "", "new parent id (empty clears)")
	fl.StringVar(&in.Note, "note", "", "append a log note")
	fl.BoolVar(&in.Force, "force", false, "allow leaving a terminal status")
	return cmd
}

func newNoteCmd(rs *appState) *cobra.Command {
	return &cobra.Command{
		Use:   "note <id> <text...>",
		Short: "Append a note to an issue's log",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.setupProject(false, true); err != nil {
				return err
			}
			res, err := rs.mutate(cmd.Context(), ops.Update(rs.opsEnv(), args[0], ops.UpdateInput{Note: strings.Join(args[1:], " ")}))
			if err != nil {
				return err
			}
			return rs.printCommit(cmd, res, "noted "+args[0])
		},
	}
}

func newCloseCmd(rs *appState) *cobra.Command {
	var reason string
	var force, suggest bool
	cmd := &cobra.Command{
		Use:   "close <id...>",
		Short: "Close issues with a reason (one commit per id)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(reason) == "" && !force {
				return errors.New("a reason is required: -r \"why\" (or --force)")
			}
			if err := rs.setupProject(false, true); err != nil {
				return err
			}
			var readyBefore map[string]bool
			if suggest {
				ix, err := rs.readIndex(cmd.Context())
				if err != nil {
					return err
				}
				readyBefore = map[string]bool{}
				for _, r := range ix.Ready("", true) {
					readyBefore[r.ID] = true
				}
			}
			var results []map[string]any
			for _, id := range args {
				res, err := rs.mutate(cmd.Context(), ops.Close(rs.opsEnv(), id, reason))
				if err != nil {
					return fmt.Errorf("%s: %w", id, err)
				}
				results = append(results, map[string]any{"id": id, "commit": res.SHA, "pushed": res.Pushed, "message": res.Message})
				if !rs.jsonOut {
					if res.SHA == "" {
						fmt.Fprintf(cmd.OutOrStdout(), "%s already closed\n", id)
					} else {
						fmt.Fprintf(cmd.OutOrStdout(), "closed %s (%s)\n", id, res.SHA[:7])
					}
				}
			}
			var unblocked []string
			if suggest {
				rs.idx = nil
				ix, err := rs.readIndex(cmd.Context())
				if err != nil {
					return err
				}
				for _, r := range ix.Ready("", true) {
					if !readyBefore[r.ID] {
						unblocked = append(unblocked, r.ID)
					}
				}
				if !rs.jsonOut && len(unblocked) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "now ready: %s\n", strings.Join(unblocked, ", "))
				}
			}
			if rs.jsonOut {
				return writeJSON(map[string]any{"closed": results, "now_ready": unblocked})
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&reason, "reason", "r", "", "why the issue is closed")
	cmd.Flags().BoolVar(&force, "force", false, "close without a reason")
	cmd.Flags().BoolVar(&suggest, "suggest-next", false, "print ids that became ready")
	return cmd
}

func newReopenCmd(rs *appState) *cobra.Command {
	return &cobra.Command{
		Use:   "reopen <id>",
		Short: "Return a closed issue to the default status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.setupProject(false, true); err != nil {
				return err
			}
			res, err := rs.mutate(cmd.Context(), ops.Reopen(rs.opsEnv(), args[0]))
			if err != nil {
				return err
			}
			return rs.printCommit(cmd, res, "reopened "+args[0])
		},
	}
}

func newDeleteCmd(rs *appState) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete an issue file (refuses when other notes link to it)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.setupProject(false, true); err != nil {
				return err
			}
			res, err := rs.mutate(cmd.Context(), ops.Delete(rs.opsEnv(), args[0], force))
			if err != nil {
				return err
			}
			return rs.printCommit(cmd, res, "deleted "+args[0])
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "also remove links from other issues")
	return cmd
}
