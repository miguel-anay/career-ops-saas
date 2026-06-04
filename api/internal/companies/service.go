package companies

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
)

// ErrNotFound is returned when a company does not exist for this user.
var ErrNotFound = errors.New("not found")

// Service contains business logic for the companies domain.
type Service struct {
	pool *pgxpool.Pool
}

// NewService creates a new companies Service.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

func (s *Service) queries() *db.Queries {
	sqlDB := stdlib.OpenDBFromPool(s.pool)
	return db.New(sqlDB)
}

// DetectProvider infers the ATS provider from a careers URL hostname.
// Returns one of: "greenhouse", "ashby", "lever", "recruitee",
// "smartrecruiters", "workable", or "unknown".
func DetectProvider(careersURL string) string {
	if careersURL == "" {
		return "unknown"
	}
	parsed, err := url.Parse(careersURL)
	if err != nil {
		return "unknown"
	}
	hostname := strings.ToLower(parsed.Hostname())
	switch {
	case strings.Contains(hostname, "greenhouse.io"):
		return "greenhouse"
	case strings.Contains(hostname, "ashby.com") || strings.Contains(hostname, "ashbyhq.com"):
		return "ashby"
	case strings.Contains(hostname, "lever.co"):
		return "lever"
	case strings.Contains(hostname, "recruitee.com"):
		return "recruitee"
	case strings.Contains(hostname, "smartrecruiters.com"):
		return "smartrecruiters"
	case strings.Contains(hostname, "workable.com"):
		return "workable"
	default:
		return "unknown"
	}
}

// List returns all watched companies for the given user.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]db.WatchedCompany, error) {
	q := s.queries()
	companies, err := q.ListWatchedCompaniesByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list companies: %w", err)
	}
	return companies, nil
}

// Add creates a new watched company for the user.
// If providerID is empty, it is auto-detected from careersURL.
func (s *Service) Add(ctx context.Context, userID uuid.UUID, name, careersURL, providerID string) (*db.WatchedCompany, error) {
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	if providerID == "" {
		providerID = DetectProvider(careersURL)
	}

	q := s.queries()
	company, err := q.InsertWatchedCompany(ctx, db.InsertWatchedCompanyParams{
		UserID: userID,
		Name:   name,
		CareersUrl: sql.NullString{
			String: careersURL,
			Valid:  careersURL != "",
		},
		ProviderID: sql.NullString{
			String: providerID,
			Valid:  providerID != "" && providerID != "unknown",
		},
		AtsApiUrl: sql.NullString{},
		Enabled:   true,
	})
	if err != nil {
		return nil, fmt.Errorf("insert company: %w", err)
	}
	return &company, nil
}

// Remove deletes a watched company by ID.
// RLS ensures only the owning user's companies are visible.
func (s *Service) Remove(ctx context.Context, userID uuid.UUID, companyID uuid.UUID) error {
	q := s.queries()
	// Verify ownership before deletion (RLS handles it but we return clean error).
	company, err := q.GetWatchedCompanyByID(ctx, companyID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("get company: %w", err)
	}
	if company.UserID != userID {
		return ErrNotFound
	}

	if err := q.DeleteWatchedCompany(ctx, companyID); err != nil {
		return fmt.Errorf("delete company: %w", err)
	}
	return nil
}
