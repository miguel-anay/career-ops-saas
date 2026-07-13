package digest_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/digest"
	"github.com/miguel-anay/career-ops-saas/api/internal/testsupport/rlsdb"
	"github.com/stretchr/testify/require"
)

// TestDeleteDigest_NotOwned proves the T-304 scenario: a delete attempt
// against another user's article_digests row is denied by the DeleteDigest
// (:execrows) ownership guard (design.md Decision 4) — it must not succeed
// via either RLS or the app-layer `WHERE user_id` scope. Owner A's row must
// survive untouched.
//
// Skips cleanly when TEST_DATABASE_URL is unset (see rlsdb.New).
func TestDeleteDigest_NotOwned(t *testing.T) {
	ctx := context.Background()
	h := rlsdb.New(ctx, t)

	userA := h.SeedUser(ctx, t, "digest-itest-a@test.invalid", "digest_itest_google_a")
	userB := h.SeedUser(ctx, t, "digest-itest-b@test.invalid", "digest_itest_google_b")

	var digestID uuid.UUID
	err := h.AdminPool.QueryRow(ctx, `
		INSERT INTO article_digests (user_id, title, content_md)
		VALUES ($1, 'A Project', '# hero metrics')
		RETURNING id`,
		userA,
	).Scan(&digestID)
	require.NoError(t, err, "seed digest for A via AdminPool")

	svc := digest.NewService(h.AppPool)

	err = svc.DeleteDigest(ctx, userB, digestID)
	require.ErrorIs(t, err, digest.ErrNotFound, "B deleting A's row must be denied and mapped to ErrNotFound")

	var stillExists bool
	err = h.AdminPool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM article_digests WHERE id = $1)`, digestID).Scan(&stillExists)
	require.NoError(t, err)
	require.True(t, stillExists, "A's digest must still exist after B's failed delete attempt")
}

// TestDeleteDigest_Owner proves the owner's own delete succeeds (n==1 path)
// and the row is gone afterward.
func TestDeleteDigest_Owner(t *testing.T) {
	ctx := context.Background()
	h := rlsdb.New(ctx, t)

	userA := h.SeedUser(ctx, t, "digest-itest-owner-a@test.invalid", "digest_itest_owner_google_a")

	var digestID uuid.UUID
	err := h.AdminPool.QueryRow(ctx, `
		INSERT INTO article_digests (user_id, title, content_md)
		VALUES ($1, 'A Second Project', '# more hero metrics')
		RETURNING id`,
		userA,
	).Scan(&digestID)
	require.NoError(t, err, "seed digest for A via AdminPool")

	svc := digest.NewService(h.AppPool)

	err = svc.DeleteDigest(ctx, userA, digestID)
	require.NoError(t, err, "A's own delete of A's row must succeed")

	var stillExists bool
	err = h.AdminPool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM article_digests WHERE id = $1)`, digestID).Scan(&stillExists)
	require.NoError(t, err)
	require.False(t, stillExists, "A's own digest must be gone after the owner's delete")
}
