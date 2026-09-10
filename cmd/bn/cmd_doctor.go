package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/issue"
)

type doctorFinding struct {
	Kind    string `json:"kind"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

func newDoctorCmd(rs *appState) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check the hub for problems (exit 1 when any)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rs.setupProject(false, true); err != nil {
				return err
			}
			var findings []doctorFinding
			if err := rs.hub.Preflight(cmd.Context()); err != nil {
				findings = append(findings, doctorFinding{Kind: "git", Message: err.Error()})
			}
			if _, err := os.Stat(rs.hub.JournalPath()); err == nil {
				findings = append(findings, doctorFinding{Kind: "journal", Path: rs.hub.JournalPath(), Message: "an operation was interrupted; the next bn command commits its partial changes as a recovered partial"})
			}
			_ = filepath.WalkDir(rs.paths.Hub, func(p string, d os.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if d.IsDir() && d.Name() == ".git" {
					return filepath.SkipDir
				}
				if !d.IsDir() && strings.HasSuffix(d.Name(), ".tmp") {
					rel, _ := filepath.Rel(rs.paths.Hub, p)
					findings = append(findings, doctorFinding{Kind: "tmp", Path: filepath.ToSlash(rel), Message: "leftover temporary file; the next mutation deletes it"})
				}
				return nil
			})
			ix, err := rs.readIndex(cmd.Context())
			if err != nil {
				return err
			}
			project := rs.projectScope(all)
			for _, w := range ix.Warnings {
				if project != "" && !strings.HasPrefix(w.Path, "projects/"+project+"/") && strings.HasPrefix(w.Path, "projects/") {
					continue
				}
				findings = append(findings, doctorFinding{Kind: "index", Path: w.Path, Message: w.Err.Error()})
			}
			for _, iss := range ix.Issues {
				if project != "" && iss.Project != project {
					continue
				}
				base := noteBasename(iss)
				if base != iss.ID && !strings.HasPrefix(base, iss.ID+"-") {
					findings = append(findings, doctorFinding{Kind: "filename", Path: iss.Path, Message: fmt.Sprintf("filename does not start with id %s", iss.ID)})
				}
				if !issue.ValidID(iss.ID) {
					findings = append(findings, doctorFinding{Kind: "id", Path: iss.Path, Message: fmt.Sprintf("id %q does not match the grammar", iss.ID)})
				}
			}
			for _, c := range ix.Cycles() {
				findings = append(findings, doctorFinding{Kind: "cycle", Message: strings.Join(c, " → ")})
			}
			if rs.jsonOut {
				if findings == nil {
					findings = []doctorFinding{}
				}
				if err := writeJSON(findings); err != nil {
					return err
				}
			} else if len(findings) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "hub is healthy")
			} else {
				w := tableWriter(cmd)
				for _, f := range findings {
					fmt.Fprintf(w, "%s\t%s\t%s\n", f.Kind, f.Path, f.Message)
				}
				_ = w.Flush()
			}
			if len(findings) > 0 {
				return &codedError{code: exitUsage, msg: fmt.Sprintf("%d problem(s) found", len(findings))}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all-projects", false, "every project in the hub")
	return cmd
}
