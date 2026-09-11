package issue

import (
	"embed"
	"os"
	"path/filepath"
)

//go:embed templates/*.md
var templateFS embed.FS

// DefaultTemplate returns the built-in body template for an issue type. Unknown
// types get the task template. A template is a body only, no frontmatter.
func DefaultTemplate(typ string) string {
	data, err := templateFS.ReadFile("templates/" + typ + ".md")
	if err != nil {
		data, _ = templateFS.ReadFile("templates/task.md")
	}
	return string(data)
}

// LoadTemplate returns the body template for typ: projectDir/templates/<typ>.md,
// then hubDir/templates/<typ>.md, then the built-in default. Either directory
// may be empty to skip it.
func LoadTemplate(typ, projectDir, hubDir string) string {
	for _, dir := range []string{projectDir, hubDir} {
		if dir == "" {
			continue
		}
		if data, err := os.ReadFile(filepath.Join(dir, "templates", typ+".md")); err == nil {
			return string(data)
		}
	}
	return DefaultTemplate(typ)
}

// DefaultRequestTemplate returns the built-in body template for a request.
func DefaultRequestTemplate() string {
	data, _ := templateFS.ReadFile("templates/request.md")
	return string(data)
}

// LoadRequestTemplate returns the request body template from the target
// project, then the hub, then the built-in default.
func LoadRequestTemplate(projectDir, hubDir string) string {
	for _, dir := range []string{projectDir, hubDir} {
		if dir == "" {
			continue
		}
		if data, err := os.ReadFile(filepath.Join(dir, "templates", "request.md")); err == nil {
			return string(data)
		}
	}
	return DefaultRequestTemplate()
}
