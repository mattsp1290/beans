package ops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mattsp1290/beans/issue"
)

func TestRequestCreateUpdateAndLinks(t *testing.T) {
	env, hub := testEnv(t)
	issueID := create(t, env, hub, "Linked issue", CreateInput{Priority: 2})
	op, made := RequestCreate(env, RequestCreateInput{Title: "A request", Priority: 1, Issues: []string{issueID}}, "p")
	paths := apply(t, hub, op)
	if len(paths) != 1 || !issue.ValidRequestID(made.ID) {
		t.Fatalf("paths=%v id=%q", paths, made.ID)
	}
	r, loc, err := LoadRequest(hub, made.ID)
	if err != nil || len(r.Issues) != 1 || r.Issues[0].Target != issueID {
		t.Fatalf("request=%+v loc=%+v err=%v", r, loc, err)
	}
	status := issue.RequestAccepted
	priority := 0
	apply(t, hub, RequestUpdate(env, made.ID, RequestUpdateInput{Status: &status, Priority: &priority}))
	r, _, _ = LoadRequest(hub, made.ID)
	if r.Status != status || r.Priority != priority {
		t.Fatalf("request=%+v", r)
	}
	bad := issue.RequestResolved
	if _, err := RequestUpdate(env, made.ID, RequestUpdateInput{Status: &bad}).Apply(hub); err == nil {
		t.Fatal("invalid transition succeeded")
	}
	apply(t, hub, RequestUnlink(env, made.ID, []string{issueID}))
	r, _, _ = LoadRequest(hub, made.ID)
	if len(r.Issues) != 0 {
		t.Fatalf("issues=%+v", r.Issues)
	}
	_, issueLoc, err := Load(hub, issueID)
	if err != nil {
		t.Fatal(err)
	}
	apply(t, hub, RequestLink(env, made.ID, []string{issueLoc.Rel}))
	r, _, _ = LoadRequest(hub, made.ID)
	if len(r.Issues) != 1 || r.Issues[0].Target != issueID {
		t.Fatalf("path link=%+v", r.Issues)
	}
}

func TestRequestLinkIsAtomicAndFindIsExact(t *testing.T) {
	env, hub := testEnv(t)
	issueID := create(t, env, hub, "Existing", CreateInput{Priority: 2})
	op, made := RequestCreate(env, RequestCreateInput{Title: "A request", Priority: 2}, "p")
	apply(t, hub, op)
	if _, err := RequestLink(env, made.ID, []string{issueID, "missing-a1b2"}).Apply(hub); err == nil {
		t.Fatal("bad multi-link succeeded")
	}
	r, _, err := LoadRequest(hub, made.ID)
	if err != nil || len(r.Issues) != 0 {
		t.Fatalf("request=%+v err=%v", r, err)
	}
	if _, err := FindRequest(hub, "p-r-not-real"); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestRequestCreateExplicitEmptyBodySkipsTemplate(t *testing.T) {
	env, hub := testEnv(t)
	op, result := RequestCreate(env, RequestCreateInput{Title: "Empty", Priority: 2, BodyProvided: true}, "p")
	apply(t, hub, op)
	r, _, err := LoadRequest(hub, result.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Body != "" {
		t.Fatalf("explicit empty body loaded template: %q", r.Body)
	}
}

func TestArtifactIDRegistrySeesAliasesAndBasenames(t *testing.T) {
	env, hub := testEnv(t)
	id := create(t, env, hub, "Alias target", CreateInput{Priority: 2})
	iss, loc, err := Load(hub, id)
	if err != nil {
		t.Fatal(err)
	}
	iss.Aliases = append(iss.Aliases, "reserved-r-a3f2")
	if err := save(hub, loc, iss); err != nil {
		t.Fatal(err)
	}
	exists := existsID(hub)
	if !exists(id) || !exists("reserved-r-a3f2") {
		t.Fatal("id registry missed parsed id or alias")
	}
}

func TestArtifactIDRegistrySeesHubGlobalBasenames(t *testing.T) {
	_, hub := testEnv(t)
	if err := os.MkdirAll(filepath.Join(hub, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hub, "docs", "reserved-r-a3f2.md"), []byte("# Reserved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !existsID(hub)("reserved-r-a3f2") {
		t.Fatal("id registry missed hub-global document basename")
	}
}
