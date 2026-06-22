package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
	"github.com/miguel-anay/career-ops-saas/api/internal/platform"
)

// ErrNotFound is returned when a requested resource does not exist or
// belongs to a different user.
var ErrNotFound = errors.New("not found")

// Service contains business logic for the jobs domain.
type Service struct {
	pool *pgxpool.Pool
}

// NewService creates a new jobs Service.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// AddManual validates a URL, detects the platform, and upserts the job for the user.
func (s *Service) AddManual(ctx context.Context, userID uuid.UUID, rawURL string) (*db.Job, error) {
	if !strings.HasPrefix(rawURL, "https://") {
		return nil, fmt.Errorf("URL must start with https://")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	platformID := detectPlatform(parsed.Hostname())

	var job db.Job
	err = platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
		r := newRepoFromQueries(q)
		j, err := r.UpsertByURL(ctx, db.UpsertJobByURLParams{
			UserID:  userID,
			Title:   "Pending",
			Company: "Unknown",
			Url:     rawURL,
			Platform: sql.NullString{
				String: platformID,
				Valid:  platformID != "unknown",
			},
			Status:         db.JobStatusTNew,
			ScrapedContent: sql.NullString{},
			ReceivedAt:     sql.NullTime{},
		})
		if err != nil {
			return err
		}
		job = j
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("upsert job: %w", err)
	}
	return &job, nil
}

// List returns a paginated list of jobs for the given user.
func (s *Service) List(ctx context.Context, userID uuid.UUID, page, limit int) ([]db.Job, error) {
	var jobsList []db.Job
	err := platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
		r := newRepoFromQueries(q)
		l, err := r.ListByUser(ctx, userID, page, limit)
		if err != nil {
			return err
		}
		jobsList = l
		return nil
	})
	if err != nil {
		return nil, err
	}
	return jobsList, nil
}

// GetByID returns the job for the user. Returns ErrNotFound if the job belongs
// to a different user (RLS enforces this at the DB level; we return a clean error).
func (s *Service) GetByID(ctx context.Context, userID uuid.UUID, jobID uuid.UUID) (*db.Job, error) {
	var job db.Job
	err := platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
		r := newRepoFromQueries(q)
		j, err := r.GetByID(ctx, jobID)
		if err != nil {
			return err
		}
		job = j
		return nil
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get job: %w", err)
	}
	// Double-check ownership even though RLS handles it (defense-in-depth,
	// consistent with scan.GetScanRun's kept app-layer check).
	if job.UserID != userID {
		return nil, ErrNotFound
	}
	return &job, nil
}

// detectPlatform infers the ATS platform from a URL hostname.
func detectPlatform(hostname string) string {
	hostname = strings.ToLower(hostname)
	switch {
	case strings.Contains(hostname, "greenhouse.io"):
		return "greenhouse"
	case strings.Contains(hostname, "ashby.com") || strings.Contains(hostname, "ashbyhq.com"):
		return "ashby"
	case strings.Contains(hostname, "lever.co"):
		return "lever"
	case strings.Contains(hostname, "recruitee.com"):
		return "recruitee"
	case strings.Contains(hostname, "smartrecruiters.com"):
		return "smartrecruiters"
	case strings.Contains(hostname, "workable.com"):
		return "workable"
	default:
		return "unknown"
	}
}
