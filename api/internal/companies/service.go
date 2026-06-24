package companies

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
	"github.com/miguel-anay/career-ops-saas/api/internal/platform"
)

// ErrNotFound is returned when a company does not exist for this user.
var ErrNotFound = errors.New("not found")

// ErrCatalogNotFound is returned when a catalog entry does not exist.
var ErrCatalogNotFound = errors.New("catalog entry not found")

// ErrAlreadyWatched is returned when the user already watches that catalog company.
var ErrAlreadyWatched = errors.New("company already watched")

// Service contains business logic for the companies domain.
type Service struct {
	pool *pgxpool.Pool
}

// NewService creates a new companies Service.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
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
	var companies []db.WatchedCompany
	err := platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
		var err error
		companies, err = q.ListWatchedCompaniesByUser(ctx, userID)
		return err
	})
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

	var company db.WatchedCompany
	err := platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
		var err error
		company, err = q.InsertWatchedCompany(ctx, db.InsertWatchedCompanyParams{
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
			CompanyID: uuid.NullUUID{}, // manual entry — not linked to the catalog
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("insert company: %w", err)
	}
	return &company, nil
}

// ListCatalog returns the global, install-wide company catalog. The catalog
// is reference data with no RLS, so it is read directly on the pool — no
// tenant context is required (and none should be assumed).
func (s *Service) ListCatalog(ctx context.Context) ([]db.CompaniesCatalog, error) {
	catalog, err := platform.PoolQueries(s.pool).ListCompaniesCatalog(ctx)
	if err != nil {
		return nil, fmt.Errorf("list catalog: %w", err)
	}
	return catalog, nil
}

// AddFromCatalog adds a watched company for the user by resolving a catalog
// entry, so the careers URL / provider / ATS API URL are guaranteed valid
// (no free-text typos). The catalog lookup runs on the pool (global, no RLS);
// the watched_companies insert runs inside the tenant tx so RLS scopes the
// write to this user.
func (s *Service) AddFromCatalog(ctx context.Context, userID uuid.UUID, catalogID uuid.UUID) (*db.WatchedCompany, error) {
	entry, err := platform.PoolQueries(s.pool).GetCompaniesCatalogByID(ctx, catalogID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCatalogNotFound
		}
		return nil, fmt.Errorf("get catalog entry: %w", err)
	}

	var company db.WatchedCompany
	err = platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
		var err error
		company, err = q.InsertWatchedCompany(ctx, db.InsertWatchedCompanyParams{
			UserID: userID,
			Name:   entry.Name,
			CareersUrl: sql.NullString{
				String: entry.CareersUrl,
				Valid:  entry.CareersUrl != "",
			},
			ProviderID: sql.NullString{
				String: entry.ProviderID,
				Valid:  entry.ProviderID != "" && entry.ProviderID != "unknown",
			},
			AtsApiUrl: entry.AtsApiUrl,
			Enabled:   true,
			CompanyID: uuid.NullUUID{UUID: catalogID, Valid: true},
		})
		return err
	})
	if err != nil {
		// idx_watched_companies_user_company makes a second watch of the same
		// catalog company a unique violation — surface it as a clean error.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrAlreadyWatched
		}
		return nil, fmt.Errorf("insert company from catalog: %w", err)
	}
	return &company, nil
}

// Remove deletes a watched company by ID.
// DeleteWatchedCompany has no `WHERE user_id` clause in the sqlc query
// itself — RLS is the ONLY mechanism preventing cross-tenant deletion.
// GetWatchedCompanyByID and DeleteWatchedCompany MUST run inside the SAME
// tenant tx so the GUC-scoping invariant is consistent across both
// statements (a non-owner's row is invisible to the SELECT under RLS
// `USING`, so the lookup itself fails before DELETE is ever attempted).
func (s *Service) Remove(ctx context.Context, userID uuid.UUID, companyID uuid.UUID) error {
	err := platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
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
	})
	if err != nil {
		return err
	}
	return nil
}
