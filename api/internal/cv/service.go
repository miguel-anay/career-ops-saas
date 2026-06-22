package cv

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
	"github.com/miguel-anay/career-ops-saas/api/internal/platform"
	"github.com/miguel-anay/career-ops-saas/api/internal/queue"
)

// ErrNotFound is returned when a CV, application, or report does not exist for this user.
var ErrNotFound = errors.New("not found")

// ErrNoPDFPath is returned when the application has no PDF path yet.
var ErrNoPDFPath = errors.New("PDF not yet generated")

// ErrUsageLimitExceeded is returned when the user has reached their free plan ingestion limit.
var ErrUsageLimitExceeded = errors.New("ingestion limit reached for free plan")

const freePlanIngestLimit = 5

// Service contains business logic for the cv domain.
type Service struct {
	pool *pgxpool.Pool
}

// NewService creates a new cv Service.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

func (s *Service) queries() *db.Queries {
	sqlDB := stdlib.OpenDBFromPool(s.pool)
	return db.New(sqlDB)
}

// generatePDFPayload is the pg-boss job payload for "generate-pdf".
type generatePDFPayload struct {
	UserID        uuid.UUID `json:"user_id"`
	JobID         uuid.UUID `json:"job_id"`
	ApplicationID uuid.UUID `json:"application_id"`
}

// EnqueuePDFGeneration checks prerequisites and enqueues a "generate-pdf" pg-boss job.
func (s *Service) EnqueuePDFGeneration(ctx context.Context, userID, jobID uuid.UUID) (string, error) {
	q := s.queries()

	// 1. Check application + report exist for this job.
	application, err := q.GetApplicationByJobID(ctx, jobID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("get application: %w", err)
	}
	if application.UserID != userID {
		return "", ErrNotFound
	}

	// Verify report exists.
	_, err = q.GetReportByApplicationID(ctx, application.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("no evaluation report found for this job")
		}
		return "", fmt.Errorf("get report: %w", err)
	}

	// 2. Get user's master CV id.
	_, err = q.GetMasterCVByUser(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("no master CV found for user")
		}
		return "", fmt.Errorf("get master CV: %w", err)
	}

	// 3. Enqueue generate-pdf.
	queueID := uuid.New()
	payload, err := json.Marshal(generatePDFPayload{
		UserID:        userID,
		JobID:         jobID,
		ApplicationID: application.ID,
	})
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}

	if err := queue.Enqueue(ctx, s.pool, queue.Job{
		Name: "generate-pdf",
		Data: json.RawMessage(payload),
	}); err != nil {
		return "", fmt.Errorf("enqueue generate-pdf: %w", err)
	}

	return queueID.String(), nil
}

// GetDownloadURL returns a signed R2 download URL for the PDF of the given job.
func (s *Service) GetDownloadURL(ctx context.Context, r2 *platform.R2Client, userID, jobID uuid.UUID) (string, time.Time, error) {
	q := s.queries()

	application, err := q.GetApplicationByJobID(ctx, jobID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", time.Time{}, ErrNotFound
		}
		return "", time.Time{}, fmt.Errorf("get application: %w", err)
	}
	if application.UserID != userID {
		return "", time.Time{}, ErrNotFound
	}
	if !application.PdfPath.Valid || application.PdfPath.String == "" {
		return "", time.Time{}, ErrNoPDFPath
	}

	expiry := 24 * time.Hour
	signedURL, err := r2.SignedDownloadURL(application.PdfPath.String, expiry)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("generate signed URL: %w", err)
	}

	expiresAt := time.Now().UTC().Add(expiry)
	return signedURL, expiresAt, nil
}

// ListCVs returns all CVs for the given user.
func (s *Service) ListCVs(ctx context.Context, userID uuid.UUID) ([]db.Cv, error) {
	q := s.queries()
	cvs, err := q.ListCVsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list CVs: %w", err)
	}
	return cvs, nil
}

// CreateCV inserts a new CV for the user.
func (s *Service) CreateCV(ctx context.Context, userID uuid.UUID, title, contentMd string, isMaster bool) (*db.Cv, error) {
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if contentMd == "" {
		return nil, fmt.Errorf("content_md is required")
	}

	q := s.queries()
	cvRecord, err := q.InsertCV(ctx, db.InsertCVParams{
		UserID:    userID,
		Title:     title,
		ContentMd: contentMd,
		IsMaster:  isMaster,
	})
	if err != nil {
		return nil, fmt.Errorf("insert CV: %w", err)
	}
	return &cvRecord, nil
}

// SetMasterCV sets the master CV for the user.
func (s *Service) SetMasterCV(ctx context.Context, userID, cvID uuid.UUID) error {
	q := s.queries()
	return q.SetMasterCV(ctx, db.SetMasterCVParams{
		ID:     cvID,
		UserID: userID,
	})
}

// ingestCVPayload is the pg-boss job payload for "ingest-cv".
type ingestCVPayload struct {
	UserID uuid.UUID `json:"user_id"`
	RunID  uuid.UUID `json:"run_id"`
	RawCV  string    `json:"raw_cv"`
}

// EnqueueIngest checks the free-plan ingestion usage limit, inserts a cv_ingestions
// row, increments usage.ingestions_count, and enqueues an "ingest-cv" pg-boss job.
// Returns the cv_ingestions row id (run_id).
//
// The usage check, the cv_ingestions insert, and the usage increment all run
// inside ONE tenant-scoped transaction (platform.WithTenantTx) so
// app.current_user_id is set for RLS and the three steps are atomic. The
// pg-boss enqueue happens AFTER that transaction commits, using the plain
// pool (pgboss.job has no RLS policy). If enqueue fails after commit, the
// cv_ingestions row and usage increment remain — an orphaned 'pending' row
// is acceptable for MVP.
//
// IMPORTANT: usage.ingestions_count is incremented HERE, at enqueue time, not
// in the worker. See apply-progress for the Seam-C note: T-102 (worker
// handleIngestCV) must NOT also increment usage, or counts would double.
func (s *Service) EnqueueIngest(ctx context.Context, userID uuid.UUID, rawCV string) (uuid.UUID, error) {
	month := time.Now().UTC().Format("2006-01")

	var runID uuid.UUID
	err := platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
		// 1. Check usage limit for free plan (current month).
		usage, err := q.GetUsageByUserMonth(ctx, db.GetUsageByUserMonthParams{
			UserID: userID,
			Month:  month,
		})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("get usage: %w", err)
		}
		// If no usage row exists yet, IngestionsCount is treated as 0.
		if !errors.Is(err, sql.ErrNoRows) && usage.IngestionsCount >= freePlanIngestLimit {
			return ErrUsageLimitExceeded
		}

		// 2. Insert cv_ingestions row.
		run, err := q.InsertCVIngestion(ctx, userID)
		if err != nil {
			return fmt.Errorf("insert ingestion: %w", err)
		}

		// 3. Increment usage.ingestions_count (UPSERT semantics).
		if _, err := q.UpsertIncrementIngestions(ctx, db.UpsertIncrementIngestionsParams{
			UserID: userID,
			Month:  month,
		}); err != nil {
			return fmt.Errorf("increment ingestions usage: %w", err)
		}

		runID = run.ID
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrUsageLimitExceeded) {
			return uuid.Nil, ErrUsageLimitExceeded
		}
		return uuid.Nil, err
	}

	// 4. Enqueue ingest-cv (outside the tenant tx — pgboss.job has no RLS policy).
	payload, err := json.Marshal(ingestCVPayload{
		UserID: userID,
		RunID:  runID,
		RawCV:  rawCV,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal payload: %w", err)
	}

	if err := queue.Enqueue(ctx, s.pool, queue.Job{
		Name: "ingest-cv",
		Data: json.RawMessage(payload),
	}); err != nil {
		return uuid.Nil, fmt.Errorf("enqueue ingest-cv: %w", err)
	}

	return runID, nil
}

// GetIngestion returns the cv_ingestions row for the given run id. The lookup
// runs inside a tenant-scoped transaction (platform.WithTenantTx) so RLS
// enforces isolation at the query layer: a non-owner's lookup returns
// sql.ErrNoRows (mapped to ErrNotFound), not because of an app-layer
// ownership check, but because the row is invisible under the caller's
// app.current_user_id.
func (s *Service) GetIngestion(ctx context.Context, userID, runID uuid.UUID) (*db.CvIngestion, error) {
	var ingestion db.CvIngestion
	err := platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
		row, err := q.GetCVIngestion(ctx, runID)
		if err != nil {
			return err
		}
		ingestion = row
		return nil
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get ingestion: %w", err)
	}
	return &ingestion, nil
}
