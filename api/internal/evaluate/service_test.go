package evaluate_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/evaluate"
	"github.com/miguel-anay/career-ops-saas/api/internal/testsupport/rlsdb"
	"github.com/stretchr/testify/require"
)

// TestEnqueueEvaluation_CVMissing proves the spec scenario "User has no
// CV": EnqueueEvaluation returns evaluate.ErrCVMissing (before the
// usage-limit check or any enqueue) when users.cv_markdown is NULL, and no
// evaluate-job pgboss job is created.
//
// DB-gated: skips cleanly when TEST_DATABASE_URL is unset (see rlsdb.New).
func TestEnqueueEvaluation_CVMissing(t *testing.T) {
	ctx := context.Background()
	h := rlsdb.New(ctx, t)
	h.EnsurePgbossSchema(ctx, t, "evaluate-job")

	userID := h.SeedUser(ctx, t, "evaluate-itest-cv-missing@test.invalid", "evaluate_itest_cv_missing")
	jobID := mustSeedJobWithScrapedContent(ctx, t, h, userID, "Some real job description content")

	svc := evaluate.NewService(h.AppPool)

	before := countEvaluateJobs(ctx, t, h)
	queueID, err := svc.EnqueueEvaluation(ctx, userID, jobID)
	require.ErrorIs(t, err, evaluate.ErrCVMissing)
	require.Empty(t, queueID)

	after := countEvaluateJobs(ctx, t, h)
	require.Equal(t, before, after, "no evaluate-job must be enqueued when CV is missing")
}

// TestEnqueueEvaluation_JobContentMissing proves the spec scenario
// "Manual/email job with no scraped content": EnqueueEvaluation returns
// evaluate.ErrJobContentMissing when the job's scraped_content is NULL,
// even though the user has a CV, and no evaluate-job job is created.
func TestEnqueueEvaluation_JobContentMissing(t *testing.T) {
	ctx := context.Background()
	h := rlsdb.New(ctx, t)
	h.EnsurePgbossSchema(ctx, t, "evaluate-job")

	userID := h.SeedUser(ctx, t, "evaluate-itest-jd-missing@test.invalid", "evaluate_itest_jd_missing")
	mustSetCVMarkdown(ctx, t, h, userID, "# My CV\n\nExperience...")
	jobID := mustSeedJobWithScrapedContent(ctx, t, h, userID, "")

	svc := evaluate.NewService(h.AppPool)

	before := countEvaluateJobs(ctx, t, h)
	queueID, err := svc.EnqueueEvaluation(ctx, userID, jobID)
	require.ErrorIs(t, err, evaluate.ErrJobContentMissing)
	require.Empty(t, queueID)

	after := countEvaluateJobs(ctx, t, h)
	require.Equal(t, before, after, "no evaluate-job must be enqueued when job content is missing")
}

func mustSeedJobWithScrapedContent(ctx context.Context, t *testing.T, h *rlsdb.Harness, userID uuid.UUID, scrapedContent string) uuid.UUID {
	t.Helper()
	jobID := uuid.New()
	_, err := h.AdminPool.Exec(ctx, `
		INSERT INTO jobs (id, user_id, title, company, url, status, scraped_content)
		VALUES ($1, $2, 'Pending', 'Unknown', $3, 'new', NULLIF($4, ''))`,
		jobID, userID, "https://boards.greenhouse.io/acme/jobs/"+uuid.New().String(), scrapedContent)
	require.NoError(t, err, "seed job")
	return jobID
}

func mustSetCVMarkdown(ctx context.Context, t *testing.T, h *rlsdb.Harness, userID uuid.UUID, cvMarkdown string) {
	t.Helper()
	_, err := h.AdminPool.Exec(ctx, `UPDATE users SET cv_markdown = $2 WHERE id = $1`, userID, cvMarkdown)
	require.NoError(t, err, "seed users.cv_markdown")
}

func countEvaluateJobs(ctx context.Context, t *testing.T, h *rlsdb.Harness) int {
	t.Helper()
	var count int
	err := h.AdminPool.QueryRow(ctx, `SELECT count(*) FROM pgboss.job WHERE name = 'evaluate-job'`).Scan(&count)
	require.NoError(t, err)
	return count
}
