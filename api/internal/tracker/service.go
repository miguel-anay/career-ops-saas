package tracker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
	"github.com/miguel-anay/career-ops-saas/api/internal/platform"
)

// ValidStatuses contains the allowed application status values.
var ValidStatuses = []string{
	"Evaluated", "Applied", "Responded", "Interview",
	"Offer", "Rejected", "Discarded", "SKIP",
}

// ErrNotFound is returned when an application does not exist for this user.
var ErrNotFound = errors.New("not found")

// ErrInvalidStatus is returned when an invalid status value is provided.
var ErrInvalidStatus = errors.New("invalid status")

// Service contains business logic for the tracker domain.
type Service struct {
	pool *pgxpool.Pool
}

// NewService creates a new tracker Service.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// ListApplications returns a paginated list of applications for the given user.
//
// The read runs inside a platform.WithTenantTx scoped to userID, so RLS
// scopes ListApplicationsByUser to the caller's own rows at the DB layer
// (in addition to the existing app-layer user_id filter).
func (s *Service) ListApplications(ctx context.Context, userID uuid.UUID, page, limit int) ([]db.Application, error) {
	if limit <= 0 {
		limit = 20
	}
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit

	var apps []db.Application
	err := platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
		var err error
		apps, err = q.ListApplicationsByUser(ctx, db.ListApplicationsByUserParams{
			UserID: userID,
			Limit:  int32(limit),
			Offset: int32(offset),
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list applications: %w", err)
	}
	return apps, nil
}

// UpdateApplication updates the status and/or notes of an application.
// Only non-nil fields are updated. At least one of status or notes must be provided.
//
// The UPDATE statement(s) run inside a platform.WithTenantTx scoped to
// userID, so RLS USING/WITH CHECK is the guard: a row owned by another
// tenant is invisible to the UPDATE's target scan (0 rows affected ->
// sql.ErrNoRows -> ErrNotFound), and WITH CHECK rejects any attempt to
// write a foreign user_id. Per design.md D8, the post-UPDATE
// `if updated.UserID != userID` check is intentionally DROPPED — it used to
// run AFTER an unscoped UPDATE had already mutated the row, so it was
// dead-for-security even before this change; RLS is now the only guard and
// it is unconditional.
func (s *Service) UpdateApplication(ctx context.Context, userID, appID uuid.UUID, status *string, notes *string) (*db.Application, error) {
	if status == nil && notes == nil {
		return nil, fmt.Errorf("at least one of status or notes must be provided")
	}

	if status != nil {
		if !isValidStatus(*status) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidStatus, *status)
		}
	}

	var updated db.Application

	err := platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
		var err error

		if status != nil {
			updated, err = q.UpdateApplicationStatus(ctx, db.UpdateApplicationStatusParams{
				ID:     appID,
				Status: db.AppStatusT(*status),
			})
			if err != nil {
				return err
			}
		}

		if notes != nil {
			updated, err = q.UpdateApplicationNotes(ctx, db.UpdateApplicationNotesParams{
				ID:    appID,
				Notes: sql.NullString{String: *notes, Valid: true},
			})
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update application: %w", err)
	}

	return &updated, nil
}

// isValidStatus returns true if the given status is in ValidStatuses.
func isValidStatus(status string) bool {
	for _, s := range ValidStatuses {
		if s == status {
			return true
		}
	}
	return false
}
