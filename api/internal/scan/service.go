package scan

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
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

func (s *Service) queries() *db.Queries {
	sqlDB := stdlib.OpenDBFromPool(s.pool)
	return db.New(sqlDB)
}

// scanCompanyPayload is the pg-boss job payload for "scan-company".
type scanCompanyPayload struct {
	UserID    uuid.UUID `json:"user_id"`
	CompanyID uuid.UUID `json:"company_id"`
	ScanRunID uuid.UUID `json:"scan_run_id"`
}

// TriggerScan creates a scan_run row and enqueues one "scan-company" job per
// enabled watched company. Returns the scan_run ID.
func (s *Service) TriggerScan(ctx context.Context, userID uuid.UUID) (uuid.UUID, error) {
	q := s.queries()

	// 1. List enabled watched companies for user.
	companies, err := q.ListEnabledWatchedCompaniesByUser(ctx, userID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("list enabled companies: %w", err)
	}

	// 2. Insert scan_run row (status defaults to 'running').
	scanRun, err := q.InsertScanRun(ctx, userID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert scan_run: %w", err)
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

// GetScanRun returns the scan_run record by ID.
func (s *Service) GetScanRun(ctx context.Context, scanRunID uuid.UUID) (*db.ScanRun, error) {
	q := s.queries()
	scanRun, err := q.GetScanRunByID(ctx, scanRunID)
	if err != nil {
		return nil, fmt.Errorf("get scan_run: %w", err)
	}
	return &scanRun, nil
}
