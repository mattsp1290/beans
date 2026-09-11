package plan

import (
	"fmt"
	"regexp"
	"strings"
)

// Validate applies structural and status-specific lifecycle requirements.
func Validate(p *Plan) error {
	if p == nil {
		return fmt.Errorf("nil plan")
	}
	if !p.Status.Valid() {
		return fmt.Errorf("invalid plan status")
	}
	if p.Status == StatusDraft {
		return nil
	}
	meaningful := func(s string) bool {
		s = strings.ReplaceAll(s, "<!-- bn:todo -->", "")
		s = regexp.MustCompile(`<!--[\s\S]*?-->`).ReplaceAllString(s, "")
		return strings.TrimSpace(s) != ""
	}
	if !meaningful(p.Summary.Outcome) {
		return fmt.Errorf("Summary Outcome must contain meaningful text")
	}
	risks := listItems(p.Summary.Risks)
	if p.Status == StatusBlocked {
		for _, r := range risks {
			if strings.HasPrefix(strings.TrimSpace(r), "BLOCKER:") {
				return nil
			}
		}
		return fmt.Errorf("blocked plans require a BLOCKER: Risks item")
	}
	if strings.Contains(p.Summary.Outcome, "<!-- bn:todo -->") || strings.Contains(p.Summary.AffectedAreas, "<!-- bn:todo -->") || strings.Contains(p.Summary.ExecutionOrder, "<!-- bn:todo -->") || strings.Contains(p.Summary.Risks, "<!-- bn:todo -->") {
		return fmt.Errorf("ready plans cannot contain bn:todo markers")
	}
	if len(listItems(p.Summary.AffectedAreas)) == 0 {
		return fmt.Errorf("ready plans require Affected areas list")
	}
	if !orderedItems(p.Summary.ExecutionOrder) {
		return fmt.Errorf("ready plans require ordered Execution order")
	}
	if len(risks) == 0 {
		return fmt.Errorf("ready plans require Risks list")
	}
	if len(p.Graph.Nodes) == 0 {
		return fmt.Errorf("ready plans require a graph node")
	}
	return nil
}
func listItems(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "- ") || strings.HasPrefix(l, "* ") {
			out = append(out, l[2:])
		}
	}
	return out
}
func orderedItems(s string) bool { return regexp.MustCompile(`(?m)^\s*\d+\.\s+\S`).MatchString(s) }
