package dto

import appstore "github.com/mattsp1290/bean-counter/internal/store"

type ReadyResponse struct {
	Issues []Issue `json:"issues"`
}

func ReadyResponseFromStore(issues []appstore.Issue) ReadyResponse {
	return ReadyResponse{Issues: IssuesFromStore(issues)}
}
