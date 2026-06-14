//go:build integration

package schema

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/testcontainers/testcontainers-go"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
)

func TestMigrateMySQLAppliesDialectDDL(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	container, err := tcmysql.Run(ctx, "mysql:8.4",
		tcmysql.WithDatabase("bn_test"),
		tcmysql.WithUsername("bn"),
		tcmysql.WithPassword("bn"),
	)
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		if isDockerUnavailable(err) {
			t.Skipf("schema integration: docker unavailable: %v", err)
		}
		t.Fatalf("schema integration: mysql.Run: %v", err)
	}

	dsn, err := container.ConnectionString(ctx, "parseTime=true", "multiStatements=true")
	if err != nil {
		t.Fatalf("schema integration: ConnectionString: %v", err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("schema integration: sql.Open: %v", err)
	}
	defer db.Close()

	if err := migrateToVersion(ctx, db, DriverMySQL, 5); err != nil {
		t.Fatalf("migrate mysql to v5: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO bn_projects (prefix) VALUES ('p')`); err != nil {
		t.Fatalf("insert project before v6: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO bn_memories (prefix, body, mtype, tags) VALUES ('p', 'alpha beta mysql', 'note', JSON_ARRAY('Design', 'design', 'Design'))`,
	); err != nil {
		t.Fatalf("insert memory before v6: %v", err)
	}
	if err := Migrate(ctx, db, DriverMySQL); err != nil {
		t.Fatalf("finish Migrate mysql: %v", err)
	}

	for _, name := range []string{
		"bn_projects",
		"bn_issues",
		"bn_memories",
		"bn_memory_tags",
		"bn_dep_graph_guard",
		"bn_project_admin_bootstraps",
		"bn_issue_repos",
	} {
		var count int
		err := db.QueryRowContext(ctx, `
			SELECT count(*)
			FROM information_schema.tables
			WHERE table_schema = database()
			  AND table_name = ?`,
			name,
		).Scan(&count)
		if err != nil {
			t.Fatalf("query information_schema for %s: %v", name, err)
		}
		if count != 1 {
			t.Fatalf("information_schema count for %s = %d, want 1", name, count)
		}
	}

	var memoryID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM bn_memories WHERE body = 'alpha beta mysql'`).Scan(&memoryID); err != nil {
		t.Fatalf("select memory id: %v", err)
	}
	var matchedID int64
	if err := db.QueryRowContext(ctx,
		`SELECT id FROM bn_memories WHERE MATCH(body) AGAINST (? IN NATURAL LANGUAGE MODE)`,
		"mysql",
	).Scan(&matchedID); err != nil {
		t.Fatalf("mysql fulltext match: %v", err)
	}
	if matchedID != memoryID {
		t.Fatalf("mysql fulltext matched id = %d, want %d", matchedID, memoryID)
	}
	var caseSensitiveCount int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM bn_memory_tags WHERE memory_id = ? AND tag = 'design'`,
		memoryID,
	).Scan(&caseSensitiveCount); err != nil {
		t.Fatalf("count case-sensitive tag: %v", err)
	}
	if caseSensitiveCount != 1 {
		t.Fatalf("case-sensitive tag count = %d, want 1", caseSensitiveCount)
	}
	var tagCount int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM bn_memory_tags WHERE memory_id = ?`,
		memoryID,
	).Scan(&tagCount); err != nil {
		t.Fatalf("count backfilled tags: %v", err)
	}
	if tagCount != 2 {
		t.Fatalf("backfilled tag count = %d, want 2", tagCount)
	}
}

func isDockerUnavailable(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "Cannot connect to the Docker daemon") ||
		strings.Contains(msg, "no such file") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "context deadline exceeded")
}
