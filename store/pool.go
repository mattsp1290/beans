package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/glebarez/sqlite"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/puddle/v2"
	gmysql "gorm.io/driver/mysql"
	gpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// ErrPoolClosed is returned by Store methods called after the pool is closed.
var ErrPoolClosed = errors.New("store: pool is closed")

// pool owns the GORM/database-sql handle for the configured database. The
// legacy pgx handle exists only while store methods are still pgx-backed.
type pool struct {
	sqlDB     atomic.Pointer[sql.DB]
	gormDB    atomic.Pointer[gorm.DB]
	legacyPGX atomic.Pointer[pgxpool.Pool]
	driver    Driver
}

// newPool opens the configured database and returns a holder ready to use.
// It pings with Config.ConnectTimeout so callers do not get a lazy bad handle.
func newPool(ctx context.Context, cfg Config) (*pool, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	dialector, err := gormDialector(cfg)
	if err != nil {
		return nil, err
	}
	gormDB, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", cfg.driverOrDefault(), err)
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, fmt.Errorf("store: sql db %s: %w", cfg.driverOrDefault(), err)
	}
	cfg.applyPoolSettings(sqlDB)

	dialCtx, cancel := context.WithTimeout(ctx, cfg.connectTimeoutOrDefault())
	defer cancel()
	if err := sqlDB.PingContext(dialCtx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("store: ping %s: %w", cfg.driverOrDefault(), err)
	}

	p := &pool{driver: cfg.driverOrDefault()}
	p.sqlDB.Store(sqlDB)
	p.gormDB.Store(gormDB)

	if cfg.isPostgres() {
		legacy, err := openLegacyPGXPool(dialCtx, cfg)
		if err != nil {
			p.close()
			return nil, err
		}
		p.legacyPGX.Store(legacy)
	}

	return p, nil
}

func gormDialector(cfg Config) (gorm.Dialector, error) {
	dsn := cfg.DSN.Reveal()
	switch cfg.driverOrDefault() {
	case DriverPostgres:
		return gpostgres.Open(dsn), nil
	case DriverMySQL:
		return gmysql.New(gmysql.Config{DSN: dsn, SkipInitializeWithVersion: true}), nil
	case DriverSQLite:
		return sqlite.Open(cfg.sqliteDSNWithForeignKeys()), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedDriver, cfg.Driver)
	}
}

func openLegacyPGXPool(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN.Reveal())
	if err != nil {
		return nil, fmt.Errorf("store: parse postgres DSN: %w", err)
	}
	if cfg.MaxConns > 0 {
		poolCfg.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		poolCfg.MinConns = cfg.MinConns
	}
	pgxPool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("store: dial postgres legacy pgx: %w", err)
	}
	if err := pgxPool.Ping(ctx); err != nil {
		pgxPool.Close()
		return nil, fmt.Errorf("store: ping postgres legacy pgx: %w", err)
	}
	return pgxPool, nil
}

// close releases all underlying connections. Idempotent.
func (p *pool) close() {
	if p == nil {
		return
	}
	if old := p.legacyPGX.Swap(nil); old != nil {
		old.Close()
	}
	if old := p.sqlDB.Swap(nil); old != nil {
		old.Close()
	}
	p.gormDB.Store(nil)
}

func (p *pool) sql() (*sql.DB, error) {
	if p == nil {
		return nil, ErrPoolClosed
	}
	db := p.sqlDB.Load()
	if db == nil {
		return nil, ErrPoolClosed
	}
	return db, nil
}

func (p *pool) gorm() (*gorm.DB, error) {
	if p == nil || p.sqlDB.Load() == nil {
		return nil, ErrPoolClosed
	}
	db := p.gormDB.Load()
	if db == nil {
		return nil, ErrPoolClosed
	}
	return db, nil
}

// pgx returns the live legacy pgxpool.Pool or nil.
func (p *pool) pgx() *pgxpool.Pool {
	if p == nil || p.sqlDB.Load() == nil {
		return nil
	}
	return p.legacyPGX.Load()
}

// conn returns the live legacy Postgres pgx pool or ErrPoolClosed.
func (p *pool) conn() (*pgxpool.Pool, error) {
	if p == nil || p.sqlDB.Load() == nil {
		return nil, ErrPoolClosed
	}
	pp := p.legacyPGX.Load()
	if pp == nil {
		return nil, ErrUnsupportedDriver
	}
	return pp, nil
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
