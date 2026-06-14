package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

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
	if err := s.EnsureProject(ctx, "sqlite"); !errors.Is(err, ErrUnsupportedDriver) {
		t.Fatalf("sqlite EnsureProject = %v, want ErrUnsupportedDriver during pgx transition", err)
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
