package store

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/mattsp1290/beans/schema"
)

// ErrEmptyDSN is returned by Config.Validate when DSN is unset.
var ErrEmptyDSN = errors.New("store: DSN is required (set BN_DSN)")

// ErrUnsupportedDriver is returned by Config.Validate for unknown database drivers.
var ErrUnsupportedDriver = errors.New("store: unsupported database driver")

// Driver selects the database dialect used by the store.
type Driver string

const (
	DriverPostgres Driver = "postgres"
	DriverMySQL    Driver = "mysql"
	DriverSQLite   Driver = "sqlite"
)

// SecretDSN wraps a database DSN with multi-axis redaction, preventing
// credential leaks through log/json/fmt formatting. Only Reveal() returns
// the raw value; all other formatting paths return the redaction marker.
type SecretDSN string

// Reveal returns the raw DSN. The pool constructor is the only intended caller.
func (s SecretDSN) Reveal() string { return string(s) }

// IsSet reports whether the DSN is non-empty.
func (s SecretDSN) IsSet() bool { return string(s) != "" }

func (s SecretDSN) marker() string {
	if !s.IsSet() {
		return "[unset]"
	}
	return "[REDACTED]"
}

func (s SecretDSN) String() string               { return s.marker() }
func (s SecretDSN) GoString() string             { return s.marker() }
func (s SecretDSN) MarshalJSON() ([]byte, error) { return []byte(`"` + s.marker() + `"`), nil }
func (s SecretDSN) LogValue() slog.Value         { return slog.StringValue(s.marker()) }

// Config selects a database for the bn tracker store.
//
// DSN is read from BN_DSN at startup. The SecretDSN wrapper redacts every
// standard formatting path so leaks can only happen via the explicit Reveal()
// call. Driver defaults to Postgres when empty for compatibility.
type Config struct {
	// Driver is the database dialect. Empty means Postgres.
	Driver Driver

	// DSN is the database connection string. Required.
	DSN SecretDSN

	// MaxConns caps database/sql's maximum open connections. 0 means driver default.
	MaxConns int32

	// MinConns maps to database/sql's max idle connections. 0 means driver default.
	MinConns int32

	// ConnectTimeout is the upper bound on NewPool's dial + ping. 0 ⇒ 5s.
	ConnectTimeout time.Duration
}

// ConnectTimeoutDefault is used when Config.ConnectTimeout is zero.
const ConnectTimeoutDefault = 5 * time.Second

// Validate enforces required DSN and supported driver selection.
func (c Config) Validate() error {
	if !c.DSN.IsSet() {
		return ErrEmptyDSN
	}
	switch c.driverOrDefault() {
	case DriverPostgres, DriverMySQL, DriverSQLite:
		return nil
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedDriver, c.Driver)
	}
}

func (c Config) driverOrDefault() Driver {
	if c.Driver == "" {
		return DriverPostgres
	}
	return c.Driver
}

func (c Config) schemaDriver() schema.Driver {
	switch c.driverOrDefault() {
	case DriverMySQL:
		return schema.DriverMySQL
	case DriverSQLite:
		return schema.DriverSQLite
	default:
		return schema.DriverPostgres
	}
}

func (c Config) isPostgres() bool {
	return c.driverOrDefault() == DriverPostgres
}

func (c Config) connectTimeoutOrDefault() time.Duration {
	if c.ConnectTimeout <= 0 {
		return ConnectTimeoutDefault
	}
	return c.ConnectTimeout
}

func (c Config) applyPoolSettings(db interface {
	SetMaxOpenConns(int)
	SetMaxIdleConns(int)
}) {
	if c.MaxConns > 0 {
		db.SetMaxOpenConns(int(c.MaxConns))
	}
	if c.MinConns > 0 {
		db.SetMaxIdleConns(int(c.MinConns))
	}
}

func (c Config) sqliteDSNWithForeignKeys() string {
	dsn := c.DSN.Reveal()
	if strings.Contains(dsn, "_pragma=foreign_keys") ||
		strings.Contains(dsn, "_pragma=foreign_keys(") ||
		strings.Contains(dsn, "_foreign_keys=") {
		return dsn
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + "_pragma=" + url.QueryEscape("foreign_keys(1)")
}

// LogValue implements slog.LogValuer — DSN is redacted, pool-sizing fields
// are emitted verbatim.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("driver", string(c.driverOrDefault())),
		slog.Any("dsn", c.DSN),
		slog.Int64("max_conns", int64(c.MaxConns)),
		slog.Int64("min_conns", int64(c.MinConns)),
		slog.Duration("connect_timeout", c.connectTimeoutOrDefault()),
	)
}

// String implements fmt.Stringer. Redacted form — DSN does not appear.
func (c Config) String() string {
	return fmt.Sprintf(
		"store.Config{Driver:%s, DSN:%s, MaxConns:%d, MinConns:%d, ConnectTimeout:%s}",
		c.driverOrDefault(), c.DSN.String(), c.MaxConns, c.MinConns, c.connectTimeoutOrDefault(),
	)
}

// GoString implements fmt.GoStringer. Same as String().
func (c Config) GoString() string { return c.String() }
