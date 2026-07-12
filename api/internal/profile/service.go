package profile

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sqlc-dev/pqtype"

	"github.com/miguel-anay/career-ops-saas/api/internal/db"
	"github.com/miguel-anay/career-ops-saas/api/internal/platform"
)

// ErrNotFound is returned when a profile_edits row does not exist for this
// user (including the RLS-invisible cross-tenant case — see UndoEdit).
var ErrNotFound = errors.New("not found")

// ErrInvalidFieldPath is returned when ApplyOverride is called with a
// fieldPath outside the fixed allowlist (see D5). Checked BEFORE any DB
// call, at the trust boundary.
var ErrInvalidFieldPath = errors.New("invalid field_path")

// allowedFieldPaths is the fixed set of top-level profile_json keys a
// manual PATCH may target (design D5). Anything else is rejected at the
// trust boundary before touching the DB.
var allowedFieldPaths = map[string]bool{
	"target_roles":  true,
	"salary_target": true,
	"narrative":     true,
	"candidate":     true,
	"deal_breakers": true,
	"comp_targets":  true,
}

func isAllowedFieldPath(fieldPath string) bool {
	return allowedFieldPaths[fieldPath]
}

// mergeProfile overlays override keys onto the base profile (shallow, per
// top-level key). Both args are raw jsonb bytes from users. Never errors —
// malformed/empty input degrades to an empty map for that side.
func mergeProfile(base, overrides []byte) (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	if len(base) > 0 {
		_ = json.Unmarshal(base, &out)
	}
	ov := map[string]json.RawMessage{}
	if len(overrides) > 0 {
		_ = json.Unmarshal(overrides, &ov)
	}
	for k, v := range ov {
		out[k] = v // whole-key replace
	}
	return out, nil
}

// EffectiveProfile is the GET /api/me/profile response shape.
type EffectiveProfile struct {
	CVMarkdown string                     `json:"cv_markdown"`
	Profile    map[string]json.RawMessage `json:"profile"`
	Edits      []db.ProfileEdit           `json:"edits"`
}

// Servicer is the interface that handlers depend on.
type Servicer interface {
	GetProfile(ctx context.Context, userID uuid.UUID) (*EffectiveProfile, error)
	ApplyOverride(ctx context.Context, userID uuid.UUID, fieldPath string, value json.RawMessage) (*db.ProfileEdit, error)
	UndoEdit(ctx context.Context, userID, editID uuid.UUID) error
}

// Service contains business logic for the profile domain.
type Service struct {
	pool *pgxpool.Pool
}

// NewService creates a new profile Service.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// GetProfile returns the effective (merged) profile plus the edits ledger,
// newest first.
func (s *Service) GetProfile(ctx context.Context, userID uuid.UUID) (*EffectiveProfile, error) {
	var result EffectiveProfile
	err := platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
		u, err := q.GetUserProfile(ctx, userID)
		if err != nil {
			return err
		}
		merged, err := mergeProfile(u.ProfileJson, u.ProfileOverrides)
		if err != nil {
			return err
		}

		edits, err := q.ListProfileEditsByUser(ctx, userID)
		if err != nil {
			return err
		}

		result = EffectiveProfile{
			CVMarkdown: u.CvMarkdown.String,
			Profile:    merged,
			Edits:      edits,
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get profile: %w", err)
	}
	return &result, nil
}

// currentKey returns the effective (merged) value of fieldPath, or nil if
// absent from both base and overrides.
func currentKey(base, overrides []byte, fieldPath string) pqtype.NullRawMessage {
	merged, _ := mergeProfile(base, overrides)
	v, ok := merged[fieldPath]
	if !ok {
		return pqtype.NullRawMessage{}
	}
	return pqtype.NullRawMessage{RawMessage: v, Valid: true}
}

// ApplyOverride writes the given top-level key into users.profile_overrides
// AND inserts a profile_edits ledger row (source=manual, status=accepted)
// atomically in ONE platform.WithTenantTx (design D5).
func (s *Service) ApplyOverride(ctx context.Context, userID uuid.UUID, fieldPath string, value json.RawMessage) (*db.ProfileEdit, error) {
	if !isAllowedFieldPath(fieldPath) {
		return nil, ErrInvalidFieldPath
	}

	var edit db.ProfileEdit
	err := platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
		u, err := q.GetUserProfile(ctx, userID)
		if err != nil {
			return err
		}
		oldVal := currentKey(u.ProfileJson, u.ProfileOverrides, fieldPath)

		if _, err := q.SetProfileOverrideKey(ctx, db.SetProfileOverrideKeyParams{
			ID:      userID,
			Column2: fieldPath,
			Column3: value,
		}); err != nil {
			return err
		}

		edit, err = q.InsertProfileEdit(ctx, db.InsertProfileEditParams{
			UserID:    userID,
			FieldPath: fieldPath,
			OldValue:  oldVal,
			NewValue:  pqtype.NullRawMessage{RawMessage: value, Valid: value != nil},
			Source:    "manual",
			Status:    "accepted",
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("apply override: %w", err)
	}
	return &edit, nil
}

// UndoEdit drops the override key that editID wrote and flips the ledger
// row's status to undone, atomically in ONE platform.WithTenantTx (design
// D5). GetProfileEdit is RLS-scoped: a cross-tenant editID resolves to
// sql.ErrNoRows (mapped to ErrNotFound) before any mutation, so an
// undo attempt on another user's edit never touches profile_overrides for
// either user.
func (s *Service) UndoEdit(ctx context.Context, userID, editID uuid.UUID) error {
	err := platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
		edit, err := q.GetProfileEdit(ctx, editID)
		if err != nil {
			return err
		}

		if _, err := q.DropProfileOverrideKey(ctx, db.DropProfileOverrideKeyParams{
			ID:      userID,
			Column2: edit.FieldPath,
		}); err != nil {
			return err
		}

		_, err = q.MarkProfileEditUndone(ctx, editID)
		return err
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("undo edit: %w", err)
	}
	return nil
}
