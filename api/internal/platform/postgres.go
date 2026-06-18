package platform

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
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

// WithTenantTx runs fn inside a transaction with app.current_user_id set via
// set_config(..., true) (transaction-local), so RLS policies on tenant
// tables are enforced for the duration of fn. Commits if fn returns nil,
// rolls back otherwise. The pgxpool is wrapped via stdlib so the
// database/sql-based sqlc *db.Queries can run on the tx.
//
// Use this for EVERY tenant-table access. Do NOT use it for pgboss.* writes
// (queue.Enqueue) — that schema has no RLS policy and must stay on the raw
// pool.
//
// Body is the proven cv.withTenant lifted one level — see
// api/internal/cv/service.go, now re-pointed at this shared helper.
func WithTenantTx(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, fn func(q *db.Queries) error) error {
	sqlDB := stdlib.OpenDBFromPool(pool)
	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tenant tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "SELECT set_config('app.current_user_id', $1, true)", userID.String()); err != nil {
		return fmt.Errorf("set tenant user: %w", err)
	}

	if err := fn(db.New(tx)); err != nil {
		return err
	}

	return tx.Commit()
}
