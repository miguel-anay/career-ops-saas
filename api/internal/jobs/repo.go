package jobs

import (
	"context"

	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
)

// Repo is a thin wrapper around sqlc db.Queries for the jobs table.
type Repo struct {
	q *db.Queries
}

// newRepoFromQueries creates a Repo from a *db.Queries already bound to a
// tenant-scoped transaction (see platform.WithTenantTx). This is the only
// constructor — every jobs query must run with app.current_user_id set, so
// there is no raw-pool constructor here.
func newRepoFromQueries(q *db.Queries) *Repo {
	return &Repo{q: q}
}

// ListByUser returns a paginated list of jobs for the given user.
func (r *Repo) ListByUser(ctx context.Context, userID uuid.UUID, page, limit int) ([]db.Job, error) {
	if limit <= 0 {
		limit = 20
	}
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit
	return r.q.ListJobsByUser(ctx, db.ListJobsByUserParams{
		UserID: userID,
		Limit:  int32(limit),
		Offset: int32(offset),
	})
}

// GetByID returns the job with the given ID.
func (r *Repo) GetByID(ctx context.Context, jobID uuid.UUID) (db.Job, error) {
	return r.q.GetJobByID(ctx, jobID)
}

// Insert creates a new job record.
func (r *Repo) Insert(ctx context.Context, params db.InsertJobParams) (db.Job, error) {
	return r.q.InsertJob(ctx, params)
}

// UpdateStatus updates the status of a job.
func (r *Repo) UpdateStatus(ctx context.Context, jobID uuid.UUID, status db.JobStatusT) (db.Job, error) {
	return r.q.UpdateJobStatus(ctx, db.UpdateJobStatusParams{
		ID:     jobID,
		Status: status,
	})
}

// UpsertByURL upserts a job by URL (INSERT ... ON CONFLICT DO UPDATE).
func (r *Repo) UpsertByURL(ctx context.Context, params db.UpsertJobByURLParams) (db.Job, error) {
	return r.q.UpsertJobByURL(ctx, params)
}
