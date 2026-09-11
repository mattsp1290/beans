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
	replacements := map[string]string{
		"{{ .ID }}": id, "{{ .Title }}": title, "{{ .Slug }}": Slug(title),
		"{{ .Created }}": now.UTC().Format(time.RFC3339), "{{ .Updated }}": now.UTC().Format(time.RFC3339),
	}
	s := scaffold
	for old, new := range replacements {
		s = strings.ReplaceAll(s, old, new)
	}
	return []byte(s)
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
