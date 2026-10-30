// Package db wraps the PostgreSQL connection pool and provides transaction
// helpers that set the row-level-security scope (app.org_id / app.user_id).
package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Scope is the RLS context for a transaction. Zero values leave the
// corresponding setting unset, so tenant tables return no rows.
type Scope struct {
	OrgID  uuid.UUID
	UserID uuid.UUID
}

// DB is the application database handle (role app_rw, subject to RLS).
type DB struct {
	Pool *pgxpool.Pool
}

// Open connects and verifies the connection.
func Open(ctx context.Context, url string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &DB{Pool: pool}, nil
}

// Close releases all connections.
func (d *DB) Close() { d.Pool.Close() }

// Ping checks connectivity (readiness probe).
func (d *DB) Ping(ctx context.Context) error { return d.Pool.Ping(ctx) }

// Tx runs fn inside a transaction with the given RLS scope. The settings are
// transaction-local (set_config(..., true)), so they never leak to other
// requests that reuse the pooled connection.
func (d *DB) Tx(ctx context.Context, scope Scope, fn func(tx pgx.Tx) error) (err error) {
	tx, err := d.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()

	if err = applyScope(ctx, tx, scope); err != nil {
		return err
	}
	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func applyScope(ctx context.Context, tx pgx.Tx, s Scope) error {
	org, user := "", ""
	if s.OrgID != uuid.Nil {
		org = s.OrgID.String()
	}
	if s.UserID != uuid.Nil {
		user = s.UserID.String()
	}
	if _, err := tx.Exec(ctx,
		`SELECT set_config('app.org_id', $1, true), set_config('app.user_id', $2, true)`,
		org, user); err != nil {
		return fmt.Errorf("set rls scope: %w", err)
	}
	return nil
}

// NewID returns a time-ordered UUIDv7.
func NewID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		// Only fails if the system random source fails.
		panic(fmt.Errorf("generate uuid: %w", err))
	}
	return id
}

// IsNoRows reports whether err means "no rows found".
func IsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// UniqueViolation returns the violated constraint name if err is a unique
// violation, or "" otherwise.
func UniqueViolation(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return pg.ConstraintName
	}
	return ""
}
