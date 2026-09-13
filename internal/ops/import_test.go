package ops

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mattsp1290/beans/issue"
	"github.com/mattsp1290/beans/vault"
)

func directoryManifest(t *testing.T, root string) map[string]string {
	t.Helper()
	entries := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return err
		}
		if d.IsDir() {
			entries[rel] = "dir"
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		entries[rel] = "file:" + string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestImportBDReportsUnmappedDependencyTypes(t *testing.T) {
	env, hub := testEnv(t)
	recs := []BDRecord{
		{ID: "p-aaaa", Title: "target", Status: "open", IssueType: "task"},
		{ID: "p-bbbb", Title: "blocks child", Status: "open", IssueType: "task", Dependencies: []BDDep{
			{IssueID: "p-bbbb", DependsOn: "p-aaaa", Type: "blocks"},
			{IssueID: "p-bbbb", DependsOn: "p-aaaa", Type: "discovered-from"},
			{IssueID: "wrong-id", DependsOn: "p-aaaa", Type: "ignored"},
		}},
		{ID: "p-cccc", Title: "parent child", Status: "open", IssueType: "task", Dependencies: []BDDep{
			{IssueID: "p-cccc", DependsOn: "p-aaaa", Type: "parent-child"},
			{IssueID: "p-cccc", DependsOn: "p-aaaa", Type: "discovered-from"},
			{IssueID: "p-cccc", DependsOn: "p-aaaa", Type: "related"},
		}},
	}
	op, rep := ImportBD(env, recs, true, false)
	before := directoryManifest(t, hub)
	if paths := apply(t, hub, op); len(paths) != 0 {
		t.Fatalf("dry run wrote paths: %v", paths)
	}
	if got := directoryManifest(t, hub); !reflect.DeepEqual(got, before) {
		t.Fatalf("dry run changed hub: got=%v want=%v", got, before)
	}
	wantWarnings := []string{
		`dropped 2 "discovered-from" edge(s); not mapped by bn import bd`,
		`dropped 1 "related" edge(s); not mapped by bn import bd`,
	}
	if rep.Blocks != 1 || rep.Parents != 1 || !reflect.DeepEqual(rep.Warnings, wantWarnings) {
		t.Fatalf("report = %+v", rep)
	}
	if paths := apply(t, hub, op); len(paths) != 0 || !reflect.DeepEqual(rep.Warnings, wantWarnings) || rep.Blocks != 1 || rep.Parents != 1 {
		t.Fatalf("replayed dry run paths=%v report=%+v", paths, rep)
	}
	if got := directoryManifest(t, hub); !reflect.DeepEqual(got, before) {
		t.Fatalf("replayed dry run changed hub: got=%v want=%v", got, before)
	}

	writeEnv, writeHub := testEnv(t)
	writeOp, _ := ImportBD(writeEnv, recs, false, false)
	apply(t, writeHub, writeOp)
	ix, err := vault.Load(writeHub)
	if err != nil {
		t.Fatal(err)
	}
	blocksChild := ix.Issues["p-bbbb"]
	parentChild := ix.Issues["p-cccc"]
	if blocksChild == nil || parentChild == nil {
		t.Fatalf("issues = %v", ix.Issues)
	}
	if len(blocksChild.BlockedBy) != 1 || blocksChild.BlockedBy[0].Target != "p-aaaa-target" {
		t.Fatalf("blocked_by = %+v", blocksChild.BlockedBy)
	}
	if parentChild.Parent.Target != "p-aaaa-target" {
		t.Fatalf("parent = %+v", parentChild.Parent)
	}
	for _, iss := range []*issue.Issue{blocksChild, parentChild} {
		if strings.Contains(iss.Description+iss.Body, "discovered-from") {
			t.Fatalf("unmapped edge persisted in body: %+v", iss)
		}
		for _, log := range iss.Log {
			if strings.Contains(log.Event, "discovered-from") || strings.Contains(log.Event, "related") {
				t.Fatalf("unmapped edge persisted in log: %+v", log)
			}
		}
	}
}

func TestImportBDLiveExport(t *testing.T) {
	env, hub := testEnv(t)
	env.Project = "beans"
	f, err := os.Open("../../cmd/bn/testdata/beans_export_2026-09-10.jsonl")
	if err != nil {
		t.Skip("fixture missing")
	}
	defer f.Close()
	recs, warnings, err := ParseBDExport(f)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("parse: %v %v", err, warnings)
	}
	op, rep := ImportBD(env, recs, true, false)
	if p := apply(t, hub, op); len(p) != 0 {
		t.Fatal("dry run wrote files")
	}
	if rep.Issues != 178 || rep.Memories != 2 || rep.Blocks != 263 || rep.Parents != 28 || len(rep.Rejected) != 0 || len(rep.Unresolved) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	op, rep = ImportBD(env, recs, false, false)
	paths := apply(t, hub, op)
	if len(paths) != 178+2+6 {
		t.Fatalf("paths = %d", len(paths))
	}
	ix, err := vault.Load(hub)
	if err != nil {
		t.Fatal(err)
	}
	if len(ix.Issues) != 178 || len(ix.Warnings) != 0 {
		for _, w := range ix.Warnings {
			t.Logf("warning: %s: %v", w.Path, w.Err)
		}
		t.Fatalf("issues=%d warnings=%d", len(ix.Issues), len(ix.Warnings))
	}
	if c := ix.Cycles(); len(c) != 0 {
		t.Fatalf("cycles: %v", c)
	}
	dotted := 0
	archived := 0
	for id, iss := range ix.Issues {
		if strings.Contains(id, ".") {
			dotted++
		}
		if iss.Archived {
			archived++
		}
	}
	if dotted != 21 || archived != 157 {
		t.Fatalf("dotted=%d archived=%d", dotted, archived)
	}
	if rep.Actors["Matt Spurlin"] != "matt-spurlin" {
		t.Fatalf("actors = %v", rep.Actors)
	}
	// re-running refuses without --force
	op, _ = ImportBD(env, recs, false, false)
	if _, err := op.Apply(hub); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("second import: %v", err)
	}
}

func TestParseBDExportSkipsBlankCommentsAndBadRows(t *testing.T) {
	recs, warnings, err := ParseBDExport(strings.NewReader("\n# comment\n{\"id\":\"p-1\",\"title\":\"one\",\"status\":\"open\"}\nnot json\n{\"_type\":\"memory\",\"key\":\"k\",\"value\":\"v\"}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || len(warnings) != 1 || !strings.Contains(warnings[0], "line 4") {
		t.Fatalf("recs=%d warnings=%v", len(recs), warnings)
	}
}

func TestImportBDGastownhallFixture(t *testing.T) {
	env, hub := testEnv(t)
	env.Project = "gastownhall"
	f, err := os.Open("../../cmd/bn/testdata/gastownhall_beads_export.jsonl")
	if err != nil {
		t.Skip("fixture missing")
	}
	defer f.Close()
	recs, warnings, err := ParseBDExport(f)
	if err != nil || len(warnings) != 0 || len(recs) == 0 {
		t.Fatalf("parse: %v %v %d", err, warnings, len(recs))
	}
	op, rep := ImportBD(env, recs, false, false)
	apply(t, hub, op)
	if rep.Issues != len(recs) || len(rep.Rejected) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if !strings.HasPrefix(op.ID, "(") || !strings.Contains(op.ID, "issues,") {
		t.Fatalf("commit subject should carry counts: %q", op.ID)
	}
	ix, err := vault.Load(hub)
	if err != nil || len(ix.Issues) != len(recs) {
		t.Fatalf("load: %v issues=%d", err, len(ix.Issues))
	}
}
