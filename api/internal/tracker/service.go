package tracker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
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

func (s *Service) queries() *db.Queries {
	sqlDB := stdlib.OpenDBFromPool(s.pool)
	return db.New(sqlDB)
}

// ListApplications returns a paginated list of applications for the given user.
func (s *Service) ListApplications(ctx context.Context, userID uuid.UUID, page, limit int) ([]db.Application, error) {
	if limit <= 0 {
		limit = 20
	}
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit

	q := s.queries()
	apps, err := q.ListApplicationsByUser(ctx, db.ListApplicationsByUserParams{
		UserID: userID,
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("list applications: %w", err)
	}
	return apps, nil
}

// UpdateApplication updates the status and/or notes of an application.
// Only non-nil fields are updated. At least one of status or notes must be provided.
func (s *Service) UpdateApplication(ctx context.Context, userID, appID uuid.UUID, status *string, notes *string) (*db.Application, error) {
	if status == nil && notes == nil {
		return nil, fmt.Errorf("at least one of status or notes must be provided")
	}

	if status != nil {
		if !isValidStatus(*status) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidStatus, *status)
		}
	}

	q := s.queries()

	var updated db.Application
	var err error

	if status != nil && notes != nil {
		// Update status first, then notes — simple sequential updates.
		updated, err = q.UpdateApplicationStatus(ctx, db.UpdateApplicationStatusParams{
			ID:     appID,
			Status: db.AppStatusT(*status),
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, fmt.Errorf("update status: %w", err)
		}
		if updated.UserID != userID {
			return nil, ErrNotFound
		}
		updated, err = q.UpdateApplicationNotes(ctx, db.UpdateApplicationNotesParams{
			ID:    appID,
			Notes: sql.NullString{String: *notes, Valid: true},
		})
		if err != nil {
			return nil, fmt.Errorf("update notes: %w", err)
		}
	} else if status != nil {
		updated, err = q.UpdateApplicationStatus(ctx, db.UpdateApplicationStatusParams{
			ID:     appID,
			Status: db.AppStatusT(*status),
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, fmt.Errorf("update status: %w", err)
		}
		if updated.UserID != userID {
			return nil, ErrNotFound
		}
	} else {
		updated, err = q.UpdateApplicationNotes(ctx, db.UpdateApplicationNotesParams{
			ID:    appID,
			Notes: sql.NullString{String: *notes, Valid: true},
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, fmt.Errorf("update notes: %w", err)
		}
		if updated.UserID != userID {
			return nil, ErrNotFound
		}
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
