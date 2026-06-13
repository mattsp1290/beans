package store

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/puddle/v2"
)

// ErrPoolClosed is returned by Store methods called after the pool is closed.
var ErrPoolClosed = errors.New("store: pool is closed")

// pool wraps a pgxpool.Pool. Unexported: callers use Store, not Pool directly.
// Concurrency: Close swaps the inner pointer atomically; in-flight readers
// complete against the captured pool before pgxpool tears it down.
type pool struct {
	p atomic.Pointer[pgxpool.Pool]
}

// newPool dials Postgres and returns a pool ready to use. Runs a connectivity
// check so a returned non-nil pool is guaranteed to have served one round-trip.
func newPool(ctx context.Context, cfg Config) (*pool, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.DSN.Reveal())
	if err != nil {
		return nil, fmt.Errorf("store: parse DSN: %w", err)
	}
	if cfg.MaxConns > 0 {
		poolCfg.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		poolCfg.MinConns = cfg.MinConns
	}

	dialCtx, cancel := context.WithTimeout(ctx, cfg.connectTimeoutOrDefault())
	defer cancel()

	pgxPool, err := pgxpool.NewWithConfig(dialCtx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("store: dial postgres: %w", err)
	}

	if err := pgxPool.Ping(dialCtx); err != nil {
		pgxPool.Close()
		return nil, fmt.Errorf("store: ping postgres: %w", err)
	}

	p := &pool{}
	p.p.Store(pgxPool)
	return p, nil
}

// close releases all underlying connections. Idempotent.
func (p *pool) close() {
	if p == nil {
		return
	}
	old := p.p.Swap(nil)
	if old != nil {
		old.Close()
	}
}

// pgx returns the live pgxpool.Pool or nil.
func (p *pool) pgx() *pgxpool.Pool {
	if p == nil {
		return nil
	}
	return p.p.Load()
}

// conn returns the live pool or ErrPoolClosed.
func (p *pool) conn() (*pgxpool.Pool, error) {
	if p == nil {
		return nil, ErrPoolClosed
	}
	pp := p.p.Load()
	if pp == nil {
		return nil, ErrPoolClosed
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
	if errors.Is(err, puddle.ErrClosedPool) {
		return ErrPoolClosed
	}
	return err
}
