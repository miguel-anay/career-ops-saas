package platform

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool creates and validates a new pgxpool connection pool.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create pgxpool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}

// WithTenant acquires a connection from the pool, sets the RLS session variable
// app.current_user_id, and executes fn within that connection context.
// The connection is returned to the pool after fn completes.
func WithTenant(ctx context.Context, pool *pgxpool.Pool, userID string, fn func(conn *pgx.Conn) error) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	_, err = conn.Exec(ctx, fmt.Sprintf("SET LOCAL app.current_user_id = '%s'", userID))
	if err != nil {
		return fmt.Errorf("set tenant user_id: %w", err)
	}

	return fn(conn.Conn())
}
