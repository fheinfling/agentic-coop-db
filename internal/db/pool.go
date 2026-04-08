// Package db owns the pgxpool used by the gateway and the helpers that
// every other package uses to start a request transaction.
//
// The pool's login role is `aicoldb_gateway`, a low-privilege role that has
// no privileges of its own beyond LOGIN and membership in dbadmin / dbuser
// (and any custom roles minted later). Privilege change happens inside the
// per-request transaction via SET LOCAL ROLE.
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolConfig is the input to OpenPool.
type PoolConfig struct {
	URL          string
	MaxConns     int32
	MinConns     int32
	ConnLifetime time.Duration
}

// OpenPool returns a configured pgxpool.Pool with sane defaults and waits
// until at least one connection is reachable.
func OpenPool(ctx context.Context, c PoolConfig) (*pgxpool.Pool, error) {
	if c.URL == "" {
		return nil, errors.New("db: empty database URL")
	}
	cfg, err := pgxpool.ParseConfig(c.URL)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if c.MaxConns > 0 {
		cfg.MaxConns = c.MaxConns
	}
	if c.MinConns > 0 {
		cfg.MinConns = c.MinConns
	}
	if c.ConnLifetime > 0 {
		cfg.MaxConnLifetime = c.ConnLifetime
	}
	cfg.ConnConfig.RuntimeParams["application_name"] = "aicoldb-gateway"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

// InTx runs fn in a transaction and rolls back on any error fn returns.
//
// fn receives the pgx.Tx so it can run multiple statements (e.g. SET LOCAL,
// then the user's SQL). The default isolation level is Postgres' default
// (READ COMMITTED) which is what every other interactive client uses.
func InTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) (err error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return
		}
		err = tx.Commit(ctx)
	}()
	return fn(tx)
}
