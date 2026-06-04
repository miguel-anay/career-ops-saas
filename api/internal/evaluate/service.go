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
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
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

func (s *Service) queries() *db.Queries {
	sqlDB := stdlib.OpenDBFromPool(s.pool)
	return db.New(sqlDB)
}

// evaluateJobPayload is the pg-boss job payload for "evaluate-job".
type evaluateJobPayload struct {
	UserID uuid.UUID `json:"user_id"`
	JobID  uuid.UUID `json:"job_id"`
}

// EnqueueEvaluation checks usage limits and enqueues an "evaluate-job" pg-boss job.
// Returns the queue job ID string.
func (s *Service) EnqueueEvaluation(ctx context.Context, userID, jobID uuid.UUID) (string, error) {
	q := s.queries()

	// 1. Check job exists and belongs to user (RLS guarantees tenant isolation).
	job, err := q.GetJobByID(ctx, jobID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("get job: %w", err)
	}
	if job.UserID != userID {
		return "", ErrNotFound
	}

	// 2. Check usage limit for free plan.
	month := time.Now().UTC().Format("2006-01")
	usage, err := q.GetUsageByUserMonth(ctx, db.GetUsageByUserMonthParams{
		UserID: userID,
		Month:  month,
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("get usage: %w", err)
	}
	// If no usage row exists yet, EvaluationsCount is 0.
	if !errors.Is(err, sql.ErrNoRows) && usage.EvaluationsCount >= freePlanEvalLimit {
		// TODO: skip limit check for pro/unlimited plans (requires user plan lookup).
		return "", ErrUsageLimitExceeded
	}

	// 3. Enqueue evaluate-job.
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
func (s *Service) GetReport(ctx context.Context, userID, jobID uuid.UUID) (*db.Report, error) {
	q := s.queries()

	// Verify job ownership.
	job, err := q.GetJobByID(ctx, jobID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get job: %w", err)
	}
	if job.UserID != userID {
		return nil, ErrNotFound
	}

	// Get application linked to this job.
	application, err := q.GetApplicationByJobID(ctx, jobID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get application: %w", err)
	}

	// Get report linked to this application.
	report, err := q.GetReportByApplicationID(ctx, application.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get report: %w", err)
	}

	return &report, nil
}
