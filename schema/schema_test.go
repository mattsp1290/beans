package schema

import (
	"io/fs"
	"sort"
	"strings"
	"testing"
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

	last := migs[len(migs)-1]
	if last.Version != 5 {
		t.Fatalf("last migration version = %d, want 5", last.Version)
	}
	if last.Name != "bn_issue_repos" {
		t.Fatalf("last migration name = %q, want %q", last.Name, "bn_issue_repos")
	}
	if !strings.Contains(last.SQL, "CREATE TABLE bn_issue_repos") {
		t.Fatalf("issue repo migration SQL missing bn_issue_repos table: %q", last.SQL)
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

	migrations, err := MigrationFS(DriverPostgres)
	if err != nil {
		t.Fatalf("MigrationFS: %v", err)
	}
	entries, err := fs.ReadDir(migrations, ".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("postgres migration fs returned no entries")
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("postgres migration fs should be rooted at files, found dir %q", entry.Name())
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

	if _, err := migrationLocker(DriverSQLite); err == nil {
		t.Fatal("sqlite migrationLocker returned nil error")
	}
}
