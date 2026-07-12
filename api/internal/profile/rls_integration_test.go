package profile_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
	"github.com/miguel-anay/career-ops-saas/api/internal/platform"
	"github.com/miguel-anay/career-ops-saas/api/internal/profile"
	"github.com/miguel-anay/career-ops-saas/api/internal/testsupport/rlsdb"
	"github.com/stretchr/testify/require"
)

// TestApplyOverride_Integration proves the T-284/285 scenario: ApplyOverride
// writes the override key AND inserts one profile_edits row atomically in a
// single platform.WithTenantTx (both committed together), and that a forced
// failure mid-transaction (same two writes, second one made to fail) leaves
// NEITHER write persisted.
//
// Skips cleanly when TEST_DATABASE_URL is unset (see rlsdb.New).
func TestApplyOverride_Integration(t *testing.T) {
	ctx := context.Background()
	h := rlsdb.New(ctx, t)

	userA := h.SeedUser(ctx, t, "profile-itest-a@test.invalid", "profile_itest_google_a")
	mustSetProfileJSON(ctx, t, h, userA, `{"target_roles":{"primary":["Backend Engineer"]}}`)

	svc := profile.NewService(h.AppPool)

	t.Run("both writes commit together", func(t *testing.T) {
		edit, err := svc.ApplyOverride(ctx, userA, "target_roles", json.RawMessage(`{"primary":["Staff Engineer"]}`))
		require.NoError(t, err)
		require.Equal(t, "target_roles", edit.FieldPath)
		require.Equal(t, "manual", edit.Source)
		require.Equal(t, "accepted", edit.Status)

		var overrides json.RawMessage
		err = h.AdminPool.QueryRow(ctx, `SELECT profile_overrides FROM users WHERE id = $1`, userA).Scan(&overrides)
		require.NoError(t, err)
		require.JSONEq(t, `{"target_roles":{"primary":["Staff Engineer"]}}`, string(overrides))

		var ledgerCount int
		err = h.AdminPool.QueryRow(ctx, `SELECT count(*) FROM profile_edits WHERE id = $1`, edit.ID).Scan(&ledgerCount)
		require.NoError(t, err)
		require.Equal(t, 1, ledgerCount, "the ledger row committed alongside the override write")
	})

	t.Run("forced failure mid-transaction leaves neither write persisted", func(t *testing.T) {
		userB := h.SeedUser(ctx, t, "profile-itest-b-atomic@test.invalid", "profile_itest_google_b_atomic")

		errForced := errors.New("forced failure after first write")
		err := platform.WithTenantTx(ctx, h.AppPool, userB, func(q *db.Queries) error {
			// Same first write ApplyOverride performs.
			if _, err := q.SetProfileOverrideKey(ctx, db.SetProfileOverrideKeyParams{
				ID:      userB,
				Column2: "narrative",
				Column3: json.RawMessage(`"forced"`),
			}); err != nil {
				return err
			}
			// Simulate the ledger insert failing.
			return errForced
		})
		require.ErrorIs(t, err, errForced)

		var overrides json.RawMessage
		qErr := h.AdminPool.QueryRow(ctx, `SELECT profile_overrides FROM users WHERE id = $1`, userB).Scan(&overrides)
		require.NoError(t, qErr)
		require.JSONEq(t, `{}`, string(overrides), "rolled-back tx must leave profile_overrides untouched")

		var ledgerCount int
		qErr = h.AdminPool.QueryRow(ctx, `SELECT count(*) FROM profile_edits WHERE user_id = $1`, userB).Scan(&ledgerCount)
		require.NoError(t, qErr)
		require.Equal(t, 0, ledgerCount, "rolled-back tx must leave no ledger row")
	})
}

// TestUndoEdit_Integration proves the T-286/287 scenarios: a cross-tenant
// undo 404s (RLS-scoped GetProfileEdit) with no mutation for either user,
// and the owning user's own undo drops the override key and flips the
// ledger row to undone.
func TestUndoEdit_Integration(t *testing.T) {
	ctx := context.Background()
	h := rlsdb.New(ctx, t)

	userA := h.SeedUser(ctx, t, "profile-itest-undo-a@test.invalid", "profile_itest_undo_google_a")
	userB := h.SeedUser(ctx, t, "profile-itest-undo-b@test.invalid", "profile_itest_undo_google_b")
	mustSetProfileJSON(ctx, t, h, userA, `{"narrative":"cv-derived narrative"}`)

	svc := profile.NewService(h.AppPool)

	edit, err := svc.ApplyOverride(ctx, userA, "narrative", json.RawMessage(`"manually edited narrative"`))
	require.NoError(t, err)

	t.Run("cross-tenant undo 404s with no mutation for either user", func(t *testing.T) {
		err := svc.UndoEdit(ctx, userB, edit.ID)
		require.ErrorIs(t, err, profile.ErrNotFound)

		var overridesA json.RawMessage
		qErr := h.AdminPool.QueryRow(ctx, `SELECT profile_overrides FROM users WHERE id = $1`, userA).Scan(&overridesA)
		require.NoError(t, qErr)
		require.JSONEq(t, `{"narrative":"manually edited narrative"}`, string(overridesA), "A's override must survive B's undo attempt")

		var status string
		qErr = h.AdminPool.QueryRow(ctx, `SELECT status FROM profile_edits WHERE id = $1`, edit.ID).Scan(&status)
		require.NoError(t, qErr)
		require.Equal(t, "accepted", status, "the ledger row must still be accepted after B's failed undo")
	})

	t.Run("owner's own undo drops the override key and flips the ledger row", func(t *testing.T) {
		err := svc.UndoEdit(ctx, userA, edit.ID)
		require.NoError(t, err)

		var overridesA json.RawMessage
		qErr := h.AdminPool.QueryRow(ctx, `SELECT profile_overrides FROM users WHERE id = $1`, userA).Scan(&overridesA)
		require.NoError(t, qErr)
		require.JSONEq(t, `{}`, string(overridesA), "narrative override key must be dropped")

		var status string
		var resolvedAtValid bool
		qErr = h.AdminPool.QueryRow(ctx, `SELECT status, resolved_at IS NOT NULL FROM profile_edits WHERE id = $1`, edit.ID).Scan(&status, &resolvedAtValid)
		require.NoError(t, qErr)
		require.Equal(t, "undone", status)
		require.True(t, resolvedAtValid, "resolved_at must be set")

		got, err := svc.GetProfile(ctx, userA)
		require.NoError(t, err)
		require.JSONEq(t, `"cv-derived narrative"`, string(got.Profile["narrative"]), "effective value falls back to profile_json after undo")
	})
}

func mustSetProfileJSON(ctx context.Context, t *testing.T, h *rlsdb.Harness, userID uuid.UUID, profileJSON string) {
	t.Helper()
	_, err := h.AdminPool.Exec(ctx, `UPDATE users SET profile_json = $2::jsonb WHERE id = $1`, userID, profileJSON)
	require.NoError(t, err, "seed users.profile_json")
}
