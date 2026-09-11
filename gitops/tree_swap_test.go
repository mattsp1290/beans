package gitops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceTreeReplacesCompleteContents(t *testing.T) {
	target := filepath.Join(t.TempDir(), "bundle")
	if err := os.MkdirAll(filepath.Join(target, "sections"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "old.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "sections", "gone.md"), []byte("gone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceTree(target, map[string][]byte{"plan.md": []byte("new\n"), "sections/kept.md": []byte("kept\n")}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "old.md")); !os.IsNotExist(err) {
		t.Fatalf("old file survived: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(target, "sections", "kept.md"))
	if err != nil || string(data) != "kept\n" {
		t.Fatalf("replacement = %q, %v", data, err)
	}
}

func TestReplaceTreeRejectsEscapingPath(t *testing.T) {
	if err := ReplaceTree(filepath.Join(t.TempDir(), "bundle"), map[string][]byte{"../escape": []byte("no")}); err == nil {
		t.Fatal("accepted escaping path")
	}
}

func TestReplaceTreeRecoversInterruptedBackup(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "bundle")
	backup := filepath.Join(parent, ".bundle.backup")
	if err := os.MkdirAll(backup, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "old.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceTree(target, map[string][]byte{"plan.md": []byte("new\n")}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "plan.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("backup remains: %v", err)
	}
}
