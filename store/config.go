package store

import (
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// ErrEmptyDSN is returned by Config.Validate when DSN is unset.
var ErrEmptyDSN = errors.New("store: DSN is required (set BN_DSN)")

// SecretDSN wraps a Postgres DSN with multi-axis redaction, preventing
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

// Config selects a Postgres database for the bn tracker store.
//
// DSN is read from BN_DSN at startup. The SecretDSN wrapper redacts every
// standard formatting path so leaks can only happen via the explicit Reveal()
// call. MaxConns defaults to pgxpool's built-in default when zero.
type Config struct {
	// DSN is the Postgres connection string. Required.
	DSN SecretDSN

	// MaxConns caps the pgxpool's maximum connections. 0 ⇒ pgxpool default.
	MaxConns int32

	// MinConns is the steady-state lower bound on idle connections. 0 ⇒ 0.
	MinConns int32

	// ConnectTimeout is the upper bound on NewPool's dial + ping. 0 ⇒ 5s.
	ConnectTimeout time.Duration
}

// ConnectTimeoutDefault is used when Config.ConnectTimeout is zero.
const ConnectTimeoutDefault = 5 * time.Second

// Validate enforces the only hard requirement: a non-empty DSN.
func (c Config) Validate() error {
	if !c.DSN.IsSet() {
		return ErrEmptyDSN
	}
	return nil
}

func (c Config) connectTimeoutOrDefault() time.Duration {
	if c.ConnectTimeout <= 0 {
		return ConnectTimeoutDefault
	}
	return c.ConnectTimeout
}

// LogValue implements slog.LogValuer — DSN is redacted, pool-sizing fields
// are emitted verbatim.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Any("dsn", c.DSN),
		slog.Int64("max_conns", int64(c.MaxConns)),
		slog.Int64("min_conns", int64(c.MinConns)),
		slog.Duration("connect_timeout", c.connectTimeoutOrDefault()),
	)
}

// String implements fmt.Stringer. Redacted form — DSN does not appear.
func (c Config) String() string {
	return fmt.Sprintf(
		"store.Config{DSN:%s, MaxConns:%d, MinConns:%d, ConnectTimeout:%s}",
		c.DSN.String(), c.MaxConns, c.MinConns, c.connectTimeoutOrDefault(),
	)
}

// GoString implements fmt.GoStringer. Same as String().
func (c Config) GoString() string { return c.String() }
