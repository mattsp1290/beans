// Command workflow_oracle exposes the canonical Beans workflow loader.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/mattsp1290/beans/issue"
)

func read(path string) ([]byte, error) {
	if path == "" {
		return nil, nil
	}
	return os.ReadFile(path)
}

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: workflow_oracle <explicit> <project> <hub>")
		os.Exit(2)
	}
	project, err := read(os.Args[2])
	if err != nil {
		panic(err)
	}
	hub, err := read(os.Args[3])
	if err != nil {
		panic(err)
	}
	wf, err := issue.LoadWorkflow(os.Args[1], project, hub)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if wf.Transitions == nil {
		wf.Transitions = map[string][]string{}
	}
	out := map[string]any{"statuses": wf.Statuses, "default": wf.Default, "active": wf.Active, "terminal": wf.Terminal, "transitions": wf.Transitions}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		panic(err)
	}
}
