package dto

import appstore "github.com/mattsp1290/bean-counter/internal/store"

type GraphResponse struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

type GraphNode struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	State    string   `json:"state"`
	Priority int      `json:"priority"`
	Labels   []string `json:"labels"`
}

type GraphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

func GraphResponseFromStore(issues []appstore.Issue, deps []appstore.DepEdge) GraphResponse {
	nodes := make([]GraphNode, 0, len(issues))
	for _, issue := range issues {
		nodes = append(nodes, GraphNode{
			ID:       issue.ID,
			Title:    issue.Title,
			State:    string(issue.State),
			Priority: int(issue.Priority),
			Labels:   copyStringSlice(issue.Labels),
		})
	}

	edges := make([]GraphEdge, 0, len(deps))
	for _, dep := range deps {
		edges = append(edges, GraphEdge{
			Source: dep.BlockedByID,
			Target: dep.IssueID,
		})
	}

	return GraphResponse{Nodes: nodes, Edges: edges}
}
