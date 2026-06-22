package companies_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/companies"
	"github.com/miguel-anay/career-ops-saas/api/internal/testsupport/rlsdb"
	"github.com/stretchr/testify/require"
)

// TestCompaniesRLS_Integration proves that companies.Service.List/Add/Remove
// are gated by Postgres RLS at the DB layer, not merely an app-layer
// ownership check running after an unscoped query.
//
// Remove is the highest-severity case: DeleteWatchedCompany has NO
// `WHERE user_id` clause in the sqlc query itself, so RLS is the ONLY
// guard preventing a non-owner from deleting another tenant's row. This
// test asserts that a cross-tenant Remove call deletes zero rows and that
// the target row is verified still present via AdminPool (ground truth,
// bypasses RLS) after the attempt.
//
// Mocked Servicer tests (handler_test.go) cannot prove RLS — that is a
// database-layer invariant, exercised here against a real app_user
// connection via the shared rlsdb harness.
//
// Skips cleanly when TEST_DATABASE_URL is unset. To run against a live DB:
//
//	TEST_DATABASE_URL="postgres://app_user:app_pw@localhost:5432/careerops?sslmode=disable" \
//	  go test ./internal/companies/... -run TestCompaniesRLS_Integration -v
func TestCompaniesRLS_Integration(t *testing.T) {
	ctx := context.Background()
	h := rlsdb.New(ctx, t)

	userA := h.SeedUser(ctx, t, "companies-itest-a@test.invalid", "companies_itest_google_a")
	userB := h.SeedUser(ctx, t, "companies-itest-b@test.invalid", "companies_itest_google_b")

	// SeedUser upserts the same user across repeated test runs, so clear
	// out any watched_companies rows left over from a prior run before
	// seeding fresh fixtures — keeps the List-count assertions deterministic.
	_, err := h.AdminPool.Exec(ctx, `DELETE FROM watched_companies WHERE user_id IN ($1, $2)`, userA, userB)
	require.NoError(t, err, "clear stale fixtures via AdminPool")

	// Seed a watched_companies row for A and one for B via AdminPool
	// (ground truth, bypasses RLS exactly like a prior signup/setup would).
	companyA := uuid.New()
	_, err = h.AdminPool.Exec(ctx, `
		INSERT INTO watched_companies (id, user_id, name, careers_url, provider_id, enabled)
		VALUES ($1, $2, 'Acme Corp', 'https://boards.greenhouse.io/acme', 'greenhouse', true)`,
		companyA, userA)
	require.NoError(t, err, "seed company for A via AdminPool")

	companyB := uuid.New()
	_, err = h.AdminPool.Exec(ctx, `
		INSERT INTO watched_companies (id, user_id, name, careers_url, provider_id, enabled)
		VALUES ($1, $2, 'Beta Inc', 'https://jobs.lever.co/beta', 'lever', true)`,
		companyB, userB)
	require.NoError(t, err, "seed company for B via AdminPool")

	svc := companies.NewService(h.AppPool)

	t.Run("RLS isolation: List returns only the caller's own companies", func(t *testing.T) {
		listB, err := svc.List(ctx, userB)
		require.NoError(t, err)
		require.Len(t, listB, 1)
		require.Equal(t, companyB, listB[0].ID)
		for _, c := range listB {
			require.NotEqual(t, companyA, c.ID, "B's List must never include A's company")
		}
	})

	t.Run("RLS isolation: Remove cannot delete another tenant's row", func(t *testing.T) {
		err := svc.Remove(ctx, userB, companyA)
		require.Error(t, err, "B's Remove against A's company ID must not succeed")

		var stillPresent bool
		err = h.AdminPool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM watched_companies WHERE id = $1)`, companyA,
		).Scan(&stillPresent)
		require.NoError(t, err)
		require.True(t, stillPresent, "A's row must still be present via AdminPool after B's cross-tenant Remove attempt")
	})

	t.Run("owner Add/List/Remove still succeed", func(t *testing.T) {
		created, err := svc.Add(ctx, userA, "Gamma LLC", "https://gamma.recruitee.com", "")
		require.NoError(t, err)
		require.Equal(t, userA, created.UserID)

		listA, err := svc.List(ctx, userA)
		require.NoError(t, err)
		ids := make([]uuid.UUID, 0, len(listA))
		for _, c := range listA {
			ids = append(ids, c.ID)
		}
		require.Contains(t, ids, companyA)
		require.Contains(t, ids, created.ID)
		require.NotContains(t, ids, companyB, "A's List must never include B's company")

		err = svc.Remove(ctx, userA, companyA)
		require.NoError(t, err, "A's own Remove must succeed")

		var deleted bool
		err = h.AdminPool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM watched_companies WHERE id = $1)`, companyA,
		).Scan(&deleted)
		require.NoError(t, err)
		require.False(t, deleted, "A's own Remove must have actually deleted the row")
	})
}
