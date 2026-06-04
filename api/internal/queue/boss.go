package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Job represents a pg-boss compatible job to be enqueued.
type Job struct {
	Name string
	Data json.RawMessage
}

// Enqueue inserts a job into the pgboss.job table with state='created'.
// This is compatible with pg-boss's job table schema.
func Enqueue(ctx context.Context, pool *pgxpool.Pool, job Job) error {
	id := uuid.New()
	now := time.Now().UTC()

	const query = `
		INSERT INTO pgboss.job (id, name, data, state, "createdOn", "startAfter", "expireIn", priority)
		VALUES ($1, $2, $3, 'created', $4, $4, interval '15 minutes', 0)
	`

	_, err := pool.Exec(ctx, query, id, job.Name, job.Data, now)
	if err != nil {
		return fmt.Errorf("enqueue job %q: %w", job.Name, err)
	}

	return nil
}
