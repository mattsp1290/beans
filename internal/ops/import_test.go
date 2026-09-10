package ops

import (
	"os"
	"strings"
	"testing"

	"github.com/mattsp1290/beans/vault"
)

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
	if len(paths) != 178+2+5 {
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
