package cv_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/cv"
	"github.com/miguel-anay/career-ops-saas/api/internal/testsupport/rlsdb"
	"github.com/stretchr/testify/require"
)

// TestCVRLS_Integration proves that the 5 cv.Service methods that were NOT
// part of the earlier ingest-cv RLS wiring (ListCVs, CreateCV, SetMasterCV,
// EnqueuePDFGeneration, GetDownloadURL) are gated by Postgres RLS at the DB
// layer, not merely an app-layer ownership check running after an unscoped
// query.
//
// EnqueueIngest/GetIngestion are already correctly wired via
// platform.WithTenantTx (Seam 1, T-127) and are NOT re-tested here — see
// ingest_integration_test.go.
//
// Mocked Servicer tests (handler_test.go) cannot prove RLS — that is a
// database-layer invariant, exercised here against a real app_user
// connection via the shared rlsdb harness.
//
// Skips cleanly when TEST_DATABASE_URL is unset. To run against a live DB:
//
//	TEST_DATABASE_URL="postgres://app_user:app_pw@localhost:5432/careerops?sslmode=disable" \
//	  go test ./internal/cv/... -run TestCVRLS_Integration -v
func TestCVRLS_Integration(t *testing.T) {
	ctx := context.Background()
	h := rlsdb.New(ctx, t)
	h.EnsurePgbossStandin(ctx, t)

	userA := h.SeedUser(ctx, t, "cv-itest-a@test.invalid", "cv_itest_google_a")
	userB := h.SeedUser(ctx, t, "cv-itest-b@test.invalid", "cv_itest_google_b")

	// SeedUser upserts the same user across repeated test runs; clear any
	// stale fixtures owned by A or B before seeding fresh ones, so the
	// List/SetMasterCV assertions are deterministic on re-run.
	_, err := h.AdminPool.Exec(ctx, `DELETE FROM applications WHERE user_id IN ($1, $2)`, userA, userB)
	require.NoError(t, err, "clear stale applications fixtures via AdminPool")
	_, err = h.AdminPool.Exec(ctx, `DELETE FROM jobs WHERE user_id IN ($1, $2)`, userA, userB)
	require.NoError(t, err, "clear stale jobs fixtures via AdminPool")
	_, err = h.AdminPool.Exec(ctx, `DELETE FROM cvs WHERE user_id IN ($1, $2)`, userA, userB)
	require.NoError(t, err, "clear stale cvs fixtures via AdminPool")

	// Seed a cvs row owned by A (master CV) via AdminPool (ground truth,
	// bypasses RLS exactly like a prior signup/setup would).
	cvA := uuid.New()
	_, err = h.AdminPool.Exec(ctx, `
		INSERT INTO cvs (id, user_id, title, content_md, is_master)
		VALUES ($1, $2, 'A Master CV', '# A CV body', true)`,
		cvA, userA)
	require.NoError(t, err, "seed CV for A via AdminPool")

	// Seed a job + application + report chain owned by A, with a pdf_path,
	// so EnqueuePDFGeneration's read chain and GetDownloadURL have something
	// to read.
	jobA := uuid.New()
	_, err = h.AdminPool.Exec(ctx, `
		INSERT INTO jobs (id, user_id, title, company, url, status)
		VALUES ($1, $2, 'Backend Engineer', 'Acme', 'https://boards.greenhouse.io/acme/jobs/cv-itest-1', 'evaluated')`,
		jobA, userA)
	require.NoError(t, err, "seed job for A via AdminPool")

	appA := uuid.New()
	pdfPath := "cv-pdfs/" + userA.String() + "/" + appA.String() + ".pdf"
	_, err = h.AdminPool.Exec(ctx, `
		INSERT INTO applications (id, user_id, job_id, score, status, pdf_path)
		VALUES ($1, $2, $3, 0.9, 'Evaluated', $4)`,
		appA, userA, jobA, pdfPath)
	require.NoError(t, err, "seed application for A via AdminPool")

	_, err = h.AdminPool.Exec(ctx, `
		INSERT INTO reports (id, user_id, application_id, content_md, blocks_json)
		VALUES (gen_random_uuid(), $1, $2, '# Report body', '[]'::jsonb)`,
		userA, appA)
	require.NoError(t, err, "seed report for A via AdminPool")

	svc := cv.NewService(h.AppPool)

	t.Run("RLS isolation: ListCVs returns only the caller's own CVs", func(t *testing.T) {
		listB, err := svc.ListCVs(ctx, userB)
		require.NoError(t, err)
		for _, c := range listB {
			require.NotEqual(t, cvA, c.ID, "B's ListCVs must never include A's CV")
		}
	})

	t.Run("RLS isolation: SetMasterCV cannot affect another tenant's row", func(t *testing.T) {
		err := svc.SetMasterCV(ctx, userB, cvA)
		require.NoError(t, err, "SetMasterCV with no matching rows under B's GUC is a no-op, not an error")

		var stillMaster bool
		err = h.AdminPool.QueryRow(ctx,
			`SELECT is_master FROM cvs WHERE id = $1`, cvA,
		).Scan(&stillMaster)
		require.NoError(t, err)
		require.True(t, stillMaster, "A's CV must still be master after B's cross-tenant SetMasterCV attempt")
	})

	t.Run("RLS isolation: GetDownloadURL denies cross-tenant read", func(t *testing.T) {
		_, _, err := svc.GetDownloadURL(ctx, nil, userB, jobA)
		require.ErrorIs(t, err, cv.ErrNotFound,
			"non-owner lookup must be denied by RLS (sql.ErrNoRows -> ErrNotFound), not an app-layer check")
	})

	t.Run("owner CreateCV/ListCVs/SetMasterCV/EnqueuePDFGeneration still succeed", func(t *testing.T) {
		created, err := svc.CreateCV(ctx, userA, "A Second CV", "# second body", false)
		require.NoError(t, err)
		require.Equal(t, userA, created.UserID)

		listA, err := svc.ListCVs(ctx, userA)
		require.NoError(t, err)
		ids := make([]uuid.UUID, 0, len(listA))
		for _, c := range listA {
			ids = append(ids, c.ID)
		}
		require.Contains(t, ids, cvA)
		require.Contains(t, ids, created.ID)

		err = svc.SetMasterCV(ctx, userA, created.ID)
		require.NoError(t, err, "A's own SetMasterCV must succeed")

		var newMaster bool
		err = h.AdminPool.QueryRow(ctx,
			`SELECT is_master FROM cvs WHERE id = $1`, created.ID,
		).Scan(&newMaster)
		require.NoError(t, err)
		require.True(t, newMaster, "A's own SetMasterCV must have actually flipped is_master")

		queueID, err := svc.EnqueuePDFGeneration(ctx, userA, jobA)
		require.NoError(t, err, "A's own EnqueuePDFGeneration must succeed")
		require.NotEmpty(t, queueID)
	})
}
