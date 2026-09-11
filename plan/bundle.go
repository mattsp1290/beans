package plan

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	MaxBundleSize int64 = 2 << 20
	MaxFileSize   int64 = 512 << 10
)

// Load reads only regular, non-symlinked files contained in root.
func Load(root string) (*Bundle, error) {
	st, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s: bundle root must be a directory", root)
	}
	files, err := capture(root)
	if err != nil {
		return nil, err
	}
	return LoadSnapshot(root, BundleSnapshot{Files: files})
}
func capture(root string) (map[string][]byte, error) {
	files := map[string][]byte{}
	var total int64
	err := filepath.WalkDir(root, func(full string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if full == root {
			return nil
		}
		rel, e := filepath.Rel(root, full)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s: symlinks are not allowed", rel)
		}
		if d.IsDir() {
			if rel != "sections" {
				return fmt.Errorf("%s: unexpected directory", rel)
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s: non-regular file", rel)
		}
		if rel != "plan.md" && !strings.HasPrefix(rel, "sections/") {
			return fmt.Errorf("%s: unexpected file", rel)
		}
		if !strings.HasSuffix(rel, ".md") {
			return fmt.Errorf("%s: only Markdown files are allowed", rel)
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if info.Size() > MaxFileSize {
			return fmt.Errorf("%s: file exceeds 512KiB", rel)
		}
		total += info.Size()
		if total > MaxBundleSize {
			return fmt.Errorf("bundle exceeds 2MiB")
		}
		b, e := os.ReadFile(full)
		if e != nil {
			return e
		}
		files[rel] = b
		return nil
	})
	return files, err
}
func LoadSnapshot(root string, snap BundleSnapshot) (*Bundle, error) {
	data, ok := snap.Files["plan.md"]
	if !ok {
		return nil, fmt.Errorf("%s: missing plan.md", root)
	}
	if len(data) > int(MaxFileSize) {
		return nil, fmt.Errorf("plan.md: file exceeds 512KiB")
	}
	var total int
	for name, b := range snap.Files {
		if !validFileName(name) {
			return nil, fmt.Errorf("%s: invalid bundle file", name)
		}
		total += len(b)
		if len(b) > int(MaxFileSize) {
			return nil, fmt.Errorf("%s: file exceeds 512KiB", name)
		}
	}
	if total > int(MaxBundleSize) {
		return nil, fmt.Errorf("bundle exceeds 2MiB")
	}
	p, err := Parse(path.Join(root, "plan.md"), data)
	if err != nil {
		return nil, err
	}
	if !ValidID(projectPrefix(p.ID), p.ID) {
		return nil, fmt.Errorf("plan.md: invalid plan id %q", p.ID)
	}
	seen := map[string]bool{}
	sections := make([]Section, 0, len(p.Sections))
	for _, name := range p.Sections {
		if !validSectionPath(name) || seen[name] {
			return nil, fmt.Errorf("plan.md: invalid or duplicate section %q", name)
		}
		seen[name] = true
		b, ok := snap.Files[name]
		if !ok {
			return nil, fmt.Errorf("%s: listed section missing", name)
		}
		if err := validText(name, b); err != nil {
			return nil, err
		}
		sections = append(sections, Section{Path: name, Markdown: string(b)})
	}
	for name := range snap.Files {
		if name == "plan.md" {
			continue
		}
		if !seen[name] {
			return nil, fmt.Errorf("%s: section is not listed", name)
		}
	}
	if err := Validate(p); err != nil {
		return nil, err
	}
	return &Bundle{Plan: p, Sections: sections, Root: root}, nil
}
func projectPrefix(id string) string { return strings.Split(id, "-plan-")[0] }
func validFileName(s string) bool    { return s == "plan.md" || validSectionPath(s) }
func validSectionPath(s string) bool {
	return strings.HasPrefix(s, "sections/") && !strings.Contains(s, "\\") && !strings.Contains(s, "..") && strings.Count(s, "/") == 1 && strings.HasSuffix(s, ".md") && len(strings.TrimSuffix(strings.TrimPrefix(s, "sections/"), ".md")) > 0
}
func validText(name string, b []byte) error {
	if bytes.IndexByte(b, 0) >= 0 || !utf8.Valid(b) || bytes.Contains(b, []byte("\r")) || len(b) == 0 || b[len(b)-1] != '\n' {
		return fmt.Errorf("%s: must be UTF-8 LF text ending in newline", name)
	}
	return nil
}
func (b *Bundle) Snapshot() BundleSnapshot {
	out := map[string][]byte{}
	data, _ := Encode(b.Plan)
	out["plan.md"] = data
	for _, s := range b.Sections {
		out[s.Path] = []byte(s.Markdown)
	}
	return BundleSnapshot{Files: out}
}
func (s BundleSnapshot) Paths() []string {
	xs := make([]string, 0, len(s.Files))
	for k := range s.Files {
		xs = append(xs, k)
	}
	sort.Strings(xs)
	return xs
}
