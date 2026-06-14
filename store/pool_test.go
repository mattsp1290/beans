package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mattsp1290/beans/model"

	gmysql "gorm.io/driver/mysql"
)

func TestConfigValidateDriverDefaultsAndRejectsUnknown(t *testing.T) {
	if err := (Config{DSN: SecretDSN("dsn")}).Validate(); err != nil {
		t.Fatalf("default driver Validate: %v", err)
	}
	if got := string((Config{DSN: SecretDSN("dsn")}).schemaDriver()); got != "postgres" {
		t.Fatalf("default schema driver = %q, want postgres", got)
	}
	err := (Config{Driver: Driver("oracle"), DSN: SecretDSN("dsn")}).Validate()
	if !errors.Is(err, ErrUnsupportedDriver) {
		t.Fatalf("unknown driver Validate = %v, want ErrUnsupportedDriver", err)
	}
}

func TestNewSQLitePoolMigratesAndCloses(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, Config{
		Driver: DriverSQLite,
		DSN:    SecretDSN(sqliteMemoryDSN(t)),
	})
	if err != nil {
		t.Fatalf("New sqlite: %v", err)
	}

	sqlDB, err := s.p.sql()
	if err != nil {
		t.Fatalf("pool sql: %v", err)
	}
	var count int
	if err := sqlDB.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE name = 'bn_projects'`,
	).Scan(&count); err != nil {
		t.Fatalf("query sqlite schema: %v", err)
	}
	if count != 1 {
		t.Fatalf("bn_projects count = %d, want 1", count)
	}
	if _, err := s.p.gorm(); err != nil {
		t.Fatalf("pool gorm: %v", err)
	}
	if pgx := s.p.pgx(); pgx != nil {
		t.Fatal("sqlite pool unexpectedly has legacy pgx handle")
	}
	var foreignKeys int
	if err := sqlDB.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatalf("query sqlite foreign_keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("sqlite foreign_keys pragma = %d, want 1", foreignKeys)
	}
	if err := s.EnsureProject(ctx, "sqlite"); err != nil {
		t.Fatalf("sqlite EnsureProject: %v", err)
	}
	exists, err := s.ProjectExists(ctx, "sqlite")
	if err != nil {
		t.Fatalf("sqlite ProjectExists: %v", err)
	}
	if !exists {
		t.Fatal("sqlite ProjectExists = false, want true")
	}

	s.Close()
	s.Close()

	if _, err := s.p.sql(); !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("sql after close = %v, want ErrPoolClosed", err)
	}
	if _, err := s.p.gorm(); !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("gorm after close = %v, want ErrPoolClosed", err)
	}
	if _, err := s.p.conn(); !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("legacy conn after close = %v, want ErrPoolClosed", err)
	}
}

func TestSQLiteIssueDependencyImportAndMemorySmoke(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, Config{
		Driver: DriverSQLite,
		DSN:    SecretDSN(sqliteMemoryDSN(t)),
	})
	if err != nil {
		t.Fatalf("New sqlite: %v", err)
	}
	defer s.Close()

	if err := s.EnsureProject(ctx, "sqlite"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	parent, err := s.CreateIssue(ctx, CreateIssueInput{
		Prefix: "sqlite", Title: "Parent", Priority: 2, IssueType: "task",
	})
	if err != nil {
		t.Fatalf("CreateIssue parent: %v", err)
	}
	child, err := s.CreateIssue(ctx, CreateIssueInput{
		Prefix: "sqlite", Title: "Child", Priority: 2, IssueType: "task",
	})
	if err != nil {
		t.Fatalf("CreateIssue child: %v", err)
	}
	if err := s.AddDep(ctx, child.ID, parent.ID); err != nil {
		t.Fatalf("AddDep: %v", err)
	}
	got, err := s.GetIssue(ctx, child.ID)
	if err != nil {
		t.Fatalf("GetIssue child: %v", err)
	}
	if len(got.BlockedBy) != 1 || got.BlockedBy[0] != parent.ID {
		t.Fatalf("child blockers = %v, want %s", got.BlockedBy, parent.ID)
	}
	ready, err := s.ReadyIssues(ctx, "sqlite", []model.IssueState{"closed"}, []model.IssueState{"open"})
	if err != nil {
		t.Fatalf("ReadyIssues: %v", err)
	}
	if len(ready) != 1 || ready[0].ID != parent.ID {
		t.Fatalf("ready IDs = %+v, want parent only", ready)
	}
	result, err := s.ImportIssuesFull(ctx, []ImportInput{{
		ID: "sqlite-import", Prefix: "sqlite", Title: "Imported", State: "open", Priority: 1, IssueType: "bug",
		Deps: []string{parent.ID},
	}}, ImportOptions{TerminalStates: []model.IssueState{"closed"}, Mode: ImportModeCreateOnly})
	if err != nil {
		t.Fatalf("ImportIssuesFull: %v", err)
	}
	if result.Created != 1 || result.DepsAdded != 1 {
		t.Fatalf("ImportIssuesFull result = %+v, want created=1 deps=1", result)
	}
	memory, err := s.InsertMemory(ctx, MemoryInput{
		Prefix: "sqlite", Body: `sqlite operational memory foo-bar 100% literal_under`, Type: "note", Tags: []string{"sqlite"},
	})
	if err != nil {
		t.Fatalf("InsertMemory: %v", err)
	}
	found, err := s.SearchMemories(ctx, "operational", MemoryFilter{Prefix: "sqlite", Tags: []string{"sqlite"}, Limit: 10})
	if err != nil {
		t.Fatalf("SearchMemories: %v", err)
	}
	if len(found) != 1 || found[0].ID != memory.ID {
		t.Fatalf("SearchMemories IDs = %+v, want %d", found, memory.ID)
	}
	for _, query := range []string{"foo-bar", `"unterminated`, "%", "_"} {
		if _, err := s.SearchMemories(ctx, query, MemoryFilter{Prefix: "sqlite", Limit: 10}); err != nil {
			t.Fatalf("SearchMemories %q: %v", query, err)
		}
	}
}

func TestMySQLDialectorSkipsUnboundedVersionProbe(t *testing.T) {
	dialector, err := gormDialector(Config{
		Driver: DriverMySQL,
		DSN:    SecretDSN("user:pass@tcp(127.0.0.1:1)/beans"),
	})
	if err != nil {
		t.Fatalf("gormDialector mysql: %v", err)
	}
	mysqlDialector, ok := dialector.(*gmysql.Dialector)
	if !ok {
		t.Fatalf("mysql dialector type = %T", dialector)
	}
	if !mysqlDialector.SkipInitializeWithVersion {
		t.Fatal("mysql dialector should skip GORM's uncapped version probe")
	}
}

func sqliteMemoryDSN(t *testing.T) string {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	return fmt.Sprintf("file:%s?mode=memory&cache=shared", name)
}
