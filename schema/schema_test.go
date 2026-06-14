package schema

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TestListMigrationsParsesEmbedded verifies the embedded migrations directory
// holds 0001_bn_init.sql and ListMigrations returns it with valid metadata.
func TestListMigrationsParsesEmbedded(t *testing.T) {
	t.Parallel()

	migs, err := ListMigrations(DriverPostgres)
	if err != nil {
		t.Fatalf("ListMigrations: %v", err)
	}
	if len(migs) == 0 {
		t.Fatal("ListMigrations returned no entries; expected at least 0001_bn_init.sql")
	}

	if migs[0].Version != 1 {
		t.Fatalf("first migration version = %d, want 1", migs[0].Version)
	}
	if migs[0].Name != "bn_init" {
		t.Fatalf("first migration name = %q, want %q", migs[0].Name, "bn_init")
	}

	if !strings.Contains(migs[0].SQL, "CREATE TABLE bn_issues") {
		t.Fatalf("first migration SQL missing bn_issues CREATE: %q", migs[0].SQL[:200])
	}

	if !sort.SliceIsSorted(migs, func(i, j int) bool { return migs[i].Version < migs[j].Version }) {
		t.Fatalf("migrations not ascending: %+v", migs)
	}

	var stateCheck *Migration
	for i := range migs {
		if migs[i].Version == 3 {
			stateCheck = &migs[i]
			break
		}
	}
	if stateCheck == nil {
		t.Fatalf("migration version 3 not found in %+v", migs)
	}
	if stateCheck.Name != "bn_issue_state_check" {
		t.Fatalf("migration 3 name = %q, want %q", stateCheck.Name, "bn_issue_state_check")
	}
	if !strings.Contains(stateCheck.SQL, "bn_issues_state_check") {
		t.Fatalf("state-check migration SQL missing constraint name: %q", stateCheck.SQL)
	}
	if !strings.Contains(stateCheck.SQL, "NOT VALID") {
		t.Fatalf("state-check migration should avoid validating legacy rows immediately: %q", stateCheck.SQL)
	}

	var repoCheck *Migration
	for i := range migs {
		if migs[i].Version == 4 {
			repoCheck = &migs[i]
			break
		}
	}
	if repoCheck == nil {
		t.Fatalf("migration version 4 not found in %+v", migs)
	}
	if repoCheck.Name != "bn_repos" {
		t.Fatalf("migration 4 name = %q, want %q", repoCheck.Name, "bn_repos")
	}
	if !strings.Contains(repoCheck.SQL, "CREATE TABLE bn_repos") {
		t.Fatalf("repo migration SQL missing bn_repos table: %q", repoCheck.SQL)
	}

	var memoryTags *Migration
	for i := range migs {
		if migs[i].Version == 6 {
			memoryTags = &migs[i]
			break
		}
	}
	if memoryTags == nil {
		t.Fatalf("migration version 6 not found in %+v", migs)
	}
	if memoryTags.Name != "bn_memory_tags" {
		t.Fatalf("migration 6 name = %q, want %q", memoryTags.Name, "bn_memory_tags")
	}
	if !strings.Contains(memoryTags.SQL, "CREATE TABLE bn_memory_tags") {
		t.Fatalf("memory tag migration SQL missing bn_memory_tags table: %q", memoryTags.SQL)
	}

	last := migs[len(migs)-1]
	if last.Version != 6 {
		t.Fatalf("last migration version = %d, want 6", last.Version)
	}
}

// TestListMigrationsBodiesAreNonEmpty ensures every embedded migration has SQL.
func TestListMigrationsBodiesAreNonEmpty(t *testing.T) {
	t.Parallel()

	migs, err := ListMigrations(DriverPostgres)
	if err != nil {
		t.Fatalf("ListMigrations: %v", err)
	}
	for _, m := range migs {
		if len(strings.TrimSpace(m.SQL)) == 0 {
			t.Errorf("migration %d (%s) has empty SQL body", m.Version, m.Name)
		}
	}
}

func TestMigrationFSDialectRoot(t *testing.T) {
	t.Parallel()

	for _, driver := range []Driver{DriverPostgres, DriverMySQL, DriverSQLite} {
		t.Run(string(driver), func(t *testing.T) {
			t.Parallel()
			migrations, err := MigrationFS(driver)
			if err != nil {
				t.Fatalf("MigrationFS: %v", err)
			}
			entries, err := fs.ReadDir(migrations, ".")
			if err != nil {
				t.Fatalf("ReadDir: %v", err)
			}
			if len(entries) == 0 {
				t.Fatalf("%s migration fs returned no entries", driver)
			}
			for _, entry := range entries {
				if entry.IsDir() {
					t.Fatalf("%s migration fs should be rooted at files, found dir %q", driver, entry.Name())
				}
			}
		})
	}
}

func TestDialectMigrationParity(t *testing.T) {
	t.Parallel()

	postgres, err := ListMigrations(DriverPostgres)
	if err != nil {
		t.Fatalf("ListMigrations postgres: %v", err)
	}
	for _, driver := range []Driver{DriverMySQL, DriverSQLite} {
		t.Run(string(driver), func(t *testing.T) {
			t.Parallel()
			got, err := ListMigrations(driver)
			if err != nil {
				t.Fatalf("ListMigrations %s: %v", driver, err)
			}
			if len(got) != len(postgres) {
				t.Fatalf("%s migration count = %d, want %d", driver, len(got), len(postgres))
			}
			for i := range postgres {
				if got[i].Version != postgres[i].Version || got[i].Name != postgres[i].Name {
					t.Fatalf("%s migration[%d] = %04d_%s, want %04d_%s",
						driver, i, got[i].Version, got[i].Name, postgres[i].Version, postgres[i].Name)
				}
			}
		})
	}
}

func TestDialectSpecificDDL(t *testing.T) {
	t.Parallel()

	mysqlSQL := allMigrationSQL(t, DriverMySQL)
	sqliteSQL := allMigrationSQL(t, DriverSQLite)

	assertMissingDDL(t, DriverMySQL, mysqlSQL, []string{
		"JSONB",
		"TIMESTAMPTZ",
		"BIGSERIAL",
		"tsvector",
		"USING GIN",
		" NOT VALID",
		" now()",
	})
	assertMissingDDL(t, DriverSQLite, sqliteSQL, []string{
		"JSONB",
		"TIMESTAMPTZ",
		"BIGSERIAL",
		"AUTO_INCREMENT",
		"REGEXP_LIKE",
		"FULLTEXT",
		" NOT VALID",
		" now()",
	})

	assertContainsDDL(t, DriverMySQL, mysqlSQL, []string{
		"JSON NOT NULL",
		"TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)",
		"CREATE FULLTEXT INDEX bn_memories_body_ft_idx",
		"VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin",
		"CREATE TABLE bn_memory_tags",
	})
	assertContainsDDL(t, DriverSQLite, sqliteSQL, []string{
		"TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP",
		"CREATE VIRTUAL TABLE bn_memories_fts USING fts5",
		"TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(tags))",
		"tag       TEXT NOT NULL COLLATE BINARY",
		"CREATE TABLE bn_memory_tags",
	})
}

func allMigrationSQL(t *testing.T, driver Driver) string {
	t.Helper()

	migs, err := ListMigrations(driver)
	if err != nil {
		t.Fatalf("ListMigrations %s: %v", driver, err)
	}
	var b strings.Builder
	for _, mig := range migs {
		fmt.Fprintf(&b, "\n-- %04d_%s\n%s\n", mig.Version, mig.Name, mig.SQL)
	}
	return b.String()
}

func assertMissingDDL(t *testing.T, driver Driver, sql string, forbidden []string) {
	t.Helper()

	lowerSQL := strings.ToLower(sql)
	for _, token := range forbidden {
		if strings.Contains(lowerSQL, strings.ToLower(token)) {
			t.Fatalf("%s DDL contains forbidden token %q", driver, token)
		}
	}
}

func assertContainsDDL(t *testing.T, driver Driver, sql string, required []string) {
	t.Helper()

	for _, token := range required {
		if !strings.Contains(sql, token) {
			t.Fatalf("%s DDL missing required token %q", driver, token)
		}
	}
}

func TestListMigrationsRejectsUnknownDriver(t *testing.T) {
	t.Parallel()

	if _, err := ListMigrations(Driver("oracle")); err == nil {
		t.Fatal("ListMigrations with unknown driver returned nil error")
	}
	if _, err := MigrationFS(Driver("oracle")); err == nil {
		t.Fatal("MigrationFS with unknown driver returned nil error")
	}
}

func TestMigrationLockerDispatch(t *testing.T) {
	t.Parallel()

	postgresLocker, err := migrationLocker(DriverPostgres)
	if err != nil {
		t.Fatalf("postgres migrationLocker: %v", err)
	}
	if postgresLocker == nil {
		t.Fatal("postgres migrationLocker returned nil")
	}

	mysqlLocker, err := migrationLocker(DriverMySQL)
	if err != nil {
		t.Fatalf("mysql migrationLocker: %v", err)
	}
	if mysqlLocker == nil {
		t.Fatal("mysql migrationLocker returned nil")
	}

	sqliteLocker, err := migrationLocker(DriverSQLite)
	if err != nil {
		t.Fatalf("sqlite migrationLocker: %v", err)
	}
	if sqliteLocker != nil {
		t.Fatal("sqlite migrationLocker returned non-nil locker")
	}
}

func TestMigrateSQLiteAppliesDialectDDL(t *testing.T) {
	t.Parallel()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm sqlite open: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sqlite DB: %v", err)
	}
	defer sqlDB.Close()

	if err := Migrate(context.Background(), sqlDB, DriverSQLite); err != nil {
		t.Fatalf("Migrate sqlite: %v", err)
	}

	for _, name := range []string{
		"bn_projects",
		"bn_issues",
		"bn_memories",
		"bn_memories_fts",
		"bn_memory_tags",
		"bn_issue_repos",
	} {
		var count int
		err := sqlDB.QueryRowContext(context.Background(),
			`SELECT count(*) FROM sqlite_master WHERE name = ?`,
			name,
		).Scan(&count)
		if err != nil {
			t.Fatalf("query sqlite_master for %s: %v", name, err)
		}
		if count != 1 {
			t.Fatalf("sqlite object %s count = %d, want 1", name, count)
		}
	}
}
