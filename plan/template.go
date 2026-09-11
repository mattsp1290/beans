package plan

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed templates/plan.md
var scaffold string

// Scaffold produces the initial valid draft body with a frozen id and slug.
func Scaffold(id, title string, now time.Time) []byte {
	body := scaffold
	if end := strings.Index(body[4:], "\n---\n"); end >= 0 {
		body = body[end+9:]
	}
	data, err := Encode(&Plan{ID: id, Aliases: []string{id}, Title: title, Slug: Slug(title), Status: StatusDraft, Created: now, Updated: now, Body: body})
	if err != nil {
		panic(err)
	}
	return data
}

// WriteScaffold creates an empty destination containing a draft bundle.
func WriteScaffold(dir, id, title string, now time.Time) error {
	if err := os.Mkdir(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), Scaffold(id, title, now), 0o644); err != nil {
		return fmt.Errorf("write scaffold: %w", err)
	}
	return nil
}
