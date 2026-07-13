package digest

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/miguel-anay/career-ops-saas/api/internal/db"
	"github.com/miguel-anay/career-ops-saas/api/internal/platform"
)

// ErrNotFound is returned when a digest entry does not exist for this user
// (including the RLS-invisible cross-tenant case — see DeleteDigest).
var ErrNotFound = errors.New("not found")

// ErrValidation is returned when CreateDigest's input fails validation.
// Wrapped with a specific message via fmt.Errorf("...: %w", ErrValidation)
// so the handler can distinguish a 400 (bad input) from a 500 (everything
// else, e.g. a DB failure) via errors.Is — the same idiom every other
// domain package in this repo uses (see cv.ErrNotFound / cv.ErrNoPDFPath).
var ErrValidation = errors.New("validation failed")

// Service contains business logic for the article-digest domain.
type Service struct {
	pool *pgxpool.Pool
}

// NewService creates a new digest Service.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// ListDigests returns all article_digests rows for the given user, newest
// first.
func (s *Service) ListDigests(ctx context.Context, userID uuid.UUID) ([]db.ArticleDigest, error) {
	var digests []db.ArticleDigest
	err := platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
		rows, err := q.ListDigestsByUser(ctx, userID)
		if err != nil {
			return err
		}
		digests = rows
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list digests: %w", err)
	}
	return digests, nil
}

// CreateDigest inserts a new article_digests row for the user. Validation
// runs BEFORE platform.WithTenantTx, so an empty title/content_md never
// reaches the DB.
func (s *Service) CreateDigest(ctx context.Context, userID uuid.UUID, title, contentMd string) (*db.ArticleDigest, error) {
	if title == "" {
		return nil, fmt.Errorf("title is required: %w", ErrValidation)
	}
	if contentMd == "" {
		return nil, fmt.Errorf("content_md is required: %w", ErrValidation)
	}

	var digestRecord db.ArticleDigest
	err := platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
		row, err := q.InsertDigest(ctx, db.InsertDigestParams{
			UserID:    userID,
			Title:     title,
			ContentMd: contentMd,
		})
		if err != nil {
			return err
		}
		digestRecord = row
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("insert digest: %w", err)
	}
	return &digestRecord, nil
}

// DeleteDigest removes one article_digests row owned by userID. The delete
// query scopes on BOTH id and user_id (defense-in-depth alongside RLS) and
// is declared :execrows so the affected row count distinguishes "not
// found/not owned" (0 rows -> ErrNotFound) from a real delete.
func (s *Service) DeleteDigest(ctx context.Context, userID, digestID uuid.UUID) error {
	return platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
		n, err := q.DeleteDigest(ctx, db.DeleteDigestParams{ID: digestID, UserID: userID})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		return nil
	})
}
