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

	if err := Migrate(ctx, db, DriverMySQL); err != nil {
		t.Fatalf("Migrate mysql: %v", err)
	}

	for _, name := range []string{
		"bn_projects",
		"bn_issues",
		"bn_memories",
		"bn_memory_tags",
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
}

func isDockerUnavailable(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "Cannot connect to the Docker daemon") ||
		strings.Contains(msg, "no such file") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "context deadline exceeded")
}
