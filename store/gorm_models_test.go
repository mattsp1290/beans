package store

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
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
		{"dep graph guard", gormDepGraphGuard{}.TableName(), "bn_dep_graph_guard"},
		{"issue note", gormIssueNote{}.TableName(), "bn_issue_notes"},
		{"memory", gormMemory{}.TableName(), "bn_memories"},
		{"memory tag", gormMemoryTag{}.TableName(), "bn_memory_tags"},
		{"repo", gormRepo{}.TableName(), "bn_repos"},
		{"repo alias", gormRepoAlias{}.TableName(), "bn_repo_aliases"},
		{"project admin", gormProjectAdmin{}.TableName(), "bn_project_admins"},
		{"project admin bootstrap", gormProjectAdminBootstrap{}.TableName(), "bn_project_admin_bootstraps"},
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
	if len(allGORMModels) != len(tests) {
		t.Fatalf("allGORMModels has %d models, want %d", len(allGORMModels), len(tests))
	}
}

func TestGORMModelsCoverMigrationTables(t *testing.T) {
	migrationTables := migrationBNTableNames(t)
	modelTables := gormModelTableNames()

	if !reflect.DeepEqual(modelTables, migrationTables) {
		t.Fatalf("GORM model tables = %v, want migration tables %v", modelTables, migrationTables)
	}
}

func TestGORMModelsUseOnlyPortableTags(t *testing.T) {
	allowed := map[string]bool{
		"column":        true,
		"primaryKey":    true,
		"not null":      true,
		"autoIncrement": true,
	}
	for _, model := range allGORMModels {
		typ := reflect.TypeOf(model)
		for i := 0; i < typ.NumField(); i++ {
			tag := typ.Field(i).Tag.Get("gorm")
			for _, clause := range strings.Split(tag, ";") {
				clause = strings.TrimSpace(clause)
				if clause == "" {
					continue
				}
				key := clause
				if before, _, ok := strings.Cut(clause, ":"); ok {
					key = before
				}
				if !allowed[key] {
					t.Fatalf("%s.%s has unsupported gorm tag clause %q in %q", typ.Name(), typ.Field(i).Name, clause, tag)
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

func gormModelTableNames() []string {
	names := make([]string, len(allGORMModels))
	for i, model := range allGORMModels {
		names[i] = model.TableName()
	}
	sort.Strings(names)
	return names
}

func migrationBNTableNames(t *testing.T) []string {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join("..", "schema", "migrations", "postgres", "*.sql"))
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	re := regexp.MustCompile(`(?i)\bCREATE\s+TABLE\s+(bn_[a-z0-9_]+)\b`)
	seen := map[string]struct{}{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read migration %s: %v", path, err)
		}
		for _, match := range re.FindAllStringSubmatch(string(raw), -1) {
			seen[match[1]] = struct{}{}
		}
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
