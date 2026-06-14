package dto

import appstore "github.com/mattsp1290/bean-counter/internal/store"

type AddDependencyRequest struct {
	BlockedByID string `json:"blocked_by_id"`
}

type Dependency struct {
	IssueID     string `json:"issue_id"`
	BlockedByID string `json:"blocked_by_id"`
}

type DependencyListResponse struct {
	Dependencies []Dependency `json:"dependencies"`
}

func DependencyFromStore(dep appstore.DepEdge) Dependency {
	return Dependency{
		IssueID:     dep.IssueID,
		BlockedByID: dep.BlockedByID,
	}
}

func DependenciesFromStore(deps []appstore.DepEdge) []Dependency {
	result := make([]Dependency, 0, len(deps))
	for _, dep := range deps {
		result = append(result, DependencyFromStore(dep))
	}
	return result
}
