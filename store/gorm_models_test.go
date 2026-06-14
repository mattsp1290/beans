package store

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"gorm.io/datatypes"

	"github.com/mattsp1290/beans/model"
)

func TestGORMModelTableNames(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"project", gormProject{}.TableName(), "bn_projects"},
		{"issue", gormIssue{}.TableName(), "bn_issues"},
		{"issue dep", gormIssueDep{}.TableName(), "bn_issue_deps"},
		{"issue note", gormIssueNote{}.TableName(), "bn_issue_notes"},
		{"memory", gormMemory{}.TableName(), "bn_memories"},
		{"repo", gormRepo{}.TableName(), "bn_repos"},
		{"repo alias", gormRepoAlias{}.TableName(), "bn_repo_aliases"},
		{"project admin", gormProjectAdmin{}.TableName(), "bn_project_admins"},
		{"repo audit", gormRepoAudit{}.TableName(), "bn_repo_audit"},
		{"issue repo", gormIssueRepo{}.TableName(), "bn_issue_repos"},
	}

	seen := map[string]string{}
	for _, tc := range tests {
		if tc.got != tc.want {
			t.Fatalf("%s TableName() = %q, want %q", tc.name, tc.got, tc.want)
		}
		if prev, ok := seen[tc.got]; ok {
			t.Fatalf("%s and %s both use table %q", prev, tc.name, tc.got)
		}
		seen[tc.got] = tc.name
	}
}

func TestGORMModelsAvoidDialectSpecificTypeTags(t *testing.T) {
	models := []any{
		gormProject{},
		gormIssue{},
		gormIssueDep{},
		gormIssueNote{},
		gormMemory{},
		gormRepo{},
		gormRepoAlias{},
		gormProjectAdmin{},
		gormRepoAudit{},
		gormIssueRepo{},
	}
	for _, model := range models {
		typ := reflect.TypeOf(model)
		for i := 0; i < typ.NumField(); i++ {
			tag := string(typ.Field(i).Tag)
			for _, forbidden := range []string{"jsonb", "timestamptz", "bigserial", "tsvector"} {
				if strings.Contains(strings.ToLower(tag), forbidden) {
					t.Fatalf("%s.%s has dialect-specific tag %q", typ.Name(), typ.Field(i).Name, tag)
				}
			}
		}
	}
}

func TestJSONMappingHelpers(t *testing.T) {
	if got := string(jsonStringArray(nil)); got != "[]" {
		t.Fatalf("jsonStringArray(nil) = %q, want []", got)
	}
	if got := stringArrayFromJSON(datatypes.JSON([]byte(`["alpha","beta"]`))); !reflect.DeepEqual(got, []string{"alpha", "beta"}) {
		t.Fatalf("stringArrayFromJSON = %v, want alpha/beta", got)
	}
	if got := stringArrayFromJSON(datatypes.JSON([]byte(`{"bad":true}`))); got != nil {
		t.Fatalf("stringArrayFromJSON(object) = %v, want nil", got)
	}

	raw, err := jsonObject(map[string]any{"count": 2, "name": "repo"})
	if err != nil {
		t.Fatalf("jsonObject: %v", err)
	}
	got := objectFromJSON(raw)
	if got["name"] != "repo" || got["count"].(float64) != 2 {
		t.Fatalf("objectFromJSON = %#v, want JSON-decoded object", got)
	}

	empty, err := jsonObject(nil)
	if err != nil {
		t.Fatalf("jsonObject(nil): %v", err)
	}
	if string(empty) != "{}" {
		t.Fatalf("jsonObject(nil) = %s, want {}", empty)
	}

	if _, err := jsonObject(map[string]any{"bad": math.Inf(1)}); err == nil {
		t.Fatal("jsonObject accepted non-JSON float")
	}
	if got := objectFromJSON(datatypes.JSON([]byte(`[]`))); len(got) != 0 {
		t.Fatalf("objectFromJSON(array) = %#v, want empty object", got)
	}
}

func TestPriorityMappingHelpers(t *testing.T) {
	tests := []struct {
		core  model.Priority
		store int
		ok    bool
	}{
		{model.PriorityUnset, 0, false},
		{model.PriorityCritical, 0, true},
		{model.PriorityHigh, 1, true},
		{model.PriorityMedium, 2, true},
		{model.PriorityLow, 3, true},
		{model.PriorityBacklog, 4, true},
		{model.Priority(99), 0, false},
	}
	for _, tc := range tests {
		got, ok := corePriorityToStore(tc.core)
		if got != tc.store || ok != tc.ok {
			t.Fatalf("corePriorityToStore(%d) = %d, %v; want %d, %v", tc.core, got, ok, tc.store, tc.ok)
		}
		if tc.ok && storePriorityToCore(tc.store) != tc.core {
			t.Fatalf("storePriorityToCore(%d) = %d, want %d", tc.store, storePriorityToCore(tc.store), tc.core)
		}
	}
}
