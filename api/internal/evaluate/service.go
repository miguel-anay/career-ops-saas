package evaluate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
	"github.com/miguel-anay/career-ops-saas/api/internal/platform"
	"github.com/miguel-anay/career-ops-saas/api/internal/queue"
)

// ErrNotFound is returned when a job or report does not exist for this user.
var ErrNotFound = errors.New("not found")

// ErrUsageLimitExceeded is returned when the user has reached their free plan limit.
var ErrUsageLimitExceeded = errors.New("evaluation limit reached for free plan")

const freePlanEvalLimit = 5

// Service contains business logic for the evaluate domain.
type Service struct {
	pool *pgxpool.Pool
}

// NewService creates a new evaluate Service.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// evaluateJobPayload is the pg-boss job payload for "evaluate-job".
type evaluateJobPayload struct {
	UserID uuid.UUID `json:"user_id"`
	JobID  uuid.UUID `json:"job_id"`
}

// EnqueueEvaluation checks usage limits and enqueues an "evaluate-job" pg-boss job.
// Returns the queue job ID string.
//
// The job lookup and usage read both run inside ONE tenant-scoped
// transaction (platform.WithTenantTx) so app.current_user_id is set for RLS
// across both reads. The pg-boss enqueue happens AFTER that transaction
// commits, using the plain pool (pgboss.job has no RLS policy) — mirrors
// cv.EnqueueIngest.
func (s *Service) EnqueueEvaluation(ctx context.Context, userID, jobID uuid.UUID) (string, error) {
	month := time.Now().UTC().Format("2006-01")

	err := platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
		// 1. Check job exists and belongs to user (RLS guarantees tenant isolation).
		job, err := q.GetJobByID(ctx, jobID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("get job: %w", err)
		}
		if job.UserID != userID {
			return ErrNotFound
		}

		// 2. Check usage limit for free plan.
		usage, err := q.GetUsageByUserMonth(ctx, db.GetUsageByUserMonthParams{
			UserID: userID,
			Month:  month,
		})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("get usage: %w", err)
		}
		// If no usage row exists yet, EvaluationsCount is 0.
		if !errors.Is(err, sql.ErrNoRows) && usage.EvaluationsCount >= freePlanEvalLimit {
			// TODO: skip limit check for pro/unlimited plans (requires user plan lookup).
			return ErrUsageLimitExceeded
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", ErrNotFound
		}
		if errors.Is(err, ErrUsageLimitExceeded) {
			return "", ErrUsageLimitExceeded
		}
		return "", err
	}

	// 3. Enqueue evaluate-job (outside the tenant tx — pgboss.job has no RLS policy).
	queueID := uuid.New()
	payload, err := json.Marshal(evaluateJobPayload{
		UserID: userID,
		JobID:  jobID,
	})
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}

	if err := queue.Enqueue(ctx, s.pool, queue.Job{
		Name: "evaluate-job",
		Data: json.RawMessage(payload),
	}); err != nil {
		return "", fmt.Errorf("enqueue evaluate-job: %w", err)
	}

	return queueID.String(), nil
}

// GetReport returns the report for the user's job.
//
// The 3-step chain (GetJobByID -> GetApplicationByJobID ->
// GetReportByApplicationID) runs inside ONE tenant-scoped transaction
// (platform.WithTenantTx) so app.current_user_id is set for RLS
// consistently across all three reads, instead of three separate tx calls.
func (s *Service) GetReport(ctx context.Context, userID, jobID uuid.UUID) (*db.Report, error) {
	var report db.Report
	err := platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
		// Verify job ownership.
		job, err := q.GetJobByID(ctx, jobID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("get job: %w", err)
		}
		if job.UserID != userID {
			return ErrNotFound
		}

		// Get application linked to this job.
		application, err := q.GetApplicationByJobID(ctx, jobID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("get application: %w", err)
		}

		// Get report linked to this application.
		r, err := q.GetReportByApplicationID(ctx, application.ID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("get report: %w", err)
		}
		report = r
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &report, nil
}
