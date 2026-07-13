package evaluate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

// ErrCVMissing is returned when the requesting user has no CV on file
// (users.cv_markdown is NULL or empty) — evaluation would burn LLM tokens
// against nothing to compare, so it's rejected before enqueue.
var ErrCVMissing = errors.New("cv missing")

// ErrJobContentMissing is returned when the target job has no scraped job
// description (jobs.scraped_content is NULL or empty), regardless of
// ingestion source (manual, email, or ATS scan).
var ErrJobContentMissing = errors.New("job content missing")

// ErrStalePosting is returned when the job's received_at exceeds the user's
// own scoring_rules.max_posting_age_days (a per-user opt-in gate — a no-op
// when that field is unset or the job's received_at is unknown).
var ErrStalePosting = errors.New("stale posting")

const freePlanEvalLimit = 5

// scoringRules is the subset of the profile_overrides/profile_json
// "scoring_rules" key this guard cares about. boost/penalize are consumed
// narratively by the worker's evaluation prompt, not here.
type scoringRules struct {
	MaxPostingAgeDays *int `json:"max_posting_age_days"`
}

// extractScoringRules reads the effective "scoring_rules" key (override wins
// over the CV-derived value, mirroring profile.currentKey's precedence —
// duplicated here rather than imported, since evaluate/profile are separate
// domain packages per this project's hexagonal convention). Malformed or
// absent input yields nil, never an error — this guard fails open.
func extractScoringRules(base, overrides []byte) *scoringRules {
	var raw json.RawMessage
	if len(overrides) > 0 {
		var ov map[string]json.RawMessage
		if err := json.Unmarshal(overrides, &ov); err == nil {
			raw = ov["scoring_rules"]
		}
	}
	if raw == nil && len(base) > 0 {
		var b map[string]json.RawMessage
		if err := json.Unmarshal(base, &b); err == nil {
			raw = b["scoring_rules"]
		}
	}
	if raw == nil {
		return nil
	}
	var sr scoringRules
	if err := json.Unmarshal(raw, &sr); err != nil {
		return nil
	}
	return &sr
}

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

		// 2. Guard CV and job-content presence, in that order, BEFORE the
		// usage-limit check and enqueue — both would otherwise waste an
		// evaluation slot and LLM tokens on an evaluation that can't run.
		user, err := q.GetUserByID(ctx, userID)
		if err != nil {
			return fmt.Errorf("get user: %w", err)
		}
		if isBlank(user.CvMarkdown) {
			return ErrCVMissing
		}
		if isBlank(job.ScrapedContent) {
			return ErrJobContentMissing
		}

		// 2b. Guard posting age against the user's own opt-in scoring_rules
		// (a no-op when unset or when the posting date is unknown — this
		// never blocks evaluation unless the user explicitly configured it).
		if sr := extractScoringRules(user.ProfileJson, user.ProfileOverrides); sr != nil && sr.MaxPostingAgeDays != nil && job.ReceivedAt.Valid {
			ageDays := int(time.Since(job.ReceivedAt.Time).Hours() / 24)
			if ageDays > *sr.MaxPostingAgeDays {
				return ErrStalePosting
			}
		}

		// 3. Check usage limit for free plan.
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
		return "", err
	}

	// 4. Enqueue evaluate-job (outside the tenant tx — pgboss.job has no RLS policy).
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

// isBlank reports whether a nullable string column is NULL or contains only
// whitespace.
func isBlank(s sql.NullString) bool {
	return !s.Valid || strings.TrimSpace(s.String) == ""
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
