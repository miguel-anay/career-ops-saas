package evaluate_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/evaluate"
	"github.com/miguel-anay/career-ops-saas/api/internal/testsupport/rlsdb"
	"github.com/stretchr/testify/require"
)

// TestEvaluateRLS_Integration proves that evaluate.Service.GetReport's
// 3-step chain (GetJobByID -> GetApplicationByJobID -> GetReportByApplicationID)
// is gated by Postgres RLS at the DB layer — a non-owner's GetReport call
// returns not-found because RLS denies all three reads, not merely an
// app-layer ownership recheck running after an unscoped query.
//
// It also proves EnqueueEvaluation's job-lookup + usage-read still succeed
// for the owner once wired through a tenant tx, and that queue.Enqueue
// still runs (against the pgboss stand-in) after that tx commits.
//
// Mocked Servicer tests (handler_test.go) cannot prove RLS — that is a
// database-layer invariant, exercised here against a real app_user
// connection via the shared rlsdb harness.
//
// Skips cleanly when TEST_DATABASE_URL is unset. To run against a live DB:
//
//	TEST_DATABASE_URL="postgres://app_user:app_pw@localhost:5432/careerops?sslmode=disable" \
//	  go test ./internal/evaluate/... -run TestEvaluateRLS_Integration -v
func TestEvaluateRLS_Integration(t *testing.T) {
	ctx := context.Background()
	h := rlsdb.New(ctx, t)
	h.EnsurePgbossSchema(ctx, t, "evaluate-job")

	userA := h.SeedUser(ctx, t, "evaluate-itest-a@test.invalid", "evaluate_itest_google_a")
	userB := h.SeedUser(ctx, t, "evaluate-itest-b@test.invalid", "evaluate_itest_google_b")

	// Seed a jobs -> applications -> reports chain owned by A, via AdminPool
	// (ground truth, bypasses RLS exactly like production setup/migrations would).
	jobID := uuid.New()
	_, err := h.AdminPool.Exec(ctx, `
		INSERT INTO jobs (id, user_id, title, company, url, status, scraped_content)
		VALUES ($1, $2, 'Pending', 'Unknown', $3, 'new', 'Some real job description content')`,
		jobID, userA, "https://boards.greenhouse.io/acme/jobs/"+uuid.New().String())
	require.NoError(t, err, "seed job for A via AdminPool")

	applicationID := uuid.New()
	_, err = h.AdminPool.Exec(ctx, `
		INSERT INTO applications (id, user_id, job_id, status)
		VALUES ($1, $2, $3, 'Evaluated')`,
		applicationID, userA, jobID)
	require.NoError(t, err, "seed application for A via AdminPool")

	_, err = h.AdminPool.Exec(ctx, `
		INSERT INTO reports (id, user_id, application_id, content_md, blocks_json)
		VALUES ($1, $2, $3, '# Report', '{}'::jsonb)`,
		uuid.New(), userA, applicationID)
	require.NoError(t, err, "seed report for A via AdminPool")

	svc := evaluate.NewService(h.AppPool)

	t.Run("RLS isolation: non-owner GetReport returns not-found across the chain", func(t *testing.T) {
		_, err := svc.GetReport(ctx, userB, jobID)
		require.Error(t, err)
		require.ErrorIs(t, err, evaluate.ErrNotFound,
			"cross-tenant GetReport must surface ErrNotFound (RLS denial across jobs/applications/reports)")
	})

	t.Run("owner GetReport still succeeds", func(t *testing.T) {
		report, err := svc.GetReport(ctx, userA, jobID)
		require.NoError(t, err)
		require.Equal(t, userA, report.UserID)
		require.Equal(t, applicationID, report.ApplicationID)
	})

	t.Run("owner EnqueueEvaluation still succeeds (job lookup + usage read, then enqueue)", func(t *testing.T) {
		// The CV-missing content guard (evaluate.ErrCVMissing) runs before
		// the usage-limit check this subtest proves; seed a CV so this
		// remains a usage/enqueue test, not a guard test (covered by
		// TestEnqueueEvaluation_CVMissing in service_test.go).
		mustSetCVMarkdown(ctx, t, h, userA, "# CV\n\nExperience...")

		queueID, err := svc.EnqueueEvaluation(ctx, userA, jobID)
		require.NoError(t, err)
		require.NotEmpty(t, queueID)
	})
}
