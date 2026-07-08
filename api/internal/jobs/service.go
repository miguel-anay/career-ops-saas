package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
	"github.com/miguel-anay/career-ops-saas/api/internal/platform"
	"github.com/miguel-anay/career-ops-saas/api/internal/queue"
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

// allowedHostPatterns mirrors worker/lib/url-normalize.mjs HOST_RULES — the
// SSRF allowlist for fetch-job-content. Only URLs on these hosts are eligible
// for Playwright-based content fetching; non-allowlisted URLs are stored but
// never enqueued (they stay with NULL scraped_content and get 422 from
// evaluation-quality's ErrJobContentMissing guard).
var allowedHostPatterns = []struct {
	platform string
	re       *regexp.Regexp
}{
	{platform: "linkedin", re: regexp.MustCompile(`(^|\.)linkedin\.com$`)},
	{platform: "indeed", re: regexp.MustCompile(`(^|\.)indeed\.com$`)},
	{platform: "computrabajo", re: regexp.MustCompile(`(^|\.)computrabajo\.com(\.\w+)?$`)},
	{platform: "bumeran", re: regexp.MustCompile(`(^|\.)bumeran\.com(\.\w+)?$`)},
}

// lookupAllowedHost checks whether a hostname matches an entry in the SSRF
// allowlist. Returns the platform name and true if matched.
func lookupAllowedHost(hostname string) (string, bool) {
	hostname = strings.ToLower(hostname)
	for _, entry := range allowedHostPatterns {
		if entry.re.MatchString(hostname) {
			return entry.platform, true
		}
	}
	return "", false
}

// AddManual validates a URL, detects the platform, upserts the job, and
// enqueues fetch-job-content if the host is in the SSRF allowlist.
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

	// SSRF gate: only enqueue fetch-job-content for known allowlisted hosts.
	// The upsert succeeds regardless — non-allowlisted hosts are still stored
	// (existing behavior preserved), but Playwright never navigates there.
	if _, ok := lookupAllowedHost(parsed.Hostname()); ok {
		payload, err := json.Marshal(fetchJobContentPayload{
			UserID: userID,
			JobID:  job.ID,
		})
		if err != nil {
			return &job, fmt.Errorf("marshal fetch-job-content payload: %w", err)
		}
		if err := queue.Enqueue(ctx, s.pool, queue.Job{
			Name: "fetch-job-content",
			Data: json.RawMessage(payload),
		}); err != nil {
			return &job, fmt.Errorf("enqueue fetch-job-content: %w", err)
		}
	}

	return &job, nil
}

// fetchJobContentPayload is the pg-boss job payload for "fetch-job-content".
type fetchJobContentPayload struct {
	UserID uuid.UUID `json:"user_id"`
	JobID  uuid.UUID `json:"job_id"`
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
