package scan

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
	"github.com/miguel-anay/career-ops-saas/api/internal/platform"
	"github.com/miguel-anay/career-ops-saas/api/internal/queue"
)

// Service contains business logic for the scan domain.
type Service struct {
	pool *pgxpool.Pool
}

// NewService creates a new scan Service.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// scanCompanyPayload is the pg-boss job payload for "scan-company".
type scanCompanyPayload struct {
	UserID    uuid.UUID `json:"user_id"`
	CompanyID uuid.UUID `json:"company_id"`
	ScanRunID uuid.UUID `json:"scan_run_id"`
}

// TriggerScan creates a scan_run row and enqueues one "scan-company" job per
// enabled watched company. Returns the scan_run ID.
//
// The watched-companies read and the scan_run insert run inside a single
// platform.WithTenantTx so both are RLS-scoped to userID. The queue.Enqueue
// loop stays OUTSIDE the tx (after commit) — pgboss.* has no RLS policy and
// must stay on the raw pool.
func (s *Service) TriggerScan(ctx context.Context, userID uuid.UUID) (uuid.UUID, error) {
	var companies []db.WatchedCompany
	var scanRun db.ScanRun

	err := platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
		// 1. List enabled watched companies for user.
		var err error
		companies, err = q.ListEnabledWatchedCompaniesByUser(ctx, userID)
		if err != nil {
			return fmt.Errorf("list enabled companies: %w", err)
		}

		// 2. Insert scan_run row (status defaults to 'running').
		scanRun, err = q.InsertScanRun(ctx, userID)
		if err != nil {
			return fmt.Errorf("insert scan_run: %w", err)
		}

		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}

	// 3. Enqueue one "scan-company" job per company.
	for _, company := range companies {
		payload, err := json.Marshal(scanCompanyPayload{
			UserID:    userID,
			CompanyID: company.ID,
			ScanRunID: scanRun.ID,
		})
		if err != nil {
			return uuid.Nil, fmt.Errorf("marshal scan-company payload: %w", err)
		}
		if err := queue.Enqueue(ctx, s.pool, queue.Job{
			Name: "scan-company",
			Data: json.RawMessage(payload),
		}); err != nil {
			// Log and continue — partial failure is acceptable.
			// The scan_run errors_json will be updated by the worker.
			_ = err
		}
	}

	return scanRun.ID, nil
}

// GetScanRun returns the scan_run record by ID, scoped to the requesting user.
// The lookup runs inside a platform.WithTenantTx scoped to userID, so a
// scan_run owned by another tenant is invisible at the DB layer (RLS USING
// denial) and GetScanRunByID itself returns sql.ErrNoRows — independent of
// the app-layer scanRun.UserID != userID check below, which is kept as
// defense-in-depth (the merged PR #8 IDOR fix).
func (s *Service) GetScanRun(ctx context.Context, userID, scanRunID uuid.UUID) (*db.ScanRun, error) {
	var scanRun db.ScanRun

	err := platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
		var err error
		scanRun, err = q.GetScanRunByID(ctx, scanRunID)
		if err != nil {
			return fmt.Errorf("get scan_run: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if scanRun.UserID != userID {
		return nil, sql.ErrNoRows
	}
	return &scanRun, nil
}
