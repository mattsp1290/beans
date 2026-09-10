package vault

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// copyFixtureHub copies vault/testdata/hub into a fresh temp directory so
// tests that mutate files on disk (Reload) never touch the checked-in
// fixture.
func copyFixtureHub(t *testing.T) string {
	t.Helper()
	src := filepath.Join("testdata", "hub")
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy fixture hub: %v", err)
	}
	return dst
}

func loadFixture(t *testing.T) (*Index, string) {
	t.Helper()
	dir := copyFixtureHub(t)
	ix, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return ix, dir
}

func TestLoadWarnings(t *testing.T) {
	ix, _ := loadFixture(t)
	if len(ix.Warnings) != 2 {
		var msgs []string
		for _, w := range ix.Warnings {
			msgs = append(msgs, w.Path+": "+w.Err.Error())
		}
		t.Fatalf("want 2 warnings, got %d: %s", len(ix.Warnings), strings.Join(msgs, " | "))
	}

	var sawParseFailure, sawUnresolvedLink bool
	for _, w := range ix.Warnings {
		switch {
		case strings.Contains(w.Path, "broken.md"):
			sawParseFailure = true
		case strings.Contains(w.Err.Error(), "unresolved link [[missing-page]]"):
			sawUnresolvedLink = true
		}
	}
	if !sawParseFailure {
		t.Errorf("expected a warning for projects/a/issues/broken.md, got %+v", ix.Warnings)
	}
	if !sawUnresolvedLink {
		t.Errorf("expected an unresolved-link warning for [[missing-page]], got %+v", ix.Warnings)
	}
}

func TestLoadBasics(t *testing.T) {
	ix, _ := loadFixture(t)

	if _, ok := ix.Projects["a"]; !ok {
		t.Error("expected project a")
	}
	if _, ok := ix.Projects["b"]; !ok {
		t.Error("expected project b")
	}

	wantIssues := []string{"a-epic001", "a-child001", "b-child001", "a-open001", "b-blocked001", "a-archived001"}
	for _, id := range wantIssues {
		if _, ok := ix.IssueByID(id); !ok {
			t.Errorf("expected issue %s to be indexed", id)
		}
	}
	if _, ok := ix.IssueByID("a-broken001"); ok {
		t.Error("broken.md should not have been indexed as an issue")
	}

	if n, ok := ix.Notes["parity"]; !ok || n.Kind != KindDoc {
		t.Errorf("expected doc note %q, got %+v ok=%v", "parity", n, ok)
	}
	if n, ok := ix.Notes["prod-schema"]; !ok || n.Kind != KindMemory {
		t.Errorf("expected memory note %q, got %+v ok=%v", "prod-schema", n, ok)
	}

	if !ix.Assets["docs/img/screenshot.png"] {
		t.Errorf("expected docs/img/screenshot.png in Assets, got %v", ix.Assets)
	}
}

func TestLookup(t *testing.T) {
	ix, _ := loadFixture(t)

	if n, ok := ix.Lookup("a-open001"); !ok || n.Basename != "a-open001" {
		t.Errorf("Lookup by basename failed: %+v ok=%v", n, ok)
	}
	if n, ok := ix.Lookup("shared-schema"); !ok || n.Basename != "a-open001" {
		t.Errorf("Lookup by alias failed: %+v ok=%v", n, ok)
	}
	if n, ok := ix.Lookup("a-open001"); !ok || n.Issue == nil || n.Issue.ID != "a-open001" {
		t.Errorf("Lookup by id failed: %+v ok=%v", n, ok)
	}
	if n, ok := ix.Lookup("A-OPEN001"); !ok || n.Basename != "a-open001" {
		t.Errorf("case-insensitive Lookup failed: %+v ok=%v", n, ok)
	}
	if _, ok := ix.Lookup("does-not-exist"); ok {
		t.Error("expected Lookup of an unknown target to fail")
	}
}

func TestBacklinks(t *testing.T) {
	ix, _ := loadFixture(t)
	refs := ix.Backlinks["a-open001"]
	var sawDoc bool
	for _, r := range refs {
		if r.From == "parity" {
			sawDoc = true
		}
	}
	if !sawDoc {
		t.Errorf("expected docs/parity.md in Backlinks[a-open001], got %+v", refs)
	}
}

func TestReloadFlipsReady(t *testing.T) {
	ix, dir := loadFixture(t)

	before := ix.Ready("b", false)
	for _, iss := range before {
		if iss.ID == "b-blocked001" {
			t.Fatalf("b-blocked001 should not be ready before a-open001 closes: %+v", before)
		}
	}

	issuePath := filepath.Join(dir, "projects", "a", "issues", "a-open001.md")
	data, err := os.ReadFile(issuePath)
	if err != nil {
		t.Fatalf("read fixture issue: %v", err)
	}
	edited := strings.Replace(string(data), "status: open", "status: closed", 1)
	if edited == string(data) {
		t.Fatal("expected to replace status: open in a-open001.md")
	}
	if err := os.WriteFile(issuePath, []byte(edited), 0o644); err != nil {
		t.Fatalf("write edited issue: %v", err)
	}

	if err := ix.Reload(issuePath); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	after := ix.Ready("b", false)
	var sawBlocked bool
	for _, iss := range after {
		if iss.ID == "b-blocked001" {
			sawBlocked = true
		}
	}
	if !sawBlocked {
		t.Fatalf("expected b-blocked001 to be ready after a-open001 closed: %+v", after)
	}

	// Untouched state must survive the reload.
	if _, ok := ix.IssueByID("a-epic001"); !ok {
		t.Error("Reload of one file must not drop unrelated issues")
	}
}
