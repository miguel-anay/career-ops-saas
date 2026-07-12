package profile

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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

// ErrAlreadyUndone is returned when UndoEdit targets an edit whose status
// is already "undone" — repeating the undo would re-drop whatever key
// currently occupies fieldPath, which may by then belong to a later edit.
var ErrAlreadyUndone = errors.New("edit already undone")

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
// top-level key). Both args are raw jsonb bytes from users. Malformed/empty
// input degrades to an empty map for that side — this never fails, so it
// takes no error return (there is nothing a caller could do differently).
func mergeProfile(base, overrides []byte) map[string]json.RawMessage {
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
	return out
}

// ProfileEditView is the wire shape for a profile_edits row — plain JSON
// values for old_value/new_value instead of db.ProfileEdit's sqlc-generated
// pqtype.NullRawMessage/sql.NullTime wrappers, which have no custom
// MarshalJSON and would otherwise serialize as {"RawMessage":...,"Valid":...}.
type ProfileEditView struct {
	ID         uuid.UUID       `json:"id"`
	FieldPath  string          `json:"field_path"`
	OldValue   json.RawMessage `json:"old_value,omitempty"`
	NewValue   json.RawMessage `json:"new_value,omitempty"`
	Source     string          `json:"source"`
	Status     string          `json:"status"`
	CreatedAt  time.Time       `json:"created_at"`
	ResolvedAt *time.Time      `json:"resolved_at,omitempty"`
}

func toProfileEditView(e db.ProfileEdit) ProfileEditView {
	v := ProfileEditView{
		ID:        e.ID,
		FieldPath: e.FieldPath,
		Source:    e.Source,
		Status:    e.Status,
		CreatedAt: e.CreatedAt,
	}
	if e.OldValue.Valid {
		v.OldValue = e.OldValue.RawMessage
	}
	if e.NewValue.Valid {
		v.NewValue = e.NewValue.RawMessage
	}
	if e.ResolvedAt.Valid {
		v.ResolvedAt = &e.ResolvedAt.Time
	}
	return v
}

// EffectiveProfile is the GET /api/me/profile response shape.
type EffectiveProfile struct {
	CVMarkdown string                     `json:"cv_markdown"`
	Profile    map[string]json.RawMessage `json:"profile"`
	Edits      []ProfileEditView          `json:"edits"`
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
		merged := mergeProfile(u.ProfileJson, u.ProfileOverrides)

		edits, err := q.ListProfileEditsByUser(ctx, userID)
		if err != nil {
			return err
		}
		views := make([]ProfileEditView, len(edits))
		for i, e := range edits {
			views[i] = toProfileEditView(e)
		}

		result = EffectiveProfile{
			CVMarkdown: u.CvMarkdown.String,
			Profile:    merged,
			Edits:      views,
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

// currentKey returns the effective value of fieldPath — an override wins
// over the CV-derived value — without merging every other top-level key
// just to read the one that's needed.
func currentKey(base, overrides []byte, fieldPath string) pqtype.NullRawMessage {
	if len(overrides) > 0 {
		var ov map[string]json.RawMessage
		if err := json.Unmarshal(overrides, &ov); err == nil {
			if v, ok := ov[fieldPath]; ok {
				return pqtype.NullRawMessage{RawMessage: v, Valid: true}
			}
		}
	}
	if len(base) > 0 {
		var b map[string]json.RawMessage
		if err := json.Unmarshal(base, &b); err == nil {
			if v, ok := b[fieldPath]; ok {
				return pqtype.NullRawMessage{RawMessage: v, Valid: true}
			}
		}
	}
	return pqtype.NullRawMessage{}
}

// ApplyOverride writes the given top-level key into users.profile_overrides
// AND inserts a profile_edits ledger row (source=manual, status=accepted)
// atomically in ONE platform.WithTenantTx (design D5).
func (s *Service) ApplyOverride(ctx context.Context, userID uuid.UUID, fieldPath string, value json.RawMessage) (*ProfileEditView, error) {
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
	view := toProfileEditView(edit)
	return &view, nil
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
		// A second undo on an already-undone edit must not silently
		// "succeed" again — it would re-drop whatever key currently
		// occupies fieldPath, which by then may belong to an unrelated,
		// later edit of the same field.
		if edit.Status == "undone" {
			return ErrAlreadyUndone
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
		if errors.Is(err, ErrAlreadyUndone) {
			return ErrAlreadyUndone
		}
		return fmt.Errorf("undo edit: %w", err)
	}
	return nil
}
