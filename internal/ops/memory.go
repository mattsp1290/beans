package ops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattsp1290/beans/gitops"
	"github.com/mattsp1290/beans/issue"
)

// MemoryInput is what bn remember accepts.
type MemoryInput struct {
	Key    string // derived from the body when empty
	Type   string
	Tags   []string
	Body   string
	Global bool // hub-level memories/ instead of the project's
}

// MemoryKey derives a key from the first six words of the body.
func MemoryKey(body string) string {
	words := strings.Fields(body)
	if len(words) > 6 {
		words = words[:6]
	}
	key := issue.Slug(strings.Join(words, " "))
	if len(key) > issue.MemoryKeyMaxLen {
		key = strings.Trim(key[:issue.MemoryKeyMaxLen], "-")
	}
	return key
}

func memoryRel(env Env, key string, global bool) string {
	if global || env.Project == "" {
		return "memories/" + key + ".md"
	}
	return filepath.ToSlash(filepath.Join("projects", env.Project, "memories", key+".md"))
}

// Remember writes or replaces a memory file. An existing file keeps its
// created time and user-owned keys; only the bn-owned fields and body change.
func Remember(env Env, in MemoryInput) (gitops.Operation, *string) {
	key := strings.TrimSpace(in.Key)
	if key == "" {
		key = MemoryKey(in.Body)
	}
	rel := memoryRel(env, key, in.Global)
	return gitops.Operation{Verb: "remember", ID: key, Apply: func(hubDir string) ([]string, error) {
		if !issue.ValidMemoryKey(key) {
			return nil, fmt.Errorf("invalid memory key %q (use [a-z0-9-], at most %d characters)", key, issue.MemoryKeyMaxLen)
		}
		if !issue.ValidMemoryType(in.Type) {
			return nil, fmt.Errorf("invalid memory type %q (user, feedback, project, reference)", in.Type)
		}
		if strings.TrimSpace(in.Body) == "" {
			return nil, errors.New("memory body is required")
		}
		var paths []string
		if !in.Global && env.Project != "" {
			created, err := ensureProject(hubDir, env.Project, "")
			if err != nil {
				return nil, err
			}
			paths = created
		}
		path := filepath.Join(hubDir, filepath.FromSlash(rel))
		body := strings.TrimRight(in.Body, "\n") + "\n"
		now := env.now()
		var m *issue.Memory
		if data, err := os.ReadFile(path); err == nil {
			cur, err := issue.ParseMemory(rel, data)
			if err != nil {
				return nil, err
			}
			if cur.Body == body && cur.Type == in.Type && equalStrings(cur.Tags, cleanList(in.Tags)) {
				return nil, nil
			}
			cur.Body = body
			cur.Type = in.Type
			cur.Tags = cleanList(in.Tags)
			cur.Updated = now
			m = cur
		} else {
			m = &issue.Memory{Key: key, Type: in.Type, Tags: cleanList(in.Tags), Created: now, Updated: now, Body: body}
		}
		data, err := issue.EncodeMemory(m)
		if err != nil {
			return nil, err
		}
		if err := gitops.WriteFile(path, data); err != nil {
			return nil, err
		}
		return append(paths, rel), nil
	}}, &key
}

// Forget deletes a memory file.
func Forget(env Env, key string, global bool) gitops.Operation {
	rel := memoryRel(env, key, global)
	return gitops.Operation{Verb: "forget", ID: key, Apply: func(hubDir string) ([]string, error) {
		path := filepath.Join(hubDir, filepath.FromSlash(rel))
		if err := os.Remove(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("%w: memory %s", ErrNotFound, key)
			}
			return nil, err
		}
		return []string{rel}, nil
	}}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// DocNew creates docs/<path>.md from templates/doc.md (project, then hub)
// or a title line. It refuses to overwrite.
func DocNew(env Env, docPath string, global bool) (gitops.Operation, string) {
	docPath = strings.TrimSuffix(strings.Trim(filepath.ToSlash(docPath), "/"), ".md")
	rel := "docs/" + docPath + ".md"
	if !global && env.Project != "" {
		rel = filepath.ToSlash(filepath.Join("projects", env.Project, rel))
	}
	return gitops.Operation{Verb: "doc new", ID: docPath, Apply: func(hubDir string) ([]string, error) {
		if docPath == "" || strings.Contains(docPath, "..") {
			return nil, fmt.Errorf("invalid doc path %q", docPath)
		}
		path := filepath.Join(hubDir, filepath.FromSlash(rel))
		if _, err := os.Stat(path); err == nil {
			return nil, nil // replay, or already there
		}
		var paths []string
		if !global && env.Project != "" {
			created, err := ensureProject(hubDir, env.Project, "")
			if err != nil {
				return nil, err
			}
			paths = created
		}
		body := ""
		for _, dir := range []string{filepath.Join(hubDir, "projects", env.Project), hubDir} {
			if data, err := os.ReadFile(filepath.Join(dir, "templates", "doc.md")); err == nil {
				body = string(data)
				break
			}
		}
		if body == "" {
			title := strings.ReplaceAll(filepath.Base(docPath), "-", " ")
			body = "# " + strings.ToUpper(title[:1]) + title[1:] + "\n"
		}
		if err := gitops.WriteFile(path, []byte(body)); err != nil {
			return nil, err
		}
		return append(paths, rel), nil
	}}, rel
}
