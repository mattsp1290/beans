package issue

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Request is a project-scoped request artifact. Its canonical issue links are
// owned solely by the request file; callers must derive reverse links.
type Request struct {
	ID          string
	Title       string
	Status      string
	Priority    int
	Labels      []string
	RequestedBy string
	Issues      []Link
	Created     time.Time
	Updated     time.Time
	Aliases     []string
	Extra       yaml.Node
	Body        string
	Log         []LogEntry
	Path        string
	Project     string

	orig *requestOriginal
}

const (
	RequestOpen       = "open"
	RequestAccepted   = "accepted"
	RequestInProgress = "in_progress"
	RequestResolved   = "resolved"
	RequestDeclined   = "declined"
)

var requestKeys = []string{"id", "aliases", "title", "status", "priority", "labels", "requested_by", "issues", "created", "updated"}

var requestKeySet = func() map[string]bool {
	m := make(map[string]bool, len(requestKeys))
	for _, key := range requestKeys {
		m[key] = true
	}
	return m
}()

var requestIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*-r-[a-z0-9]+(\.[0-9]+)*$`)

// ValidRequestID reports whether id is a valid issue-compatible request id.
func ValidRequestID(id string) bool { return ValidID(id) && requestIDRe.MatchString(id) }

// NewRequestID generates an id in the request-specific namespace.
func NewRequestID(prefix string, exists func(string) bool, length int) string {
	return NewID(prefix+"-r", exists, length)
}

// ValidRequestStatus reports whether status belongs to the fixed request workflow.
func ValidRequestStatus(status string) bool {
	switch status {
	case RequestOpen, RequestAccepted, RequestInProgress, RequestResolved, RequestDeclined:
		return true
	}
	return false
}

// ValidateRequestTransition checks an ordinary request lifecycle transition.
// Same-status changes are deliberately idempotent.
func ValidateRequestTransition(from, to string) error {
	if !ValidRequestStatus(from) || !ValidRequestStatus(to) {
		return fmt.Errorf("invalid request status transition %q -> %q", from, to)
	}
	if from == to {
		return nil
	}
	allowed := (from == RequestOpen && (to == RequestAccepted || to == RequestDeclined)) ||
		(from == RequestAccepted && (to == RequestInProgress || to == RequestDeclined)) ||
		(from == RequestInProgress && (to == RequestResolved || to == RequestDeclined))
	if !allowed {
		return fmt.Errorf("invalid request status transition %q -> %q", from, to)
	}
	return nil
}

type requestSnapshot struct {
	id, title, status, requestedBy string
	priority                       int
	labels, aliases                []string
	issues                         []Link
	created, updated               time.Time
	body                           string
}

type requestOriginal struct {
	fmLines []string
	spans   []keySpan
	rawLog  string
	tail    string
	logLen  int
	snap    requestSnapshot
}

func (r *Request) snapshot() requestSnapshot {
	return requestSnapshot{
		id: r.ID, title: r.Title, status: r.Status, priority: r.Priority,
		requestedBy: r.RequestedBy, labels: append([]string(nil), r.Labels...),
		aliases: append([]string(nil), r.Aliases...), issues: append([]Link(nil), r.Issues...),
		created: r.Created, updated: r.Updated, body: r.Body,
	}
}

// requestPathInfo validates and derives the project for a request path.
func requestPathInfo(path string) (string, error) {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(path)), "/")
	for i := 0; i+3 < len(parts); i++ {
		if parts[i] == "projects" && parts[i+1] != "" && parts[i+2] == "requests" && i+4 == len(parts) {
			if filepath.Ext(parts[i+3]) != ".md" || strings.TrimSuffix(parts[i+3], ".md") == "" {
				break
			}
			return parts[i+1], nil
		}
	}
	return "", fmt.Errorf("%s: request path must be projects/<project>/requests/<id>-<slug>.md", path)
}
