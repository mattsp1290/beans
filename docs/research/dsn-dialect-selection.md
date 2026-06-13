# DSN and Dialect Selection Decision

Issue: `beans-wva`

This decision defines how `store.Config` selects PostgreSQL, MySQL, or SQLite
after the GORM migration, how DSNs are redacted, and how existing pgx pool
settings map to `database/sql`.

## Decision

Use an **explicit driver field** as the source of truth:

```go
type Driver string

const (
	DriverPostgres Driver = "postgres"
	DriverMySQL    Driver = "mysql"
	DriverSQLite   Driver = "sqlite"
)

type Config struct {
	Driver         Driver
	DSN            SecretDSN
	MaxOpenConns   int
	MaxIdleConns   int
	ConnectTimeout time.Duration
}
```

CLI wiring should read `BN_DRIVER` into `Config.Driver` and `BN_DSN` into
`Config.DSN`. Normalize those two values once, before `Config.Validate`, so CLI
and store validation cannot diverge. `BN_DRIVER` is required for new
multi-database configurations. For backward compatibility, an empty `BN_DRIVER`
may infer `postgres` only when `BN_DSN` has an explicit `postgres://` or
`postgresql://` URL scheme, or when it parses successfully as a supported
Postgres keyword DSN using the same parser/grammar used by the Postgres open
path. Do not infer from substring checks such as `host=`, `user=`, or
`dbname=`. Ambiguous, MySQL-looking, or SQLite-looking DSNs must produce a
validation error that asks the user to set `BN_DRIVER`.

Do not use DSN-scheme inference as the general mechanism. MySQL DSNs commonly
look like `user:pass@tcp(host:3306)/db?...` and SQLite DSNs can be bare file
paths, so scheme inference is fragile and can silently choose the wrong driver.

## Accepted Driver Values

| Driver | GORM dialector | DSN examples | Notes |
| --- | --- | --- | --- |
| `postgres` | `gorm.io/driver/postgres` | `postgres://user:pass@host/db`, `host=localhost user=bn dbname=beans sslmode=disable` | Keeps compatibility with the current `BN_DSN=postgres://...` workflow. |
| `mysql` | `gorm.io/driver/mysql` | `user:pass@tcp(localhost:3306)/beans?charset=utf8mb4&parseTime=True&loc=UTC` | DSN must parse with the MySQL driver parser, set `parseTime=true`, and use an explicit UTC location policy unless tests prove timestamp handling is UTC without it. |
| `sqlite` | `github.com/glebarez/sqlite` | `file:beans.db?_pragma=foreign_keys(1)`, `file::memory:?cache=shared&_pragma=foreign_keys(1)` | Use the pure-Go driver to preserve CGO-free builds; ensure foreign keys are enabled. |

## Validation Rules

`Config.Validate` should enforce:

- `Driver` is one of `postgres`, `mysql`, or `sqlite`.
- `DSN` is non-empty for all drivers.
- MySQL DSNs parse with the MySQL driver parser, have `ParseTime == true`, and
  have `loc=UTC` unless the implementation proves all bound/scanned values are
  UTC independently of DSN location.
- SQLite opens use a connection-scoped foreign-key strategy for every pooled
  connection. Prefer normalizing the DSN to include the glebarez-supported
  foreign-key pragma; if explicit initialization is used, document and test how
  it runs per connection rather than once per DB handle.
- `MaxOpenConns` and `MaxIdleConns` are non-negative.
- `MaxIdleConns <= MaxOpenConns` when both are non-zero.
- `ConnectTimeout <= 0` keeps the existing default of five seconds.
- `BN_DRIVER` values normalize case-insensitively from `postgres`,
  `postgresql`, `pg`, `mysql`, `sqlite`, and `sqlite3` to the three canonical
  constants; unknown aliases are rejected.

## SecretDSN Redaction

Keep the `SecretDSN` type and its current safety property: `String`, `GoString`,
`MarshalJSON`, and `LogValue` must never reveal the raw DSN. The default marker
can remain `[REDACTED]`, but helper methods used in diagnostics may return a
sanitized form if useful.

Required redaction behavior if a `SafeDiagnostic(driver Driver)` helper is
added:

| DSN shape | Example raw DSN | Safe diagnostic form |
| --- | --- | --- |
| URL-style Postgres | `postgres://user:pass@host/db?sslmode=disable` | `postgres://user:xxxxx@host/db?sslmode=disable` |
| Libpq key/value | `host=h user=u password=p dbname=d` | `host=h user=u password=xxxxx dbname=d` |
| MySQL | `user:pass@tcp(host:3306)/db?parseTime=True` | `user:xxxxx@tcp(host:3306)/db?parseTime=True` |
| SQLite file | `file:/tmp/beans.db?_pragma=foreign_keys(1)&token=s` | `file:/tmp/beans.db?_pragma=foreign_keys(1)&token=xxxxx` |
| SQLite memory | `file::memory:?cache=shared&auth_token=s` | `file::memory:?cache=shared&auth_token=xxxxx` |

Implementation guidance:

- Continue making `fmt.Sprintf("%s", cfg.DSN)`, JSON, and slog show only a
  redaction marker.
- Add a separate `SafeDiagnostic(driver Driver) string` only if command errors
  need to show parsed host/db/file information.
- `SafeDiagnostic` must be parser-backed and table-tested: use `net/url` for
  URL-style DSNs, a libpq key/value grammar that handles quoted/escaped values,
  the MySQL driver parser for MySQL DSNs, and URI query parsing for SQLite.
- Redact userinfo passwords and sensitive query/key names such as `password`,
  `pass`, `sslpassword`, `token`, and `auth_*`.
- On parse failure, return only `[REDACTED]`.
- Never log or include `Reveal()` output in wrapped errors.

## Pool Mapping

Current pgx settings:

- `Config.MaxConns int32` maps to `pgxpool.Config.MaxConns`.
- `Config.MinConns int32` maps to `pgxpool.Config.MinConns`.

New `database/sql` settings:

| Old field | New field | Mapping |
| --- | --- | --- |
| `MaxConns int32` | `MaxOpenConns int` | If > 0, call `sqlDB.SetMaxOpenConns(MaxOpenConns)`. |
| `MinConns int32` | `MaxIdleConns int` | There is no direct min-idle equivalent. Treat the old value as desired max idle connections and call `sqlDB.SetMaxIdleConns(MaxIdleConns)`. |
| none | optional `ConnMaxLifetime` | Do not add in this migration unless a backend requires it. |
| `ConnectTimeout` | `ConnectTimeout` | `gorm.Open` does not accept a context. Enforce timeout through driver DSN timeout options where available and always call `sqlDB.PingContext` with `ConnectTimeout`. |

SQLite should default `MaxOpenConns` to `1` unless tests prove concurrent writes
are reliable with a higher value. For `file::memory:` or shared-cache in-memory
SQLite DSNs, keep at least one idle connection and reject `MaxIdleConns == 0` so
the database is not destroyed when the last connection closes. PostgreSQL and
MySQL should keep the `database/sql` defaults when the config values are zero.

## CLI Contract

Environment:

```text
BN_DRIVER  postgres | mysql | sqlite
BN_DSN     driver-specific DSN
```

Error behavior:

- If both are missing, report both required values with examples.
- If `BN_DRIVER` is missing but the DSN is clearly Postgres, infer `postgres`
  and emit no warning to preserve current behavior.
- If `BN_DRIVER` is missing for MySQL/SQLite DSNs, fail with a message that names
  `BN_DRIVER=mysql` or `BN_DRIVER=sqlite`.
- Update command help and `bn prime` text to stop saying "Postgres connection
  string" generically.

Examples for docs:

```bash
BN_DRIVER=postgres BN_DSN='postgres://user:pass@localhost:5432/beans?sslmode=disable'
BN_DRIVER=mysql BN_DSN='user:pass@tcp(localhost:3306)/beans?charset=utf8mb4&parseTime=True&loc=UTC'
BN_DRIVER=sqlite BN_DSN='file:beans.db?_pragma=foreign_keys(1)'
```

## Implementation Checklist

- Add `Driver`, `MaxOpenConns`, and `MaxIdleConns` to `store.Config`.
- Keep deprecated `MaxConns`/`MinConns` only temporarily if needed for a staged
  compile; remove or alias them before final cleanup.
- Implement `Config.Validate` driver-specific rules.
- Replace `pgxpool.ParseConfig` in `store/pool.go` with a GORM dialector switch.
- After `gorm.Open`, call `sqlDB.PingContext` inside `ConnectTimeout`.
- Apply `SetMaxOpenConns`/`SetMaxIdleConns` to `sqlDB`.
- Update `cmd/bn/app.go` to read `BN_DRIVER` and pass `store.Config.Driver`.
- Change `schema.Migrate` to accept the opened SQL/GORM handle plus `Driver`.
- Select goose dialect, lock strategy, and embedded migration directory from the
  same `Config.Driver` value used to open the store; do not infer again from the
  DSN inside schema code.
- Update `Store.New` to pass `cfg.Driver` into migration.
- Update `cmd/bn/cmd_memory.go`, README, prime text, and root/help strings after
  implementation.

## Tests Required

- `SecretDSN` never leaks credentials through `String`, `GoString`, JSON, or slog
  for Postgres URL, libpq key/value, MySQL, and SQLite DSNs.
- `SafeDiagnostic`, if implemented, redacts URL userinfo, libpq quoted values,
  MySQL passwords, and SQLite sensitive query parameters; parse failures return
  only `[REDACTED]`.
- `Config.Validate` rejects unknown drivers, empty DSNs, negative pool settings,
  invalid idle/open combinations, and MySQL DSNs without parser-confirmed
  `parseTime=true` and UTC location policy.
- Backward-compatible Postgres inference works for current `BN_DSN` examples
  through parser-backed URL and keyword DSN cases.
- Ambiguous MySQL/SQLite DSNs without `BN_DRIVER` fail clearly.
- SQLite tests prove foreign keys are enabled on every connection, including
  after pool recycling or multiple opens.
- SQLite file and memory DSN pool tests cover `MaxOpenConns=1`, shared-cache
  memory behavior, and rejection of destructive idle settings.
- Pool setup calls `SetMaxOpenConns`, `SetMaxIdleConns`, and `PingContext` with
  the configured timeout.
- Postgres, MySQL, and SQLite integration helpers construct `{Driver, DSN}`
  pairs and verify migrations run through the same path as CLI/store startup.
- CLI tests cover missing env vars, invalid driver values, Postgres fallback
  inference, and MySQL/SQLite missing-driver errors.
