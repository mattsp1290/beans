package plan

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var nodeID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

func parseSummary(path, body string) (Summary, ChangeGraph, error) {
	lines := strings.Split(body, "\n")
	summary := -1
	inFence := false
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			inFence = !inFence
			continue
		}
		if !inFence && strings.TrimSpace(l) == "## Summary" {
			if summary >= 0 {
				return Summary{}, ChangeGraph{}, fmt.Errorf("%s: duplicate Summary", path)
			}
			summary = i
		}
	}
	if summary < 0 {
		return Summary{}, ChangeGraph{}, fmt.Errorf("%s: missing Summary", path)
	}
	ends := len(lines)
	for i := summary + 1; i < len(lines); i++ {
		if !inFence && strings.HasPrefix(lines[i], "## ") {
			ends = i
			break
		}
	}
	names := []string{"Outcome", "Affected areas", "Execution order", "Risks", "Change graph"}
	starts := make([]int, len(names))
	for i := range starts {
		starts[i] = -1
	}
	inFence = false
	for i := summary + 1; i < ends; i++ {
		l := lines[i]
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			inFence = !inFence
			continue
		}
		if !inFence && strings.HasPrefix(l, "### ") {
			n := strings.TrimSpace(strings.TrimPrefix(l, "### "))
			for j, w := range names {
				if n == w {
					if starts[j] >= 0 {
						return Summary{}, ChangeGraph{}, fmt.Errorf("%s: duplicate Summary subsection %q", path, n)
					}
					starts[j] = i
				}
			}
		}
	}
	for i, n := range starts {
		if n < 0 {
			return Summary{}, ChangeGraph{}, fmt.Errorf("%s: missing Summary subsection %q", path, n)
		}
		if i > 0 && n < starts[i-1] {
			return Summary{}, ChangeGraph{}, fmt.Errorf("%s: Summary subsections out of order", path)
		}
	}
	vals := make([]string, 5)
	for i, s := range starts {
		e := ends
		if i+1 < len(starts) {
			e = starts[i+1]
		}
		vals[i] = strings.TrimSpace(strings.Join(lines[s+1:e], "\n"))
	}
	graph, err := ParseGraph(path, vals[4])
	if err != nil {
		return Summary{}, ChangeGraph{}, err
	}
	return Summary{vals[0], vals[1], vals[2], vals[3], vals[4]}, graph, nil
}
func ParseGraph(path, text string) (ChangeGraph, error) {
	lines := strings.Split(text, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "```bn-change-graph" {
			if start >= 0 {
				return ChangeGraph{}, fmt.Errorf("%s: multiple bn-change-graph fences", path)
			}
			start = i
		}
	}
	if start < 0 {
		return ChangeGraph{}, fmt.Errorf("%s: missing bn-change-graph fence", path)
	}
	end := -1
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "```" {
			end = i
			break
		}
	}
	if end < 0 {
		return ChangeGraph{}, fmt.Errorf("%s: unterminated bn-change-graph fence", path)
	}
	for _, l := range append(lines[:start], lines[end+1:]...) {
		if strings.TrimSpace(l) != "" {
			return ChangeGraph{}, fmt.Errorf("%s: Change graph may only contain graph fence", path)
		}
	}
	var raw struct {
		Version int         `yaml:"version"`
		Nodes   []GraphNode `yaml:"nodes"`
		Edges   []GraphEdge `yaml:"edges"`
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(strings.Join(lines[start+1:end], "\n")), &doc); err != nil {
		return ChangeGraph{}, fmt.Errorf("%s: graph YAML: %w", path, err)
	}
	if len(doc.Content) != 1 || !allowedMapping(doc.Content[0], map[string]bool{"version": true, "nodes": true, "edges": true}) {
		return ChangeGraph{}, fmt.Errorf("%s: graph contains an unknown or invalid field", path)
	}
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "nodes" && root.Content[i+1].Kind == yaml.SequenceNode {
			for _, node := range root.Content[i+1].Content {
				if !allowedMapping(node, map[string]bool{"id": true, "label": true, "kind": true, "ref": true}) {
					return ChangeGraph{}, fmt.Errorf("%s: graph node contains an unknown field", path)
				}
			}
		}
		if root.Content[i].Value == "edges" && root.Content[i+1].Kind == yaml.SequenceNode {
			for _, edge := range root.Content[i+1].Content {
				if !allowedMapping(edge, map[string]bool{"from": true, "to": true, "kind": true, "label": true}) {
					return ChangeGraph{}, fmt.Errorf("%s: graph edge contains an unknown field", path)
				}
			}
		}
	}
	if err := yaml.Unmarshal([]byte(strings.Join(lines[start+1:end], "\n")), &raw); err != nil {
		return ChangeGraph{}, fmt.Errorf("%s: graph YAML: %w", path, err)
	}
	g := ChangeGraph(raw)
	if g.Version != 1 {
		return g, fmt.Errorf("%s: graph version must be 1", path)
	}
	if len(g.Nodes) > 200 || len(g.Edges) > 400 {
		return g, fmt.Errorf("%s: graph exceeds size limit", path)
	}
	nodes := map[string]bool{}
	for _, n := range g.Nodes {
		if !nodeID.MatchString(n.ID) || strings.TrimSpace(n.Label) == "" || strings.Contains(n.Label, "\n") || len(n.Label) > 120 || !oneOf(n.Kind, "artifact", "component", "interface", "data", "workflow", "external") {
			return g, fmt.Errorf("%s: invalid graph node %q", path, n.ID)
		}
		if nodes[n.ID] {
			return g, fmt.Errorf("%s: duplicate graph node %q", path, n.ID)
		}
		nodes[n.ID] = true
	}
	seen := map[string]bool{}
	for _, e := range g.Edges {
		k := e.From + "\x00" + e.To + "\x00" + e.Kind + "\x00" + e.Label
		if !nodes[e.From] || !nodes[e.To] || !oneOf(e.Kind, "precedes", "affects", "enables", "produces", "replaces", "contains") || seen[k] {
			return g, fmt.Errorf("%s: invalid graph edge", path)
		}
		seen[k] = true
	}
	return g, nil
}

func allowedMapping(node *yaml.Node, allowed map[string]bool) bool {
	if node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Kind != yaml.ScalarNode || !allowed[node.Content[i].Value] {
			return false
		}
	}
	return true
}
func oneOf(s string, xs ...string) bool {
	for _, x := range xs {
		if s == x {
			return true
		}
	}
	return false
}
