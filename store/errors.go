package store

import (
	"database/sql"
	"errors"
	"strings"

	gosqlite "github.com/glebarez/go-sqlite"
	gmysql "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/puddle/v2"
)

// Sentinel errors returned by Store methods. Callers branch on these with
// errors.Is rather than inspecting message text.
var (
	// ErrNotFound is returned when an operation targets an issue or project
	// that does not exist. Maps to tracker.CategoryNotFound.
	ErrNotFound = errors.New("store: not found")

	// ErrDuplicateDep is returned by AddDep when the edge already exists.
	ErrDuplicateDep = errors.New("store: dependency edge already exists")

	// ErrCycle is returned by AddDep when the proposed edge would create a
	// dependency cycle.
	ErrCycle = errors.New("store: dependency would create a cycle")

	// ErrConflict is returned when a unique constraint would be violated.
	ErrConflict = errors.New("store: conflict")

	// ErrUnauthorized is returned when an actor is not authorized to mutate
	// administrative project state such as repo registry entries.
	ErrUnauthorized = errors.New("store: unauthorized")

	// ErrDisabled is returned when an operation targets a disabled repo.
	ErrDisabled = errors.New("store: disabled")

	// ErrInvalidIssueState is returned when a caller supplies an issue state
	// outside the store schema's portable lifecycle vocabulary.
	ErrInvalidIssueState = errors.New("store: invalid issue state")
)

func isPKConflict(err error) bool {
	return isDuplicateConstraint(err)
}

func isDupKeyConflict(err error) bool {
	return isDuplicateConstraint(err)
}

func isDuplicateConstraint(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	var mysqlErr *gmysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1062
	}
	if sqliteCode(err) == sqliteConstraintPrimaryKey ||
		sqliteCode(err) == sqliteConstraintUnique {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "sqlite constraint error: unique constraint failed")
}

func isSerializationFailure(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "40001"
	}
	return false
}

func normalizePoolError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrPoolClosed) {
		return err
	}
	if errors.Is(err, sql.ErrConnDone) || errors.Is(err, puddle.ErrClosedPool) {
		return ErrPoolClosed
	}
	return err
}

func sqliteCode(err error) int {
	var glebarezErr *gosqlite.Error
	if errors.As(err, &glebarezErr) {
		return glebarezErr.Code()
	}
	return 0
}

const (
	sqliteConstraintPrimaryKey = 1555
	sqliteConstraintUnique     = 2067
)
