package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// generateBenchHub writes a hub with n issues spread evenly across the given
// projects, each with a handful of labels and a parent link to spread out
// backlink/alias work, so the benchmark reflects realistic Load cost.
func generateBenchHub(tb testing.TB, dir string, projects []string, n int) {
	tb.Helper()
	for _, p := range projects {
		dir := filepath.Join(dir, "projects", p, "issues")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			tb.Fatalf("mkdir: %v", err)
		}
		toml := fmt.Sprintf("name = %q\nprefix = %q\nremotes = []\n", p, p)
		if err := os.WriteFile(filepath.Join(dir, "..", "beans.toml"), []byte(toml), 0o644); err != nil {
			tb.Fatalf("write beans.toml: %v", err)
		}
	}

	statuses := []string{"open", "in_progress", "closed"}
	for i := 0; i < n; i++ {
		p := projects[i%len(projects)]
		id := fmt.Sprintf("%s-i%05d", p, i/len(projects))
		var parent string
		if i >= len(projects) {
			parentProject := projects[(i+1)%len(projects)]
			parentIdx := (i - len(projects)) / len(projects)
			parent = fmt.Sprintf("parent: \"[[%s-i%05d]]\"\n", parentProject, parentIdx)
		}
		content := fmt.Sprintf(`---
id: %s
title: "Generated issue %d"
type: task
status: %s
priority: %d
labels: [bench, gen]
%screated: 2026-01-01T00:00:00Z
updated: 2026-01-01T00:00:00Z
---
Generated body for issue %d with a link to [[%s]].
`, id, i, statuses[i%len(statuses)], i%4, parent, i, id)
		path := filepath.Join(dir, "projects", p, "issues", id+".md")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			tb.Fatalf("write issue: %v", err)
		}
	}
}

func BenchmarkLoad(b *testing.B) {
	dir := b.TempDir()
	projects := []string{"p1", "p2", "p3", "p4", "p5"}
	generateBenchHub(b, dir, projects, 5000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ix, err := Load(dir)
		if err != nil {
			b.Fatalf("Load: %v", err)
		}
		if len(ix.Issues) != 5000 {
			b.Fatalf("expected 5000 issues, got %d", len(ix.Issues))
		}
	}
}
