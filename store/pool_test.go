package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
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

func sqliteMemoryDSN(t *testing.T) string {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	return fmt.Sprintf("file:%s?mode=memory&cache=shared", name)
}
