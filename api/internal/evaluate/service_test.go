package evaluate_test

import (
	"context"
	"testing"
	"time"

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

// TestEnqueueEvaluation_StalePosting proves the new scoring_rules-gated
// guard: EnqueueEvaluation returns evaluate.ErrStalePosting (before the
// usage-limit check or any enqueue) when the job's received_at exceeds the
// user's own scoring_rules.max_posting_age_days, and no evaluate-job is
// created.
func TestEnqueueEvaluation_StalePosting(t *testing.T) {
	ctx := context.Background()
	h := rlsdb.New(ctx, t)
	h.EnsurePgbossSchema(ctx, t, "evaluate-job")

	userID := h.SeedUser(ctx, t, "evaluate-itest-stale@test.invalid", "evaluate_itest_stale")
	mustSetCVMarkdown(ctx, t, h, userID, "# My CV\n\nExperience...")
	mustSetProfileOverrides(ctx, t, h, userID, `{"scoring_rules":{"max_posting_age_days":30}}`)
	jobID := mustSeedJobWithScrapedContent(ctx, t, h, userID, "Some real job description content")
	mustSetJobReceivedAt(ctx, t, h, jobID, time.Now().Add(-45*24*time.Hour)) // 45 days old > 30-day limit

	svc := evaluate.NewService(h.AppPool)

	before := countEvaluateJobs(ctx, t, h)
	queueID, err := svc.EnqueueEvaluation(ctx, userID, jobID)
	require.ErrorIs(t, err, evaluate.ErrStalePosting)
	require.Empty(t, queueID)

	after := countEvaluateJobs(ctx, t, h)
	require.Equal(t, before, after, "no evaluate-job must be enqueued when the posting is stale")
}

// TestEnqueueEvaluation_StalePosting_NoOpWhenUnset proves the guard is a
// true no-op (backward-compatible, byte-for-byte today's behavior) when the
// user never set scoring_rules — an old posting alone must never block
// evaluation on its own.
func TestEnqueueEvaluation_StalePosting_NoOpWhenUnset(t *testing.T) {
	ctx := context.Background()
	h := rlsdb.New(ctx, t)
	h.EnsurePgbossSchema(ctx, t, "evaluate-job")

	userID := h.SeedUser(ctx, t, "evaluate-itest-nostale@test.invalid", "evaluate_itest_nostale")
	mustSetCVMarkdown(ctx, t, h, userID, "# My CV\n\nExperience...")
	jobID := mustSeedJobWithScrapedContent(ctx, t, h, userID, "Some real job description content")
	mustSetJobReceivedAt(ctx, t, h, jobID, time.Now().Add(-200*24*time.Hour)) // very old, but no gate configured

	svc := evaluate.NewService(h.AppPool)

	queueID, err := svc.EnqueueEvaluation(ctx, userID, jobID)
	require.NoError(t, err, "without scoring_rules, posting age must never block evaluation")
	require.NotEmpty(t, queueID)
}

// TestEnqueueEvaluation_StalePosting_WithinLimit proves a posting younger
// than the configured limit is not gated.
func TestEnqueueEvaluation_StalePosting_WithinLimit(t *testing.T) {
	ctx := context.Background()
	h := rlsdb.New(ctx, t)
	h.EnsurePgbossSchema(ctx, t, "evaluate-job")

	userID := h.SeedUser(ctx, t, "evaluate-itest-fresh@test.invalid", "evaluate_itest_fresh")
	mustSetCVMarkdown(ctx, t, h, userID, "# My CV\n\nExperience...")
	mustSetProfileOverrides(ctx, t, h, userID, `{"scoring_rules":{"max_posting_age_days":30}}`)
	jobID := mustSeedJobWithScrapedContent(ctx, t, h, userID, "Some real job description content")
	mustSetJobReceivedAt(ctx, t, h, jobID, time.Now().Add(-5*24*time.Hour)) // 5 days old, within limit

	svc := evaluate.NewService(h.AppPool)

	queueID, err := svc.EnqueueEvaluation(ctx, userID, jobID)
	require.NoError(t, err, "a posting within max_posting_age_days must not be gated")
	require.NotEmpty(t, queueID)
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

func mustSetProfileOverrides(ctx context.Context, t *testing.T, h *rlsdb.Harness, userID uuid.UUID, overridesJSON string) {
	t.Helper()
	_, err := h.AdminPool.Exec(ctx, `UPDATE users SET profile_overrides = $2::jsonb WHERE id = $1`, userID, overridesJSON)
	require.NoError(t, err, "seed users.profile_overrides")
}

func mustSetJobReceivedAt(ctx context.Context, t *testing.T, h *rlsdb.Harness, jobID uuid.UUID, receivedAt time.Time) {
	t.Helper()
	_, err := h.AdminPool.Exec(ctx, `UPDATE jobs SET received_at = $2 WHERE id = $1`, jobID, receivedAt)
	require.NoError(t, err, "seed jobs.received_at")
}

func countEvaluateJobs(ctx context.Context, t *testing.T, h *rlsdb.Harness) int {
	t.Helper()
	var count int
	err := h.AdminPool.QueryRow(ctx, `SELECT count(*) FROM pgboss.job WHERE name = 'evaluate-job'`).Scan(&count)
	require.NoError(t, err)
	return count
}
