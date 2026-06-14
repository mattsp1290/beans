package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	gmysql "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestDuplicateConstraintClassification(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "postgres unique violation",
			err:  &pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint"},
			want: true,
		},
		{
			name: "postgres other",
			err:  &pgconn.PgError{Code: "23503", Message: "foreign key violation"},
		},
		{
			name: "mysql duplicate entry",
			err:  &gmysql.MySQLError{Number: 1062, Message: "Duplicate entry 'a' for key 'PRIMARY'"},
			want: true,
		},
		{
			name: "mysql other",
			err:  &gmysql.MySQLError{Number: 1452, Message: "Cannot add or update a child row"},
		},
		{
			name: "wrapped sqlite unique text",
			err:  fmt.Errorf("store: insert: %w", errors.New("sqlite constraint error: UNIQUE constraint failed: bn_projects.prefix")),
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDuplicateConstraint(tc.err); got != tc.want {
				t.Fatalf("isDuplicateConstraint(%v) = %v, want %v", tc.err, got, tc.want)
			}
			if got := isDupKeyConflict(tc.err); got != tc.want {
				t.Fatalf("isDupKeyConflict(%v) = %v, want %v", tc.err, got, tc.want)
			}
			if got := isPKConflict(tc.err); got != tc.want {
				t.Fatalf("isPKConflict(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestSQLiteDuplicateConstraintClassificationFromDriver(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, Config{
		Driver: DriverSQLite,
		DSN:    SecretDSN(sqliteMemoryDSN(t)),
	})
	if err != nil {
		t.Fatalf("New sqlite: %v", err)
	}
	defer s.Close()

	db, err := s.p.sql()
	if err != nil {
		t.Fatalf("pool sql: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO bn_projects (prefix) VALUES ('dup')`); err != nil {
		t.Fatalf("insert first project: %v", err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO bn_projects (prefix) VALUES ('dup')`)
	if !isDuplicateConstraint(err) {
		t.Fatalf("sqlite duplicate insert = %v, want duplicate classification", err)
	}
}

func TestPoolClosedClassification(t *testing.T) {
	for _, err := range []error{ErrPoolClosed, sql.ErrConnDone} {
		if got := normalizePoolError(err); !errors.Is(got, ErrPoolClosed) {
			t.Fatalf("normalizePoolError(%v) = %v, want ErrPoolClosed", err, got)
		}
	}
}
