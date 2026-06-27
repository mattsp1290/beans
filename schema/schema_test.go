package schema

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/pressly/goose/v3"
	"gorm.io/gorm"
)

var allDrivers = []Driver{DriverPostgres, DriverMySQL, DriverSQLite}

var expectedMigrations = []struct {
	version int
	name    string
}{
	{1, "bn_init"},
	{2, "bn_memories"},
	{3, "bn_issue_state_check"},
	{4, "bn_repos"},
	{5, "bn_issue_repos"},
	{6, "bn_memory_tags"},
	{7, "bn_semantic_guards"},
	{8, "bn_dep_type"},
	{9, "bn_remote_url_unique"},
	{10, "bn_issue_state_drop_check"},
	{11, "bn_issue_repos_creation_commit"},
}

// TestListMigrationsParsesEmbedded verifies every dialect's embedded migration
// directory returns the expected ordered migration metadata.
func TestListMigrationsParsesEmbedded(t *testing.T) {
	t.Parallel()

	for _, driver := range allDrivers {
		t.Run(string(driver), func(t *testing.T) {
			t.Parallel()
			migs, err := ListMigrations(driver)
			if err != nil {
				t.Fatalf("ListMigrations: %v", err)
			}
			if len(migs) != len(expectedMigrations) {
				t.Fatalf("migration count = %d, want %d", len(migs), len(expectedMigrations))
			}
			if !sort.SliceIsSorted(migs, func(i, j int) bool { return migs[i].Version < migs[j].Version }) {
				t.Fatalf("migrations not ascending: %+v", migs)
			}
			for i, want := range expectedMigrations {
				if migs[i].Version != want.version || migs[i].Name != want.name {
					t.Fatalf("migration[%d] = %04d_%s, want %04d_%s", i, migs[i].Version, migs[i].Name, want.version, want.name)
				}
			}
		})
	}
}

func TestMigrationRequiredObjects(t *testing.T) {
	t.Parallel()

	for _, driver := range allDrivers {
		t.Run(string(driver), func(t *testing.T) {
			t.Parallel()
			migs, err := ListMigrations(driver)
			if err != nil {
				t.Fatalf("ListMigrations: %v", err)
			}
			byVersion := migrationsByVersion(migs)

			assertContainsDDL(t, driver, byVersion[1].SQL, []string{
				"CREATE TABLE bn_projects",
				"CREATE TABLE bn_issues",
				"CREATE INDEX bn_issues_prefix_state_idx",
				"CREATE TABLE bn_issue_deps",
				"CREATE INDEX bn_issue_deps_blocker_idx",
				"CREATE TABLE bn_issue_notes",
				"CREATE INDEX bn_issue_notes_issue_idx",
			})
			assertContainsDDL(t, driver, byVersion[2].SQL, []string{
				"CREATE TABLE bn_memories",
				"CREATE INDEX bn_memories_prefix_idx",
			})
			assertContainsDDL(t, driver, byVersion[4].SQL, []string{
				"CREATE TABLE bn_repos",
				"CREATE INDEX bn_repos_prefix_enabled_idx",
				"CREATE TABLE bn_repo_aliases",
				"CREATE INDEX bn_repo_aliases_repo_idx",
				"CREATE TABLE bn_project_admins",
				"CREATE TABLE bn_repo_audit",
				"CREATE INDEX bn_repo_audit_prefix_created_idx",
				"CREATE INDEX bn_repo_audit_repo_created_idx",
			})
			assertContainsDDL(t, driver, byVersion[5].SQL, []string{
				"CREATE TABLE bn_issue_repos",
				"CREATE INDEX bn_issue_repos_repo_idx",
			})
			assertContainsDDL(t, driver, byVersion[6].SQL, []string{
				createTableToken(driver, "bn_memory_tags"),
				"CREATE INDEX bn_memory_tags_tag_memory_idx",
				"CREATE INDEX bn_memory_tags_memory_idx",
			})
			assertContainsDDL(t, driver, byVersion[7].SQL, []string{
				"CREATE TABLE bn_dep_graph_guard",
				"CREATE TABLE bn_project_admin_bootstraps",
			})

			assertContainsDDL(t, driver, byVersion[9].SQL, []string{
				"bn_repos_remote_url_idx",
				"bn_repos",
				"remote_url",
			})
			assertContainsDDL(t, driver, byVersion[11].SQL, []string{
				"ALTER TABLE bn_issue_repos ADD COLUMN creation_commit",
				"TEXT NOT NULL DEFAULT",
			})

			stateSQL := byVersion[3].SQL
			if driver == DriverSQLite {
				stateSQL = byVersion[1].SQL
			}
			assertContainsDDL(t, driver, stateSQL, []string{
				"CHECK (state IN",
				"'open'",
				"'in_progress'",
				"'blocked'",
				"'closed'",
				"'done'",
			})
		})
	}
}

// TestListMigrationsBodiesAreNonEmpty ensures every embedded migration has SQL.
func TestListMigrationsBodiesAreNonEmpty(t *testing.T) {
	t.Parallel()

	for _, driver := range allDrivers {
		t.Run(string(driver), func(t *testing.T) {
			t.Parallel()
			migs, err := ListMigrations(driver)
			if err != nil {
				t.Fatalf("ListMigrations: %v", err)
			}
			for _, m := range migs {
				if len(strings.TrimSpace(m.SQL)) == 0 {
					t.Errorf("migration %d (%s) has empty SQL body", m.Version, m.Name)
				}
			}
		})
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
	postgresSQL := allMigrationSQL(t, DriverPostgres)

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

	assertContainsDDL(t, DriverPostgres, postgresSQL, []string{
		"JSONB",
		"TIMESTAMPTZ",
		"BIGSERIAL",
		"tsvector GENERATED ALWAYS AS",
		"USING GIN",
	})
	assertContainsDDL(t, DriverMySQL, mysqlSQL, []string{
		"JSON NOT NULL",
		"TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)",
		"CREATE FULLTEXT INDEX bn_memories_body_ft_idx",
		"VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin",
		"CREATE TABLE IF NOT EXISTS bn_memory_tags",
		"CREATE TABLE bn_dep_graph_guard",
		"CREATE TABLE bn_project_admin_bootstraps",
		"ADD COLUMN dep_type VARCHAR(64) NOT NULL DEFAULT 'blocks'",
		"ADD COLUMN creation_commit TEXT NOT NULL DEFAULT ('')",
	})
	assertContainsDDL(t, DriverSQLite, sqliteSQL, []string{
		"TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP",
		"CREATE VIRTUAL TABLE bn_memories_fts USING fts5",
		"TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(tags))",
		"tag       TEXT NOT NULL COLLATE BINARY",
		"CREATE TABLE bn_memory_tags",
		"CREATE TABLE bn_dep_graph_guard",
		"CREATE TABLE bn_project_admin_bootstraps",
		"ADD COLUMN dep_type TEXT NOT NULL DEFAULT 'blocks'",
		"ADD COLUMN creation_commit TEXT NOT NULL DEFAULT ''",
	})

	assertContainsDDL(t, DriverPostgres, postgresSQL, []string{
		"ADD COLUMN dep_type TEXT NOT NULL DEFAULT 'blocks'",
		"ADD COLUMN creation_commit TEXT NOT NULL DEFAULT ''",
		"CREATE UNIQUE INDEX bn_repos_remote_url_idx ON bn_repos (remote_url)",
	})
	assertContainsDDL(t, DriverSQLite, sqliteSQL, []string{
		"CREATE UNIQUE INDEX bn_repos_remote_url_idx ON bn_repos (remote_url)",
	})
	assertContainsDDL(t, DriverMySQL, mysqlSQL, []string{
		"MODIFY remote_url VARCHAR(768) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL",
		"CREATE UNIQUE INDEX bn_repos_remote_url_idx ON bn_repos (remote_url)",
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

func migrationsByVersion(migs []Migration) map[int]Migration {
	out := make(map[int]Migration, len(migs))
	for _, mig := range migs {
		out[mig.Version] = mig
	}
	return out
}

func createTableToken(driver Driver, table string) string {
	if driver == DriverMySQL {
		return "CREATE TABLE IF NOT EXISTS " + table
	}
	return "CREATE TABLE " + table
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

	db, err := gorm.Open(sqlite.Open(sqliteMemoryDSN(t)), &gorm.Config{})
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
		"bn_dep_graph_guard",
		"bn_project_admin_bootstraps",
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

	if _, err := sqlDB.ExecContext(context.Background(), `PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable sqlite foreign keys: %v", err)
	}
	if _, err := sqlDB.ExecContext(context.Background(), `INSERT INTO bn_projects (prefix) VALUES ('p')`); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := sqlDB.ExecContext(context.Background(),
		`INSERT INTO bn_memories (prefix, body, mtype, tags) VALUES ('p', 'alpha beta sqlite', 'note', '["Design","design"]')`,
	); err != nil {
		t.Fatalf("insert memory: %v", err)
	}
	var memoryID int64
	if err := sqlDB.QueryRowContext(context.Background(), `SELECT id FROM bn_memories WHERE body = 'alpha beta sqlite'`).Scan(&memoryID); err != nil {
		t.Fatalf("select memory id: %v", err)
	}
	if _, err := sqlDB.ExecContext(context.Background(),
		`INSERT INTO bn_memory_tags (memory_id, tag) VALUES (?, 'Design'), (?, 'design')`,
		memoryID, memoryID,
	); err != nil {
		t.Fatalf("insert memory tags: %v", err)
	}
	var ftsRowID int64
	if err := sqlDB.QueryRowContext(context.Background(),
		`SELECT rowid FROM bn_memories_fts WHERE bn_memories_fts MATCH 'sqlite'`,
	).Scan(&ftsRowID); err != nil {
		t.Fatalf("sqlite fts match: %v", err)
	}
	if ftsRowID != memoryID {
		t.Fatalf("sqlite fts rowid = %d, want memory id %d", ftsRowID, memoryID)
	}
	if _, err := sqlDB.ExecContext(context.Background(), `DELETE FROM bn_memories WHERE id = ?`, memoryID); err != nil {
		t.Fatalf("delete memory: %v", err)
	}
	var tagCount int
	if err := sqlDB.QueryRowContext(context.Background(), `SELECT count(*) FROM bn_memory_tags WHERE memory_id = ?`, memoryID).Scan(&tagCount); err != nil {
		t.Fatalf("count cascaded tags: %v", err)
	}
	if tagCount != 0 {
		t.Fatalf("sqlite memory tag count after delete = %d, want 0", tagCount)
	}

	assertSQLiteTextColumn(t, sqlDB, "bn_issue_repos", "creation_commit", true, "''")
}

func TestMigrateSQLite0010DropsStateCheckAndPreservesIssueShape(t *testing.T) {
	t.Parallel()

	db, err := gorm.Open(sqlite.Open(sqliteMemoryDSN(t)), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm sqlite open: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sqlite DB: %v", err)
	}
	defer sqlDB.Close()

	ctx := context.Background()
	if err := migrateToVersion(ctx, sqlDB, DriverSQLite, 9); err != nil {
		t.Fatalf("migrate sqlite to v9: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable sqlite foreign keys: %v", err)
	}

	if _, err := sqlDB.ExecContext(ctx, `INSERT INTO bn_projects (prefix) VALUES ('p')`); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
		INSERT INTO bn_repos (id, prefix, slug, display_name, remote_url, auth_ref)
		VALUES ('repo-1', 'p', 'repo', 'Repo', 'file:///tmp/repo', 'none')`); err != nil {
		t.Fatalf("insert repo: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
		INSERT INTO bn_issues (id, prefix, identifier, title, description, priority, issue_type, state, labels, branch_name, url)
		VALUES
			('p-1', 'p', '1', 'one', 'first', 1, 'task', 'open', '["a"]', 'branch', 'https://example.invalid/1'),
			('p-2', 'p', '2', 'two', 'second', 2, 'bug', 'blocked', '[]', '', '')`); err != nil {
		t.Fatalf("insert legacy issues: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
		INSERT INTO bn_issue_deps (issue_id, blocked_by_id)
		VALUES ('p-2', 'p-1')`); err != nil {
		t.Fatalf("insert dependency: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
		INSERT INTO bn_issue_notes (issue_id, actor, body)
		VALUES ('p-1', 'tester', 'note')`); err != nil {
		t.Fatalf("insert note: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
		INSERT INTO bn_issue_repos (issue_id, repo_id, requested_ref, base_ref, work_branch, worktree_subdir, metadata)
		VALUES ('p-1', 'repo-1', 'main', 'main', 'work', 'subdir', '{"ok":true}')`); err != nil {
		t.Fatalf("insert issue repo link: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `UPDATE bn_issues SET state = 'ready_for_review' WHERE id = 'p-1'`); err == nil {
		t.Fatal("legacy SQLite state CHECK accepted ready_for_review before v10")
	}

	if err := migrateToVersion(ctx, sqlDB, DriverSQLite, 10); err != nil {
		t.Fatalf("migrate sqlite to v10: %v", err)
	}

	assertSQLiteColumns(t, sqlDB, "bn_issues", []string{
		"id",
		"prefix",
		"identifier",
		"title",
		"description",
		"priority",
		"issue_type",
		"state",
		"labels",
		"branch_name",
		"url",
		"created_at",
		"updated_at",
	})
	assertSQLiteIndexExists(t, sqlDB, "bn_issues_prefix_state_idx")
	assertSQLiteTableSQLMissing(t, sqlDB, "bn_issues", "CHECK (state IN")

	if _, err := sqlDB.ExecContext(ctx, `UPDATE bn_issues SET state = 'ready_for_review' WHERE id = 'p-1'`); err != nil {
		t.Fatalf("state update after v10: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
		INSERT INTO bn_issues (id, prefix, title, description, state)
		VALUES ('p-3', 'p', 'three', '', 'ready_for_validation')`); err != nil {
		t.Fatalf("insert ready_for_validation after v10: %v", err)
	}

	var (
		state    string
		labels   string
		repoRows int
		depRows  int
		noteRows int
	)
	if err := sqlDB.QueryRowContext(ctx, `
		SELECT state, labels FROM bn_issues WHERE id = 'p-1'`,
	).Scan(&state, &labels); err != nil {
		t.Fatalf("select migrated issue: %v", err)
	}
	if state != "ready_for_review" || labels != `["a"]` {
		t.Fatalf("migrated issue state/labels = %q/%q, want ready_for_review/[\"a\"]", state, labels)
	}
	if err := sqlDB.QueryRowContext(ctx, `SELECT count(*) FROM bn_issue_repos WHERE issue_id = 'p-1'`).Scan(&repoRows); err != nil {
		t.Fatalf("count issue repo rows: %v", err)
	}
	if err := sqlDB.QueryRowContext(ctx, `SELECT count(*) FROM bn_issue_deps WHERE issue_id = 'p-2' AND blocked_by_id = 'p-1'`).Scan(&depRows); err != nil {
		t.Fatalf("count dep rows: %v", err)
	}
	if err := sqlDB.QueryRowContext(ctx, `SELECT count(*) FROM bn_issue_notes WHERE issue_id = 'p-1'`).Scan(&noteRows); err != nil {
		t.Fatalf("count note rows: %v", err)
	}
	if repoRows != 1 || depRows != 1 || noteRows != 1 {
		t.Fatalf("preserved child rows repo/deps/notes = %d/%d/%d, want 1/1/1", repoRows, depRows, noteRows)
	}
}

func TestMigrateSQLiteBackfillsIssueRepoCreationCommit(t *testing.T) {
	t.Parallel()

	db, err := gorm.Open(sqlite.Open(sqliteMemoryDSN(t)), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm sqlite open: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sqlite DB: %v", err)
	}
	defer sqlDB.Close()

	ctx := context.Background()
	if err := migrateToVersion(ctx, sqlDB, DriverSQLite, 10); err != nil {
		t.Fatalf("migrate sqlite to v10: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable sqlite foreign keys: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `INSERT INTO bn_projects (prefix) VALUES ('p')`); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
		INSERT INTO bn_issues (id, prefix, title, description)
		VALUES ('p-1', 'p', 'issue', '')`); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
		INSERT INTO bn_repos (id, prefix, slug, display_name, remote_url, auth_ref)
		VALUES ('repo-1', 'p', 'repo', 'Repo', 'file:///tmp/repo', 'none')`); err != nil {
		t.Fatalf("insert repo: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
		INSERT INTO bn_issue_repos (issue_id, repo_id, metadata)
		VALUES ('p-1', 'repo-1', '{}')`); err != nil {
		t.Fatalf("insert legacy issue repo link: %v", err)
	}

	if err := Migrate(ctx, sqlDB, DriverSQLite); err != nil {
		t.Fatalf("finish sqlite migrate: %v", err)
	}

	assertSQLiteTextColumn(t, sqlDB, "bn_issue_repos", "creation_commit", true, "''")

	var creationCommit string
	if err := sqlDB.QueryRowContext(ctx,
		`SELECT creation_commit FROM bn_issue_repos WHERE issue_id = 'p-1'`,
	).Scan(&creationCommit); err != nil {
		t.Fatalf("select creation_commit: %v", err)
	}
	if creationCommit != "" {
		t.Fatalf("creation_commit = %q, want empty string", creationCommit)
	}
}

func TestMigrateSQLiteBackfillsMemoryTags(t *testing.T) {
	t.Parallel()

	db, err := gorm.Open(sqlite.Open(sqliteMemoryDSN(t)), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm sqlite open: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sqlite DB: %v", err)
	}
	defer sqlDB.Close()

	ctx := context.Background()
	if err := migrateToVersion(ctx, sqlDB, DriverSQLite, 5); err != nil {
		t.Fatalf("migrate sqlite to v5: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `INSERT INTO bn_projects (prefix) VALUES ('p')`); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx,
		`INSERT INTO bn_memories (prefix, body, tags) VALUES ('p', 'legacy sqlite alpha', '["Design","design","Design"]')`,
	); err != nil {
		t.Fatalf("insert legacy memory: %v", err)
	}
	if err := Migrate(ctx, sqlDB, DriverSQLite); err != nil {
		t.Fatalf("finish sqlite migrate: %v", err)
	}

	rows, err := sqlDB.QueryContext(ctx, `
		SELECT tag
		FROM bn_memory_tags
		WHERE memory_id = (SELECT id FROM bn_memories WHERE body = 'legacy sqlite alpha')
		ORDER BY tag`)
	if err != nil {
		t.Fatalf("select memory tags: %v", err)
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			t.Fatalf("scan tag: %v", err)
		}
		tags = append(tags, tag)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("tags rows: %v", err)
	}
	if got, want := strings.Join(tags, ","), "Design,design"; got != want {
		t.Fatalf("backfilled tags = %q, want %q", got, want)
	}
}

func TestMigrateSQLiteRejectsInvalidLegacyMemoryTags(t *testing.T) {
	t.Parallel()

	db, err := gorm.Open(sqlite.Open(sqliteMemoryDSN(t)), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm sqlite open: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sqlite DB: %v", err)
	}
	defer sqlDB.Close()

	ctx := context.Background()
	if err := migrateToVersion(ctx, sqlDB, DriverSQLite, 5); err != nil {
		t.Fatalf("migrate sqlite to v5: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `INSERT INTO bn_projects (prefix) VALUES ('p')`); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx,
		`INSERT INTO bn_memories (prefix, body, tags) VALUES ('p', 'bad tags', '["ok",123]')`,
	); err != nil {
		t.Fatalf("insert invalid legacy memory: %v", err)
	}
	if err := Migrate(ctx, sqlDB, DriverSQLite); err == nil {
		t.Fatal("finish sqlite migrate with invalid legacy tags succeeded, want error")
	}
}

func migrateToVersion(ctx context.Context, db *sql.DB, driver Driver, version int64) error {
	migrations, err := MigrationFS(driver)
	if err != nil {
		return err
	}
	dialect, err := gooseDialect(driver)
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(
		dialect,
		db,
		migrations,
		goose.WithTableName(migrationVersionTable),
	)
	if err != nil {
		return fmt.Errorf("schema: goose provider for %s: %w", driver, err)
	}
	if _, err := provider.UpTo(ctx, version); err != nil {
		return fmt.Errorf("schema: migrate up to %d for %s: %w", version, driver, err)
	}
	return nil
}

func assertSQLiteTextColumn(t *testing.T, db *sql.DB, table, column string, notNull bool, defaultValue string) {
	t.Helper()

	rows, err := db.QueryContext(context.Background(), fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		t.Fatalf("pragma table_info(%s): %v", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid       int
			name      string
			columnTyp string
			notnull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &columnTyp, &notnull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan table_info(%s): %v", table, err)
		}
		if name != column {
			continue
		}
		if columnTyp != "TEXT" {
			t.Fatalf("%s.%s type = %q, want TEXT", table, column, columnTyp)
		}
		if (notnull == 1) != notNull {
			t.Fatalf("%s.%s notnull = %d, want %t", table, column, notnull, notNull)
		}
		if !dfltValue.Valid || dfltValue.String != defaultValue {
			t.Fatalf("%s.%s default = %q (valid %t), want %q", table, column, dfltValue.String, dfltValue.Valid, defaultValue)
		}
		return
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info(%s) rows: %v", table, err)
	}
	t.Fatalf("%s.%s column not found", table, column)
}

func assertSQLiteColumns(t *testing.T, db *sql.DB, table string, want []string) {
	t.Helper()

	rows, err := db.QueryContext(context.Background(), fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		t.Fatalf("pragma table_info(%s): %v", table, err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var (
			cid       int
			name      string
			columnTyp string
			notnull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &columnTyp, &notnull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan table_info(%s): %v", table, err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info(%s) rows: %v", table, err)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%s columns = %v, want %v", table, got, want)
	}
}

func assertSQLiteIndexExists(t *testing.T, db *sql.DB, index string) {
	t.Helper()

	var count int
	if err := db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = ?`,
		index,
	).Scan(&count); err != nil {
		t.Fatalf("query sqlite index %s: %v", index, err)
	}
	if count != 1 {
		t.Fatalf("sqlite index %s count = %d, want 1", index, count)
	}
}

func assertSQLiteTableSQLMissing(t *testing.T, db *sql.DB, table, forbidden string) {
	t.Helper()

	var tableSQL string
	if err := db.QueryRowContext(context.Background(),
		`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`,
		table,
	).Scan(&tableSQL); err != nil {
		t.Fatalf("query sqlite table %s SQL: %v", table, err)
	}
	if strings.Contains(tableSQL, forbidden) {
		t.Fatalf("sqlite table %s SQL contains forbidden token %q: %s", table, forbidden, tableSQL)
	}
}

func sqliteMemoryDSN(t *testing.T) string {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	return fmt.Sprintf("file:%s?mode=memory&cache=shared", name)
}
